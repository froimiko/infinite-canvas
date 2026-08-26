package service

// 后台 goroutine 的 panic 兜底。
//
// ── 为什么必须有这个文件 ──────────────────────────────────────────────
//
// 线上故障（2026-08-26）：运行一段时间后前端所有 /api/* 变成
//
//	connect ECONNREFUSED 127.0.0.1:8080
//
// ECONNREFUSED 的含义是「没有进程在监听该端口」—— 不是卡死、不是超时、
// 也不是网络问题，而是 Go 进程**已经不存在了**。（已排除 OOM：故障时
// 宿主内存占用不到一半。）
//
// Go 的规则：**任何 goroutine 里未被 recover 的 panic 都会杀死整个进程。**
// gin.Default() 自带的 Recovery 中间件只能保护「请求所在的那个 goroutine」，
// 对我们自己 go 出来的后台任务完全无效：
//
//   - novelai_sse.go 的出图心跳 ticker
//   - novelai_adapter.go 的排队进度汇报 ticker
//   - cron 里的 SyncRemotePromptCategories
//
// 这些地方一旦 panic（nil map 写入、close 已关闭的 channel、切片越界……），
// 整个服务端瞬间消失。而偶发性 panic 正好符合「跑一阵子才出问题、重启就好」
// 的现象。
//
// ⚠️ 纪律：本项目中**所有** `go func()` 都必须走 SafeGo 或在函数体首行
// `defer RecoverPanic(...)`。裸 `go func()` 等于给进程装了一颗定时炸弹。

import (
	"log"
	"runtime/debug"
)

// RecoverPanic 捕获 panic 并打印带堆栈的日志。
//
// 必须以 `defer RecoverPanic("场景名")` 的形式放在 goroutine 函数体首行。
// name 用于定位是哪个后台任务出的事，请写得具体（如 "novelai-sse-heartbeat"）。
//
// 刻意打印完整堆栈：这类 panic 往往是偶发竞态，没有堆栈基本无法复现定位。
func RecoverPanic(name string) {
	if reason := recover(); reason != nil {
		log.Printf(
			"[panic-recovered] goroutine=%s reason=%v\n--- stack ---\n%s\n--- end stack ---",
			name, reason, debug.Stack(),
		)
	}
}

// SafeGo 启动一个带 panic 兜底的 goroutine。
//
// 等价于 `go func() { defer RecoverPanic(name); fn() }()`，
// 但把「别忘了 recover」这件事收敛到一处，避免以后新增 goroutine 时漏掉。
func SafeGo(name string, fn func()) {
	go func() {
		defer RecoverPanic(name)
		fn()
	}()
}
