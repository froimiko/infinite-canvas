#!/bin/bash
# 容器入口：同时跑「Go API（内部 8080）」与「Next.js（对外 3000）」。
#
# ── 核心不变量 ────────────────────────────────────────────────────────
#
# **任一进程退出 → 容器整体退出。**
#
# 线上故障（2026-08-26）：旧入口是 `PORT=8080 /app/server & node server.js`，
# Go 被扔到后台、PID 1 是 node。Go 进程死掉后没人重启它，而 node 还活着，
# 所以容器状态始终「健康」，compose 的 restart: unless-stopped 永不触发。
# 表现为：页面打得开，但所有 /api/* 都是 connect ECONNREFUSED 127.0.0.1:8080，
# 只能人工重启容器。
#
# 这里监控两个子进程，任一方挂掉就主动退出，把恢复交给 Docker 重启策略。
#
# ── ⚠️ 为什么是 #!/bin/bash 而不是 #!/bin/sh ──────────────────────────
#
# 本脚本需要「等第一个退出的子进程」。bash 的 `wait -n` 能做到，但它是
# **bash 4.3+ 的内建扩展，不属于 POSIX**；而 Debian 的 /bin/sh 指向 dash，
# dash 没有 `wait -n`，用 #!/bin/sh 会直接报错退出 —— 那等于容器起不来。
#
# 基础镜像 node:22-bookworm-slim 自带 bash，所以这里显式用 bash。
# 但为了不依赖 `wait -n` 的具体版本行为，下面改用「轮询 kill -0」的写法：
# 语义更直白、跨 bash 版本稳定，代价只是最多 2 秒的检测延迟（可忽略，
# 因为紧接着就要重建容器）。
#
# 改动此文件前请确认：
#   1. 两个进程都要在后台启动并记录 PID；
#   2. 必须 trap SIGTERM/SIGINT 并转发给子进程，否则 docker stop 要等满
#      10 秒超时才被强杀，滚动更新会明显变慢；
#   3. 日志要写清「是谁先挂的」，这决定下次排查看哪一侧。

set -u

api_pid=""
web_pid=""
shutting_down=0

# 收到停止信号时，把信号转发给两个子进程，让它们有机会优雅退出。
forward_term() {
    shutting_down=1
    echo "[entrypoint] received termination signal, stopping children"
    [ -n "$api_pid" ] && kill -TERM "$api_pid" 2>/dev/null
    [ -n "$web_pid" ] && kill -TERM "$web_pid" 2>/dev/null
    wait
    exit 0
}
trap forward_term TERM INT

echo "[entrypoint] starting Go API on :8080"
PORT=8080 /app/server &
api_pid=$!

echo "[entrypoint] starting Next.js on :3000"
(
    cd /app/web || exit 1
    PORT=3000 exec node server.js
) &
web_pid=$!

echo "[entrypoint] api_pid=$api_pid web_pid=$web_pid"

# 轮询两个进程是否还活着。正常运行期间这里会一直循环。
# 2 秒间隔足够及时：进程死了本来就要重建容器，差几秒无关紧要。
while [ "$shutting_down" -eq 0 ]; do
    if ! kill -0 "$api_pid" 2>/dev/null; then
        wait "$api_pid" 2>/dev/null
        exit_code=$?
        echo "[entrypoint] FATAL: Go API exited (code=$exit_code)."
        echo "[entrypoint] Shutting down container so Docker's restart policy can recover it."
        echo "[entrypoint] If this repeats, search the API log above for 'panic' / 'panic-recovered' / 'server exiting'."
        kill -TERM "$web_pid" 2>/dev/null
        wait 2>/dev/null
        exit "${exit_code:-1}"
    fi

    if ! kill -0 "$web_pid" 2>/dev/null; then
        wait "$web_pid" 2>/dev/null
        exit_code=$?
        echo "[entrypoint] FATAL: Next.js exited (code=$exit_code)."
        echo "[entrypoint] Shutting down container so Docker's restart policy can recover it."
        kill -TERM "$api_pid" 2>/dev/null
        wait 2>/dev/null
        exit "${exit_code:-1}"
    fi

    sleep 2
done
