package handler

// NovelAI 生图的 SSE（Server-Sent Events）流式保活。
//
// ── 为什么需要这个文件 ──────────────────────────────────────────────
//
// 云端部署在 Cloudflare 后面。CF 的 100 秒超时，**限制对象是「响应头(TTFB)」，
// 而不是整个请求耗时**：只要响应头已经发出、并且之后持续有字节流动，计时器就
// 转为「字节间隔」超时，请求可以跑很久也不会被掐断。
//
// 而 NovelAI Opus 免费生图不支持并发，同一渠道下所有用户必须串行排队。V4.5 单张
// 约 11-12 秒，排到第 9 位累计就超过 100 秒 —— 老的同步实现在排队+出图全程一个
// 字节都不返回，于是必然撞上 CF 的 TTFB 计时器，前端表现为 524。
//
// 解法就是本文件做的事：**在入队之前就把响应头发出去**，然后在排队/出图期间周期性
// 推送进度事件。这样排队再久也不会 524，而且这些事件正好用来告诉用户「前方还有
// N 张图正在生成」—— 保活和 UX 是同一件事。
//
// ── 事件契约（前端按此解析，改动需同步前端）────────────────────────
//
//	event: queued
//	data: {"imagesAhead":3,"estimatedSeconds":36,"total":1}
//
//	event: generating
//	data: {"current":1,"total":4}
//
//	event: done
//	data: {"data":[{"b64_json":"..."}]}
//
//	event: error
//	data: {"message":"NovelAI 鉴权失败..."}
//
// done 的 data 结构与非 SSE 响应体完全一致（都是 marshalOpenAIImageResponse 的
// 产出），因此前端可以复用同一套图片解析逻辑。
//
// ⚠️ 一旦响应头发出（200），就再也不能改 HTTP 状态码了。因此本文件里所有在
// 「响应头已发出」之后发生的错误，都只能通过 event: error 传递，绝不能试图
// 调用 Fail()/WriteHeader()（那会被 net/http 忽略并打 superfluous 日志）。

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/basketikun/infinite-canvas/model"
	"github.com/basketikun/infinite-canvas/service"
)

// novelAIGeneratingHeartbeat 是「出图阶段」的心跳间隔。
//
// 排队阶段的保活由 withNovelAIQueue 的 onQueueUpdate 兜底（每 2 秒一次），
// 但出图阶段没有那个回调：单张 V4.5 就要 11-12 秒，批量 10 张能到两分钟。
// 如果这段时间完全静默，CF 的字节间隔计时器同样会掐断连接 —— 524 就绕不掉了。
// 10 秒相对 CF 的百秒级阈值有约 10 倍余量。
const novelAIGeneratingHeartbeat = 10 * time.Second

// wantsNovelAISSE 判断客户端是否要 SSE。
// 前端不发这个头时走完全不变的老逻辑，因此本特性可以整体回退。
func wantsNovelAISSE(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

type streamNovelAIParams struct {
	OpenAIReq           openAIImageRequest
	SampleReq           *novelAIRequest
	Channel             model.ModelChannel
	User                model.AuthUser
	Credits             int
	TotalCredits        int
	RequestCount        int
	ForceSingleRequests bool
}

// novelAISSEWriter 封装 SSE 写入。
//
// 一旦某次写入失败（客户端断开），failed 置位，后续写入全部短路 —— 避免在
// 已经断开的连接上继续傻写，也让调用方能通过 Failed() 尽快收工、让出队列名额。
//
// ⚠️ 必须加锁：send 会被三个 goroutine 并发调用 ——
//  1. 主流程（初始 queued / done / error）
//  2. withNovelAIQueue 内部的排队 ticker（每 2 秒的 queued）
//  3. 本文件的出图心跳 ticker（generating）
//
// http.ResponseWriter 不是并发安全的，不加锁会写坏 SSE 帧（事件交错撕裂）并触发
// data race。这里的 mu 只保护「写一帧 + Flush」这段纯内存/IO 操作，不用于等待任何
// 长任务，因此与「排队互斥量不得用 Mutex」那条约束不冲突 —— 那条针对的是等上游
// 出图的可取消等待，必须用 cap=1 channel + ctx。
type novelAISSEWriter struct {
	mu         sync.Mutex
	w          http.ResponseWriter
	controller *http.ResponseController
	failed     bool
}

func newNovelAISSEWriter(w http.ResponseWriter) *novelAISSEWriter {
	return &novelAISSEWriter{w: w, controller: http.NewResponseController(w)}
}

// start 立即写出 SSE 响应头并 Flush。
//
// ⚠️ 必须在入队/出图**之前**调用 —— 这正是绕过 CF 524 的关键动作。
// 调用之后 HTTP 状态码就定死为 200，所有错误只能走 event: error。
func (s *novelAISSEWriter) start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	header := s.w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	// 关掉中间反代（nginx / Render 边缘等）的响应缓冲，否则事件会被攒着不发，
	// 保活就失效了 —— 那等于什么都没做。
	header.Set("X-Accel-Buffering", "no")
	s.w.WriteHeader(http.StatusOK)
	if err := s.controller.Flush(); err != nil {
		s.failed = true
	}
}

// send 写一个事件。payload 会被 JSON 序列化成单行，天然不含裸换行，
// 因此不需要按 SSE 规范对多行 data 做逐行 "data: " 前缀处理。
func (s *novelAISSEWriter) send(event string, payload any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.failed {
		return
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		log.Printf("NovelAI SSE marshal failed: event=%s err=%v", event, err)
		return
	}
	if _, err := s.w.Write([]byte("event: " + event + "\ndata: " + string(encoded) + "\n\n")); err != nil {
		s.failed = true
		return
	}
	if err := s.controller.Flush(); err != nil {
		s.failed = true
	}
}

// Failed 表示连接已断开（写失败）。调用方据此提前收工。
func (s *novelAISSEWriter) Failed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed
}

// streamNovelAIImageRequest 以 SSE 方式执行一次生图请求。
//
// 调用前提：参数校验、免费锁校验、请求转换、扣费都已在 proxyNovelAIImageRequest
// 中完成（那时还没写响应头，失败可以正常返回 HTTP 错误码）。
func streamNovelAIImageRequest(w http.ResponseWriter, r *http.Request, p streamNovelAIParams) {
	ctx := r.Context()
	stream := newNovelAISSEWriter(w)

	// ★ 关键顺序：先发响应头，再做任何可能长时间阻塞的事（排队 / 出图）。
	// 这一步之后 CF 的 TTFB 计时器就停了，排队再久也不会 524。
	stream.start()
	if stream.Failed() {
		// 响应头都写不出去，说明连接已经没了。此时还没打上游、没出图，
		// 已扣的算力点必须退回。
		refundNovelAICredits(p, p.TotalCredits, "sse start failed")
		return
	}

	limits := novelAIQueueLimitsFrom(p.Channel.FreeGenerationLock)

	// 入队前先汇报一次「正在排队」，让前端立刻有反馈，而不是干等到第一次
	// onQueueUpdate 回调。
	stream.send("queued", map[string]any{
		"imagesAhead":      novelAIQueueFor(p.Channel).queuedImages(),
		"estimatedSeconds": estimateNovelAISeconds(p.SampleReq.Model, p.RequestCount, limits.EstimatedSecondsPerImage),
		"total":            p.RequestCount,
	})

	onQueueUpdate := func(imagesAhead int, estimatedSeconds int) {
		stream.send("queued", map[string]any{
			"imagesAhead":      imagesAhead,
			"estimatedSeconds": estimatedSeconds,
			"total":            p.RequestCount,
		})
	}

	// 出图阶段的心跳：进入 fn 之后 onQueueUpdate 就不再触发了，必须自己维持字节流动。
	// generatingState 由 heartbeat goroutine 和主流程共享，用 channel 串行化避免竞态。
	progress := make(chan [2]int, p.RequestCount+1)
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan struct{})

	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(novelAIGeneratingHeartbeat)
		defer ticker.Stop()
		current, total := 0, p.RequestCount
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-ctx.Done():
				return
			case item := <-progress:
				current, total = item[0], item[1]
				stream.send("generating", map[string]any{"current": current, "total": total})
			case <-ticker.C:
				// 定期重发当前进度当心跳：既保活又刷新前端显示。
				stream.send("generating", map[string]any{"current": current, "total": total})
			}
		}
	}()

	onImageDone := func(current, total int) {
		select {
		case progress <- [2]int{current, total}:
		default:
		}
	}

	var data []map[string]interface{}
	var requestErr error
	succeededCount := p.RequestCount

	if p.ForceSingleRequests && p.RequestCount > 1 {
		data, succeededCount, requestErr = requestNovelAISingleImageBatch(
			ctx, p.OpenAIReq, p.RequestCount, p.Channel, p.User.ID, onQueueUpdate, onImageDone,
		)
	} else {
		enqueuedAt := time.Now()
		data, requestErr = withNovelAIQueue(ctx, p.Channel, p.SampleReq.Model, p.User.ID, 1, onQueueUpdate,
			func(entry *novelAIQueueEntry) ([]map[string]interface{}, error) {
				onImageDone(0, 1)
				images, upstream, err := doNovelAIUpstreamRequest(ctx, p.Channel, p.SampleReq)
				if err != nil {
					return nil, err
				}
				log.Printf(
					"NovelAI SSE request done: model=%s wait=%.1fs upstream=%.1fs images=%d",
					p.SampleReq.Model, time.Since(enqueuedAt).Seconds()-upstream.Seconds(), upstream.Seconds(), len(images),
				)
				// 出图成功才扣 V5 配额预测；非 V5 模型在函数内部短路，不受影响。
				consumeNovelAIV5Quota(p.Channel, p.SampleReq.Model, p.SampleReq.Parameters.Width, p.SampleReq.Parameters.Height, 1)
				return images, nil
			})
	}

	close(stopHeartbeat)
	<-heartbeatDone

	if requestErr != nil {
		refundNovelAICredits(p, p.TotalCredits, "request failed")
		stream.send("error", map[string]any{"message": requestErr.Error()})
		return
	}

	// 部分失败：只退没出成的那部分。
	if succeededCount < p.RequestCount {
		refundNovelAICredits(p, p.Credits*(p.RequestCount-succeededCount), "partial failure")
	}

	// ⚠️ 退款取舍：到这里图**已经真的生成出来了**，NovelAI 侧的 Anlas 也已经扣掉。
	// 如果此刻客户端已经断开、导致 done 事件写不出去，我们**不退款** —— 服务确实
	// 提供了，成本也确实付了，退款等于让用户白嫖一次断线。前端重试会重新计费，
	// 这与「失败/取消才退款」的整体口径一致。
	stream.send("done", map[string]any{"data": data})
	if stream.Failed() {
		log.Printf(
			"NovelAI SSE client disconnected before done: model=%s images=%d (credits intentionally not refunded)",
			p.SampleReq.Model, len(data),
		)
	}
}

func refundNovelAICredits(p streamNovelAIParams, amount int, reason string) {
	if amount <= 0 {
		return
	}
	if err := service.RefundUserCredits(p.User.ID, p.SampleReq.Model, amount, "/images/generations"); err != nil {
		log.Printf("NovelAI SSE refund failed: reason=%s amount=%d err=%v", reason, amount, err)
	}
}
