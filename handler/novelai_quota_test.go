package handler

// NovelAI V5 充能条配额守卫的单元测试。
//
// ⚠️ 测试纪律（与 novelai_queue_test.go 同源的历史教训）：
// novelAIQuotaCaches 是**包级 sync.Map**，-count=N 重复运行时会跨轮串味 ——
// 上一轮缓存的配额值被下一轮读到，测试会碰巧通过。因此每个触碰缓存的用例都必须
// 用 quotaTestChannel 生成唯一的 baseURL+Token 组合（缓存 key 由二者哈希而来），
// 并在开头 resetNovelAIQuotaCacheForTest。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/basketikun/infinite-canvas/model"
)

// quotaTestSeq 保证每个用例拿到互不相同的缓存 key。
var quotaTestSeq atomic.Int64

// quotaTestChannel 构造一个带唯一 Token 的 NovelAI 渠道。
// baseURL 指向测试服务器；APIKey 唯一 → 缓存 key 唯一 → 用例之间互不干扰。
func quotaTestChannel(t *testing.T, baseURL string, lock *model.FreeGenerationLock) model.ModelChannel {
	t.Helper()
	channel := model.ModelChannel{
		Protocol:           "novelai",
		Name:               fmt.Sprintf("quota-test-%d", quotaTestSeq.Add(1)),
		BaseURL:            baseURL,
		APIKey:             fmt.Sprintf("token-%s-%d", t.Name(), quotaTestSeq.Add(1)),
		FreeGenerationLock: lock,
	}
	resetNovelAIQuotaCacheForTest(channel)
	t.Cleanup(func() { resetNovelAIQuotaCacheForTest(channel) })
	return channel
}

// guardLock 返回一个「免费锁开启 + 守卫开启」的配置。
func guardLock() *model.FreeGenerationLock {
	return &model.FreeGenerationLock{
		Enabled:              true,
		MaxPixels:            1048576,
		MaxSteps:             28,
		V5QuotaGuardEnabled:  true,
		V5QuotaReserveImages: quotaReserve(1),
	}
}

// quotaReserve 是 *int 字面量的小助手。
// V5QuotaReserveImages 用指针是为了区分「未配置」(nil→默认1) 与「显式 0」(不保留)。
func quotaReserve(value int) *int { return &value }

// subscriptionServer 起一个假的 /user/subscription。
// 返回服务器与「被请求次数」计数器，用于验证缓存/单飞与「非 V5 零查询」。
func subscriptionServer(t *testing.T, body string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/subscription" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

// opusBody 生成一个有效 Opus 订阅 + 指定配额的响应体。
//
// ⚠️ percent 必须用 strconv.FormatFloat(-1) 输出「能精确 round-trip 的最短表示」，
// 不能用 %f（只有 6 位小数）。本文件多个用例把 percent 设成「恰好 N 张的量」来测
// 保留位边界，而单张只占 1/17.3 ≈ 0.0578%，%f 的截断误差就落在这个量级上 ——
// 会让边界用例随舍入方向时对时错。
func opusBody(percent float64, isNegative bool) string {
	return fmt.Sprintf(
		`{"tier":3,"active":true,"usage":{"percent":%s,"isNegative":%t,"timeUntilNextPercent":120}}`,
		strconv.FormatFloat(percent, 'f', -1, 64), isNegative,
	)
}

// quotaEpsilon 是一个远小于「单张配额」（≈0.0578%）但远大于 float64 ulp（≈1e-17）
// 的偏移量。
//
// 边界用例用它明确表达「刚好够」还是「差一点」，而不是依赖浮点精确相等：守卫内部的
// needPercent 是多次除法与加法的结果，与测试里构造的 percent 可能相差 1 ulp，用精确
// 相等做断言会让用例随舍入方向时对时错（典型的「本地过、CI 挂」）。
const quotaEpsilon = 1e-9

// quotaFor 返回「n 份配额」对应的百分比。
// 刻意复用守卫内部的同一个函数，避免测试自己重算一遍导致口径漂移。
func quotaFor(units int) float64 { return novelAIQuotaPercentForUnits(units) }

// ---------------------------------------------------------------------------
// 模型门禁：只有 V5 两个 ID 受限
// ---------------------------------------------------------------------------

func TestNovelAIModelHasOpusUsageLimitOnlyV5(t *testing.T) {
	limited := []string{"nai-diffusion-5-full", "nai-diffusion-5-curated"}
	for _, naiModel := range limited {
		if !novelAIModelHasOpusUsageLimit(naiModel) {
			t.Errorf("novelAIModelHasOpusUsageLimit(%q) = false, want true", naiModel)
		}
	}

	// V4.5 是最容易踩的坑：它的 ID 里同时含有 "4-5" 与结尾的 "5"，
	// 若用 strings.Contains("diffusion-5") 判定就会被误判成 V5 而被错误拦截。
	unlimited := []string{
		"nai-diffusion-4-5-full",
		"nai-diffusion-4-5-curated",
		"nai-diffusion-4-full",
		"nai-diffusion-4-curated-preview",
		"nai-diffusion-3",
		"nai-diffusion-2",
		"nai-diffusion-furry",
		"",
	}
	for _, naiModel := range unlimited {
		if novelAIModelHasOpusUsageLimit(naiModel) {
			t.Errorf("novelAIModelHasOpusUsageLimit(%q) = true, want false", naiModel)
		}
	}
}

// TestNovelAIQuotaModelGateMatchesResolvedIDs 串起「前端模型名 → 标准 ID → 是否受限」，
// 确保守卫吃的是 resolveNovelAIModel 的输出，而不是原始输入。
func TestNovelAIQuotaModelGateMatchesResolvedIDs(t *testing.T) {
	tests := []struct {
		input   string
		limited bool
	}{
		{"nai-diffusion-5-full", true},
		{"v5", true},
		{"NAI Diffusion V5 Curated", true},
		{"__cloud__::nai-diffusion-5-full", true},
		// V4.5 的小数点末尾是 5，绝不能被当成 V5。
		{"v4.5", false},
		{"NAI Diffusion V4.5 Curated", false},
		{"nai-diffusion-4-5-full", false},
		{"v3", false},
		{"xv5y", false}, // 词边界保护：不是 V5
	}
	for _, tt := range tests {
		resolved := resolveNovelAIModel(tt.input)
		if got := novelAIModelHasOpusUsageLimit(resolved); got != tt.limited {
			t.Errorf("model %q resolved to %q, hasLimit = %v, want %v", tt.input, resolved, got, tt.limited)
		}
	}
}

// ---------------------------------------------------------------------------
// 面积分档与百分比换算
// ---------------------------------------------------------------------------

func TestNovelAIQuotaUnitsForPixelsTiers(t *testing.T) {
	tests := []struct {
		pixels int
		units  int
	}{
		{0, 1},               // 非法输入按最小档
		{512 * 512, 1},       //
		{1024 * 1024, 1},     // 恰好 1MP 边界，仍是 1 份
		{1048577, 2},         // 越过 1MP 一个像素即进入 2 份
		{1747627, 2},         // 边界
		{1747628, 3},         //
		{2446678, 3},         // 边界
		{2446679, 4},         //
		{3145728, 4},         // 边界
		{4096 * 4096, 4},     // 超出最大档按最大档（保守）
	}
	for _, tt := range tests {
		if got := novelAIQuotaUnitsForPixels(tt.pixels); got != tt.units {
			t.Errorf("novelAIQuotaUnitsForPixels(%d) = %d, want %d", tt.pixels, got, tt.units)
		}
	}
}

func TestNovelAIQuotaPercentForUnits(t *testing.T) {
	// 1 张 1MP 图 ≈ 1/17.3 ≈ 0.0578%（参考实现 _imagesPerPercent = 17.3）。
	single := novelAIQuotaPercentForUnits(1)
	if single <= 0.057 || single >= 0.058 {
		t.Fatalf("single image percent = %f, want ≈0.0578", single)
	}
	// 线性：10 张应为单张的 10 倍。
	if got, want := novelAIQuotaPercentForUnits(10), single*10; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("10 units percent = %f, want %f", got, want)
	}
	if got := novelAIQuotaPercentForUnits(0); got != 0 {
		t.Fatalf("zero units percent = %f, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// 短路：不该碰网络的场景一次请求都不发
// ---------------------------------------------------------------------------

// TestEnsureNovelAIV5QuotaSkipsNonV5Models 是本特性最重要的回归测试。
//
// V4.5 / V4 / V3 仍然是无限免费小图，若守卫给它们也发订阅查询，就是给本来
// 免费无忧的模型平白加一个延迟点与失败点 —— 纯倒退。
func TestEnsureNovelAIV5QuotaSkipsNonV5Models(t *testing.T) {
	// 配额已透支：一旦真去查了，V5 会被拦截，非 V5 必须照样放行。
	server, calls := subscriptionServer(t, opusBody(-2, true))

	for _, naiModel := range []string{
		"nai-diffusion-4-5-full",
		"nai-diffusion-4-5-curated",
		"nai-diffusion-4-full",
		"nai-diffusion-3",
		"nai-diffusion-furry",
	} {
		channel := quotaTestChannel(t, server.URL, guardLock())
		if err := ensureNovelAIV5Quota(context.Background(), channel, naiModel, 1024, 1024, 4); err != nil {
			t.Fatalf("model %s must be allowed unconditionally, got: %v", naiModel, err)
		}
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("non-V5 models triggered %d subscription lookups, want 0", got)
	}
}

func TestEnsureNovelAIV5QuotaSkipsWhenGuardDisabled(t *testing.T) {
	server, calls := subscriptionServer(t, opusBody(-2, true))

	// 免费锁整体关闭 → 付费并发模式，守卫不介入。
	lockOff := guardLock()
	lockOff.Enabled = false
	channelLockOff := quotaTestChannel(t, server.URL, lockOff)
	if err := ensureNovelAIV5Quota(context.Background(), channelLockOff, "nai-diffusion-5-full", 1024, 1024, 1); err != nil {
		t.Fatalf("free lock disabled must allow, got: %v", err)
	}

	// 免费锁开着但守卫没勾 → 放行。
	guardOff := guardLock()
	guardOff.V5QuotaGuardEnabled = false
	channelGuardOff := quotaTestChannel(t, server.URL, guardOff)
	if err := ensureNovelAIV5Quota(context.Background(), channelGuardOff, "nai-diffusion-5-full", 1024, 1024, 1); err != nil {
		t.Fatalf("guard disabled must allow, got: %v", err)
	}

	// nil 配置 → 放行。
	channelNil := quotaTestChannel(t, server.URL, nil)
	if err := ensureNovelAIV5Quota(context.Background(), channelNil, "nai-diffusion-5-full", 1024, 1024, 1); err != nil {
		t.Fatalf("nil lock must allow, got: %v", err)
	}

	if got := calls.Load(); got != 0 {
		t.Fatalf("disabled guard triggered %d lookups, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// 配额判定
// ---------------------------------------------------------------------------

func TestEnsureNovelAIV5QuotaAllowsWhenQuotaSufficient(t *testing.T) {
	server, _ := subscriptionServer(t, opusBody(50, false))
	channel := quotaTestChannel(t, server.URL, guardLock())

	if err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 4); err != nil {
		t.Fatalf("50%% quota must cover 4 images, got: %v", err)
	}
}

func TestEnsureNovelAIV5QuotaBlocksWhenNegative(t *testing.T) {
	server, _ := subscriptionServer(t, opusBody(-2, true))
	channel := quotaTestChannel(t, server.URL, guardLock())

	err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-curated", 1024, 1024, 1)
	if err == nil {
		t.Fatal("exhausted quota must be blocked")
	}
	if !strings.Contains(err.Error(), "耗尽") {
		t.Fatalf("error should explain exhaustion, got: %v", err)
	}
	// 文案必须提示「其他模型仍免费」，否则用户会以为整个 NovelAI 都不能用了。
	if !strings.Contains(err.Error(), "V4.5") {
		t.Fatalf("error should suggest fallback models, got: %v", err)
	}
}

// TestEnsureNovelAIV5QuotaReservesLastImage 钉死「保留最后一张」。
//
// 这是本实现相对参考项目额外加的保护：isNegative 是事后信号，等它变 true 时
// 最后一张已经花掉了。所以剩余配额恰好只够 N 张时，请求 N 张必须被拒。
func TestEnsureNovelAIV5QuotaReservesLastImage(t *testing.T) {
	// 恰好够 2 张、不够 2+1 张 → 请求 2 张应被拒（要留 1 张）。
	// 用 -epsilon 明确落在「2 张够、3 张差一点」的区间，不依赖浮点精确相等。
	server, _ := subscriptionServer(t, opusBody(quotaFor(3)-quotaEpsilon, false))
	channel := quotaTestChannel(t, server.URL, guardLock())
	err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 2)
	if err == nil {
		t.Fatal("quota that exactly covers the request must be blocked to preserve the reserve")
	}
	if !strings.Contains(err.Error(), "保留") {
		t.Fatalf("error should mention the reserve, got: %v", err)
	}

	// 同样的余量请求 1 张则可以（1 张 + 1 张保留 = 2 张，有余）。
	channelOK := quotaTestChannel(t, server.URL, guardLock())
	if err := ensureNovelAIV5Quota(context.Background(), channelOK, "nai-diffusion-5-full", 1024, 1024, 1); err != nil {
		t.Fatalf("1 image + 1 reserve fits in this quota, got: %v", err)
	}

	// 保留位配成 0 时（管理员显式选择用到见底），够 2 张就该放行。
	noReserve := guardLock()
	noReserve.V5QuotaReserveImages = quotaReserve(0)
	channelNoReserve := quotaTestChannel(t, server.URL, noReserve)
	if err := ensureNovelAIV5Quota(context.Background(), channelNoReserve, "nai-diffusion-5-full", 1024, 1024, 2); err != nil {
		t.Fatalf("explicit reserve=0 must allow spending down to empty, got: %v", err)
	}

	// 负数同样按 0 处理（防御性输入纠正）。
	negativeReserve := guardLock()
	negativeReserve.V5QuotaReserveImages = quotaReserve(-1)
	channelNegative := quotaTestChannel(t, server.URL, negativeReserve)
	if err := ensureNovelAIV5Quota(context.Background(), channelNegative, "nai-diffusion-5-full", 1024, 1024, 2); err != nil {
		t.Fatalf("negative reserve must be treated as 0, got: %v", err)
	}
}

// TestEnsureNovelAIV5QuotaScalesWithArea 验证大图按面积扣多份配额。
func TestEnsureNovelAIV5QuotaScalesWithArea(t *testing.T) {
	// 给 3 张 1MP 图的量。
	server, _ := subscriptionServer(t, opusBody(novelAIQuotaPercentForUnits(3), false))

	// 1MP：1 份/张 → 请求 1 张（+1 保留 = 2 份）够。
	small := quotaTestChannel(t, server.URL, guardLock())
	if err := ensureNovelAIV5Quota(context.Background(), small, "nai-diffusion-5-full", 1024, 1024, 1); err != nil {
		t.Fatalf("1MP single image should fit, got: %v", err)
	}

	// 1.5MP：2 份/张 → 请求 1 张需 2 份 + 保留 2 份 = 4 份 > 3 份，应拒。
	large := quotaTestChannel(t, server.URL, guardLock())
	if err := ensureNovelAIV5Quota(context.Background(), large, "nai-diffusion-5-full", 1216, 1216, 1); err == nil {
		t.Fatal("1.5MP image consumes 2 quota units and must be blocked at this remaining level")
	}
}

func TestEnsureNovelAIV5QuotaBlocksNonOpusAccount(t *testing.T) {
	// tier=1（Tablet）：V5 出图必然扣 Anlas。
	server, _ := subscriptionServer(t, `{"tier":1,"active":true,"usage":{"percent":100,"isNegative":false}}`)
	channel := quotaTestChannel(t, server.URL, guardLock())

	err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 1)
	if err == nil {
		t.Fatal("non-Opus account must be blocked for V5")
	}
	if !strings.Contains(err.Error(), "Opus") {
		t.Fatalf("error should explain the Opus requirement, got: %v", err)
	}
}

// TestEnsureNovelAIV5QuotaHandlesMissingUsageField 覆盖第三方兼容站/老接口：
// 响应里压根没有 usage 字段时，绝不能当成「配额充足」放行。
func TestEnsureNovelAIV5QuotaHandlesMissingUsageField(t *testing.T) {
	server, _ := subscriptionServer(t, `{"tier":3,"active":true}`)

	blocked := quotaTestChannel(t, server.URL, guardLock())
	if err := ensureNovelAIV5Quota(context.Background(), blocked, "nai-diffusion-5-full", 1024, 1024, 1); err == nil {
		t.Fatal("missing usage field must be blocked under fail-closed")
	}

	failOpen := guardLock()
	failOpen.V5QuotaAllowOnLookupFailure = true
	allowed := quotaTestChannel(t, server.URL, failOpen)
	if err := ensureNovelAIV5Quota(context.Background(), allowed, "nai-diffusion-5-full", 1024, 1024, 1); err != nil {
		t.Fatalf("missing usage field must be allowed under fail-open, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 查询失败策略
// ---------------------------------------------------------------------------

func TestEnsureNovelAIV5QuotaFailClosedOnLookupError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid token"}`))
	}))
	t.Cleanup(server.Close)

	channel := quotaTestChannel(t, server.URL, guardLock())
	err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 1)
	if err == nil {
		t.Fatal("lookup failure must block under fail-closed")
	}
	// 必须说「无法确认」而不是「配额不足」：后者会让用户以为自己用完了去充值。
	if !strings.Contains(err.Error(), "无法确认") {
		t.Fatalf("error must say the quota could not be determined, got: %v", err)
	}
}

func TestEnsureNovelAIV5QuotaFailOpenOnLookupError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	lock := guardLock()
	lock.V5QuotaAllowOnLookupFailure = true
	channel := quotaTestChannel(t, server.URL, lock)

	if err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 1); err != nil {
		t.Fatalf("lookup failure must be allowed under fail-open, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 缓存：TTL、单飞、本地递减
// ---------------------------------------------------------------------------

// TestNovelAIQuotaCacheDeduplicatesConcurrentLookups 验证单飞。
//
// 没有单飞的话，N 个排队用户会同时打 /user/subscription，可能触发上游限流 ——
// 讽刺的是那会让守卫自己变成故障源。
func TestNovelAIQuotaCacheDeduplicatesConcurrentLookups(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// 拖一会儿，确保其他 goroutine 真的处在等待状态。
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(opusBody(80, false)))
	}))
	t.Cleanup(server.Close)

	channel := quotaTestChannel(t, server.URL, guardLock())

	const concurrency = 12
	var wg sync.WaitGroup
	errs := make([]error, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			errs[index] = ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 1)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d blocked unexpectedly: %v", i, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent lookups hit upstream %d times, want 1 (single-flight)", got)
	}
}

func TestNovelAIQuotaCacheReusesValueWithinTTL(t *testing.T) {
	server, calls := subscriptionServer(t, opusBody(90, false))
	channel := quotaTestChannel(t, server.URL, guardLock())

	for i := 0; i < 5; i++ {
		if err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 1); err != nil {
			t.Fatalf("call %d blocked unexpectedly: %v", i, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("5 sequential calls hit upstream %d times, want 1 (TTL cache)", got)
	}
}

func TestNovelAIQuotaCacheRefetchesAfterTTL(t *testing.T) {
	server, calls := subscriptionServer(t, opusBody(90, false))

	lock := guardLock()
	lock.V5QuotaCacheSeconds = 1 // 最小可配 TTL
	channel := quotaTestChannel(t, server.URL, lock)

	if err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 1); err != nil {
		t.Fatalf("first call blocked: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 1); err != nil {
		t.Fatalf("second call blocked: %v", err)
	}

	if got := calls.Load(); got != 2 {
		t.Fatalf("upstream hit %d times across TTL boundary, want 2", got)
	}
}

// TestConsumeNovelAIV5QuotaAffectsSubsequentChecks 验证本地乐观递减。
//
// 若不本地递减，同一个 TTL 窗口内所有排队请求都会读到同一个「配额充足」快照
// 而一起放行，保留位形同虚设。
func TestConsumeNovelAIV5QuotaAffectsSubsequentChecks(t *testing.T) {
	// 给「刚好超过 3 张」的量：够「2 张 + 1 保留」，出 2 张后只剩约 1 张的量，
	// 而「1 张 + 1 保留」需要 2 份 → 第二次必然被拒。
	// +epsilon 确保首次判定稳定通过，不卡在浮点相等的刀锋上。
	server, calls := subscriptionServer(t, opusBody(quotaFor(3)+quotaEpsilon, false))

	lock := guardLock()
	lock.V5QuotaCacheSeconds = 3600 // 锁住 TTL，确保变化只可能来自本地递减
	channel := quotaTestChannel(t, server.URL, lock)

	// 首次：请求 2 张（需 2 + 1 保留 = 3 份）够。
	if err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 2); err != nil {
		t.Fatalf("initial check should pass: %v", err)
	}

	// 出了 2 张，本地扣掉。
	consumeNovelAIV5Quota(channel, "nai-diffusion-5-full", 1024, 1024, 2)

	// 再来一张：本地预测只剩约 1 张的量，而 1 张 + 1 保留需要 2 份 → 应拒。
	if err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 1); err == nil {
		t.Fatal("after consuming quota the next request must be blocked by the reserve")
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream hit %d times, want 1 (local decrement must not refetch)", got)
	}
}

// TestConsumeNovelAIV5QuotaIgnoresNonV5 确保非 V5 出图不会误扣 V5 配额缓存。
func TestConsumeNovelAIV5QuotaIgnoresNonV5(t *testing.T) {
	perImage := novelAIQuotaPercentForUnits(1)
	server, _ := subscriptionServer(t, opusBody(perImage*3, false))

	lock := guardLock()
	lock.V5QuotaCacheSeconds = 3600
	channel := quotaTestChannel(t, server.URL, lock)

	if err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 2); err != nil {
		t.Fatalf("initial check should pass: %v", err)
	}

	// 大量非 V5 出图不应影响 V5 配额。
	for i := 0; i < 10; i++ {
		consumeNovelAIV5Quota(channel, "nai-diffusion-4-5-full", 1024, 1024, 1)
	}

	if err := ensureNovelAIV5Quota(context.Background(), channel, "nai-diffusion-5-full", 1024, 1024, 2); err != nil {
		t.Fatalf("non-V5 generations must not consume V5 quota, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 配置解析
// ---------------------------------------------------------------------------

func TestNovelAIQuotaSettingsFromDefaults(t *testing.T) {
	// nil → 全默认，守卫关闭。
	if settings := novelAIQuotaSettingsFrom(nil); settings.GuardEnabled ||
		settings.ReserveImages != defaultNovelAIV5QuotaReserveImages ||
		settings.CacheTTL != defaultNovelAIV5QuotaCacheSeconds*time.Second {
		t.Fatalf("nil lock produced unexpected settings: %+v", settings)
	}

	// 老配置（只有队列字段，V5 字段全为零值/nil）→ 守卫关闭 + 安全默认。
	legacy := novelAIQuotaSettingsFrom(&model.FreeGenerationLock{Enabled: true, MaxPixels: 1048576})
	if legacy.GuardEnabled {
		t.Fatal("legacy config must leave the guard disabled")
	}
	if legacy.ReserveImages != defaultNovelAIV5QuotaReserveImages {
		t.Fatalf("legacy ReserveImages = %d, want %d", legacy.ReserveImages, defaultNovelAIV5QuotaReserveImages)
	}
	if legacy.CacheTTL != defaultNovelAIV5QuotaCacheSeconds*time.Second {
		t.Fatalf("legacy CacheTTL = %v, want %v", legacy.CacheTTL, defaultNovelAIV5QuotaCacheSeconds*time.Second)
	}

	// ⚠️ 显式 0 必须被尊重，不能回落成默认值 1。
	// 后台 tooltip 明确承诺「填 0 表示允许用到见底」，若这里回落到 1 就是欺骗用户。
	// 这也是 V5QuotaReserveImages 必须用 *int 的原因：int 的零值区分不了
	// 「老配置未设置」与「管理员显式选 0」。
	explicitZero := novelAIQuotaSettingsFrom(&model.FreeGenerationLock{
		Enabled:              true,
		V5QuotaGuardEnabled:  true,
		V5QuotaReserveImages: quotaReserve(0),
	})
	if explicitZero.ReserveImages != 0 {
		t.Fatalf("explicit ReserveImages=0 must stay 0, got %d (regression: zero value treated as unset)", explicitZero.ReserveImages)
	}

	// 负数保留位纠正为 0；显式值原样生效。
	custom := novelAIQuotaSettingsFrom(&model.FreeGenerationLock{
		Enabled:                     true,
		V5QuotaGuardEnabled:         true,
		V5QuotaReserveImages:        quotaReserve(-5),
		V5QuotaAllowOnLookupFailure: true,
		V5QuotaCacheSeconds:         120,
	})
	if !custom.GuardEnabled || !custom.AllowOnLookupFailure {
		t.Fatalf("custom flags not applied: %+v", custom)
	}
	if custom.ReserveImages != 0 {
		t.Fatalf("negative ReserveImages = %d, want 0", custom.ReserveImages)
	}
	if custom.CacheTTL != 120*time.Second {
		t.Fatalf("custom CacheTTL = %v, want 120s", custom.CacheTTL)
	}
}

func TestNovelAIQuotaAffordableImages(t *testing.T) {
	perImage := novelAIQuotaPercentForUnits(1)
	if got := novelAIQuotaAffordableImages(perImage*5, 1); got != 5 {
		t.Fatalf("affordable = %d, want 5", got)
	}
	if got := novelAIQuotaAffordableImages(0, 1); got != 0 {
		t.Fatalf("affordable at 0%% = %d, want 0", got)
	}
	if got := novelAIQuotaAffordableImages(-3, 1); got != 0 {
		t.Fatalf("affordable at negative = %d, want 0", got)
	}
	// 2 份/张时同样的百分比只够一半张数。
	if got := novelAIQuotaAffordableImages(perImage*6, 2); got != 3 {
		t.Fatalf("affordable with 2 units/image = %d, want 3", got)
	}
}

func TestNovelAIQuotaRefillHintFormats(t *testing.T) {
	tests := []struct {
		seconds float64
		want    string
	}{
		{0, "自动回充"},
		{30, "秒"},
		{600, "分钟"},
		{7200, "小时"},
	}
	for _, tt := range tests {
		got := novelAIQuotaRefillHint(novelAIOpusUsage{TimeUntilNextPercent: tt.seconds})
		if !strings.Contains(got, tt.want) {
			t.Errorf("refill hint for %.0fs = %q, want it to contain %q", tt.seconds, got, tt.want)
		}
	}
}

// TestFetchNovelAIOpusUsageParsesOfficialShape 钉死字段名。
//
// 参考实现的 OpusUsageInfo 没有任何 JsonKey 重命名，所以 JSON 里的字段名就是
// percent / isNegative / timeUntilNextPercent。写错任何一个都会静默读成 0 值，
// 而 percent=0 在守卫看来就是「配额耗尽」，会把所有 V5 请求拦死。
func TestFetchNovelAIOpusUsageParsesOfficialShape(t *testing.T) {
	server, _ := subscriptionServer(t, `{
		"tier": 3,
		"active": true,
		"trainingStepsLeft": {"fixedTrainingStepsLeft": 10000, "purchasedTrainingSteps": 0},
		"usage": {"percent": 98.5, "isNegative": false, "timeUntilNextPercent": 4800}
	}`)
	channel := quotaTestChannel(t, server.URL, guardLock())

	usage, err := fetchNovelAIOpusUsage(context.Background(), channel)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if !usage.IsOpus {
		t.Fatal("tier=3 + active must be recognised as Opus")
	}
	if !usage.HasQuotaInfo {
		t.Fatal("usage field present but HasQuotaInfo = false")
	}
	if usage.Percent != 98.5 {
		t.Fatalf("Percent = %f, want 98.5", usage.Percent)
	}
	if usage.IsNegative {
		t.Fatal("IsNegative = true, want false")
	}
	if usage.TimeUntilNextPercent != 4800 {
		t.Fatalf("TimeUntilNextPercent = %f, want 4800", usage.TimeUntilNextPercent)
	}
}

// TestFetchNovelAIOpusUsageAcceptsOverflowAndNegative 覆盖 percent 的两个非直觉取值：
// 持续回充会让它超过 100（参考实现测试里出现过 192），透支时为负。
func TestFetchNovelAIOpusUsageAcceptsOverflowAndNegative(t *testing.T) {
	overflow, _ := subscriptionServer(t, opusBody(192, false))
	channelOverflow := quotaTestChannel(t, overflow.URL, guardLock())
	usage, err := fetchNovelAIOpusUsage(context.Background(), channelOverflow)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if usage.Percent != 192 {
		t.Fatalf("overflow Percent = %f, want 192 (must not be clamped)", usage.Percent)
	}

	negative, _ := subscriptionServer(t, opusBody(-2, true))
	channelNegative := quotaTestChannel(t, negative.URL, guardLock())
	usage, err = fetchNovelAIOpusUsage(context.Background(), channelNegative)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if usage.Percent != -2 || !usage.IsNegative {
		t.Fatalf("negative usage parsed as percent=%f isNegative=%v", usage.Percent, usage.IsNegative)
	}
}

// TestFetchNovelAIOpusUsageRespectsExpiry 覆盖订阅有效性判定：
// tier=3 但已过期 → 不是有效 Opus。
func TestFetchNovelAIOpusUsageRespectsExpiry(t *testing.T) {
	expired := time.Now().Add(-24 * time.Hour).Unix()
	server, _ := subscriptionServer(t, fmt.Sprintf(
		`{"tier":3,"active":true,"expiresAt":%d,"usage":{"percent":100,"isNegative":false}}`, expired,
	))
	channel := quotaTestChannel(t, server.URL, guardLock())

	usage, err := fetchNovelAIOpusUsage(context.Background(), channel)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if usage.IsOpus {
		t.Fatal("expired subscription must not count as Opus")
	}

	// 内部账号类型不受过期限制（参考实现 user_subscription.dart:61-64）。
	internal, _ := subscriptionServer(t, fmt.Sprintf(
		`{"tier":3,"active":false,"expiresAt":%d,"accountType":2,"usage":{"percent":100,"isNegative":false}}`, expired,
	))
	channelInternal := quotaTestChannel(t, internal.URL, guardLock())
	usage, err = fetchNovelAIOpusUsage(context.Background(), channelInternal)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if !usage.IsOpus {
		t.Fatal("privileged accountType must count as Opus regardless of expiry")
	}
}
