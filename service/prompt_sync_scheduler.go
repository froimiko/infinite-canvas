package service

import (
	"log"
	"sync"

	"github.com/basketikun/infinite-canvas/model"
	"github.com/basketikun/infinite-canvas/repository"
	"github.com/robfig/cron/v3"
)

const defaultPromptSyncCron = "*/5 * * * *"

var (
	promptSyncCron *cron.Cron
	promptSyncOnce sync.Once
	promptSyncMu   sync.Mutex
)

func StartPromptSyncScheduler() {
	promptSyncOnce.Do(func() {
		promptSyncCron = cron.New()
		promptSyncCron.Start()
	})
	RefreshPromptSyncScheduler()
}

func RefreshPromptSyncScheduler() {
	promptSyncMu.Lock()
	defer promptSyncMu.Unlock()
	if promptSyncCron == nil {
		return
	}
	for _, entry := range promptSyncCron.Entries() {
		promptSyncCron.Remove(entry.ID)
	}
	settings, err := repository.GetSettings()
	if err != nil {
		log.Printf("load prompt sync setting failed err=%v", err)
		return
	}
	setting := normalizePromptSyncSetting(settings.Private.PromptSync)
	if setting.Enabled == nil || !*setting.Enabled {
		return
	}
	if _, err := promptSyncCron.AddFunc(setting.Cron, safeSyncRemotePromptCategories); err != nil {
		log.Printf("add prompt sync cron failed cron=%s err=%v", setting.Cron, err)
	}
}

// safeSyncRemotePromptCategories 是给 cron 用的带兜底版本。
//
// ⚠️ robfig/cron 在自己的 goroutine 里执行 job，**它不会 recover panic**
// （cron.New() 未配置 Recover 包装器时，job 里的 panic 会直接冒泡杀死进程）。
// 这个任务默认每 5 分钟跑一次、会去抓外部 GitHub 内容，是典型的
// 「跑久了才偶发出错」来源 —— 正好符合线上「运行一段时间后 8080 无人监听」的现象。
func safeSyncRemotePromptCategories() {
	defer RecoverPanic("prompt-sync-cron")
	SyncRemotePromptCategories()
}

func SyncRemotePromptCategories() {
	for _, category := range repository.PromptCategories() {
		if !category.Remote {
			continue
		}
		log.Printf("scheduled prompt sync start category=%s", category.Category)
		if _, err := SyncPromptCategory(category.Category); err != nil {
			log.Printf("scheduled prompt sync failed category=%s err=%v", category.Category, err)
			continue
		}
		log.Printf("scheduled prompt sync done category=%s", category.Category)
	}
}

func normalizePromptSyncSetting(setting model.PromptSyncSetting) model.PromptSyncSetting {
	if setting.Cron == "" {
		setting.Cron = defaultPromptSyncCron
	}
	if setting.Enabled == nil {
		enabled := true
		setting.Enabled = &enabled
	}
	return setting
}
