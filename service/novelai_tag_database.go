package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/basketikun/infinite-canvas/model"
	"github.com/basketikun/infinite-canvas/repository"
)

const (
	promptTagGitHubAPIBase   = "https://api.github.com"
	promptTagRawBase         = "https://raw.githubusercontent.com"
	promptTagGitHubUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	maxPromptTagSQLBytes     = 64 * 1024 * 1024
)

var promptTagHTTPClient = &http.Client{Timeout: 60 * time.Second}

type promptTagGitHubTreeResponse struct {
	Tree []promptTagGitHubTreeItem `json:"tree"`
}

type promptTagGitHubTreeItem struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

type promptTagGitHubContentItem struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	SHA         string `json:"sha"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"download_url"`
}

type PromptTagInstallRequest struct {
	Type  model.PromptTagPackageType `json:"type"`
	Paths []string                   `json:"paths"`
}

type PromptTagInstallResult struct {
	Installed []model.PromptTagInstalledPackage `json:"installed"`
	Skipped   []model.PromptTagInstalledPackage `json:"skipped"`
	Failed    []model.PromptTagInstalledPackage `json:"failed"`
	Status    model.PromptTagDatabaseStatus     `json:"status"`
}

func PromptTagDatabaseStatus() (model.PromptTagDatabaseStatus, error) {
	settings, err := repository.GetSettings()
	if err != nil {
		return model.PromptTagDatabaseStatus{}, err
	}
	return repository.PromptTagDatabaseStatus(normalizePrivateSetting(settings.Private).PromptTagDatabase)
}

func PromptTagDatabaseMainTree() ([]model.PromptTagPackage, error) {
	if _, err := promptTagDatabaseSetting(); err != nil {
		return nil, err
	}
	// WeiLin 的根目录固定只有 tags / danbooru。这里避免每次打开后台都请求
	// GitHub git tree API，降低匿名 API 403 / rate limit 对入口页的影响。
	return []model.PromptTagPackage{
		{Type: model.PromptTagPackageTypeTags, Kind: "dir", Path: "tags", Name: "tags"},
		{Type: model.PromptTagPackageTypeDanbooru, Kind: "dir", Path: "danbooru", Name: "danbooru"},
	}, nil
}

func PromptTagDatabaseTree(treePath string) ([]model.PromptTagPackage, error) {
	setting, err := promptTagDatabaseSetting()
	if err != nil {
		return nil, err
	}
	treePath, err = normalizePromptTagTreePath(treePath)
	if err != nil {
		return nil, err
	}
	apiURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", promptTagGitHubAPIBase, url.PathEscape(setting.Owner), url.PathEscape(setting.Repo), escapePromptTagPath(treePath), url.QueryEscape(setting.Branch))
	var payload []promptTagGitHubContentItem
	if err := fetchPromptTagGitHubJSON(apiURL, &payload); err != nil {
		return nil, err
	}
	packages := make([]model.PromptTagPackage, 0, len(payload))
	for _, item := range payload {
		pkg := model.PromptTagPackage{
			Type:        promptTagPackageTypeFromPath(item.Path),
			Kind:        item.Type,
			Path:        strings.TrimLeft(item.Path, "/"),
			Name:        item.Name,
			SHA:         item.SHA,
			Size:        item.Size,
			DownloadURL: item.DownloadURL,
		}
		if pkg.Kind == "file" && !strings.HasSuffix(strings.ToLower(pkg.Path), ".sql") {
			continue
		}
		if installed, installedPkg := promptTagInstalledPackage(pkg.Path); installed {
			pkg.Installed = true
			pkg.InstalledAt = installedPkg.InstalledAt
			pkg.Error = installedPkg.Error
		}
		packages = append(packages, pkg)
	}
	sort.SliceStable(packages, func(i, j int) bool {
		if packages[i].Kind != packages[j].Kind {
			return packages[i].Kind == "dir"
		}
		return packages[i].Name < packages[j].Name
	})
	return packages, nil
}

func InstallPromptTagDatabasePackages(request PromptTagInstallRequest) (PromptTagInstallResult, error) {
	setting, err := promptTagDatabaseSetting()
	if err != nil {
		return PromptTagInstallResult{}, err
	}
	requestedType := request.Type
	result := PromptTagInstallResult{
		Installed: []model.PromptTagInstalledPackage{},
		Skipped:   []model.PromptTagInstalledPackage{},
		Failed:    []model.PromptTagInstalledPackage{},
	}
	for _, rawPath := range request.Paths {
		packagePath, err := normalizePromptTagSQLPath(rawPath, requestedType)
		if err != nil {
			failed := promptTagInstalledPackageRecord(setting, requestedType, rawPath, "", 0, err.Error())
			result.Failed = append(result.Failed, failed)
			continue
		}
		packageType := promptTagPackageTypeFromPath(packagePath)
		if installed, installedPackage := promptTagInstalledPackage(packagePath); installed {
			result.Skipped = append(result.Skipped, installedPackage)
			continue
		}
		sqlContent, size, err := downloadPromptTagSQL(setting, packagePath)
		if err != nil {
			failed := promptTagInstalledPackageRecord(setting, packageType, packagePath, "", size, err.Error())
			result.Failed = append(result.Failed, failed)
			continue
		}
		if err := repository.ExecutePromptTagSQL(sqlContent); err != nil {
			failed := promptTagInstalledPackageRecord(setting, packageType, packagePath, "", size, err.Error())
			_, _ = repository.SavePromptTagInstalledPackage(failed)
			result.Failed = append(result.Failed, failed)
			continue
		}
		installed := promptTagInstalledPackageRecord(setting, packageType, packagePath, "", size, "")
		saved, err := repository.SavePromptTagInstalledPackage(installed)
		if err != nil {
			installed.Error = err.Error()
			result.Failed = append(result.Failed, installed)
			continue
		}
		result.Installed = append(result.Installed, saved)
	}
	status, err := repository.PromptTagDatabaseStatus(setting)
	if err != nil {
		return result, err
	}
	result.Status = status
	return result, nil
}

func SearchPromptTags(query model.PromptTagSearchQuery) ([]model.PromptTagSearchResult, error) {
	setting, err := promptTagDatabaseSettingForQuery()
	if err != nil {
		return nil, err
	}
	if setting.Enabled != nil && !*setting.Enabled {
		return []model.PromptTagSearchResult{}, nil
	}
	return repository.SearchPromptTags(query)
}

func TranslatePromptTags(tags []string) (map[string]string, error) {
	setting, err := promptTagDatabaseSettingForQuery()
	if err != nil {
		return nil, err
	}
	if setting.Enabled != nil && !*setting.Enabled {
		return map[string]string{}, nil
	}
	return repository.TranslatePromptTags(tags)
}

func promptTagDatabaseSetting() (model.PromptTagDatabaseSetting, error) {
	setting, err := promptTagDatabaseSettingForQuery()
	if err != nil {
		return model.PromptTagDatabaseSetting{}, err
	}
	if setting.Owner != model.PromptTagDatabaseDefaultOwner || setting.Repo != model.PromptTagDatabaseDefaultRepo || setting.Branch != model.PromptTagDatabaseDefaultBranch {
		return model.PromptTagDatabaseSetting{}, safeMessageError{message: "提示词数据库第一版仅允许使用 WeiLin 官方 Prompt 仓库"}
	}
	return setting, nil
}

func promptTagDatabaseSettingForQuery() (model.PromptTagDatabaseSetting, error) {
	settings, err := repository.GetSettings()
	if err != nil {
		return model.PromptTagDatabaseSetting{}, err
	}
	return normalizePrivateSetting(settings.Private).PromptTagDatabase, nil
}

func fetchPromptTagGitHubJSON(apiURL string, target any) error {
	request, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	applyPromptTagGitHubHeaders(request, true)
	response, err := promptTagHTTPClient.Do(request)
	if err != nil {
		return safeMessageError{message: "读取 WeiLin 数据库仓库失败：网络不可达"}
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode >= http.StatusBadRequest {
		return safeMessageError{message: promptTagGitHubErrorMessage("读取 WeiLin 数据库仓库失败", response, body)}
	}
	if err := json.Unmarshal(body, target); err != nil {
		return safeMessageError{message: "读取 WeiLin 数据库仓库失败：返回格式异常"}
	}
	return nil
}

func downloadPromptTagSQL(setting model.PromptTagDatabaseSetting, packagePath string) (string, int64, error) {
	rawURL := fmt.Sprintf("%s/%s/%s/%s/%s", promptTagRawBase, url.PathEscape(setting.Owner), url.PathEscape(setting.Repo), url.PathEscape(setting.Branch), escapePromptTagPath(packagePath))
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	applyPromptTagGitHubHeaders(request, false)
	response, err := promptTagHTTPClient.Do(request)
	if err != nil {
		return "", 0, safeMessageError{message: "下载 WeiLin SQL 失败：网络不可达"}
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(response.Body)
		return "", 0, safeMessageError{message: promptTagGitHubErrorMessage("下载 WeiLin SQL 失败", response, body)}
	}
	reader := io.LimitReader(response.Body, maxPromptTagSQLBytes+1)
	body, err := io.ReadAll(reader)
	if err != nil {
		return "", int64(len(body)), err
	}
	if int64(len(body)) > maxPromptTagSQLBytes {
		return "", int64(len(body)), safeMessageError{message: "下载 WeiLin SQL 失败：文件过大"}
	}
	return string(body), int64(len(body)), nil
}

func applyPromptTagGitHubHeaders(request *http.Request, wantsJSON bool) {
	request.Header.Set("User-Agent", promptTagGitHubUserAgent)
	if wantsJSON {
		request.Header.Set("Accept", "application/json")
	}
	if token := promptTagGitHubToken(); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}

func promptTagGitHubToken() string {
	if token := strings.TrimSpace(os.Getenv("PROMPT_TAG_GITHUB_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

func promptTagGitHubErrorMessage(prefix string, response *http.Response, body []byte) string {
	message := fmt.Sprintf("%s：GitHub 返回 %d", prefix, response.StatusCode)
	if response.StatusCode == http.StatusForbidden {
		message += "，可能是 GitHub 匿名 API 限流或云端出口被风控；可配置 PROMPT_TAG_GITHUB_TOKEN 或 GITHUB_TOKEN 后重试"
	}
	if reset := strings.TrimSpace(response.Header.Get("X-RateLimit-Reset")); reset != "" {
		message += "，RateLimit-Reset=" + reset
	}
	if len(body) > 0 {
		text := strings.TrimSpace(string(body))
		if len(text) > 180 {
			text = text[:180] + "..."
		}
		if text != "" {
			message += "，响应：" + text
		}
	}
	return message
}

func normalizePromptTagTreePath(value string) (string, error) {
	value = path.Clean(strings.TrimLeft(strings.TrimSpace(value), "/"))
	if value == "." {
		value = ""
	}
	if value != "tags" && value != "danbooru" && !strings.HasPrefix(value, "tags/") && !strings.HasPrefix(value, "danbooru/") {
		return "", safeMessageError{message: "仅允许浏览 WeiLin tags/danbooru 目录"}
	}
	if strings.Contains(value, "..") {
		return "", safeMessageError{message: "数据库路径不合法"}
	}
	return value, nil
}

func normalizePromptTagSQLPath(value string, packageType model.PromptTagPackageType) (string, error) {
	value = path.Clean(strings.TrimLeft(strings.TrimSpace(value), "/"))
	if value == "." || strings.Contains(value, "..") || !strings.HasSuffix(strings.ToLower(value), ".sql") {
		return "", safeMessageError{message: "仅允许安装 WeiLin SQL 文件"}
	}
	if packageType == model.PromptTagPackageTypeTags && !strings.HasPrefix(value, "tags/") {
		return "", safeMessageError{message: "tags 类型只能安装 tags/ 目录下的 SQL"}
	}
	if packageType == model.PromptTagPackageTypeDanbooru && !strings.HasPrefix(value, "danbooru/") {
		return "", safeMessageError{message: "danbooru 类型只能安装 danbooru/ 目录下的 SQL"}
	}
	if !strings.HasPrefix(value, "tags/") && !strings.HasPrefix(value, "danbooru/") {
		return "", safeMessageError{message: "仅允许安装 WeiLin tags/danbooru SQL"}
	}
	return value, nil
}

func escapePromptTagPath(value string) string {
	parts := strings.Split(strings.TrimLeft(value, "/"), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func promptTagInstalledPackage(path string) (bool, model.PromptTagInstalledPackage) {
	packages, err := repository.ListPromptTagInstalledPackages()
	if err != nil {
		return false, model.PromptTagInstalledPackage{}
	}
	for _, item := range packages {
		if item.Path == path {
			return true, item
		}
	}
	return false, model.PromptTagInstalledPackage{}
}

func promptTagInstalledPackageRecord(setting model.PromptTagDatabaseSetting, packageType model.PromptTagPackageType, packagePath string, sha string, size int64, errorMessage string) model.PromptTagInstalledPackage {
	nowValue := now()
	if packageType == "" {
		packageType = promptTagPackageTypeFromPath(packagePath)
	}
	return model.PromptTagInstalledPackage{
		Path:        strings.TrimSpace(packagePath),
		Type:        packageType,
		SourceOwner: setting.Owner,
		SourceRepo:  setting.Repo,
		Branch:      setting.Branch,
		SHA:         sha,
		Size:        size,
		InstalledAt: nowValue,
		UpdatedAt:   nowValue,
		Error:       strings.TrimSpace(errorMessage),
	}
}

func PromptTagInstallHasFailure(result PromptTagInstallResult) bool {
	return len(result.Failed) > 0
}

func promptTagInstallError(result PromptTagInstallResult) error {
	if !PromptTagInstallHasFailure(result) {
		return nil
	}
	messages := make([]string, 0, len(result.Failed))
	for _, item := range result.Failed {
		if strings.TrimSpace(item.Error) != "" {
			messages = append(messages, item.Path+": "+item.Error)
		}
	}
	if len(messages) == 0 {
		return errors.New("部分 WeiLin SQL 安装失败")
	}
	return errors.New(strings.Join(messages, "; "))
}
