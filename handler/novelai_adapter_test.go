package handler

import (
	"encoding/json"
	"sync/atomic"
	"strings"
	"testing"
	"time"

	"github.com/basketikun/infinite-canvas/model"
)

const defaultNovelAINegativePrompt = "lowres, bad anatomy, bad hands, text, error, missing fingers, extra digit, fewer digits, cropped, worst quality, low quality, normal quality, jpeg artifacts, signature, watermark, username, blurry"

func TestConvertToNovelAIRequestUsesDefaultNegativePrompt(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Model:  "nai-diffusion-3",
		Prompt: "girl in a garden",
		Size:   "1024x1024",
	})

	if req.Parameters.NegativePrompt != defaultNovelAINegativePrompt {
		t.Fatalf("negative prompt = %q, want default", req.Parameters.NegativePrompt)
	}
}

func TestConvertToNovelAIRequestDisabledKeepsLegacyDefaults(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Model:          "nai-diffusion-3",
		Prompt:         "girl in a garden",
		NegativePrompt: "bad hands",
		N:              2,
		Size:           "1024x1024",
		Quality:        "hd",
		Sampler:        "k_euler",
		Steps:          intPtr(12),
		CfgScale:       float64Ptr(7.5),
		Seed:           int64Ptr(123),
		UCPreset:       "Heavy",
		CfgRescale:     float64Ptr(0.18),
		NoiseSchedule:  "native",
	})

	if req.Model != "nai-diffusion-3" {
		t.Fatalf("model = %q, want legacy model", req.Model)
	}
	if req.Parameters.Sampler != "k_euler_ancestral" {
		t.Fatalf("sampler = %q, want legacy default", req.Parameters.Sampler)
	}
	if req.Parameters.Steps != 28 {
		t.Fatalf("steps = %d, want legacy quality-derived steps", req.Parameters.Steps)
	}
	if req.Parameters.Scale != 5.5 {
		t.Fatalf("scale = %v, want legacy quality-derived scale", req.Parameters.Scale)
	}
	if req.Parameters.Seed != 0 {
		t.Fatalf("seed = %d, want legacy random seed marker 0", req.Parameters.Seed)
	}
	if req.Parameters.UCPreset != 4 {
		t.Fatalf("ucPreset = %d, want legacy default", req.Parameters.UCPreset)
	}
	if req.Parameters.CfgRescale != 0 {
		t.Fatalf("cfg_rescale = %v, want legacy default", req.Parameters.CfgRescale)
	}
	if req.Parameters.NoiseSchedule != "karras" {
		t.Fatalf("noise_schedule = %q, want legacy default", req.Parameters.NoiseSchedule)
	}
}

func TestConvertToNovelAIRequestUsesUserNegativePrompt(t *testing.T) {
	const userNegativePrompt = "low quality, extra fingers"
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Model:          "nai-diffusion-3",
		Prompt:         "girl in a garden",
		NegativePrompt: "  " + userNegativePrompt + "  ",
		Size:           "1024x1024",
	})

	if req.Parameters.NegativePrompt != userNegativePrompt {
		t.Fatalf("negative prompt = %q, want %q", req.Parameters.NegativePrompt, userNegativePrompt)
	}
}

func TestConvertToNovelAIRequestSyncsV4NegativePrompt(t *testing.T) {
	const userNegativePrompt = "bad hands, watermark"
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Model:          "nai-diffusion-4-curated",
		Prompt:         "girl in a garden",
		NegativePrompt: userNegativePrompt,
		Size:           "1024x1024",
	})

	if req.Parameters.NegativePrompt != userNegativePrompt {
		t.Fatalf("negative prompt = %q, want %q", req.Parameters.NegativePrompt, userNegativePrompt)
	}
	if req.Parameters.V4NegativePrompt == nil {
		t.Fatal("expected V4NegativePrompt to be set")
	}
	if got := req.Parameters.V4NegativePrompt.Caption.BaseCaption; got != userNegativePrompt {
		t.Fatalf("v4 negative base caption = %q, want %q", got, userNegativePrompt)
	}
}

func TestConvertToNovelAIRequestEnabledOverridesParameters(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Model:               "nai-diffusion-3",
		Prompt:              "girl in a garden",
		NegativePrompt:      "bad hands",
		N:                   3,
		Size:                "1024x1024",
		Quality:             "low",
		NovelAIEnabled:      true,
		NovelAIModel:        "nai-diffusion-4-5-curated",
		Sampler:             "k_dpmpp_2m",
		Steps:               intPtr(35),
		CfgScale:            float64Ptr(6.25),
		Seed:                int64Ptr(123456),
		UCPreset:            "Light",
		CfgRescale:          float64Ptr(0.42),
		NoiseSchedule:       "native",
		SM:                  boolPtr(true),
		SMDyn:               boolPtr(true),
		DynamicThresholding: boolPtr(true),
		VarietyPlus:         boolPtr(true),
		AqtPreset:           "anime",
	})

	if req.Model != "nai-diffusion-4-5-curated" {
		t.Fatalf("model = %q, want NovelAI model override", req.Model)
	}
	if req.Parameters.Sampler != "k_dpmpp_2m" {
		t.Fatalf("sampler = %q, want override", req.Parameters.Sampler)
	}
	if req.Parameters.Steps != 35 {
		t.Fatalf("steps = %d, want override", req.Parameters.Steps)
	}
	if req.Parameters.Scale != 6.25 {
		t.Fatalf("scale = %v, want override", req.Parameters.Scale)
	}
	if req.Parameters.Seed != 123456 {
		t.Fatalf("seed = %d, want override", req.Parameters.Seed)
	}
	if req.Parameters.UCPreset != 2 {
		t.Fatalf("ucPreset = %d, want Light mapping", req.Parameters.UCPreset)
	}
	if req.Parameters.CfgRescale != 0.42 {
		t.Fatalf("cfg_rescale = %v, want override", req.Parameters.CfgRescale)
	}
	if req.Parameters.NoiseSchedule != "karras" {
		t.Fatalf("noise_schedule = %q, want native remapped to karras for V4", req.Parameters.NoiseSchedule)
	}
	if req.Parameters.SM != nil || req.Parameters.SMDyn != nil {
		t.Fatal("V4 request should omit sm/sm_dyn")
	}
	if !req.Parameters.DynamicThresholding {
		t.Fatal("dynamic_thresholding should use enabled override")
	}
	if req.Parameters.SkipCfgAboveSigma == nil {
		t.Fatal("variety_plus should set skip_cfg_above_sigma")
	}
	if req.Parameters.AqtPreset != "anime" {
		t.Fatalf("aqtPreset = %q, want override", req.Parameters.AqtPreset)
	}
}

func TestConvertToNovelAIRequestV4CharacterPrompts(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:             "two girls in a garden",
		NegativePrompt:     "bad hands",
		Size:               "1024x1024",
		NovelAIEnabled:     true,
		NovelAIModel:       "nai-diffusion-4-full",
		DivideRoles:        true,
		UseAutoPositioning: false,
		CharacterPrompts: []novelAICharacterPromptInput{
			{
				DisplayName:             "Alice",
				CharacterPrompt:         "alice, red hair",
				CharacterNegativePrompt: "blue hair",
				Coords:                  &novelAIGridCoords{X: 2, Y: 4},
			},
			{
				DisplayName:     "Beth",
				CharacterPrompt: "beth, black hair",
				Coords:          &novelAIGridCoords{X: 3, Y: 1},
			},
		},
	})

	if req.Parameters.V4Prompt == nil || req.Parameters.V4NegativePrompt == nil {
		t.Fatal("expected V4 prompt structures")
	}
	if got := req.Parameters.V4Prompt.Caption.BaseCaption; got != "two girls in a garden" {
		t.Fatalf("v4 base caption = %q, want main prompt", got)
	}
	if !req.Parameters.V4Prompt.UseCoords || !req.Parameters.V4NegativePrompt.UseCoords {
		t.Fatal("manual role positioning should enable use_coords")
	}
	if !req.Parameters.V4Prompt.UseOrder || !req.Parameters.V4NegativePrompt.UseOrder {
		t.Fatal("role prompts should keep use_order enabled")
	}
	if got := len(req.Parameters.V4Prompt.Caption.CharCaptions); got != 2 {
		t.Fatalf("char caption count = %d, want 2", got)
	}
	first := req.Parameters.V4Prompt.Caption.CharCaptions[0]
	if first.CharCaption != "alice, red hair" {
		t.Fatalf("first char prompt = %q", first.CharCaption)
	}
	if len(first.Centers) != 1 || first.Centers[0].X != 0.3 || first.Centers[0].Y != 0.7 {
		t.Fatalf("first center = %#v, want mapped coords 0.3/0.7", first.Centers)
	}
	if got := req.Parameters.V4NegativePrompt.Caption.CharCaptions[0].CharCaption; got != "blue hair" {
		t.Fatalf("first negative char prompt = %q, want role negative", got)
	}
	if got := len(req.Parameters.CharacterPrompts); got != 2 {
		t.Fatalf("compat characterPrompts count = %d, want 2", got)
	}
	if req.Parameters.CharacterPrompts[0].Center == nil || req.Parameters.CharacterPrompts[0].Center.X != 0.3 {
		t.Fatalf("compat center = %#v, want mapped center", req.Parameters.CharacterPrompts[0].Center)
	}
}

func TestConvertToNovelAIRequestEnabledV3OmitsV4OnlyFields(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:         "girl in a garden",
		NegativePrompt: "bad hands",
		Size:           "1024x1024",
		NovelAIEnabled: true,
		NovelAIModel:   "nai-diffusion-3",
		AqtPreset:      "safe",
		DivideRoles:    true,
		CharacterPrompts: []novelAICharacterPromptInput{
			{DisplayName: "Alice", CharacterPrompt: "alice", CharacterNegativePrompt: "bad alice", Coords: &novelAIGridCoords{X: 2, Y: 2}},
		},
	})

	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	payload := string(body)
	for _, forbidden := range []string{"\"aqtPreset\"", "\"v4_prompt\"", "\"v4_negative_prompt\"", "\"characterPrompts\""} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("V3 enabled payload should not contain %s: %s", forbidden, payload)
		}
	}
}

func TestNormalizeNovelAISeedRandomizesOutOfRangeSeed(t *testing.T) {
	got := normalizeNovelAISeed(int64Ptr(4294967296))
	if got < 0 || got > 4294967295 {
		t.Fatalf("seed = %d, want 32-bit unsigned range", got)
	}
}

func TestMapNovelAICharacterPromptCoordsClampsOutOfRange(t *testing.T) {
	center := mapNovelAICharacterPromptCoords(&novelAIGridCoords{X: -1, Y: 99})
	if center == nil {
		t.Fatal("expected center")
	}
	if center.X != 0.0 || center.Y != 0.7 {
		t.Fatalf("center = %#v, want clamped 0.0/0.7", center)
	}
}

func TestConvertToNovelAIRequestRolePromptsManualPositioningDefaultsMissingCoords(t *testing.T) {
	req := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:             "two girls in a garden",
		NegativePrompt:     "bad hands",
		Size:               "1024x1024",
		NovelAIEnabled:     true,
		NovelAIModel:       "nai-diffusion-4-full",
		DivideRoles:        true,
		UseAutoPositioning: false,
		CharacterPrompts: []novelAICharacterPromptInput{
			{DisplayName: "Alice", CharacterPrompt: "alice"},
		},
	})

	if req.Parameters.V4Prompt == nil || !req.Parameters.V4Prompt.UseCoords {
		t.Fatal("manual role positioning should keep use_coords enabled")
	}
	centers := req.Parameters.V4Prompt.Caption.CharCaptions[0].Centers
	if len(centers) != 1 || centers[0].X != 0.5 || centers[0].Y != 0.5 {
		t.Fatalf("centers = %#v, want default center 0.5/0.5", centers)
	}
}

func TestApplyFreeGenerationLockUsesExplicitNovelAISteps(t *testing.T) {
	steps := 35
	err := applyFreeGenerationLock(&openAIImageRequest{
		Size:           "1024x1024",
		Quality:        "low",
		NovelAIEnabled: true,
		Steps:          &steps,
	}, &model.FreeGenerationLock{
		Enabled:   true,
		MaxPixels: 1024 * 1024,
		MaxSteps:  28,
	}, false)

	if err == nil {
		t.Fatal("expected explicit NovelAI steps to be checked against free lock")
	}
	if !strings.Contains(err.Error(), "35") || !strings.Contains(err.Error(), "显式 NovelAI steps") {
		t.Fatalf("error = %q, want explicit steps detail", err.Error())
	}
}

func TestWithNovelAIFreeGenerationLockSerializesSameToken(t *testing.T) {
	channel := model.ModelChannel{
		BaseURL: "https://image.novelai.net/",
		APIKey:  "same-token",
		FreeGenerationLock: &model.FreeGenerationLock{
			Enabled: true,
		},
	}

	var active int32
	var maxActive int32
	releaseFirst := make(chan struct{})
	firstStarted := make(chan struct{})
	done := make(chan error, 2)

	call := func(id int) {
		_, err := withNovelAIFreeGenerationLock(channel, func() ([]map[string]interface{}, error) {
			current := atomic.AddInt32(&active, 1)
			for {
				max := atomic.LoadInt32(&maxActive)
				if current <= max || atomic.CompareAndSwapInt32(&maxActive, max, current) {
					break
				}
			}
			if id == 1 {
				close(firstStarted)
				<-releaseFirst
			}
			atomic.AddInt32(&active, -1)
			return []map[string]interface{}{{"id": id}}, nil
		})
		done <- err
	}

	go call(1)
	<-firstStarted
	go call(2)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("first call returned unexpected error: %v", err)
		}
		t.Fatal("second call should wait for same channel/token lock")
	case <-time.After(30 * time.Millisecond):
	}

	close(releaseFirst)
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatalf("locked call returned error: %v", err)
		}
	}
	if maxActive != 1 {
		t.Fatalf("max active same-token calls = %d, want 1", maxActive)
	}
}

func TestWithNovelAIFreeGenerationLockAllowsDifferentTokens(t *testing.T) {
	channelA := model.ModelChannel{
		BaseURL: "https://image.novelai.net",
		APIKey:  "token-a",
		FreeGenerationLock: &model.FreeGenerationLock{
			Enabled: true,
		},
	}
	channelB := model.ModelChannel{
		BaseURL: "https://image.novelai.net",
		APIKey:  "token-b",
		FreeGenerationLock: &model.FreeGenerationLock{
			Enabled: true,
		},
	}

	startedA := make(chan struct{})
	releaseA := make(chan struct{})
	done := make(chan error, 2)

	go func() {
		_, err := withNovelAIFreeGenerationLock(channelA, func() ([]map[string]interface{}, error) {
			close(startedA)
			<-releaseA
			return []map[string]interface{}{{"token": "a"}}, nil
		})
		done <- err
	}()
	<-startedA

	go func() {
		_, err := withNovelAIFreeGenerationLock(channelB, func() ([]map[string]interface{}, error) {
			return []map[string]interface{}{{"token": "b"}}, nil
		})
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("different-token call returned error: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("different token should not wait for channelA lock")
	}

	close(releaseA)
	if err := <-done; err != nil {
		t.Fatalf("released call returned error: %v", err)
	}
}

func mustConvertToNovelAIRequest(t *testing.T, req openAIImageRequest) *novelAIRequest {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	naiReq, err := convertToNovelAIRequest(body)
	if err != nil {
		t.Fatalf("convert request: %v", err)
	}
	if strings.TrimSpace(naiReq.Parameters.NegativePrompt) == "" {
		t.Fatal("negative prompt should not be empty")
	}
	return naiReq
}

func intPtr(value int) *int {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func float64Ptr(value float64) *float64 {
	return &value
}
