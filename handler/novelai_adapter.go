package handler

import (
	"archive/zip"
	"bytes"
	"context"
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

// 渠道级串行队列已迁移到 novelai_queue.go（novelAIQueues）。
// 那里在原有「可取消 channel 锁」之外，额外维护 FIFO 票号与剩余张数，
// 使排队位置和预估等待时间可查询；key 仍是 novelAIFreeGenerationLockKey。

// NovelAI API 请求结构（V4+ 结构化规范，V5 沿用 v4_prompt 字段）
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

	// V4+ 兼容参数
	UCPreset          int      `json:"ucPreset"`
	QualityToggle     bool     `json:"qualityToggle"`
	SkipCfgAboveSigma *float64 `json:"skip_cfg_above_sigma"` // Variety+: 非 nil 为开启，nil 为关闭
	CfgRescale        float64  `json:"cfg_rescale"`

	// V4 结构化 Prompt
	V4Prompt         *v4PromptStructure             `json:"v4_prompt,omitempty"`
	V4NegativePrompt *v4NegativePromptStructure     `json:"v4_negative_prompt,omitempty"`
	CharacterPrompts []novelAICharacterPromptCompat `json:"characterPrompts,omitempty"`

	// 固定参数（保持兼容性）
	NoiseSchedule                      string  `json:"noise_schedule"`
	SM                                 *bool   `json:"sm,omitempty"`
	SMDyn                              *bool   `json:"sm_dyn,omitempty"`
	DynamicThresholding                bool    `json:"dynamic_thresholding"`
	ControlnetStrength                 float64 `json:"controlnet_strength"`
	Legacy                             bool    `json:"legacy"`
	AddOriginalImage                   bool    `json:"add_original_image"`
	DeliberateEulerAncestralBug        bool    `json:"deliberate_euler_ancestral_bug"`
	PreferBrownian                     bool    `json:"prefer_brownian"`
	AutoSmea                           bool    `json:"autoSmea"`
	NormalizeReferenceStrengthMultiple bool    `json:"normalize_reference_strength_multiple"`
	LegacyV3Extend                     *bool   `json:"legacy_v3_extend,omitempty"` // V4 专用
	UseCoords                          *bool   `json:"use_coords,omitempty"`       // V4 专用：顶层副本
	UC                                 string  `json:"uc,omitempty"`               // V3 专用：负面提示词冗余字段

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

// novelAICharacterPromptInput 是前端上报的单个角色。
//
// 坐标有两套字段，优先级固定为 Center > Coords：
//   - Center 是连续坐标 0-1，与参考实现 nai_image_request_builder.dart:194-206 一致，
//     位置画布拖到哪就发哪，不做任何量化（量化会让「松手即生效」变成吸附跳格）；
//   - Coords 是历史的 5x5 网格整数 0-4，画布 NovelAI 节点的角色面板仍在用，
//     经 mapNovelAIGridCoord 量化成 0.0/0.1/0.3/0.5/0.7（注意最大只到 0.7）。
//
// 两套并存而不是替换：换掉 Coords 语义会让存量画布节点的角色位置整体漂移。
type novelAICharacterPromptInput struct {
	DisplayName             string             `json:"displayName"`
	CharacterPrompt         string             `json:"characterPrompt"`
	CharacterNegativePrompt string             `json:"characterNegativePrompt"`
	Coords                  *novelAIGridCoords `json:"coords"`
	Center                  *v4Position        `json:"center"`
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

	NovelAIEnabled      bool                          `json:"novelai_enabled"`
	QualityToggle       *bool                         `json:"quality_toggle"`
	AddOriginalImage    *bool                         `json:"add_original_image"`
	NovelAIModel        string                        `json:"novelai_model"`
	Sampler             string                        `json:"sampler"`
	Steps               *int                          `json:"steps"`
	CfgScale            *float64                      `json:"cfg_scale"`
	Seed                *int64                        `json:"seed"`
	UCPreset            string                        `json:"uc_preset"`
	CfgRescale          *float64                      `json:"cfg_rescale"`
	NoiseSchedule       string                        `json:"noise_schedule"`
	SM                  *bool                         `json:"sm"`
	SMDyn               *bool                         `json:"sm_dyn"`
	DynamicThresholding *bool                         `json:"dynamic_thresholding"`
	VarietyPlus         *bool                         `json:"variety_plus"`
	DivideRoles         bool                          `json:"divide_roles"`
	UseAutoPositioning  bool                          `json:"use_auto_positioning"`
	CharacterPrompts    []novelAICharacterPromptInput `json:"character_prompts"`
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

	usesStructuredPrompt := usesNovelAIStructuredPrompt(model)

	// 采样器按模型能力归一化：DDIM 在 V4+ 不被支持，直接发会让上游返回 500。
	// 必须放在 usesStructuredPrompt 之后、SMEA 处理之前 —— 因为回退结果会影响
	// 下面「DDIM 必须关 SMEA」的判定。
	sampler = mapNovelAISamplerForModel(sampler, model)

	// DDIM 系采样器与 SMEA 互斥（参考实现 image_params.dart:324-346）。
	// V4+ 在下面的分支里本就不发 SMEA，这里主要覆盖 V3 + DDIM 的组合。
	if novelAISamplerDisablesSMEA(sampler) {
		sm = boolPtr(false)
		smDyn = boolPtr(false)
	}

	if openAI.NovelAIEnabled && usesStructuredPrompt {
		// V4+ 结构化模型拒绝/忽略 SMEA；用 omitempty 指针避免发送。
		sm = nil
		smDyn = nil
		// V4+ 不支持 native，在此统一回退为 karras。
		if noiseSchedule == "native" {
			noiseSchedule = "karras"
		}
		// dynamic_thresholding（Decrisp）仅 V3 生效，V4+ 下不发送。
		dynamicThresholding = false
	}

	// 质量词由前端按模型注入，这里不再追加，避免与前端预设重复。
	fullPrompt := openAI.Prompt

	// uc 是 V3 时代的负面提示词冗余字段，仅在非结构化模型下发送。
	// V4/V4.5/V5 使用 v4_negative_prompt 承载负面词，多发 uc 会造成重复解析。
	v3UC := ""
	if !usesStructuredPrompt {
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

			// V4+ 兼容参数
			UCPreset:          ucPreset,
			QualityToggle:     normalizeBool(openAI.QualityToggle, false),
			SkipCfgAboveSigma: skipCfgAboveSigma,
			CfgRescale:        cfgRescale,

			// 固定参数（NovelAI RequestParameters 支持字段）
			NoiseSchedule:                      noiseSchedule,
			SM:                                 sm,
			SMDyn:                              smDyn,
			DynamicThresholding:                dynamicThresholding,
			ControlnetStrength:                 1.0,
			Legacy:                             false,
			AddOriginalImage:                   normalizeBool(openAI.AddOriginalImage, false),
			DeliberateEulerAncestralBug:        false,
			PreferBrownian:                     true,
			AutoSmea:                           false,
			NormalizeReferenceStrengthMultiple: true,
			UC:                                 v3UC,
		},
	}

	// V4/V4.5/V5 模型：添加结构化 Prompt（官方字段名仍为 v4_prompt / v4_negative_prompt）。
	if usesStructuredPrompt {
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

	// 兜底剥掉前端的 `渠道id::` 前缀（可能叠了多层）。
	// 正常路径下前端已用 modelOptionName 处理过，这里只是防止漏剥时
	// 把整串当模型名发给上游，造成模糊匹配错位甚至 502。
	for {
		index := strings.LastIndex(modelName, "::")
		if index < 0 {
			break
		}
		modelName = strings.TrimSpace(modelName[index+2:])
	}

	// 完全匹配已知模型 ID，直接返回。
	switch modelName {
	case "nai-diffusion-5-full", "nai-diffusion-5-curated",
		"nai-diffusion-4-5-full", "nai-diffusion-4-5-curated",
		"nai-diffusion-4-full", "nai-diffusion-4-curated-preview",
		"nai-diffusion-3", "nai-diffusion-2", "nai-diffusion-furry":
		return modelName
	}

	// 模糊匹配：使用分隔符边界防止 "xv3y" 匹配 "v3"。
	// 顺序从新到旧。裸 "5" 仅允许完全相等，否则 "v4.5" 的末尾 5 会误判成 V5。
	if modelName == "5" || containsModelKeyword(modelName, "v5") || containsModelKeyword(modelName, "nai-diffusion-5") {
		if strings.Contains(modelName, "curated") {
			return "nai-diffusion-5-curated"
		}
		return "nai-diffusion-5-full"
	}
	// 例如 "nai-diffusion-4-5" 应匹配 V4.5，而 "anime-v3-style" 不应匹配 V3。
	if containsModelKeyword(modelName, "4.5") || containsModelKeyword(modelName, "v4.5") || containsModelKeyword(modelName, "4-5") {
		if strings.Contains(modelName, "curated") {
			return "nai-diffusion-4-5-curated"
		}
		return "nai-diffusion-4-5-full"
	}
	if containsModelKeyword(modelName, "nai-diffusion-4") || containsModelKeyword(modelName, "v4") {
		if strings.Contains(modelName, "full") {
			return "nai-diffusion-4-full"
		}
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

// usesNovelAIStructuredPrompt 判断模型是否使用官方的 v4_prompt / v4_negative_prompt 结构。
// 字段名虽然仍叫 v4，但 V5 沿用同一协议；不要再用版本号命名这个判定，避免以后漏新模型。
func usesNovelAIStructuredPrompt(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "nai-diffusion-4") || strings.HasPrefix(model, "nai-diffusion-5")
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

// mapNovelAISamplerForModel 把采样器按模型能力归一化。
//
// ⚠️ 这一步不能省：DDIM 系采样器在 V4 起（V4 / V4.5 / V5）**不被支持**，
// 原样发上去 NovelAI 会直接返回 500 Internal Server Error。
//
// 线上真实故障（2026-08-26）：用户选了 ddim_v3 + nai-diffusion-4-5-full，
// 连续 4 次请求全部拿到
//
//	NovelAI upstream error: model=nai-diffusion-4-5-full status=500
//	body={"statusCode":500,"message":"Internal Server Error"}
//
// 而同一模型换回默认采样器后立刻成功。当时误以为是用户网络问题 —— 其实请求
// 早就打到上游并拿到了响应（upstream=2.7s），网络不通根本走不到这一步。
//
// 归一化规则逐条对齐参考实现 Aaalice_NAI_Launcher
// （nai_image_generation_api_service.dart:54-74 mapSamplerForModel）：
//   - V4+：DDIM 不支持 → 回退 k_euler_ancestral
//   - V3：DDIM → ddim_v3（V3 专用变体）
//   - 其余组合原样透传
func mapNovelAISamplerForModel(sampler, naiModel string) string {
	sampler = strings.ToLower(strings.TrimSpace(sampler))
	if !strings.Contains(sampler, "ddim") {
		return sampler
	}

	// usesNovelAIStructuredPrompt 正好等价于「V4 及以上」，复用它避免再写一份版本判定。
	if usesNovelAIStructuredPrompt(naiModel) {
		log.Printf("NovelAI sampler %s not supported by %s, falling back to k_euler_ancestral", sampler, naiModel)
		return "k_euler_ancestral"
	}
	if strings.Contains(naiModel, "diffusion-3") {
		return "ddim_v3"
	}
	return sampler
}

// novelAISamplerDisablesSMEA 判断该采样器是否必须关闭 SMEA。
//
// 参考实现在 sampler 含 ddim 时强制 effectiveSmea/effectiveSmeaDyn = false
// （image_params.dart:324-346）。V4+ 分支本就不发 SMEA，但 V3 + DDIM 也必须关，
// 否则是上游不接受的组合。
func novelAISamplerDisablesSMEA(sampler string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(sampler)), "ddim")
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

		// 坐标优先级 Center > Coords：新的位置画布走连续坐标直传，
		// 老的画布节点仍只带网格坐标，两条链路各取所需。
		center := clampNovelAICharacterCenter(prompt.Center)
		if center == nil {
			center = mapNovelAICharacterPromptCoords(prompt.Coords)
		}
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

// clampNovelAICharacterCenter 钳制连续坐标到 0.0-1.0。
//
// 与 mapNovelAIGridCoord 的区别：这里**不做任何量化**，拖到 0.37 就发 0.37，
// 与参考实现一致。NaN/Inf 直接丢弃（返回 nil 走后续回退），
// 否则序列化出的 JSON 会带非法数值让上游直接拒绝整个请求。
func clampNovelAICharacterCenter(center *v4Position) *v4Position {
	if center == nil {
		return nil
	}
	if math.IsNaN(center.X) || math.IsNaN(center.Y) || math.IsInf(center.X, 0) || math.IsInf(center.Y, 0) {
		return nil
	}
	return &v4Position{
		X: clampNovelAIUnitCoord(center.X),
		Y: clampNovelAIUnitCoord(center.Y),
	}
}

func clampNovelAIUnitCoord(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
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

	// 0.2 NAI V5 充能条配额守卫。
	//
	// ★ 位置有讲究，别挪：
	//   - 必须在 ConsumeUserCredits **之前** —— 拦下来的请求不该扣算力点。
	//   - 必须在 wantsNovelAISSE 分支**之前** —— SSE 一旦发出 200 响应头就再也
	//     改不了状态码，那时只能走 event: error，前端拿不到干净的 HTTP 错误。
	//
	// 这里用 sampleReq 的**已解析模型 ID** 与**实际出图尺寸**：前者保证 V4.5 不会
	// 被误判成 V5，后者保证大图按面积扣多份配额。非 V5 模型在守卫内部第一步就短路，
	// 一次上游订阅查询都不会发。
	if err := ensureNovelAIV5Quota(
		r.Context(), channel, sampleReq.Model,
		sampleReq.Parameters.Width, sampleReq.Parameters.Height, requestCount,
	); err != nil {
		log.Printf("NovelAI V5 quota guard rejected: model=%s count=%d err=%v", sampleReq.Model, requestCount, err)
		Fail(w, err.Error())
		return
	}

	if err := service.ConsumeUserCredits(user.ID, sampleReq.Model, totalCredits, "/images/generations"); err != nil {
		FailError(w, err)
		return
	}

	// 2. 走 SSE 还是普通 JSON。
	//
	// 到这里为止都还没写响应头，所以上面任何失败都能正常返回 HTTP 错误码。
	// SSE 分支从这一刻开始会立即发出 200 响应头（详见 streamNovelAIImageRequest），
	// 之后就无法再改状态码了。
	if wantsNovelAISSE(r) {
		streamNovelAIImageRequest(w, r, streamNovelAIParams{
			OpenAIReq:           openAIReq,
			SampleReq:           sampleReq,
			Channel:             channel,
			User:                user,
			Credits:             credits,
			TotalCredits:        totalCredits,
			RequestCount:        requestCount,
			ForceSingleRequests: forceSingleRequests,
		})
		return
	}

	var data []map[string]interface{}
	var requestErr error
	succeededCount := requestCount
	if forceSingleRequests && requestCount > 1 {
		data, succeededCount, requestErr = requestNovelAISingleImageBatch(r.Context(), openAIReq, requestCount, channel, user.ID, nil, nil)
	} else {
		data, requestErr = requestNovelAIImageData(r.Context(), channel, sampleReq)
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

// requestNovelAISingleImageBatch 在**一个**队列名额内串行出多张图。
//
// 历史实现开 count 个 goroutine、每个各自去抢同一把串行锁：在 NovelAI 免费生图
// 「不支持并发」的前提下这并不会更快（实际仍逐张出图），却有两个坏处：
//  1. 往队列里插了 count 个分散占位，别人看到的「前方还有几张」完全失真；
//  2. 单个用户点 10 张就能独占队列，把后面所有人拖到 Cloudflare 100s 之外。
//
// 现在整批只占一个名额，内部串行出图，并在每张完成后 markProgress 递减剩余张数，
// 这样后面排队的人看到的前方张数会实时下降。
//
// onImageDone 可为 nil；非 nil 时每张出图完成后回调 (current, total)，供 SSE 推进度。
func requestNovelAISingleImageBatch(
	ctx context.Context,
	openAIReq openAIImageRequest,
	count int,
	channel model.ModelChannel,
	userID string,
	onQueueUpdate func(imagesAhead int, estimatedSeconds int),
	onImageDone func(current, total int),
) ([]map[string]interface{}, int, error) {
	if count < 1 {
		count = 1
	}

	// 先把每张的请求体转换好：转换失败属于参数问题，没必要占着队列名额再报错。
	requests := make([]*novelAIRequest, 0, count)
	for index := 0; index < count; index++ {
		slotReq := openAIReq
		slotReq.N = 1
		body, err := json.Marshal(slotReq)
		if err != nil {
			return nil, 0, err
		}
		naiReq, err := convertToNovelAIRequest(body)
		if err != nil {
			return nil, 0, err
		}
		requests = append(requests, naiReq)
	}

	naiModel := ""
	if len(requests) > 0 {
		naiModel = requests[0].Model
	}

	succeededCount := 0
	var firstErr error

	merged, err := withNovelAIQueue(ctx, channel, naiModel, userID, count, onQueueUpdate,
		func(entry *novelAIQueueEntry) ([]map[string]interface{}, error) {
			queue := novelAIQueueFor(channel)
			ordered := make([][]map[string]interface{}, count)

			for index, naiReq := range requests {
				// 客户端断开就别再往下出图了：既省 Anlas，也尽快让出队列。
				if ctx.Err() != nil {
					if firstErr == nil {
						firstErr = errNovelAIRequestCanceled
					}
					break
				}

				// V5 配额批内复查。
				//
				// 为什么开头查过还要逐张查：批量 10 张在入口一次性放行后，可能在第 7 张
				// 时才跨过临界点（本批自己在消耗，别的用户也在消耗同一个 Token 的配额）。
				// 不复查就会把剩下几张白扣成 Anlas —— 正是本特性要防的事。
				//
				// 这里通常只读缓存（TTL 内不发请求），代价极低；一旦不通过就中止本批，
				// 已出的图照常返回，未出的部分走既有「部分失败 → 部分退款」路径。
				if err := ensureNovelAIV5Quota(ctx, channel, naiReq.Model, naiReq.Parameters.Width, naiReq.Parameters.Height, 1); err != nil {
					log.Printf("NovelAI batch stopped by V5 quota guard: slot=%d/%d err=%v", index+1, count, err)
					if firstErr == nil {
						firstErr = err
					}
					break
				}

				data, _, err := doNovelAIUpstreamRequest(ctx, channel, naiReq)
				if err != nil {
					// 单张失败不中断整批：剩下的继续出，末尾按实际成功张数部分退款。
					log.Printf("NovelAI batch image failed: slot=%d/%d err=%v", index+1, count, err)
					if firstErr == nil {
						firstErr = err
					}
				} else {
					ordered[index] = data
					succeededCount++
					// 出图成功才扣配额：失败/取消的请求上游不会计入配额池。
					consumeNovelAIV5Quota(channel, naiReq.Model, naiReq.Parameters.Width, naiReq.Parameters.Height, 1)
				}

				// 无论成败都要递减：这张已经不再占用后面人的等待时间了。
				if entry != nil {
					queue.markProgress(entry.Ticket(), count-index-1)
				}
				if onImageDone != nil {
					onImageDone(index+1, count)
				}
			}

			merged := make([]map[string]interface{}, 0, count)
			for _, item := range ordered {
				merged = append(merged, item...)
			}
			if len(merged) == 0 {
				if firstErr != nil {
					return nil, firstErr
				}
				return nil, errors.New("NovelAI 响应中未找到有效图片")
			}
			return merged, nil
		})
	if err != nil {
		return nil, 0, err
	}
	if firstErr != nil {
		log.Printf("NovelAI batch completed with partial failures: requested=%d succeeded=%d", count, succeededCount)
	}
	return merged, succeededCount, nil
}

// withNovelAIFreeGenerationLock 在免费生图锁下串行执行 fn。
// 现在是 withNovelAIQueue 的薄封装（images=1、无用户标识、不回调进度），
// 保留该签名是为了不动现有调用点与验证串行语义的既有测试。
func withNovelAIFreeGenerationLock(ctx context.Context, channel model.ModelChannel, fn func() ([]map[string]interface{}, error)) ([]map[string]interface{}, error) {
	return withNovelAIQueue(ctx, channel, "", "", 1, nil, func(*novelAIQueueEntry) ([]map[string]interface{}, error) {
		return fn()
	})
}

// novelAIQueueUpdateInterval 是等锁期间回调排队状态的周期。
// Phase 2 的 SSE 心跳会用它：只要每 2 秒往下游写一次事件，
// 就能在 Cloudflare 的 100s 响应头超时之前先把响应头发出去，从根上消灭 524。
const novelAIQueueUpdateInterval = 2 * time.Second

// withNovelAIQueue 在渠道级 FIFO 队列中串行执行 fn，并可周期性汇报排队状态。
//
// userID 用于单用户配额统计（空串表示不限制该维度）；images 是本次要生成的张数，
// 用于计算「前方还有多少张图」；onQueueUpdate 非 nil 时会在等锁期间被周期调用，
// 参数为当前前方待生成张数与预估等待秒数 —— Phase 2 的 SSE 推送就挂在这里。
//
// ⚠️ naiModel 必须是**已解析的 NovelAI 模型 ID**（如 nai-diffusion-4-5-full），
// 即 recordNovelAIDuration 记录 EWMA 样本时用的同一个 key。曾经这里误传 channel.Name
// （渠道名，如「NovelAI官方」），与记录侧的模型 ID 永不匹配，导致 EWMA 样本永远查不到、
// 预估恒定回落冷启动值：V3 用户被告知等 12s 实际只需 2s。改动时务必保持两侧 key 一致。
//
// ⚠️ dequeue 与 release 必须 defer：ctx 取消路径同样要清理干净。
// 漏掉任何一个都会导致锁泄漏或队列条目残留，后续所有请求会永久卡死
// （历史上就是这类泄漏造成「之后每次都 502，只能删控件」）。
func withNovelAIQueue(
	ctx context.Context,
	channel model.ModelChannel,
	naiModel string,
	userID string,
	images int,
	onQueueUpdate func(imagesAhead int, estimatedSeconds int),
	fn func(entry *novelAIQueueEntry) ([]map[string]interface{}, error),
) ([]map[string]interface{}, error) {
	// 未启用免费生图锁的渠道是付费并发模式，不需要排队。
	if channel.FreeGenerationLock == nil || !channel.FreeGenerationLock.Enabled {
		return fn(nil)
	}

	if images <= 0 {
		images = 1
	}

	queue := novelAIQueueFor(channel)
	limits := novelAIQueueLimitsFrom(channel.FreeGenerationLock)

	entry, err := queue.enqueue(userID, images, limits)
	if err != nil {
		return nil, err
	}
	// 先 defer dequeue：下面任何一条 return 路径（含 ctx 取消）都要注销票号。
	defer queue.dequeue(entry.ticket)

	if onQueueUpdate != nil {
		// 立即汇报一次，让调用方在开始等待前就能拿到初始排队位置。
		ahead := queue.imagesAhead(entry.ticket)
		onQueueUpdate(ahead, estimateNovelAISeconds(naiModel, ahead+images, limits.EstimatedSecondsPerImage))

		// 周期汇报只在等锁期间存活，通过 stop 保证 acquire 返回后立刻退出，
		// 不会泄漏 goroutine，也不会在 fn 执行期间继续推送排队事件。
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			ticker := time.NewTicker(novelAIQueueUpdateInterval)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					ahead := queue.imagesAhead(entry.ticket)
					onQueueUpdate(ahead, estimateNovelAISeconds(naiModel, ahead+images, limits.EstimatedSecondsPerImage))
				}
			}
		}()
	}

	waitStart := time.Now()
	imagesAheadAtEntry := queue.imagesAhead(entry.ticket)
	if err := queue.acquire(ctx); err != nil {
		log.Printf(
			"NovelAI queue canceled while waiting: ticket=%d wait=%.1fs imagesAhead=%d queued=%d",
			entry.ticket, time.Since(waitStart).Seconds(), imagesAheadAtEntry, queue.queuedImages(),
		)
		return nil, errors.New("请求已取消")
	}
	defer queue.release()

	waitSeconds := time.Since(waitStart).Seconds()

	// 拿到锁后再确认一次：可能是在排队期间断开的。
	if ctx.Err() != nil {
		log.Printf("NovelAI queue canceled after acquire: ticket=%d wait=%.1fs", entry.ticket, waitSeconds)
		return nil, errors.New("请求已取消")
	}

	if imagesAheadAtEntry > 0 || waitSeconds >= 1 {
		log.Printf(
			"NovelAI queue acquired: ticket=%d wait=%.1fs imagesAhead=%d images=%d queued=%d",
			entry.ticket, waitSeconds, imagesAheadAtEntry, images, queue.queuedImages(),
		)
	}

	return fn(entry)
}

func novelAIFreeGenerationLockKey(channel model.ModelChannel) string {
	baseURL := strings.TrimRight(strings.TrimSpace(channel.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://image.novelai.net"
	}
	sum := sha256.Sum256([]byte(baseURL + "\x00" + channel.APIKey))
	return hex.EncodeToString(sum[:])
}

// novelAIHTTPClient 专用客户端。
// Timeout 是**单张出图**的预算，不含排队等待：V4.5 Full 实测约 12s，120s 已有
// 10 倍余量；排队时长由队列机制（withNovelAIQueue）管控，不靠这个超时兜。
// 原来设 5 分钟远超 Cloudflare 的 100s 预算，只会造成「CF 已断开、后端还在跑、
// Anlas 已扣但用户拿不到图」的纯白烧，因此必须收敛到单张量级。
// 另外绝不能用 http.DefaultClient（零超时）：上游卡住时 goroutine 会永久持有
// 串行锁，把后续所有请求一起拖死。
var novelAIHTTPClient = &http.Client{Timeout: 120 * time.Second}

// requestNovelAIImageData 排队并向上游发起一次单张生图请求。
// ctx 来自客户端请求，反代超时/用户取消时会被取消，从而尽快释放免费生图锁，
// 避免"前端已 502、后端还在跑"导致后续请求排队雪崩。
//
// 注意：本函数会自己占一个队列名额。批量出图（requestNovelAISingleImageBatch）
// 已经在外层持有名额，绝不能再调用它 —— 那会嵌套排队自我死锁。批量请走
// doNovelAIUpstreamRequest（不排队版）。
func requestNovelAIImageData(ctx context.Context, channel model.ModelChannel, naiReq *novelAIRequest) ([]map[string]interface{}, error) {
	// enqueuedAt 在排队之前打点，这样 wait 才是真实的「等锁耗时」。
	enqueuedAt := time.Now()
	return withNovelAIQueue(ctx, channel, naiReq.Model, "", 1, nil, func(entry *novelAIQueueEntry) ([]map[string]interface{}, error) {
		// 排查 524/502 时最关键的两个数字：等锁多久、上游出图多久。
		// 二者混在一起看不出到底是队列太长还是上游变慢，所以必须分开统计。
		waitSeconds := time.Since(enqueuedAt).Seconds()
		imagesAhead := 0
		if entry != nil {
			imagesAhead = novelAIQueueFor(channel).imagesAhead(entry.Ticket())
		}

		data, upstreamDuration, err := doNovelAIUpstreamRequest(ctx, channel, naiReq)
		if err != nil {
			log.Printf(
				"NovelAI request failed: model=%s wait=%.1fs upstream=%.1fs imagesAhead=%d err=%v",
				naiReq.Model, waitSeconds, upstreamDuration.Seconds(), imagesAhead, err,
			)
			return nil, err
		}

		log.Printf(
			"NovelAI request done: model=%s wait=%.1fs upstream=%.1fs imagesAhead=%d images=%d",
			naiReq.Model, waitSeconds, upstreamDuration.Seconds(), imagesAhead, len(data),
		)
		// 出图成功才扣 V5 配额预测；非 V5 模型在函数内部短路，不受影响。
		consumeNovelAIV5Quota(channel, naiReq.Model, naiReq.Parameters.Width, naiReq.Parameters.Height, 1)
		return data, nil
	})
}

// doNovelAIUpstreamRequest 只负责「打一次上游、解析出图片」，**不排队**。
//
// 拆出这一层是为了让批量出图能在「持有单个队列名额」的前提下串行出多张：
// 若批量复用 requestNovelAIImageData，会在已持有名额时再次入队，直接自我死锁。
//
// 返回的 duration 是本次上游耗时，调用方用它喂 EWMA / 打日志。
// 只在成功出图时喂 EWMA 样本：失败/取消的耗时（秒回的 500、瞬断的客户端）
// 混进平均值会让预估严重偏低，用户看到"还剩 3 秒"却等了半分钟，比不显示更糟。
func doNovelAIUpstreamRequest(ctx context.Context, channel model.ModelChannel, naiReq *novelAIRequest) ([]map[string]interface{}, time.Duration, error) {
	naiBody, err := json.Marshal(naiReq)
	if err != nil {
		return nil, 0, errors.New("构建 NovelAI 请求失败")
	}

	if novelAIDebugEnabled() {
		// 打印完整请求体，方便和官方网页端 / Aaalice_NAI_Launcher 的请求逐字段对照。
		// Token 在 Authorization 头里，不会出现在这段日志中。
		log.Printf("[novelai-debug] request body=%s", string(naiBody))
	}

	naiURL := buildNovelAIURL(channel.BaseURL, "/ai/generate-image")
	upstreamStart := time.Now()

	// 请求在锁内构建：等锁期间若客户端已断开，就不必再打上游。
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, naiURL, bytes.NewReader(naiBody))
	if err != nil {
		return nil, time.Since(upstreamStart), errors.New("创建 NovelAI 请求失败")
	}
	request.Header.Set("Authorization", "Bearer "+channel.APIKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := novelAIHTTPClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("NovelAI request canceled by client: model=%s url=%s", naiReq.Model, naiURL)
			return nil, time.Since(upstreamStart), errNovelAIRequestCanceled
		}
		log.Printf("NovelAI upstream transport error: model=%s url=%s err=%v", naiReq.Model, naiURL, err)
		return nil, time.Since(upstreamStart), errors.New("NovelAI 接口请求失败")
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		log.Printf(
			"NovelAI upstream error: model=%s status=%d upstream=%.1fs body=%s",
			naiReq.Model, response.StatusCode, time.Since(upstreamStart).Seconds(), string(body),
		)
		return nil, time.Since(upstreamStart), errors.New(readNovelAIError(response.StatusCode, body))
	}

	zipData, err := io.ReadAll(response.Body)
	if err != nil {
		log.Printf("NovelAI response read failed: model=%s err=%v", naiReq.Model, err)
		return nil, time.Since(upstreamStart), errors.New("读取 NovelAI 响应失败")
	}
	data, err := extractNovelAIImageData(zipData)
	if err != nil {
		log.Printf("NovelAI response conversion failed: model=%s err=%v", naiReq.Model, err)
		return nil, time.Since(upstreamStart), fmt.Errorf("NovelAI 响应转换失败: %w", err)
	}

	upstreamDuration := time.Since(upstreamStart)
	recordNovelAIDuration(naiReq.Model, upstreamDuration)
	return data, upstreamDuration, nil
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
