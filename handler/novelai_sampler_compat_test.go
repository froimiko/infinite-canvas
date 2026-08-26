package handler

// 采样器与模型兼容性的回归测试。
//
// 背景（2026-08-26 线上故障）：用户在 V4.5 上选了 ddim_v3，连续 4 次请求全部拿到
// 上游 500：
//
//	NovelAI upstream error: model=nai-diffusion-4-5-full status=500
//	body={"statusCode":500,"message":"Internal Server Error"}
//
// 当时因为「我自己用 WiFi/流量都正常」而误判成用户网络问题。实际上请求早就打到
// 上游并拿到了响应（日志里 upstream=2.7s），网络不通根本走不到那一步 ——
// 真正原因是 DDIM 在 V4 起就不被支持，而我们把它原样透传了。
//
// 这组测试的作用：任何人把 mapNovelAISamplerForModel 删掉或改错，都会立刻红。

import (
	"strings"
	"testing"
)

// TestMapNovelAISamplerForModelRejectsDDIMOnV4Plus 是核心回归：
// DDIM 在 V4 / V4.5 / V5 上必须被换成 k_euler_ancestral。
func TestMapNovelAISamplerForModelRejectsDDIMOnV4Plus(t *testing.T) {
	v4PlusModels := []string{
		"nai-diffusion-4-full",
		"nai-diffusion-4-curated-preview",
		"nai-diffusion-4-5-full",
		"nai-diffusion-4-5-curated",
		"nai-diffusion-5-full",
		"nai-diffusion-5-curated",
	}
	for _, naiModel := range v4PlusModels {
		for _, sampler := range []string{"ddim", "ddim_v3"} {
			got := mapNovelAISamplerForModel(sampler, naiModel)
			if got != "k_euler_ancestral" {
				t.Errorf(
					"mapNovelAISamplerForModel(%q, %q) = %q, want k_euler_ancestral "+
						"(DDIM on V4+ makes NovelAI return HTTP 500)",
					sampler, naiModel, got,
				)
			}
		}
	}
}

// TestMapNovelAISamplerForModelMapsDDIMOnV3 验证 V3 走专用变体而非回退。
func TestMapNovelAISamplerForModelMapsDDIMOnV3(t *testing.T) {
	if got := mapNovelAISamplerForModel("ddim", "nai-diffusion-3"); got != "ddim_v3" {
		t.Errorf(`mapNovelAISamplerForModel("ddim", "nai-diffusion-3") = %q, want ddim_v3`, got)
	}
	// 已经是 ddim_v3 的保持原样。
	if got := mapNovelAISamplerForModel("ddim_v3", "nai-diffusion-3"); got != "ddim_v3" {
		t.Errorf(`ddim_v3 on V3 = %q, want ddim_v3`, got)
	}
}

// TestMapNovelAISamplerForModelLeavesNonDDIMUntouched 确保没有波及其他采样器。
// 这一条防的是「过度修复」：只有 DDIM 需要特殊处理，别的必须原样透传。
func TestMapNovelAISamplerForModelLeavesNonDDIMUntouched(t *testing.T) {
	samplers := []string{"k_euler", "k_euler_ancestral", "k_dpmpp_2s_ancestral", "k_dpmpp_2m", "k_dpmpp_sde"}
	models := []string{"nai-diffusion-3", "nai-diffusion-4-5-full", "nai-diffusion-5-full"}
	for _, sampler := range samplers {
		for _, naiModel := range models {
			if got := mapNovelAISamplerForModel(sampler, naiModel); got != sampler {
				t.Errorf("mapNovelAISamplerForModel(%q, %q) = %q, want unchanged", sampler, naiModel, got)
			}
		}
	}
}

// TestConvertToNovelAIRequestNeverSendsDDIMToV4Plus 是端到端版本：
// 直接构造「前端选了 ddim_v3 + V4.5」的请求，确认发给上游的请求体里没有 ddim。
//
// 单测 mapNovelAISamplerForModel 正确不代表接进去了 —— 这条覆盖「忘记调用」的情形。
func TestConvertToNovelAIRequestNeverSendsDDIMToV4Plus(t *testing.T) {
	for _, naiModel := range []string{"nai-diffusion-4-full", "nai-diffusion-4-5-full", "nai-diffusion-5-curated"} {
		req := mustConvertToNovelAIRequest(t, openAIImageRequest{
			Prompt:         "1girl, solo",
			Size:           "832x1216",
			N:              1,
			NovelAIEnabled: true,
			NovelAIModel:   naiModel,
			Sampler:        "ddim_v3", // 前端下拉曾允许选到这个组合
		})
		if strings.Contains(req.Parameters.Sampler, "ddim") {
			t.Errorf("model %s: sampler = %q, DDIM must never reach a V4+ model", naiModel, req.Parameters.Sampler)
		}
		if req.Parameters.Sampler != "k_euler_ancestral" {
			t.Errorf("model %s: sampler = %q, want k_euler_ancestral", naiModel, req.Parameters.Sampler)
		}
	}
}

// TestConvertToNovelAIRequestDDIMDisablesSMEAOnV3 覆盖 V3 + DDIM 必须关 SMEA。
//
// 参考实现 image_params.dart:324-346 在 sampler 含 ddim 时强制 effectiveSmea=false。
// V4+ 本就不发 SMEA，所以这条只在 V3 上可观测。
func TestConvertToNovelAIRequestDDIMDisablesSMEAOnV3(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:         "1girl",
		Size:           "832x1216",
		N:              1,
		NovelAIEnabled: true,
		NovelAIModel:   "nai-diffusion-3",
		Sampler:        "ddim_v3",
		SM:             boolPtr(true), // 用户显式打开 SMEA
		SMDyn:          boolPtr(true),
	})
	if req.Parameters.Sampler != "ddim_v3" {
		t.Fatalf("V3 sampler = %q, want ddim_v3", req.Parameters.Sampler)
	}
	if req.Parameters.SM == nil || *req.Parameters.SM {
		t.Error("DDIM must force sm=false (DDIM and SMEA are mutually exclusive)")
	}
	if req.Parameters.SMDyn == nil || *req.Parameters.SMDyn {
		t.Error("DDIM must force sm_dyn=false")
	}
}

// TestConvertToNovelAIRequestKeepsSMEAWithoutDDIM 反向确认：
// 非 DDIM 采样器下，V3 的 SMEA 设置要被尊重，别被上面的修复顺手关掉。
func TestConvertToNovelAIRequestKeepsSMEAWithoutDDIM(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:         "1girl",
		Size:           "832x1216",
		N:              1,
		NovelAIEnabled: true,
		NovelAIModel:   "nai-diffusion-3",
		Sampler:        "k_euler_ancestral",
		SM:             boolPtr(true),
	})
	if req.Parameters.SM == nil || !*req.Parameters.SM {
		t.Error("non-DDIM sampler on V3 must keep the user's sm=true")
	}
}
