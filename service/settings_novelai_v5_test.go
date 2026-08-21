package service

import "testing"

func TestNormalizeNovelAIModelNameV5(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"NAI Diffusion V5 Full", "nai-diffusion-5-full"},
		{"NAI Diffusion V5 Curated", "nai-diffusion-5-curated"},
		{"nai-diffusion-5-full", "nai-diffusion-5-full"},
		{"nai-diffusion-5-curated", "nai-diffusion-5-curated"},
		{"v5", "nai-diffusion-5-full"},
		{"5", "nai-diffusion-5-full"},
		// 不能把 V4.5 的末尾 5 或其他厂商的 gpt-5.5 误判成 NAI V5。
		{"NAI Diffusion V4.5 Curated", "nai-diffusion-4-5-curated"},
		{"gpt-5.5", "gpt-5.5"},
	}
	for _, tt := range tests {
		if got := normalizeNovelAIModelName(tt.input); got != tt.expect {
			t.Errorf("normalizeNovelAIModelName(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestModelChannelNameMatchesNovelAIV5(t *testing.T) {
	tests := []struct {
		configured string
		requested  string
		want       bool
	}{
		{"NAI Diffusion V5 Full", "nai-diffusion-5-full", true},
		{"NAI Diffusion V5 Curated", "nai-diffusion-5-curated", true},
		{"NAI Diffusion V5 Full", "nai-diffusion-5-curated", false},
	}
	for _, tt := range tests {
		if got := modelChannelNameMatches("novelai", tt.configured, tt.requested); got != tt.want {
			t.Errorf("modelChannelNameMatches(novelai, %q, %q) = %v, want %v", tt.configured, tt.requested, got, tt.want)
		}
	}
}

func TestNovelAIImageModelsIncludeV5(t *testing.T) {
	models := novelAIImageModels()
	if len(models) < 2 || models[0] != "NAI Diffusion V5 Full" || models[1] != "NAI Diffusion V5 Curated" {
		t.Fatalf("novelAIImageModels() must start with V5 Full/Curated, got %v", models)
	}
}
