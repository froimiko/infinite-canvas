package handler

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestResolveNovelAIModelV5Variants(t *testing.T) {
    tests := []struct {
        input  string
        expect string
    }{
        {"nai-diffusion-5-full", "nai-diffusion-5-full"},
        {"nai-diffusion-5-curated", "nai-diffusion-5-curated"},
        {"v5", "nai-diffusion-5-full"},
        {"5", "nai-diffusion-5-full"},
        {"NAI Diffusion V5 Curated", "nai-diffusion-5-curated"},
        {"__cloud__::nai-diffusion-5-full", "nai-diffusion-5-full"},
        // 关键防呆：V4.5 的小数点不能把末尾 5 误判成 V5。
        {"v4.5", "nai-diffusion-4-5-full"},
        {"NAI Diffusion V4.5 Curated", "nai-diffusion-4-5-curated"},
        {"xv5y", "nai-diffusion-3"},
    }
    for _, tt := range tests {
        if got := resolveNovelAIModel(tt.input); got != tt.expect {
            t.Errorf("resolveNovelAIModel(%q) = %q, want %q", tt.input, got, tt.expect)
        }
    }
}

func TestConvertToNovelAIRequestV5UsesStructuredPrompt(t *testing.T) {
    for _, model := range []string{"nai-diffusion-5-full", "nai-diffusion-5-curated"} {
        req := mustConvertToNovelAIRequest(t, openAIImageRequest{
            Prompt:             "1girl, solo",
            NegativePrompt:     "bad hands",
            Size:               "832x1216",
            NovelAIEnabled:     true,
            NovelAIModel:       model,
            NoiseSchedule:      "native",
            SM:                 boolPtr(true),
            SMDyn:              boolPtr(true),
            DynamicThresholding: boolPtr(true),
        })
        if req.Model != model {
            t.Fatalf("model = %q, want %q", req.Model, model)
        }
        if req.Parameters.V4Prompt == nil || req.Parameters.V4NegativePrompt == nil {
            t.Fatalf("%s request must include v4_prompt and v4_negative_prompt", model)
        }
        if req.Parameters.UC != "" {
            t.Fatalf("%s request must omit V3 uc, got %q", model, req.Parameters.UC)
        }
        if req.Parameters.SM != nil || req.Parameters.SMDyn != nil {
            t.Fatalf("%s request must omit sm/sm_dyn", model)
        }
        if req.Parameters.NoiseSchedule != "karras" {
            t.Fatalf("%s noise_schedule = %q, want karras", model, req.Parameters.NoiseSchedule)
        }
        if req.Parameters.DynamicThresholding {
            t.Fatalf("%s dynamic_thresholding must be false", model)
        }
        if req.Parameters.LegacyV3Extend == nil || req.Parameters.UseCoords == nil {
            t.Fatalf("%s request must include V4+ compatibility flags", model)
        }
    }
}

func TestNovelAIModelV5ChannelPrefixDoesNotLeakToPayload(t *testing.T) {
    req := mustConvertToNovelAIRequest(t, openAIImageRequest{
        Prompt:         "1girl",
        Size:           "832x1216",
        NovelAIEnabled: true,
        NovelAIModel:   "__cloud__::nai-diffusion-5-curated",
    })
    body, err := json.Marshal(req)
    if err != nil {
        t.Fatal(err)
    }
    if strings.Contains(string(body), "__cloud__::") {
        t.Fatalf("channel prefix leaked into NovelAI payload: %s", body)
    }
}
