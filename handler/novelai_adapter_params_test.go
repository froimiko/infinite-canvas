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

// 前端云端模型值带 `渠道id::` 前缀，必须在发送前用 modelOptionName 剥掉。
// 这里锁住后端侧的兜底：即使漏剥，也不能把整串当成模型名发给上游。
// 现象回顾：`__cloud__::nai-diffusion-4-5-full` 靠模糊匹配侥幸命中 V4.5，
// 但反复切换模型会叠成 `__cloud__::__cloud__::...`，最终 502。
func TestResolveNovelAIModelStripsChannelPrefix(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"__cloud__::nai-diffusion-5-full", "nai-diffusion-5-full"},
		{"__cloud__::nai-diffusion-5-curated", "nai-diffusion-5-curated"},
		{"__cloud__::nai-diffusion-4-5-full", "nai-diffusion-4-5-full"},
		{"__cloud__::nai-diffusion-4-5-curated", "nai-diffusion-4-5-curated"},
		{"__cloud__::nai-diffusion-4-full", "nai-diffusion-4-full"},
		{"__cloud__::nai-diffusion-3", "nai-diffusion-3"},
		{"__cloud__::__cloud__::nai-diffusion-4-5-full", "nai-diffusion-4-5-full"},
		{"ch-abc123::nai-diffusion-4-5-curated", "nai-diffusion-4-5-curated"},
	}
	for _, tt := range tests {
		if got := resolveNovelAIModel(tt.input); got != tt.expect {
			t.Errorf("resolveNovelAIModel(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

// 参考实现里不存在 aqtPreset 这个字段，任何模型都不能发送。
func TestConvertToNovelAIRequestNeverSendsAqtPreset(t *testing.T) {
	for _, model := range []string{"nai-diffusion-3", "nai-diffusion-4-full", "nai-diffusion-4-5-full", "nai-diffusion-4-5-curated", "nai-diffusion-5-full", "nai-diffusion-5-curated"} {
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
		if strings.Contains(strings.ToLower(string(body)), "aqt") {
			t.Fatalf("%s payload must not contain aqtPreset: %s", model, string(body))
		}
	}
}

// 锁定"前端只传模型和尺寸"时的上游默认值，逐项对齐 Aaalice_NAI_Launcher 的 ImageParams。
// 这些默认值直接决定出图观感：sampler / cfg_rescale / noise_schedule / smea / decrisp
// 任何一项漂移都会让 28 步的图看起来像没跑完（发软、发平、细节丢失）。
// 想看完整请求体：go test ./handler/ -run GoldenDefaults -v
func TestConvertToNovelAIRequestGoldenDefaults(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:         "1girl, solo",
		NegativePrompt: "bad hands",
		Size:           "832x1216",
		NovelAIEnabled: true,
		NovelAIModel:   "nai-diffusion-4-5-full",
	})

	if req.Parameters.Sampler != "k_euler_ancestral" {
		t.Errorf("sampler = %q, want k_euler_ancestral", req.Parameters.Sampler)
	}
	if req.Parameters.CfgRescale != 0 {
		t.Errorf("cfg_rescale = %v, want 0", req.Parameters.CfgRescale)
	}
	if req.Parameters.NoiseSchedule != "karras" {
		t.Errorf("noise_schedule = %q, want karras", req.Parameters.NoiseSchedule)
	}
	if req.Parameters.Steps != 28 {
		t.Errorf("steps = %d, want 28", req.Parameters.Steps)
	}
	if req.Parameters.Scale != 5.0 {
		t.Errorf("scale = %v, want 5.0", req.Parameters.Scale)
	}
	if req.Parameters.DynamicThresholding {
		t.Error("dynamic_thresholding should default to false")
	}
	if req.Parameters.SkipCfgAboveSigma != nil {
		t.Errorf("skip_cfg_above_sigma should be null when Variety+ is off, got %v", *req.Parameters.SkipCfgAboveSigma)
	}
	if req.Parameters.UCPreset != 0 {
		t.Errorf("ucPreset = %d, want 0 (Heavy)", req.Parameters.UCPreset)
	}
	if req.Parameters.QualityToggle {
		t.Error("qualityToggle should default to false: 质量词已由前端按模型注入，上游再加一遍会双份")
	}
	if req.Parameters.AddOriginalImage {
		t.Error("add_original_image should default to false: 纯文生图不需要附加原图")
	}

	body, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	t.Logf("golden upstream request:\n%s", string(body))
}

// V3 分支同样要用参考默认值（V3 才会真正发送 sm / sm_dyn / dynamic_thresholding）。
func TestConvertToNovelAIRequestV3GoldenDefaults(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:         "1girl, solo",
		NegativePrompt: "bad hands",
		Size:           "832x1216",
		NovelAIEnabled: true,
		NovelAIModel:   "nai-diffusion-3",
	})

	if req.Parameters.SM == nil || req.Parameters.SMDyn == nil {
		t.Fatal("V3 request should send sm / sm_dyn")
	}
	if *req.Parameters.SM {
		t.Error("sm should default to false")
	}
	if *req.Parameters.SMDyn {
		t.Error("sm_dyn should default to false")
	}
	if req.Parameters.DynamicThresholding {
		t.Error("dynamic_thresholding (decrisp) should default to false")
	}
	if req.Parameters.NoiseSchedule != "karras" {
		t.Errorf("noise_schedule = %q, want karras", req.Parameters.NoiseSchedule)
	}
}

// 回归：novelai_model 传了非 NovelAI 的模型名（例如画布节点首次生成时误把全局默认
// 模型 gpt-image-2 当成 NovelAI 模型发过来）时，后端会静默回落 nai-diffusion-3。
//
// 这层回落本身是必要的兜底，但它意味着**前端发错模型名后端不会报错**，
// 症状是"下拉显示 V4.5、实际按 V3 出图"。所以前端 buildGenerationConfig 必须
// 显式把已解析的模型传给 normalizeNovelAISettings，不能让空值落到 config.model。
// 这个测试固定住回落行为，避免以后有人把回落改成报错或改成别的模型。
func TestResolveNovelAIModelFallsBackForNonNovelAINames(t *testing.T) {
	cases := []string{
		"gpt-image-2",
		"__cloud__::gpt-image-2",
		"gpt-5.5",
		"seedream-3",
		"",
		"   ",
	}
	for _, name := range cases {
		if got := resolveNovelAIModel(name); got != "nai-diffusion-3" {
			t.Errorf("resolveNovelAIModel(%q) = %q, want nai-diffusion-3 (兜底)", name, got)
		}
	}

	// 反向确认：正常的 NovelAI 模型名不会被误伤。
	if got := resolveNovelAIModel("__cloud__::nai-diffusion-4-5-full"); got != "nai-diffusion-4-5-full" {
		t.Errorf("resolveNovelAIModel(cloud v4.5 full) = %q, want nai-diffusion-4-5-full", got)
	}
}