package model

// PromptTranslator identifies the translation mode used by the prompt editor.
type PromptTranslator string

const (
	PromptTranslatorLibrary PromptTranslator = "library"
)

// PromptTranslationService identifies a supported network translation service.
type PromptTranslationService string

const (
	PromptTranslationServiceAlibaba PromptTranslationService = "alibaba"
	PromptTranslationServiceBing    PromptTranslationService = "bing"
	PromptTranslationServiceYoudao  PromptTranslationService = "youdao"

	DefaultPromptTranslator                 = PromptTranslatorLibrary
	DefaultPromptTranslationService         = PromptTranslationServiceAlibaba
	DefaultPromptTranslationSourceLanguage = "en"
	DefaultPromptTranslationTargetLanguage = "zh"
)

// PromptTranslationSetting is the private network translation configuration.
type PromptTranslationSetting struct {
	Enabled        *bool                    `json:"enabled"`
	Translator     PromptTranslator         `json:"translator"`
	Service        PromptTranslationService `json:"service"`
	SourceLanguage string                   `json:"sourceLanguage"`
	TargetLanguage string                   `json:"targetLanguage"`
}
