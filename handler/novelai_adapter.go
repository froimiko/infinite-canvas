package handler

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/basketikun/infinite-canvas/model"
	"github.com/basketikun/infinite-canvas/service"
)

// novelAIDebugEnabled 由环境变量 NOVELAI_DEBUG_LOG=1 开启。
// 开启后会打印发往上游的完整请求参数与返回 ZIP 的条目清单，
// 用于和官方网页端/参考启动器的请求逐字段对照排查画质问题。
// Authorization 头不在请求体内，因此这里的日志不含 Token。
func novelAIDebugEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("NOVELAI_DEBUG_LOG"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

var novelAIFreeGenerationLocks sync.Map // map[string]*sync.Mutex，key 为 baseURL+APIKey 的 SHA-256，避免泄露 Token。

// NovelAI API 请求结构（完整 V4/V4.5 规范）
type novelAIRequest struct {
	Input      string            `json:"input"`
	Model      string            `json:"model"`
	Action     string            `json:"action"`
	Parameters novelAIParameters `json:"parameters"`
}

type novelAIParameters struct {
	// 核心参数
	ParamsVersion int     `json:"params_version"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	Scale         float64 `json:"scale"`
	Sampler       string  `json:"sampler"`
	Steps         int     `json:"steps"`
	NSamples      int     `json:"n_samples"`
	Seed          int64   `json:"seed"`

	// 负面提示词
	NegativePrompt string `json:"negative_prompt"`

	// V4/V4.5 特性参数
	UCPreset          int      `json:"ucPreset"`
	QualityToggle     bool     `json:"qualityToggle"`
	SkipCfgAboveSigma *float64 `json:"skip_cfg_above_sigma"` // Variety+: 非 nil 为开启，nil 为关闭
	CfgRescale        float64  `json:"cfg_rescale"`
	AqtPreset         string   `json:"aqtPreset,omitempty"`

	// V4 结构化 Prompt
	V4Prompt         *v4PromptStructure             `json:"v4_prompt,omitempty"`
	V4NegativePrompt *v4NegativePromptStructure     `json:"v4_negative_prompt,omitempty"`
	CharacterPrompts []novelAICharacterPromptCompat `json:"characterPrompts,omitempty"`

	// 固定参数（保持兼容性）
	NoiseSchedule                    string  `json:"noise_schedule"`
	SM                               *bool   `json:"sm,omitempty"`
	SMDyn                            *bool   `json:"sm_dyn,omitempty"`
	DynamicThresholding              bool    `json:"dynamic_thresholding"`
	ControlnetStrength               float64 `json:"controlnet_strength"`
	Legacy                           bool    `json:"legacy"`
	AddOriginalImage                 bool    `json:"add_original_image"`
	DeliberateEulerAncestralBug      bool    `json:"deliberate_euler_ancestral_bug"`
	PreferBrownian                   bool    `json:"prefer_brownian"`
	AutoSmea                           bool  `json:"autoSmea"`
	NormalizeReferenceStrengthMultiple bool  `json:"normalize_reference_strength_multiple"`
	LegacyV3Extend                     *bool `json:"legacy_v3_extend,omitempty"` // V4 专用
	UseCoords                          *bool `json:"use_coords,omitempty"`       // V4 专用：顶层副本
	UC                                 string `json:"uc,omitempty"`              // V3 专用：负面提示词冗余字段

	// img2img 参数（Phase 3）
	Image    string  `json:"image,omitempty"`
	Strength float64 `json:"strength,omitempty"`
	Noise    float64 `json:"noise,omitempty"`
}

// v4PromptStructure 是 v4_prompt 的载荷（正面提示词）。
// 参考实现只在正面结构里发送 use_coords / use_order。
type v4PromptStructure struct {
	Caption   v4Caption `json:"caption"`
	UseCoords bool      `json:"use_coords"`
	UseOrder  bool      `json:"use_order"`
}

// v4NegativePromptStructure 是 v4_negative_prompt 的载荷（负面提示词）。
// 参考实现只发送 caption + legacy_uc，多发 use_coords / use_order 会让上游
// 按不同分支解析负面词，导致负面约束强度与网页端不一致。
type v4NegativePromptStructure struct {
	Caption  v4Caption `json:"caption"`
	LegacyUC bool      `json:"legacy_uc"`
}

type v4Caption struct {
	BaseCaption  string          `json:"base_caption"`
	CharCaptions []v4CharCaption `json:"char_captions"`
}

type v4CharCaption struct {
	CharCaption string       `json:"char_caption"`
	Centers     []v4Position `json:"centers"`
}

type v4Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type novelAICharacterPromptInput struct {
	DisplayName             string             `json:"displayName"`
	CharacterPrompt         string             `json:"characterPrompt"`
	CharacterNegativePrompt string             `json:"characterNegativePrompt"`
	Coords                  *novelAIGridCoords `json:"coords"`
}

type novelAIGridCoords struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type novelAICharacterPromptCompat struct {
	Prompt string      `json:"prompt"`
	UC     string      `json:"uc"`
	Center *v4Position `json:"center"`
}

// OpenAI 兼容请求结构（简化版）
type openAIImageRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	NegativePrompt string `json:"negative_prompt"`
	N              int    `json:"n"`
	Size           string `json:"size"`
	Quality        string `json:"quality"`
	ResponseFormat string `json:"response_format"`

	NovelAIEnabled       bool                          `json:"novelai_enabled"`
	QualityToggle        *bool                         `json:"quality_toggle"`
	AddOriginalImage     *bool                         `json:"add_original_image"`
	NovelAIModel         string                        `json:"novelai_model"`
	Sampler              string                        `json:"sampler"`
	Steps                *int                          `json:"steps"`
	CfgScale             *float64                      `json:"cfg_scale"`
	Seed                 *int64                        `json:"seed"`
	UCPreset             string                        `json:"uc_preset"`
	CfgRescale           *float64                      `json:"cfg_rescale"`
	NoiseSchedule        string                        `json:"noise_schedule"`
	SM                   *bool                         `json:"sm"`
	SMDyn                *bool                         `json:"sm_dyn"`
	DynamicThresholding  *bool                         `json:"dynamic_thresholding"`
	VarietyPlus          *bool                         `json:"variety_plus"`
	AqtPreset            string                        `json:"aqt_preset"`
	DivideRoles          bool                          `json:"divide_roles"`
	UseAutoPositioning   bool                          `json:"use_auto_positioning"`
	CharacterPrompts     []novelAICharacterPromptInput `json:"character_prompts"`
}

func convertToNovelAIRequest(openAIBody []byte) (*novelAIRequest, error) {
	var openAI openAIImageRequest
	if err := json.Unmarshal(openAIBody, &openAI); err != nil {
		return nil, fmt.Errorf("解析 OpenAI 请求失败: %w", err)
	}

	// 解析尺寸
	width, height, err := parseOpenAISize(openAI.Size)
	if err != nil {
		return nil, err
	}

	// 负面提示词：OpenAI 兼容层允许 NAI 分支消费 negative_prompt；未传入时保留既有默认值。
	negativePrompt := strings.TrimSpace(openAI.NegativePrompt)
	if negativePrompt == "" {
		negativePrompt = "lowres, bad anatomy, bad hands, text, error, missing fingers, extra digit, fewer digits, cropped, worst quality, low quality, normal quality, jpeg artifacts, signature, watermark, username, blurry"
	}

	steps, scale := mapQualityToNovelAI(openAI.Quality, width, height)
	model := resolveNovelAIModel(openAI.Model)
	sampler := "k_euler_ancestral"
	seed := int64(0)
	ucPreset := 3 // 非 NovelAI 扩展分支保持"无预设"，对应 API 值 3。
	cfgRescale := 0.0
	noiseSchedule := "karras"
	sm := boolPtr(false)
	smDyn := boolPtr(false)
	dynamicThresholding := false
	var skipCfgAboveSigma *float64
	aqtPreset := ""

	if openAI.NovelAIEnabled {
		model = resolveNovelAIModel(firstNonEmpty(openAI.NovelAIModel, openAI.Model))
		steps = normalizeNovelAISteps(openAI.Steps, 28)
		scale = normalizeNovelAICfgScale(openAI.CfgScale, 5.0)
		// 下列 fallback 必须与 Aaalice_NAI_Launcher 的 ImageParams 默认值一致，
		// 否则前端未显式传值时会静默切到另一套画风：
		// sampler=k_euler_ancestral、cfg_rescale=0、noise_schedule=karras、smea/decrisp=false。
		sampler = normalizeNovelAISampler(openAI.Sampler, "k_euler_ancestral")
		seed = normalizeNovelAISeed(openAI.Seed)
		ucPreset = normalizeNovelAIUCPreset(openAI.UCPreset, 0) // 前端默认 Heavy，对应 API 值 0。
		cfgRescale = normalizeNovelAICfgRescale(openAI.CfgRescale, 0)
		noiseSchedule = normalizeNovelAINoiseSchedule(openAI.NoiseSchedule, "karras")
		sm = boolPtr(normalizeBool(openAI.SM, false))
		smDyn = boolPtr(normalizeBool(openAI.SMDyn, false))
		dynamicThresholding = normalizeBool(openAI.DynamicThresholding, false)
		if normalizeBool(openAI.VarietyPlus, false) {
			skipCfgAboveSigma = calculateSkipCfgAboveSigma(width, height)
		}
	}

	isV4Model := isNovelAIV4Model(model)
	if openAI.NovelAIEnabled && isV4Model {
		// NAI4 会拒绝/忽略 SMEA，参考实现直接删除 sm/sm_dyn；这里用 omitempty 指针避免发送。
		sm = nil
		smDyn = nil
		aqtPreset = normalizeNovelAIAqtPreset(openAI.AqtPreset, "safe")
		// V4 不支持 native，参考实现同样在此回退为 karras。
		if noiseSchedule == "native" {
			noiseSchedule = "karras"
		}
		// dynamic_thresholding（Decrisp）仅 V3 生效，V4 下不发送。
		dynamicThresholding = false
	}

	// 质量词由前端按模型注入，这里不再追加，避免与前端预设重复。
	fullPrompt := openAI.Prompt

	// uc 是 V3 时代的负面提示词冗余字段，参考实现仅在非 V4 模型下发送。
	// V4 使用 v4_negative_prompt 承载负面词，多发 uc 会造成负面词被重复解析。
	v3UC := ""
	if !isV4Model {
		v3UC = negativePrompt
	}

	// 构建 NovelAI 请求
	naiReq := &novelAIRequest{
		Input:  fullPrompt,
		Model:  model,
		Action: "generate",
		Parameters: novelAIParameters{
			// 核心参数
			ParamsVersion:  3,
			Width:          width,
			Height:         height,
			Scale:          scale,
			Sampler:        sampler,
			Steps:          steps,
			NSamples:       normalizeOpenAIImageCount(openAI.N),
			Seed:           seed,
			NegativePrompt: negativePrompt,

			// V4/V4.5 特性参数
			UCPreset:          ucPreset,
			QualityToggle:     normalizeBool(openAI.QualityToggle, true),
			SkipCfgAboveSigma: skipCfgAboveSigma,
			CfgRescale:        cfgRescale,
			AqtPreset:         aqtPreset,

			// 固定参数（NovelAI RequestParameters 支持字段）
			NoiseSchedule:                      noiseSchedule,
			SM:                                 sm,
			SMDyn:                              smDyn,
			DynamicThresholding:                dynamicThresholding,
			ControlnetStrength:                 1.0,
			Legacy:                             false,
			AddOriginalImage:                   normalizeBool(openAI.AddOriginalImage, true),
			DeliberateEulerAncestralBug:        false,
			PreferBrownian:                     true,
			AutoSmea:                           false,
			NormalizeReferenceStrengthMultiple: true,
			UC:                                 v3UC,
		},
	}

	// V4/V4.5 模型：添加结构化 Prompt
	if isV4Model {
		useManualRoleCoords := openAI.NovelAIEnabled && openAI.DivideRoles && !openAI.UseAutoPositioning
		charCaptions, charNegCaptions, compatPrompts := buildNovelAICharacterPrompts(openAI.CharacterPrompts, useManualRoleCoords)
		useRolePrompts := openAI.NovelAIEnabled && openAI.DivideRoles && len(charCaptions) > 0
		useCoords := false
		if useRolePrompts {
			useCoords = useManualRoleCoords
		}

		// 单人场景：完整提示词必须留在 base_caption，char_captions 保持为空，
		// 否则 NAI4 会把画面主体当成一个角色，导致构图松散、提示词遵循度下降。
		v4BaseCaption := fullPrompt
		v4CharCaptions := []v4CharCaption{}
		v4NegativeCharCaptions := []v4CharCaption{}
		if useRolePrompts {
			v4CharCaptions = charCaptions
			v4NegativeCharCaptions = charNegCaptions
			naiReq.Parameters.CharacterPrompts = compatPrompts
		}

		naiReq.Parameters.V4Prompt = &v4PromptStructure{
			Caption: v4Caption{
				BaseCaption:  v4BaseCaption,
				CharCaptions: v4CharCaptions,
			},
			UseCoords: useCoords,
			UseOrder:  true,
		}

		// 参考实现在 V4 分支额外写入这两个顶层字段。
		naiReq.Parameters.UseCoords = boolPtr(useCoords)
		naiReq.Parameters.LegacyV3Extend = boolPtr(false)

		naiReq.Parameters.V4NegativePrompt = &v4NegativePromptStructure{
			Caption: v4Caption{
				BaseCaption:  negativePrompt,
				CharCaptions: v4NegativeCharCaptions,
			},
			LegacyUC: false,
		}
	}

	return naiReq, nil
}

func normalizeOpenAIImageCount(count int) int {
	if count < 1 {
		return 1
	}
	if count > 10 {
		return 10
	}
	return count
}

// 解析 OpenAI 尺寸格式 "1024x1024" → width, height
func parseOpenAISize(size string) (int, int, error) {
	size = strings.TrimSpace(size)
	if size == "" || strings.EqualFold(size, "auto") {
		return 1024, 1024, nil // 默认尺寸
	}

	parts := strings.Split(strings.ToLower(size), "x")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return 0, 0, fmt.Errorf("无效的尺寸格式: %s（应为 1024x1024）", size)
	}

	var width, height int
	if _, err := fmt.Sscan(strings.TrimSpace(parts[0]), &width); err != nil {
		return 0, 0, fmt.Errorf("无效的尺寸格式: %s（应为 1024x1024）", size)
	}
	if _, err := fmt.Sscan(strings.TrimSpace(parts[1]), &height); err != nil {
		return 0, 0, fmt.Errorf("无效的尺寸格式: %s（应为 1024x1024）", size)
	}
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("无效的尺寸格式: %s（宽高必须为正整数）", size)
	}

	// NovelAI 要求尺寸必须是 64 的倍数
	width = alignTo64(width)
	height = alignTo64(height)

	// 限制最大尺寸（NovelAI V3 最大支持 2048）
	if width > 2048 {
		width = 2048
	}
	if height > 2048 {
		height = 2048
	}

	return width, height, nil
}

// 对齐到最近的 64 倍数，避免自定义宽高被静默缩水（例如 1000 对齐到 1024 而不是 960）。
func alignTo64(value int) int {
	if value < 64 {
		return 64
	}

	return ((value + 32) / 64) * 64
}

// 映射 OpenAI quality 到 NovelAI steps + scale
func mapQualityToNovelAI(quality string, width, height int) (steps int, scale float64) {
	quality = strings.ToLower(strings.TrimSpace(quality))

	// 计算总像素
	totalPixels := width * height

	// 根据质量和尺寸映射参数
	switch quality {
	case "hd", "high":
		if totalPixels <= 1024*1024 {
			// 小图高质量: 更多步数
			return 28, 5.5
		}
		return 28, 5.0
	case "standard", "medium":
		return 28, 5.0
	case "low":
		return 20, 5.0
	default:
		// 默认：免费生图参数
		return 28, 5.0
	}
}

// resolveNovelAIModel 将前端传入的模型名解析为 NovelAI 标准模型 ID。
// 支持简写（如 v3, v4.5）但使用词边界检测，避免 "anime-v3-style" 之类的子串误匹配。
func resolveNovelAIModel(modelName string) string {
	modelName = strings.ToLower(strings.TrimSpace(modelName))

	// 完全匹配已知模型 ID，直接返回。
	switch modelName {
	case "nai-diffusion-4-5-full", "nai-diffusion-4-5-curated",
		"nai-diffusion-4-full", "nai-diffusion-4-curated-preview",
		"nai-diffusion-3", "nai-diffusion-2", "nai-diffusion-furry":
		return modelName
	}

	// 模糊匹配：使用分隔符边界防止 "xv3y" 匹配 "v3"。
	// 例如 "nai-diffusion-4-5" 应匹配 V4.5，而 "anime-v3-style" 不应匹配 V3。
	if containsModelKeyword(modelName, "4.5") || containsModelKeyword(modelName, "v4.5") || containsModelKeyword(modelName, "4-5") {
		return "nai-diffusion-4-5-full"
	}
	if containsModelKeyword(modelName, "nai-diffusion-4") || containsModelKeyword(modelName, "v4") {
		return "nai-diffusion-4-curated-preview"
	}
	if containsModelKeyword(modelName, "nai-diffusion-3") || containsModelKeyword(modelName, "v3") {
		return "nai-diffusion-3"
	}
	if containsModelKeyword(modelName, "nai-diffusion-2") || containsModelKeyword(modelName, "v2") {
		return "nai-diffusion-2"
	}
	if containsModelKeyword(modelName, "furry") {
		return "nai-diffusion-furry"
	}

	// 默认使用 V3 模型（更稳定，V4 需要订阅）
	return "nai-diffusion-3"
}

// containsModelKeyword 检查 keyword 是否在 modelName 中以词边界出现。
// 词边界为：字符串首尾、连字符、下划线、空格、点号。
// 例如 "nai-diffusion-v4-5" 中 "v4" 的前后都是连字符 → 匹配。
// "xv4y" 中 "v4" 前后都是字母 → 不匹配。
func containsModelKeyword(modelName, keyword string) bool {
	idx := strings.Index(modelName, keyword)
	for idx >= 0 {
		beforeOK := idx == 0 || !isModelNameChar(modelName[idx-1])
		afterIdx := idx + len(keyword)
		afterOK := afterIdx >= len(modelName) || !isModelNameChar(modelName[afterIdx])
		if beforeOK && afterOK {
			return true
		}
		next := strings.Index(modelName[idx+1:], keyword)
		if next < 0 {
			break
		}
		idx = idx + 1 + next
	}
	return false
}

// isModelNameChar 判断字符是否为"字母数字"（即非分隔符），用于词边界检测。
func isModelNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

func isNovelAIV4Model(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model == "nai-diffusion-4-5-full" ||
		model == "nai-diffusion-4-5-curated" ||
		model == "nai-diffusion-4-full" ||
		model == "nai-diffusion-4-curated-preview" ||
		strings.HasPrefix(model, "nai-diffusion-4")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func boolPtr(value bool) *bool {
	return &value
}

func normalizeBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func normalizeNovelAISampler(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "k_euler", "k_euler_ancestral", "k_dpmpp_2s_ancestral", "k_dpmpp_2m", "k_dpmpp_sde", "ddim_v3":
		return value
	default:
		return fallback
	}
}

func normalizeNovelAISteps(value *int, fallback int) int {
	if value == nil || *value < 1 || *value > 50 {
		return fallback
	}
	return *value
}

func normalizeNovelAICfgScale(value *float64, fallback float64) float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 1 || *value > 25 {
		return fallback
	}
	return *value
}

func normalizeNovelAISeed(value *int64) int64 {
	if value == nil || *value < 0 || *value > 4294967295 {
		return randomNovelAISeed()
	}
	return *value
}

func randomNovelAISeed() int64 {
	max := big.NewInt(4294967296)
	seed, err := rand.Int(rand.Reader, max)
	if err == nil {
		return seed.Int64()
	}
	return time.Now().UnixNano() & 0xffffffff
}

// normalizeNovelAIUCPreset 把前端 UC Preset 名称映射为 NovelAI API 的整数值。
// 取值与 Aaalice_NAI_Launcher 的 UcPresets.toApiValue 完全一致：
//
//	Heavy = 0, Light = 1, Human Focus = 2, None = 3
//
// 注意不要与 NAI 早期版本的显示顺序（Heavy=4/Light=5/...）混淆，
// 发错值会让上游按另一套预设跑，直接造成画风与质量偏移。
func normalizeNovelAIUCPreset(value string, fallback int) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "heavy":
		return 0
	case "light":
		return 1
	case "human focus", "human_focus", "human-focus":
		return 2
	case "none":
		return 3
	default:
		return fallback
	}
}

func normalizeNovelAICfgRescale(value *float64, fallback float64) float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 1 {
		return fallback
	}
	return *value
}

func normalizeNovelAINoiseSchedule(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "native", "karras", "exponential", "polyexponential":
		return value
	default:
		return fallback
	}
}

func normalizeNovelAIAqtPreset(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "safe", "nai", "full", "balanced", "anime", "furry", "pony":
		return value
	default:
		return fallback
	}
}

// calculateSkipCfgAboveSigma 计算 Variety+ 的 skip_cfg_above_sigma。
// 系数与 Aaalice_NAI_Launcher 保持一致：所有模型统一使用 58.0，不按模型区分。
// 之前按模型分 19.0/59.0 会让 Variety+ 行为明显偏离官方网页端，造成画风漂移。
func calculateSkipCfgAboveSigma(width, height int) *float64 {
	w := float64(width) / 8
	h := float64(height) / 8
	value := 58.0 * math.Sqrt(4.0*w*h/63232)
	return &value
}

func buildNovelAICharacterPrompts(prompts []novelAICharacterPromptInput, useDefaultCenter bool) ([]v4CharCaption, []v4CharCaption, []novelAICharacterPromptCompat) {
	charCaptions := make([]v4CharCaption, 0, len(prompts))
	charNegCaptions := make([]v4CharCaption, 0, len(prompts))
	compatPrompts := make([]novelAICharacterPromptCompat, 0, len(prompts))

	for _, prompt := range prompts {
		characterPrompt := strings.TrimSpace(prompt.CharacterPrompt)
		characterNegativePrompt := strings.TrimSpace(prompt.CharacterNegativePrompt)
		if characterPrompt == "" && characterNegativePrompt == "" {
			continue
		}

		center := mapNovelAICharacterPromptCoords(prompt.Coords)
		if center == nil && useDefaultCenter {
			center = &v4Position{X: 0.5, Y: 0.5}
		}
		centers := []v4Position{}
		if center != nil {
			centers = append(centers, *center)
		}

		charCaptions = append(charCaptions, v4CharCaption{
			CharCaption: characterPrompt,
			Centers:     centers,
		})
		charNegCaptions = append(charNegCaptions, v4CharCaption{
			CharCaption: characterNegativePrompt,
			Centers:     centers,
		})
		compatPrompts = append(compatPrompts, novelAICharacterPromptCompat{
			Prompt: characterPrompt,
			UC:     characterNegativePrompt,
			Center: center,
		})
	}

	return charCaptions, charNegCaptions, compatPrompts
}

func mapNovelAICharacterPromptCoords(coords *novelAIGridCoords) *v4Position {
	if coords == nil {
		return nil
	}
	return &v4Position{
		X: mapNovelAIGridCoord(coords.X),
		Y: mapNovelAIGridCoord(coords.Y),
	}
}

func mapNovelAIGridCoord(value int) float64 {
	if value < 0 {
		value = 0
	}
	if value > 4 {
		value = 4
	}
	switch value {
	case 0:
		return 0.0
	case 1:
		return 0.1
	case 2:
		return 0.3
	case 3:
		return 0.5
	default:
		return 0.7
	}
}

// 转换 NovelAI ZIP 响应为 OpenAI JSON 格式
func convertNovelAIResponse(zipData []byte) ([]byte, error) {
	data, err := extractNovelAIImageData(zipData)
	if err != nil {
		return nil, err
	}
	return marshalOpenAIImageResponse(data)
}

func extractNovelAIImageData(zipData []byte) ([]map[string]interface{}, error) {
	// 读取 ZIP 文件
	zipReader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("解压 NovelAI 响应失败: %w", err)
	}

	// 收集图片条目并按文件名排序。
	// NovelAI 的 ZIP 条目顺序不保证稳定，直接按归档顺序取第一张有可能拿到
	// 中间预览帧（表现为"步数没跑完"的糊图）。按名字排序后 image_0 恒定在前，
	// 多图批量也能保证顺序与请求一致。
	files := make([]*zip.File, 0, len(zipReader.File))
	for _, file := range zipReader.File {
		if file.FileInfo().IsDir() || !isImageFile(file.Name) {
			continue
		}
		files = append(files, file)
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	if novelAIDebugEnabled() {
		names := make([]string, 0, len(zipReader.File))
		for _, file := range zipReader.File {
			names = append(names, fmt.Sprintf("%s(%d bytes)", file.Name, file.UncompressedSize64))
		}
		log.Printf("[novelai-debug] zip entries=%d detail=%s", len(zipReader.File), strings.Join(names, ", "))
	}

	data := make([]map[string]interface{}, 0, len(files))
	for _, file := range files {
		rc, err := file.Open()
		if err != nil {
			continue
		}
		imageData, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || len(imageData) == 0 {
			continue
		}

		data = append(data, map[string]interface{}{
			"b64_json": base64.StdEncoding.EncodeToString(imageData),
		})
	}

	if len(data) == 0 {
		return nil, errors.New("NovelAI 响应中未找到有效图片")
	}
	return data, nil
}

func marshalOpenAIImageResponse(data []map[string]interface{}) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"data": data,
	})
}

// 判断是否为图片文件
func isImageFile(filename string) bool {
	filename = strings.ToLower(filename)
	return strings.HasSuffix(filename, ".png") ||
		strings.HasSuffix(filename, ".jpg") ||
		strings.HasSuffix(filename, ".jpeg") ||
		strings.HasSuffix(filename, ".webp")
}

// NovelAI 代理请求主函数
func proxyNovelAIImageRequest(w http.ResponseWriter, r *http.Request, body []byte, channel model.ModelChannel, user model.AuthUser, credits int) {
	// 0. 解析 OpenAI 请求（用于免费生图锁校验）
	var openAIReq openAIImageRequest
	if err := json.Unmarshal(body, &openAIReq); err != nil {
		log.Printf("NovelAI parse request failed: %v", err)
		Fail(w, fmt.Sprintf("请求格式错误: %v", err))
		return
	}

	// 0.1 检查免费生图锁（在转换之前）
	hasReferenceImages := false // Phase 1 暂不支持参考图，Phase 3 时需要从 body 中检测
	if err := applyFreeGenerationLock(&openAIReq, channel.FreeGenerationLock, hasReferenceImages); err != nil {
		log.Printf("NovelAI free lock rejected: %v", err)
		Fail(w, err.Error())
		return
	}

	requestCount := normalizeOpenAIImageCount(openAIReq.N)
	forceSingleRequests := channel.FreeGenerationLock != nil && channel.FreeGenerationLock.Enabled
	if forceSingleRequests {
		openAIReq.N = 1
	}

	// 1. 先转换一次，确定模型名并在上游请求前扣费
	sampleBody, err := json.Marshal(openAIReq)
	if err != nil {
		Fail(w, "构建 NovelAI 请求失败")
		return
	}
	sampleReq, err := convertToNovelAIRequest(sampleBody)
	if err != nil {
		log.Printf("NovelAI request conversion failed: %v", err)
		Fail(w, fmt.Sprintf("请求格式转换失败: %v", err))
		return
	}

	totalCredits := credits * requestCount
	if err := service.ConsumeUserCredits(user.ID, sampleReq.Model, totalCredits, "/images/generations"); err != nil {
		FailError(w, err)
		return
	}

	var data []map[string]interface{}
	var requestErr error
	succeededCount := requestCount
	if forceSingleRequests && requestCount > 1 {
		data, succeededCount, requestErr = requestNovelAISingleImageBatch(openAIReq, requestCount, channel)
	} else {
		data, requestErr = requestNovelAIImageData(channel, sampleReq)
	}
	if requestErr != nil {
		if err := service.RefundUserCredits(user.ID, sampleReq.Model, totalCredits, "/images/generations"); err != nil {
			log.Printf("Refund failed: %v", err)
		}
		Fail(w, requestErr.Error())
		return
	}
	jsonResponse, err := marshalOpenAIImageResponse(data)
	if err != nil {
		if err := service.RefundUserCredits(user.ID, sampleReq.Model, totalCredits, "/images/generations"); err != nil {
			log.Printf("Refund failed: %v", err)
		}
		Fail(w, "构建 NovelAI 响应失败")
		return
	}
	if succeededCount < requestCount {
		refundCredits := credits * (requestCount - succeededCount)
		if err := service.RefundUserCredits(user.ID, sampleReq.Model, refundCredits, "/images/generations"); err != nil {
			log.Printf("Partial refund failed: %v", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jsonResponse)
}

type novelAIImageBatchResult struct {
	Index int
	Data  []map[string]interface{}
	Err   error
}

func requestNovelAISingleImageBatch(openAIReq openAIImageRequest, count int, channel model.ModelChannel) ([]map[string]interface{}, int, error) {
	resultCh := make(chan novelAIImageBatchResult, count)
	for index := 0; index < count; index++ {
		go func(index int) {
			slotReq := openAIReq
			slotReq.N = 1
			body, err := json.Marshal(slotReq)
			if err != nil {
				resultCh <- novelAIImageBatchResult{Index: index, Err: err}
				return
			}
			naiReq, err := convertToNovelAIRequest(body)
			if err != nil {
				resultCh <- novelAIImageBatchResult{Index: index, Err: err}
				return
			}
			data, err := requestNovelAIImageData(channel, naiReq)
			resultCh <- novelAIImageBatchResult{Index: index, Data: data, Err: err}
		}(index)
	}

	ordered := make([][]map[string]interface{}, count)
	var firstErr error
	succeededCount := 0
	for index := 0; index < count; index++ {
		result := <-resultCh
		if result.Err != nil {
			log.Printf("NovelAI single-image request failed: slot=%d err=%v", result.Index, result.Err)
			if firstErr == nil {
				firstErr = result.Err
			}
			continue
		}
		succeededCount++
		ordered[result.Index] = result.Data
	}

	merged := make([]map[string]interface{}, 0, count)
	for _, item := range ordered {
		merged = append(merged, item...)
	}
	if len(merged) == 0 {
		if firstErr != nil {
			return nil, 0, firstErr
		}
		return nil, 0, errors.New("NovelAI 响应中未找到有效图片")
	}
	if firstErr != nil {
		log.Printf("NovelAI batch completed with partial failures: requested=%d succeeded=%d", count, succeededCount)
	}
	return merged, succeededCount, nil
}

func withNovelAIFreeGenerationLock(channel model.ModelChannel, fn func() ([]map[string]interface{}, error)) ([]map[string]interface{}, error) {
	if channel.FreeGenerationLock == nil || !channel.FreeGenerationLock.Enabled {
		return fn()
	}

	key := novelAIFreeGenerationLockKey(channel)
	value, _ := novelAIFreeGenerationLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	return fn()
}

func novelAIFreeGenerationLockKey(channel model.ModelChannel) string {
	baseURL := strings.TrimRight(strings.TrimSpace(channel.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://image.novelai.net"
	}
	sum := sha256.Sum256([]byte(baseURL + "\x00" + channel.APIKey))
	return hex.EncodeToString(sum[:])
}

func requestNovelAIImageData(channel model.ModelChannel, naiReq *novelAIRequest) ([]map[string]interface{}, error) {
	naiBody, err := json.Marshal(naiReq)
	if err != nil {
		return nil, errors.New("构建 NovelAI 请求失败")
	}

	if novelAIDebugEnabled() {
		// 打印完整请求体，方便和官方网页端 / Aaalice_NAI_Launcher 的请求逐字段对照。
		// Token 在 Authorization 头里，不会出现在这段日志中。
		log.Printf("[novelai-debug] request body=%s", string(naiBody))
	}

	naiURL := buildNovelAIURL(channel.BaseURL, "/ai/generate-image")
	request, err := http.NewRequest(http.MethodPost, naiURL, bytes.NewReader(naiBody))
	if err != nil {
		return nil, errors.New("创建 NovelAI 请求失败")
	}
	request.Header.Set("Authorization", "Bearer "+channel.APIKey)
	request.Header.Set("Content-Type", "application/json")

	return withNovelAIFreeGenerationLock(channel, func() ([]map[string]interface{}, error) {
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			log.Printf("NovelAI request failed: url=%s err=%v", naiURL, err)
			return nil, errors.New("NovelAI 接口请求失败")
		}
		defer response.Body.Close()

		if response.StatusCode >= http.StatusBadRequest {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			log.Printf("NovelAI upstream error: status=%d body=%s", response.StatusCode, string(body))
			return nil, errors.New(readNovelAIError(response.StatusCode, body))
		}

		zipData, err := io.ReadAll(response.Body)
		if err != nil {
			log.Printf("NovelAI response read failed: %v", err)
			return nil, errors.New("读取 NovelAI 响应失败")
		}
		data, err := extractNovelAIImageData(zipData)
		if err != nil {
			log.Printf("NovelAI response conversion failed: %v", err)
			return nil, fmt.Errorf("NovelAI 响应转换失败: %w", err)
		}
		return data, nil
	})
}

// 构建 NovelAI URL
func buildNovelAIURL(baseURL, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://image.novelai.net"
	}
	return baseURL + path
}

// 应用免费生图锁限制
func applyFreeGenerationLock(req *openAIImageRequest, lock *model.FreeGenerationLock, hasReferenceImages bool) error {
	if lock == nil || !lock.Enabled {
		return nil
	}

	// 1. 强制单张上游请求
	// 免费模式限制的是单个 NovelAI generation request 必须 n_samples=1；
	// 外层 OpenAI n>1 会在代理层拆成多个并发的单图请求，不在这里拒绝。

	// 2. 禁用图生图
	if lock.DisableImg2Img && hasReferenceImages {
		return errors.New("该渠道已启用免费生图锁，不支持图生图或参考图功能（仅限纯文生图）")
	}

	// 3. 限制尺寸
	width, height, err := parseOpenAISize(req.Size)
	if err != nil {
		return err
	}
	totalPixels := width * height
	if totalPixels > lock.MaxPixels {
		return fmt.Errorf(
			"该渠道已启用免费生图锁（NovelAI Opus 无限免费生图模式）\n"+
				"当前尺寸: %dx%d (%d 像素)\n"+
				"限制尺寸: ≤%d 像素（推荐 1024×1024）\n\n"+
				"建议：将尺寸调整为 1024×1024 或更小，即可免费生成",
			width, height, totalPixels, lock.MaxPixels,
		)
	}

	// 4. 限制步数：NovelAI 扩展模式显式传 steps 时优先检查；否则保持旧的 quality 推断逻辑。
	steps, _ := mapQualityToNovelAI(req.Quality, width, height)
	stepSource := fmt.Sprintf("从 quality=%s 推断", req.Quality)
	if req.NovelAIEnabled && req.Steps != nil {
		steps = normalizeNovelAISteps(req.Steps, 28)
		stepSource = "显式 NovelAI steps"
	}
	if steps > lock.MaxSteps {
		return fmt.Errorf(
			"该渠道已启用免费生图锁（NovelAI Opus 无限免费生图模式）\n"+
				"当前步数: %d（%s）\n"+
				"限制步数: ≤%d\n\n"+
				"建议：使用默认质量参数或降低 steps/quality",
			steps, stepSource, lock.MaxSteps,
		)
	}

	return nil
}

// 读取 NovelAI 错误信息
func readNovelAIError(statusCode int, body []byte) string {
	// 尝试解析 JSON 错误
	var errResp struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil {
		if errResp.Message != "" {
			return fmt.Sprintf("NovelAI 错误: %s", errResp.Message)
		}
		if errResp.Error != "" {
			return fmt.Sprintf("NovelAI 错误: %s", errResp.Error)
		}
	}

	// 通用错误
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "NovelAI 鉴权失败，请检查 API Token（Persistent API Token）"
	case http.StatusTooManyRequests:
		return "NovelAI 请求限流或 Anlas 不足"
	case http.StatusPaymentRequired:
		return "NovelAI Anlas 余额不足，请充值或使用免费生图锁"
	default:
		return fmt.Sprintf("NovelAI 请求失败: HTTP %d", statusCode)
	}
}
