package handler

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// NovelAI 官方 UC Preset 整数取值，与 Aaalice_NAI_Launcher 的 UcPresets 一致。
func TestNormalizeNovelAIUCPresetMatchesOfficialValues(t *testing.T) {
	tests := []struct {
		input  string
		expect int
	}{
		{"Heavy", 0},
		{"heavy", 0},
		{"Light", 1},
		{"light", 1},
		{"Human Focus", 2},
		{"human_focus", 2},
		{"human-focus", 2},
		{"None", 3},
		{"none", 3},
	}
	for _, tt := range tests {
		if got := normalizeNovelAIUCPreset(tt.input, -1); got != tt.expect {
			t.Errorf("normalizeNovelAIUCPreset(%q) = %d, want %d", tt.input, got, tt.expect)
		}
	}

	if got := normalizeNovelAIUCPreset("unknown", 7); got != 7 {
		t.Errorf("unknown preset should fall back, got %d", got)
	}
}

// Variety+ 的 skip_cfg_above_sigma 系数必须统一为 58.0，不按模型区分。
func TestCalculateSkipCfgAboveSigmaUses58Coefficient(t *testing.T) {
	tests := [][2]int{
		{832, 1216},
		{1024, 1024},
		{1216, 832},
	}
	for _, size := range tests {
		width, height := size[0], size[1]
		got := calculateSkipCfgAboveSigma(width, height)
		if got == nil {
			t.Fatalf("skip_cfg_above_sigma should not be nil for %dx%d", width, height)
		}
		want := 58.0 * math.Sqrt(4.0*(float64(width)/8)*(float64(height)/8)/63232)
		if math.Abs(*got-want) > 1e-9 {
			t.Errorf("skip_cfg_above_sigma(%dx%d) = %v, want %v", width, height, *got, want)
		}
	}
}

// v4_negative_prompt 只能携带 caption + legacy_uc，多发 use_coords/use_order 会改变上游解析分支。
func TestConvertToNovelAIRequestV4NegativePromptShapeMatchesReference(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:         "1girl, solo",
		NegativePrompt: "bad hands",
		Size:           "832x1216",
		NovelAIEnabled: true,
		NovelAIModel:   "nai-diffusion-4-5-full",
	})

	body, err := json.Marshal(req.Parameters.V4NegativePrompt)
	if err != nil {
		t.Fatalf("marshal v4_negative_prompt: %v", err)
	}
	payload := string(body)
	for _, forbidden := range []string{"use_coords", "use_order"} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("v4_negative_prompt should not contain %s: %s", forbidden, payload)
		}
	}
	if !strings.Contains(payload, "\"legacy_uc\"") {
		t.Fatalf("v4_negative_prompt should contain legacy_uc: %s", payload)
	}
	if !strings.Contains(payload, "\"base_caption\"") {
		t.Fatalf("v4_negative_prompt should contain base_caption: %s", payload)
	}
}

// v4_prompt 仍需保留 use_coords / use_order。
func TestConvertToNovelAIRequestV4PromptKeepsCoordFlags(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:         "1girl, solo",
		NegativePrompt: "bad hands",
		Size:           "832x1216",
		NovelAIEnabled: true,
		NovelAIModel:   "nai-diffusion-4-5-full",
	})

	body, err := json.Marshal(req.Parameters.V4Prompt)
	if err != nil {
		t.Fatalf("marshal v4_prompt: %v", err)
	}
	payload := string(body)
	for _, required := range []string{"\"use_coords\"", "\"use_order\"", "\"base_caption\""} {
		if !strings.Contains(payload, required) {
			t.Fatalf("v4_prompt should contain %s: %s", required, payload)
		}
	}
}

// 参考实现在所有请求里都发送 autoSmea 与 normalize_reference_strength_multiple。
func TestConvertToNovelAIRequestAlwaysSendsBaseCompatFields(t *testing.T) {
	for _, model := range []string{"nai-diffusion-3", "nai-diffusion-4-5-full"} {
		req := mustConvertToNovelAIRequest(t, openAIImageRequest{
			Prompt:         "1girl, solo",
			NegativePrompt: "bad hands",
			Size:           "832x1216",
			NovelAIEnabled: true,
			NovelAIModel:   model,
		})
		body, err := json.Marshal(req.Parameters)
		if err != nil {
			t.Fatalf("marshal parameters: %v", err)
		}
		payload := string(body)
		for _, required := range []string{"\"autoSmea\"", "\"normalize_reference_strength_multiple\""} {
			if !strings.Contains(payload, required) {
				t.Fatalf("%s payload should contain %s: %s", model, required, payload)
			}
		}
		if req.Parameters.AutoSmea {
			t.Fatalf("%s autoSmea should be false", model)
		}
		if !req.Parameters.NormalizeReferenceStrengthMultiple {
			t.Fatalf("%s normalize_reference_strength_multiple should be true", model)
		}
	}
}

// V4 分支需要额外写入顶层 use_coords / legacy_v3_extend；V3 不发送这两个字段。
func TestConvertToNovelAIRequestTopLevelV4OnlyFlags(t *testing.T) {
	v4 := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:         "1girl, solo",
		NegativePrompt: "bad hands",
		Size:           "832x1216",
		NovelAIEnabled: true,
		NovelAIModel:   "nai-diffusion-4-5-full",
	})
	if v4.Parameters.UseCoords == nil || v4.Parameters.LegacyV3Extend == nil {
		t.Fatal("V4 request should set top-level use_coords and legacy_v3_extend")
	}
	if *v4.Parameters.LegacyV3Extend {
		t.Fatal("legacy_v3_extend should be false")
	}
	if v4.Parameters.UC != "" {
		t.Fatalf("V4 request should not send uc, got %q", v4.Parameters.UC)
	}

	v3 := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:         "1girl, solo",
		NegativePrompt: "bad hands",
		Size:           "832x1216",
		NovelAIEnabled: true,
		NovelAIModel:   "nai-diffusion-3",
	})
	if v3.Parameters.UseCoords != nil || v3.Parameters.LegacyV3Extend != nil {
		t.Fatal("V3 request should omit top-level use_coords and legacy_v3_extend")
	}
	if v3.Parameters.UC != v3.Parameters.NegativePrompt {
		t.Fatalf("V3 uc = %q, want same as negative_prompt %q", v3.Parameters.UC, v3.Parameters.NegativePrompt)
	}
}

// NovelAI 扩展未开启时保持"无预设"语义（ucPreset=3）。
func TestConvertToNovelAIRequestDisabledUsesNonePreset(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Model:  "nai-diffusion-3",
		Prompt: "1girl",
		Size:   "1024x1024",
	})
	if req.Parameters.UCPreset != 3 {
		t.Fatalf("ucPreset = %d, want 3 (none) when NovelAI extension is off", req.Parameters.UCPreset)
	}
}
