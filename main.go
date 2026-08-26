package main

import (
	"log"

	"github.com/basketikun/infinite-canvas/config"
	"github.com/basketikun/infinite-canvas/router"
	"github.com/basketikun/infinite-canvas/service"
)

func main() {
	// ⚠️ 顶层 panic 兜底 + 退出原因留痕。
	//
	// 线上故障（2026-08-26）：运行一段时间后前端所有 /api/* 变成
	// `connect ECONNREFUSED 127.0.0.1:8080` —— 意味着本进程已经消失。
	// 已排除 OOM（宿主内存占用不到一半），高度怀疑是某个 goroutine 的
	// panic 冒泡把进程带走了。
	//
	// 问题是当时**没有任何退出痕迹**可查：进程死得无声无息，日志最后一行
	// 只是正常的请求记录。所以这里做两件事：
	//   1. 兜住 main goroutine 自己的 panic 并打完整堆栈；
	//   2. 无论如何退出，都留下一行明确的 "server exiting" 日志。
	//
	// 这样下次复发时，日志里必定能看到「是 panic 还是正常退出」，
	// 不用再靠猜。后台 goroutine 的兜底见 service.SafeGo / service.RecoverPanic。
	defer service.RecoverPanic("main")

	if err := config.Load(); err != nil {
		log.Fatalf("server exiting: config load failed err=%v", err)
	}
	if err := service.EnsureDefaultAdmin(); err != nil {
		log.Fatalf("server exiting: ensure default admin failed err=%v", err)
	}
	service.StartPromptSyncScheduler()

	log.Printf("server listening on :%s", config.Cfg.Port)
	// Run 只在监听失败或进程被要求退出时返回，正常服务期间不会走到下一行。
	err := router.New().Run(":" + config.Cfg.Port)
	log.Fatalf("server exiting: http server stopped err=%v", err)
}
