package handler

import (
	"encoding/json"
	"strings"
	"testing"
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
