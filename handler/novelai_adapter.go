package handler

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/basketikun/infinite-canvas/model"
	"github.com/basketikun/infinite-canvas/service"
)

// NovelAI API 请求结构
type novelAIRequest struct {
	Input      string            `json:"input"`
	Model      string            `json:"model"`
	Action     string            `json:"action"`
	Parameters novelAIParameters `json:"parameters"`
}

type novelAIParameters struct {
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Scale          float64 `json:"scale"`
	Sampler        string  `json:"sampler"`
	Steps          int     `json:"steps"`
	NSamples       int     `json:"n_samples"`
	Seed           int64   `json:"seed"`
	NegativePrompt string  `json:"negative_prompt"`
	UCPreset       int     `json:"ucPreset"`
	QualityToggle  bool    `json:"qualityToggle"`
	// img2img 参数（Phase 3）
	Image    string  `json:"image,omitempty"`
	Strength float64 `json:"strength,omitempty"`
	Noise    float64 `json:"noise,omitempty"`
}

// OpenAI 兼容请求结构（简化版）
type openAIImageRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n"`
	Size           string `json:"size"`
	Quality        string `json:"quality"`
	ResponseFormat string `json:"response_format"`
}

// 转换 OpenAI 请求为 NovelAI 请求
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

	// 映射质量参数
	steps, scale := mapQualityToNovelAI(openAI.Quality, width, height)

	// 构建 NovelAI 请求
	naiReq := &novelAIRequest{
		Input:  openAI.Prompt,
		Model:  resolveNovelAIModel(openAI.Model),
		Action: "generate", // 基础文生图
		Parameters: novelAIParameters{
			Width:          width,
			Height:         height,
			Scale:          scale,
			Sampler:        "k_euler", // 默认采样器
			Steps:          steps,
			NSamples:       1, // 强制单张（项目已是分开请求）
			Seed:           0, // 随机种子
			NegativePrompt: "lowres, bad anatomy, bad hands, text, error, missing fingers",
			UCPreset:       0,
			QualityToggle:  true,
		},
	}

	return naiReq, nil
}

// 解析 OpenAI 尺寸格式 "1024x1024" → width, height
func parseOpenAISize(size string) (int, int, error) {
	if size == "" || strings.ToLower(size) == "auto" {
		return 1024, 1024, nil // 默认尺寸
	}

	var width, height int
	if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil {
		return 0, 0, fmt.Errorf("无效的尺寸格式: %s（应为 1024x1024）", size)
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

	// 简写映射
	if strings.Contains(modelName, "nai-diffusion-3") || strings.Contains(modelName, "v3") {
		return "nai-diffusion-3"
	}
	if strings.Contains(modelName, "nai-diffusion-2") || strings.Contains(modelName, "v2") {
		return "nai-diffusion-2"
	}
	if strings.Contains(modelName, "furry") {
		return "nai-diffusion-furry"
	}

	// 默认使用最新模型
	return "nai-diffusion-3"
}

// 转换 NovelAI ZIP 响应为 OpenAI JSON 格式
func convertNovelAIResponse(zipData []byte) ([]byte, error) {
	// 读取 ZIP 文件
	zipReader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("解压 NovelAI 响应失败: %w", err)
	}

	// 查找第一个图片文件
	var imageData []byte
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
		imageData, err = io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		break // 只取第一张图
	}

	if len(imageData) == 0 {
		return nil, errors.New("NovelAI 响应中未找到有效图片")
	}

	// 转为 base64
	base64Image := base64.StdEncoding.EncodeToString(imageData)

	// 构建 OpenAI 兼容响应
	response := map[string]interface{}{
		"data": []map[string]interface{}{
			{
				"b64_json": base64Image,
			},
		},
	}

	return json.Marshal(response)
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
func proxyNovelAIImageRequest(w http.ResponseWriter, r *http.Request, body []byte, channel model.ModelChannel, user model.User, credits int) {
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

	// 1. 转换请求格式
	naiReq, err := convertToNovelAIRequest(body)
	if err != nil {
		log.Printf("NovelAI request conversion failed: %v", err)
		Fail(w, fmt.Sprintf("请求格式转换失败: %v", err))
		return
	}

	// 2. 构建 NovelAI API 请求
	naiBody, err := json.Marshal(naiReq)
	if err != nil {
		Fail(w, "构建 NovelAI 请求失败")
		return
	}

	// NovelAI 图像生成端点
	naiURL := buildNovelAIURL(channel.BaseURL, "/ai/generate-image")

	request, err := http.NewRequest(http.MethodPost, naiURL, bytes.NewReader(naiBody))
	if err != nil {
		Fail(w, "创建 NovelAI 请求失败")
		return
	}

	// 3. 设置请求头
	request.Header.Set("Authorization", "Bearer "+channel.APIKey)
	request.Header.Set("Content-Type", "application/json")

	// 4. 扣费
	if err := service.ConsumeUserCredits(user.ID, naiReq.Model, credits, "/images/generations"); err != nil {
		FailError(w, err)
		return
	}

	// 5. 发送请求
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		log.Printf("NovelAI request failed: url=%s err=%v", naiURL, err)
		if err := service.RefundUserCredits(user.ID, naiReq.Model, credits, "/images/generations"); err != nil {
			log.Printf("Refund failed: %v", err)
		}
		Fail(w, "NovelAI 接口请求失败")
		return
	}
	defer response.Body.Close()

	// 6. 检查响应状态
	if response.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		log.Printf("NovelAI upstream error: status=%d body=%s", response.StatusCode, string(body))
		if err := service.RefundUserCredits(user.ID, naiReq.Model, credits, "/images/generations"); err != nil {
			log.Printf("Refund failed: %v", err)
		}
		Fail(w, readNovelAIError(response.StatusCode, body))
		return
	}

	// 7. 读取 ZIP 响应
	zipData, err := io.ReadAll(response.Body)
	if err != nil {
		log.Printf("NovelAI response read failed: %v", err)
		if err := service.RefundUserCredits(user.ID, naiReq.Model, credits, "/images/generations"); err != nil {
			log.Printf("Refund failed: %v", err)
		}
		Fail(w, "读取 NovelAI 响应失败")
		return
	}

	// 8. 转换为 OpenAI JSON 格式
	jsonResponse, err := convertNovelAIResponse(zipData)
	if err != nil {
		log.Printf("NovelAI response conversion failed: %v", err)
		if err := service.RefundUserCredits(user.ID, naiReq.Model, credits, "/images/generations"); err != nil {
			log.Printf("Refund failed: %v", err)
		}
		Fail(w, fmt.Sprintf("NovelAI 响应转换失败: %v", err))
		return
	}

	// 9. 返回响应
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jsonResponse)
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

	// 1. 强制单张生成
	if lock.ForceCountOne && req.N > 1 {
		return fmt.Errorf("该渠道已启用免费生图锁，仅支持单次生成 1 张图片（n=%d 不符合要求，需 n=1）", req.N)
	}

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

	// 4. 限制步数（从 quality 推断）
	steps, _ := mapQualityToNovelAI(req.Quality, width, height)
	if steps > lock.MaxSteps {
		return fmt.Errorf(
			"该渠道已启用免费生图锁（NovelAI Opus 无限免费生图模式）\n"+
				"当前步数: %d（从 quality=%s 推断）\n"+
				"限制步数: ≤%d\n\n"+
				"建议：使用默认质量参数或调整为 standard/low",
			steps, req.Quality, lock.MaxSteps,
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
