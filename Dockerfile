# 构建 Next.js 前端产物。
FROM oven/bun:1.3.13 AS web-build

WORKDIR /app/web
COPY web/package.json web/bun.lock ./
RUN --mount=type=cache,target=/root/.bun/install/cache bun install --frozen-lockfile --cache-dir=/root/.bun/install/cache
COPY VERSION /app/VERSION
COPY CHANGELOG.md /app/CHANGELOG.md
COPY web ./
RUN bun run build

# 构建 Go 后端入口。
FROM golang:1.25-alpine AS api-build

WORKDIR /app
COPY go.mod go.sum ./
COPY config ./config
COPY handler ./handler
COPY middleware ./middleware
COPY model ./model
COPY repository ./repository
COPY router ./router
COPY service ./service
COPY main.go ./
RUN go build -o /server .

# 运行镜像：Next.js 对外监听 3000，Go 只在容器内部监听 8080。
FROM node:22-bookworm-slim

WORKDIR /app
COPY VERSION /app/VERSION
COPY CHANGELOG.md /app/CHANGELOG.md
COPY --from=api-build /server /app/server
COPY --from=web-build /app/web/public /app/web/public
COPY --from=web-build /app/web/.next/standalone /app/web
COPY --from=web-build /app/web/.next/static /app/web/.next/static
ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV PORT=3000
ENV PROMPT_DATA_DIR=/app/data/prompts
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
RUN mkdir -p /app/data/prompts

EXPOSE 3000

# ── 进程模型：为什么不能用 `A & B` ────────────────────────────────────
#
# 线上故障（2026-08-26）：运行一段时间后前端所有 /api/* 变成
# `connect ECONNREFUSED 127.0.0.1:8080`，必须手动重启容器才恢复。
#
# 原因不在于「Go 为什么会死」，而在于**它死了之后没人管**：
#
#   旧写法 CMD ["sh","-c","PORT=8080 /app/server & cd /app/web && node server.js"]
#
# Go 被 `&` 扔到后台，容器的 PID 1 是 node。于是 Go 进程一旦退出：
#   - 没有任何东西会重启它；
#   - node 仍然存活 → 容器状态一直是「运行中」；
#   - docker-compose 的 restart: unless-stopped **永远不会触发**。
#
# 结果就是容器「活着但残废」：页面能打开，所有 API 全部 ECONNREFUSED。
#
# 修复思路：任一进程退出 → 整个容器退出，把恢复交给 Docker 的重启策略。
# 用 `wait -n` 等「第一个退出的子进程」，拿到它的退出码后主动收尾。
# 这样 restart: unless-stopped 能正常接管，实现自愈。
#
# ⚠️ 必须用 exec 形式启动 sh 并显式 trap，保证 SIGTERM 能传到子进程，
# 否则 docker stop 会每次都等满 10 秒超时才强杀。
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh
CMD ["/app/docker-entrypoint.sh"]
