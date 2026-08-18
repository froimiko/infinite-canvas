package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/basketikun/infinite-canvas/model"
	"github.com/basketikun/infinite-canvas/repository"
)

const (
	promptTranslationTimeout      = 15 * time.Second
	promptTranslationMaxBodyBytes = 1 << 20
	promptTranslationMaxRunes     = 5000
	promptTranslationMaxLines     = 50
	youdaoTranslationMaxRunes     = 1000
	bingTranslationTokenTTL       = 3 * time.Hour
	promptTranslationUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/116.0.0.0 Safari/537.36"
)

// TranslatePromptText translates user text with the saved private translation setting.
func TranslatePromptText(text string) (string, error) {
	return TranslatePromptTextWithDirection(text, "")
}

// TranslatePromptTextWithDirection 按调用方指定的方向翻译文本。
//
// direction 只接受三种值，故意不接受裸语言码：语言对属于后台私有设置，
// 允许前端直接传语言码等于把私有配置暴露成可绕过的入参。
//
//	""/"config" —— 完全按后台配置的 源语言 → 目标语言（历史行为）
//	"reverse"   —— 交换后台配置的源/目标语言
//	"auto"      —— 文本已经是目标语言（CJK）时自动交换，否则按配置
func TranslatePromptTextWithDirection(text string, direction string) (string, error) {
	settings, err := repository.GetSettings()
	if err != nil {
		return "", err
	}
	setting := normalizePrivateSetting(settings.Private).PromptTranslation
	return translatePromptText(text, applyPromptTranslationDirection(setting, text, direction))
}

// applyPromptTranslationDirection 根据 direction 决定是否交换源/目标语言。
//
// 生图工作台的「一键翻译」用 auto：用户写中文时需要英文提示词（zh → en），
// 粘贴英文提示词时需要中文释义（en → zh），而后台只配置了一个方向。
//
// 两条硬约束：
//  1. 源语言为 auto 时永不交换 —— 交换会把目标语言变成 auto，
//     而 checkPromptTranslation 明确拒绝目标语言为 auto。
//  2. auto 只在目标语言属于 CJK 时生效。非 CJK 语言对（例如 en → fr）
//     无法靠字符集判断文本语言，一律退回配置方向，避免瞎猜。
func applyPromptTranslationDirection(setting model.PromptTranslationSetting, text string, direction string) model.PromptTranslationSetting {
	source := strings.ToLower(strings.TrimSpace(setting.SourceLanguage))
	target := strings.ToLower(strings.TrimSpace(setting.TargetLanguage))
	if source == "" || target == "" || source == "auto" {
		return setting
	}

	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "reverse":
	case "auto":
		if !isCJKLanguage(target) || !containsCJK(text) {
			return setting
		}
	default:
		return setting
	}

	setting.SourceLanguage, setting.TargetLanguage = target, source
	return setting
}

// containsCJK 判断文本里是否出现汉字/假名/谚文，用于「文本已经是中日韩」的粗判。
func containsCJK(text string) bool {
	for _, char := range text {
		switch {
		case char >= 0x3040 && char <= 0x30FF: // 平假名 / 片假名
			return true
		case char >= 0x3400 && char <= 0x4DBF: // 扩展 A 区汉字
			return true
		case char >= 0x4E00 && char <= 0x9FFF: // 基本区汉字
			return true
		case char >= 0xAC00 && char <= 0xD7AF: // 谚文音节
			return true
		case char >= 0xF900 && char <= 0xFAFF: // 兼容汉字
			return true
		}
	}
	return false
}

// isCJKLanguage 判断语言码是否属于中日韩，兼容 zh-hans / zh-chs / ja-jp 这类变体。
func isCJKLanguage(lang string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	for _, prefix := range []string{"zh", "ja", "jp", "ko", "kr", "yue"} {
		if lang == prefix || strings.HasPrefix(lang, prefix+"-") || strings.HasPrefix(lang, prefix+"_") {
			return true
		}
	}
	return false
}

// TestPromptTranslation translates text with an admin provided setting that may not be saved yet.
func TestPromptTranslation(text string, setting model.PromptTranslationSetting) (string, error) {
	return translatePromptText(text, normalizePromptTranslationSetting(setting))
}

func translatePromptText(text string, setting model.PromptTranslationSetting) (string, error) {
	text, setting, err := checkPromptTranslation(text, setting)
	if err != nil {
		return "", err
	}
	var translated string
	switch setting.Service {
	case model.PromptTranslationServiceAlibaba:
		translated, err = defaultAlibabaTranslator.translate(text, setting.SourceLanguage, setting.TargetLanguage)
	case model.PromptTranslationServiceBing:
		translated, err = defaultBingTranslator.translate(text, setting.SourceLanguage, setting.TargetLanguage)
	case model.PromptTranslationServiceYoudao:
		translated, err = defaultYoudaoTranslator.translate(text, setting.SourceLanguage, setting.TargetLanguage)
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(translated) == "" {
		return "", safeMessageError{message: "翻译失败：上游返回空结果"}
	}
	return translated, nil
}

func checkPromptTranslation(text string, setting model.PromptTranslationSetting) (string, model.PromptTranslationSetting, error) {
	if setting.Enabled != nil && !*setting.Enabled {
		return "", setting, safeMessageError{message: "网络翻译未开启"}
	}
	if setting.Translator != model.PromptTranslatorLibrary {
		return "", setting, safeMessageError{message: "暂不支持该翻译器"}
	}
	switch setting.Service {
	case model.PromptTranslationServiceAlibaba, model.PromptTranslationServiceBing, model.PromptTranslationServiceYoudao:
	default:
		return "", setting, safeMessageError{message: "不支持的翻译服务"}
	}
	setting.SourceLanguage = strings.ToLower(strings.TrimSpace(setting.SourceLanguage))
	setting.TargetLanguage = strings.ToLower(strings.TrimSpace(setting.TargetLanguage))
	if setting.SourceLanguage == "" || setting.TargetLanguage == "" {
		return "", setting, safeMessageError{message: "请先配置翻译源语言和目标语言"}
	}
	if setting.TargetLanguage == "auto" {
		return "", setting, safeMessageError{message: "目标语言不能为自动识别"}
	}
	if setting.SourceLanguage != "auto" && setting.SourceLanguage == setting.TargetLanguage {
		return "", setting, safeMessageError{message: "源语言和目标语言不能相同"}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", setting, safeMessageError{message: "翻译内容不能为空"}
	}
	if utf8.RuneCountInString(text) > promptTranslationMaxRunes {
		return "", setting, safeMessageError{message: fmt.Sprintf("翻译内容过长，最多 %d 个字符", promptTranslationMaxRunes)}
	}
	if len(strings.Split(text, "\n")) > promptTranslationMaxLines {
		return "", setting, safeMessageError{message: fmt.Sprintf("翻译行数过多，最多 %d 行", promptTranslationMaxLines)}
	}
	return text, setting, nil
}

func newPromptTranslationClient() *http.Client {
	return &http.Client{Timeout: promptTranslationTimeout}
}

func newPromptTranslationCookieClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Timeout: promptTranslationTimeout, Jar: jar}
}

func readPromptTranslationResponse(client *http.Client, request *http.Request, service string) ([]byte, int, error) {
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, safeMessageError{message: service + "翻译失败：网络不可达或超时"}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, promptTranslationMaxBodyBytes))
	if err != nil {
		return nil, response.StatusCode, safeMessageError{message: service + "翻译失败：读取响应失败"}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, response.StatusCode, safeMessageError{message: fmt.Sprintf("%s翻译失败：上游状态码 %d", service, response.StatusCode)}
	}
	return body, response.StatusCode, nil
}

var (
	alibabaLanguageResourcePattern = regexp.MustCompile(`(?:https:)?//[^"'\s]*?translation-open-portal_interface\.json|/[^"'\s]*?translation-open-portal_interface\.json`)
	alibabaLanguageCodePattern     = regexp.MustCompile(`interface\.([A-Za-z][A-Za-z\-]{1,6})":"`)
)

type alibabaTranslator struct {
	hostURL     string
	csrfURL     string
	apiURL      string
	client      *http.Client
	mutex       sync.Mutex
	headerName  string
	token       string
	languageSet bool
}

func newAlibabaTranslator(baseURL string, client *http.Client) *alibabaTranslator {
	baseURL = strings.TrimRight(baseURL, "/")
	return &alibabaTranslator{
		hostURL: baseURL,
		csrfURL: baseURL + "/api/translate/csrftoken",
		apiURL:  baseURL + "/api/translate/text",
		client:  client,
	}
}

var defaultAlibabaTranslator = newAlibabaTranslator("https://translate.alibaba.com", newPromptTranslationCookieClient())

func (translator *alibabaTranslator) translate(text, source, target string) (string, error) {
	headerName, token, err := translator.credentials(false)
	if err != nil {
		return "", err
	}
	translated, status, err := translator.requestTranslate(text, source, target, headerName, token)
	if err == nil {
		return translated, nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden && status != 419 {
		return "", err
	}
	headerName, token, err = translator.credentials(true)
	if err != nil {
		return "", err
	}
	translated, _, err = translator.requestTranslate(text, source, target, headerName, token)
	return translated, err
}

func (translator *alibabaTranslator) credentials(refresh bool) (string, string, error) {
	translator.mutex.Lock()
	defer translator.mutex.Unlock()
	if refresh {
		translator.headerName, translator.token, translator.languageSet = "", "", false
	}
	if translator.token != "" && translator.languageSet {
		return translator.headerName, translator.token, nil
	}
	if !translator.languageSet {
		if err := translator.loadLanguages(); err != nil {
			return "", "", err
		}
		translator.languageSet = true
	}
	headerName, token, err := translator.loadCSRFToken()
	if err != nil {
		return "", "", err
	}
	translator.headerName, translator.token = headerName, token
	return headerName, token, nil
}

func (translator *alibabaTranslator) loadLanguages() error {
	host, err := translator.get(translator.hostURL)
	if err != nil {
		return err
	}
	resourceURL, err := translator.languageResourceURL(string(host))
	if err != nil {
		return err
	}
	languages, err := translator.get(resourceURL)
	if err != nil {
		return err
	}
	if !alibabaLanguageCodePattern.Match(languages) {
		return safeMessageError{message: "Alibaba 语言配置格式异常"}
	}
	return nil
}

func (translator *alibabaTranslator) languageResourceURL(host string) (string, error) {
	match := alibabaLanguageResourcePattern.FindString(host)
	switch {
	case match == "":
		return "", safeMessageError{message: "Alibaba 语言配置地址解析失败"}
	case strings.HasPrefix(match, "https:"):
		return match, nil
	case strings.HasPrefix(match, "//"):
		return "https:" + match, nil
	default:
		return translator.hostURL + match, nil
	}
}

func (translator *alibabaTranslator) loadCSRFToken() (string, string, error) {
	if _, err := translator.get(translator.csrfURL); err != nil {
		return "", "", err
	}
	body, err := translator.get(translator.csrfURL)
	if err != nil {
		return "", "", err
	}
	var payload struct {
		HeaderName string `json:"headerName"`
		Token      string `json:"token"`
	}
	if json.Unmarshal(body, &payload) != nil || strings.TrimSpace(payload.HeaderName) == "" || strings.TrimSpace(payload.Token) == "" {
		return "", "", safeMessageError{message: "Alibaba 令牌获取失败"}
	}
	return payload.HeaderName, payload.Token, nil
}

func (translator *alibabaTranslator) get(target string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", promptTranslationUserAgent)
	request.Header.Set("Referer", translator.hostURL)
	body, _, err := readPromptTranslationResponse(translator.client, request, "Alibaba ")
	return body, err
}

func (translator *alibabaTranslator) requestTranslate(text, source, target, headerName, token string) (string, int, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for _, field := range [][2]string{{"query", text}, {"srcLang", source}, {"tgtLang", target}, {"_csrf", token}, {"domain", "general"}} {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return "", 0, err
		}
	}
	if err := writer.Close(); err != nil {
		return "", 0, err
	}
	request, err := http.NewRequest(http.MethodPost, translator.apiURL, body)
	if err != nil {
		return "", 0, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set(headerName, token)
	request.Header.Set("Origin", translator.origin())
	request.Header.Set("Referer", translator.hostURL)
	request.Header.Set("User-Agent", promptTranslationUserAgent)
	responseBody, status, err := readPromptTranslationResponse(translator.client, request, "Alibaba ")
	if err != nil {
		return "", status, err
	}
	var payload struct {
		Data struct {
			TranslateText string `json:"translateText"`
		} `json:"data"`
	}
	if json.Unmarshal(responseBody, &payload) != nil || strings.TrimSpace(payload.Data.TranslateText) == "" {
		return "", status, safeMessageError{message: "Alibaba 翻译失败：返回格式异常"}
	}
	return payload.Data.TranslateText, status, nil
}

func (translator *alibabaTranslator) origin() string {
	if parsed, err := url.Parse(translator.hostURL); err == nil && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return translator.hostURL
}

type bingTranslator struct {
	authURL      string
	translateURL string
	client       *http.Client
	mutex        sync.Mutex
	token        string
	tokenTime    time.Time
}

func newBingTranslator(authURL, translateURL string, client *http.Client) *bingTranslator {
	return &bingTranslator{authURL: authURL, translateURL: translateURL, client: client}
}

var defaultBingTranslator = newBingTranslator(
	"https://edge.microsoft.com/translate/auth",
	"https://api-edge.cognitive.microsofttranslator.com/translate",
	newPromptTranslationClient(),
)

func (translator *bingTranslator) translate(text, source, target string) (string, error) {
	token, err := translator.authToken(false)
	if err != nil {
		return "", err
	}
	translated, status, err := translator.requestTranslate(text, source, target, token)
	if err == nil {
		return translated, nil
	}
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		return "", err
	}
	token, err = translator.authToken(true)
	if err != nil {
		return "", err
	}
	translated, _, err = translator.requestTranslate(text, source, target, token)
	return translated, err
}

func (translator *bingTranslator) authToken(refresh bool) (string, error) {
	translator.mutex.Lock()
	defer translator.mutex.Unlock()
	if refresh {
		translator.token, translator.tokenTime = "", time.Time{}
	}
	if translator.token != "" && time.Since(translator.tokenTime) < bingTranslationTokenTTL {
		return translator.token, nil
	}
	request, err := http.NewRequest(http.MethodGet, translator.authURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", promptTranslationUserAgent)
	body, _, err := readPromptTranslationResponse(translator.client, request, "Bing ")
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		return "", safeMessageError{message: "Bing 翻译失败：获取令牌失败"}
	}
	translator.token, translator.tokenTime = token, time.Now()
	return token, nil
}

func (translator *bingTranslator) requestTranslate(text, source, target, token string) (string, int, error) {
	payload, err := json.Marshal([]map[string]string{{"Text": text}})
	if err != nil {
		return "", 0, err
	}
	query := url.Values{}
	query.Set("from", bingTranslationLanguage(source))
	query.Set("to", bingTranslationLanguage(target))
	query.Set("api-version", "3.0")
	query.Set("includeSentenceLength", "true")
	request, err := http.NewRequest(http.MethodPost, translator.translateURL+"?"+query.Encode(), bytes.NewReader(payload))
	if err != nil {
		return "", 0, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", promptTranslationUserAgent)
	body, status, err := readPromptTranslationResponse(translator.client, request, "Bing ")
	if err != nil {
		return "", status, err
	}
	var result []struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if json.Unmarshal(body, &result) != nil || len(result) == 0 || len(result[0].Translations) == 0 || strings.TrimSpace(result[0].Translations[0].Text) == "" {
		return "", status, safeMessageError{message: "Bing 翻译失败：返回格式异常"}
	}
	return result[0].Translations[0].Text, status, nil
}

// bingTranslationLanguage maps the setting language to the code accepted by Microsoft Translator.
func bingTranslationLanguage(language string) string {
	if language == "zh" {
		return "zh-Hans"
	}
	return language
}

type youdaoTranslator struct {
	translateURL string
	client       *http.Client
}

func newYoudaoTranslator(translateURL string, client *http.Client) *youdaoTranslator {
	return &youdaoTranslator{translateURL: translateURL, client: client}
}

var defaultYoudaoTranslator = newYoudaoTranslator("https://aidemo.youdao.com/trans", newPromptTranslationClient())

func (translator *youdaoTranslator) translate(text, source, target string) (string, error) {
	if utf8.RuneCountInString(text) > youdaoTranslationMaxRunes {
		return "", safeMessageError{message: fmt.Sprintf("有道翻译失败：文本长度不能超过 %d 个字符", youdaoTranslationMaxRunes)}
	}
	form := url.Values{
		"q":    {text},
		"from": {youdaoTranslationLanguage(source)},
		"to":   {youdaoTranslationLanguage(target)},
	}
	request, err := http.NewRequest(http.MethodPost, translator.translateURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", promptTranslationUserAgent)
	body, _, err := readPromptTranslationResponse(translator.client, request, "有道")
	if err != nil {
		return "", err
	}
	var payload struct {
		ErrorCode   json.RawMessage `json:"errorCode"`
		Translation []string        `json:"translation"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", safeMessageError{message: "有道翻译失败：返回格式异常"}
	}
	code := strings.Trim(strings.TrimSpace(string(payload.ErrorCode)), `"`)
	if code == "" {
		return "", safeMessageError{message: "有道翻译失败：返回格式异常"}
	}
	if code != "0" {
		return "", safeMessageError{message: fmt.Sprintf("有道翻译失败：错误码 %s", code)}
	}
	if len(payload.Translation) == 0 || strings.TrimSpace(payload.Translation[0]) == "" {
		return "", safeMessageError{message: "有道翻译失败：返回格式异常"}
	}
	return payload.Translation[0], nil
}

func youdaoTranslationLanguage(language string) string {
	switch language {
	case "zh":
		return "zh-CHS"
	case "auto":
		return "Auto"
	}
	return language
}
