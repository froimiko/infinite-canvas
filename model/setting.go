package model

import "encoding/json"

type SettingKey string

const (
	SettingKeyPublic  SettingKey = "public"
	SettingKeyPrivate SettingKey = "private"
)

// ModelChannel 模型渠道配置。
type ModelChannel struct {
	Protocol           string                `json:"protocol"`
	Name               string                `json:"name"`
	BaseURL            string                `json:"baseUrl"`
	APIKey             string                `json:"apiKey"`
	Models             []string              `json:"models"`
	Weight             int                   `json:"weight"`
	Enabled            bool                  `json:"enabled"`
	Remark             string                `json:"remark"`
	FreeGenerationLock *FreeGenerationLock   `json:"freeGenerationLock,omitempty"` // NovelAI 免费生图锁（Phase 2）
}

// FreeGenerationLock NovelAI 免费生图锁配置（Opus 套餐无限免费生图条件）
type FreeGenerationLock struct {
	Enabled        bool `json:"enabled"`        // 是否启用免费生图锁
	MaxPixels      int  `json:"maxPixels"`      // 最大总像素（默认 1048576 = 1024×1024）
	MaxSteps       int  `json:"maxSteps"`       // 最大步数（默认 28）
	ForceCountOne  bool `json:"forceCountOne"`  // 强制单张生成（默认 true）
	DisableImg2Img bool `json:"disableImg2Img"` // 禁用图生图（默认 true）

	// 以下三项服务于排队队列内核（Phase 1）。
	// 注意：这里刻意不提供「并发度」开关 —— NovelAI Opus 免费生图不支持并发，
	// 暴露该开关只会诱导误配置，导致违反条款或被上游限流。渠道级串行是硬约束。
	EstimatedSecondsPerImage int `json:"estimatedSecondsPerImage"` // 单张预估耗时冷启动值，秒（默认 12；有真实样本后按 EWMA 走）
	MaxUserQueuedImages      int `json:"maxUserQueuedImages"`      // 单用户队列内最大张数（默认 20，防单人灌满队列）
	MaxQueuedImages          int `json:"maxQueuedImages"`          // 全队列绝对兜底张数（默认 500，仅防内存失控，正常不触发）

	// 以下四项服务于「NAI V5 充能条配额守卫」。
	//
	// 背景：NovelAI 官方只对 V5 两个模型（nai-diffusion-5-full / nai-diffusion-5-curated）
	// 把 Opus 的「无限免费小图」改成了随时间回充的配额池（充能条），透支后按正常价扣 Anlas。
	// V4.5 / V4 / V3 / furry 仍然是无限免费小图，**绝不能**被这套逻辑波及。
	//
	// ⚠️ 零值即默认，不需要 DB migration：老配置反序列化后这些字段为 0/false，
	// 由 handler 侧的 novelAIQuotaSettingsFrom 统一回落到安全默认值。
	V5QuotaGuardEnabled bool `json:"v5QuotaGuardEnabled"` // 是否启用 V5 配额守卫（出图前查充能条余量并拦截）
	// V5QuotaReserveImages 是「始终保留不花」的张数（未设置时默认 1）。
	//
	// 为什么必须留：上游的 usage.isNegative 是**事后**信号 —— 它变 true 时，把配额
	// 推成负数的那张图已经出了、Anlas 已经扣了。只看这个标志天然晚一张。多留一张余量，
	// 临界点那张就永远花不掉，从根上消灭「这一张恰好跨线」的窗口。
	//
	// ⚠️ 用 *int 而不是 int：0 必须能表达「管理员显式选择不保留」，而 int 的零值
	// 无法与「老配置里压根没这个字段」区分开 —— 那会让填 0 静默回落成默认值 1，
	// 与后台 tooltip 承诺的「填 0 表示允许用到见底」矛盾。
	// nil = 未配置（用默认 1）；0 = 显式不保留；负数按 0 处理。
	V5QuotaReserveImages *int `json:"v5QuotaReserveImages,omitempty"`
	// V5QuotaAllowOnLookupFailure 决定查订阅失败（超时/401/上游 5xx）时怎么办。
	// false（默认）= fail-closed：拒绝 V5，保点数优先；true = fail-open：照常出图，可用性优先。
	// 无论取值如何，非 V5 模型永不受影响。
	V5QuotaAllowOnLookupFailure bool `json:"v5QuotaAllowOnLookupFailure"`
	V5QuotaCacheSeconds         int  `json:"v5QuotaCacheSeconds"` // 配额缓存 TTL，秒（默认 30，对齐参考实现的刷新周期）
}

// ModelCost 模型算力点配置。
type ModelCost struct {
	Model   string `json:"model"`
	Credits int    `json:"credits"`
}

// PublicModelChannelSetting 公开模型渠道配置。
type PublicModelChannelSetting struct {
	AvailableModels    []string    `json:"availableModels"`
	ModelCosts         []ModelCost `json:"modelCosts"`
	DefaultModel       string      `json:"defaultModel"`
	DefaultImageModel  string      `json:"defaultImageModel"`
	DefaultVideoModel  string      `json:"defaultVideoModel"`
	DefaultTextModel   string      `json:"defaultTextModel"`
	SystemPrompt       string      `json:"systemPrompt"`
	AllowCustomChannel *bool       `json:"allowCustomChannel"`
}

// PublicSetting 公开配置。
type PublicSetting struct {
	ModelChannel PublicModelChannelSetting `json:"modelChannel"`
	Auth         PublicAuthSetting         `json:"auth"`
}

type PublicAuthSetting struct {
	AllowRegister *bool                    `json:"allowRegister"`
	LinuxDo       PublicLinuxDoAuthSetting `json:"linuxDo"`
}

type PublicLinuxDoAuthSetting struct {
	Enabled bool `json:"enabled"`
}

// PrivateSetting 私有配置。
type PrivateSetting struct {
	Channels          []ModelChannel             `json:"channels"`
	PromptSync        PromptSyncSetting          `json:"promptSync"`
	PromptTagDatabase PromptTagDatabaseSetting   `json:"promptTagDatabase"`
	PromptTranslation PromptTranslationSetting   `json:"promptTranslation"`
	Auth              PrivateAuthSetting         `json:"auth"`
}

// PromptSyncSetting 提示词定时同步配置。
type PromptSyncSetting struct {
	Enabled *bool  `json:"enabled"`
	Cron    string `json:"cron"`
}

type PrivateAuthSetting struct {
	LinuxDo PrivateLinuxDoAuthSetting `json:"linuxDo"`
}

type PrivateLinuxDoAuthSetting struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// Setting 系统配置。
type Setting struct {
	Key       SettingKey      `json:"key" gorm:"primaryKey"`
	Value     json.RawMessage `json:"value" gorm:"serializer:json"`
	CreatedAt string          `json:"createdAt"`
	UpdatedAt string          `json:"updatedAt"`
}

// Settings 系统公开和私有配置。
type Settings struct {
	Public  PublicSetting  `json:"public"`
	Private PrivateSetting `json:"private"`
}
