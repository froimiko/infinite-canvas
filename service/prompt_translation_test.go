package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/basketikun/infinite-canvas/model"
)

func promptTranslationTestSetting(service model.PromptTranslationService) model.PromptTranslationSetting {
	return model.PromptTranslationSetting{
		Translator:     model.PromptTranslatorLibrary,
		Service:        service,
		SourceLanguage: "en",
		TargetLanguage: "zh",
	}
}

func TestTranslatePromptTextRejectsInvalidRequests(t *testing.T) {
	disabled := false
	disabledSetting := promptTranslationTestSetting(model.PromptTranslationServiceAlibaba)
	disabledSetting.Enabled = &disabled
	invalidServiceSetting := promptTranslationTestSetting(model.PromptTranslationService("baidu"))
	sameLanguageSetting := promptTranslationTestSetting(model.PromptTranslationServiceBing)
	sameLanguageSetting.TargetLanguage = "en"

	cases := []struct {
		name    string
		text    string
		setting model.PromptTranslationSetting
		message string
	}{
		{name: "empty text", text: "   \n  ", setting: promptTranslationTestSetting(model.PromptTranslationServiceAlibaba), message: "翻译内容不能为空"},
		{name: "too many lines", text: strings.Repeat("tag\n", 50) + "tag", setting: promptTranslationTestSetting(model.PromptTranslationServiceAlibaba), message: "翻译行数过多"},
		{name: "invalid service", text: "1girl", setting: invalidServiceSetting, message: "不支持的翻译服务"},
		{name: "disabled", text: "1girl", setting: disabledSetting, message: "网络翻译未开启"},
		{name: "same language", text: "1girl", setting: sameLanguageSetting, message: "源语言和目标语言不能相同"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			translated, err := translatePromptText(item.text, item.setting)
			if err == nil {
				t.Fatalf("translatePromptText returned %q, want error", translated)
			}
			if !strings.Contains(err.Error(), item.message) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), item.message)
			}
		})
	}
}

func TestAlibabaTranslatorCompletesFullFlow(t *testing.T) {
	csrfCalls := 0
	fields := map[string]string{}
	csrfHeader := ""
	origin := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(`<script src="/mcms/translation-open-portal/1.2.3/translation-open-portal_interface.json"></script>`))
		case "/mcms/translation-open-portal/1.2.3/translation-open-portal_interface.json":
			_, _ = w.Write([]byte(`{"en_US":{"interface.en":"English","interface.zh":"Chinese"},"zh_CN":{"interface.en":"英语"}}`))
		case "/api/translate/csrftoken":
			csrfCalls++
			_, _ = w.Write([]byte(`{"headerName":"x-csrf-token","token":"csrf-token-value"}`))
		case "/api/translate/text":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse multipart form: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			for _, name := range []string{"query", "srcLang", "tgtLang", "_csrf", "domain"} {
				fields[name] = r.FormValue(name)
			}
			csrfHeader = r.Header.Get("x-csrf-token")
			origin = r.Header.Get("Origin")
			_, _ = w.Write([]byte(`{"data":{"translateText":"一个女孩"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	translator := newAlibabaTranslator(server.URL, server.Client())
	translated, err := translator.translate("1girl", "en", "zh")
	if err != nil {
		t.Fatalf("alibaba translate returned error: %v", err)
	}
	if translated != "一个女孩" {
		t.Fatalf("translated = %q, want 一个女孩", translated)
	}
	if csrfCalls != 2 {
		t.Fatalf("csrf calls = %d, want 2", csrfCalls)
	}
	if csrfHeader != "csrf-token-value" {
		t.Fatalf("csrf header = %q, want csrf-token-value", csrfHeader)
	}
	if origin != server.URL {
		t.Fatalf("origin = %q, want %q", origin, server.URL)
	}
	want := map[string]string{"query": "1girl", "srcLang": "en", "tgtLang": "zh", "_csrf": "csrf-token-value", "domain": "general"}
	for name, value := range want {
		if fields[name] != value {
			t.Fatalf("multipart field %s = %q, want %q", name, fields[name], value)
		}
	}
}

func TestAlibabaTranslatorRejectsInvalidLanguageResource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte(`<script src="/lang/translation-open-portal_interface.json"></script>`))
		case "/lang/translation-open-portal_interface.json":
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	translator := newAlibabaTranslator(server.URL, server.Client())
	if _, err := translator.translate("1girl", "en", "zh"); err == nil || !strings.Contains(err.Error(), "语言配置格式异常") {
		t.Fatalf("error = %v, want 语言配置格式异常", err)
	}
}

func TestBingTranslatorTranslatesText(t *testing.T) {
	query := url.Values{}
	authorization := ""
	body := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/translate/auth":
			_, _ = w.Write([]byte("  bing-token-1  \n"))
		case "/translate":
			query = r.URL.Query()
			authorization = r.Header.Get("Authorization")
			raw, _ := io.ReadAll(r.Body)
			body = string(raw)
			_, _ = w.Write([]byte(`[{"translations":[{"text":"一个女孩","to":"zh-Hans"}]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	translator := newBingTranslator(server.URL+"/translate/auth", server.URL+"/translate", server.Client())
	translated, err := translator.translate("1girl", "en", "zh")
	if err != nil {
		t.Fatalf("bing translate returned error: %v", err)
	}
	if translated != "一个女孩" {
		t.Fatalf("translated = %q, want 一个女孩", translated)
	}
	if authorization != "Bearer bing-token-1" {
		t.Fatalf("authorization = %q, want Bearer bing-token-1", authorization)
	}
	if body != `[{"Text":"1girl"}]` {
		t.Fatalf("body = %q, want [{\"Text\":\"1girl\"}]", body)
	}
	if query.Get("from") != "en" || query.Get("to") != "zh-Hans" || query.Get("api-version") != "3.0" || query.Get("includeSentenceLength") != "true" {
		t.Fatalf("query = %v, want from=en to=zh-Hans api-version=3.0 includeSentenceLength=true", query)
	}
}

func TestBingTranslatorRefreshesTokenAfterUnauthorized(t *testing.T) {
	authCalls := 0
	translateCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/translate/auth":
			authCalls++
			if authCalls == 1 {
				_, _ = w.Write([]byte("expired-token"))
				return
			}
			_, _ = w.Write([]byte("fresh-token"))
		case "/translate":
			translateCalls++
			if translateCalls == 1 {
				if got := r.Header.Get("Authorization"); got != "Bearer expired-token" {
					t.Errorf("first authorization = %q, want Bearer expired-token", got)
				}
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer fresh-token" {
				t.Errorf("retry authorization = %q, want Bearer fresh-token", got)
			}
			_, _ = w.Write([]byte(`[{"translations":[{"text":"一个女孩"}]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	translator := newBingTranslator(server.URL+"/translate/auth", server.URL+"/translate", server.Client())
	translated, err := translator.translate("1girl", "en", "zh")
	if err != nil {
		t.Fatalf("bing translate returned error: %v", err)
	}
	if translated != "一个女孩" {
		t.Fatalf("translated = %q, want 一个女孩", translated)
	}
	if authCalls != 2 || translateCalls != 2 {
		t.Fatalf("auth calls = %d, translate calls = %d, want 2 and 2", authCalls, translateCalls)
	}
}

func TestBingTranslatorRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/translate/auth" {
			_, _ = w.Write([]byte("bing-token"))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	translator := newBingTranslator(server.URL+"/translate/auth", server.URL+"/translate", server.Client())
	if _, err := translator.translate("1girl", "en", "zh"); err == nil || !strings.Contains(err.Error(), "返回格式异常") {
		t.Fatalf("error = %v, want 返回格式异常", err)
	}
}

func TestYoudaoTranslatorSendsFormAndMapsChinese(t *testing.T) {
	form := url.Values{}
	contentType := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		form = r.PostForm
		contentType = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"errorCode":0,"translation":["一个女孩"]}`))
	}))
	defer server.Close()

	translator := newYoudaoTranslator(server.URL, server.Client())
	translated, err := translator.translate("1girl", "en", "zh")
	if err != nil {
		t.Fatalf("youdao translate returned error: %v", err)
	}
	if translated != "一个女孩" {
		t.Fatalf("translated = %q, want 一个女孩", translated)
	}
	if contentType != "application/x-www-form-urlencoded" {
		t.Fatalf("content type = %q, want application/x-www-form-urlencoded", contentType)
	}
	if form.Get("q") != "1girl" || form.Get("from") != "en" || form.Get("to") != "zh-CHS" {
		t.Fatalf("form = %v, want q=1girl from=en to=zh-CHS", form)
	}
}

func TestYoudaoTranslatorRejectsLongTextAndUpstreamError(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"errorCode":"108","translation":[]}`))
	}))
	defer server.Close()

	translator := newYoudaoTranslator(server.URL, server.Client())
	if _, err := translator.translate(strings.Repeat("a", youdaoTranslationMaxRunes+1), "en", "zh"); err == nil || !strings.Contains(err.Error(), "1000") {
		t.Fatalf("error = %v, want length limit error", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0 for over-limit text", requests)
	}
	if _, err := translator.translate("1girl", "en", "zh"); err == nil || !strings.Contains(err.Error(), "108") {
		t.Fatalf("error = %v, want upstream error code 108", err)
	}
}
