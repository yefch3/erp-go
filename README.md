# erp-go

外贸 ERP 系统。Go 微服务 + gRPC + PostgreSQL + Kafka，前端 Vue 3。

完整设计见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) —— 服务拆分、数据库设计、
通信约定、不变量清单与分阶段任务全部在那里。

## 快速开始

```sh
make up      # 启动本地基础设施（PostgreSQL / Kafka / Redis / MinIO）
make proto   # lint + 重新生成 gRPC 代码
make test    # 跑全部测试
make migrate # 执行各服务的数据库迁移
```

工具全部通过 `go run` 固定版本调用，除 Go 1.24+ 与 Docker 外无需安装任何东西。

## 本地端口

为避免与其他项目冲突，宿主机端口整体错开（容器网络内仍是标准端口）：

| 组件 | 宿主机端口 | 覆盖变量 |
|---|---|---|
| PostgreSQL | 5433 | `PG_PORT` |
| Kafka | 19092 | `KAFKA_PORT` |
| Redis | 6380 | `REDIS_PORT` |
| MinIO API / 控制台 | 19000 / 19001 | `MINIO_PORT` / `MINIO_CONSOLE_PORT` |

数据库账号：每服务一个（`erp_export` / `erp_inventory` / …，密码 `erp_<svc>_pw`），
跨库无权限 —— 这是数据归属规则的物理保障，不要用超级账号连业务库。

## 仓库结构

```
proto/      gRPC 契约（buf 管理，CI 检查破坏性变更）
gen/go/     生成代码（提交入库，CI 校验与 proto 一致）
pkg/        跨服务基础库：money / apierr / grpcx / pgdb / outbox / kafkax / blobstore / idempotency
services/   每服务一个 Go module（阶段 1 起逐个出现）
deploy/     compose 与 Dockerfile
scripts/    CI 检查脚本（tenant_id 约定等）
```

## 约定速查

- 金额一律 `pkg/money`（decimal），**禁止 float64**
- 写库 + 发事件必须走 `pkg/outbox`，禁止在事务里直接调 Kafka
- 每张业务表带 `tenant_id`，唯一键包含 `tenant_id`（CI 强制）
- 业务错误用 `pkg/apierr` 带稳定错误码，客户端不得匹配错误消息字符串
