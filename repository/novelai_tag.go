package repository

import (
	"strings"

	"github.com/basketikun/infinite-canvas/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const defaultPromptTagSearchLimit = 20

// ensurePromptTagSchema relies on GORM AutoMigrate/index tags for cross-dialect indexes.
// It intentionally does not download or execute remote SQL; installation is handled by admin-only install APIs.
func ensurePromptTagSchema(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.PromptTagGroup{},
		&model.PromptTagSubgroup{},
		&model.PromptTagTag{},
		&model.PromptDanbooruTag{},
		&model.PromptTagInstalledPackage{},
		&model.PromptTagExternalTranslation{},
		&model.PromptTagTranslationInstalledPackage{},
	)
}

// PromptTagDatabaseStatus returns local database counts and installed package records.
func PromptTagDatabaseStatus(setting model.PromptTagDatabaseSetting) (model.PromptTagDatabaseStatus, error) {
	db, err := DB()
	if err != nil {
		return model.PromptTagDatabaseStatus{}, err
	}

	var tagCount int64
	if err := db.Model(&model.PromptTagTag{}).Count(&tagCount).Error; err != nil {
		return model.PromptTagDatabaseStatus{}, err
	}
	var danbooruTagCount int64
	if err := db.Model(&model.PromptDanbooruTag{}).Count(&danbooruTagCount).Error; err != nil {
		return model.PromptTagDatabaseStatus{}, err
	}
	packages, err := ListPromptTagInstalledPackages()
	if err != nil {
		return model.PromptTagDatabaseStatus{}, err
	}

	status := model.PromptTagDatabaseStatus{
		Enabled:           setting.Enabled == nil || *setting.Enabled,
		Owner:             setting.Owner,
		Repo:              setting.Repo,
		Branch:            setting.Branch,
		TagCount:          tagCount,
		DanbooruTagCount:  danbooruTagCount,
		InstalledPackages: packages,
	}
	for _, item := range packages {
		if item.InstalledAt > status.LastInstalledAt {
			status.LastInstalledAt = item.InstalledAt
		}
		if strings.TrimSpace(item.Error) != "" {
			status.LastError = item.Error
		}
	}
	return status, nil
}

func PromptTagTranslationDatabaseStatus(setting model.PromptTagTranslationDatabaseSetting) (model.PromptTagTranslationDatabaseStatus, error) {
	db, err := DB()
	if err != nil {
		return model.PromptTagTranslationDatabaseStatus{}, err
	}
	var translationCount int64
	if err := db.Model(&model.PromptTagExternalTranslation{}).Count(&translationCount).Error; err != nil {
		return model.PromptTagTranslationDatabaseStatus{}, err
	}
	packages, err := ListPromptTagTranslationInstalledPackages()
	if err != nil {
		return model.PromptTagTranslationDatabaseStatus{}, err
	}
	status := model.PromptTagTranslationDatabaseStatus{
		Enabled:           setting.Enabled,
		Owner:             setting.Owner,
		Repo:              setting.Repo,
		TranslationCount:  translationCount,
		InstalledPackages: packages,
	}
	for _, item := range packages {
		if strings.TrimSpace(item.Error) == "" && item.ReleaseTag > status.ReleaseTag {
			status.ReleaseTag = item.ReleaseTag
		}
		if item.InstalledAt > status.LastInstalledAt {
			status.LastInstalledAt = item.InstalledAt
		}
		if strings.TrimSpace(item.Error) != "" {
			status.LastError = item.Error
		}
	}
	return status, nil
}

// ListPromptTagInstalledPackages returns installed WeiLin SQL package records ordered by install time.
func ListPromptTagInstalledPackages() ([]model.PromptTagInstalledPackage, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	items := []model.PromptTagInstalledPackage{}
	if err := db.Order("installed_at desc, path asc").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

func ListPromptTagTranslationInstalledPackages() ([]model.PromptTagTranslationInstalledPackage, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	items := []model.PromptTagTranslationInstalledPackage{}
	if err := db.Order("installed_at desc, asset_name asc").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// PromptTagPackageInstalled checks whether a WeiLin SQL package path has already been installed.
func PromptTagPackageInstalled(path string) (bool, error) {
	db, err := DB()
	if err != nil {
		return false, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	var count int64
	if err := db.Model(&model.PromptTagInstalledPackage{}).Where("path = ?", path).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// SavePromptTagInstalledPackage upserts an installed SQL package record.
func SavePromptTagInstalledPackage(item model.PromptTagInstalledPackage) (model.PromptTagInstalledPackage, error) {
	db, err := DB()
	if err != nil {
		return item, err
	}
	item.Path = strings.TrimSpace(item.Path)
	if item.Path == "" {
		return item, nil
	}
	item.Type = promptTagPackageTypeForPath(item.Type, item.Path)
	err = db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "path"}},
		DoUpdates: clause.AssignmentColumns([]string{"type", "source_owner", "source_repo", "branch", "sha", "size", "installed_at", "updated_at", "error"}),
	}).Create(&item).Error
	return item, err
}

func SavePromptTagTranslationInstalledPackage(item model.PromptTagTranslationInstalledPackage) (model.PromptTagTranslationInstalledPackage, error) {
	db, err := DB()
	if err != nil {
		return item, err
	}
	item.AssetName = strings.TrimSpace(item.AssetName)
	if item.AssetName == "" {
		return item, nil
	}
	err = db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "asset_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_owner", "source_repo", "release_tag", "size", "installed_at", "updated_at", "error"}),
	}).Create(&item).Error
	return item, err
}

func UpsertPromptTagExternalTranslations(items []model.PromptTagExternalTranslation) error {
	if len(items) == 0 {
		return nil
	}
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"normalized_name", "category", "cn_name", "post_count", "source_owner", "source_repo", "release_tag", "asset_name", "updated_at"}),
	}).CreateInBatches(items, 1000).Error
}

// DeletePromptTagInstalledPackage removes an installed package record by path.
func DeletePromptTagInstalledPackage(path string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	return db.Delete(&model.PromptTagInstalledPackage{}, "path = ?", path).Error
}

// ExecutePromptTagSQL executes a selected WeiLin SQL package against the local database.
// The caller is responsible for restricting source repository and SQL path.
func ExecutePromptTagSQL(sqlContent string) error {
	db, err := DB()
	if err != nil {
		return err
	}
	return db.Exec(sqlContent).Error
}

// SearchPromptTags provides a lightweight local autocomplete query over tag_tags and danbooru_tag.
func SearchPromptTags(query model.PromptTagSearchQuery) ([]model.PromptTagSearchResult, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	keyword := strings.TrimSpace(query.Keyword)
	limit := normalizePromptTagSearchLimit(query.Limit)
	results := []model.PromptTagSearchResult{}

	if promptTagSourceEnabled(query.Sources, model.PromptTagSourceTags) {
		items, err := searchPromptTagTags(db, keyword, limit)
		if err != nil {
			return nil, err
		}
		results = append(results, items...)
	}
	if len(results) < limit && promptTagSourceEnabled(query.Sources, model.PromptTagSourceDanbooru) {
		items, err := searchPromptDanbooruTags(db, keyword, limit-len(results))
		if err != nil {
			return nil, err
		}
		results = append(results, items...)
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// TranslatePromptTags returns local translations for the provided tag texts.
func TranslatePromptTags(tags []string, externalEnabled bool) (map[string]string, error) {
	db, err := DB()
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, tag := range tags {
		key := strings.TrimSpace(tag)
		if key == "" || result[key] != "" {
			continue
		}
		translation, err := translatePromptTag(db, key, externalEnabled)
		if err != nil {
			return nil, err
		}
		if translation != "" {
			result[key] = translation
		}
	}
	return result, nil
}

func searchPromptTagTags(db *gorm.DB, keyword string, limit int) ([]model.PromptTagSearchResult, error) {
	items := []model.PromptTagTag{}
	tx := db.Model(&model.PromptTagTag{})
	if keyword != "" {
		like := "%" + keyword + "%"
		tx = tx.Where("text LIKE ? OR "+quotePromptTagColumn(db, "desc")+" LIKE ?", like, like)
	}
	if err := tx.Order("create_time desc, id_index asc").Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	results := make([]model.PromptTagSearchResult, 0, len(items))
	for _, item := range items {
		results = append(results, model.PromptTagSearchResult{
			PromptTagEntry: model.PromptTagEntry{
				IDIndex:     item.IDIndex,
				Source:      model.PromptTagSourceTags,
				Text:        item.Text,
				Translation: item.Desc,
				Color:       item.Color,
				SubgroupID:  item.SubgroupID,
				CreateTime:  item.CreateTime,
				TUUID:       item.TUUID,
				GUUID:       item.GUUID,
			},
			Score: promptTagMatchScore(keyword, item.Text, item.Desc),
			Count: item.CreateTime,
		})
	}
	return results, nil
}

func searchPromptDanbooruTags(db *gorm.DB, keyword string, limit int) ([]model.PromptTagSearchResult, error) {
	items := []model.PromptDanbooruTag{}
	tx := db.Model(&model.PromptDanbooruTag{})
	if keyword != "" {
		like := "%" + keyword + "%"
		tx = tx.Where("tag LIKE ? OR translate LIKE ?", like, like)
	}
	if err := tx.Order("hot desc, aliases desc, id_index asc").Limit(limit).Find(&items).Error; err != nil {
		return nil, err
	}
	results := make([]model.PromptTagSearchResult, 0, len(items))
	for _, item := range items {
		results = append(results, model.PromptTagSearchResult{
			PromptTagEntry: model.PromptTagEntry{
				IDIndex:     item.IDIndex,
				Source:      model.PromptTagSourceDanbooru,
				Text:        item.Tag,
				Translation: item.Translate,
				ColorID:     item.ColorID,
				Hot:         item.Hot,
				Aliases:     item.Aliases,
			},
			Score: promptTagMatchScore(keyword, item.Tag, item.Translate),
			Count: item.Hot,
		})
	}
	return results, nil
}

func translatePromptTag(db *gorm.DB, tag string, externalEnabled bool) (string, error) {
	variants := promptTagTranslationVariants(tag)
	var tagItem model.PromptTagTag
	err := db.Where("text IN ?", variants).Order("id_index asc").First(&tagItem).Error
	if err == nil && strings.TrimSpace(tagItem.Desc) != "" {
		return tagItem.Desc, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return "", err
	}

	var danbooruItem model.PromptDanbooruTag
	err = db.Where("tag IN ?", variants).Order("hot desc, id_index asc").First(&danbooruItem).Error
	if err == nil && strings.TrimSpace(danbooruItem.Translate) != "" {
		return danbooruItem.Translate, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return "", err
	}
	if !externalEnabled {
		return "", nil
	}
	var external model.PromptTagExternalTranslation
	err = db.Where("name IN ? OR normalized_name IN ?", variants, variants).Order("post_count desc, name asc").First(&external).Error
	if err == nil {
		return strings.TrimSpace(external.CNName), nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return "", err
	}
	return "", nil
}

func promptTagTranslationVariants(tag string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range []string{tag, strings.ToLower(tag), strings.ReplaceAll(tag, " ", "_"), normalizePromptTagName(tag)} {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func normalizePromptTagName(value string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), " ", "_")
}

func normalizePromptTagSearchLimit(limit int) int {
	if limit <= 0 {
		return defaultPromptTagSearchLimit
	}
	if limit > model.MaxPageSize {
		return model.MaxPageSize
	}
	return limit
}

func promptTagPackageTypeForPath(current model.PromptTagPackageType, path string) model.PromptTagPackageType {
	if current != "" {
		return current
	}
	path = strings.TrimLeft(strings.ToLower(strings.TrimSpace(path)), "/")
	if strings.HasPrefix(path, "danbooru/") {
		return model.PromptTagPackageTypeDanbooru
	}
	return model.PromptTagPackageTypeTags
}

func promptTagSourceEnabled(sources []model.PromptTagSource, source model.PromptTagSource) bool {
	if len(sources) == 0 {
		return true
	}
	for _, item := range sources {
		if item == source {
			return true
		}
	}
	return false
}

func quotePromptTagColumn(db *gorm.DB, column string) string {
	switch db.Dialector.Name() {
	case "mysql":
		return "`" + column + "`"
	case "postgres", "sqlite":
		return "\"" + column + "\""
	default:
		return column
	}
}

func promptTagMatchScore(keyword string, values ...string) int {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return 0
	}
	best := 0
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		score := 0
		switch {
		case value == keyword:
			score = 100
		case strings.HasPrefix(value, keyword):
			score = 80
		case strings.Contains(value, keyword):
			score = 50
		}
		if score > best {
			best = score
		}
	}
	return best
}
