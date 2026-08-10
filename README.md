# erp-go

外贸 ERP 系统。Go 微服务 + gRPC + PostgreSQL + Kafka，前端 Vue 3（中 / 英 / 西三语）。

完整设计见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) —— 服务拆分、数据库设计、
通信约定、不变量清单与分阶段任务全部在那里。这份 README 只讲怎么跑起来。

## 快速开始

```sh
make up         # 启动本地基础设施（PostgreSQL / Kafka / Redis / MinIO）
make migrate    # 执行各服务的数据库迁移
make proto      # lint + 重新生成 gRPC 代码
make sqlc       # 重新生成 sqlc 查询代码
make test       # 跑全部测试
make ci         # lint + 测试 + tenant_id 检查，提交前跑这个
```

起服务与前端：

```sh
make services-up                              # 全部后端服务
cd frontend && npm install && npm run dev     # 前端 :5173
```

浏览器打开 <http://localhost:5173>，用 `admin` / `admin123` 登录。

工具全部通过 `go run` 固定版本调用，除 Go 1.24+、Node 18+ 与 Docker 外无需安装任何东西。

## 服务与端口

| 服务 | gRPC | 职责 |
|---|---|---|
| iam | 9001 | 员工、角色、权限、数据范围、登录 |
| masterdata | 9002 | 客户、供应商、联系人、单据编号、选项字典 |
| fx | 9003 | 汇率快照 |
| product | 9004 | 产品、SKU、品类、属性模板与召回 |
| approval | 9005 | 审批流设计与运行 |
| export | 9006 | 报价、合同、出运单、收款对账 |
| procurement | 9007 | 采购需求、采购单 |
| inventory | 9008 | 库存、出入库、成本 |
| mail | 9010 | 邮件：撰写、群发、投递、收取、失败处理 |
| gateway | **HTTP 8080** | REST 门面：校验 JWT、映射到 gRPC、翻译错误 |

网关是唯一对外的 HTTP 入口，自己不持有任何业务逻辑和数据库。

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

> 上面这些账号密码、以及 `admin/admin123` 和 JWT 密钥
> `dev-secret-change-in-production`，**只用于本地开发**。部署前全部替换。

## 仓库结构

```
proto/      gRPC 契约（buf 管理，CI 检查破坏性变更）
gen/go/     生成代码（提交入库，CI 校验与 proto 一致）
pkg/        跨服务基础库：money / apierr / grpcx / pgdb / outbox / kafkax / blobstore / idempotency
services/   每服务一个 Go module，内部按 cmd / internal/{app,adapter,store,config} 分层
frontend/   Vue 3 + Element Plus + vue-i18n
deploy/     compose 与 Dockerfile
docs/       架构设计（唯一的设计事实来源）
scripts/    CI 检查脚本（tenant_id 约定等）
```

服务内部分层的意思是：`app` 是业务用例，不认识 gRPC 也不认识 HTTP；
`adapter/grpcin` 把 gRPC 请求翻译成用例调用，`adapter/grpcout` 把对外依赖包成接口；
`store` 是 sqlc 生成的查询。**跨服务只走 gRPC，绝不跨库读表。**

## 约定速查

- 金额一律 `pkg/money`（decimal），**禁止 float64**
- 写库 + 发事件必须走 `pkg/outbox`，禁止在事务里直接调 Kafka
- 每张业务表带 `tenant_id`，唯一键包含 `tenant_id`（`make check-tenant` 强制）
- 业务错误用 `pkg/apierr` 带稳定错误码，客户端不得匹配错误消息字符串
- 状态、类型等枚举在库里是英文常量，显示名在前端 i18n 翻译
- 单据不删除，只作废或写反向记录；已生效的内容不可改，只能出新版本

## 邮件模块的当前状态

邮件（`mail`）除**真实发信**外已完整：撰写（富文本 + 附件 + 签名）、
一人一封群发、退避重试、失败转人工、拒收名单、草稿箱、按数据范围查看同事往来。

```
MAIL_PROVIDER=dev   # 默认。记录「本该发什么」而不真发
```

`dev` 适配器还提供三个魔法域名用来触发失败路径，本地验证不需要真发信：

| 收件地址 | 触发 |
|---|---|
| `*@bounce.test` | 永久退信 → 自动进拒收名单 |
| `*@defer.test` | 临时失败 → 退避重试 |
| `*@timeout.test` | 结果未知 → 营销停下等人工确认 |

**接真实服务商还差什么**：选定服务商、验证发信域名（SPF / DKIM / DMARC）、
申请解除沙箱，然后补一个 `provider/ses.go`（约 80 行，接口已留好）。
**收件箱**还需要服务商的入站通道，界面上已经存在但明确写着未接通 ——
详见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) §5.12。

### 客户询盘转公司询价 Excel

在收件邮件里选中文字后右键，或在附件卡片上右键，选择「生成询价 Excel」。
服务端调用 OpenAI Responses API 抽取询盘，并强制整理为公司固定列：产品、标准、
牌号、尺寸、表面、涂层、公差、卷重、卷内径、包装、交期、付款、贸易术语、港口、
单位、备注、数量、单价、总价。模型只负责提取事实，不能决定列名和公式；单价留空，
总价由 Excel 公式 `数量 × 单价` 计算。界面先显示预览，再提供 `.xlsx` 下载。
正文/附件只有在用户明确点击时才会发送，API key 始终留在 mail 服务。

```sh
export OPENAI_API_KEY='...'
export OPENAI_MODEL='gpt-5.6-luna'                 # 默认
export OPENAI_BASE_URL='https://api.openai.com/v1' # 可选
export OPENAI_TIMEOUT='2m'                         # 可选
```

未配置 `OPENAI_API_KEY` 不影响邮箱其他功能，界面也不会显示生成操作。支持常见图片、
PDF、文本、Word、PowerPoint 和电子表格附件；单个附件沿用邮件模块的 10 MB 上限。
模型请求使用 `store: false`，生成结果直接返回浏览器，不在数据库或对象存储中留下副本。

为控制模型费用，每位用户最多在 10 分钟内发起 12 次转换。生成 Excel 时，普通数字会
保持为可计算的数值；超过 Excel 15 位安全精度的数字会按文本保存，避免订单号、客户编号
或高精度数据被静默舍入。
