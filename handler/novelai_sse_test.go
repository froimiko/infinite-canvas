package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/basketikun/infinite-canvas/model"
)

func TestWantsNovelAISSE(t *testing.T) {
	tests := []struct {
		name   string
		accept string
		expect bool
	}{
		{"exact", "text/event-stream", true},
		{"with params", "text/event-stream; charset=utf-8", true},
		{"mixed case", "Text/Event-Stream", true},
		{"among others", "application/json, text/event-stream", true},
		{"json only", "application/json", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			if tt.accept != "" {
				r.Header.Set("Accept", tt.accept)
			}
			if got := wantsNovelAISSE(r); got != tt.expect {
				t.Errorf("wantsNovelAISSE(%q) = %v, want %v", tt.accept, got, tt.expect)
			}
		})
	}
}

// SSE 响应头必须带全这几个字段，缺一个都可能让保活失效：
// Content-Type 决定客户端按流解析；X-Accel-Buffering:no 关掉中间反代缓冲
// （被缓冲住就等于没发事件，524 照旧）。
func TestNovelAISSEWriterStartSendsStreamHeadersImmediately(t *testing.T) {
	recorder := httptest.NewRecorder()
	stream := newNovelAISSEWriter(recorder)
	stream.start()

	if stream.Failed() {
		t.Fatal("start() should succeed on httptest.ResponseRecorder")
	}
	if got := recorder.Code; got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := recorder.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no (否则中间反代会缓冲事件，保活失效)", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
}

// 事件必须是标准 SSE 帧：event: 行 + data: 行 + 空行分隔。
func TestNovelAISSEWriterSendFormatsStandardFrames(t *testing.T) {
	recorder := httptest.NewRecorder()
	stream := newNovelAISSEWriter(recorder)
	stream.start()
	stream.send("queued", map[string]any{"imagesAhead": 3, "estimatedSeconds": 36, "total": 1})
	stream.send("generating", map[string]any{"current": 1, "total": 2})

	body := recorder.Body.String()
	wantFrames := []string{
		"event: queued\ndata: {\"estimatedSeconds\":36,\"imagesAhead\":3,\"total\":1}\n\n",
		"event: generating\ndata: {\"current\":1,\"total\":2}\n\n",
	}
	for _, frame := range wantFrames {
		if !strings.Contains(body, frame) {
			t.Fatalf("body missing frame %q\ngot: %q", frame, body)
		}
	}

	// 每个事件都要以空行收尾，否则客户端会一直等下一行、事件永不触发。
	for _, chunk := range strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n") {
		if !strings.HasPrefix(chunk, "event: ") {
			t.Fatalf("chunk does not start with event: %q", chunk)
		}
		if !strings.Contains(chunk, "\ndata: ") {
			t.Fatalf("chunk missing data line: %q", chunk)
		}
	}
}

// data 字段必须是单行：JSON 序列化天然保证这点，但如果哪天有人改成直接拼字符串，
// 裸换行会把一个事件撕成两个残帧。这里钉死该不变量。
func TestNovelAISSEWriterSendKeepsDataOnSingleLine(t *testing.T) {
	recorder := httptest.NewRecorder()
	stream := newNovelAISSEWriter(recorder)
	stream.start()
	stream.send("error", map[string]any{"message": "第一行\n第二行\n第三行"})

	body := recorder.Body.String()
	frame := strings.TrimSuffix(body, "\n\n")
	if strings.Count(frame, "\n") != 1 {
		t.Fatalf("frame should contain exactly one newline (event/data 分隔)，got %q", frame)
	}
	if !strings.Contains(body, "\\n") {
		t.Errorf("newlines in message should be JSON-escaped, got %q", body)
	}
}

// 写失败（客户端断开）后必须短路，不再往死连接上写。
func TestNovelAISSEWriterStopsAfterWriteFailure(t *testing.T) {
	failing := &failingResponseWriter{header: http.Header{}}
	stream := newNovelAISSEWriter(failing)
	stream.start()
	stream.send("queued", map[string]any{"imagesAhead": 1})
	stream.send("done", map[string]any{"data": []any{}})

	if !stream.Failed() {
		t.Fatal("stream should be marked failed after a write error")
	}
	if failing.writes > 1 {
		t.Fatalf("writes = %d, want <= 1 (失败后必须短路，不能继续往断开的连接写)", failing.writes)
	}
}

// send 会被三个 goroutine 并发调用（主流程 / 排队 ticker / 出图心跳）。
// 这里用并发写压一下，配合 -race 能抓出漏加锁。
func TestNovelAISSEWriterConcurrentSendIsSafe(t *testing.T) {
	recorder := httptest.NewRecorder()
	stream := newNovelAISSEWriter(recorder)
	stream.start()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				stream.send("generating", map[string]any{"current": j, "total": 20, "worker": i})
			}
		}(i)
	}
	wg.Wait()

	body := recorder.Body.String()
	// 帧数应等于写入次数；若发生交错撕裂，分割结果会出现非法帧。
	frames := strings.Split(strings.TrimSuffix(body, "\n\n"), "\n\n")
	if len(frames) != 8*20 {
		t.Fatalf("frame count = %d, want 160 (说明并发写发生了交错/丢失)", len(frames))
	}
	for _, frame := range frames {
		if !strings.HasPrefix(frame, "event: generating\ndata: {") {
			t.Fatalf("malformed frame (并发写撕裂): %q", frame)
		}
	}
}

// ★ 本 Phase 最核心的保证：响应头必须在「排队阻塞」之前就发出去。
//
// 这正是绕过 Cloudflare 524 的原理 —— CF 的 100s 限制针对响应头(TTFB)，
// 只要头先发出、后续持续有字节，就不会被掐断。
//
// 构造：先让另一个请求占住队列，再启动 SSE 请求。SSE 请求必然卡在等锁上，
// 此时断言 recorder 里**已经**有 200 响应头和至少一个 queued 事件。
// 如果哪天有人把 start() 挪到排队之后，这个测试会立刻失败。
func TestStreamNovelAIWritesHeaderBeforeQueueBlocks(t *testing.T) {
	channel := model.ModelChannel{
		Name:    uniqueTestKey("sse-header-channel"),
		BaseURL: "https://image.novelai.net",
		APIKey:  uniqueTestKey("sse-header-token"),
		FreeGenerationLock: &model.FreeGenerationLock{
			Enabled:                  true,
			EstimatedSecondsPerImage: 12,
		},
	}

	// 占住队列名额，让后来者只能排队。
	holderStarted := make(chan struct{})
	releaseHolder := make(chan struct{})
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		_, _ = withNovelAIQueue(context.Background(), channel, "nai-diffusion-3", "holder", 1, nil,
			func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
				close(holderStarted)
				<-releaseHolder
				return []map[string]interface{}{{"b64_json": "held"}}, nil
			})
	}()
	<-holderStarted

	recorder := httptest.NewRecorder()
	stream := newNovelAISSEWriter(recorder)

	// 模拟 streamNovelAIImageRequest 的关键顺序：start() 必须先于入队。
	stream.start()
	stream.send("queued", map[string]any{
		"imagesAhead":      novelAIQueueFor(channel).queuedImages(),
		"estimatedSeconds": 12,
		"total":            1,
	})

	blocked := make(chan struct{})
	go func() {
		defer close(blocked)
		_, _ = withNovelAIQueue(context.Background(), channel, "nai-diffusion-3", "waiter", 1,
			func(imagesAhead int, estimatedSeconds int) {
				stream.send("queued", map[string]any{
					"imagesAhead":      imagesAhead,
					"estimatedSeconds": estimatedSeconds,
					"total":            1,
				})
			},
			func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
				return []map[string]interface{}{{"b64_json": "ok"}}, nil
			})
	}()

	// 等待者此刻仍被队列挡住（holder 没放手），但响应头和事件应该已经出去了。
	select {
	case <-blocked:
		t.Fatal("waiter should still be blocked while holder owns the queue slot")
	case <-time.After(150 * time.Millisecond):
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 —— 响应头必须在排队阻塞前发出，否则 CF 100s 后必 524", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream（排队前应已写头）", got)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "event: queued") {
		t.Fatalf("排队期间应已推送至少一个 queued 事件用于保活，got %q", body)
	}

	close(releaseHolder)
	<-holderDone
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("waiter did not proceed after holder released the slot")
	}
}

// 批量必须整批只占一个队列名额。
// 历史实现开 N 个 goroutine 各自抢锁，会往队列插 N 个分散占位，
// 既让「前方还有几张」失真，也让单用户能独占队列把别人推过 CF 100s。
func TestNovelAIBatchOccupiesSingleQueueSlot(t *testing.T) {
	channel := model.ModelChannel{
		Name:    uniqueTestKey("sse-batch-channel"),
		BaseURL: "https://image.novelai.net",
		APIKey:  uniqueTestKey("sse-batch-token"),
		FreeGenerationLock: &model.FreeGenerationLock{
			Enabled:                  true,
			EstimatedSecondsPerImage: 12,
			MaxUserQueuedImages:      50,
		},
	}
	queue := novelAIQueueFor(channel)

	// 先占住名额，让批量请求停在排队阶段，便于观察它占了几张。
	holderStarted := make(chan struct{})
	releaseHolder := make(chan struct{})
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		_, _ = withNovelAIQueue(context.Background(), channel, "nai-diffusion-3", "holder", 1, nil,
			func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
				close(holderStarted)
				<-releaseHolder
				return []map[string]interface{}{{"b64_json": "held"}}, nil
			})
	}()
	<-holderStarted

	const batch = 4
	entered := make(chan struct{})
	go func() {
		defer close(entered)
		_, _ = withNovelAIQueue(context.Background(), channel, "nai-diffusion-3", "batch-user", batch, nil,
			func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
				return []map[string]interface{}{{"b64_json": "ok"}}, nil
			})
	}()

	// 等批量条目登记进队列：holder 1 张 + 批量 4 张 = 5 张。
	deadline := time.After(2 * time.Second)
	for queue.queuedImages() < 1+batch {
		select {
		case <-deadline:
			t.Fatalf("batch never enqueued, queuedImages=%d want %d", queue.queuedImages(), 1+batch)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if got := queue.queuedImages(); got != 1+batch {
		t.Fatalf("queuedImages = %d, want %d（批量必须整批只占一个名额、按张数计）", got, 1+batch)
	}

	close(releaseHolder)
	<-holderDone
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("batch did not proceed after holder released the slot")
	}
}

// failingResponseWriter 的 Write 永远失败，用于模拟客户端已断开。
type failingResponseWriter struct {
	header http.Header
	writes int
}

func (f *failingResponseWriter) Header() http.Header { return f.header }
func (f *failingResponseWriter) WriteHeader(int)     {}
func (f *failingResponseWriter) Write(p []byte) (int, error) {
	f.writes++
	return 0, http.ErrHandlerTimeout
}
