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
	Channels                     []ModelChannel                      `json:"channels"`
	PromptSync                   PromptSyncSetting                   `json:"promptSync"`
	PromptTagDatabase            PromptTagDatabaseSetting            `json:"promptTagDatabase"`
	PromptTagTranslationDatabase PromptTagTranslationDatabaseSetting `json:"promptTagTranslationDatabase"`
	Auth                         PrivateAuthSetting                  `json:"auth"`
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
