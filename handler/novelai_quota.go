package handler

// NovelAI V5「充能条」配额守卫。
//
// ── 背景 ────────────────────────────────────────────────────────────
//
// NovelAI 改了 Opus 会员的免费出图政策，但**只改了 V5 两个模型**：
//
//	nai-diffusion-5-full / nai-diffusion-5-curated
//	  → Opus 免费额度从「无限小图」变成随时间回充的配额池（充能条），
//	    配额透支后按正常价扣 Anlas。
//
//	nai-diffusion-4-5-* / nai-diffusion-4-* / nai-diffusion-3 / furry
//	  → 仍然是无限免费小图，**完全不受配额限制**。
//
// 这个结论直接来自参考实现 Aaalice_NAI_Launcher：只有 v5Curated / v5Full 两条
// 能力记录带 hasOpusUsageLimit: true（lib/core/constants/model_capabilities.dart:274,294）。
//
// 因此本文件最重要的一条不变量是：
//
//	★ 非 V5 模型必须一次上游订阅查询都不发。★
//
// 否则就是给「本来无限免费」的 V4.5/V4/V3 平白加一个延迟点和失败点，纯倒退。
// novelai_quota_test.go 里有专门的回归测试钉死这条。
//
// ── 为什么要「保留最后一张」────────────────────────────────────────
//
// 上游的 usage.isNegative 是**事后**信号：它变 true 的那一刻，把配额推成负数的
// 那张图已经出了、Anlas 已经扣了。所以只看这个布尔值，天然会晚一张。
//
// 于是这里在「够不够本次」之外再要求留出 ReserveImages 张的余量：
//
//	剩余配额 >= 本次所需 + 保留所需
//
// 这样耗尽前的最后一张永远花不掉，「这一张恰好跨过临界点」的窗口就不存在了。
//
// ── 并发模型的边界（与 novelai_queue.go 同一套纪律）──────────────────
//
// 本文件的缓存只用 sync.Mutex 保护**内存字段**（几个 float64/time.Time），
// 临界区是微秒级的纯内存操作。
//
// ⚠️ 严禁在持有 mu 期间发 HTTP 请求。拉取上游一律在锁外执行，多个并发请求
// 通过 inflight channel 做单飞协调。把 I/O 挪进临界区会复刻历史上那个
// 「锁内阻塞 → 请求堆积 → 之后每次都 502」的事故。
//
// 一句话：等 I/O 用 channel，护内存字段用 mutex。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/basketikun/infinite-canvas/model"
)

// 配额守卫的兜底默认值。老配置（JSON 里没有这些字段，反序列化后为 0/false）
// 读取时自动回落到这里，因此不需要 DB migration。
const (
	// defaultNovelAIV5QuotaReserveImages 默认保留 1 张。见文件头「为什么要保留最后一张」。
	defaultNovelAIV5QuotaReserveImages = 1
	// defaultNovelAIV5QuotaCacheSeconds 对齐参考实现的订阅刷新周期（30s）。
	defaultNovelAIV5QuotaCacheSeconds = 30
)

// novelAIQuotaImagesPerPercent 是「1% 配额折合多少张图」。
//
// 取自参考实现 opus_usage_chip.dart:25 的 _imagesPerPercent = 17.3，
// 口径是「1MP 以内、每张消耗 1 份配额」。反过来说单张 1MP 图约消耗
// 1/17.3 ≈ 0.0578% 配额。
//
// ⚠️ 这是官方网页端的估算系数，不是精确公式。改动前先核对参考实现，
// 不要凭直觉调（调大会让守卫过于宽松，等于白做）。
const novelAIQuotaImagesPerPercent = 17.3

// novelAIQuotaUnitTiers 是「按输出面积分档，一张图消耗几份配额」。
//
// 同样取自 opus_usage_chip.dart:28-33：大图一张要扣多份配额。
// 判定必须从小到大依次比较，命中即返回。
var novelAIQuotaUnitTiers = []struct {
	MaxPixels int
	Units     int
}{
	{1048576, 1}, // 1024×1024
	{1747627, 2},
	{2446678, 3},
	{3145728, 4},
}

const (
	// novelAIQuotaLookupTimeout 是单次 /user/subscription 查询的超时。
	//
	// 刻意留得比「单张出图预算」短得多：这只是一次轻量 GET，慢到 10s
	// 基本等同于上游不可用，没必要让用户干等。绝不能用 http.DefaultClient
	// （零超时）—— 上游卡住时会把 goroutine 永久挂在这里。
	novelAIQuotaLookupTimeout = 10 * time.Second

	// novelAIQuotaErrorCacheSeconds 是「查询失败」的缓存时长。
	//
	// 故意远短于成功值的 TTL：fail-closed 模式下把一次网络抖动缓存满 30s，
	// 会让 V5 白白不可用半分钟。5s 既能挡住失败风暴，又能快速恢复。
	novelAIQuotaErrorCacheSeconds = 5

	// novelAIQuotaSettlementDelay 是「上游余额结算延迟」的保护窗口。
	//
	// 刚出完图时上游余额可能还没落库，此时拉到的 percent 会偏高（把刚花掉的
	// 配额又放出来了），从而绕过保留位。因此在本地递减后的这段时间内，
	// 拉取值与本地预测值取更保守的那个。
	//
	// 参考实现的做法是扣费后延迟 500ms 再刷新（subscription_provider.dart:39），
	// 这里留 2s 是更宽的余量。
	novelAIQuotaSettlementDelay = 2 * time.Second
)

// novelAIQuotaHTTPClient 是配额查询专用客户端。
// 与出图用的 novelAIHTTPClient 分开：两者超时预算差一个数量级，
// 混用会让「查配额」继承 120s 的出图超时。
var novelAIQuotaHTTPClient = &http.Client{Timeout: novelAIQuotaLookupTimeout}

// novelAIQuotaSettings 是从渠道配置解析出来的配额守卫参数快照。
type novelAIQuotaSettings struct {
	GuardEnabled         bool
	ReserveImages        int
	AllowOnLookupFailure bool
	CacheTTL             time.Duration
}

// novelAIQuotaSettingsFrom 从渠道配置读取守卫参数，字段为 0/负数时回落默认值。
// 这样历史配置（没有这几个字段）可以零迁移直接生效。
func novelAIQuotaSettingsFrom(lock *model.FreeGenerationLock) novelAIQuotaSettings {
	settings := novelAIQuotaSettings{
		ReserveImages: defaultNovelAIV5QuotaReserveImages,
		CacheTTL:      time.Duration(defaultNovelAIV5QuotaCacheSeconds) * time.Second,
	}
	if lock == nil {
		return settings
	}

	settings.GuardEnabled = lock.V5QuotaGuardEnabled
	settings.AllowOnLookupFailure = lock.V5QuotaAllowOnLookupFailure
	// nil 表示「老配置/未设置」→ 保留默认值 1。
	// 显式 0 必须被尊重（管理员选择用到见底），因此这里只纠负数，不把 0 当缺省。
	if lock.V5QuotaReserveImages != nil {
		settings.ReserveImages = *lock.V5QuotaReserveImages
		if settings.ReserveImages < 0 {
			settings.ReserveImages = 0
		}
	}
	if lock.V5QuotaCacheSeconds > 0 {
		settings.CacheTTL = time.Duration(lock.V5QuotaCacheSeconds) * time.Second
	}
	return settings
}

// novelAIModelHasOpusUsageLimit 判断模型的 Opus 免费额度是否受配额池限制。
//
// ⚠️ 只有 V5 两个正式模型 ID 返回 true。传入的必须是**已解析的标准模型 ID**
// （resolveNovelAIModel 的输出，如 nai-diffusion-5-full），不是前端传来的原始
// 模型名或渠道显示名。
//
// 用精确等值而不是 strings.Contains("diffusion-5")：后者会把
// "nai-diffusion-4-5-full"（V4.5，仍然无限免费）误判成受限模型 —— 那会导致
// V4.5 被错误拦截，而 V4.5 是最常用的模型之一。
func novelAIModelHasOpusUsageLimit(naiModel string) bool {
	switch naiModel {
	case "nai-diffusion-5-full", "nai-diffusion-5-curated":
		return true
	default:
		return false
	}
}

// novelAIQuotaUnitsForPixels 返回「这个面积的一张图消耗几份配额」。
// 超过最大分档时按最大分档计（保守：宁可多算也不少算）。
func novelAIQuotaUnitsForPixels(pixels int) int {
	if pixels <= 0 {
		return 1
	}
	for _, tier := range novelAIQuotaUnitTiers {
		if pixels <= tier.MaxPixels {
			return tier.Units
		}
	}
	return novelAIQuotaUnitTiers[len(novelAIQuotaUnitTiers)-1].Units
}

// novelAIQuotaPercentForUnits 把「配额份数」换算成「配额百分比」。
func novelAIQuotaPercentForUnits(units int) float64 {
	if units <= 0 {
		return 0
	}
	return float64(units) / novelAIQuotaImagesPerPercent
}

// novelAIOpusUsage 是一次 /user/subscription 查询的结果快照。
type novelAIOpusUsage struct {
	// IsOpus 表示该 Token 拥有**有效的** Opus 订阅权益。
	IsOpus bool
	// HasQuotaInfo 表示响应里确实带了 usage 字段。
	//
	// 必须与「Percent == 0」区分开：第三方兼容站或老版本接口可能压根不返回
	// usage，此时我们无从判断配额，只能按配置的失败策略处理，绝不能当成
	// 「配额充足」放行。
	HasQuotaInfo bool
	// Percent 是剩余配额百分比，0~100 标度。
	//
	// ⚠️ 可以 >100（持续回充会溢出，参考实现测试里出现过 192），
	// 透支时为负值。因此不要假设它落在 [0,100] 区间。
	Percent float64
	// IsNegative 是上游给的「配额已透支」标志。见文件头：这是事后信号。
	IsNegative bool
	// TimeUntilNextPercent 是距下一个 1% 回充的秒数，用于给用户一个等待提示。
	TimeUntilNextPercent float64
}

// novelAISubscriptionResponse 只声明配额判定用得上的字段。
// 其余字段（trainingStepsLeft / perks 等）与本特性无关，不解析。
type novelAISubscriptionResponse struct {
	Tier        int    `json:"tier"`
	Active      bool   `json:"active"`
	ExpiresAt   *int64 `json:"expiresAt"`
	AccountType *int   `json:"accountType"`
	Usage       *struct {
		Percent              float64 `json:"percent"`
		IsNegative           bool    `json:"isNegative"`
		TimeUntilNextPercent float64 `json:"timeUntilNextPercent"`
	} `json:"usage"`
}

// hasActiveSubscription 复刻参考实现 user_subscription.dart:60-72 的判定：
// 内部账号类型不受过期时间限制；有 expiresAt 时以它为准；老响应回落到 active。
func (r novelAISubscriptionResponse) hasActiveSubscription() bool {
	if r.AccountType != nil {
		switch *r.AccountType {
		case 1, 2, 3, 4:
			return true
		}
	}
	if r.ExpiresAt != nil {
		return r.Tier > 0 && *r.ExpiresAt > time.Now().Unix()
	}
	return r.Tier > 0 && r.Active
}

// fetchNovelAIOpusUsage 查一次上游订阅信息。**不带缓存、不做单飞**，
// 调用方应通过 novelAIQuotaCache 访问。
//
// 端点选择：官方把 /user/subscription 挂在 image API 主机上
// （参考实现 nai_api_endpoint.dart:60-67），而渠道配置里的 BaseURL 默认
// 就是 https://image.novelai.net，所以直接复用 buildNovelAIURL 即可。
func fetchNovelAIOpusUsage(ctx context.Context, channel model.ModelChannel) (novelAIOpusUsage, error) {
	url := buildNovelAIURL(channel.BaseURL, "/user/subscription")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return novelAIOpusUsage{}, errors.New("创建 NovelAI 订阅查询请求失败")
	}
	request.Header.Set("Authorization", "Bearer "+channel.APIKey)

	response, err := novelAIQuotaHTTPClient.Do(request)
	if err != nil {
		return novelAIOpusUsage{}, fmt.Errorf("查询 NovelAI 订阅信息失败: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return novelAIOpusUsage{}, errors.New(readNovelAIError(response.StatusCode, body))
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return novelAIOpusUsage{}, errors.New("读取 NovelAI 订阅响应失败")
	}

	var parsed novelAISubscriptionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return novelAIOpusUsage{}, errors.New("解析 NovelAI 订阅响应失败")
	}

	usage := novelAIOpusUsage{
		IsOpus: parsed.Tier == 3 && parsed.hasActiveSubscription(),
	}
	if parsed.Usage != nil {
		usage.HasQuotaInfo = true
		usage.Percent = parsed.Usage.Percent
		usage.IsNegative = parsed.Usage.IsNegative
		usage.TimeUntilNextPercent = parsed.Usage.TimeUntilNextPercent
	}
	return usage, nil
}

// ---------------------------------------------------------------------------
// 渠道级配额缓存（TTL + 单飞 + 本地乐观递减）
// ---------------------------------------------------------------------------

// novelAIQuotaCacheEntry 是单个渠道（baseURL+Token）的配额缓存。
type novelAIQuotaCacheEntry struct {
	// mu 只保护下面的内存字段，临界区必须极短。
	// ⚠️ 严禁在持有 mu 期间发 HTTP 请求 —— 见文件头「并发模型的边界」。
	mu sync.Mutex

	usage     novelAIOpusUsage
	hasValue  bool
	lookupErr error
	fetchedAt time.Time

	// localConsumedAt 是最近一次本地递减的时间，用于结算延迟保护。
	localConsumedAt time.Time

	// inflight 非 nil 表示已经有人在拉取；其余请求等它关闭后复读缓存，
	// 避免 N 个排队用户同时打 /user/subscription 触发上游限流。
	inflight chan struct{}
}

// novelAIQuotaCaches 全局注册表：map[string]*novelAIQuotaCacheEntry。
// key 沿用 novelAIFreeGenerationLockKey，即 sha256(baseURL + "\x00" + APIKey)：
// 既按渠道+Token 隔离配额（不同 Token 是不同账号的充能条），
// 又不会把明文 Token 落到内存 key / 日志里。
var novelAIQuotaCaches sync.Map

func novelAIQuotaCacheFor(channel model.ModelChannel) *novelAIQuotaCacheEntry {
	key := novelAIFreeGenerationLockKey(channel)
	if value, ok := novelAIQuotaCaches.Load(key); ok {
		return value.(*novelAIQuotaCacheEntry)
	}
	value, _ := novelAIQuotaCaches.LoadOrStore(key, &novelAIQuotaCacheEntry{})
	return value.(*novelAIQuotaCacheEntry)
}

// resetNovelAIQuotaCacheForTest 清掉某渠道的配额缓存。
//
// 仅供测试使用：novelAIQuotaCaches 是包级 sync.Map，-count=N 重复运行时会跨轮
// 串味（上一轮的配额值被下一轮读到，导致测试碰巧通过）。新增触碰该 map 的测试
// 必须用唯一的渠道 key，并在开头调用本函数。
func resetNovelAIQuotaCacheForTest(channel model.ModelChannel) {
	novelAIQuotaCaches.Delete(novelAIFreeGenerationLockKey(channel))
}

// isFresh 判断缓存是否仍然有效。失败结果用更短的 TTL，便于快速恢复。
// 调用方必须已持有 mu。
func (e *novelAIQuotaCacheEntry) isFresh(ttl time.Duration) bool {
	if e.fetchedAt.IsZero() {
		return false
	}
	if e.lookupErr != nil {
		ttl = novelAIQuotaErrorCacheSeconds * time.Second
	}
	return time.Since(e.fetchedAt) < ttl
}

// get 返回配额快照，必要时拉取上游。并发调用只会触发一次拉取。
func (e *novelAIQuotaCacheEntry) get(ctx context.Context, channel model.ModelChannel, ttl time.Duration) (novelAIOpusUsage, error) {
	for {
		e.mu.Lock()
		if e.isFresh(ttl) {
			usage, err := e.usage, e.lookupErr
			e.mu.Unlock()
			return usage, err
		}
		if wait := e.inflight; wait != nil {
			// 已有人在拉，等它完成后回到循环复读缓存。
			e.mu.Unlock()
			select {
			case <-wait:
				continue
			case <-ctx.Done():
				return novelAIOpusUsage{}, ctx.Err()
			}
		}
		// 由本 goroutine 负责拉取。
		done := make(chan struct{})
		e.inflight = done
		e.mu.Unlock()

		// ★ I/O 在锁外执行。
		//
		// 这里刻意用 WithoutCancel 剥掉调用方的 ctx 取消信号：单飞意味着这次
		// 拉取的结果要给所有等待者用，若被某一个先断开的客户端带走，其余等待者
		// 会一起拿到「取消」错误 —— 在 fail-closed 模式下这就是无谓的拦截。
		// 超时仍由 novelAIQuotaLookupTimeout 保证，不会永久挂住。
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), novelAIQuotaLookupTimeout)
		usage, err := fetchNovelAIOpusUsage(fetchCtx, channel)
		cancel()

		e.mu.Lock()
		if err == nil {
			// 结算延迟保护：刚本地递减过的话，上游可能还没落库，
			// 取更保守的那个值，避免刚花掉的配额被重新放出来。
			if e.hasValue && !e.localConsumedAt.IsZero() && time.Since(e.localConsumedAt) < novelAIQuotaSettlementDelay {
				if e.usage.Percent < usage.Percent {
					usage.Percent = e.usage.Percent
				}
				usage.IsNegative = usage.IsNegative || e.usage.IsNegative
			}
			e.usage = usage
			e.hasValue = true
			e.lookupErr = nil
		} else {
			e.lookupErr = err
		}
		e.fetchedAt = time.Now()
		e.inflight = nil
		e.mu.Unlock()

		close(done)
		return usage, err
	}
}

// consume 在本地乐观扣掉已消耗的配额。
//
// 为什么必需：TTL 窗口内所有请求读到的是同一个快照。若不本地递减，10 个排队
// 用户会在同一个 30s 窗口里全部读到「配额充足」而一起放行，保留位形同虚设。
func (e *novelAIQuotaCacheEntry) consume(percent float64) {
	if percent <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.hasValue {
		// 没有基准值时无从递减；下次 get 会拉到真实值。
		return
	}
	e.usage.Percent -= percent
	if e.usage.Percent <= 0 {
		// 本地预测已见底，按透支处理（保守）。TTL 到期后会被真实值纠正。
		e.usage.IsNegative = true
	}
	e.localConsumedAt = time.Now()
}

// ---------------------------------------------------------------------------
// 守卫入口
// ---------------------------------------------------------------------------

// ensureNovelAIV5Quota 在出图前确认 V5 充能条余量是否足够。
//
// 返回 nil 表示可以出图；返回错误表示应当拦截，错误文案面向终端用户。
//
// ⚠️ 调用位置有讲究：必须在「写响应头之前」调用（SSE 一旦发出 200 就没法再改
// 状态码），也应在扣算力点之前 —— 拦下来就不该扣费。
//
// 短路顺序刻意如此排列，保证非 V5 模型一次上游请求都不发：
//  1. 免费生图锁未启用 → 付费并发模式，本守卫不介入
//  2. 守卫未勾选 → 直接放行
//  3. 模型不受配额限制（V4.5/V4/V3/furry）→ 直接放行
//  4. 到这里才会碰网络
func ensureNovelAIV5Quota(ctx context.Context, channel model.ModelChannel, naiModel string, width, height, images int) error {
	lock := channel.FreeGenerationLock
	if lock == nil || !lock.Enabled {
		return nil
	}
	settings := novelAIQuotaSettingsFrom(lock)
	if !settings.GuardEnabled {
		return nil
	}
	if !novelAIModelHasOpusUsageLimit(naiModel) {
		// ★ 关键不变量：V4.5 / V4 / V3 / furry 仍是无限免费小图，
		// 这里必须原样放行，不产生任何上游调用。
		return nil
	}
	if images < 1 {
		images = 1
	}

	usage, err := novelAIQuotaCacheFor(channel).get(ctx, channel, settings.CacheTTL)
	if err != nil {
		if settings.AllowOnLookupFailure {
			log.Printf("NovelAI V5 quota lookup failed, allowing by config: model=%s err=%v", naiModel, err)
			return nil
		}
		log.Printf("NovelAI V5 quota lookup failed, blocking: model=%s err=%v", naiModel, err)
		// 文案必须说清是「无法确认」而不是「配额不足」，否则用户会以为自己
		// 真的用完了，跑去充值。
		return fmt.Errorf(
			"无法确认 NovelAI V5 免费配额余量，已暂停出图以避免误消耗 Anlas\n"+
				"原因: %v\n\n"+
				"说明：V5 模型（%s）的 Opus 免费额度是随时间回充的配额池，查不到余量时无法判断本次是否免费。\n"+
				"建议：稍后重试，或改用 V4.5 / V4 / V3 模型（这些仍是无限免费小图）",
			err, naiModel,
		)
	}

	if !usage.IsOpus {
		return fmt.Errorf(
			"该渠道的 NovelAI 账号不是有效的 Opus 订阅，V5 模型出图会直接消耗 Anlas，已拦截\n\n"+
				"当前模型: %s\n"+
				"建议：改用 V4.5 / V4 / V3 模型，或在后台关闭「V5 配额守卫」以允许付费出图",
			naiModel,
		)
	}

	if !usage.HasQuotaInfo {
		if settings.AllowOnLookupFailure {
			log.Printf("NovelAI V5 quota field missing, allowing by config: model=%s", naiModel)
			return nil
		}
		return fmt.Errorf(
			"NovelAI 订阅接口未返回配额信息（usage 字段缺失），无法确认 V5 免费余量，已暂停出图\n\n"+
				"当前模型: %s\n"+
				"建议：改用 V4.5 / V4 / V3 模型，或在后台把「查询失败时」改为放行",
			naiModel,
		)
	}

	units := novelAIQuotaUnitsForPixels(width * height)
	needPercent := novelAIQuotaPercentForUnits(units * images)
	reservePercent := novelAIQuotaPercentForUnits(units * settings.ReserveImages)

	if usage.IsNegative {
		return fmt.Errorf(
			"NovelAI V5 免费配额已耗尽，继续出图会消耗 Anlas，已拦截\n"+
				"当前模型: %s\n%s\n\n"+
				"说明：官方仅对 V5 两个模型启用充能条配额，V4.5 / V4 / V3 仍是无限免费小图。\n"+
				"建议：等配额回充后再试，或改用 V4.5 / V4 / V3 模型",
			naiModel, novelAIQuotaRefillHint(usage),
		)
	}

	if usage.Percent < needPercent+reservePercent {
		affordable := novelAIQuotaAffordableImages(usage.Percent, units)
		return fmt.Errorf(
			"NovelAI V5 免费配额不足，已拦截以避免消耗 Anlas\n"+
				"当前模型: %s\n"+
				"剩余配额: %.1f%%（约 %d 张，本次请求 %d 张）\n"+
				"保留张数: %d 张（刻意留出，避免把最后一张也花掉）\n%s\n\n"+
				"说明：官方仅对 V5 两个模型启用充能条配额，V4.5 / V4 / V3 仍是无限免费小图。\n"+
				"建议：减少生成张数、等配额回充，或改用 V4.5 / V4 / V3 模型",
			naiModel, usage.Percent, affordable, images, settings.ReserveImages,
			novelAIQuotaRefillHint(usage),
		)
	}

	return nil
}

// consumeNovelAIV5Quota 在一张 V5 图**成功出图之后**扣减本地配额预测。
//
// 只在成功时调用：失败/取消的请求上游不会扣配额，本地跟着减会让守卫过度保守。
// 与 ensureNovelAIV5Quota 一样，非 V5 模型直接短路，不碰缓存。
func consumeNovelAIV5Quota(channel model.ModelChannel, naiModel string, width, height, images int) {
	lock := channel.FreeGenerationLock
	if lock == nil || !lock.Enabled {
		return
	}
	if !novelAIQuotaSettingsFrom(lock).GuardEnabled {
		return
	}
	if !novelAIModelHasOpusUsageLimit(naiModel) {
		return
	}
	if images < 1 {
		images = 1
	}
	units := novelAIQuotaUnitsForPixels(width * height)
	novelAIQuotaCacheFor(channel).consume(novelAIQuotaPercentForUnits(units * images))
}

// novelAIQuotaAffordableImages 按当前剩余配额估算还能出几张（不含保留位）。
//
// ⚠️ 必须加 epsilon 再截断。percent 恰好是 N 份时，percent/perImage 在 IEEE-754 下
// 可能落在 N-1e-16（例：5 份算出 4.999999999999999），直接 int() 截断会少报一张。
// 这个函数只用于错误文案里的「约 N 张」，少报虽不影响安全，但会让用户困惑
// （明明还有 5 张的量却被告知只剩 4 张）。
func novelAIQuotaAffordableImages(percent float64, units int) int {
	if percent <= 0 || units <= 0 {
		return 0
	}
	perImage := novelAIQuotaPercentForUnits(units)
	if perImage <= 0 {
		return 0
	}
	return int(percent/perImage + 1e-9)
}

// novelAIQuotaRefillHint 把「距下一个 1% 回充的秒数」变成人话。
// 上游没给这个字段时返回空串，调用方拼进文案不会留下突兀的空行。
func novelAIQuotaRefillHint(usage novelAIOpusUsage) string {
	seconds := usage.TimeUntilNextPercent
	if seconds <= 0 {
		return "配额会随时间自动回充"
	}
	switch {
	case seconds < 60:
		return fmt.Sprintf("距下一次回充（+1%%）约 %d 秒", int(seconds))
	case seconds < 3600:
		return fmt.Sprintf("距下一次回充（+1%%）约 %d 分钟", int(seconds/60))
	default:
		return fmt.Sprintf("距下一次回充（+1%%）约 %.1f 小时", seconds/3600)
	}
}
