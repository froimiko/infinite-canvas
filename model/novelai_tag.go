package model

const (
	PromptTagDatabaseDefaultOwner  = "weilin9999"
	PromptTagDatabaseDefaultRepo   = "WeiLin-Comfyui-Tools-Prompt"
	PromptTagDatabaseDefaultBranch = "master"
)

// PromptTagPackageType 标识 WeiLin 提示词数据库 SQL 包类型。
type PromptTagPackageType string

const (
	PromptTagPackageTypeTags     PromptTagPackageType = "tags"
	PromptTagPackageTypeDanbooru PromptTagPackageType = "danbooru"
)

// PromptTagSource 标识补全结果来自 WeiLin tag 数据库还是 Danbooru 数据库。
type PromptTagSource string

const (
	PromptTagSourceTags     PromptTagSource = "tags"
	PromptTagSourceDanbooru PromptTagSource = "danbooru"
)

// PromptTagDatabaseSetting 是管理员私有设置中的提示词数据库配置。
// 第一版固定使用 weilin9999/WeiLin-Comfyui-Tools-Prompt/master，字段保留用于后台展示和 normalize。
type PromptTagDatabaseSetting struct {
	Enabled  *bool                      `json:"enabled"`
	Owner    string                     `json:"owner"`
	Repo     string                     `json:"repo"`
	Branch   string                     `json:"branch"`
	Packages []PromptTagDatabasePackage `json:"packages,omitempty"`
}

// PromptTagDatabasePackage 描述管理员可选择或已选择的 WeiLin SQL 包。
type PromptTagDatabasePackage = PromptTagPackage

// PromptTagPackage 描述 WeiLin Prompt 仓库中的一个 SQL 包。
type PromptTagPackage struct {
	Type        PromptTagPackageType `json:"type"`
	Kind        string               `json:"kind,omitempty"` // GitHub contents API: file / dir
	Path        string               `json:"path"`
	Name        string               `json:"name"`
	SHA         string               `json:"sha,omitempty"`
	Size        int64                `json:"size,omitempty"`
	DownloadURL string               `json:"downloadUrl,omitempty"`
	Installed   bool                 `json:"installed,omitempty"`
	InstalledAt string               `json:"installedAt,omitempty"`
	Error       string               `json:"error,omitempty"`
}

// PromptTagTag 对应 WeiLin tag_tags 表。
type PromptTagTag struct {
	IDIndex    int64  `json:"idIndex" gorm:"column:id_index;primaryKey;autoIncrement"`
	SubgroupID int64  `json:"subgroupId" gorm:"column:subgroup_id;index"`
	Text       string `json:"text" gorm:"column:text;index:idx_tag_tags_text"`
	Desc       string `json:"desc" gorm:"column:desc;index:idx_tag_tags_desc"`
	Color      string `json:"color" gorm:"column:color"`
	CreateTime int64  `json:"createTime" gorm:"column:create_time;index:idx_tag_tags_create_time"`
	TUUID      string `json:"tUuid" gorm:"column:t_uuid;size:128;index"`
	GUUID      string `json:"gUuid" gorm:"column:g_uuid;size:128;index"`
}

func (PromptTagTag) TableName() string {
	return "tag_tags"
}

// PromptDanbooruTag 对应 WeiLin danbooru_tag 表。
type PromptDanbooruTag struct {
	IDIndex   int64  `json:"idIndex" gorm:"column:id_index;primaryKey;autoIncrement"`
	Tag       string `json:"tag" gorm:"column:tag;index:idx_danbooru_tag_tag"`
	ColorID   int64  `json:"colorId" gorm:"column:color_id;index"`
	Translate string `json:"translate" gorm:"column:translate;index:idx_danbooru_tag_translate"`
	Hot       int64  `json:"hot" gorm:"column:hot;default:0;index:idx_danbooru_tag_hot"`
	Aliases   int64  `json:"aliases" gorm:"column:aliases;default:0;index:idx_danbooru_tag_aliases"`
}

func (PromptDanbooruTag) TableName() string {
	return "danbooru_tag"
}

// PromptTagInstalledPackage 记录已安装 SQL 包，避免重复执行同一 WeiLin 数据包。
type PromptTagInstalledPackage struct {
	Path        string               `json:"path" gorm:"primaryKey;size:512"`
	Type        PromptTagPackageType `json:"type" gorm:"index;size:32"`
	SourceOwner string               `json:"sourceOwner" gorm:"size:128"`
	SourceRepo  string               `json:"sourceRepo" gorm:"size:256"`
	Branch      string               `json:"branch" gorm:"size:128"`
	SHA         string               `json:"sha" gorm:"size:128"`
	Size        int64                `json:"size"`
	InstalledAt string               `json:"installedAt" gorm:"index"`
	UpdatedAt   string               `json:"updatedAt"`
	Error       string               `json:"error"`
}

func (PromptTagInstalledPackage) TableName() string {
	return "prompt_tag_installed_packages"
}

// PromptTagEntry 是前端 autocomplete/translation 使用的统一 tag 记录。
type PromptTagEntry struct {
	IDIndex     int64           `json:"idIndex"`
	Source      PromptTagSource `json:"source"`
	Text        string          `json:"text"`
	Translation string          `json:"translation,omitempty"`
	Color       string          `json:"color,omitempty"`
	ColorID     int64           `json:"colorId,omitempty"`
	Hot         int64           `json:"hot,omitempty"`
	Aliases     int64           `json:"aliases,omitempty"`
	SubgroupID  int64           `json:"subgroupId,omitempty"`
	CreateTime  int64           `json:"createTime,omitempty"`
	TUUID       string          `json:"tUuid,omitempty"`
	GUUID       string          `json:"gUuid,omitempty"`
}

// PromptTagSearchResult 是提示词 tag 搜索结果。
type PromptTagSearchResult struct {
	PromptTagEntry
	Score int   `json:"score"`
	Count int64 `json:"count"`
}

// PromptTagSearchQuery 是后续 autocomplete API 的查询参数模型。
type PromptTagSearchQuery struct {
	Keyword string            `json:"keyword"`
	Limit   int               `json:"limit"`
	Sources []PromptTagSource `json:"sources,omitempty"`
}

// PromptTagDatabaseStatus 汇总本地提示词数据库状态。
type PromptTagDatabaseStatus struct {
	Enabled           bool                        `json:"enabled"`
	Owner             string                      `json:"owner"`
	Repo              string                      `json:"repo"`
	Branch            string                      `json:"branch"`
	TagCount          int64                       `json:"tagCount"`
	DanbooruTagCount  int64                       `json:"danbooruTagCount"`
	InstalledPackages []PromptTagInstalledPackage `json:"installedPackages"`
	LastInstalledAt   string                      `json:"lastInstalledAt,omitempty"`
	LastError         string                      `json:"lastError,omitempty"`
}
