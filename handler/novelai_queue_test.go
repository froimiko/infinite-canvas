package handler

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/basketikun/infinite-canvas/model"
)

// newTestNovelAIQueue 返回一个独立队列，避免测试之间通过全局注册表相互污染。
func newTestNovelAIQueue() *novelAIQueue {
	return newNovelAIQueue()
}

// testKeySeq 为 EWMA 统计表（novelAIDurationSamples）和队列注册表（novelAIQueues）
// 生成唯一 key。这两张表都是包级 sync.Map，进程内不会随测试结束而重置，
// 因此写死常量 key 在 `go test -count=2` 时第二轮会读到上一轮的残留而失败。
var testKeySeq atomic.Int64

func uniqueTestKey(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, testKeySeq.Add(1))
}

func testNovelAIQueueLimits() novelAIQueueLimits {
	return novelAIQueueLimitsFrom(nil)
}

func TestNovelAIQueueTicketsAreMonotonic(t *testing.T) {
	queue := newTestNovelAIQueue()
	limits := testNovelAIQueueLimits()

	var previous int64
	for i := 0; i < 5; i++ {
		entry, err := queue.enqueue("user-1", 1, limits)
		if err != nil {
			t.Fatalf("enqueue #%d failed: %v", i, err)
		}
		if entry.ticket <= previous {
			t.Fatalf("ticket #%d = %d, want strictly greater than %d", i, entry.ticket, previous)
		}
		previous = entry.ticket
	}
}

// 票号必须在 dequeue 之后继续递增，不能复用已注销的号。
// 若复用，后来的请求会被算作「排在前面」，FIFO 顺序就乱了。
func TestNovelAIQueueTicketsDoNotReuseAfterDequeue(t *testing.T) {
	queue := newTestNovelAIQueue()
	limits := testNovelAIQueueLimits()

	first, err := queue.enqueue("user-1", 1, limits)
	if err != nil {
		t.Fatalf("first enqueue failed: %v", err)
	}
	queue.dequeue(first.ticket)

	second, err := queue.enqueue("user-1", 1, limits)
	if err != nil {
		t.Fatalf("second enqueue failed: %v", err)
	}
	if second.ticket <= first.ticket {
		t.Fatalf("ticket after dequeue = %d, want greater than %d", second.ticket, first.ticket)
	}
}

// imagesAhead 统计的是「张数」而不是「请求数」：
// 三个请求分别要 1/4/2 张，第三个请求前方应为 1+4=5 张。
func TestNovelAIQueueImagesAheadCountsImagesNotRequests(t *testing.T) {
	queue := newTestNovelAIQueue()
	limits := testNovelAIQueueLimits()

	first, err := queue.enqueue("user-1", 1, limits)
	if err != nil {
		t.Fatalf("enqueue first failed: %v", err)
	}
	second, err := queue.enqueue("user-2", 4, limits)
	if err != nil {
		t.Fatalf("enqueue second failed: %v", err)
	}
	third, err := queue.enqueue("user-3", 2, limits)
	if err != nil {
		t.Fatalf("enqueue third failed: %v", err)
	}

	if got := queue.imagesAhead(first.ticket); got != 0 {
		t.Fatalf("imagesAhead(first) = %d, want 0", got)
	}
	if got := queue.imagesAhead(second.ticket); got != 1 {
		t.Fatalf("imagesAhead(second) = %d, want 1", got)
	}
	if got := queue.imagesAhead(third.ticket); got != 5 {
		t.Fatalf("imagesAhead(third) = %d, want 5 (1+4)", got)
	}
	if got := queue.queuedImages(); got != 7 {
		t.Fatalf("queuedImages = %d, want 7 (1+4+2)", got)
	}
}

func TestNovelAIQueueImagesAheadShrinksAfterDequeue(t *testing.T) {
	queue := newTestNovelAIQueue()
	limits := testNovelAIQueueLimits()

	first, err := queue.enqueue("user-1", 1, limits)
	if err != nil {
		t.Fatalf("enqueue first failed: %v", err)
	}
	second, err := queue.enqueue("user-2", 4, limits)
	if err != nil {
		t.Fatalf("enqueue second failed: %v", err)
	}
	third, err := queue.enqueue("user-3", 2, limits)
	if err != nil {
		t.Fatalf("enqueue third failed: %v", err)
	}

	if got := queue.imagesAhead(third.ticket); got != 5 {
		t.Fatalf("imagesAhead(third) before dequeue = %d, want 5", got)
	}

	queue.dequeue(first.ticket)
	if got := queue.imagesAhead(third.ticket); got != 4 {
		t.Fatalf("imagesAhead(third) after first dequeue = %d, want 4", got)
	}

	queue.dequeue(second.ticket)
	if got := queue.imagesAhead(third.ticket); got != 0 {
		t.Fatalf("imagesAhead(third) after second dequeue = %d, want 0", got)
	}
	if got := queue.queuedImages(); got != 2 {
		t.Fatalf("queuedImages = %d, want 2 (only third remains)", got)
	}
}

// markProgress 供 Phase 2 批量逐张回调递减剩余张数，
// 递减后后方请求看到的前方张数必须同步变小。
func TestNovelAIQueueMarkProgressUpdatesImagesAhead(t *testing.T) {
	queue := newTestNovelAIQueue()
	limits := testNovelAIQueueLimits()

	first, err := queue.enqueue("user-1", 4, limits)
	if err != nil {
		t.Fatalf("enqueue first failed: %v", err)
	}
	second, err := queue.enqueue("user-2", 1, limits)
	if err != nil {
		t.Fatalf("enqueue second failed: %v", err)
	}

	if got := queue.imagesAhead(second.ticket); got != 4 {
		t.Fatalf("imagesAhead(second) = %d, want 4", got)
	}

	queue.markProgress(first.ticket, 1)
	if got := queue.imagesAhead(second.ticket); got != 1 {
		t.Fatalf("imagesAhead(second) after progress = %d, want 1", got)
	}

	// 负数要被夹到 0，不能让前方张数变成负值把预估算成负时间。
	queue.markProgress(first.ticket, -3)
	if got := queue.imagesAhead(second.ticket); got != 0 {
		t.Fatalf("imagesAhead(second) after negative progress = %d, want 0", got)
	}
}

func TestNovelAIQueueAcquireRespectsContextCancel(t *testing.T) {
	queue := newTestNovelAIQueue()

	if err := queue.acquire(context.Background()); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}

	// 队列已被占用，带取消的 acquire 必须及时返回而不是永久阻塞。
	// 这正是不能用 sync.Mutex 的原因：Mutex.Lock 无法被 ctx 打断。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := queue.acquire(ctx); err == nil {
		t.Fatal("acquire on busy queue with canceled ctx should fail")
	}

	queue.release()
	if err := queue.acquire(context.Background()); err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
	queue.release()
}

// ctx 取消时票号必须被注销、slot 必须被归还。
// 验证方式：取消的请求返回后，另一个请求应能「立即」拿到锁并看到前方 0 张。
// 若清理失败，这里会超时 —— 那正是历史上「之后每次都 502，只能删控件」的成因。
func TestWithNovelAIQueueCleansUpOnContextCancel(t *testing.T) {
	channel := model.ModelChannel{
		BaseURL: "https://image.novelai.net",
		APIKey:  uniqueTestKey("cleanup-token"),
		FreeGenerationLock: &model.FreeGenerationLock{
			Enabled: true,
		},
	}
	queue := novelAIQueueFor(channel)

	holderStarted := make(chan struct{})
	releaseHolder := make(chan struct{})
	holderDone := make(chan error, 1)

	go func() {
		_, err := withNovelAIQueue(context.Background(), channel, "", "holder", 1, nil,
			func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
				close(holderStarted)
				<-releaseHolder
				return []map[string]interface{}{{"ok": true}}, nil
			})
		holderDone <- err
	}()
	<-holderStarted

	// 第二个请求进入排队，然后被取消。
	ctx, cancel := context.WithCancel(context.Background())
	canceledDone := make(chan error, 1)
	go func() {
		_, err := withNovelAIQueue(ctx, channel, "", "canceled-user", 3, nil,
			func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
				return nil, nil
			})
		canceledDone <- err
	}()

	// 等它确实登记进队列（否则可能在 enqueue 之前就被取消，测不到清理路径）。
	deadline := time.After(2 * time.Second)
	for queue.queuedImages() < 4 {
		select {
		case <-deadline:
			t.Fatalf("canceled request never enqueued, queuedImages=%d", queue.queuedImages())
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	select {
	case err := <-canceledDone:
		if err == nil {
			t.Fatal("canceled request should return an error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled request did not return; ctx cancel was not honored while queued")
	}

	// 票号已注销：只剩持锁者那 1 张。
	if got := queue.queuedImages(); got != 1 {
		t.Fatalf("queuedImages after cancel = %d, want 1 (canceled entry must be dequeued)", got)
	}

	close(releaseHolder)
	if err := <-holderDone; err != nil {
		t.Fatalf("holder returned error: %v", err)
	}

	// slot 已归还：新请求应立即拿到锁且前方 0 张。
	acquired := make(chan int, 1)
	nextDone := make(chan error, 1)
	go func() {
		_, err := withNovelAIQueue(context.Background(), channel, "", "next", 1, nil,
			func(entry *novelAIQueueEntry) ([]map[string]interface{}, error) {
				acquired <- queue.imagesAhead(entry.Ticket())
				return []map[string]interface{}{{"ok": true}}, nil
			})
		nextDone <- err
	}()
	select {
	case ahead := <-acquired:
		if ahead != 0 {
			t.Fatalf("imagesAhead for next request = %d, want 0 (stale entries leaked)", ahead)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("next request could not acquire slot; slot was leaked")
	}

	// 必须等 withNovelAIQueue 整体返回后再断言清空：acquired 是在 fn *内部* 发出的，
	// 此时 defer dequeue 尚未执行，直接断言会读到 1 张而偶发失败（此前就是这样 flaky 的）。
	if err := <-nextDone; err != nil {
		t.Fatalf("next request returned error: %v", err)
	}
	if got := queue.queuedImages(); got != 0 {
		t.Fatalf("queuedImages at end = %d, want 0", got)
	}
}

func TestNovelAIQueueEnforcesPerUserImageLimit(t *testing.T) {
	queue := newTestNovelAIQueue()
	limits := novelAIQueueLimits{
		EstimatedSecondsPerImage: 12,
		MaxUserQueuedImages:      5,
		MaxQueuedImages:          500,
	}

	if _, err := queue.enqueue("heavy-user", 4, limits); err != nil {
		t.Fatalf("first enqueue within limit failed: %v", err)
	}

	// 4 + 3 > 5，必须拦截，且错误信息要能让用户看懂现状。
	_, err := queue.enqueue("heavy-user", 3, limits)
	if err == nil {
		t.Fatal("expected per-user image limit to reject the request")
	}
	if !strings.Contains(err.Error(), "5") {
		t.Fatalf("error = %q, want it to mention the limit", err.Error())
	}

	// 同一上限不应影响其他用户。
	if _, err := queue.enqueue("other-user", 4, limits); err != nil {
		t.Fatalf("different user should not be blocked by per-user limit: %v", err)
	}

	// 恰好压线（4+1=5）应放行。
	if _, err := queue.enqueue("heavy-user", 1, limits); err != nil {
		t.Fatalf("enqueue exactly at limit failed: %v", err)
	}
}

func TestNovelAIQueueEnforcesGlobalImageLimit(t *testing.T) {
	queue := newTestNovelAIQueue()
	limits := novelAIQueueLimits{
		EstimatedSecondsPerImage: 12,
		MaxUserQueuedImages:      100,
		MaxQueuedImages:          6,
	}

	if _, err := queue.enqueue("user-1", 4, limits); err != nil {
		t.Fatalf("enqueue user-1 failed: %v", err)
	}
	if _, err := queue.enqueue("user-2", 2, limits); err != nil {
		t.Fatalf("enqueue user-2 failed: %v", err)
	}

	if _, err := queue.enqueue("user-3", 1, limits); err == nil {
		t.Fatal("expected global queue limit to reject the request")
	}
}

// 空 userID（内部调用/匿名）不应触发单用户上限，
// 否则所有匿名请求会被当成同一个人互相挤占。
func TestNovelAIQueueEmptyUserIDSkipsPerUserLimit(t *testing.T) {
	queue := newTestNovelAIQueue()
	limits := novelAIQueueLimits{
		EstimatedSecondsPerImage: 12,
		MaxUserQueuedImages:      2,
		MaxQueuedImages:          500,
	}

	for i := 0; i < 5; i++ {
		if _, err := queue.enqueue("", 1, limits); err != nil {
			t.Fatalf("anonymous enqueue #%d failed: %v", i, err)
		}
	}
}

func TestNovelAIQueueLimitsFallBackToDefaults(t *testing.T) {
	// nil 配置（渠道未启用免费锁时的默认路径）
	limits := novelAIQueueLimitsFrom(nil)
	if limits.EstimatedSecondsPerImage != defaultNovelAIEstimatedSecondsPerImage {
		t.Fatalf("estimatedSecondsPerImage = %d, want %d", limits.EstimatedSecondsPerImage, defaultNovelAIEstimatedSecondsPerImage)
	}
	if limits.MaxUserQueuedImages != defaultNovelAIMaxUserQueuedImages {
		t.Fatalf("maxUserQueuedImages = %d, want %d", limits.MaxUserQueuedImages, defaultNovelAIMaxUserQueuedImages)
	}
	if limits.MaxQueuedImages != defaultNovelAIMaxQueuedImages {
		t.Fatalf("maxQueuedImages = %d, want %d", limits.MaxQueuedImages, defaultNovelAIMaxQueuedImages)
	}

	// 老配置：三个字段缺失，JSON 反序列化后为 0，必须零迁移回落默认值。
	legacy := novelAIQueueLimitsFrom(&model.FreeGenerationLock{Enabled: true})
	if legacy != limits {
		t.Fatalf("legacy limits = %+v, want defaults %+v", legacy, limits)
	}

	// 负数同样视为未配置。
	negative := novelAIQueueLimitsFrom(&model.FreeGenerationLock{
		Enabled:                  true,
		EstimatedSecondsPerImage: -1,
		MaxUserQueuedImages:      -5,
		MaxQueuedImages:          -10,
	})
	if negative != limits {
		t.Fatalf("negative limits = %+v, want defaults %+v", negative, limits)
	}

	// 显式配置生效。
	custom := novelAIQueueLimitsFrom(&model.FreeGenerationLock{
		Enabled:                  true,
		EstimatedSecondsPerImage: 3,
		MaxUserQueuedImages:      7,
		MaxQueuedImages:          77,
	})
	if custom.EstimatedSecondsPerImage != 3 || custom.MaxUserQueuedImages != 7 || custom.MaxQueuedImages != 77 {
		t.Fatalf("custom limits = %+v, want 3/7/77", custom)
	}
}

func TestRecordNovelAIDurationEWMAConverges(t *testing.T) {
	testModel := uniqueTestKey("test-ewma-converge")

	if _, ok := novelAIAverageSeconds(testModel); ok {
		t.Fatal("expected no samples for a fresh model key")
	}

	// 首个样本直接赋值：若从 0 起做 EWMA，前几次预估会被 0 严重拉低。
	recordNovelAIDuration(testModel, 12*time.Second)
	avg, ok := novelAIAverageSeconds(testModel)
	if !ok {
		t.Fatal("expected a sample after first record")
	}
	if avg < 11.9 || avg > 12.1 {
		t.Fatalf("avg after first sample = %v, want ~12", avg)
	}

	// 持续喂同一量级的样本，平均值应稳定收敛在该量级附近。
	for i := 0; i < 10; i++ {
		recordNovelAIDuration(testModel, 12*time.Second)
	}
	avg, _ = novelAIAverageSeconds(testModel)
	if avg < 11.5 || avg > 12.5 {
		t.Fatalf("avg after steady samples = %v, want ~12", avg)
	}

	// 切到 V3 量级（约 2s）后应朝新值移动，但不会一步跳到位（滑动平均特性）。
	for i := 0; i < 8; i++ {
		recordNovelAIDuration(testModel, 2*time.Second)
	}
	avg, _ = novelAIAverageSeconds(testModel)
	if avg >= 6 {
		t.Fatalf("avg after switching to 2s samples = %v, want it to move well below 6", avg)
	}
	if avg <= 1.9 {
		t.Fatalf("avg = %v, want it to stay above the sample floor (EWMA should not overshoot)", avg)
	}
}

// 非法样本不能污染平均值，否则预估会出现 0/NaN 之类的荒谬数字。
func TestRecordNovelAIDurationIgnoresInvalidSamples(t *testing.T) {
	testModel := uniqueTestKey("test-ewma-invalid")

	recordNovelAIDuration(testModel, 0)
	recordNovelAIDuration(testModel, -5*time.Second)
	recordNovelAIDuration("", 10*time.Second)

	if _, ok := novelAIAverageSeconds(testModel); ok {
		t.Fatal("invalid durations should not be recorded as samples")
	}
	if _, ok := novelAIAverageSeconds(""); ok {
		t.Fatal("empty model key should not be recorded")
	}
}

func TestEstimateNovelAISecondsUsesFallbackWithoutSamples(t *testing.T) {
	testModel := uniqueTestKey("test-estimate-fallback")

	if got := estimateNovelAISeconds(testModel, 3, 12); got != 36 {
		t.Fatalf("estimate = %d, want 36 (3 images * 12s fallback)", got)
	}
	// fallback 非法时回落到内置默认值。
	if got := estimateNovelAISeconds(testModel, 1, 0); got != defaultNovelAIEstimatedSecondsPerImage {
		t.Fatalf("estimate with zero fallback = %d, want %d", got, defaultNovelAIEstimatedSecondsPerImage)
	}
	// 0 张无需等待。
	if got := estimateNovelAISeconds(testModel, 0, 12); got != 0 {
		t.Fatalf("estimate for 0 images = %d, want 0", got)
	}
}

func TestEstimateNovelAISecondsPrefersRecordedAverage(t *testing.T) {
	testModel := uniqueTestKey("test-estimate-average")

	recordNovelAIDuration(testModel, 2*time.Second)
	// 有真实样本时忽略 fallback，用 EWMA 平均值。
	if got := estimateNovelAISeconds(testModel, 3, 12); got != 6 {
		t.Fatalf("estimate = %d, want 6 (3 images * 2s measured average)", got)
	}
	// 已确定要出图，预估不能返回 0（前端会显示"即将完成"然后长时间不动）。
	if got := estimateNovelAISeconds(testModel, 1, 12); got < 1 {
		t.Fatalf("estimate = %d, want at least 1", got)
	}
}

// 未启用免费生图锁的渠道是付费并发模式：不排队、不占票号、直接执行。
func TestWithNovelAIQueueSkipsQueueWhenLockDisabled(t *testing.T) {
	cases := []struct {
		name    string
		channel model.ModelChannel
	}{
		{
			name: "nil lock",
			channel: model.ModelChannel{
				BaseURL: "https://image.novelai.net",
				APIKey:  uniqueTestKey("no-lock-token"),
			},
		},
		{
			name: "lock disabled",
			channel: model.ModelChannel{
				BaseURL:            "https://image.novelai.net",
				APIKey:             uniqueTestKey("disabled-lock-token"),
				FreeGenerationLock: &model.FreeGenerationLock{Enabled: false},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 并发执行两次：未启用锁时不应互相串行。
			started := make(chan struct{})
			release := make(chan struct{})
			done := make(chan error, 2)

			go func() {
				_, err := withNovelAIQueue(context.Background(), tc.channel, "", "user-1", 1, nil,
					func(entry *novelAIQueueEntry) ([]map[string]interface{}, error) {
						if entry != nil {
							t.Error("entry should be nil when the free-generation lock is disabled")
						}
						close(started)
						<-release
						return []map[string]interface{}{{"ok": true}}, nil
					})
				done <- err
			}()
			<-started

			go func() {
				_, err := withNovelAIQueue(context.Background(), tc.channel, "", "user-2", 1, nil,
					func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
						return []map[string]interface{}{{"ok": true}}, nil
					})
				done <- err
			}()

			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("second call returned error: %v", err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("second call was serialized even though the free-generation lock is disabled")
			}

			close(release)
			if err := <-done; err != nil {
				t.Fatalf("first call returned error: %v", err)
			}

			if got := novelAIQueueFor(tc.channel).queuedImages(); got != 0 {
				t.Fatalf("queuedImages = %d, want 0 (disabled lock must not enqueue)", got)
			}
		})
	}
}

// onQueueUpdate 是 Phase 2 SSE 推送的挂载点：入队后必须立即回调一次初始状态，
// 这样调用方能在 Cloudflare 100s 响应头超时之前先把响应头发出去。
func TestWithNovelAIQueueReportsInitialQueueState(t *testing.T) {
	channel := model.ModelChannel{
		BaseURL: "https://image.novelai.net",
		APIKey:  uniqueTestKey("queue-update-token"),
		FreeGenerationLock: &model.FreeGenerationLock{
			Enabled:                  true,
			EstimatedSecondsPerImage: 10,
		},
	}

	holderStarted := make(chan struct{})
	releaseHolder := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		_, err := withNovelAIQueue(context.Background(), channel, "", "holder", 2, nil,
			func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
				close(holderStarted)
				<-releaseHolder
				return []map[string]interface{}{{"ok": true}}, nil
			})
		holderDone <- err
	}()
	<-holderStarted

	type update struct {
		ahead     int
		estimated int
	}
	updates := make(chan update, 4)
	waiterDone := make(chan error, 1)
	go func() {
		_, err := withNovelAIQueue(context.Background(), channel, "", "waiter", 1,
			func(imagesAhead int, estimatedSeconds int) {
				select {
				case updates <- update{ahead: imagesAhead, estimated: estimatedSeconds}:
				default:
				}
			},
			func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
				return []map[string]interface{}{{"ok": true}}, nil
			})
		waiterDone <- err
	}()

	select {
	case got := <-updates:
		if got.ahead != 2 {
			t.Fatalf("imagesAhead in first update = %d, want 2 (holder needs 2 images)", got.ahead)
		}
		// ahead(2) + own(1) = 3 张，冷启动 10s/张 → 30s。
		if got.estimated != 30 {
			t.Fatalf("estimatedSeconds = %d, want 30 (3 images * 10s cold-start estimate)", got.estimated)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onQueueUpdate was not invoked with the initial queue state")
	}

	close(releaseHolder)
	if err := <-holderDone; err != nil {
		t.Fatalf("holder returned error: %v", err)
	}
	if err := <-waiterDone; err != nil {
		t.Fatalf("waiter returned error: %v", err)
	}
}

// 队列必须按渠道+Token 隔离：不同 Token 是不同的 NovelAI 账号，各有独立的串行域。
func TestNovelAIQueueForIsolatesChannelsByToken(t *testing.T) {
	channelA := model.ModelChannel{
		BaseURL:            "https://image.novelai.net",
		APIKey:             uniqueTestKey("isolate-token-a"),
		FreeGenerationLock: &model.FreeGenerationLock{Enabled: true},
	}
	channelB := model.ModelChannel{
		BaseURL:            "https://image.novelai.net",
		APIKey:             uniqueTestKey("isolate-token-b"),
		FreeGenerationLock: &model.FreeGenerationLock{Enabled: true},
	}

	if novelAIQueueFor(channelA) == novelAIQueueFor(channelB) {
		t.Fatal("different tokens must map to different queues")
	}
	// 同一渠道多次获取必须是同一实例，否则串行锁形同虚设。
	if novelAIQueueFor(channelA) != novelAIQueueFor(channelA) {
		t.Fatal("same channel must map to the same queue instance")
	}
	// 尾部斜杠差异不应产生两个队列（key 里 baseURL 已 TrimRight）。
	channelATrailing := channelA
	channelATrailing.BaseURL = "https://image.novelai.net/"
	if novelAIQueueFor(channelA) != novelAIQueueFor(channelATrailing) {
		t.Fatal("trailing slash in baseURL must not create a separate queue")
	}
}

// release 在未持有 slot 时被误调用不能阻塞或 panic，
// 否则一次编程错误就会连带卡死整条请求链。
func TestNovelAIQueueReleaseWithoutAcquireIsSafe(t *testing.T) {
	queue := newTestNovelAIQueue()

	done := make(chan struct{})
	go func() {
		queue.release()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("release without acquire blocked")
	}

	// 状态未被破坏：仍能正常 acquire / release。
	if err := queue.acquire(context.Background()); err != nil {
		t.Fatalf("acquire after stray release failed: %v", err)
	}
	queue.release()
}

// EWMA 的「记录侧」与「预估侧」必须用同一个 key，且这个 key 必须是 NovelAI 模型 ID。
//
// 防的是一个真实发生过的 bug：withNovelAIQueue 计算预估等待时间时，传给
// estimateNovelAISeconds 的第一个参数误用了 channel.Name（渠道**显示名**，例如
// 「NovelAI官方」），而 recordNovelAIDuration 写入样本时用的 key 是 NovelAI **模型 ID**
// （例如 nai-diffusion-4-5-full）。两个 key 永不相等 → novelAIDurationSamples 查表恒定
// miss → 预估永远回落到渠道配置的冷启动值。实际后果：V3 用户真实约 2s/张，却被告知
// 「预计等待 12 秒」，而且喂再多样本都不会自愈（因为样本压根查不到）。
// 修复方式是给 withNovelAIQueue 增加显式的 naiModel 参数，由调用方传 naiReq.Model。
//
// 这个断言为什么能防住它：样本按模型 ID 记成 2s，而冷启动值故意设成好识别的 99s。
// key 一致时 3 张的预估就是 3*2=6s；一旦有人把 naiModel 改回 channel.Name，查表 miss、
// 预估变成 3*99=297s（冷启动值的倍数），断言立刻失败。
func TestWithNovelAIQueueEstimateUsesModelKeyNotChannelName(t *testing.T) {
	// 模型 ID 和渠道名都走 uniqueTestKey：novelAIDurationSamples 是包级 sync.Map，
	// 进程内不随测试结束重置，写死常量在 `go test -count=2` 第二轮会读到上一轮残留。
	// 渠道名同样必须唯一 —— 否则回归版本（误用 channel.Name）在多轮下可能恰好读到
	// 前一轮遗留在该 key 上的样本，反而「碰巧」通过，测试就白写了。
	naiModel := uniqueTestKey("nai-diffusion-4-5-full")
	channelName := uniqueTestKey("渠道显示名")

	// 好识别的冷启动值：预估结果里只要出现 99 的倍数，就说明 EWMA 查表 miss 了。
	const coldStartSeconds = 99
	channel := model.ModelChannel{
		Name:    channelName,
		BaseURL: "https://image.novelai.net",
		APIKey:  uniqueTestKey("estimate-key-token"),
		FreeGenerationLock: &model.FreeGenerationLock{
			Enabled:                  true,
			EstimatedSecondsPerImage: coldStartSeconds,
		},
	}

	// 记录侧：按「模型 ID」喂入 2s 样本（V3 的真实量级），与冷启动值 99 差异明显。
	// 全部样本同值，EWMA 会稳定在 2.0，预估结果因此是确定值而非区间。
	for i := 0; i < 5; i++ {
		recordNovelAIDuration(naiModel, 2*time.Second)
	}
	avg, ok := novelAIAverageSeconds(naiModel)
	if !ok || avg < 1.9 || avg > 2.1 {
		t.Fatalf("average for the model key = (%v, %v), want ~2s recorded", avg, ok)
	}
	// 渠道名这个 key 上不该有任何样本 —— 这正是当年恒定 miss 的直接原因。
	// 这行同时保证下面的断言不是空跑：它钉死了「按渠道名查 → 297s」这条对照基线，
	// 若两侧 key 被写成同一个值，297 与下面期望的 6 不可能同时成立。
	if got := estimateNovelAISeconds(channelName, 3, coldStartSeconds); got != 3*coldStartSeconds {
		t.Fatalf("estimate keyed by channel name = %d, want %d (the channel name must carry no EWMA samples)", got, 3*coldStartSeconds)
	}

	// holder 先占住渠道队列（2 张），waiter 随后入队 1 张，
	// 于是 waiter 的初始回调里 ahead=2、参与预估的总张数是 ahead+own=3。
	holderStarted := make(chan struct{})
	releaseHolder := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		_, err := withNovelAIQueue(context.Background(), channel, naiModel, "holder", 2, nil,
			func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
				close(holderStarted)
				<-releaseHolder
				return []map[string]interface{}{{"ok": true}}, nil
			})
		holderDone <- err
	}()
	<-holderStarted

	type update struct {
		ahead     int
		estimated int
	}
	updates := make(chan update, 4)
	waiterDone := make(chan error, 1)
	go func() {
		_, err := withNovelAIQueue(context.Background(), channel, naiModel, "waiter", 1,
			func(imagesAhead int, estimatedSeconds int) {
				select {
				case updates <- update{ahead: imagesAhead, estimated: estimatedSeconds}:
				default:
				}
			},
			func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
				return []map[string]interface{}{{"ok": true}}, nil
			})
		waiterDone <- err
	}()

	select {
	case got := <-updates:
		// 入队后立即发出的第一次回调（非 ticker 周期回调），此时 holder 仍持锁。
		if got.ahead != 2 {
			t.Fatalf("imagesAhead in first update = %d, want 2 (holder needs 2 images)", got.ahead)
		}
		// 3 张 * 2s 实测均值 = 6s。
		if got.estimated != 6 {
			t.Fatalf(
				"estimatedSeconds = %d, want 6 (3 images * 2s recorded EWMA average); "+
					"a value of %d (= 3 * the %ds cold-start fallback) means the EWMA lookup missed, "+
					"i.e. estimateNovelAISeconds was keyed by channel.Name instead of the NovelAI model ID",
				got.estimated, 3*coldStartSeconds, coldStartSeconds,
			)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onQueueUpdate was not invoked with the initial queue state")
	}

	close(releaseHolder)
	if err := <-holderDone; err != nil {
		t.Fatalf("holder returned error: %v", err)
	}
	if err := <-waiterDone; err != nil {
		t.Fatalf("waiter returned error: %v", err)
	}
}
