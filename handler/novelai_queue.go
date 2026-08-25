package handler

// NovelAI 排队队列内核（Phase 1）。
//
// 背景：NovelAI Opus 免费生图不支持并发，因此同一渠道（baseURL+Token）下所有
// 用户的生图请求必须严格串行。历史实现是一把「不可观测的裸 channel 锁」：
// 请求在等锁处静默阻塞，既不知道自己排在第几位，也不知道大概还要等多久。
// 由于整个 HTTP 请求是同步阻塞的（排队期间不返回任何字节），排到第 9 位时
// 累计等待就会超过 Cloudflare 的 100s 响应头超时，前端只能看到 524。
//
// 本文件把那把裸锁升级为「FIFO 票号队列」：每个请求先取票号并登记要生成的张数，
// 于是任何时刻都能算出「我前面还有多少张图要出」，再配合 per-model 的 EWMA
// 单张耗时统计，就能给出预估等待秒数。这两个数字是 Phase 2 做 SSE 进度推送的
// 唯一数据源，Phase 1 只建内核、不发 SSE。
//
// ⚠️ 并发模型的边界（历史踩坑，改动前务必读完）：
//
//  1. 「排队互斥量」绝对不能用 sync.Mutex。Mutex.Lock 无法被 ctx 打断，一旦反代
//     超时、客户端断开，请求会在等锁处持续堆积，锁越排越长，历史现象是
//     「之后每次都 502，只能删控件」。因此排队必须用 cap=1 的 channel +
//     select { case slot <- struct{}{}: case <-ctx.Done(): } 模式。
//
//  2. 但「用 sync.Mutex 保护队列元数据 map」是允许且必要的。那是纯内存的
//     微秒级临界区（改几个 int、增删一个 map 条目），不含任何 I/O、不等锁、
//     不阻塞在 channel 上，因此不存在无法取消的长等待。
//
//     一句话区分：等「上游出图」用可取消 channel；护「内存字段」用 mutex。
//     临界区内严禁出现 I/O、channel 发送/接收、或调用可能阻塞的函数。

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/basketikun/infinite-canvas/model"
)

// 队列相关配置的兜底默认值。老配置（JSON 里没有这几个字段，反序列化后为 0）
// 无需迁移，读取时自动回落到这里，因此不需要 DB migration。
const (
	defaultNovelAIEstimatedSecondsPerImage = 12  // V4.5 Full 832x1216/28步 实测约 11-12s
	defaultNovelAIMaxUserQueuedImages      = 20  // 防单人灌满队列
	defaultNovelAIMaxQueuedImages          = 500 // 绝对兜底，仅防内存失控，正常不触发
)

// novelAIQueueLimits 是从渠道配置解析出来的队列上限快照。
type novelAIQueueLimits struct {
	EstimatedSecondsPerImage int
	MaxUserQueuedImages      int
	MaxQueuedImages          int
}

// novelAIQueueLimitsFrom 从渠道配置读取队列参数，字段为 0 或负数时回落默认值。
// 这样历史配置（没有这三个字段）可以零迁移直接生效。
func novelAIQueueLimitsFrom(lock *model.FreeGenerationLock) novelAIQueueLimits {
	limits := novelAIQueueLimits{
		EstimatedSecondsPerImage: defaultNovelAIEstimatedSecondsPerImage,
		MaxUserQueuedImages:      defaultNovelAIMaxUserQueuedImages,
		MaxQueuedImages:          defaultNovelAIMaxQueuedImages,
	}
	if lock == nil {
		return limits
	}
	if lock.EstimatedSecondsPerImage > 0 {
		limits.EstimatedSecondsPerImage = lock.EstimatedSecondsPerImage
	}
	if lock.MaxUserQueuedImages > 0 {
		limits.MaxUserQueuedImages = lock.MaxUserQueuedImages
	}
	if lock.MaxQueuedImages > 0 {
		limits.MaxQueuedImages = lock.MaxQueuedImages
	}
	return limits
}

// novelAIQueueEntry 是队列中的一张「票」，代表一次进行中或等待中的生图请求。
type novelAIQueueEntry struct {
	ticket    int64  // 单调递增票号，决定 FIFO 顺序
	userID    string // 用于单用户配额统计；匿名/内部调用可为空
	remaining int    // 本请求还剩几张要生成（Phase 2 逐张回调时递减）
}

// Ticket 返回票号。仅用于日志/测试，队列顺序语义由内部维护。
func (e *novelAIQueueEntry) Ticket() int64 {
	if e == nil {
		return 0
	}
	return e.ticket
}

// novelAIQueue 是单个渠道（baseURL+Token）的串行队列。
type novelAIQueue struct {
	// slot 是可取消互斥量，cap 必须为 1。严禁改成 sync.Mutex：
	// Mutex.Lock 不响应 ctx，客户端断开后请求会在此永久堆积（历史 502 雪崩）。
	slot chan struct{}

	// mu 只保护下面的元数据字段，临界区必须极短（微秒级、纯内存）。
	// 严禁在持有 mu 期间做 I/O、收发 channel 或等待 slot。
	mu         sync.Mutex
	nextTicket int64
	entries    map[int64]*novelAIQueueEntry
}

func newNovelAIQueue() *novelAIQueue {
	return &novelAIQueue{
		slot:    make(chan struct{}, 1),
		entries: make(map[int64]*novelAIQueueEntry),
	}
}

// novelAIQueues 全局注册表：map[string]*novelAIQueue。
// key 沿用 novelAIFreeGenerationLockKey，即 sha256(baseURL + "\x00" + APIKey)，
// 既能按渠道+Token 隔离串行域，又不会把明文 Token 落到内存 key / 日志里。
var novelAIQueues sync.Map

func novelAIQueueFor(channel model.ModelChannel) *novelAIQueue {
	key := novelAIFreeGenerationLockKey(channel)
	if value, ok := novelAIQueues.Load(key); ok {
		return value.(*novelAIQueue)
	}
	// LoadOrStore 保证并发下同一 key 只有一个队列实例生效。
	value, _ := novelAIQueues.LoadOrStore(key, newNovelAIQueue())
	return value.(*novelAIQueue)
}

// enqueue 取票号并登记条目。取号前检查单用户与全队列两个张数上限。
// 返回的 entry 必须在请求结束时通过 dequeue 注销，否则前方张数会永久虚高。
func (q *novelAIQueue) enqueue(userID string, images int, limits novelAIQueueLimits) (*novelAIQueueEntry, error) {
	if images <= 0 {
		images = 1
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	totalRemaining := 0
	userRemaining := 0
	for _, entry := range q.entries {
		totalRemaining += entry.remaining
		if userID != "" && entry.userID == userID {
			userRemaining += entry.remaining
		}
	}

	// 单用户上限：防一个人用批量把队列灌满，让其他人无限期等待。
	if userID != "" && limits.MaxUserQueuedImages > 0 && userRemaining+images > limits.MaxUserQueuedImages {
		return nil, fmt.Errorf(
			"你在该渠道的排队张数已达上限（%d 张）\n"+
				"当前你已排队 %d 张，本次又请求 %d 张。\n\n"+
				"NovelAI 免费生图不支持并发，所有请求必须排队串行出图。\n"+
				"建议：等已排队的图出完后再提交，或减少本次生成张数",
			limits.MaxUserQueuedImages, userRemaining, images,
		)
	}

	// 全队列上限：绝对兜底，只为防止内存无限增长，正常流量不会触发。
	if limits.MaxQueuedImages > 0 && totalRemaining+images > limits.MaxQueuedImages {
		return nil, fmt.Errorf(
			"该渠道排队队列已满（当前 %d 张，上限 %d 张）\n\n"+
				"NovelAI 免费生图不支持并发，队列过长时请稍后再试",
			totalRemaining, limits.MaxQueuedImages,
		)
	}

	q.nextTicket++
	entry := &novelAIQueueEntry{
		ticket:    q.nextTicket,
		userID:    userID,
		remaining: images,
	}
	q.entries[entry.ticket] = entry
	return entry, nil
}

// imagesAhead 返回「排在该票号之前、还需要生成的图片张数」。
// 这是给用户展示的核心数字：比「前面还有几个请求」更准，因为一个请求可能要出多张。
func (q *novelAIQueue) imagesAhead(ticket int64) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	ahead := 0
	for _, entry := range q.entries {
		if entry.ticket < ticket {
			ahead += entry.remaining
		}
	}
	return ahead
}

// dequeue 注销条目。必须在 defer 中调用：ctx 取消路径也要走到，
// 否则条目残留会让后续请求看到永远不减的前方张数。
func (q *novelAIQueue) dequeue(ticket int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.entries, ticket)
}

// markProgress 更新剩余张数，供 Phase 2 批量逐张回调时递减。
func (q *novelAIQueue) markProgress(ticket int64, remaining int) {
	if remaining < 0 {
		remaining = 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if entry, ok := q.entries[ticket]; ok {
		entry.remaining = remaining
	}
}

// queuedImages 返回全队列待生成张数，用于日志与容量观测。
func (q *novelAIQueue) queuedImages() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	total := 0
	for _, entry := range q.entries {
		total += entry.remaining
	}
	return total
}

// acquire 获取串行执行权，等待期间响应 ctx 取消。
// 这里刻意不用 sync.Mutex —— 见文件头「并发模型的边界」第 1 条。
func (q *novelAIQueue) acquire(ctx context.Context) error {
	select {
	case q.slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return errNovelAIRequestCanceled
	}
}

// release 归还串行执行权。只能由成功 acquire 的一方调用。
func (q *novelAIQueue) release() {
	select {
	case <-q.slot:
	default:
		// 没持有却调用 release 属于编程错误；这里不 panic，避免把整个进程带崩，
		// 但也绝不能阻塞（否则会连带卡死整条请求链）。
	}
}

// errNovelAIRequestCanceled 统一取消错误，文案与历史实现保持一致，
// 避免前端已有的错误匹配逻辑失效。
var errNovelAIRequestCanceled = &novelAICanceledError{}

type novelAICanceledError struct{}

func (*novelAICanceledError) Error() string { return "请求已取消" }

// ---------------------------------------------------------------------------
// EWMA 单张耗时统计（per-model 滑动平均，用于预估等待时间）
// ---------------------------------------------------------------------------

// novelAIDurationSamples 存 map[string]float64：model -> 单张平均秒数。
// 值用 mutex 保护的小结构体承载，写入频率极低（每次成功出图一次），无需 atomic。
var novelAIDurationSamples sync.Map

type novelAIDurationStat struct {
	mu      sync.Mutex
	avg     float64
	samples int
}

// novelAIDurationEWMAAlpha 新样本权重。0.3 意味着约 3-4 次请求就能跟上模型切换
// 带来的耗时变化（V3 约 2s ↔ V4.5 约 12s），同时仍能抹平单次抖动。
const novelAIDurationEWMAAlpha = 0.3

func novelAIDurationStatFor(model string) *novelAIDurationStat {
	if value, ok := novelAIDurationSamples.Load(model); ok {
		return value.(*novelAIDurationStat)
	}
	value, _ := novelAIDurationSamples.LoadOrStore(model, &novelAIDurationStat{})
	return value.(*novelAIDurationStat)
}

// recordNovelAIDuration 记录一次「单张」实际耗时样本。
//
// ⚠️ 只在成功出图时调用。失败/取消的耗时毫无参考价值：
// 客户端断开可能只用了 0.2s，上游 500 也可能秒回，混进平均值会让预估严重偏低，
// 用户看到「预计还要 3 秒」却等了半分钟，比不显示更糟。
func recordNovelAIDuration(model string, d time.Duration) {
	if model == "" || d <= 0 {
		return
	}
	sample := d.Seconds()
	if math.IsNaN(sample) || math.IsInf(sample, 0) {
		return
	}

	stat := novelAIDurationStatFor(model)
	stat.mu.Lock()
	defer stat.mu.Unlock()
	if stat.samples == 0 {
		// 首个样本直接赋值：若从 0 开始做 EWMA，前几次预估会被 0 严重拉低。
		stat.avg = sample
	} else {
		stat.avg = stat.avg*(1-novelAIDurationEWMAAlpha) + sample*novelAIDurationEWMAAlpha
	}
	stat.samples++
}

// novelAIAverageSeconds 返回该模型的单张平均秒数；无样本时返回 (0, false)。
func novelAIAverageSeconds(model string) (float64, bool) {
	value, ok := novelAIDurationSamples.Load(model)
	if !ok {
		return 0, false
	}
	stat := value.(*novelAIDurationStat)
	stat.mu.Lock()
	defer stat.mu.Unlock()
	if stat.samples == 0 || stat.avg <= 0 {
		return 0, false
	}
	return stat.avg, true
}

// estimateNovelAISeconds 预估生成 images 张所需秒数。
// 无历史样本时用 fallback（渠道配置的冷启动值）兜底，保证首个用户也能看到数字。
func estimateNovelAISeconds(model string, images int, fallback int) int {
	if images <= 0 {
		return 0
	}
	if fallback <= 0 {
		fallback = defaultNovelAIEstimatedSecondsPerImage
	}

	perImage := float64(fallback)
	if avg, ok := novelAIAverageSeconds(model); ok {
		perImage = avg
	}

	estimated := int(math.Round(perImage * float64(images)))
	if estimated < 1 {
		// 已经确定要出图了，返回 0 秒会让前端显示「即将完成」然后长时间不动。
		estimated = 1
	}
	return estimated
}
