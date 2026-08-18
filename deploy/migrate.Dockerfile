# 迁移工具，随每次构建一起出炉，与业务镜像共用同一个 git SHA。
#
# 为什么专门做一个镜像，而不是在服务器上装 Go、或者用别人发布的 goose 镜像：
#   * 服务器不装编译工具链——它只负责跑容器（deploy.sh 的前提）。
#   * 第三方镜像意味着生产迁移的供应链握在陌生人手里；而这一步是唯一能
#     改动生产数据库结构的动作。
#   * 迁移脚本烤进镜像，于是「跑的迁移」和「跑的代码」在物理上同源：
#     同一个 SHA 的 migrate 镜像与业务镜像，不可能对不上。
#
# 构建上下文是仓库根目录，与 deploy/Dockerfile 一致。
FROM golang:1.26-alpine AS build
# 版本与 Makefile 里 make migrate 用的一致；两处不同步会让本地和生产
# 跑在不同的迁移引擎上。
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.24.0

FROM alpine:3.20 AS collect
COPY services/ /src/services/
# 只留迁移脚本，按服务分目录；源码不进最终镜像。
RUN mkdir -p /migrations && cd /src/services && \
    for d in */db/migrations; do \
      [ -d "$d" ] || continue; \
      svc="${d%%/*}"; mkdir -p "/migrations/$svc"; cp "$d"/*.sql "/migrations/$svc/"; \
    done

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /go/bin/goose /usr/local/bin/goose
COPY --from=collect /migrations /migrations
COPY deploy/migrate-entrypoint.sh /usr/local/bin/migrate
RUN chmod +x /usr/local/bin/migrate
ENTRYPOINT ["/usr/local/bin/migrate"]
