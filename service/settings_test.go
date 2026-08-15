package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/basketikun/infinite-canvas/model"
)

func TestFetchAdminChannelModelsParsesOpenAIModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"z-model"},{"id":"a-model"},{"id":""}]}`))
	}))
	defer server.Close()

	models, err := fetchAdminChannelModels(model.ModelChannel{
		BaseURL: server.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("fetchAdminChannelModels returned error: %v", err)
	}
	if want := []string{"a-model", "z-model"}; !reflect.DeepEqual(models, want) {
		t.Fatalf("models = %#v, want %#v", models, want)
	}
}

func TestFetchAdminChannelModelsReportsArkPlanModelsUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/plan/v3/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := fetchAdminChannelModels(model.ModelChannel{
		BaseURL: server.URL + "/api/plan/v3/contents/generations/tasks",
		APIKey:  "test-key",
	})
	if err == nil {
		t.Fatal("expected unsupported /models error")
	}
	if !strings.Contains(err.Error(), "Agent Plan 未提供 OpenAI /models") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestBuildModelChannelURLNormalizesArkPlanTaskPath(t *testing.T) {
	got := BuildModelChannelURL(model.ModelChannel{BaseURL: "https://ark.cn-beijing.volces.com/api/plan/v3/contents/generations/tasks?debug=1"}, "/models")
	want := "https://ark.cn-beijing.volces.com/api/plan/v3/models"
	if got != want {
		t.Fatalf("BuildModelChannelURL = %q, want %q", got, want)
	}
}

func TestNormalizeSettingsPublishesEnabledChannelModelsAndRepairsDefaults(t *testing.T) {
	settings := normalizeSettings(model.Settings{
		Public: model.PublicSetting{
			ModelChannel: model.PublicModelChannelSetting{
				AvailableModels:   []string{"grok-imagine-video", "disabled-model"},
				DefaultModel:      "grok-imagine-video",
				DefaultTextModel:  "missing-text",
				DefaultImageModel: "missing-image",
				DefaultVideoModel: "missing-video",
			},
		},
		Private: model.PrivateSetting{
			Channels: []model.ModelChannel{
				{Enabled: true, Models: []string{"gpt-5.5", "doubao-seedream-5.0-lite", "doubao-seedance-2.0-fast", "gpt-5.5"}},
				{Enabled: false, Models: []string{"disabled-model"}},
			},
		},
	})

	channel := settings.Public.ModelChannel
	wantModels := []string{"gpt-5.5", "doubao-seedream-5.0-lite", "doubao-seedance-2.0-fast"}
	if !reflect.DeepEqual(channel.AvailableModels, wantModels) {
		t.Fatalf("available models = %#v, want %#v", channel.AvailableModels, wantModels)
	}
	if channel.DefaultModel != "gpt-5.5" {
		t.Fatalf("default model = %q, want text model", channel.DefaultModel)
	}
	if channel.DefaultTextModel != "gpt-5.5" {
		t.Fatalf("default text model = %q, want text model", channel.DefaultTextModel)
	}
	if channel.DefaultImageModel != "doubao-seedream-5.0-lite" {
		t.Fatalf("default image model = %q, want seedream", channel.DefaultImageModel)
	}
	if channel.DefaultVideoModel != "doubao-seedance-2.0-fast" {
		t.Fatalf("default video model = %q, want seedance", channel.DefaultVideoModel)
	}
}

func TestNormalizePromptTranslationSettingUsesDefaults(t *testing.T) {
	setting := normalizePromptTranslationSetting(model.PromptTranslationSetting{})
	if setting.Enabled == nil || !*setting.Enabled {
		t.Fatalf("enabled = %#v, want true", setting.Enabled)
	}
	if setting.Translator != model.PromptTranslatorLibrary {
		t.Fatalf("translator = %q, want %q", setting.Translator, model.PromptTranslatorLibrary)
	}
	if setting.Service != model.PromptTranslationServiceAlibaba {
		t.Fatalf("service = %q, want %q", setting.Service, model.PromptTranslationServiceAlibaba)
	}
	if setting.SourceLanguage != "en" || setting.TargetLanguage != "zh" {
		t.Fatalf("languages = %q -> %q, want en -> zh", setting.SourceLanguage, setting.TargetLanguage)
	}
}

func TestNormalizePrivateSettingDefaultsPromptTranslationForLegacyJSON(t *testing.T) {
	var setting model.PrivateSetting
	if err := json.Unmarshal([]byte("{\"promptTagTranslationDatabase\":{\"enabled\":false,\"owner\":\"ffdkj\",\"repo\":\"legacy\"}}"), &setting); err != nil {
		t.Fatalf("unmarshal legacy private settings: %v", err)
	}
	translation := normalizePrivateSetting(setting).PromptTranslation
	enabled := translation.Enabled != nil && *translation.Enabled
	if !enabled || translation.Translator != model.PromptTranslatorLibrary || translation.Service != model.PromptTranslationServiceAlibaba || translation.SourceLanguage != "en" || translation.TargetLanguage != "zh" {
		t.Fatalf("promptTranslation = enabled:%t translator:%q service:%q source:%q target:%q, want true/library/alibaba/en/zh", enabled, translation.Translator, translation.Service, translation.SourceLanguage, translation.TargetLanguage)
	}
}

func TestNormalizePromptTranslationSettingRepairsInvalidValues(t *testing.T) {
	disabled := false
	setting := normalizePromptTranslationSetting(model.PromptTranslationSetting{
		Enabled:        &disabled,
		Translator:     model.PromptTranslator("invalid"),
		Service:        model.PromptTranslationService("invalid"),
		SourceLanguage: "  EN-US ",
		TargetLanguage: "  ",
	})
	if setting.Enabled == nil || *setting.Enabled {
		t.Fatalf("enabled = %#v, want false", setting.Enabled)
	}
	if setting.Translator != model.PromptTranslatorLibrary || setting.Service != model.PromptTranslationServiceAlibaba {
		t.Fatalf("translator/service = %q/%q, want library/alibaba", setting.Translator, setting.Service)
	}
	if setting.SourceLanguage != "en-us" || setting.TargetLanguage != "zh" {
		t.Fatalf("languages = %q -> %q, want en-us -> zh", setting.SourceLanguage, setting.TargetLanguage)
	}
}
