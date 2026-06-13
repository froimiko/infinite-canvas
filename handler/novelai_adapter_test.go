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
