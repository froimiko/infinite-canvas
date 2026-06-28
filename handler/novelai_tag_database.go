package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/basketikun/infinite-canvas/model"
	"github.com/basketikun/infinite-canvas/service"
)

type promptTagDatabaseTreeRequest struct {
	Path string `json:"path"`
}

type promptTagAutocompleteRequest struct {
	Keyword string                  `json:"keyword"`
	Limit   int                     `json:"limit"`
	Sources []model.PromptTagSource `json:"sources"`
}

type promptTagTranslationsRequest struct {
	Tags []string `json:"tags"`
}

type promptTagDatabaseInstallRequest = service.PromptTagInstallRequest
type promptTagTranslationDatabaseInstallRequest = model.PromptTagTranslationInstallRequest

type promptTagTranslationDatabaseInstallRequest = model.PromptTagTranslationInstallRequest

func AdminPromptTagDatabaseStatus(w http.ResponseWriter, r *http.Request) {
	status, err := service.PromptTagDatabaseStatus()
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, status)
}

func AdminPromptTagDatabaseMainTree(w http.ResponseWriter, r *http.Request) {
	items, err := service.PromptTagDatabaseMainTree()
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, items)
}

func AdminPromptTagDatabaseTree(w http.ResponseWriter, r *http.Request) {
	var request promptTagDatabaseTreeRequest
	_ = json.NewDecoder(r.Body).Decode(&request)
	items, err := service.PromptTagDatabaseTree(request.Path)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, items)
}

func AdminInstallPromptTagDatabasePackages(w http.ResponseWriter, r *http.Request) {
	var request promptTagDatabaseInstallRequest
	_ = json.NewDecoder(r.Body).Decode(&request)
	result, err := service.InstallPromptTagDatabasePackages(request)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, result)
}

func AdminPromptTagTranslationDatabaseStatus(w http.ResponseWriter, r *http.Request) {
	status, err := service.PromptTagTranslationDatabaseStatus()
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, status)
}

func AdminPromptTagTranslationDatabaseAssets(w http.ResponseWriter, r *http.Request) {
	items, err := service.PromptTagTranslationDatabaseAssets()
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, items)
}

func AdminInstallPromptTagTranslationDatabasePackage(w http.ResponseWriter, r *http.Request) {
	var request promptTagTranslationDatabaseInstallRequest
	_ = json.NewDecoder(r.Body).Decode(&request)
	result, err := service.InstallPromptTagTranslationDatabasePackage(request)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, result)
}

func PromptTagAutocomplete(w http.ResponseWriter, r *http.Request) {
	query := promptTagSearchQueryFromRequest(r)
	items, err := service.SearchPromptTags(query)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, items)
}

func PromptTagTranslations(w http.ResponseWriter, r *http.Request) {
	var request promptTagTranslationsRequest
	_ = json.NewDecoder(r.Body).Decode(&request)
	translations, err := service.TranslatePromptTags(request.Tags)
	if err != nil {
		FailError(w, err)
		return
	}
	OK(w, translations)
}

func promptTagSearchQueryFromRequest(r *http.Request) model.PromptTagSearchQuery {
	var request promptTagAutocompleteRequest
	_ = json.NewDecoder(r.Body).Decode(&request)
	params := r.URL.Query()
	if keyword := strings.TrimSpace(params.Get("keyword")); keyword != "" {
		request.Keyword = keyword
	}
	if limitValue := strings.TrimSpace(params.Get("limit")); limitValue != "" {
		if limit, err := strconv.Atoi(limitValue); err == nil {
			request.Limit = limit
		}
	}
	if sourceValues, ok := params["sources"]; ok {
		request.Sources = promptTagSourcesFromValues(sourceValues)
	}
	return model.PromptTagSearchQuery{
		Keyword: strings.TrimSpace(request.Keyword),
		Limit:   request.Limit,
		Sources: request.Sources,
	}
}

func promptTagSourcesFromValues(values []string) []model.PromptTagSource {
	sources := make([]model.PromptTagSource, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == string(model.PromptTagSourceTags) || part == string(model.PromptTagSourceDanbooru) {
				sources = append(sources, model.PromptTagSource(part))
			}
		}
	}
	return sources
}
