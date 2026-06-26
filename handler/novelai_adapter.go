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
	"strings"
	"sync"
	"time"

	"github.com/basketikun/infinite-canvas/model"
	"github.com/basketikun/infinite-canvas/service"
)

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
	V4Prompt         *v4PromptStructure `json:"v4_prompt,omitempty"`
	V4NegativePrompt *v4PromptStructure `json:"v4_negative_prompt,omitempty"`
	CharacterPrompts []novelAICharacterPromptCompat `json:"characterPrompts,omitempty"`

	// 固定参数（保持兼容性）
	NoiseSchedule               string  `json:"noise_schedule"`
	SM                          *bool   `json:"sm,omitempty"`
	SMDyn                       *bool   `json:"sm_dyn,omitempty"`
	DynamicThresholding         bool    `json:"dynamic_thresholding"`
	ControlnetStrength          float64 `json:"controlnet_strength"`
	Legacy                      bool    `json:"legacy"`
	AddOriginalImage            bool    `json:"add_original_image"`
	DeliberateEulerAncestralBug bool    `json:"deliberate_euler_ancestral_bug"`
	PreferBrownian              bool    `json:"prefer_brownian"`

	// img2img 参数（Phase 3）
	Image    string  `json:"image,omitempty"`
	Strength float64 `json:"strength,omitempty"`
	Noise    float64 `json:"noise,omitempty"`
}

// V4 Prompt 结构（用于多角色控制）
type v4PromptStructure struct {
	Caption   v4Caption `json:"caption"`
	UseCoords bool      `json:"use_coords"`
	UseOrder  bool      `json:"use_order"`
	LegacyUC  bool      `json:"legacy_uc,omitempty"` // 仅用于 negative_prompt
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
	ucPreset := 4 // 旧逻辑保持既有 None/关闭预设行为。
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
		sampler = normalizeNovelAISampler(openAI.Sampler, "k_euler")
		seed = normalizeNovelAISeed(openAI.Seed)
		ucPreset = normalizeNovelAIUCPreset(openAI.UCPreset, 3) // 前端默认 Heavy；当前推荐映射 Heavy=3。
		cfgRescale = normalizeNovelAICfgRescale(openAI.CfgRescale, 0.18)
		noiseSchedule = normalizeNovelAINoiseSchedule(openAI.NoiseSchedule, "native")
		sm = boolPtr(normalizeBool(openAI.SM, true))
		smDyn = boolPtr(normalizeBool(openAI.SMDyn, true))
		dynamicThresholding = normalizeBool(openAI.DynamicThresholding, true)
		if normalizeBool(openAI.VarietyPlus, false) {
			skipCfgAboveSigma = calculateSkipCfgAboveSigma(model, width, height)
		}
	}

	isV4Model := isNovelAIV4Model(model)
	if openAI.NovelAIEnabled && isV4Model {
		// NAI4 会拒绝/忽略 SMEA，参考实现直接删除 sm/sm_dyn；这里用 omitempty 指针避免发送。
		sm = nil
		smDyn = nil
		aqtPreset = normalizeNovelAIAqtPreset(openAI.AqtPreset, "safe")
		if noiseSchedule == "native" {
			noiseSchedule = "karras"
		}
	}

	// 准备 quality tags（V4 模型会自动添加）
	qualityTags := ""
	if isV4Model {
		qualityTags = "very aesthetic, masterpiece, no text"
	} else {
		qualityTags = "masterpiece, best quality"
	}

	// 构建完整 input（提示词 + quality tags）
	fullPrompt := openAI.Prompt
	if qualityTags != "" {
		fullPrompt = openAI.Prompt + ", " + qualityTags
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
			QualityToggle:     true,
			SkipCfgAboveSigma: skipCfgAboveSigma,
			CfgRescale:        cfgRescale,
			AqtPreset:         aqtPreset,

			// 固定参数（NovelAI RequestParameters 支持字段）
			NoiseSchedule:               noiseSchedule,
			SM:                          sm,
			SMDyn:                       smDyn,
			DynamicThresholding:         dynamicThresholding,
			ControlnetStrength:          1.0,
			Legacy:                      false,
			AddOriginalImage:            true,
			DeliberateEulerAncestralBug: false,
			PreferBrownian:              true,
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

		v4BaseCaption := qualityTags
		v4CharCaptions := []v4CharCaption{
			{
				CharCaption: openAI.Prompt,
				Centers: []v4Position{
					{X: 0.5, Y: 0.5}, // 居中
				},
			},
		}
		v4NegativeCharCaptions := []v4CharCaption{
			{
				CharCaption: "",
				Centers: []v4Position{
					{X: 0.5, Y: 0.5},
				},
			},
		}
		if useRolePrompts {
			v4BaseCaption = openAI.Prompt
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

		naiReq.Parameters.V4NegativePrompt = &v4PromptStructure{
			Caption: v4Caption{
				BaseCaption:  negativePrompt,
				CharCaptions: v4NegativeCharCaptions,
			},
			UseCoords: useCoords,
			UseOrder:  true,
			LegacyUC:  false,
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

// 对齐到 64 的倍数（向下取整）
func alignTo64(value int) int {
	if value < 64 {
		return 64
	}
	return (value / 64) * 64
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

// 解析 NovelAI 模型名（兼容简写）
func resolveNovelAIModel(modelName string) string {
	modelName = strings.ToLower(strings.TrimSpace(modelName))

	switch modelName {
	case "nai-diffusion-4-5-full", "nai-diffusion-4-5-curated", "nai-diffusion-4-full", "nai-diffusion-4-curated-preview", "nai-diffusion-3", "nai-diffusion-2", "nai-diffusion-furry":
		return modelName
	}

	// V4.5 模型（最新）
	if strings.Contains(modelName, "4.5") || strings.Contains(modelName, "v4.5") || strings.Contains(modelName, "4-5") {
		return "nai-diffusion-4-5-full"
	}

	// V4 模型
	if strings.Contains(modelName, "nai-diffusion-4") || strings.Contains(modelName, "v4") {
		return "nai-diffusion-4-curated-preview"
	}

	// V3 模型
	if strings.Contains(modelName, "nai-diffusion-3") || strings.Contains(modelName, "v3") {
		return "nai-diffusion-3"
	}

	// V2 模型
	if strings.Contains(modelName, "nai-diffusion-2") || strings.Contains(modelName, "v2") {
		return "nai-diffusion-2"
	}

	// Furry 模型
	if strings.Contains(modelName, "furry") {
		return "nai-diffusion-furry"
	}

	// 默认使用最新的 V3 模型（更稳定，V4 需要订阅）
	return "nai-diffusion-3"
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

func normalizeNovelAIUCPreset(value string, fallback int) int {
	// 前端 UC Preset 推荐映射：None=0, Light=2, Heavy=3, Human Focus=1。
	// NovelAI 历史实现里 None 也曾使用 4；这里仅在 novelai_enabled=true 时按前端显式参数映射。
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "none":
		return 0
	case "light":
		return 2
	case "heavy":
		return 3
	case "human focus", "human_focus", "human-focus":
		return 1
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

func calculateSkipCfgAboveSigma(model string, width, height int) *float64 {
	w := float64(width) / 8
	h := float64(height) / 8
	v := math.Sqrt(4.0 * w * h / 63232)
	var value float64
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "nai-diffusion-4-full":
		value = 19.0 * v
	case "nai-diffusion-4-5-curated":
		value = 59.0 * v
	default:
		value = 19.0 * v
	}
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

	// 查找所有图片文件
	data := make([]map[string]interface{}, 0, len(zipReader.File))
	for _, file := range zipReader.File {
		// 跳过目录和非图片文件
		if file.FileInfo().IsDir() || !isImageFile(file.Name) {
			continue
		}

		// 读取图片内容
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
