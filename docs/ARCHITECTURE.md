# 外贸 ERP 系统架构设计

> 依据《ERP 模块结构与互动说明》重建。后端 Go，服务间 gRPC，数据库 PostgreSQL，异步消息 Kafka，本地与部署均通过 Docker。

---

## 目录

1. [设计原则](#1-设计原则)
2. [服务拆分](#2-服务拆分)
3. [技术栈](#3-技术栈)
4. [通信设计](#4-通信设计)
5. [数据库设计](#5-数据库设计)
6. [关键业务流程](#6-关键业务流程)
7. [代码结构与骨架](#7-代码结构与骨架)
8. [Docker 部署](#8-docker-部署)
9. [分阶段任务清单](#9-分阶段任务清单)
10. [不变量清单](#10-不变量清单)

---

## 1. 设计原则

需求文档第三节《最重要的数据归属规则》是整个架构的地基。它规定了每类数据有且只有一个负责模块，其他模块只能引用。本设计把这条规则从「文档约定」变成「结构性保障」：

| 原则 | 落地手段 |
|---|---|
| 一类数据只有一个属主 | 每个服务独占一个 PostgreSQL database，独立数据库账号 |
| 不允许跨模块直接读表 | 数据库账号无跨库权限；跨服务只能走 gRPC |
| 引用而不复制 | 跨服务只存业务 ID，不做数据冗余（读模型除外，且只读） |
| 报表与驾驶舱只读 | 使用只读数据库账号 + 只暴露 Query 类 gRPC 方法 |
| 汇率快照不可回溯 | 快照以值对象内嵌进单据表，不存外键 |
| 生效后不可改 | 合同用版本表，历史版本不可变；账目表 append-only |
| 引用后不可删 | 主数据服务不提供 Delete，只提供 Deactivate |

### 关于服务粒度

拆分粒度按「业务能力 + 数据属主」，不按技术分层，也不按数据库表数量。共 **11 个服务**。

出口业务（报价 / 合同 / 出货计划 / 信用证 / 单证 / 退税 / 收汇）合并为**一个** `export` 服务，不再细拆。原因：这 7 块共享合同这一个聚合根，且相互之间有强不变量（累计出货量不得超合同数量、信用证金额需覆盖合同、收汇计划由合同生成）。拆开会把本可以在单个数据库事务内保证的约束，变成需要 saga 补偿的分布式问题。

`export` 内部按子域分包，将来若确有独立扩容需求，包边界即是拆分线。

---

## 2. 服务拆分

```
                          ┌─────────────┐
                          │   前端 Vue   │
                          └──────┬──────┘
                                 │ HTTPS / REST + SSE
                          ┌──────▼──────┐
                          │   gateway   │  BFF：认证、聚合、协议转换
                          └──────┬──────┘
                                 │ gRPC
        ┌────────────┬───────────┼───────────┬────────────┐
        │            │           │           │            │
   ┌────▼────┐  ┌────▼─────┐ ┌───▼────┐ ┌────▼─────┐ ┌────▼────┐
   │ export  │  │procurement│ │inventory│ │ shipping │ │reporting│
   │ 出口业务 │  │  采购管理  │ │ 仓储管理 │ │ 船期管理  │ │报表/驾驶舱│
   └────┬────┘  └────┬─────┘ └───┬────┘ └────┬─────┘ └────▲────┘
        │            │           │           │            │
        └────────────┴─────┬─────┴───────────┘            │
                           │ gRPC（同步查询与强约束调用）      │
        ┌──────────┬───────┼────────┬──────────┐          │
        │          │       │        │          │          │
   ┌────▼───┐ ┌────▼───┐ ┌─▼────┐ ┌─▼──────┐ ┌─▼────┐     │
   │masterdata│ │product│ │  fx  │ │approval│ │ iam  │     │
   │ 基础数据 │ │产品管理│ │汇率中心│ │ 审批流  │ │身份权限│     │
   └────────┘ └───────┘ └──────┘ └────────┘ └──────┘     │
                                                          │
        ┌─────────────────────────────────────────────────┘
        │  Kafka（异步事件：进度投影、预警、日志）
   ┌────▼────┐
   │  audit  │  操作日志（只写入、只查询）
   └─────────┘
```

### 服务清单

| 服务 | 端口 | 对应文档模块 | 职责 | 数据属主 |
|---|---|---|---|---|
| `gateway` | 8080 | — | REST 入口、JWT 校验、请求聚合、SSE 推送 | 无 |
| `iam` | 9001 | 基础数据（部门/员工/角色/权限） | 登录、员工、部门、角色、权限、数据范围 | ✅ |
| `masterdata` | 9002 | 基础数据（客户/供应商/选项/编码） | 客户、供应商、业务选项、编码规则 | ✅ |
| `product` | 9003 | 6. 产品管理 | 产品、SKU、单位换算、包装、条码 | ✅ |
| `fx` | 9004 | 8. 汇率中心 | 实时汇率抓取、历史、快照发放、异常检测 | ✅ |
| `approval` | 9005 | 基础数据（审批流配置） | 审批定义、实例、任务、我的待办 | ✅ |
| `export` | 9006 | 2. 出口业务管理 | 报价、合同、出货计划、信用证、单证、退税、收汇 | ✅ |
| `procurement` | 9007 | 3. 采购管理 | 采购需求、采购单、收货、付款 | ✅ |
| `inventory` | 9008 | 4. 仓储管理 | 库存、锁定、出入库、调拨盘点、预警 | ✅ |
| `shipping` | 9009 | 5. 船期管理 | 船期订舱、集装箱、运输节点、费用、异常 | ✅ |
| `reporting` | 9010 | 1. 驾驶舱 / 9. 报表中心 / 2.4 合同执行进度 | 只读投影与聚合查询 | 只读副本 |
| `audit` | 9011 | 基础数据（操作日志） | 消费全局事件，落库、查询 | ✅ append-only |

---

## 3. 技术栈

### 后端

| 关注点 | 选型 | 说明 |
|---|---|---|
| 语言 | Go 1.23+ | |
| RPC | gRPC + Protocol Buffers | 服务间唯一同步通道 |
| gRPC 网关 | `grpc-gateway` 或手写 BFF | 推荐手写 BFF，聚合逻辑可控 |
| HTTP（gateway） | `chi` | 中间件清晰，无隐式行为 |
| 数据访问 | `pgx/v5` + **`sqlc`** | 见下方说明 |
| 迁移 | `goose` | 每服务独立迁移目录 |
| 消息 | Kafka（`segmentio/kafka-go`） | 仅用于事件投影与通知 |
| 金额 / 汇率 | `shopspring/decimal` | **禁止 float64** |
| 配置 | 环境变量 + `koanf` | 12-factor |
| 日志 | 标准库 `log/slog` | 结构化，含 trace_id |
| 链路追踪 | OpenTelemetry | gRPC / HTTP / DB 自动埋点 |
| 校验 | `protoc-gen-validate` + 领域层校验 | 边界与领域两道 |
| 测试 | `testcontainers-go` | 真实 PostgreSQL / Kafka，不 mock 数据库 |
| 认证 | JWT（gateway 校验）+ gRPC metadata 透传 | |

**为什么用 sqlc 而不是 ORM**：sqlc 在**代码生成期**把每条 SQL 与实际 schema 做校验，字段写错、表不存在都会导致 `go generate` 失败。ORM 的实体与表是隐式映射，字段漂移只有运行时才暴露。这类「实体定义与数据库不一致但服务照常启动」的问题，正是上一版系统最大的坑。

### 前端

| 关注点 | 选型 |
|---|---|
| 框架 | Vue 3 + TypeScript |
| 构建 | Vite |
| UI | Element Plus |
| 客户端状态 | Pinia |
| 服务端状态 | TanStack Query (vue-query) |
| 路由 | Vue Router |
| 表单 | Element Plus Form + Zod 校验 |

服务端状态与客户端状态严格分离：列表、详情等来自后端的数据一律走 TanStack Query（自带缓存、失效、重试），Pinia 只存 UI 状态和当前用户上下文。不要把接口返回值塞进 Pinia。

---

## 4. 通信设计

### 4.1 同步还是异步：判据

这是微服务架构下最容易做错的一件事。判据只有一条：

> **跨服务的业务不变量 → 同步 gRPC 调用**
> **投影、通知、看板刷新、日志 → Kafka 事件**

| 场景 | 方式 | 原因 |
|---|---|---|
| 出货计划确认 → 锁定库存 | 同步 gRPC | 库存不足必须当场返回缺口，不能「先确认再补偿」 |
| 单据创建 → 取汇率快照 | 同步 gRPC | 快照是单据的一部分，取不到就不该创建 |
| 提交审批 → 创建审批实例 | 同步 gRPC | 审批实例创建失败，单据不应进入待审状态 |
| 任何写操作 → 校验权限 | 同步 gRPC（或 gateway 预校验） | 安全边界 |
| 合同生效 → 生成采购需求 | Kafka | 采购员看到待办即可，允许秒级延迟 |
| 船期节点更新 → 刷新执行进度 | Kafka | 纯投影 |
| 库存变化 → 预警中心 | Kafka | 纯投影 |
| 一切业务操作 → 操作日志 | Kafka | 旁路，不应阻塞主流程 |

### 4.2 Outbox 模式（强制）

任何「改本地数据库 + 发 Kafka 事件」的操作，都必须走 outbox，不允许在事务里直接调 Kafka producer。

```
BEGIN;
  INSERT INTO contracts ...;
  INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload) VALUES (...);
COMMIT;

-- 独立 relay 协程轮询 outbox_events → 投递 Kafka → 标记 published_at
```

理由：直接发消息会出现「库写成功但消息没发」或「消息发了但事务回滚」。outbox 保证二者原子。

每个服务的数据库都有一张 `outbox_events`，结构统一：

```sql
CREATE TABLE outbox_events (
    id             BIGSERIAL PRIMARY KEY,
    aggregate_type TEXT        NOT NULL,
    aggregate_id   TEXT        NOT NULL,
    event_type     TEXT        NOT NULL,
    payload        JSONB       NOT NULL,
    trace_id       TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ,
    attempts       INT         NOT NULL DEFAULT 0,
    last_error     TEXT
);
CREATE INDEX outbox_events_unpublished_idx ON outbox_events (created_at) WHERE published_at IS NULL;
```

### 4.3 Kafka Topic 设计

命名：`erp.<服务>.<聚合>.v<版本>`。分区键统一使用聚合根 ID，保证同一业务对象的事件有序。

| Topic | 生产者 | 主要消费者 | 关键事件 |
|---|---|---|---|
| `erp.export.contract.v1` | export | procurement, inventory, reporting, audit | ContractEffective, ContractAmended, ContractClosed |
| `erp.export.shipment.v1` | export | inventory, shipping, reporting | ShipmentPlanConfirmed, ShipmentPlanCancelled |
| `erp.export.receivable.v1` | export | reporting | ReceiptConfirmed, ReceiptReversed |
| `erp.procurement.order.v1` | procurement | inventory, reporting | POApproved, POReceived, POPaid |
| `erp.inventory.stock.v1` | inventory | export, reporting | StockChanged, StockLocked, StockReleased, StockAlert |
| `erp.shipping.voyage.v1` | shipping | export, reporting | VoyageBooked, NodeReached, ExceptionRaised |
| `erp.approval.task.v1` | approval | 各业务服务, reporting | ApprovalApproved, ApprovalRejected, ApprovalReturned |
| `erp.fx.rate.v1` | fx | reporting | RateUpdated, RateAnomalyDetected |
| `erp.audit.operation.v1` | 全部 | audit | OperationLogged |

消费端必须**幂等**：事件携带 `event_id`，消费者维护 `processed_events(event_id, consumer_group, processed_at)` 去重表。

**已落地的消费组（2026-07-28）**

| 消费组 | 服务 | 订阅 | 作用 |
|---|---|---|---|
| `export.approval.v1` | export | `erp.approval.task.v1` | 审批通过 → 合同转「待签署」；驳回/退回 → 退回提交前状态 |

> **死信要跳过，不要重试。** 处理器只对「本服务暂时办不到」的错误返回 error（不提交
> offset，等重投）。事件本身没救的情况——payload 解析不了、`biz_id` 指向不存在的单据
> ——记一条 WARN 并提交 offset。若按错误处理会把整个消费组永久卡死在这几条上。
> （早期那 4 条 `biz_id` 1001–1005 的压测实例已于 2026-07-28 清理。）

### 4.3.1 Kafka 不承担的事：界面实时刷新

**审批节点之间的推进不发 Kafka。** 一次 `Act` 是 approval 库里的**单个本地事务**：
标记任务已办 → 关闭同节点其他任务 → 推进实例 → 生成下一节点任务。
只有**整个实例结束**时才往 outbox 写一条决策事件，收件人是**业务服务**（export），
不是下一个审批人。实测：一份合同两级审批 = 2 个通过动作，但只有 1 条 Kafka 事件。

界面的实时刷新走另一条通道 —— `pkg/livefeed`，**Redis pub/sub**：

| | Kafka `erp.*` | Redis `erp.live.t{租户}.e{员工}` |
|---|---|---|
| 内容 | 业务事实 | 「你正在看的东西变了，去重新读」 |
| 收件人 | 其他**服务** | 某个**人**的浏览器 |
| 保证 | 持久、有序、可重放、幂等消费 | 尽力而为，丢了就退化成手动刷新 |
| 丢一条的后果 | 业务状态不一致 | 用户晚几秒看到 |

选 Redis 而不是 Kafka 的理由：这些提示**不需要**持久化、顺序或重放；而且网关是多副本的，
每个副本都要收到**全部**消息，用 Kafka 就得给每个副本编一个唯一 consumer group，
是在跟工具较劲。Redis pub/sub 的 fire-and-forget 语义正好对上。

**三条实现纪律**：

1. **提示只说「变了」，不带新数据。** 页面收到后走正常 REST 接口重新读，
   权限在原来的地方重新校验，加载数据只有一条代码路径。
2. **必须在事务提交之后发。** 事务里发提示、事务又回滚，就是让浏览器去读一件没发生的事。
3. **发送失败只记 WARN，绝不影响业务操作。** 提示丢了不值得让审批失败。

网关侧是 **SSE**（`GET /api/events`）而不是 WebSocket：流量是单向的，
浏览器什么都不回传。

> **踩过的坑（2026-07-28）**：go-redis 的 `Subscribe(ctx, ...)` 里那个 ctx **只管首次调用**，
> 订阅不会随 ctx 取消而关闭，`sub.Channel()` 也永远不会自己关。
> 最初的实现用 `for range sub.Channel()` 收消息，浏览器断开后这个 goroutine
> 会永远阻塞在一个安静的 channel 上——**每开一次页面泄漏一个 goroutine 和一个 Redis 订阅**。
> 实测订阅数只增不减才发现。修法是另起一个 goroutine 等 `ctx.Done()` 后显式 `sub.Close()`。
> 验证：开 3 条连接 2→5，全部断开后回到 2。

### 4.4 跨服务事务：Saga

微服务架构下无法跨库单事务，涉及强不变量的流程用 saga + 补偿。系统中只有一处需要：**出货计划确认 → 锁定库存**。

```
export: 出货计划 status = PENDING_STOCK
        │
        ├─ gRPC → inventory.LockStock(lock_ref="shipment_plan:{id}", items)
        │
        ├─ 成功 → status = CONFIRMED，发 ShipmentPlanConfirmed 事件
        └─ 库存不足 → status = STOCK_SHORTAGE，返回缺口明细
                      前端提示「建议采购或调拨」

补偿：出货计划取消 / 合同取消
        └─ gRPC → inventory.ReleaseLock(lock_ref="shipment_plan:{id}")
```

关键设计：`LockStock` 以 `lock_ref` 作为**幂等键**，重复调用不会重复锁定；`ReleaseLock` 按 `lock_ref` 释放，天然幂等。这样即使网络超时重试也安全，无需复杂的补偿状态机。

### 4.5 gRPC 接口约定

- 所有服务的 proto 放在 `proto/` 顶层目录，统一编译，版本化包名 `erp.export.v1`
- 只读方法以 `Get` / `List` / `Query` 开头；写方法以动词开头（`CreateContract`、`ConfirmShipmentPlan`）
- 分页统一 `PageRequest{page, page_size}` / `PageResponse{total, items}`
- 所有写操作携带 `Operator{employee_id, name, ip, trace_id}`，用于操作日志
- 错误用 `google.rpc.Status` + 业务错误码枚举，不要把业务错误塞进 message 字符串

---

## 5. 数据库设计

每个服务独立 database，命名 `erp_<service>`。同一 PostgreSQL 实例内即可（本地开发一个容器），生产可分实例。

### 公共约定

- 主键统一 `BIGSERIAL`（或雪花 ID，若将来需要跨库合并）
- 所有时间列 `TIMESTAMPTZ`，存 UTC
- 金额 `NUMERIC(18,2)`，汇率 `NUMERIC(18,8)`，数量 `NUMERIC(18,4)`
- 状态列用 `VARCHAR(32)` + `CHECK` 约束，不用数据库 enum 类型（便于演进）
- 每张业务表带 `created_at / created_by / updated_at / updated_by`
- 逻辑删除只用于可恢复场景；主数据用 `status` 停用，账目类**不允许删除**
- **跨服务引用只存 ID，不建外键**；服务内部正常建外键
- **本位币为 USD**（业务确认）：所有 `base_amount` 均为折 USD 金额；`fx_base_currency` 默认 `'USD'`；采购、船期费用等 CNY 单据经各自汇率快照折 USD 进入报表与汇兑损益
- 附件类列（`file_path` / `bank_slip_path` / `voucher_path`）统一存**对象存储 key**（`bucket/object-key`），不存本地路径；存储后端走 S3 兼容接口（本地 MinIO，云端换 S3/OSS 只改 endpoint 配置）

### 多租户约定（预留）

按「预留多租户」设计（业务确认）：当前单租户运行（`tenant_id = 1`），但结构从第一天支持多租户，避免事后为每张表补列、改唯一键的灾难性改造。

- 每张业务表带 `tenant_id BIGINT NOT NULL DEFAULT 1`
- **所有业务唯一键必须包含 tenant_id**：`UNIQUE (tenant_id, quote_no)` 而非 `UNIQUE (quote_no)`；取号表主键为 `(tenant_id, biz_type, period_key)`
- 复合索引以 `tenant_id` 开头
- JWT 携带 `tenant_id`，gateway 写入 gRPC metadata `x-tenant-id`，服务端拦截器注入 context，所有查询强制带租户过滤
- Kafka 事件 payload 携带 `tenant_id`
- 阶段 0 的 CI 加迁移检查脚本：扫描所有 `CREATE TABLE`，缺 `tenant_id` 直接失败
- 阶段 8 可选启用 PostgreSQL Row Level Security 作为第二道防线

> 为可读性，第 5 节的建表 SQL **省略了 `tenant_id` 列及其索引**；实际迁移文件必须包含，由上述 CI 检查强制。

### 5.1 iam（身份与权限）

```sql
CREATE TABLE departments (
    id          BIGSERIAL PRIMARY KEY,
    code        VARCHAR(50)  NOT NULL UNIQUE,
    name        VARCHAR(100) NOT NULL,
    parent_id   BIGINT REFERENCES departments(id),
    path        VARCHAR(500) NOT NULL,          -- 物化路径 /1/5/12/，便于数据范围查询
    level       INT          NOT NULL DEFAULT 1,
    manager_id  BIGINT,
    sort_order  INT          NOT NULL DEFAULT 0,
    status      VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE employees (
    id            BIGSERIAL PRIMARY KEY,
    code          VARCHAR(50)  NOT NULL UNIQUE,   -- 工号
    name          VARCHAR(100) NOT NULL,
    department_id BIGINT       NOT NULL REFERENCES departments(id),
    position      VARCHAR(100),
    email         VARCHAR(200),
    phone         VARCHAR(50),
    status        VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE','LEFT')),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX employees_dept_idx ON employees (department_id) WHERE status = 'ACTIVE';

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    employee_id   BIGINT       NOT NULL UNIQUE REFERENCES employees(id),
    username      VARCHAR(100) NOT NULL UNIQUE,
    password_hash VARCHAR(255) NOT NULL,          -- argon2id
    status        VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','LOCKED','DISABLED')),
    last_login_at TIMESTAMPTZ,
    failed_count  INT          NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE roles (
    id          BIGSERIAL PRIMARY KEY,
    code        VARCHAR(50)  NOT NULL UNIQUE,
    name        VARCHAR(100) NOT NULL,
    description TEXT,
    status      VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE permissions (
    id        BIGSERIAL PRIMARY KEY,
    code      VARCHAR(100) NOT NULL UNIQUE,   -- export:contract:approve
    name      VARCHAR(100) NOT NULL,
    module    VARCHAR(50)  NOT NULL,
    resource  VARCHAR(50)  NOT NULL,
    action    VARCHAR(50)  NOT NULL,
    menu_path VARCHAR(200)                    -- 非空表示同时是菜单项
);

CREATE TABLE role_permissions (
    role_id       BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id BIGINT NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE employee_roles (
    employee_id BIGINT NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    role_id     BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (employee_id, role_id)
);

-- 数据范围：决定「能看哪些数据」，文档 7. 角色和权限的第三层含义
CREATE TABLE role_data_scopes (
    id          BIGSERIAL PRIMARY KEY,
    role_id     BIGINT      NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    module      VARCHAR(50) NOT NULL,          -- export / procurement / ...
    scope_type  VARCHAR(32) NOT NULL CHECK (scope_type IN ('SELF','DEPT','DEPT_AND_SUB','ALL','CUSTOM')),
    custom_dept_ids BIGINT[],                      -- CUSTOM 为预留枚举，暂不实现其逻辑与 UI（业务确认）
    UNIQUE (role_id, module)
);
```

### 5.2 masterdata（客户 / 供应商 / 选项 / 编码）

```sql
CREATE TABLE customers (
    id                BIGSERIAL PRIMARY KEY,
    code              VARCHAR(50)  NOT NULL UNIQUE,
    name              VARCHAR(200) NOT NULL,
    short_name        VARCHAR(100),
    country           VARCHAR(50),
    default_currency  VARCHAR(3)   NOT NULL DEFAULT 'USD',
    default_incoterm  VARCHAR(20),
    payment_term      VARCHAR(50),
    credit_limit      NUMERIC(18,2),
    sales_employee_id BIGINT,                       -- 引用 iam.employees，不建外键
    status            VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by        BIGINT,
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by        BIGINT
);

CREATE TABLE customer_contacts (
    id          BIGSERIAL PRIMARY KEY,
    customer_id BIGINT       NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    name        VARCHAR(100) NOT NULL,
    title       VARCHAR(100),
    email       VARCHAR(200),
    phone       VARCHAR(50),
    is_primary  BOOLEAN      NOT NULL DEFAULT false,
    status      VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE'
);
CREATE UNIQUE INDEX customer_primary_contact_idx
    ON customer_contacts (customer_id) WHERE is_primary AND status = 'ACTIVE';

CREATE TABLE customer_addresses (
    id           BIGSERIAL PRIMARY KEY,
    customer_id  BIGINT      NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    address_type VARCHAR(32) NOT NULL CHECK (address_type IN ('BILLING','SHIPPING','OTHER')),
    country      VARCHAR(50),
    city         VARCHAR(100),
    detail       TEXT,
    postal_code  VARCHAR(20),
    is_default   BOOLEAN     NOT NULL DEFAULT false
);

CREATE TABLE suppliers (
    id                    BIGSERIAL PRIMARY KEY,
    code                  VARCHAR(50)  NOT NULL UNIQUE,
    name                  VARCHAR(200) NOT NULL,
    country               VARCHAR(50),
    default_currency      VARCHAR(3)   NOT NULL DEFAULT 'CNY',
    payment_term          VARCHAR(50),
    purchaser_employee_id BIGINT,
    bank_name             VARCHAR(200),
    bank_account          VARCHAR(100),
    tax_no                VARCHAR(100),
    status                VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE supplier_contacts (
    id          BIGSERIAL PRIMARY KEY,
    supplier_id BIGINT       NOT NULL REFERENCES suppliers(id) ON DELETE CASCADE,
    name        VARCHAR(100) NOT NULL,
    phone       VARCHAR(50),
    email       VARCHAR(200),
    is_primary  BOOLEAN      NOT NULL DEFAULT false
);

-- 业务选项：付款方式、贸易术语、费用类型、异常原因等所有下拉项
CREATE TABLE option_types (
    id          BIGSERIAL PRIMARY KEY,
    code        VARCHAR(50)  NOT NULL UNIQUE,   -- PAYMENT_METHOD / INCOTERM / COST_TYPE / EXCEPTION_REASON
    name        VARCHAR(100) NOT NULL,
    description TEXT,
    editable    BOOLEAN      NOT NULL DEFAULT true
);

CREATE TABLE options (
    id         BIGSERIAL PRIMARY KEY,
    type_id    BIGINT       NOT NULL REFERENCES option_types(id) ON DELETE CASCADE,
    code       VARCHAR(50)  NOT NULL,
    label      VARCHAR(100) NOT NULL,
    label_en   VARCHAR(100),
    sort_order INT          NOT NULL DEFAULT 0,
    extra      JSONB,                            -- 额外属性，如贸易术语的责任划分
    status     VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE',
    UNIQUE (type_id, code)
);

-- 编码规则：自动生成报价单号、合同号、采购单号、出入库单号、船期号
CREATE TABLE number_rules (
    id           BIGSERIAL PRIMARY KEY,
    biz_type     VARCHAR(50)  NOT NULL UNIQUE,   -- QUOTATION / CONTRACT / PO / INBOUND / OUTBOUND / VOYAGE
    prefix       VARCHAR(20)  NOT NULL,
    date_format  VARCHAR(20)  NOT NULL DEFAULT 'YYYYMMDD',
    seq_length   INT          NOT NULL DEFAULT 4,
    reset_cycle  VARCHAR(20)  NOT NULL DEFAULT 'DAY' CHECK (reset_cycle IN ('NEVER','DAY','MONTH','YEAR')),
    separator    VARCHAR(5)   NOT NULL DEFAULT '',
    status       VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE'
);

CREATE TABLE number_sequences (
    biz_type   VARCHAR(50) NOT NULL,
    period_key VARCHAR(20) NOT NULL,             -- 20260727 / 202607 / 2026 / ALL
    current_no BIGINT      NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (biz_type, period_key)
);
-- 分配：INSERT ... ON CONFLICT DO UPDATE SET current_no = number_sequences.current_no + 1 RETURNING current_no
```

**取号时机**：单据在**保存时**由服务端取号（编码留空即视为「请系统生成」），
取号与插入在同一个事务里完成。这样打开表单又取消不会消耗号码，插入失败时
序号随事务一起回滚。行锁只在这个短事务内持有，不影响并发正确性——重号仍然
不可能，断号仍然允许（跨事务的并发失败场景）。前端**不再预先调用取号接口**。

### 5.3 product（产品主数据）

```sql
CREATE TABLE product_categories (
    id         BIGSERIAL PRIMARY KEY,
    code       VARCHAR(50)  NOT NULL UNIQUE,
    name       VARCHAR(100) NOT NULL,
    parent_id  BIGINT REFERENCES product_categories(id),
    path       VARCHAR(500) NOT NULL,
    level      INT          NOT NULL DEFAULT 1,
    sort_order INT          NOT NULL DEFAULT 0,
    status     VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE'
);

CREATE TABLE uoms (
    id       BIGSERIAL PRIMARY KEY,
    code     VARCHAR(20)  NOT NULL UNIQUE,       -- PCS / CTN / KG / CBM
    name     VARCHAR(50)  NOT NULL,
    uom_type VARCHAR(32)  NOT NULL CHECK (uom_type IN ('COUNT','WEIGHT','VOLUME','LENGTH')),
    status   VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE'
);

CREATE TABLE products (
    id                  BIGSERIAL PRIMARY KEY,
    code                VARCHAR(100) NOT NULL UNIQUE,
    name                VARCHAR(200) NOT NULL,
    name_en             VARCHAR(200),
    category_id         BIGINT       NOT NULL REFERENCES product_categories(id),
    product_type        VARCHAR(32)  NOT NULL CHECK (product_type IN ('FINISHED','SEMI','MATERIAL','SERVICE')),
    brand               VARCHAR(100),
    base_uom_id         BIGINT       NOT NULL REFERENCES uoms(id),
    reference_price     NUMERIC(18,2),
    reference_currency  VARCHAR(3),
    -- 退税所需税务信息（文档 2.8 退税管理 ← 产品管理）
    hs_code             VARCHAR(50),
    tax_rate            NUMERIC(5,2),
    export_rebate_rate  NUMERIC(5,2),
    description         TEXT,
    status              VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('DRAFT','ACTIVE','INACTIVE')),
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by          BIGINT,
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_by          BIGINT
);
CREATE INDEX products_category_idx ON products (category_id) WHERE status = 'ACTIVE';

CREATE TABLE skus (
    id          BIGSERIAL PRIMARY KEY,
    product_id  BIGINT       NOT NULL REFERENCES products(id),
    code        VARCHAR(100) NOT NULL UNIQUE,
    spec        VARCHAR(500),
    attributes  JSONB,                            -- {"color":"red","size":"XL"}
    status      VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX skus_product_idx ON skus (product_id);

CREATE TABLE uom_conversions (
    id          BIGSERIAL PRIMARY KEY,
    product_id  BIGINT        NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    from_uom_id BIGINT        NOT NULL REFERENCES uoms(id),
    to_uom_id   BIGINT        NOT NULL REFERENCES uoms(id),
    factor      NUMERIC(18,6) NOT NULL CHECK (factor > 0),
    UNIQUE (product_id, from_uom_id, to_uom_id)
);

CREATE TABLE packagings (
    id               BIGSERIAL PRIMARY KEY,
    sku_id           BIGINT        NOT NULL REFERENCES skus(id) ON DELETE CASCADE,
    package_type     VARCHAR(50)   NOT NULL,     -- CARTON / PALLET
    qty_per_package  NUMERIC(18,4) NOT NULL,
    gross_weight_kg  NUMERIC(18,4),
    net_weight_kg    NUMERIC(18,4),
    length_cm        NUMERIC(18,2),
    width_cm         NUMERIC(18,2),
    height_cm        NUMERIC(18,2),
    volume_cbm       NUMERIC(18,6),
    is_default       BOOLEAN       NOT NULL DEFAULT false
);

CREATE TABLE product_barcodes (
    id          BIGSERIAL PRIMARY KEY,
    sku_id      BIGINT       NOT NULL REFERENCES skus(id) ON DELETE CASCADE,
    barcode     VARCHAR(100) NOT NULL UNIQUE,
    barcode_type VARCHAR(32) NOT NULL DEFAULT 'EAN13'
);

CREATE TABLE product_attachments (
    id          BIGSERIAL PRIMARY KEY,
    product_id  BIGINT       NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    file_name   VARCHAR(255) NOT NULL,
    file_path   VARCHAR(500) NOT NULL,
    file_size   BIGINT,
    content_type VARCHAR(100),
    uploaded_by BIGINT,
    uploaded_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);
```

> **不变量**：产品服务的 gRPC 接口**不提供 Delete**，只有 `DeactivateProduct`。文档：「产品被合同、采购或库存引用后不能直接删除，只能停用。」

**实现补充（2026-07-27）**：
- 产品编码由 masterdata 取号服务签发（`PRODUCT` 规则，前缀 `P`，5 位），与客户/供应商同源；留空提交即自动生成。
- 参考价、税率、退税率在 SQL 里以 `::text` 出入，Go 侧全程是十进制字符串，不经过 float64。
- 分类下还有产品时拒绝停用（`PD_CATEGORY_IN_USE`），保证产品永远能解析到可见分类。
- 预签名 URL 的**签名覆盖 Host**，所以服务端持有两个对象存储客户端：容器内用 `minio:9000` 读写，签发给浏览器的 URL 用 `MINIO_PUBLIC_ENDPOINT`（本地 `localhost:19000`）签名；region 固定写死，避免签名时反查 bucket 位置。
- 注册附件时校验 object key 前缀必须属于该产品（`PD_FILE_KEY_MISMATCH`），否则客户端可把别处的对象挂到自己名下。

### 5.4 fx（汇率中心）

```sql
CREATE TABLE fx_rates (
    id             BIGSERIAL PRIMARY KEY,
    base_currency  VARCHAR(3)    NOT NULL,
    quote_currency VARCHAR(3)    NOT NULL,
    rate           NUMERIC(18,8) NOT NULL CHECK (rate > 0),
    quote_time     TIMESTAMPTZ   NOT NULL,
    source         VARCHAR(50)   NOT NULL,        -- ECB / BOC / MANUAL
    created_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (base_currency, quote_currency, quote_time, source)
);
CREATE INDEX fx_rates_lookup_idx ON fx_rates (base_currency, quote_currency, quote_time DESC);

-- 最新汇率物化表，避免每次查询都扫时间序列
CREATE TABLE fx_rate_latest (
    base_currency  VARCHAR(3)    NOT NULL,
    quote_currency VARCHAR(3)    NOT NULL,
    rate           NUMERIC(18,8) NOT NULL,
    quote_time     TIMESTAMPTZ   NOT NULL,
    source         VARCHAR(50)   NOT NULL,
    updated_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (base_currency, quote_currency)
);

CREATE TABLE fx_manual_overrides (
    id             BIGSERIAL PRIMARY KEY,
    base_currency  VARCHAR(3)    NOT NULL,
    quote_currency VARCHAR(3)    NOT NULL,
    rate           NUMERIC(18,8) NOT NULL,
    effective_from TIMESTAMPTZ   NOT NULL,
    effective_to   TIMESTAMPTZ,
    reason         TEXT          NOT NULL,
    created_by     BIGINT        NOT NULL,
    created_at     TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE fx_anomalies (
    id             BIGSERIAL PRIMARY KEY,
    base_currency  VARCHAR(3)    NOT NULL,
    quote_currency VARCHAR(3)    NOT NULL,
    rate           NUMERIC(18,8) NOT NULL,
    prev_rate      NUMERIC(18,8) NOT NULL,
    change_pct     NUMERIC(10,4) NOT NULL,
    threshold_pct  NUMERIC(10,4) NOT NULL,
    detected_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    status         VARCHAR(32)   NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','ACKNOWLEDGED','IGNORED'))
);
```

> **不变量**：汇率快照**不存在这张表里**。每张业务单据自带 `fx_rate / fx_rate_at / fx_source` 三列，是值对象内嵌。这样「实时汇率变化后，历史合同、采购单和收汇记录不能跟着改变」在结构上就成立了 —— 单据根本没有指向汇率表的引用。
>
> **汇率源**（业务确认）：免费 API（frankfurter.app，欧央行数据，工作日每日更新）+ `fx_manual_overrides` 手工录入兜底。本位币 USD，核心币对 USD/CNY、EUR/USD。手工价与 API 价偏差超过阈值时写入 `fx_anomalies` 并发预警事件。

### 5.4.1 直接新建合同（2026-07-29 落地）

业务确认后的现实:**合同是邮件谈的,签好的 PDF 传回来**,报价单这一步根本不存在。
所以合同不能只有「由报价生成」一条入口。

`POST /api/contracts/direct`,和报价单那条路并存。schema 不用改——
`quotation_id` 本来就可空,而且那条「一个报价单只能有一个合同」的唯一索引是
**部分索引**(`WHERE quotation_id IS NOT NULL`),所以没有报价单的合同之间不会互相冲突。

**两条路的区别只有一处**

报价单那条有一条这条没有的规则:**只有报价单负责人可以生成合同**。
因为那里已经存在一个负责人需要保护;而直接新建时,录入的人**就是第一个负责人**,
没有谁的所有权需要保护。

**明细仍然必须录**

合同本身是上传的 PDF,但**下游没有任何东西能读 PDF**:
出运要知道还差多少没发,收款要有金额去对账。所以没有明细的合同看起来完整、
实际上是死的——直接拒绝,错误信息里说清为什么:

```
EX_ITEMS_REQUIRED  请至少录入一条合同明细——发货进度和收款对账都要靠它
```

**汇率**按新建时取一次存下来,和报价单同样的规则。顺带修了个小谎:
详情页原来一律显示「继承自报价单」,直接新建的合同并没有报价单可继承,
现在按有没有 `quote_no` 分开显示。

**实测（2026-07-29）**

```
不录明细        → EX_ITEMS_REQUIRED
直接新建 800 件 → CT-202607-0027  quotation_id = NULL  负责人 = 录入人
提交审批        → 通过 → 待签署
上传签署件      → MinIO 直传，登记为 SIGNED / MANUAL
确认签署        → EFFECTIVE
下游            → inventory 预留 800，缺口 0        ← 整条链和报价单来的合同一样
```

### 5.5 approval（审批流）

```sql
CREATE TABLE approval_definitions (
    id         BIGSERIAL PRIMARY KEY,
    biz_type   VARCHAR(50)  NOT NULL,             -- CONTRACT / PURCHASE_ORDER / STOCK_ADJUST / PAYMENT / LC_AMENDMENT
    name       VARCHAR(100) NOT NULL,
    version    INT          NOT NULL DEFAULT 1,
    condition  JSONB,                             -- 生效条件，如 {"amount_gte": 100000}
    status     VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('DRAFT','ACTIVE','INACTIVE')),
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (biz_type, version)
);

CREATE TABLE approval_nodes (
    id            BIGSERIAL PRIMARY KEY,
    definition_id BIGINT      NOT NULL REFERENCES approval_definitions(id) ON DELETE CASCADE,
    seq           INT         NOT NULL,
    name          VARCHAR(100) NOT NULL,          -- 销售主管审批 / 财务主管审批 / 总经理审批
    approver_type VARCHAR(32) NOT NULL CHECK (approver_type IN ('ROLE','EMPLOYEE','DEPT_MANAGER','SUPERIOR')),
    approver_ref  BIGINT,                          -- role_id 或 employee_id
    approve_mode  VARCHAR(32) NOT NULL DEFAULT 'ANY' CHECK (approve_mode IN ('ANY','ALL')),
    allow_return  BOOLEAN     NOT NULL DEFAULT true,
    UNIQUE (definition_id, seq)
);

CREATE TABLE approval_instances (
    id              BIGSERIAL PRIMARY KEY,
    definition_id   BIGINT      NOT NULL REFERENCES approval_definitions(id),
    biz_type        VARCHAR(50) NOT NULL,
    biz_id          BIGINT      NOT NULL,
    biz_no          VARCHAR(100) NOT NULL,        -- 单据编号，便于待办展示
    biz_summary     JSONB,                        -- 摘要：客户、金额，避免待办列表回调业务服务
    submitter_id    BIGINT      NOT NULL,
    status          VARCHAR(32) NOT NULL DEFAULT 'RUNNING'
                    CHECK (status IN ('RUNNING','APPROVED','REJECTED','CANCELLED')),
    current_seq     INT         NOT NULL DEFAULT 1,
    submitted_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ,
    UNIQUE (biz_type, biz_id, submitted_at)
);
CREATE INDEX approval_instances_biz_idx ON approval_instances (biz_type, biz_id);

CREATE TABLE approval_tasks (
    id          BIGSERIAL PRIMARY KEY,
    instance_id BIGINT      NOT NULL REFERENCES approval_instances(id) ON DELETE CASCADE,
    node_seq    INT         NOT NULL,
    node_name   VARCHAR(100) NOT NULL,
    assignee_id BIGINT      NOT NULL,
    status      VARCHAR(32) NOT NULL DEFAULT 'PENDING'
                CHECK (status IN ('PENDING','APPROVED','REJECTED','RETURNED','SKIPPED','CANCELLED')),
    comment     TEXT,
    acted_at    TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- 「我的待办」的核心索引
CREATE INDEX approval_tasks_todo_idx ON approval_tasks (assignee_id, status) WHERE status = 'PENDING';
```

> 审批引擎不知道「合同」是什么，业务服务不知道审批有几级。业务服务调 `Submit`，订阅 `ApprovalApproved` 事件推进自己的状态机。「我的待办」直接查 `approval_tasks`，无需单独建表。

**实现补充（2026-07-27）**：
- 实例状态增加 `RETURNED`：退回和驳回一样是终态，业务服务收到 `ApprovalReturned` 后应允许单据重新编辑并再次提交（新实例）。
- `approval_instances` 上有 `WHERE status='RUNNING'` 的唯一索引：同一单据同时只能有一个在途审批，重复提交返回 `AP_ALREADY_RUNNING`。
- 节点审批人 `ROLE` 类型在**提交/进入节点时**调 iam 的 `ListRoleMembers` 落成具体任务；该 gRPC 调用发生在事务之外（数据库事务里不等待其他服务）。
- 决策事件通过 outbox 与状态变更同事务写入，relay 发到 `erp.approval.task.v1`。

### 5.6 export（出口业务）

这是最大的服务。表按子域分组。

#### 5.6.1 报价单

```sql
CREATE TABLE quotations (
    id                BIGSERIAL PRIMARY KEY,
    quote_no          VARCHAR(50)   NOT NULL UNIQUE,
    customer_id       BIGINT        NOT NULL,
    contact_id        BIGINT,
    currency          VARCHAR(3)    NOT NULL,
    incoterm          VARCHAR(20)   NOT NULL,     -- FOB / CIF / EXW
    port_of_loading   VARCHAR(100),
    port_of_discharge VARCHAR(100),
    payment_method    VARCHAR(50)   NOT NULL,
    valid_until       DATE          NOT NULL,
    -- 汇率快照（值对象内嵌）
    fx_rate           NUMERIC(18,8) NOT NULL,
    fx_rate_at        TIMESTAMPTZ   NOT NULL,
    fx_source         VARCHAR(50)   NOT NULL,
    fx_base_currency  VARCHAR(3)    NOT NULL,
    total_amount      NUMERIC(18,2) NOT NULL DEFAULT 0,
    remark            TEXT,
    status            VARCHAR(32)   NOT NULL DEFAULT 'DRAFT'
                      CHECK (status IN ('DRAFT','SENT','ACCEPTED','REJECTED','EXPIRED','CANCELLED')),
    sales_employee_id BIGINT        NOT NULL,
    sent_at           TIMESTAMPTZ,
    responded_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_by        BIGINT        NOT NULL,
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_by        BIGINT
);
CREATE INDEX quotations_customer_idx ON quotations (customer_id, created_at DESC);

CREATE TABLE quotation_items (
    id            BIGSERIAL PRIMARY KEY,
    quotation_id  BIGINT        NOT NULL REFERENCES quotations(id) ON DELETE CASCADE,
    line_no       INT           NOT NULL,
    product_id    BIGINT        NOT NULL,
    sku_id        BIGINT,
    product_code  VARCHAR(100)  NOT NULL,        -- 冗余快照，防止产品改名影响历史报价展示
    product_name  VARCHAR(200)  NOT NULL,
    spec          VARCHAR(500),
    qty           NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    uom_id        BIGINT        NOT NULL,
    uom_code      VARCHAR(20)   NOT NULL,
    unit_price    NUMERIC(18,4) NOT NULL,
    amount        NUMERIC(18,2) NOT NULL,
    remark        TEXT,
    UNIQUE (quotation_id, line_no)
);
```

> **不变量**：报价单不能扣库存、不能创建采购单。`export` 服务的报价用例里不允许调用 `inventory` 或 `procurement` 的写方法。

**报价单实现补充（2026-07-27）**：
- 报价单记录**发给客户的哪位联系人**（`contact_id` + 姓名/邮箱快照）；不指定则取主联系人。
  选了不属于该客户的联系人会被拒绝（`EX_CONTACT_NOT_FOUND`）。

**「发送」的现状与目标（2026-07-27 业务确认）**

现状：`SendQuotation` 只做状态流转与时间戳，**系统不发邮件**，实际投递由销售自己完成。

目标（**已确认为需求，非可选增强**）：点「发送」即把报价单**真正发到客户联系人邮箱**，
客户在邮件里点「接受」或「拒绝」**直接更新报价单状态**。落地在阶段 6，与通知服务一起。

### 发送侧

1. **报价单 PDF**：模板（抬头、买卖双方、明细、单价金额、贸易术语、有效期、条款），
   生成后存对象存储，`quotations` 记 `pdf_key`
2. **邮件通道**：export 在发送事务里往 outbox 写 `QuotationSent`（含 `contact_email`、
   `pdf_key`、两个动作链接），通知服务消费后发信。**SMTP 故障不回滚报价单状态**，
   重试由消费端负责
3. **送达记录**：`quotation_deliveries (quotation_id, to_email, sent_at, result, error)`，
   退信可见、可重发

### 客户确认侧（无需登录的公开链接）

```
CREATE TABLE quotation_response_tokens (
    token_hash  BYTEA        PRIMARY KEY,      -- 只存哈希，与密码同理
    quotation_id BIGINT      NOT NULL,
    action      VARCHAR(16)  NOT NULL CHECK (action IN ('ACCEPT','REJECT')),
    expires_at  TIMESTAMPTZ  NOT NULL,         -- 不晚于 valid_until
    used_at     TIMESTAMPTZ,
    used_ip     VARCHAR(64),
    used_agent  TEXT
);
```

- 每张报价单发送时生成**两个一次性 token**（接受 / 拒绝），拼进邮件里的两个链接
- 网关开**两条免登录路由**：`GET /public/quotations/respond/{token}` → 落地页确认 →
  `POST` 提交。免登录是这里唯一的例外，因此单独一组路由、单独限流
- 校验：token 哈希命中 → 未过期 → 未使用 → 报价单仍是 `SENT`。任一条不满足都返回
  同一个中性提示（「链接已失效」），**不透露报价单是否存在、是否已被答复**
- 成功后：写 `used_at / used_ip / used_agent`，把报价单推到 `ACCEPTED` / `REJECTED`，
  并把**同一张单的另一个 token 一并作废**
- `responded_by` 记录来源：`CUSTOMER_LINK`（客户点的）或 `SALES`（销售代录）。
  销售代录的路径保留——客户经常直接打电话说「就这么定了」

> **安全边界**：这是系统唯一对公网开放的写操作。它不读取任何客户数据、不返回单据内容，
> 只做「把这张单标记为接受/拒绝」这一件事；token 一次性、短期、按动作分发，
> 泄漏一个 token 的最大影响是**一张报价单被误答复**，销售可以看到答复来源与 IP 并纠正。
- 汇率快照在**创建时**读取一次并写入单据；编辑草稿**不刷新**快照，否则同一张单在两次编辑之间会悄悄改价。
- 明细的产品编码、名称、单位在保存时由服务端从 product 服务取回并**固化**：产品日后改名不会改写历史报价。
- 金额按行四舍五入后再求和，合计恰好等于各行之和（而不是重新对乘积取整）——客户看到的是行金额。
- `base_amount` 用快照汇率折算存下，报表跨币种汇总时不必重算。
- 状态机集中在 `lifecycle.go`：DRAFT→SENT→ACCEPTED/REJECTED/EXPIRED/CANCELLED，非法跳转返回 `EX_STATUS_TRANSITION`；已发送的报价不可编辑（`EX_NOT_DRAFT`）。

#### 5.6.2 合同与版本

```sql
CREATE TABLE contracts (
    id                 BIGSERIAL PRIMARY KEY,
    contract_no        VARCHAR(50) NOT NULL UNIQUE,
    quotation_id       BIGINT REFERENCES quotations(id),
    customer_id        BIGINT      NOT NULL,
    current_version_id BIGINT,                    -- 指向 contract_versions，循环引用故可空
    status             VARCHAR(32) NOT NULL DEFAULT 'DRAFT'
                       CHECK (status IN ('DRAFT','PENDING_APPROVAL','PENDING_SIGN','EFFECTIVE',
                                         'EXECUTING','COMPLETED','CANCELLED')),
    sales_employee_id  BIGINT      NOT NULL,
    signed_at          TIMESTAMPTZ,
    effective_at       TIMESTAMPTZ,
    completed_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by         BIGINT      NOT NULL
);

-- 不可变版本快照。生效后任何变更都是插入新版本，而不是 UPDATE
CREATE TABLE contract_versions (
    id                BIGSERIAL PRIMARY KEY,
    contract_id       BIGINT        NOT NULL REFERENCES contracts(id),
    version_no        INT           NOT NULL,
    buyer_name        VARCHAR(200)  NOT NULL,
    buyer_address     TEXT,
    seller_name       VARCHAR(200)  NOT NULL,
    seller_address    TEXT,
    currency          VARCHAR(3)    NOT NULL,
    incoterm          VARCHAR(20)   NOT NULL,
    port_of_loading   VARCHAR(100),
    port_of_discharge VARCHAR(100),
    payment_method    VARCHAR(50)   NOT NULL,
    delivery_date     DATE          NOT NULL,
    terms             TEXT,
    total_amount      NUMERIC(18,2) NOT NULL,
    -- 合同汇率快照，签订时固定，永不随实时汇率变化
    fx_rate           NUMERIC(18,8) NOT NULL,
    fx_rate_at        TIMESTAMPTZ   NOT NULL,
    fx_source         VARCHAR(50)   NOT NULL,
    fx_base_currency  VARCHAR(3)    NOT NULL,
    change_reason     TEXT,                       -- 变更版本必填
    status            VARCHAR(32)   NOT NULL DEFAULT 'DRAFT'
                      CHECK (status IN ('DRAFT','PENDING_APPROVAL','APPROVED','SUPERSEDED')),
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_by        BIGINT        NOT NULL,
    UNIQUE (contract_id, version_no)
);

CREATE TABLE contract_items (
    id                  BIGSERIAL PRIMARY KEY,
    contract_version_id BIGINT        NOT NULL REFERENCES contract_versions(id) ON DELETE CASCADE,
    line_no             INT           NOT NULL,
    product_id          BIGINT        NOT NULL,
    sku_id              BIGINT,
    product_code        VARCHAR(100)  NOT NULL,
    product_name        VARCHAR(200)  NOT NULL,
    spec                VARCHAR(500),
    qty                 NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    uom_id              BIGINT        NOT NULL,
    uom_code            VARCHAR(20)   NOT NULL,
    unit_price          NUMERIC(18,4) NOT NULL,
    amount              NUMERIC(18,2) NOT NULL,
    hs_code             VARCHAR(50),
    remark              TEXT,
    UNIQUE (contract_version_id, line_no)
);
```

> **不变量**：`contract_versions` 状态为 `APPROVED` 后禁止 UPDATE，由数据库触发器强制。变更走「新建版本 → 重新审批 → **客户重新签署**（合同状态回退 `PENDING_SIGN`）→ 新版本生效、旧版本置 SUPERSEDED」。
>
> **重签期间的存量业务**（业务确认）：已确认的出货计划与已锁定库存**继续执行，不冻结**。新版本生效时系统逐条校验，若新版本数量小于某明细已累计计划/已出库数量，标记冲突并生成风险预警，由人工处理（调整计划或再次变更合同）——系统不自动取消任何计划。

**合同实现补充（2026-07-28）**

- **`current_version_id` 指的是「当前生效」而不是「最新」**。变更期间它停在旧版本上不动，
  直到客户签署新版本才移动；首次签署前为 `NULL`。因此「正在编辑的版本」要按
  `version_no` 最大取（`LatestContractVersionID`），两者不能混用。
  阶段 5 的出货计划引用合同明细时，必须走 `current_version_id`。
- **冻结由数据库触发器强制**，不是只靠应用层：
  `contract_versions_freeze` 拦截对 `APPROVED` / `SUPERSEDED` 版本的任何 UPDATE，
  只放行 `APPROVED → SUPERSEDED` 这一种状态变化且其余列必须完全一致；
  `contract_items_freeze` 同样拦截已冻结版本下明细的 INSERT / UPDATE / DELETE。
  直连数据库改数也会被拒。
- **驳回要回到哪里，靠 `status_before_approval` 记录**。首次审批被驳回回到 `DRAFT`，
  但对已生效合同发起的变更被驳回必须回到 `EFFECTIVE` / `EXECUTING`，
  仅凭当前状态无法区分，所以提交审批时把原状态存下来。
- **审批实例先建，本地状态后提交**。远程副作用无法参与本地事务，
  所以顺序是「调 `approval.Submit` → 成功后再提交本地状态变更」。
  若本地提交失败，重试时 `Submit` 返回 `AP_ALREADY_RUNNING`，
  export 会查回那条 RUNNING 实例复用，不会死在半路。
- **一张报价单只能有一份存活合同**：`contracts_quotation_idx` 是
  `WHERE quotation_id IS NOT NULL AND status <> 'CANCELLED'` 的部分唯一索引。
  作废后允许基于同一报价重新起草。「由报价生成」的下拉用
  `ListQuotations(without_contract=true)` 过滤，不把注定失败的选项摆给用户。
- **`delivery_date` 允许建单时为空**，与设计稿的 `NOT NULL` 不同：报价单上没有交期可继承，
  强制填写会让「一键生成」变成一个必须先填表的对话框。改为**提交审批前校验**。
- **卖方抬头暂时来自配置**（`SELLER_NAME` / `SELLER_ADDRESS`），
  等出现第二个签约主体时再迁到 masterdata 的公司主体表。
- **草稿的明细可改,变更版本的明细也可改,但入口不同**:
  `UpdateContract` 改的是**当前草稿版本**(首次起草、被驳回退回的、以及尚未提交的变更版本),
  `ChangeContract` 是**对已生效合同新开一版**。最初只让 `UpdateContract` 改条款,
  结果「因为单价报错被驳回」这种最常见的情况无路可走——两个入口都够不着。
  现在两者都收 `items`,`UpdateContract` 在版本仍是 `DRAFT` 时删了重写,
  汇率快照始终继承不动:改错字不该顺带把交易重新定价。
- **暂缓**：合同不支持脱离报价单独建（`quotation_id` 已可空，接口留待需要时补）；
  已生效合同的「终止」与「作废」是两回事，目前只实现作废（且仅限从未生效过的合同）。

**合同文件（2026-07-28 已实现）**

`contract_attachments` 存我方拟稿(`DRAFT`)、客户签回件(`SIGNED`)和其他附件(`OTHER`)。
上传是三步：`presign` 拿签名 URL → 浏览器 **PUT 直传对象存储** → `register` 登记落库。
文件字节**不经过网关**，一份 30 MB 的扫描件不会变成一个 30 MB 的请求体。

- **附件绑定到版本而不是合同**。变更会产生新版本并需要客户重新签署，
  「客户到底签的是哪一版」只有靠 `contract_version_id` 才答得出来。
- `file_key` 前缀是 `contracts/{租户}/{合同}/`，`register` 时校验前缀，
  否则可以拿别的合同的 key 登记到自己名下，把别人的文件读出来。
- **删除只删记录，不删对象**。误点一下就把签回的合同弄丢，代价太大；
  孤儿对象留给后续的清扫任务。

**客户签署：两条路，一条自动一条人工（2026-07-28 业务确认）**

**人工路径已收口（2026-07-28）**,平台路径待阶段 6。

原来「客户已签署」是个裸按钮:点了合同就生效,不要求任何凭证,
签回件传不传、传成什么类型都不影响。现在这条路上有四道闸:

| 闸 | 落点 | 拒绝码 |
|---|---|---|
| 签署前必须有该版本的 `SIGNED` 附件 | `SignContract` | `EX_SIGNED_COPY_REQUIRED` |
| 生效后签署件不能删 | `RemoveContractFile` | `EX_SIGNED_COPY_LOCKED` |
| REST 上传一律 `MANUAL` | `RegisterContractFile` | proto 里根本没有 `source` 字段 |
| 签署方式记在合同上 | `contracts.signature_source` | — |

第三条比原计划更硬:`RegisterContractFileRequest` 里**没有** `source`,
所以伪造请求在网关就被 protojson 挡下(`unknown field "source"`),
根本到不了业务代码。

「必须有签署件」这一条**按版本算**——v1 的扫描件不能作为 v2 的凭证。

列表页不再有「签署」快捷按钮:签署需要凭证,而凭证只有抽屉里看得见,
一个点了必然报错的按钮不如没有。

实测:
- 合同 4(无签署件)点签署 → `EX_SIGNED_COPY_REQUIRED`;
  上传签回件后再点 → `EFFECTIVE`,`signature_source = MANUAL`
- 合同 3 已生效,删它的签署件 → `EX_SIGNED_COPY_LOCKED`
- 登记时塞 `"source":"PLATFORM"` → 网关 `GATEWAY_BAD_JSON`

> 改了 proto 之后**网关也要重建**。只重建 export 的话,
> 网关里编译进去的旧 message 没有第 12 号字段,会把它静默丢掉——
> 数据库里是 `MANUAL`,接口返回却是空。排查过一次,记在这里。

目标是**两条并存的路**——因为大量出口客户不用电子签平台,
他们打印、签字、盖章、扫描、邮件发回来。没有人工那条路,业务做不下去;
只有人工那条路,签名就没有可信来源。

| | ① 平台签署（自动） | ② 邮件签回（人工） |
|---|---|---|
| 客户怎么签 | DocuSign / e签宝,点 Finish | 打印签字盖章,扫描邮件发回 |
| 谁触发状态变更 | **平台 webhook 自动推 `EFFECTIVE`** | 员工点「客户已签署」 |
| 界面上有没有签署按钮 | **没有**,等回调 | 有 |
| 签回件哪来的 | 系统从平台拉回,自动存档 | **员工上传,且必须先传才能点签署** |
| 附件记录 | `SIGNED` / `PLATFORM` | `SIGNED` / `MANUAL` |
| 谁认的 | `signed_by = CUSTOMER_PLATFORM` | `signed_by = SALES` |

**关键约束:上传本身不构成签署,签署也不能没有凭证。**

- 附件加 `source` 列(`PLATFORM` / `MANUAL`),**`PLATFORM` 只有 webhook 处理器能写**,
  REST 上传路由硬编码 `MANUAL`。这是代码层面能强制的,不靠约定。
- 走人工路径时,「客户已签署」**要求该版本已有 `SIGNED` 附件**,否则拒绝
  ——手工确认必须留下它凭什么确认的东西。
- 界面上两种来源要**视觉可分**:平台回传标「平台签署」,人工的标「人工留档」。
  不标注,用户就会默认它们等价,而它们的证据力完全不同。

**为什么不干脆禁止员工上传签回件**:那会把纸质签署流程整个挡在门外,
结果不是更规范,而是大家把签回件传成「其他附件」——连它是签回件都不知道了,
数据反而更糟。要管的是**来源可辨**,不是**谁能上传**。

**平台对接的实现要点（阶段 6）**

```sql
CREATE TABLE contract_signature_tokens (
    token_hash          BYTEA        PRIMARY KEY,   -- 只存哈希，与密码同理
    contract_id         BIGINT       NOT NULL,
    contract_version_id BIGINT       NOT NULL,      -- 签的是哪一版，不可含糊
    action              VARCHAR(16)  NOT NULL CHECK (action IN ('SIGN','DECLINE')),
    expires_at          TIMESTAMPTZ  NOT NULL,
    used_at             TIMESTAMPTZ,
    used_ip             VARCHAR(64),
    used_agent          TEXT
);
```

- 回传口两个,都在 export:`POST /public/contracts/sign/callback`(平台 webhook,验签)
  和 `POST /public/contracts/sign/{token}`(自建链接,给不接平台时用)
- **webhook 必须验签**,用平台的签名头。这是唯一不带登录态却能改合同状态的入口。
- **回调幂等**:平台会重投,用平台的信封 id 走 `processed_events` 去重,和 Kafka 消费端同一套。
- **签完的 PDF 拉回来存成 `SIGNED/PLATFORM`**,绑到当时那个版本。平台链接会过期,我们的对象存储不会。
- **版本一变旧 token 立即失效**:变更版本获批后作废旧版所有未使用 token,
  否则客户可能签到一份已经被替换掉的合同。
- 校验失败一律返回**同一句中性提示**(「链接已失效」),不透露合同是否存在、是否已签。
- 免登录路由单独一组、单独限流,和报价单答复共用中间件。

**报价单的接受/拒绝走完全相同的形状**,只是没有「必须上传」那一条
——意向确认不需要纸质凭证。

**审批流现状（2026-07-28）**

`CONTRACT` 定义了两级,都按**角色**指定,串行:

| 顺序 | 节点 | 审批人 | 模式 |
|---|---|---|---|
| 1 | 销售主管审批 | 角色 `SALES_MANAGER` | ANY |
| 2 | 总经理审批 | 角色 `SUPER_ADMIN` | ANY |

- 提交时只生成第一个节点的任务;第一级通过后才生成第二级。第二级审批人在此之前看不到这单。
- **任何一级的驳回或退回都立即结束整个实例**,退回给提交人而不是上一级。
  重新提交是开一个全新实例,从第一级重走。
- **`ANY` 模式下同节点其他人的任务置 `SKIPPED`;实例结束时所有未处理任务置 `CANCELLED`。**
  两者都不会出现在那个人的「我已驳回/我已退回」页签里(那里只装他自己按过的),
  而是落在「全部已处理」,状态显示「已跳过」或「已取消」。实测:管理员驳回后,
  同节点李娜的任务变成 `CANCELLED`。

**驳回与退回的分工(2026-07-28 修订)**

原先两者对合同的效果完全相同,都退回可编辑草稿——这**架空了「驳回」**:
提交人原封不动再点一次提交就又走一遍。现在:

| 动作 | 版本 | 合同 | 之后能做什么 |
|---|---|---|---|
| 退回 `RETURN` | → `DRAFT` | → 提交前状态 | 改明细、改条款、重新提交 |
| 驳回 `REJECT` | → `REJECTED`(冻结) | 首次审批 → `REJECTED`<br>变更被驳回 → 保持 `EFFECTIVE` | **只能作废**;改不了也提交不了 |

- 驳回一个**变更版本**不会拖垮合同本身:旧版本仍在执行,合同状态不变。
- **被驳回的变更版本不能卡死后续变更**:`ChangeContract` 只在最新版本是
  `DRAFT` / `PENDING_APPROVAL`(即真的有一版在处理中)时才拒绝。
- **变更版本的基线取自 `current_version_id`(在force的那版),不是最新版**。
  上一次尝试被驳回了,新一版应该从正在执行的条款起草,而不是从一份被否决的稿子。

**审批进度对所有人可见(2026-07-28)**

新权限码 `approval:instance:read`「查看审批进度」,已授予全部现有角色,
网关的 `/api/approvals/instances*` 两条读路由改用它。
**看进度和能审批是两回事**:销售需要知道自己的合同卡在销售主管那里,
但不该因此获得任何审批权。审批权仍然只由审批流的角色决定。

> 前端权限清单是**登录时缓存**的,新增权限码后已登录的会话要重新登录才生效。
> 后端不受影响(网关每次请求都实时问 iam)。

> `export:contract:approve` 仍然**空转**——种子里有、管理员有,但没有任何代码检查它。
> 建议删掉:能不能审由审批流决定,留着只会让人以为改它有用。

**合同审批流的现行配置(2026-07-28)**

```
seq 1  直属上级审批    MANAGER  levels=1  ANY
seq 2  上级的上级审批  MANAGER  levels=2  ANY
```

两级都按**汇报线**走，都从**提交人**起算：第 1 级是提交人的上级，
第 2 级是那个人的上级。汇报线：张三 → 李娜 → 系统管理员（顶层无上级）。

实测三种深度：

| 提交人 | 第 1 级 | 第 2 级 | 结果 |
|---|---|---|---|
| 张三 | 李娜 | 系统管理员 | 两级都走 |
| 李娜 | 系统管理员 | 无人 → 跳过 | 一级即完成 |
| 系统管理员 | 无人 | 无人 | 实例直接 APPROVED，任务数 0 |

**没人可审的层级跳过，而不是报错。** 汇报线在公司最顶端就到头了，
硬要求两级会让高层永远提交不了单据。全部层级都无人时，实例仍然创建并立即标记
`APPROVED`（备注「提交人之上没有审批人，自动通过」），
这样审计链上仍有一条记录说明为什么没人审，业务服务也照常收到事件。

**审批流配置界面(2026-07-28)**

`/settings/approvals`,权限码 `approval:flow:read` / `approval:flow:write`(只给超级管理员)。
可增删节点、上下移动、选审批人类型与或签会签。

**保存永远写新版本,从不原地改。** 这是整个设计的关键:
原地改会让正在审批的单据脚下换规则——审批人打开待办,看到的节点可能已经移位或消失。
新版本让它们各走各的,因为每个实例记着自己开始时的 `definition_id`。

```
保存 → 建 version=N+1 (ACTIVE) → 把该单据类型其余 ACTIVE 版本置 INACTIVE
在跑的实例 → definition_id 不动 → 按旧版本走完
```

实测:v1(两级)有一个实例在跑时保存了 v2(三级)——
旧实例走完仍是两级,没有被塞进新增的「财务复核」;之后的新提交才用 v2。
**旧版本永不删除**,列表里显示它还有多少实例在跑,就是不能删的原因。

校验:空流程 `AP_DEFINITION_EMPTY`、缺节点名 `AP_NODE_NAME_REQUIRED`、
选了角色/指定人却没选具体对象 `AP_APPROVER_REF_REQUIRED`。
`MANAGER` 的 `approver_ref` 是级数,允许为空(当作 1)。

**按金额分档(2026-07-28)**

一个单据类型可以有多套流程,按金额自动选,**人不参与选择**。
让提交人挑自己这单走哪套流程等于让人自己选谁来批自己,审批就失去意义了;
所以配置的粒度是「金额区间」,不是「某个员工」也不是「某一份合同」。

**区间用下限定义,不存上限**:

```
0 起        → 直属上级 → 上级的上级          （两级）
100,000 起  → 直属上级 → 上级的上级 → 总经理会签（三级）
```

上限就是下一档的下限。存一个独立的 `max_amount` 迟早会和下一档的下限对不上,
产生**空档**(「10 万整没有流程匹配」)或**重叠**(「两套流程都匹配」)。
选择逻辑是「下限 ≤ 金额 的最大那一档」,0 档永远存在,所以恰好命中一条。
这两种错法在数据模型里就表达不出来。

实测:5,000 的合同走 0 档(两级),250,000 的走 10 万档(三级)。

**引擎仍然不认识业务。** `Submit` 多带一个 `amount`,由业务服务**折算成本位币**后传入
(比较 100,000 JPY 和一个按美元设的阈值毫无意义);
引擎只把它当一个可比较的标量去匹配区间下限,不知道那是钱。
实例上存了这个金额,用来解释「这单当时为什么走了三级」。

**版本按区间独立计数**:改大额流程不会让小额流程的版本号跟着跳。
0 档不能删——总得有东西匹配金额 0。

**审批流的改动现在有作者**:`approval_definitions.created_by`。
能改流程的人可以把任何单据路由给自己再自己批,这个动作不该是全系统唯一没有署名的那个。

**审批人的三种指定方式(2026-07-28)**

| `approver_type` | 含义 | 什么时候用 |
|---|---|---|
| `ROLE` | 该角色下所有在职员工 | 岗位职责固定,人员会变动 |
| `EMPLOYEE` | 指定某个人 | 只有一个人能拍板,且不打算换 |
| `MANAGER` | **提交人的上级**，`approver_ref` 是往上几级 | 按汇报线走，每个人报给自己的上司 |

`MANAGER` 永远从**提交人**起算，`approver_ref` 说明往上几级（0 当作 1）。
不从「上一个审批人」起算：那样审批级数会取决于提交人在架构里的位置，
同一份合同基层提交过五关、总监提交过一关，流程就没有确定性了。

上级存在 `employees.manager_id`(自引用、可空)。放在员工身上而不是部门负责人,
是因为「我的上级」和「我部门的负责人」经常不是同一个人:矩阵汇报、副职、
以及挂在一个部门却向另一个部门汇报的人。

- 该层级没人时**跳过该节点**（不是报错）。全部跳过则实例立即通过，见上表。
- 不能把自己设成自己的上级(`IAM_MANAGER_SELF`)。更深的环没有在每次保存时递归检查——
  代价不值得,真出现了会在提交时因为找不到人而失败。

### 4.6 数据范围:谁能看到谁的单据

`role_data_scopes(role_id, module, scope_type, custom_dept_ids)` 从设计之初就在表里,
2026-07-28 才真正接上。业务服务**不自己判断归属**,而是问 iam:

```
export.ListContracts
  → iam.VisibleEmployees(employee_id, module="export")
  → {all: false, employee_ids: [2]}          ← SELF
  → SQL: AND (visible_all OR sales_employee_id = ANY(visible_ids))
```

| scope_type | 看到谁的 |
|---|---|
| `SELF` | 只有自己的 |
| `DEPT` | 本部门 |
| `DEPT_AND_SUB` | 本部门及下级部门(部门 `path` 是物化路径,前缀匹配即可) |
| `CUSTOM` | 指定若干部门 |
| `ALL` | 全部 |

四条设计约束:

1. **没配置时默认 `SELF`,不是 `ALL`。** 没人想过的角色应该看得最少;
   反过来是失败开放,「忘了配」就变成泄漏。
2. **多角色取最宽的那个。** 加一个角色绝不能反而看得更少。
3. **自己的单永远看得见**,不管范围怎么配——被挡在自己创建的单据外面是 bug。
4. **越权按 id 直接访问返回「不存在」而不是「无权限」**:
   告诉别人「这份合同存在但不关你事」本身就是泄漏。

**审批人永远看得见自己要审的单（2026-07-28）**

数据范围之外还有一条**不可配置**的规则:被指派过审批任务的单据一律可读。

这原本是个 bug,只是被掩盖着——李娜的角色恰好是 `ALL`,所以看不出来。
把她改成 `SELF` 之后,待办里躺着三份合同,点进去全是「合同不存在」:
**系统要求她审批一份她无权阅读的文件**。

修法不是放宽范围,而是把两件事分开:

```
可见 = 数据范围（政策选择：能浏览谁的单据）
     ∪ 我被指派过审批任务的单据（正确性要求，不可关闭）
```

**两种事件,给两种人（2026-07-28）**

审批推进时,受影响的是两拨人,他们看的不是同一件事:

| 收件人 | 事件 | 因为 |
|---|---|---|
| 新审批人、任务被作废的同事 | `todo.changed` | 他们的**待办队列**变了 |
| 提交人（= 负责人） | `doc.changed` | 他们的**单据**变了,待办没变 |

合成一种类型的话,提交人的合同页会因为一条不相干的待办到达而整页重刷。

`doc.changed` 在**每一次决策**后都发,不只是流程结束时。
在此之前只有驳回/退回/最终通过才通知提交人,
结果是第一级批完进到第二级时,提交人页面上还停在「待第 1 级」,
要手动刷新才看得到——而他恰恰是最盯着这个的人。

实测:李娜批准合同 17 的第 1 级 →
`erp.live.t1.e1` 收到 `todo.changed`(管理员是第 2 级审批人)、
`erp.live.t1.e2` 收到 `doc.changed`(张三是提交人);
张三挂着的 SSE 连接上落到 `event: doc.changed`。
退回时只有提交人收到,因为没有新审批人也没有兄弟任务。

export 问 approval 要 `MyInvolvedDocuments(biz_type)`(任何状态的任务都算,
审完了也还能回看),与 iam 给的范围取并集。跨库,所以是一次 gRPC,不是 join。

实测(李娜范围=仅本人):她自己的 1 份 + 她审过的 19 份 = 20 份;
管理员那份**没人审过**的 CT-202607-0015 她看不到。
张三从不审批,只看到自己的 12 份。

> **权限和范围是两件事。** 权限码回答「你能不能用这个功能」,
> 数据范围回答「在谁的数据上」。张三有 `export:contract:read`,
> 但范围是 `SELF`,所以他看得到合同页、页面上只有自己那一份。
>
> **合同与报价单都已接上**（2026-07-28）。后续的出货计划、采购单还是全可见，
> 要按同样的方式补 `visible_all / visible_ids` 两个参数。
> 实测：张三（SELF）看到 3 张报价、2 份合同；管理员和李娜（ALL）看到全部 12 张报价。

**已种的默认范围**:超级管理员 `ALL`、销售主管 `ALL`、销售只读 `SELF`。
（2026-07-28 调整:销售主管改为 `SELF`——目前只有「总部」一个部门,
`DEPT`/`DEPT_AND_SUB`/`ALL` 三档完全等价,主管选任何一档都等于看全部。
等真正分了部门,这一档才有区别。）

**配置在角色权限页**(2026-07-28):权限勾选框下面多了一段「数据范围」,按模块选。
改动**立即生效,不需要重新登录**——网关每次请求都实时问 iam,不像权限码那样缓存在前端。
实测:把销售只读从 `SELF` 改成 `DEPT`,张三看到的合同从 4 份变成 13 份,改回即恢复。

> `CUSTOM`(指定若干部门)后端支持,界面暂时没放——目前只有一个部门,选了也没有意义。
- ~~流程只能改数据库~~ **已有配置界面**(2026-07-28,`/settings/approvals`)。
- ~~没有金额分档~~ **已按金额分档**(2026-07-28),见下。
- `export:contract:approve` 权限码目前**空转**:种子建了、管理员有,但代码里无人校验。
  能不能审由审批流的角色决定。要么后续接上,要么删掉,不要让人误以为改它能影响审批。

### 4.7 负责人:读得宽,写得窄

单据的**负责人**是 `sales_employee_id`——报价单是报价的销售,合同继承报价单的销售。
「谁点的按钮」记在 `created_by`,不改变归属。

读和写用两套规则,这是有意的:

```
读 = 数据范围 ∪ 我要审批的        ← 可以很宽:主管要看团队,审批人要看单据
写 = 数据范围                      ← 窄:范围是用来「看管」别人的活,不是替他干
生成合同 = 必须本人                ← 最窄
```

**为什么写不带审批并集。** 被指派审批一份合同,恰恰意味着不该先把它改了再签字。
审批人能读、能批、能退,唯独不能编辑。

**为什么生成合同要求本人（2026-07-28）。** 合同的负责人是报价单的销售,
如果范围更宽的人（比如管理员）能拿别人的报价单生成合同,
那份合同会挂在一个从没同意过的人名下,业绩、提成、后续跟单全跟着错。
所以这一条**不看数据范围**,只看 `quote.sales_employee_id == op.id`,
管理员也不例外:要接手,先转移负责人,转移这件事本身会留痕。

落点(export):

| 位置 | 规则 |
|---|---|
| `CreateContractFromQuotation` | `GetQuotationFor` + 负责人必须等于操作人 |
| `UpdateQuotation` / `Send` / `Respond` / `Cancel` | `mayWrite` |
| `UpdateContract` / `ChangeContract` / `Submit` / `Sign` / `Cancel` | `mustOwnContract` |
| 附件 presign / register / remove | `mustOwnContract` |
| 附件 list | `GetContractFor`——审批人要能打开自己在批的那份 PDF |

在此之前这些写接口**只查权限码不查归属**:任何有 `export:quotation:write` 的人
按 id 就能改别人的单。范围只挡列表不挡写入,等于没挡。

**界面也要按归属显示（2026-07-28 补）**

服务端拦住之后还剩一个界面问题:按钮是按**权限码**渲染的,不看归属。
结果李娜在自己列表里看到张三的草稿合同上挂着「提交审批」,
点下去必然得到 `EX_CONTRACT_NOT_OWNER`。

她**看得见是对的**——那份合同是她退回过的,审批人可读并集本来就该包含它;
错的是给了她一个必然失败的动作。

```
可见  = 数据范围 ∪ 我审批过的     ← 决定列表里有什么
可操作 = 归属                     ← 决定按钮出不出来
```

前端加了 `auth.owns(ownerId)`,合同与报价单的列表按钮、抽屉动作区、
报价单编辑弹窗（他人的一律只读）都按它渲染;
抽屉里不是自己的会显示一行「这份合同由他人负责，你只能查看」,
而不是默默什么都不显示。

为此列表接口补了 `sales_employee_id`——原来只返回名字,前端无从判断。

> 这跟「转移下拉不列当前负责人」「没有签回件时签署按钮置灰」是同一条原则:
> **一个点了必然报错的控件，比没有这个控件更糟。**

**配置在角色权限页**(2026-07-28):权限勾选框下面多了一段「数据范围」,按模块选。
改动**立即生效,不需要重新登录**——网关每次请求都实时问 iam,不像权限码那样缓存在前端。
实测:把销售只读从 `SELF` 改成 `DEPT`,张三看到的合同从 4 份变成 13 份,改回即恢复。

> `CUSTOM`(指定若干部门)后端支持,界面暂时没放——目前只有一个部门,选了也没有意义。
- ~~流程只能改数据库~~ **已有配置界面**(2026-07-28,`/settings/approvals`)。
- ~~没有金额分档~~ **已按金额分档**(2026-07-28),见下。
- `export:contract:approve` 权限码目前**空转**:种子建了、管理员有,但代码里无人校验。
  能不能审由审批流的角色决定。要么后续接上,要么删掉,不要让人误以为改它能影响审批。

### 4.8 负责人转移(2026-07-28 完成)

「生成合同必须本人」把转移变成了必需品:离职、休假、客户改由别人跟,
都要有一条正当路径把单据交出去,否则就只能靠改数据库。

**由上级操作,不是负责人自己转。** 三个理由:

1. **负责人决定谁看得见。** `SELF` 范围下,自己把单子转走等于把它从自己视野里删掉。
   一个人能单方面让一份单据从自己名下消失,审计上说不通。
2. **归属就是业绩。** 提成、KPI 都挂在负责人上。能自己改归属的人,等于能自己改账。
3. **真实触发场景是离职和长假**,这时候当事人往往不在、不愿意、或者已经没账号了。
   把能力放在他手上,恰好在最需要用的时候用不了。

设计要点:

- 新权限码 `export:*:transfer`,默认给主管和管理员;不复用 `write`。
- 只能在**操作人自己的数据范围内**转:主管不能把单据丢给不相干部门的人。
- **审批中(`PENDING_APPROVAL`)不允许转移**。审批流是按*提交人*的汇报线生成的,
  中途换人会让已发出的任务对不上新的负责人。要转先撤回或等审批结束。
- 转移写一张 `ownership_transfers(biz_type, biz_id, from_id, to_id, by_id, reason, at)`,
  不是原地改一个字段就完事——没有这张表,业绩争议无从查起。

**转移的单位是「一单生意」,不是「一份单据」。**

报价单和合同是严格 1:1 的:

```sql
CREATE UNIQUE INDEX contracts_quotation_idx ON contracts (tenant_id, quotation_id)
    WHERE quotation_id IS NOT NULL AND status <> 'CANCELLED';
```

一张报价单最多一份未作废的合同;作废后索引放开,可以重开一份。
而且目前**只有**从报价单生成合同这一条路,`quotation_id` 可空只是给将来的
独立合同留位置。所以今天每份合同都恰好对应一张报价单。

分开转会断在「作废重开」这条路径上:

```
Q(李娜) → C(李娜)      生成时校验通过,两边同主
C 作废
主管把 C 转给张三       Q 还挂在李娜名下
张三重开合同 → 被挡     「只有报价单负责人可以生成合同」
张三去转 Q → 看不见     SELF 范围下 Q 不在他视野里
```

张三拿着一份死合同,前面没路,而且看不到自己被什么挡住。
另外李娜停用后 Q 成了孤儿单,接手的人追不到价格和汇率的来源。

所以维持一条不变式:

```
同一条 报价单 → 合同 链上,负责人恒等
```

生成时的「必须本人」保证起点相同,转移时整条链一起转保证之后一直相同。

**连带结论:业绩不能靠 `sales_employee_id` 算。**
在这个规则下这个字段的含义固定成「现在归谁管」,不是「当时谁做的」——
人走了单子转出去,历史业绩不该跟着改名。
要做业绩报表得在**成交那一刻单独记一笔快照**,
跟汇率快照、客户名快照同一个道理,而不是 join 合同表读一个会变的外键。

**什么情况才该分开:** 「销售报价、跟单员执行」这种分工——
但那是**加一个 `handler_id` 字段**,不是把负责人拆成两个。
拆负责人会同时打坏可见性和业绩;加字段只是多一个人能看到。目前无此需求。

**落地(2026-07-28)**

| | |
|---|---|
| 权限码 | `export:ownership:transfer`,种给超级管理员和销售主管,**不给销售只读** |
| 接口 | `POST /api/ownership/transfer`、`GET /api/ownership/transfers` |
| 入口 | 报价单号或合同号任选一端,另一端自己找出来一起转 |
| 留痕 | `ownership_transfers`,一份单据一行,所以一次交接产生两行 |

拒绝的情况:接收人就是当前负责人、接收人已停用、
单据或接收人不在操作人的数据范围内、合同正在审批中。

> 界面上「转给」的下拉**不列当前负责人**,也不列已停用的人——
> 一个选了必然报错的选项比没有这个选项更糟。

实测:QT-20260728-0012 与 CT-202607-0016 从李娜转到张三再转回,
两端同步移动、两行流水;审批中的 CT-202607-0025 被 `EX_TRANSFER_IN_APPROVAL` 挡下。

**注意:销售主管现在转不了任何东西。** 权限有,但数据范围是 `SELF`,
而转移要求单据和接收人都在自己范围内——范围里只有自己,就没有人可以转给。
等分了部门、主管改成 `DEPT` 之后这一档才活过来;目前实际能转移的只有管理员。

#### 5.6.3 出货计划

```sql
CREATE TABLE shipment_plans (
    id                BIGSERIAL PRIMARY KEY,
    plan_no           VARCHAR(50) NOT NULL UNIQUE,
    contract_id       BIGINT      NOT NULL REFERENCES contracts(id),
    batch_no          INT         NOT NULL,       -- 第几批
    planned_out_date  DATE        NOT NULL,       -- 计划出库
    planned_ship_date DATE        NOT NULL,       -- 计划装运
    warehouse_id      BIGINT,
    voyage_id         BIGINT,                     -- 引用 shipping.voyages
    status            VARCHAR(32) NOT NULL DEFAULT 'DRAFT'
                      CHECK (status IN ('DRAFT','PENDING_STOCK','STOCK_SHORTAGE','CONFIRMED',
                                        'PARTIALLY_SHIPPED','SHIPPED','DELIVERED','CANCELLED')),
    shortage_detail   JSONB,                      -- 库存不足时的缺口明细
    remark            TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by        BIGINT      NOT NULL,
    UNIQUE (contract_id, batch_no)
);

CREATE TABLE shipment_plan_items (
    id               BIGSERIAL PRIMARY KEY,
    plan_id          BIGINT        NOT NULL REFERENCES shipment_plans(id) ON DELETE CASCADE,
    contract_item_id BIGINT        NOT NULL REFERENCES contract_items(id),
    product_id       BIGINT        NOT NULL,
    sku_id           BIGINT,
    planned_qty      NUMERIC(18,4) NOT NULL CHECK (planned_qty > 0),
    locked_qty       NUMERIC(18,4) NOT NULL DEFAULT 0,
    shipped_qty      NUMERIC(18,4) NOT NULL DEFAULT 0,
    delivered_qty    NUMERIC(18,4) NOT NULL DEFAULT 0,
    uom_id           BIGINT        NOT NULL,
    CHECK (shipped_qty <= planned_qty AND delivered_qty <= shipped_qty)
);
```

> **不变量**：「检查累计出货量不能超过合同数量」。在 `ConfirmShipmentPlan` 用例中，以 `contract_item_id` 聚合所有未取消计划的 `planned_qty`，与 `contract_items.qty` 比较。这个校验必须在**同一事务内加行锁**（`SELECT ... FOR UPDATE` 锁 contract_items），否则并发创建两个计划会双双通过。

#### 5.6.4 信用证

```sql
CREATE TABLE letters_of_credit (
    id                    BIGSERIAL PRIMARY KEY,
    lc_no                 VARCHAR(100)  NOT NULL UNIQUE,
    contract_id           BIGINT        NOT NULL REFERENCES contracts(id),
    issuing_bank          VARCHAR(200)  NOT NULL,
    advising_bank         VARCHAR(200),
    amount                NUMERIC(18,2) NOT NULL,
    currency              VARCHAR(3)    NOT NULL,
    tolerance_pct         NUMERIC(5,2)  NOT NULL DEFAULT 0,
    issue_date            DATE          NOT NULL,
    expiry_date           DATE          NOT NULL,
    latest_shipment_date  DATE          NOT NULL,
    presentation_days     INT           NOT NULL DEFAULT 21,
    partial_shipment      BOOLEAN       NOT NULL DEFAULT false,
    transshipment         BOOLEAN       NOT NULL DEFAULT false,
    required_documents    JSONB,                  -- 决定单证制作要求
    status                VARCHAR(32)   NOT NULL DEFAULT 'DRAFT'
                          CHECK (status IN ('DRAFT','ISSUED','AMENDED','PRESENTED','ACCEPTED',
                                            'DISCREPANT','SETTLED','EXPIRED','CANCELLED')),
    examination_result    TEXT,
    discrepancies         JSONB,                  -- 不符点
    file_path             VARCHAR(500),
    created_at            TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_by            BIGINT        NOT NULL
);
CREATE INDEX lc_contract_idx ON letters_of_credit (contract_id);

CREATE TABLE lc_amendments (
    id            BIGSERIAL PRIMARY KEY,
    lc_id         BIGINT       NOT NULL REFERENCES letters_of_credit(id) ON DELETE CASCADE,
    amendment_no  INT          NOT NULL,
    content       TEXT         NOT NULL,
    amended_at    DATE         NOT NULL,
    file_path     VARCHAR(500),
    -- 不符点改单需独立审批（业务确认）：approval biz_type = LC_AMENDMENT
    status        VARCHAR(32)  NOT NULL DEFAULT 'PENDING_APPROVAL'
                  CHECK (status IN ('PENDING_APPROVAL','APPROVED','REJECTED','ACCEPTED')),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (lc_id, amendment_no)
);
```

> **不变量**：只有合同付款方式为 L/C 才允许创建信用证；创建时校验 `amount * (1 + tolerance_pct/100) >= contract.total_amount`。

#### 5.6.5 单证制作

```sql
CREATE TABLE documents (
    id               BIGSERIAL PRIMARY KEY,
    doc_no           VARCHAR(50)  NOT NULL UNIQUE,
    contract_id      BIGINT       NOT NULL REFERENCES contracts(id),
    shipment_plan_id BIGINT REFERENCES shipment_plans(id),
    doc_type         VARCHAR(50)  NOT NULL
                     CHECK (doc_type IN ('COMMERCIAL_INVOICE','PACKING_LIST','CUSTOMS_DECLARATION',
                                         'BILL_OF_LADING','CERTIFICATE_OF_ORIGIN','INSURANCE_POLICY','OTHER')),
    version          INT          NOT NULL DEFAULT 1,
    owner_id         BIGINT       NOT NULL,       -- 负责人
    due_date         DATE,
    status           VARCHAR(32)  NOT NULL DEFAULT 'PENDING'
                     CHECK (status IN ('PENDING','DRAFTING','SUBMITTED','REVIEWING','APPROVED','REJECTED')),
    content          JSONB,                       -- 结构化单证内容
    file_path        VARCHAR(500),
    reviewed_by      BIGINT,
    reviewed_at      TIMESTAMPTZ,
    review_comment   TEXT,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by       BIGINT       NOT NULL,
    UNIQUE (contract_id, shipment_plan_id, doc_type, version)
);
CREATE INDEX documents_plan_idx ON documents (shipment_plan_id, status);
```

#### 5.6.6 退税

```sql
-- 按月汇总申报（业务确认）：一个申报批次归集当月多笔出口退税
CREATE TABLE tax_refund_declarations (
    id          BIGSERIAL PRIMARY KEY,
    declare_no  VARCHAR(50)   NOT NULL UNIQUE,
    period      CHAR(7)       NOT NULL,            -- 2026-07
    total_amount NUMERIC(18,2) NOT NULL DEFAULT 0,
    status      VARCHAR(32)   NOT NULL DEFAULT 'PREPARING'
                CHECK (status IN ('PREPARING','DECLARED','REVIEWING','COMPLETED')),
    declared_at DATE,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_by  BIGINT        NOT NULL,
    UNIQUE (period)
);

CREATE TABLE tax_refunds (
    id                 BIGSERIAL PRIMARY KEY,
    refund_no          VARCHAR(50)   NOT NULL UNIQUE,
    contract_id        BIGINT        NOT NULL REFERENCES contracts(id),
    shipment_plan_id   BIGINT REFERENCES shipment_plans(id),
    declaration_id     BIGINT REFERENCES tax_refund_declarations(id),   -- 归属的月度申报批次
    customs_doc_no     VARCHAR(100),
    export_amount      NUMERIC(18,2) NOT NULL,
    currency           VARCHAR(3)    NOT NULL,
    expected_amount    NUMERIC(18,2) NOT NULL,    -- 预计退税额
    actual_amount      NUMERIC(18,2),             -- 实际到账
    status             VARCHAR(32)   NOT NULL DEFAULT 'PENDING'
                       CHECK (status IN ('PENDING','COLLECTING','DECLARED','REVIEWING',
                                         'APPROVED','REJECTED','RECEIVED')),
    declared_at        DATE,
    approved_at        DATE,
    received_at        DATE,
    reject_reason      TEXT,
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE tax_refund_documents (
    id            BIGSERIAL PRIMARY KEY,
    tax_refund_id BIGINT       NOT NULL REFERENCES tax_refunds(id) ON DELETE CASCADE,
    doc_type      VARCHAR(50)  NOT NULL,
    document_id   BIGINT REFERENCES documents(id),
    file_path     VARCHAR(500),
    collected_at  TIMESTAMPTZ
);
```

#### 5.6.7 收汇

```sql
CREATE TABLE receivable_plans (
    id             BIGSERIAL PRIMARY KEY,
    contract_id    BIGINT        NOT NULL REFERENCES contracts(id),
    seq            INT           NOT NULL,
    plan_type      VARCHAR(32)   NOT NULL CHECK (plan_type IN ('DEPOSIT','BALANCE','INSTALLMENT')),
    due_date       DATE          NOT NULL,
    planned_amount NUMERIC(18,2) NOT NULL,
    currency       VARCHAR(3)    NOT NULL,
    received_amount NUMERIC(18,2) NOT NULL DEFAULT 0,
    status         VARCHAR(32)   NOT NULL DEFAULT 'PENDING'
                   CHECK (status IN ('PENDING','PARTIAL','SETTLED','OVERDUE')),
    UNIQUE (contract_id, seq)
);

-- 账目表：append-only，不允许 DELETE，冲销以反向记录实现
CREATE TABLE receipts (
    id                 BIGSERIAL PRIMARY KEY,
    receipt_no         VARCHAR(50)   NOT NULL UNIQUE,
    contract_id        BIGINT        NOT NULL REFERENCES contracts(id),
    plan_id            BIGINT REFERENCES receivable_plans(id),
    lc_id              BIGINT REFERENCES letters_of_credit(id),
    amount             NUMERIC(18,2) NOT NULL,    -- 冲销记录为负数
    currency           VARCHAR(3)    NOT NULL,
    received_at        DATE          NOT NULL,
    -- 实际到账汇率快照
    fx_rate            NUMERIC(18,8) NOT NULL,
    fx_rate_at         TIMESTAMPTZ   NOT NULL,
    fx_source          VARCHAR(50)   NOT NULL,
    base_amount        NUMERIC(18,2) NOT NULL,    -- 折本币金额
    bank_name          VARCHAR(200),
    bank_slip_path     VARCHAR(500),
    entry_type         VARCHAR(32)   NOT NULL DEFAULT 'NORMAL'
                       CHECK (entry_type IN ('NORMAL','REVERSAL')),
    reverses_id        BIGINT REFERENCES receipts(id),  -- 冲销指向被冲销的记录
    status             VARCHAR(32)   NOT NULL DEFAULT 'CONFIRMED'
                       CHECK (status IN ('DRAFT','CONFIRMED','REVERSED')),
    remark             TEXT,
    created_at         TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_by         BIGINT        NOT NULL
);
CREATE INDEX receipts_contract_idx ON receipts (contract_id, received_at);
```

> **不变量**：数据库层 `REVOKE DELETE ON receipts FROM erp_export;`。文档：「支持冲销或作废，但不能直接删除已确认记录。」

#### 5.6.8 出口业务的 outbox

```sql
CREATE TABLE outbox_events (...);  -- 见 4.2 节统一结构
```

### 5.6.9 两层库存占用（2026-07-28 落地）

出口的交期长，七月签的合同十月才走货。这中间那批货必须**立刻从可售里扣掉**，
否则同一箱货会被卖两次；但它也不该在三个月里被钉死在某个拣货位上。
所以是两层：

| | 什么时候发生 | 语义 | 列 |
|---|---|---|---|
| **预留** | 合同生效 | 卖出去了，还没拣 | `reserved_qty` |
| **锁定** | 出货计划确认 | 已指定批次库位，准备出库 | `locked_qty` |

```sql
available_qty = on_hand - reserved - locked - frozen   -- 生成列
CHECK (reserved_qty + locked_qty + frozen_qty <= on_hand_qty)
```

`available_qty` 是生成列，别处**算不出第二个版本**；那条 CHECK 让超卖在结构上不可能，
而不只是「一般不会」。出货计划确认时是把数量从 `reserved` 挪到 `locked`，
`available` 不动——否则同一批货会被扣两次。

**事件链**

```
export      合同签署 → ContractEffective
inventory   消费 → 占用 → StockAllocated（含每行的 demand / reserved / shortage）
procurement 消费 → 只对 shortage > 0 的行建采购需求
```

procurement **不再监听合同**。谁该买什么，只有库存能回答，
让采购直接听合同就是第一版错误的根源。

`StockAllocated` 即使一件都不缺也照发：「这份合同什么都不用买」也是采购要知道的事实，
一个只在出问题时才收到消息的消费者，分不清「没缺口」和「还没处理」。

**踩到的坑：库存不能只按 SKU 索引**

第一版 `stocks` 唯一键是 `(tenant, warehouse, sku_id)`，占用也按 `sku_id` 查。
但**没有变体的产品根本没有 SKU 行**，合同明细里 `sku_id` 是 NULL、到这里是 0。
结果是：仓库堆满货，合同却匹配不到自己的库存，报出缺口去采购已经在货架上的东西。

改成 `(tenant, warehouse, product_id, sku_id)`，0 表示「该产品无变体」，
跟 export 在事件里 coalesce 掉 null 的约定一致。入库校验也一并放开——
原来强制要 SKU，等于让半个产品目录无法入库。

**实测（2026-07-28）**

```
库存 2000，签 CT-202607-0012（50 件）
  → 预留 50，可用 2000→1950，采购需求 0 条          ← 旧实现会错误地买 50
     日志：short_lines=0, covered_by_stock=1

库存 12，签 CT-202607-0013（20 件）
  → 预留 12，可用 0，缺口 8
  → 采购需求 1 条：8 件                              ← 只买差额，不是 20
```

### 5.6.10 采购与合同是分开的（2026-07-28）

合同缺口会**自动**产生采购需求，但采购不只有这一条来源。
买手随时可以自己发起一条——旺季备货、长交期物料、供应商给了好价钱。
一个只能买「已经卖掉的东西」的公司，根本没法维持库存。

| | 合同缺口 | 自行发起 |
|---|---|---|
| 谁触发 | 库存占用后算出的缺口 | 人 |
| 看库存吗 | **看**，只买差额 | **不看** |
| `source` | `CONTRACT` | `MANUAL` |
| 关联合同 | 有，用于追溯和倒排交期 | 无 |

自行发起那条**故意不查库存**。合同缺口要跟在手库存相减，因为它的目的就是补缺；
而人决定要买什么是另一回事，用可用量去否决它，恰好否决掉的就是「补库存」本身。

**缺口被补上之后要关掉需求**

重放历史数据时发现的：某行原来缺 8 件、建了需求，后来库存补足、缺口变成 0，
那条需求却还挂在待采购里。买手照着它会去买已经到货的东西。

现在 `shortage <= 0` 时会把该行**还没下过单**的需求置为 `CANCELLED`，
理由写「库存已可满足，无需采购」。不删除——昨天看到它的人有权知道它去哪了；
只关未动过的——已经下单的是对供应商的真实承诺，货照样会到。

### 5.6.11 出库：缺口没补上就出不了库（2026-07-29 落地）

这是「提示需要采购」真正有约束力的地方。否则缺口只是一句建议，
仓库照样可以把不存在的货发出去。

**一条明细能出多少**

```
可出 = reserved − locked − shipped
```

`reserved` 是「库存替这条明细扣下来的量」，**转锁定和出库时都不减**。
减了的话 `shortage = demand − reserved` 会重新变大，
一条已经发完的明细会看起来又缺货，采购去买已经出了门的东西。
另外记 `locked_qty` / `shipped_qty` 两个计数器，
约束 `locked + shipped <= reserved` 由 CHECK 保证。

**三个动作**

| | 库存变化 | 可用量 |
|---|---|---|
| 生成出库单 | `reserved -= q; locked += q` | **不变** |
| 确认出库 | `on_hand -= q; locked -= q` | **不变** |
| 取消出库单 | `locked -= q; reserved += q` | **不变** |

可用量三次都不动，这是对的：货在合同生效那天就已经不可售了。
`available_qty` 是生成列，这一点结构上算不错。

草稿才能取消。货出了门再撤是退货，是另一套单据。

**拒绝的时候要说清缺多少**

```
库存不足，无法出库：全棉抗菌毛巾 70x140 需出 300，可出 220，缺 80，请先完成采购入库
```

数字同时放进 message 和 error meta。这是整条链上唯一一个
「缺多少」就是全部内容的错误，它得在只显示一行 toast 的地方也读得通。
（gateway 原来会把 apierr 的 meta 丢掉，一并修了。）

**到货要自动归属给在等的合同**

不做这个的话模型是在撒谎。「合同生效即占用库存」只有在
**后到的货也被占用**时才成立；否则专门为补缺口买回来的货
会挂着「可用」，被后面签的合同拿走，掏钱下单的那份合同还是缺。

入库时按 **交期最早优先** 分配给缺货的明细——不是按签约顺序，
早签晚交的合同不该插队。分完重新发一次 `StockAllocated`，
procurement 那边缺口归零就关需求、缺口变小就改数量。

```
到货 1000（另有闲置 720）
  → CT-202607-0004 交期 2026-10-15  缺 1500 → 全部拿到
  → CT-202607-0004 交期 2026-10-15  缺  300 → 拿到 220，还缺 80
  → CT-202607-0005 交期 2026-11-20  一件没拿到    ← 交期晚，排后面
  采购需求：1500 那条关掉，300 那条改成 80
```

出库时还有一次兜底补预留（合同生效前就有的货、重放漏掉的），
补的是**整条明细的缺口**而不是本次出库的差额——
只补够这一次的话，剩下的会留在「可用」里被别的合同拿走。

**顺带修掉的两个真问题**

1. **合同改版后旧版预留没释放**。CT-202607-0004 改到 v2 之后，
   v1 还占着 720 件，而且出库页会拿 v1 的明细让仓库发货——
   发一份已经不存在的合同。现在新版本生效时释放旧版本预留，
   释放出来的货立刻走一遍「分给在等的合同」。
   **已锁定或已出库的不动**，留给人处理（与 §重签期间的存量业务 一致）。

2. **需求一旦被关就再也活不过来**。`UpsertRequirement` 冲突时不改 status，
   所以被 SUPERSEDED / CANCELLED 的行即使缺口又出现也永远是关的，
   没人去买。现在**没下过单的**自动复活成 PENDING；
   已经下单的不动——那是对供应商的真实承诺，复活会导致重复采购。

**单号**

出库单号在**校验全部通过之后**才去 masterdata 取。
提前取的话每次缺货被拒都烧掉一个号，而被拒是这里的常态，
单号会一跳一大截，看起来像丢了单据。

**实测（2026-07-29）**

```
缺货合同强行出库  → IV_STOCK_SHORT「需出 300，可出 220，缺 80」
连拒 3 次后再成功 → OUT-20260729-0003 → 0004，单号不跳
生成出库单 4 件   → 在库 2000 预留 1996 锁定 4 可用 0     ← 可用不变
确认出库 4 件     → 在库 1996 预留 1990 锁定 6 可用 0
取消出库单 6 件   → 在库 1996 预留 1996 锁定 0 可用 0     ← 退回预留
到货 1000         → 交期最早的合同吃满，采购需求 1500→关，300→80
出满后            → 该明细 status = SHIPPED，自动从待出库列表消失
```

### 5.6.12 出库回写合同：发货进度（2026-07-29）

`StockOutbound` 事件之前**没有任何消费者**。仓库可以把一份合同发得干干净净,
而卖这单的销售在系统里看不到任何痕迹——只能打电话问仓库货走了没有。
inventory 知道,export 不知道,同一笔生意的两半从来没见过面。

现在 export 消费 inventory 的 topic,把发货记到 `contract_shipments`。

**为什么不是 contract_items 加一个 shipped_qty 列**

已批准的合同版本被触发器**冻结**,因为它记录的是「双方谈定了什么」。
而发了多少货是「世界上发生了什么」,每走一趟车就变一次。
把一个会动的数放进冻结的文档里,要么破坏冻结,要么让冻结长出例外——
**一条有例外的不可变规则就不再是不可变规则了**。所以它在旁边,不在里面。

**按产品统计,不按明细行**

这是重放历史数据时发现的:CT-202607-0004 已经是 v4 了,
而那 1800 件是当初按 **v2 的明细行 id** 发出去的。按行统计的话
v4 的新行 id 匹配不上,页面会显示「一件没发」——货明明已经在海上了。

客户买的是**产品**;改版改的是条款,不是箱子里的东西。所以按 `product_id + sku_id` 汇总。

**待发货可以是负数,而且这是最有价值的输出**

```
CT-202607-0004 v4
  全棉抗菌毛巾  已售 1500  已发 1800  待发 -300  ⚠ 超发 300
```

意思是**合同在发货之后被改小了**。这正是 §重签期间的存量业务 说要
「标记冲突并生成风险预警,由人工处理」的情况——在此之前系统完全看不到它。
不 clamp 到 0,因为 clamp 掉的恰好是唯一需要人管的那件事。

**加消费者会自动补全历史**

这个消费者是新加的,消费组从头读,一启动就把之前所有的出库单全补上了:

```
shipment recorded  CT-202607-0006  OUT-20260729-0006  lines=1
shipment recorded  CT-202607-0004  OUT-20260729-0005  lines=2
shipment recorded  CT-202607-0004  OUT-20260729-0007  lines=1
```

这是 outbox + Kafka 这套结构真正的回报:几个月后新增一个读者,历史自动重放,
不需要写一次性补数脚本。

### 5.7.3 采购需求的操作（2026-07-29 补全）

原来只有「关闭」一个动作,理由是需求是**纯派生数据**——数量由
`合同数量 − 已预留` 算出,人改了下一个 `StockAllocated` 就覆盖回去,
所以只留了唯一一个不会被算法覆盖的人的判断:「这批我们不买」。

**采购单做完之后这个理由就不成立了**,而最该有的动作恰恰缺着。补上:

| 动作 | 为什么 |
|---|---|
| **去下单** | 站在需求上直接下单。以前要记住产品名跑到采购单页从两百条里翻 |
| **详情** | 看这条需求被哪些采购单覆盖了。同一行「没人买」和「买了还没到」长得一模一样,处理方式完全相反 |
| **重新打开** | 关错了要能撤。只允许**没下过单**的——已下单的是对供应商的真实承诺 |

下单动作跳到采购单页并带上选中的行(`?requirements=1,2,3`),
而不是在需求页再做一个一样的对话框——两个会漂移的对话框比一次跳转糟得多。

### 5.6.13 库存计价：移动加权平均（2026-07-29 落地）

补的是路线图第 1 步。在此之前采购单上写着 12.50，收货时价格就丢了，
「刚出库那批货成本是多少」这个财务必答的问题**答不出来**。

**为什么是移动加权平均**

它是唯一不需要月末机器就正确的方法：每一次移动之后均价就是对的。
标准成本 + 差异科目是另一个常见选择，但它需要成本计算运行和差异过账——
那属于总账，不该排在总账前面。

```sql
avg_cost = CASE WHEN on_hand_qty > 0 THEN total_cost / on_hand_qty ELSE 0 END  -- 生成列
```

生成列，所以**没有第二个地方能算出不同的答案**，和 `available_qty` 同样的理由。

**每一笔移动都记成本**

`stock_ledger` 加了 `unit_cost` / `amount` / `avg_cost_after`。
只有入库和出库动值；预留和锁定改变的是「谁能拿」，不是「值多少」，
那两类记 0 但仍带上当时的均价，否则流水读起来像是均价掉到了 0。

**出库价必须是「之前」的均价**

```sql
WITH before AS (SELECT id, avg_cost FROM stocks WHERE ... FOR UPDATE)
UPDATE stocks s SET
    on_hand_qty = s.on_hand_qty - q,
    total_cost  = greatest(s.total_cost - q * b.avg_cost, 0)
FROM before b WHERE s.id = b.id
RETURNING b.avg_cost AS unit_cost, (q * b.avg_cost) AS amount;
```

更新后再读均价，等于用「这次出库产生的均价」给这次出库定价——循环，
而且错的幅度恰好就是要算的那个量。`greatest(..., 0)` 防的是最后一件出库时
几个千分位的累积舍入让成本池变负，从而让一次完全正确的出库失败。

**不填单价的入库按当前均价入账**

盘盈、库存更正没有采购价。按 0 入账的话，收 10000 件「零成本」会把
货架上所有东西的均价腰斩。所以留空 = 按现有均价，**对估值零影响**。

**多币种是明确的已知边界，不是遗漏**

一个成本池只能有一种币。外币采购要按收货日汇率折本位币，需要 fx 服务，
而 inventory 还没有这个依赖。所以币种不符时**大声拒绝或降级到当前均价并
记 ERROR 日志**，而不是混进去算出一个没有意义的数。

**实测（2026-07-29）**

```
入库 1000 @ 12.00  → 成本池 12,000   均价 0.6597   ← 被历史零成本库存稀释
入库 1000 @ 18.00  → 成本池 30,000   均价 1.5633
入库  500 不填单价  → 成本池 30,781.66  均价 1.5633   ← 估值零影响
出库 1200          → 成本池 28,905.68  出库成本 1,875.98 CNY = 1200 × 1.5633
```

均价看起来偏低，是因为 17190 件历史库存的成本是 0——它们早于这个功能。
真实上线需要一次**期初建账**把存货估值导进来。

### 5.6.14 出运单：哪批货上了哪条船（2026-07-29 落地）

业务确认：**货从供应商直接拉到船上，中间没有仓库**。所以出运是**人工录入**的
主路径，§5.6.12 的出库回写作为第二个来源保留——两条路都写进
`contract_shipments`，发货进度只读这一张台账。

#### 合同挂在明细行上，不挂表头

**一条船可以拼多个合同**（同一客户的几单，或几个客户同拼一柜），这是常态不是例外。
表头上放 `contract_id` 会逼着一个拼柜拆成几张根本不存在的出运单。

```sql
CREATE TABLE shipments (          -- 表头：关于船的一切
    shipment_no, vessel_name, voyage_no, bl_no,
    container_no      VARCHAR(500),   -- 自由文本：一次订舱常含多个柜，
                                      -- 货代就是一行发过来的
    port_of_discharge, etd DATE, eta DATE,
    status  CHECK (status IN ('DRAFT','SHIPPED','ARRIVED','CANCELLED')),
    created_by, created_by_name, ...
);
CREATE TABLE shipment_items (     -- 明细：关于货的一切，每行自带合同
    shipment_id, line_no,
    contract_id, contract_no, customer_name,       -- 快照
    contract_item_id,                              -- 无外键：版本可能已被取代
    product_id, sku_id, product_name, spec, qty, uom_code,
    UNIQUE (tenant_id, shipment_id, contract_item_id)
);
```

#### 草稿不算发货，确认开船才算

```
DRAFT ──确认开船──▶ SHIPPED ──▶ ARRIVED
  │                    │
  └──────作废──────────┴──▶ CANCELLED（已开船的作废会把货从台账里扣回去）
```

- **只有 `ConfirmSailing` 写 `contract_shipments`**，`outbound_no` 用出运单号。
  在那之前草稿写什么都不影响任何合同。
- 开船后不能改。货代和报关行手上拿的是那份单据，事后改这边只会让两边对不上。
  要更正就作废重开。
- 作废保留单据（标 `CANCELLED`），只删台账行：**台账是派生数据，单据才是记录**。
  删掉单据等于抹掉「有人录错过」这件事。
- **到港不动数量**：货是开船那天走的，到港改变的是位置，不是走没走。

#### 超发在确认时拦，不在草稿时拦

按**产品**分组比对，与 `ShipmentProgressOf` 用同一口径——合同改版会重写明细行
id，按行比对会让同一批货跨版本发两次。

```
EX_SHIP_EXCEEDS_CONTRACT
  contract_no CT-202607-0027 · product 全棉抗菌毛巾 · remaining 800 · requested 900 · over 100
```

**为什么是确认时而不是草稿时**：两张草稿各自看都不超，合起来就超了，只有其中
一张能是「走过头的那张」。草稿阶段没有唯一正确答案，确认阶段有。前端在挑货表里
直接显示未发数量，操作的人边填边看得见。

> **前端必须按产品分组挑货。** 一张合同可以把同一产品列两行（改价、加行、
> 换备注）。按行显示会把该产品的全部未发量显示在每一行上——未发 700 出现两次，
> 「全部装满」就会提出装 1400。这个 bug 在 CT-202607-0008 上真实出现过。

#### 可见性由货决定，不由录单人决定

拼柜里装着几个客户的货，「这个人能不能看这张出运单」只能是
**「这船上有没有一个他能看的合同」**——表头上根本没有一个负责人可比。
`ListShipments` 用 `EXISTS (shipment_items JOIN contracts WHERE sales_employee_id = ANY(...))`，
外加「自己建的草稿自己能看见」，否则刚存盘就从列表里消失了。

权限单独一对 `export:shipment:read/write`，不搭合同的车：货代台天天打提单，
不能改价格；销售要看货在哪条船上，不能宣布开船。

#### 验证（2026-07-29，真实数据）

```
拼柜 SH-0001：CT-0027 装 800 + CT-0009 装 300      两个客户一条船
超发拦截：CT-0027 只剩 800 却装 900               → EX_SHIP_EXCEEDS_CONTRACT over 100
跨单累计：CT-0009 已发 300，第二条船再装 201       → 拦下，remaining 200
开船后改单                                       → EX_SHIPMENT_NOT_DRAFT
作废已开船的 SH-0002                             → 进度从 500 退回 300
到港后作废                                       → EX_SHIPMENT_ARRIVED
被拒的建单不烧号                                  → 连拒 3 次后仍是 SH-0003
销售（SELF 范围，非本人合同）看列表                → total 0；开船 → AUTH_PERMISSION_DENIED
```

### 5.6.15 收款对账（2026-07-30 落地）

业务确认：**大额走 T/T，T/T 不带我们的 reference，所以归属必然是财务人工选**。
自动匹配是附加能力，人工匹配是主路径——这决定了整个模块的形状。

#### 两张表，分界线是「谁说了算」

```sql
bank_transactions     -- 银行说的话。事实，除处置状态外永不编辑
    bank_ref          -- 银行流水号，幂等的钥匙（UNIQUE per account）
    amount, currency, value_date, counterparty, remittance_info
    source            -- MANUAL / CSV / API / PSP
    trusted_ref       -- 我们自己下发的 reference（付款链接），与客户手打的分开存
    disposition       -- UNPROCESSED / ALLOCATED / IRRELEVANT
    irrelevant_type   -- 退税 / 利息 / 内部划转 / 供应商退款 / 保证金 / 其他

receipt_allocations   -- 人做的判断。append-only
    transaction_id, contract_id, contract_no, customer_name（快照）
    amount            -- 从这笔流水分走多少；冲销行为负
    fee_amount        -- 我方吸收的手续费差额，不占流水
    reversal_of       -- 冲销行指向被冲销的那行；partial unique 保证只能冲一次
```

银行流水是事实，核销是判断。判断会错、会改、会被审计追问；事实不能动。
混在一张表里，一次改错就再也说不清「银行到底给了多少」。

#### 两个恒等式

```
流水金额 = Σ 分配金额 + 未分配余额
合同已收 = Σ (分配金额 + 手续费)
```

`fee_amount` 不参与第一式。客户汇 3260、中间行扣 25、到账 3235 时，
分配 3235 + 手续费 25，流水正好分完，合同也正好收清——**两边同时平**。

#### 全部导入，不在入口筛

退税款、银行利息、结汇入账、供应商退款、付出去的款……全部进 `bank_transactions`。
导入时过滤掉「看起来不像客户打款的」，就永远没法解释银行余额和系统的差异。

因此必须有 **`IRRELEVANT` 这个出口并强制选类别**。没有它，这些流水会永远躺在
待处理队列里，队列越来越长，财务就不看了——然后整个页面失去意义。

`INTERNAL`（内部划转）是最容易出事的一类：外币户结汇到人民币户，人民币户上
就是一笔大额收入，长得和客户货款一模一样。`bank_accounts` 存本公司账户清单，
就是为了认出它。

#### 自动只做预填，不做核销

| reference 来源 | 可信度 | 处理 |
|---|---|---|
| `trusted_ref`（付款链接下发） | 高——我们自己生成、与金额一起下发 | 可自动核销（PSP 通道未实现，位置留好） |
| `remittance_info`（客户手打） | 低——常是上一单的号，或号对金额不对 | **只预填建议，必须人确认** |

正则匹配单号的形状（`[A-Z]{2,6}-[0-9]{4,8}-[0-9]{2,6}`）而非固定前缀，因为编号规则
是配置；查不到的自然不返回，误报零成本。大小写不敏感。

#### 四条规则

1. **冲销，不删除。** 改错了插一条负数行指向原行，`reversal_of` 上的 partial
   unique index 保证同一条只能冲一次——两次点击不会让合同余额多退一遍。
2. **只允许同币种核销。** 跨币种产生汇兑损益，而在总账建成之前没有地方安置它。
   拒绝是诚实的；猜一个汇率等于凭空造钱。
3. **允许留未分配。** 财务可以先处理确定的部分。只有未分配 = 0 才转 `ALLOCATED`，
   否则留在队列里——剩下的仍是别人的钱。
4. **已核销的流水不能标记为无关**，要先冲销。

#### 前端两个坑（都实际踩到了）

- **付出去的款不能核销**：列表上给 DEBIT 行显示「去匹配」等于设陷阱，
  服务端必拒。改成「归类」，且对话框里不渲染分配表单。
- **Vue 响应式**：`draft.value.push(row)` 之后再改 `row`，改的是**裸对象**，
  Vue 收不到通知——值设了但界面不刷新。必须 push 之后从数组里取回代理再改。

#### 验证（2026-07-30，真实数据）

```
拆分核销   一笔 7395 分给 CT-0027(4160) + CT-0008(3235+手续费25)  → 两张合同同时收清
手续费容差 3235 + 25 = 3260 = 合同额                              → 收清，不留 25 的尾巴
超额分配   7395 的流水想分 7420                                   → EX_ALLOC_EXCEEDS_PAYMENT
币种不符   USD 流水核销 EUR 合同                                  → EX_ALLOC_CURRENCY_MISMATCH
重复流水号                                                        → EX_TX_REF_TAKEN
冲销无理由                                                        → EX_REVERSE_REASON_REQUIRED
冲销       退回未分配 3235，合同 3260 → 0，流水回 UNPROCESSED
重复冲销                                                          → EX_ALLOC_ALREADY_REVERSED
已核销标无关                                                      → EX_TX_HAS_ALLOCATIONS
付出去的款核销                                                    → EX_TX_NOT_CREDIT
退税款标记 TAX_REFUND                                             → IRRELEVANT，离开队列
```

#### 未做（接口位置已留）

- **PSP webhook**：付款链接那条自动通道。需先选定服务商并完成商户入网。
  注意 PSP 是**按批次结算**的：银行流水上是一笔合并款，与合同不是一一对应，
  那是第二层账（资金到账），本轮只做第一层（收款确认）。
- **CSV 导入**：`source` 字段与流水号唯一约束已就位，接上去只是多一个入口，
  下游一行不改。做之前需要一份真实的银行导出文件来做字段映射。
- **涉外收入申报**：境外汇款到账后要在规定时间内申报，单据是出口退税要用的。

### 5.6.16 需求单解析与产品召回（2026-07-30 规划，未实现）

客户把需求单发过来——PDF 或图片，几十行同类不同规格的钢材。人工挑产品会挑错，
所以后端解析并**召回候选**，人做最后的确认。

#### 这是三个问题，工具完全不同

| 步骤 | 做什么 | 用什么 |
|---|---|---|
| ① 文档 → 结构 | 扫描件变成文本和表格 | **专用文档解析**，不是 LLM |
| ② 结构 → 行项 | 拆出品名、规格、数量、单位 | LLM，**只喂文本不喂图** |
| ③ 行项 → 候选 | 对上产品库 | **多路检索 + 规则**，不是 LLM |

把三步合成「把 PDF 丢给 LLM 要 JSON」能做出演示，上线会死在扫描质量、跨页表格、
复合规格和单位不统一上。第 ③ 步尤其不能交给 LLM：产品匹配必须**可解释、可复现、
可调**，LLM 匹配错了无法调试，同样的输入还可能给不同答案。

**服务归属**：product 拥有产品目录和检索索引，对外提供「给我这一行的候选」；
export 拥有需求单文档和审核流程。跨服务一次调用，不复制目录。

#### 真正的瓶颈是产品库，不是模型（先做这一步）

现在规格存在 `skus.spec VARCHAR(500)` 里，是自由文本；`attributes JSONB` 是空的。
钢材一行需求长这样：

```
Q235B 冷轧板卷 3.0*1250*C 酸洗 20吨
```

库里也是自由文本的话，客户写 `3.0*1250`、我们写 `3.0x1250`、或者写
`厚3.0 宽1250`，就对不上——**换任何模型都救不了**。把 `attributes` 结构化之后：

```json
{"material":"Q235B","process":"冷轧","thickness_mm":3.0,"width_mm":1250,"surface":"酸洗"}
```

匹配变成「材质精确 + 厚度在公差内 + 宽度精确」的一句 SQL，
准确率从「大概能对上」变成「对上就是对的」。

> **因此第一步不是选模型，是定义规格字段结构并补齐库存数据。**
> AI 解析出来的东西，得有个结构化的靶子可以打。

#### 但每个行业的规格都不一样：把属性定义变成数据（2026-07-30 确认）

钢材有厚宽长，日用品有颜色尺码，化工有纯度包装。**不能在代码里写死任何一套**——
加一个新行业就要改 schema，等于每来一个客户做一次迁移。

做法是**每个租户 × 每个品类一张属性模板**，模板本身是数据：

```sql
attribute_templates          -- 租户 × 品类
    tenant_id, category_id

attribute_defs
    template_id, key, label
    data_type        -- DIMENSION / NUMBER / ENUM / TEXT / RANGE
    unit             -- mm / kg / ℃
    enum_values      -- 材质：Q235B / SPCC / SUS304
    match_strategy   -- EXACT / TOLERANCE / SYNONYM / FUZZY / OVERLAP
    tolerance_pct    -- 厚度允许 ±3%
    is_matchable, is_required, sort_order
```

`skus.attributes` JSONB 按模板存值，GIN 索引承担查询，类型与单位在写入时对模板校验。
这是 PIM（产品信息管理）系统的标准结构，不是自创。

```
钢材租户   → 材质(ENUM) 厚度(DIMENSION ±3%) 宽度(DIMENSION 精确) 表面(ENUM)
日用品租户 → 颜色(ENUM) 尺码(ENUM) 材质(ENUM) 克重(NUMBER)
化工租户   → 纯度(RANGE) 包装规格(DIMENSION) CAS号(EXACT)
```

加一个行业是加一条模板数据，**召回引擎一行代码不改**。

##### 匹配策略也是数据

模板不只说「有哪些字段」，还说**每个字段怎么匹配**，召回引擎读模板生成查询：

| data_type | 例子 | 匹配方式 |
|---|---|---|
| `DIMENSION` | 厚度 3.0mm | 数值 + 公差区间 |
| `ENUM` | 材质 Q235B | 精确 + 同义词表 |
| `RANGE` | 纯度 ≥99.5% | 区间重叠 |
| `TEXT` | 表面处理 | 模糊 + 向量 |

##### 三层降级：不是达标才能用

> **系统必须在三个层次都能工作。** 要求补齐数据才能上线，这功能就永远上不了线——
> 没人会为一个还没用过的功能先补几千条产品数据。

| 层 | 状态 | 匹配靠什么 | 效果 |
|---|---|---|---|
| 0 | 只有自由文本 `spec` | 全文 + 向量 | 召回还行，精度差 |
| 1 | 模板定了、数据没填全 | 填了的精确匹配，没填的退回文本 | 逐步变好 |
| 2 | 模板完整 + 数据补齐 | 结构化精确匹配 | 最准 |

新租户第一天在第 0 层可用，随着补数据自己爬到第 2 层。

##### 模板从哪来

1. **预置行业模板库**（钢材 / 纺织 / 五金 / 化工…），新租户选一个开箱即用
2. **从存量数据反推**——把现有 `spec` 自由文本喂 LLM，让它提议属性字段与取值范围，
   人审一遍确认。**这才是 LLM 在本功能里最合适的位置**：一次性、离线、有人审的
   schema 发现，比拿它做逐行匹配可靠得多
3. 租户自己在界面上定义

##### 一个硬限制：向量检索对数字规格是瞎的

`3.0*1250` 与 `3.5*1250` 的 embedding 几乎相同，商业上却是两个东西。
**规格即商品**的品类（钢材、五金、化工）必须有数值字段，不能靠语义检索糊弄；
日用品（`红色 XL 纯棉T恤`）反而语义检索效果好，因为区分维度本来就是词。
不同品类需要不同的匹配策略——模板系统正是让这件事可以按品类表达。

#### 召回优先：宁可多给，不可漏（2026-07-30 业务确认）

这条改变设计，不只是调个阈值：

1. **多路召回取并集**，不是单一策略取 top-N。并集保召回，排序保可用：

   | 路 | 命中什么 |
   |---|---|
   | 结构化属性 | 材质精确 + 尺寸在公差内 |
   | 全文 / 模糊（`pg_trgm`） | 写法不同但字面接近 |
   | 向量（`pgvector`） | 说法完全不同但语义相近 |
   | 别名表 | 这个客户惯用的叫法 |
   | **历史成交** | 这个客户以前买过什么——信号极强 |

2. **低分候选排后面，不删除。** 界面折叠但可展开，不设硬阈值砍掉尾巴。
3. **候选数量给得宽**（每行 20 条量级），因为漏掉的代价远大于多看几行的代价。

> **召回率必须可度量。** 审核时如果业务员没在候选里选、而是自己搜出来的，
> 这一行记 `picked_from = MANUAL_SEARCH`——**那就是一次召回失败的样本**。
> 没有这个字段，召回率永远只是感觉。这也是别名表的来源：
> 从这些样本里提炼「客户这么叫 → 其实是这个产品」，召回随使用变好。

#### 两条路并存：AI 或人工，员工自己选

- **上传时**可选「AI 解析」或「手工录入」
- **审核时**每一行都能放弃候选、直接搜产品库

不是两套流程，是一套流程 + 可忽略的 AI 预填。两条路进同一个审核界面、
同一张决策表，所以随时可以切换，也能公平地比较两者的效率。

#### 表结构（草案）

```sql
rfq_documents            -- 需求单
    customer_id, customer_name（快照）, file_key, page_count
    mode                 -- AI / MANUAL
    status               -- UPLOADED/PARSING/PARSED/PARSE_FAILED/REVIEWING/CONFIRMED/CANCELLED
    parse_error, parsed_at, confirmed_at

rfq_lines                -- 解析出的行项。机器说的，不可变
    rfq_id, line_no
    raw_text                        -- 原文
    source_page, source_bbox JSONB  -- 定位，审核时高亮原文
    raw_name, raw_spec, raw_qty, raw_uom, raw_remark
    parsed_attrs JSONB              -- 归一化后的结构化规格
    confidence           -- HIGH / MEDIUM / LOW

rfq_line_candidates      -- 召回的候选。机器说的
    rfq_line_id, product_id, sku_id
    product_code, product_name, spec（快照）
    score, recall_source -- ATTR / FULLTEXT / VECTOR / ALIAS / HISTORY
    reason               -- 为什么召回它，审核时要看得见

rfq_line_decisions       -- 人做的判断
    rfq_line_id, product_id, sku_id, qty, uom_id
    decision             -- PICKED / NO_MATCH / SKIPPED
    picked_from          -- CANDIDATE / MANUAL_SEARCH  ← 召回率的度量口
    decided_by, decided_at

product_aliases          -- 客户的叫法 → 我们的产品
    product_id, sku_id, alias, customer_id（可空）
    source               -- MANUAL / LEARNED（从 MANUAL_SEARCH 样本提炼）
```

#### 流程

```
上传 → MinIO，rfq_documents = PARSING
  → outbox → Kafka → 解析 worker
      ① 文档 → 结构化文本/表格
      ② LLM 抽行项 → rfq_lines（记来源页码与坐标）
      ③ 多路召回 → rfq_line_candidates
  → PARSED
人工审核：左原文 右解析，逐行确认 / 换产品 / 自己搜 / 标记"库里没有"
  → CONFIRMED → 生成报价单
```

**必须异步。** 解析要几秒到几十秒，HTTP 请求不能挂着——与邮件投递同一条理由
（§5.12.2.1）：先落库，再交给 worker，进度靠轮询或 SSE。

#### 四条规则

1. **原文必须留痕。** 每行记来源页码与坐标，审核界面左右对照。
   没有这个，人工审核就是在盲信 AI。
2. **置信度分档，不给百分比。** 高=自动选中，中=预选待确认，低=留空。
   给个 73% 让人自己判断等于没给。
3. **解析不可变，决策单独存。** 与收款对账同一条分界：机器说的是一回事，
   人说的是另一回事，人改了要看得出改了什么。
4. **绝不自动通过。** 解析结果不能直接变成报价单，必须过审核。

#### 选型与成本

数据出境不是问题——**公司主体在美国，走海外 API 无需额外手续**（2026-07-30 确认）。

- ①：Azure Document Intelligence / Google Document AI（表格强）；
  开源备选 MinerU、Marker、PaddleOCR + PP-Structure
- ②：任何支持 JSON Schema 强约束输出的模型；输入已是结构化文本，**小模型够用**
- ③：`pg_trgm` + `pgvector` 装在现有 Postgres 上，不加组件；
  embedding 用 BGE-m3 / gte-Qwen2 / jina-v3 或商用皆可

一份 5 页需求单：专用 OCR + LLM 抽取约几毛；全程 VLM 看图约几块；
自建 GPU 每月几千起还要运维。**在这个量级上自建不划算**，价格签前复核。

### 5.7 procurement（采购管理）

```sql
-- 由合同生效事件产生的采购需求
CREATE TABLE purchase_requirements (
    id             BIGSERIAL PRIMARY KEY,
    contract_id    BIGINT        NOT NULL,        -- 引用 export.contracts
    contract_no    VARCHAR(50)   NOT NULL,
    contract_item_id BIGINT      NOT NULL,
    product_id     BIGINT        NOT NULL,
    sku_id         BIGINT,
    required_qty   NUMERIC(18,4) NOT NULL,
    ordered_qty    NUMERIC(18,4) NOT NULL DEFAULT 0,
    required_date  DATE          NOT NULL,
    source         VARCHAR(32)   NOT NULL DEFAULT 'CONTRACT'
                   CHECK (source IN ('CONTRACT','STOCK_ALERT','MANUAL')),
    status         VARCHAR(32)   NOT NULL DEFAULT 'PENDING'
                   CHECK (status IN ('PENDING','PARTIALLY_ORDERED','ORDERED','CANCELLED')),
    created_at     TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX pr_contract_idx ON purchase_requirements (contract_id);
```

**实现说明（2026-07-28 落地，与上面的草案有出入）**

实际建表时补了三样草案里没有的东西:

| 列 | 为什么 |
|---|---|
| `contract_version_id` / `version_no` | 合同变更会产生新版本、新行 id。没有这个，需求说不出自己属于哪一版条款 |
| 产品快照（`product_code/name/spec/uom`）+ `customer_name` | 采购不读 export 的表。事件里带什么就存什么，产品后来改名也不影响已经在采购的东西 |
| `closed_reason` | 需求凭空消失是审计一定会问的事 |

状态多了一档 `SUPERSEDED`——被新版本取代，跟人为 `CANCELLED` 是两回事。

**两个方向的幂等**，因为 Kafka 只保证至少一次:

```
同一条事件重投  → 撞 (tenant_id, contract_item_id) 唯一索引 → 刷新快照，不产生第二条
同一合同新版本  → 把旧版本还没动过的需求置 SUPERSEDED
```

第二条只退**没人动过的**（`PENDING`）。已经下过采购单的需求是对供应商的真实承诺,
悄悄作废等于把问题藏起来——那些留着,另外记一条 `WARN` 日志交给人处理。

整段在**一个事务**里:半途而废会让采购员同时看到两个版本的需求。

**实测（2026-07-28）**

服务一起来就把 topic 里积压的 7 条历史事件全消费了:

```
CT-202607-0004 v1 → 2 条需求
CT-202607-0004 v2 → 2 条新需求，v1 那 2 条自动置 SUPERSEDED
  v1: 1200 件 / 交期 2026-09-30   → SUPERSEDED「合同变更，已由新版本的需求取代」
  v2: 1500 件 / 交期 2026-10-15   → PENDING
```

变更路径在**真实历史数据上**自己跑通了，没有专门造数据。

现场再签一份 CT-202607-0008:上传签回件 → 点签署 → **4 秒内**采购侧出现 2 条需求。
链路是 `SignContract → outbox → relay → Kafka → procurement 消费者 → purchase_requirements`。

关闭需求必须填原因（`PR_CANCEL_REASON_REQUIRED`），
重复关闭被 `PR_REQUIREMENT_NOT_PENDING` 挡下。

**界面（`/requirements`）**

页面标题旁边直接写着「由合同生效自动产生，不能手工新建」——
没有「新建」按钮，找不到入口的人不用去猜。

按状态分页签，默认停在**待采购**：买手打开这个页面是来看还欠什么的，
其余是他们特意去翻的历史。列表按 `PENDING → 部分下单 → 其余`、
再按需求日期排，过期的日期标红。

来源合同做成链接直接跳到合同抽屉——买手要花钱之前，
得能一键读到到底卖了什么。

「已被取代」页签是变更留下的痕迹：

```
待采购    CT-202607-0004 v2  1500 PCS  2026-10-15
已被取代  CT-202607-0004 v1  1200 PCS  2026-09-30  合同变更，已由新版本的需求取代
```

> 权限码是登录时缓存在前端的，所以新加的 `procurement:requirement:*`
> **要重新登录才会出现在菜单里**。这一条踩过两次了。

```sql

CREATE TABLE purchase_orders (
    id              BIGSERIAL PRIMARY KEY,
    po_no           VARCHAR(50)   NOT NULL UNIQUE,
    supplier_id     BIGINT        NOT NULL,       -- 引用 masterdata.suppliers
    contract_id     BIGINT,                       -- 可空：备货型采购无关联合同
    currency        VARCHAR(3)    NOT NULL,
    fx_rate         NUMERIC(18,8) NOT NULL,
    fx_rate_at      TIMESTAMPTZ   NOT NULL,
    fx_source       VARCHAR(50)   NOT NULL,
    total_amount    NUMERIC(18,2) NOT NULL DEFAULT 0,
    order_date      DATE          NOT NULL,
    delivery_date   DATE          NOT NULL,
    warehouse_id    BIGINT,
    buyer_id        BIGINT        NOT NULL,
    status          VARCHAR(32)   NOT NULL DEFAULT 'DRAFT'
                    CHECK (status IN ('DRAFT','PENDING_APPROVAL','APPROVED','PARTIALLY_RECEIVED',
                                      'RECEIVED','CLOSED','CANCELLED')),
    remark          TEXT,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_by      BIGINT        NOT NULL
);

CREATE TABLE purchase_order_items (
    id             BIGSERIAL PRIMARY KEY,
    po_id          BIGINT        NOT NULL REFERENCES purchase_orders(id) ON DELETE CASCADE,
    line_no        INT           NOT NULL,
    requirement_id BIGINT REFERENCES purchase_requirements(id),
    product_id     BIGINT        NOT NULL,
    sku_id         BIGINT,
    product_code   VARCHAR(100)  NOT NULL,
    product_name   VARCHAR(200)  NOT NULL,
    qty            NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    uom_id         BIGINT        NOT NULL,
    unit_price     NUMERIC(18,4) NOT NULL,
    amount         NUMERIC(18,2) NOT NULL,
    received_qty   NUMERIC(18,4) NOT NULL DEFAULT 0,
    UNIQUE (po_id, line_no),
    CHECK (received_qty <= qty)
);

CREATE TABLE purchase_receipts (
    id           BIGSERIAL PRIMARY KEY,
    receipt_no   VARCHAR(50) NOT NULL UNIQUE,
    po_id        BIGINT      NOT NULL REFERENCES purchase_orders(id),
    warehouse_id BIGINT      NOT NULL,
    received_at  DATE        NOT NULL,
    operator_id  BIGINT      NOT NULL,
    status       VARCHAR(32) NOT NULL DEFAULT 'DRAFT'
                 CHECK (status IN ('DRAFT','CONFIRMED','CANCELLED')),
    inbound_id   BIGINT,                          -- 引用 inventory.inbounds
    remark       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE purchase_receipt_items (
    id            BIGSERIAL PRIMARY KEY,
    receipt_id    BIGINT        NOT NULL REFERENCES purchase_receipts(id) ON DELETE CASCADE,
    po_item_id    BIGINT        NOT NULL REFERENCES purchase_order_items(id),
    qty           NUMERIC(18,4) NOT NULL,
    qualified_qty NUMERIC(18,4) NOT NULL,
    rejected_qty  NUMERIC(18,4) NOT NULL DEFAULT 0,
    batch_no      VARCHAR(100)
);

CREATE TABLE purchase_payments (
    id           BIGSERIAL PRIMARY KEY,
    payment_no   VARCHAR(50)   NOT NULL UNIQUE,
    po_id        BIGINT        NOT NULL REFERENCES purchase_orders(id),
    supplier_id  BIGINT        NOT NULL,
    amount       NUMERIC(18,2) NOT NULL,
    currency     VARCHAR(3)    NOT NULL,
    fx_rate      NUMERIC(18,8) NOT NULL,
    fx_rate_at   TIMESTAMPTZ   NOT NULL,
    fx_source    VARCHAR(50)   NOT NULL,
    base_amount  NUMERIC(18,2) NOT NULL,
    paid_at      DATE          NOT NULL,
    method       VARCHAR(50)   NOT NULL,
    voucher_path VARCHAR(500),
    entry_type   VARCHAR(32)   NOT NULL DEFAULT 'NORMAL'
                 CHECK (entry_type IN ('NORMAL','REVERSAL')),
    reverses_id  BIGINT REFERENCES purchase_payments(id),
    status       VARCHAR(32)   NOT NULL DEFAULT 'CONFIRMED',
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    created_by   BIGINT        NOT NULL
);
```

### 5.7.1 采购单（2026-07-29 落地）

需求说「要买什么」，采购单说「向谁买、多少钱」。中间那步是**合并**：
三份合同各缺 500，并成一张 1500 的单去压价——这是采购这个岗位一半的价值。
所以采购单挂的是**需求**不是合同，一条需求也可以拆到几张单上。

```
采购需求 ──┐
采购需求 ──┼─→ 采购单（一个供应商）→ 审批 → 已下单 → 收货 → 库存
采购需求 ──┘
```

**状态机**

```
DRAFT → PENDING_APPROVAL → ORDERED → PARTIALLY_RECEIVED → RECEIVED
          ↓ 驳回                ↓ 未到货可取消
        REJECTED            CANCELLED
```

**审批通过才算下单。** 草稿阶段需求的 `ordered_qty` 一动不动——
没人批准花钱之前什么都没承诺，提前标记会让下一个买手以为这批货有人管了，
然后它就悄悄地永远没人买。审批通过的那一刻才写 `ordered_qty`。

驳回或取消会把占用的数量退回 `PENDING`。**已有到货的单不能取消**——
货都进仓库了，撤单是退货，是另一套单据。

**收货：跨库事务用事件桥接**

收货记录写在 procurement 库，库存加在 inventory 库,两个库没法共用事务。
直接调 inventory 的话,调用成功但本地事务回滚,就会出现「有货没单据」。
所以收货和 `PurchaseReceived` 事件在**同一个事务**里落库,inventory 消费后加库存。

这层间接还白捡了一件事:inventory 的入库路径**本来就会**把新货分给在等的合同、
并重新广播缩小后的缺口。所以一次到货会自动关掉它覆盖的采购需求——
**包括别的合同的需求**——而 procurement 完全不知道「预留」这个概念的存在。

**手工需求终于有了关闭路径**

合同缺口的需求靠「库存已可满足」自动关;手工需求(旺季备货)不行——
它背后没有合同,别人的货到多少都满足不了它。它只能靠**自己那张单到货**来关:
`received_qty >= required_qty` → `RECEIVED`。

用库存去判断是错的:「备货 5000」的意思是「我要买 5000」,不是「库存要有 5000」。
买到了就该关,而库存转头可能被合同吃掉,拿库存判断会把它反复关了又开。

**两个守卫**

| 场景 | 拒绝 |
|---|---|
| 下单 6000,需求只剩 5000 | `PO_EXCEEDS_REQUIREMENT`（需求未下单部分 5000） |
| 收货 4000,已收 2000、共订 5000 | `PO_EXCEEDS_ORDERED`（已收 2000 + 本次 4000 > 5000） |

超收不是「多了就多了」——供应商多发货是一次对话,不是一次没人同意付钱的库存增加。

**单号在校验通过之后才取**（和出库单同样的理由）。
实测连续两次超量下单被拒后再成功,单号是 `PO-0002 → PO-0003`,不跳号。

**实测（2026-07-29）**

```
手工需求 5000（旺季备货，此前永远无法关闭）
  → 下单 6000  → PO_EXCEEDS_REQUIREMENT
  → 下单 5000 @12.50 = 62500  → PO-202607-0002 草稿
     此时需求 ordered_qty 仍为 0            ← 草稿不算下单
  → 提交审批 → 通过 → ORDERED
     需求 ordered_qty = 5000, status = ORDERED
  → 收货 2000  → 库存 12190 → 14190, 单据 PARTIALLY_RECEIVED
  → 收货 4000  → PO_EXCEEDS_ORDERED
  → 收货 3000  → 库存 17190, 单据 RECEIVED
     需求 received_qty = 5000, status = RECEIVED   ← 终于关掉了
```

### 5.7.2 采购需求实时推送（2026-07-29）

采购需求会因为**页面上没人操作**的原因变化:库存到货补上了缺口、
合同改版退掉了旧行、采购单审批通过。买手照着几分钟前的列表下单会买错东西。

livefeed 原来只有「发给某个人」的通道(`erp.live.t{租户}.e{员工}`),
但采购需求不属于任何人。加了一条**租户广播**通道 `erp.live.t{租户}.all`,
gateway 的 SSE 订阅同时听两条。

广播给所有人是安全的,因为推送内容只有「去重读」三个字:
重读走的是正常的、带权限校验的 API,没权限的人什么也读不到。
把变化内容本身放进广播就不安全了。

实测:取消采购单后 SSE 立刻收到

```
event: requirement.changed
data: {"type":"requirement.changed","at":"2026-07-29T18:28:16Z"}
```

### 5.8 inventory（仓储管理）

> **单仓库实施**（业务确认）：schema 保留 `warehouse_id`（多仓的结构成本为零），应用层固定使用默认仓库；`stock_transfers` 调拨表结构保留，其 UI 与逻辑推迟到多仓需求出现时实现。届时只加功能，不改表。

```sql
CREATE TABLE warehouses (
    id         BIGSERIAL PRIMARY KEY,
    code       VARCHAR(50)  NOT NULL UNIQUE,
    name       VARCHAR(100) NOT NULL,
    wh_type    VARCHAR(32)  NOT NULL CHECK (wh_type IN ('NORMAL','BONDED','TRANSIT','VIRTUAL')),
    address    TEXT,
    manager_id BIGINT,
    capacity_cbm NUMERIC(18,2),
    status     VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE locations (
    id           BIGSERIAL PRIMARY KEY,
    warehouse_id BIGINT       NOT NULL REFERENCES warehouses(id),
    code         VARCHAR(50)  NOT NULL,
    name         VARCHAR(100),
    loc_type     VARCHAR(32)  NOT NULL DEFAULT 'STORAGE'
                 CHECK (loc_type IN ('STORAGE','PICKING','STAGING','QUARANTINE')),
    capacity_cbm NUMERIC(18,2),
    status       VARCHAR(32)  NOT NULL DEFAULT 'ACTIVE',
    UNIQUE (warehouse_id, code)
);

-- 库存主表。available_qty 为生成列，杜绝手工计算不一致
CREATE TABLE stocks (
    id             BIGSERIAL PRIMARY KEY,
    warehouse_id   BIGINT        NOT NULL REFERENCES warehouses(id),
    location_id    BIGINT REFERENCES locations(id),
    product_id     BIGINT        NOT NULL,
    sku_id         BIGINT        NOT NULL,
    uom_id         BIGINT        NOT NULL,
    on_hand_qty    NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (on_hand_qty >= 0),
    locked_qty     NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (locked_qty >= 0),
    frozen_qty     NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (frozen_qty >= 0),
    in_transit_qty NUMERIC(18,4) NOT NULL DEFAULT 0 CHECK (in_transit_qty >= 0),
    available_qty  NUMERIC(18,4) GENERATED ALWAYS AS (on_hand_qty - locked_qty - frozen_qty) STORED,
    updated_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    UNIQUE (warehouse_id, location_id, sku_id),
    CHECK (locked_qty + frozen_qty <= on_hand_qty)
);
CREATE INDEX stocks_sku_idx ON stocks (sku_id);

-- 库存锁定：lock_ref 是幂等键，重复调用不会重复锁
CREATE TABLE stock_locks (
    id            BIGSERIAL PRIMARY KEY,
    lock_ref_type VARCHAR(50)   NOT NULL,        -- SHIPMENT_PLAN
    lock_ref_id   BIGINT        NOT NULL,
    warehouse_id  BIGINT        NOT NULL,
    sku_id        BIGINT        NOT NULL,
    qty           NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    status        VARCHAR(32)   NOT NULL DEFAULT 'ACTIVE'
                  CHECK (status IN ('ACTIVE','CONSUMED','RELEASED')),
    locked_at     TIMESTAMPTZ   NOT NULL DEFAULT now(),
    released_at   TIMESTAMPTZ,
    UNIQUE (lock_ref_type, lock_ref_id, sku_id)  -- 幂等保证
);

CREATE TABLE inbounds (
    id              BIGSERIAL PRIMARY KEY,
    inbound_no      VARCHAR(50) NOT NULL UNIQUE,
    inbound_type    VARCHAR(32) NOT NULL CHECK (inbound_type IN ('PURCHASE','RETURN','TRANSFER','OTHER')),
    source_ref_type VARCHAR(50),                  -- PURCHASE_RECEIPT
    source_ref_id   BIGINT,
    warehouse_id    BIGINT      NOT NULL REFERENCES warehouses(id),
    status          VARCHAR(32) NOT NULL DEFAULT 'DRAFT'
                    CHECK (status IN ('DRAFT','CONFIRMED','CANCELLED')),
    operator_id     BIGINT      NOT NULL,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE inbound_items (
    id          BIGSERIAL PRIMARY KEY,
    inbound_id  BIGINT        NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
    product_id  BIGINT        NOT NULL,
    sku_id      BIGINT        NOT NULL,
    qty         NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    uom_id      BIGINT        NOT NULL,
    location_id BIGINT REFERENCES locations(id),
    batch_no    VARCHAR(100),
    expiry_date DATE
);

CREATE TABLE outbounds (
    id              BIGSERIAL PRIMARY KEY,
    outbound_no     VARCHAR(50) NOT NULL UNIQUE,
    outbound_type   VARCHAR(32) NOT NULL CHECK (outbound_type IN ('SALES','SAMPLE','TRANSFER','OTHER')),
    source_ref_type VARCHAR(50),                  -- SHIPMENT_PLAN
    source_ref_id   BIGINT,
    warehouse_id    BIGINT      NOT NULL REFERENCES warehouses(id),
    status          VARCHAR(32) NOT NULL DEFAULT 'DRAFT'
                    CHECK (status IN ('DRAFT','PICKING','CONFIRMED','CANCELLED')),
    operator_id     BIGINT      NOT NULL,
    completed_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE outbound_items (
    id           BIGSERIAL PRIMARY KEY,
    outbound_id  BIGINT        NOT NULL REFERENCES outbounds(id) ON DELETE CASCADE,
    product_id   BIGINT        NOT NULL,
    sku_id       BIGINT        NOT NULL,
    qty          NUMERIC(18,4) NOT NULL CHECK (qty > 0),
    uom_id       BIGINT        NOT NULL,
    location_id  BIGINT REFERENCES locations(id),
    batch_no     VARCHAR(100)
);

-- 库存流水：append-only，任何库存变动都留痕
CREATE TABLE stock_movements (
    id           BIGSERIAL PRIMARY KEY,
    movement_no  VARCHAR(50)   NOT NULL UNIQUE,
    move_type    VARCHAR(32)   NOT NULL
                 CHECK (move_type IN ('INBOUND','OUTBOUND','TRANSFER','ADJUST','COUNT','LOCK','RELEASE')),
    warehouse_id BIGINT        NOT NULL,
    location_id  BIGINT,
    sku_id       BIGINT        NOT NULL,
    qty_delta    NUMERIC(18,4) NOT NULL,
    before_qty   NUMERIC(18,4) NOT NULL,
    after_qty    NUMERIC(18,4) NOT NULL,
    ref_type     VARCHAR(50),
    ref_id       BIGINT,
    occurred_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    operator_id  BIGINT        NOT NULL
);
CREATE INDEX stock_movements_sku_time_idx ON stock_movements (sku_id, occurred_at DESC);

CREATE TABLE stock_transfers (
    id             BIGSERIAL PRIMARY KEY,
    transfer_no    VARCHAR(50) NOT NULL UNIQUE,
    from_warehouse_id BIGINT   NOT NULL,
    to_warehouse_id   BIGINT   NOT NULL,
    status         VARCHAR(32) NOT NULL DEFAULT 'DRAFT'
                   CHECK (status IN ('DRAFT','IN_TRANSIT','COMPLETED','CANCELLED')),
    shipped_at     TIMESTAMPTZ,
    received_at    TIMESTAMPTZ,
    operator_id    BIGINT      NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE stock_counts (
    id           BIGSERIAL PRIMARY KEY,
    count_no     VARCHAR(50) NOT NULL UNIQUE,
    warehouse_id BIGINT      NOT NULL,
    count_date   DATE        NOT NULL,
    status       VARCHAR(32) NOT NULL DEFAULT 'DRAFT'
                 CHECK (status IN ('DRAFT','COUNTING','PENDING_APPROVAL','COMPLETED','CANCELLED')),
    operator_id  BIGINT      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE stock_count_items (
    id           BIGSERIAL PRIMARY KEY,
    count_id     BIGINT        NOT NULL REFERENCES stock_counts(id) ON DELETE CASCADE,
    sku_id       BIGINT        NOT NULL,
    location_id  BIGINT,
    system_qty   NUMERIC(18,4) NOT NULL,
    counted_qty  NUMERIC(18,4),
    diff_qty     NUMERIC(18,4) GENERATED ALWAYS AS (counted_qty - system_qty) STORED,
    reason       TEXT
);

CREATE TABLE stock_alerts (
    id            BIGSERIAL PRIMARY KEY,
    alert_type    VARCHAR(32)   NOT NULL
                  CHECK (alert_type IN ('LOW_STOCK','OUT_OF_STOCK','EXPIRING','SLOW_MOVING','OVER_CAPACITY')),
    warehouse_id  BIGINT        NOT NULL,
    sku_id        BIGINT,
    threshold     NUMERIC(18,4),
    current_value NUMERIC(18,4),
    severity      VARCHAR(32)   NOT NULL DEFAULT 'MEDIUM'
                  CHECK (severity IN ('LOW','MEDIUM','HIGH','CRITICAL')),
    message       TEXT          NOT NULL,
    status        VARCHAR(32)   NOT NULL DEFAULT 'OPEN'
                  CHECK (status IN ('OPEN','ACKNOWLEDGED','RESOLVED')),
    detected_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    resolved_at   TIMESTAMPTZ
);

CREATE TABLE stock_thresholds (
    id            BIGSERIAL PRIMARY KEY,
    warehouse_id  BIGINT        NOT NULL,
    sku_id        BIGINT        NOT NULL,
    min_qty       NUMERIC(18,4) NOT NULL DEFAULT 0,
    max_qty       NUMERIC(18,4),
    safety_qty    NUMERIC(18,4) NOT NULL DEFAULT 0,
    UNIQUE (warehouse_id, sku_id)
);
```

### 5.9 shipping（船期管理）

```sql
CREATE TABLE voyages (
    id           BIGSERIAL PRIMARY KEY,
    voyage_no    VARCHAR(50)  NOT NULL UNIQUE,   -- 系统内部编号
    booking_no   VARCHAR(100),                    -- 订舱号
    bl_no        VARCHAR(100),                    -- 提单号
    vessel_name  VARCHAR(200),
    voyage_code  VARCHAR(50),                     -- 航次
    carrier      VARCHAR(200),
    forwarder    VARCHAR(200),
    pol          VARCHAR(100) NOT NULL,           -- 起运港
    pod          VARCHAR(100) NOT NULL,           -- 目的港
    etd          DATE,
    eta          DATE,
    atd          DATE,
    ata          DATE,
    status       VARCHAR(32)  NOT NULL DEFAULT 'DRAFT'
                 CHECK (status IN ('DRAFT','BOOKED','LOADED','DEPARTED','ARRIVED','CLEARED','DELIVERED','CANCELLED')),
    remark       TEXT,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    created_by   BIGINT       NOT NULL
);

CREATE TABLE voyage_shipment_plans (
    voyage_id        BIGINT NOT NULL REFERENCES voyages(id) ON DELETE CASCADE,
    shipment_plan_id BIGINT NOT NULL,             -- 引用 export.shipment_plans
    contract_id      BIGINT NOT NULL,
    contract_no      VARCHAR(50) NOT NULL,
    PRIMARY KEY (voyage_id, shipment_plan_id)
);

CREATE TABLE containers (
    id             BIGSERIAL PRIMARY KEY,
    voyage_id      BIGINT        NOT NULL REFERENCES voyages(id) ON DELETE CASCADE,
    container_no   VARCHAR(50)   NOT NULL,
    seal_no        VARCHAR(50),
    container_type VARCHAR(20)   NOT NULL,        -- 20GP / 40HQ
    gross_weight_kg NUMERIC(18,4),
    volume_cbm     NUMERIC(18,4),
    loaded_at      TIMESTAMPTZ,
    UNIQUE (voyage_id, container_no)
);

CREATE TABLE container_items (
    id                    BIGSERIAL PRIMARY KEY,
    container_id          BIGINT        NOT NULL REFERENCES containers(id) ON DELETE CASCADE,
    shipment_plan_item_id BIGINT        NOT NULL,
    sku_id                BIGINT        NOT NULL,
    qty                   NUMERIC(18,4) NOT NULL,
    package_count         INT,
    gross_weight_kg       NUMERIC(18,4),
    volume_cbm            NUMERIC(18,4)
);

CREATE TABLE transport_nodes (
    id          BIGSERIAL PRIMARY KEY,
    voyage_id   BIGINT      NOT NULL REFERENCES voyages(id) ON DELETE CASCADE,
    node_type   VARCHAR(32) NOT NULL
                CHECK (node_type IN ('BOOKING','LOADING','CUSTOMS_DECLARE','DEPARTURE',
                                     'ARRIVAL','CUSTOMS_CLEAR','DELIVERY')),
    seq         INT         NOT NULL,
    planned_at  TIMESTAMPTZ,
    actual_at   TIMESTAMPTZ,
    status      VARCHAR(32) NOT NULL DEFAULT 'PENDING'
                CHECK (status IN ('PENDING','IN_PROGRESS','COMPLETED','SKIPPED')),
    operator_id BIGINT,
    remark      TEXT,
    UNIQUE (voyage_id, node_type)
);

-- 保存某次船运实际使用的单证版本（文档 5. 单证附件【改造】）
CREATE TABLE shipping_documents (
    id          BIGSERIAL PRIMARY KEY,
    voyage_id   BIGINT       NOT NULL REFERENCES voyages(id) ON DELETE CASCADE,
    document_id BIGINT       NOT NULL,            -- 引用 export.documents
    doc_type    VARCHAR(50)  NOT NULL,
    version     INT          NOT NULL,
    file_path   VARCHAR(500) NOT NULL,
    attached_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    attached_by BIGINT       NOT NULL,
    UNIQUE (voyage_id, document_id, version)
);

CREATE TABLE freight_costs (
    id          BIGSERIAL PRIMARY KEY,
    voyage_id   BIGINT        NOT NULL REFERENCES voyages(id),
    cost_type   VARCHAR(50)   NOT NULL,           -- OCEAN_FREIGHT / THC / CUSTOMS_FEE / OTHER
    supplier_id BIGINT,
    amount      NUMERIC(18,2) NOT NULL,
    currency    VARCHAR(3)    NOT NULL,
    fx_rate     NUMERIC(18,8) NOT NULL,
    fx_rate_at  TIMESTAMPTZ   NOT NULL,
    fx_source   VARCHAR(50)   NOT NULL,
    base_amount NUMERIC(18,2) NOT NULL,
    invoice_no  VARCHAR(100),
    status      VARCHAR(32)   NOT NULL DEFAULT 'PENDING'
                CHECK (status IN ('PENDING','CONFIRMED','PAID')),
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE TABLE shipping_exceptions (
    id             BIGSERIAL PRIMARY KEY,
    voyage_id      BIGINT      NOT NULL REFERENCES voyages(id),
    exception_type VARCHAR(32) NOT NULL
                   CHECK (exception_type IN ('DELAY','ROLLOVER','AMENDMENT','DOC_MISSING','DAMAGE','OTHER')),
    severity       VARCHAR(32) NOT NULL DEFAULT 'MEDIUM',
    description    TEXT        NOT NULL,
    impact         TEXT,
    occurred_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at    TIMESTAMPTZ,
    resolution     TEXT,
    status         VARCHAR(32) NOT NULL DEFAULT 'OPEN'
                   CHECK (status IN ('OPEN','HANDLING','RESOLVED','CLOSED')),
    reported_by    BIGINT      NOT NULL
);
```

### 5.10 reporting（报表 / 驾驶舱 / 执行进度）

**全部为投影表**，由 Kafka 事件驱动更新，服务只暴露查询方法，数据库账号只有 SELECT + 投影写入权限。

```sql
-- 合同执行进度（文档 2.4）：一张汇总看板，不重复保存明细
CREATE TABLE contract_progress (
    contract_id          BIGINT PRIMARY KEY,
    contract_no          VARCHAR(50)   NOT NULL,
    customer_id          BIGINT        NOT NULL,
    customer_name        VARCHAR(200),
    total_amount         NUMERIC(18,2) NOT NULL,
    currency             VARCHAR(3)    NOT NULL,
    contract_status      VARCHAR(32)   NOT NULL,
    -- 采购
    purchase_order_count INT           NOT NULL DEFAULT 0,
    purchase_received_pct NUMERIC(5,2) NOT NULL DEFAULT 0,
    -- 仓储
    stock_locked_pct     NUMERIC(5,2)  NOT NULL DEFAULT 0,
    outbound_pct         NUMERIC(5,2)  NOT NULL DEFAULT 0,
    -- 船期
    voyage_status        VARCHAR(32),
    etd                  DATE,
    eta                  DATE,
    -- 信用证
    lc_status            VARCHAR(32),
    -- 单证
    doc_total            INT           NOT NULL DEFAULT 0,
    doc_approved         INT           NOT NULL DEFAULT 0,
    doc_completion_pct   NUMERIC(5,2)  NOT NULL DEFAULT 0,
    -- 退税
    tax_refund_status    VARCHAR(32),
    tax_refund_amount    NUMERIC(18,2),
    -- 收汇
    received_amount      NUMERIC(18,2) NOT NULL DEFAULT 0,
    pending_amount       NUMERIC(18,2) NOT NULL DEFAULT 0,
    overdue_amount       NUMERIC(18,2) NOT NULL DEFAULT 0,
    -- 责任人与异常
    owner_id             BIGINT,
    exception_count      INT           NOT NULL DEFAULT 0,
    updated_at           TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- 风险预警（文档 1. 驾驶舱 → 风险预警）
CREATE TABLE risk_alerts (
    id            BIGSERIAL PRIMARY KEY,
    source_module VARCHAR(50)  NOT NULL,          -- inventory / shipping / export / fx
    risk_type     VARCHAR(50)  NOT NULL
                  CHECK (risk_type IN ('LOW_STOCK','SHIPMENT_DELAY','DOC_MISSING',
                                       'FX_ANOMALY','RECEIVABLE_OVERDUE','LC_EXPIRING')),
    biz_ref_type  VARCHAR(50)  NOT NULL,
    biz_ref_id    BIGINT       NOT NULL,
    biz_ref_no    VARCHAR(100),
    severity      VARCHAR(32)  NOT NULL DEFAULT 'MEDIUM',
    message       TEXT         NOT NULL,
    detected_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    status        VARCHAR(32)  NOT NULL DEFAULT 'OPEN'
                  CHECK (status IN ('OPEN','ACKNOWLEDGED','RESOLVED')),
    UNIQUE (source_module, risk_type, biz_ref_type, biz_ref_id)
);

-- 经营总览指标（按日聚合，驾驶舱直接读）
CREATE TABLE daily_metrics (
    metric_date        DATE          NOT NULL,
    metric_key         VARCHAR(100)  NOT NULL,    -- contract_amount / purchase_cost / ...
    dimension          VARCHAR(100)  NOT NULL DEFAULT 'ALL',
    metric_value       NUMERIC(18,4) NOT NULL,
    updated_at         TIMESTAMPTZ   NOT NULL DEFAULT now(),
    PRIMARY KEY (metric_date, metric_key, dimension)
);

-- 消费幂等
CREATE TABLE processed_events (
    event_id       VARCHAR(100) NOT NULL,
    consumer_group VARCHAR(100) NOT NULL,
    processed_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, consumer_group)
);
```

### 5.11 audit（操作日志）

```sql
CREATE TABLE operation_logs (
    id            BIGSERIAL,
    trace_id      VARCHAR(64),
    module        VARCHAR(50)  NOT NULL,
    biz_type      VARCHAR(50)  NOT NULL,
    biz_id        BIGINT,
    biz_no        VARCHAR(100),
    action        VARCHAR(50)  NOT NULL,          -- CREATE / UPDATE / APPROVE / CANCEL / EXPORT
    operator_id   BIGINT       NOT NULL,
    operator_name VARCHAR(100) NOT NULL,
    ip            INET,
    user_agent    TEXT,
    before_data   JSONB,
    after_data    JSONB,
    result        VARCHAR(32)  NOT NULL DEFAULT 'SUCCESS',
    error_message TEXT,
    occurred_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    PRIMARY KEY (id, occurred_at)
) PARTITION BY RANGE (occurred_at);

CREATE TABLE operation_logs_2026_07 PARTITION OF operation_logs
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');

CREATE INDEX operation_logs_biz_idx ON operation_logs (biz_type, biz_id, occurred_at DESC);
CREATE INDEX operation_logs_operator_idx ON operation_logs (operator_id, occurred_at DESC);
```

> **不变量**：`GRANT INSERT, SELECT ON operation_logs TO erp_audit;` 不给 UPDATE / DELETE。文档：「不允许编辑或删除」。分区表便于按月归档。

### 5.12 mail（邮件）

阶段 6 落地。它承载三件已确认的需求：报价单发送到客户邮箱（§5.6.1）、
账号邀请链接（见「账号开通与初始密码」）、审批到达提醒——以及下面这一整块
**业务邮件与群发**（2026-07-29 业务确认，六条需求）。

#### 5.12.1 六条需求与可行性

| # | 需求 | 结论 |
|---|---|---|
| 1 | 群发可靠，失败重试，超限转人工 | ✅ 能做，重点在**幂等**，见 5.12.3 |
| 2 | 收件人只看到发给自己，看不到还有别人 | ✅ 能做，且**必须**这么做，见 5.12.4 |
| 3 | 正文写 `Dear {{contact_name}}` 自动替换成联系人姓名 | ✅ 能做，见 5.12.5 |
| 4 | 上级能看见下属发的邮件 | ✅ 能做，**边界要说清**，见 5.12.6 |
| 5 | 员工能知道邮件是否被打开 | ⚠️ **能做但不可靠**，见 5.12.7 |
| 6 | Best regards 尾缀存成模板 | ✅ 能做，见 5.12.8 |

只有第 5 条要打折扣。其余五条都是常规工程量。

#### 5.12.2 硬性前提：发送通道

**先厘清一个常见误解：问题不在 SMTP 协议，在于拿谁的邮箱当通道。**
SES / SendGrid 自己也提供 SMTP 端点，用它们「循环发」完全没问题。
真正不能用的是**业务员的企业邮箱账号**（腾讯企业邮 / 阿里 / Exchange /
Google Workspace）当群发通道。四个理由：

1. **配额是按「人写信」设计的**。企业邮箱的上限假设一个人一天写几十封，
   典型量级是每天几百到几千封、每分钟几十封（看套餐，签之前查自己的）。
   群发撞上限的表现正是**发到一半开始失败**——「有的成功有的失败」的头号成因。
2. **拿不到投递结果——最致命的一条**。SMTP 的 `250 OK` 只代表「这一跳我收下了」。
   之后的结果通过**退信邮件**回到发件箱，那是给人看的邮件：要解析就得反过来用
   IMAP 读发件人收件箱，退信本身可能进垃圾箱，格式各家不统一，
   而打开 / 点击**完全没有**。**需求 1 和需求 5 在这个通道上做不出来。**
3. **信誉隔离不了**。企业邮箱出口 IP 所有租户共享，DKIM 签的是邮箱服务商的域，
   没法给群发单独配子域。开发信被投诉 → 拉低整个企业邮箱信誉 →
   连累「跟老客户对合同」那些真正要紧的邮件。
4. **会被自己的邮箱服务商封号**。风控盯着「一个账号短时间给几百个陌生外部地址
   发信」这种模式，轻则限流，重则封号——真封了业务员连正常邮件都发不出去。

**换成专业发送服务，本质买的是反馈回路**：`delivered` / `bounced`（硬软区分）/
`complained` / `opened` / `clicked` 以结构化 webhook 实时推回来。

| | Amazon SES | SendGrid | 阿里云邮件推送 |
|---|---|---|---|
| 价格 | 最便宜 | 有免费额度，之后贵些 | 便宜 |
| Webhook | 完整（SNS / EventBridge） | 完整，面板现成 | 有，字段少些 |
| 子域独立 DKIM | ✅ | ✅ | ✅ |
| 上手 | 新账号在沙箱，需申请解除 | 最快 | 快 |
| 发欧美链路 | 好 | 好 | 一般 |

**倾向 SES**（外贸发欧美 / 拉美，且价格最低）。**排除 Postmark**——送达率口碑最好，
但它**明确禁止冷邮件营销**，开发信正好撞在禁令上。价格与配额会变，签之前复核。

选定服务商后，**协议用 HTTP API 而非 SMTP 端点**（工程便利，不是原则）：
API 直接返回 `MessageId` 可立刻落库供 webhook 关联；能带自定义 header / message tag
挂 `message_key` 做幂等；不用维护 TLS 连接池；错误是结构化 JSON 不是要正则抠的状态码。

配套的两条：

- **群发用独立子域**（如 `mail.<公司域>.com`），与日常往来邮件的主域隔离。
  开发信把子域信誉打坏，不会连累「跟老客户对合同」那些真正重要的邮件。
- 子域配齐 **SPF + DKIM + DMARC**。2024 年起 Gmail、Outlook 对批量发件人
  强制要求 DKIM 签名与 DMARC 策略，缺一条就是大面积静默丢弃。

> **事务性邮件是否也必须换？** 量上不必——报价单、账号邀请、审批提醒一天几十封，
> 企业邮箱扛得住。但需求 1（失败转人工）和需求 5（送达可见）**对事务性邮件同样成立**：
> 报价单没送到，销售也要知道。所以结论是**一套服务商两用**：
> 同一账号下开两个子域、两个 configuration set、两条队列，营销拥堵不影响事务性。

#### 5.12.2.1 投递架构：Kafka 传触发，数据库当队列

**群发的 500 封信不进 Kafka。** 进 Kafka 的只是「要发一次群发」这个跨服务通知。

```
① 触发（跨服务）——沿用已有的 outbox 模式
   export 发出报价单 → outbox → Kafka → mail 消费 → 插 1 行
   iam 建员工        → outbox → Kafka → mail 消费 → 插 1 行

② 逐封投递（服务内部）——数据库就是队列
   email_messages N 行 ──worker 轮询 FOR UPDATE SKIP LOCKED──▶ 服务商
```

群发**连 ① 都不需要**：用户在群发页点发送，请求直接进 mail，
插 N 行 `email_messages`，worker 自己发完，全程不过 Kafka。

**为什么逐封投递不放 Kafka：**

1. **速率控制**。服务商有每秒上限，`LIMIT 50` 一句就精确控速；
   Kafka 消费者要自己做令牌桶，多分区下全局限速很别扭。
2. **退避重试**。`next_retry_at` 一列解决。Kafka 里做延迟重试要么建一堆
   retry topic（5min / 30min / 2h 各一个），要么阻塞分区——
   一封卡住的信会把它后面整个分区堵死。
3. **状态要能查**。「哪些失败待人工处理」是需求 1 明确要的页面，那是一句 SQL。
   Kafka 是日志，查不了。
4. **状态表反正必须有**。需求 1 与需求 5 都要求逐收件人记状态；
   表已经在了，再往 Kafka 放一份就是**两个真相源**。
5. **量级不够**。一次群发几百上千封，数据库队列毫无压力。
   Kafka 是为这里不存在的吞吐量准备的。

取任务的写法与 `pkg/outbox/relay.go` 完全一致：

```sql
SELECT * FROM email_messages
WHERE tenant_id = $1
  AND status = 'QUEUED'
  AND (next_retry_at IS NULL OR next_retry_at <= now())
ORDER BY id
LIMIT $2
FOR UPDATE SKIP LOCKED;
```

`SKIP LOCKED` 是关键：没有它多个 worker 会阻塞在同一行上，有了它它们各取各的。

> **分工**：Kafka 负责跨服务传消息，数据库负责当队列。
> 这是本系统既有的分工，邮件不另立一套。

#### 5.12.3 需求 1：可靠投递

一次群发 = 一个 campaign，**每个收件人一行 message**，各自独立重试。

```sql
CREATE TABLE email_campaigns (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     BIGINT       NOT NULL,
    campaign_no   VARCHAR(32)  NOT NULL,
    subject_tpl   TEXT         NOT NULL,       -- 含 {{变量}} 的原文
    body_tpl      TEXT         NOT NULL,
    signature_id  BIGINT,
    sender_id     BIGINT       NOT NULL,       -- 发起人
    sender_name   VARCHAR(100) NOT NULL,
    status        VARCHAR(20)  NOT NULL,       -- DRAFT / SENDING / DONE
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ,
    UNIQUE (tenant_id, campaign_no)
);

CREATE TABLE email_messages (
    id            BIGSERIAL PRIMARY KEY,
    tenant_id     BIGINT       NOT NULL,
    campaign_id   BIGINT,                      -- 单发时为 NULL
    message_key   UUID         NOT NULL,       -- 幂等键，发送前生成
    sender_id     BIGINT       NOT NULL,
    to_email      VARCHAR(320) NOT NULL,
    to_name       VARCHAR(200) NOT NULL,
    customer_id   BIGINT,                      -- 快照，用于「这个客户发过什么」
    contact_id    BIGINT,
    subject       TEXT         NOT NULL,       -- 渲染后
    body_html     TEXT         NOT NULL,       -- 渲染后：客户实际收到的那一封
    -- QUEUED / SENDING / ACCEPTED / SEND_UNKNOWN / DELIVERED /
    -- SOFT_BOUNCED / HARD_BOUNCED / NEEDS_ATTENTION，见 5.12.3 与 5.12.3.1
    status        VARCHAR(24)  NOT NULL,
    attempt_count INT          NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    provider_id   VARCHAR(128),                -- 服务商返回的 message id
    last_error    TEXT,
    queued_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    sent_at       TIMESTAMPTZ,
    delivered_at  TIMESTAMPTZ,
    UNIQUE (tenant_id, message_key)
);
```

状态机：

```
QUEUED ──▶ SENDING ──▶ ACCEPTED ──▶ DELIVERED
             │                └──▶ SOFT_BOUNCED ─┐
             │                └──▶ HARD_BOUNCED   │ 永久失败
             ├──▶ RETRYABLE ──(退避)──▶ QUEUED    │
             └──▶ NEEDS_ATTENTION ◀───────────────┘
```

**重试只对可重试错误。** 分类必须明确，不能一律重试：

| 错误 | 处理 |
|---|---|
| 网络超时、服务商 5xx、限流 | 退避重试 |
| SMTP 4xx（含灰名单） | 退避重试——灰名单就是靠重试过的 |
| `550 5.1.1` 地址不存在 | **不重试**，直接 `HARD_BOUNCED` |
| `550 5.7.x` 内容/策略拒收 | **不重试**，重发只会再被拒 |
| 变量渲染失败 | **发都不发**，见 5.12.5 |

退避 1min → 5min → 30min → 2h → 6h，共 5 次（可配）。耗尽后进
`NEEDS_ATTENTION`，前端一个「发送失败待处理」页面，可以改地址重发 /
标记放弃 / 转人工联系。硬退信同样进这个队列，因为地址需要有人去核。

> **不变量 · 重试必须幂等。** 重试最大的风险不是发不出去，是**发两遍**。
> `message_key` 在**调用服务商之前**生成并落库，随请求作为**标签**带出去
> （SES 的 message tag / SendGrid 的 `custom_args` / 自定义 header）。
> 如果状态停在 `SENDING`，**不允许盲目重发**——先凭这个标签反查服务商侧的
> 投递事件；查不到才按 5.12.3.1 的策略处理。
> 客户收到两封同样的开发信，比晚收到一天严重得多。

##### 5.12.3.1 网络中断：发没发出去，往往无法确定

**更正一条容易想当然的事：邮件服务商大多没有请求级幂等键。**
Stripe 那种 `Idempotency-Key`（重复请求直接返回第一次的结果）在
SES v2 `SendEmail`、SendGrid v3 `mail/send` 上**没有等价物**。
它们能带的只有标签，标签**不会让服务商去重**，只能让我们事后查得到。
选型时要实际确认，不要照抄本节。

网络故障分三种，确定性完全不同：

| 情况 | 例子 | 发出去了吗 | 动作 |
|---|---|---|---|
| ① 连接未建立 | DNS 失败、connection refused、TLS 握手失败 | **100% 没有**——字节从未离开本机 | 直接重试，安全 |
| ② **请求发了，响应没回来** | 读超时、连接中断、504 | **不知道** | 见下 |
| ③ 明确错误响应 | 400 / 429 / 503 | 知道失败了 | 按 5.12.3 的分类重试表 |

**只有 ② 是难的。** 服务商可能已经收下并投递，只是响应在回程丢了。

**处理 ②：先反查，查不到再按邮件类型选边。**

1. **反查**：`message_key` 作为标签传出，超时后**不立刻重发**，等服务商的
   `Send` / `Delivery` 事件。收到带该标签的事件 → 已发出，转 `ACCEPTED`。
   注意**等不到 ≠ 没发出**，也可能只是事件延迟，所以这是概率判断而非确定判断，
   要设判定窗口（默认 10 分钟）。
2. **窗口内没等到，按类型选边**——这是业务决策，不是技术决策，按邮件类型配置：

| 策略 | 做法 | 适用 |
|---|---|---|
| **宁可漏发** | 不自动重发，进人工队列由人决定 | **营销 / 开发信**——两封一样的开发信观感极差，且拉高投诉率 |
| **宁可重发** | 自动重发，接受偶尔重复 | **报价单、账号邀请、审批提醒**——没收到就丢生意，收到两封只是尴尬 |

**状态机要为 ② 留一个格子**，不能把它和「明确失败」混在一起：

```
QUEUED  →  SENDING  →  ACCEPTED        服务商明确收下（拿到 provider_id）
              │
              ├──────→  SEND_UNKNOWN   超时 / 连接中断，发没发不知道
              └──────→  FAILED         明确失败
```

进程崩在 `SENDING` 的记录，启动时扫描超过窗口时长的一律转 `SEND_UNKNOWN`。

> **不变量 · 落库必须早于调用。** 顺序只能是
> ①落库 `SENDING` + `message_key`（事务提交）→ ②调服务商 → ③按响应更新。
>
> | 崩在 | 库里是 | 实际 | 恢复时 |
> |---|---|---|---|
> | ①之前 | 无记录 | 没发 | 重跑，安全 |
> | ①②之间 | `SENDING` | **一定没发** | 直接发 |
> | ②③之间 | `SENDING` | **可能发了** | 凭标签反查事件再定 |
>
> **那行记录是「有人尝试过」这件事的唯一证据。** 反过来做（先调再落库），
> 进程崩在中间就连「可能发过」都问不出来——信到了客户手上，库里一行都没有。
> 那是静默的、不可恢复的丢失，比重复严重得多。
>
> 这条规则本系统已经在用：`pkg/outbox` 的注释写的是同一件事——业务变更与事件
> 在同一个本地事务里提交，relay 事后投递，**绝不在业务事务里调用 Kafka 生产者**。
> 邮件只是把下游从 Kafka 换成了发送服务商。

**日常抖动不需要人发现。** 情况①的记录留在 `QUEUED`，worker 退避重试，
网络恢复即自动发出；只有重试耗尽才进人工队列。真正需要人看的只有
`SEND_UNKNOWN`，正常运行下这是罕见事件。

#### 5.12.4 需求 2：一人一封，看不到别人

**实现方式是逐个独立发送，不是 BCC。** BCC 群发有四个问题：

1. 依然是一次投递，中转环节和某些客户端仍可能暴露收件人规模；
2. 大量 BCC 是**极强的垃圾邮件特征**，直接拉高判垃圾分数；
3. 无法个性化——需求 3 做不了；
4. 无法逐人追踪——需求 1 的退信定位和需求 5 的打开检测都落空。

一人一封同时满足需求 2、3、5：**它们是同一个架构决策的三个侧面。**

> **不变量**：群发生成的每封邮件，`To:` 有且只有一个地址，
> `Cc` / `Bcc` 为空。这条要在发送适配器里强制，不靠调用方自觉。

#### 5.12.5 需求 3：变量替换

设计成通用变量，不只是称呼：

| 变量 | 取值 |
|---|---|
| `{{contact_name}}` | 联系人姓名 |
| `{{contact_first_name}}` | 名（欧美客户习惯只称呼名） |
| `{{company_name}}` | 客户公司名 |
| `{{my_name}}` / `{{my_title}}` / `{{my_phone}}` | 发件业务员 |
| `{{contract_no}}` | 关联合同（催款、发货通知用） |

**语法用 `{{ }}` 定界，不用裸词。** 需求里写的是 `Dear connect_to`，
裸词的问题是会误伤正文——正文里恰好出现同样的词就被替换掉了。
前端在编辑器里做成可点击插入的标签，用户不用手打。

> **不变量 · 渲染不出来就不发。** 任何一个变量取不到值（联系人没填姓名、
> 合同号为空），这封信**不进发送队列**，直接进 `NEEDS_ATTENTION`。
> 客户收到 `Dear ,` 或 `Dear {{contact_name}},` 在外贸场景里是实打实的事故，
> 比没发出去更糟。

发送前必须能**预览**：选一个收件人，看渲染后的完整样子（含签名）。

#### 5.12.6 需求 4：上级能看见下属的邮件

复用现有的 `role_data_scopes`，新增 `mail` 模块：

| 范围 | 能看到 |
|---|---|
| `SELF` | 只有自己发的 |
| `DEPT` | 本部门所有人发的 |
| `ALL` | 全部 |

**边界必须说清楚两条：**

1. **只覆盖通过系统发的邮件。** 业务员用 Outlook 自己发的，系统看不见。
   要全覆盖就得托管邮箱（IMAP 抓取），那是另一个量级的工程，
   而且会把员工的全部邮件——包括私人的——拉进系统，范围要重新界定后再谈。
2. **查看要记审计。** 谁在什么时候看了谁的邮件，写 `operation_logs`
   （`module='mail'`, `action='VIEW'`）。这不是防上级，是出事时能说清楚。

#### 5.12.7 需求 5：已读检测——能做，但它不是事实

实现是标准的追踪像素：邮件里嵌一个 1×1 透明图 `https://.../t/{token}.gif`，
被加载就记一次 open。**问题在于这个信号三个方向都会失真：**

| 失真 | 后果 |
|---|---|
| **Apple 邮件隐私保护**（iOS 15+ / macOS Monterey+ 默认开） | Apple 代理在邮件**到达时预加载所有图片**，用户根本没看也算「已打开」，时间戳是收到时间。欧美 B2B 里 Apple 邮箱占比不低——**假已读** |
| **Gmail 图片代理** | 图片被缓存到 Google 服务器。首次能测到，**重复打开测不准**，IP 是 Google 的不是客户的 |
| **默认不加载图片的客户端**（部分 Outlook 桌面配置、纯文本阅读） | 真读了也**测不到**——**假未读** |

所以：

> **不变量 · 「打开」是弱信号。** UI 措辞必须是「检测到打开」而不是「已读」，
> 并在旁边给出上面三条说明。**业务上不允许拿它做判断依据**——
> 「他已读没回说明不想理我」这个推论在数据上不成立。

**更可靠的信号是点击。** 正文里的链接走带 token 的跳转，点击是用户主动行为，
没有预加载问题。两个都记，**报表以点击为主、打开为辅**。

两个副作用要知道：追踪像素会**轻微抬高垃圾判定分数**；欧盟 GDPR 下追踪
理论上需要告知。B2B 外贸影响有限，但这是已知的成本，不是零成本功能。

```sql
CREATE TABLE email_events (               -- append-only
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT      NOT NULL,
    message_id  BIGINT      NOT NULL,
    kind        VARCHAR(16) NOT NULL,     -- DELIVERED/OPEN/CLICK/BOUNCE/COMPLAINT
    occurred_at TIMESTAMPTZ NOT NULL,
    ip          INET,
    user_agent  TEXT,
    is_proxy    BOOLEAN     NOT NULL DEFAULT false,  -- 判定为 Apple/Gmail 代理预取
    target_url  TEXT,                                -- kind='CLICK' 时
    raw         JSONB
);
CREATE INDEX email_events_msg_idx ON email_events (message_id, occurred_at);
```

`is_proxy` 靠 User-Agent 与 IP 段识别（Apple 的 `Mozilla/5.0` + Apple 代理段、
Google 的 `GoogleImageProxy`）。前端默认**把代理预取从「已打开」里排除**，
可切换显示。识别规则会随对方变化而失效，这是维护成本，不是一次性的。

#### 5.12.8 需求 6：签名模板

```sql
CREATE TABLE email_signatures (
    id           BIGSERIAL PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL,
    owner_type   VARCHAR(16)  NOT NULL,   -- TENANT（公司统一）/ EMPLOYEE（个人）
    owner_id     BIGINT       NOT NULL,   -- employee_id，TENANT 时为 0
    name         VARCHAR(100) NOT NULL,   -- 「英文签名」「西语签名」
    content_html TEXT         NOT NULL,
    is_default   BOOLEAN      NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX email_signatures_default_idx
    ON email_signatures (tenant_id, owner_type, owner_id) WHERE is_default;
```

- 每人可以有多个（中 / 英 / 西各一套），一个默认，写信时自动附加、可切换、可临时关掉
- **签名里也支持变量**（`{{my_name}}` `{{my_title}}` `{{my_phone}}`），
  所以公司统一模板改一次，全员的签名跟着变，不用挨个通知改
- 两层：**公司级模板**（管理员维护，保证格式统一）+ **个人覆盖**。
  外贸公司通常要求对外签名格式一致，但业务员的电话分机各不相同

#### 5.12.9 地址可达性：一次学会，永久生效

**发之前无法可靠地知道一个地址存不存在。** 这不是工程能力问题，是协议现实。
所以设计不能建立在「先验证再发」上，只能建立在「发过一次就把结论记住」上。

**退信分三类，处理方式完全不同：**

| 类 | 典型码 | 含义 | 动作 |
|---|---|---|---|
| **硬退信** | `550 5.1.1` User unknown | 人离职、地址拼错 | 永久失败，标记地址作废 |
| | `550 5.1.2` Host unknown | 域名过期、公司倒闭 | 同上 |
| | `550 5.7.1` Blocked by policy | 信誉 / DKIM / 内容被拒 | 不重试，重发只会再被拒 |
| **软退信** | `452 4.2.2` Mailbox full | 对方邮箱满了 | 退避重试 |
| | `450 4.2.0` Greylisting | **故意先拒**，测你会不会重试 | 退避重试——灰名单就是靠这个过的 |
| | `421 4.7.0` Too many connections | 对方在限流 | 退避重试 |
| **静默丢弃** | 无（回了 `250`） | 收下了直接扔垃圾箱 / 黑洞 | **没有退信可读** |

第三类最阴险：系统显示「已投递」，客户根本没看见。这是打开 / 点击追踪即使不可靠
也仍有价值的原因——**某个域名下全部零打开，通常意味着被静默丢弃了**。

还有一个不是退信但更危险的信号：**投诉（complaint）**，用户点了「举报垃圾邮件」，
经 feedback loop 回来。投诉率超过 0.1% 就会被 Gmail 降权，杀伤力大于退信。

**验证只有四层，前三层都只降低概率：**

1. **语法**（免费）。只挡得住 `john@@abc..com`，挡不住格式完美但人已离职的地址。
2. **域名 / MX 检查**（免费，毫秒级）——**性价比最高，应在录入联系人时就做**。
   挡掉域名过期、公司倒闭、域名拼错、根本不收邮件的域名。
3. **SMTP 探测**（`VRFY` / 走到 `RCPT TO` 再 `RSET`）——**实际基本废了**：
   多数服务器关闭了 `VRFY`；Gmail / Outlook 对任何 `RCPT TO` 一律回 `250`；
   很多企业邮箱配了 catch-all；而且频繁探测会让 IP 进黑名单，
   因为这正是垃圾邮件发送者的行为特征。第三方验证服务本质就是这个加历史库比对，
   对 catch-all 域名依然只能标 `unknown`。
4. **真发一次，读退信**——唯一确定的方法。

**落地：状态存在联系人上，不是每次群发重新赌一遍。**

```sql
ALTER TABLE customer_contacts
    ADD COLUMN email_status VARCHAR(20) NOT NULL DEFAULT 'UNKNOWN',
    -- UNKNOWN / VALID（至少成功投递过一次）/ SOFT_FAILING（连续软退）/ INVALID（硬退）
    ADD COLUMN email_checked_at  TIMESTAMPTZ,
    ADD COLUMN email_bounce_code VARCHAR(32),
    ADD COLUMN email_bounce_at   TIMESTAMPTZ;
```

```
录入 → 语法 + MX 检查 → UNKNOWN
首发 → delivered → VALID ｜ 硬退 → INVALID ｜ 连续 3 次软退 → SOFT_FAILING
以后群发：INVALID 自动跳过，并在人工队列提示「XX 的邮箱已失效，请更新」
```

> **不变量**：`INVALID` 的地址不进发送队列。持续往死地址发是**最快毁掉域名信誉的
> 方式**——收件方把高退信率当作垃圾邮件发送者的头号特征。退信率要压在 2% 以下，
> 最好 1%。5% 退信率的发件人，连正常业务邮件都会开始进垃圾箱。
> 外贸客户库这个风险尤其高：几年前的展会名片、离职的采购、倒闭的贸易公司，
> 死地址比例很容易到两位数。

**角色地址**（`info@` `sales@` `admin@`）标记出来但**不自动排除**：它们通常存在
（不会退信）却没人认真看、投诉率偏高，但外贸有时确实只拿得到这一个地址。

#### 5.12.10 还需要补的（本节未展开）

- **退订**。发到欧美的开发信受 CAN-SPAM / GDPR 约束，需要可识别的退订机制和
  实际地址。退订名单是**租户级黑名单**，任何 campaign 都不得越过。
- **域名预热**。新子域第一天就发几千封必被判垃圾，要按天爬坡。
- **发送速率限制**。服务商侧和我们侧都要有，避免一次群发把配额打满，
  导致同一时间的报价单、邀请链接发不出去——**事务性邮件与营销邮件走不同队列、
  不同子域**，营销拥堵不能影响业务。

---


#### 5.12.11 会话串联：Message-ID 是唯一可靠的线索

**目标**：打开一封信，看到这条线上的全部往来 —— 我们发的、客户回的，按时间上下交替，
就像 Gmail 的会话视图。

**做这件事只有一个可靠依据。** 客户回信时，他的邮件客户端会把原信的 `Message-ID`
原样放进 `In-Reply-To`，并把整条线的 id 累积进 `References`。这是 RFC 5322 规定的，
所有客户端都遵守。其他办法都不行：

| 办法 | 为什么不行 |
|---|---|
| 按主题匹配 | `Re:` / `答复:` / `RE:` 前缀各客户端不一；客户改标题很常见；转发会带上无关的主题 |
| 按对方邮箱匹配 | 同一个客户几年下来几十上百封，全糊成一条线 |
| 按时间接近 | 猜的 |

**因此有一条必须现在定下、且越晚越贵的约定：**

```
Message-ID: <{message_key}@{发信域名}>
```

`message_key` 每封信已经逐封落库了（UUID，见 §5.12.3.1），所以只要发信时按这个规则
生成头，**历史邮件的 Message-ID 也能反推出来** —— 这一点让这件事不至于紧急到必须
今天实现，但规则必须先定，否则将来换了个生成方式，之前发的信就永久断链。

两个前提：

1. **发信域名要定**（建议 `mail.<公司域名>`，见 §5.12.2）。
2. **发送必须走能自己设头的接口** —— SES 的 `SendRawEmail` 而不是简化版
   `SendEmail`（后者由服务商自己生成 Message-ID）。反正带附件和
   multipart/alternative 本来就必须走 raw 那条，不算额外成本。

**入站侧的表**（等服务商定了再建）：

```sql
-- 收到的信。和 email_messages 分开：一个是我们说的，一个是对方说的，
-- 状态机、重试、数据范围规则全都不一样。
CREATE TABLE email_inbound (
    id             BIGSERIAL PRIMARY KEY,
    tenant_id      BIGINT NOT NULL DEFAULT 1,
    message_id     TEXT NOT NULL,          -- 对方那封信自己的 Message-ID
    in_reply_to    TEXT NOT NULL DEFAULT '',
    references_ids TEXT[] NOT NULL DEFAULT '{}',
    -- 串联结果：命中的我方 email_messages.id。匹配不上就是 0，
    -- 说明是客户主动来信而不是回复——那也是要看的，不能丢。
    replies_to     BIGINT NOT NULL DEFAULT 0,
    from_email     VARCHAR(320) NOT NULL,
    subject        TEXT NOT NULL DEFAULT '',
    body           TEXT NOT NULL DEFAULT '',
    body_format    VARCHAR(8) NOT NULL DEFAULT 'TEXT',
    received_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, message_id)         -- 服务商会重投，这是幂等键
);
```

**匹配顺序**：先按 `in_reply_to` 精确命中；命中不了再顺着 `references_ids` 从后往前找
（转发链里最近的那封）；都找不到就挂在客户身上、`replies_to = 0`。
**匹配不上不是错误** —— 客户直接来信、换了邮箱回、客户端不带头，都会这样，
这些信一样要出现在收件箱里。

> **不变量**：入站信按 `Message-ID` 幂等。服务商保证的是至少投递一次，
> 同一封信会重复推送，`UNIQUE (tenant_id, message_id)` 是唯一防线。

#### 5.12.12 草稿：未说出口的话，不是往来

草稿存在 `email_drafts`，**不是** `email_campaigns` 里一行 `status='DRAFT'`。两个原因：

1. **编号**。campaign 一创建就抽号，草稿废弃会在序列里留下永久空洞 ——
   而人是把编号当记录看的。
2. **收件人**。campaign 的收件人是 `email_messages` 里的真行，worker 会认领。
   草稿的收件人是还在编辑的一个列表。存成 message 意味着 worker 可能捞走一封没写完的信。

所以草稿的收件人和附件存成 JSONB —— 它们是**发信的草稿**，不是发信本身。
草稿发出时走普通的创建路径，那一步才抽号、才写 message 行。

**草稿是私人的。** 主管有 ALL 数据范围时能读下属的**往来**，但读不到草稿 ——
草稿是还没说出口的话，谁都没对谁说过。这个服务里**不存在**返回他人草稿的查询，
所有权检查写在 SQL 的 `WHERE` 里而不只在参数里，否则拿别人的 id 做 upsert
会静默覆盖掉对方的草稿。


## 6. 关键业务流程

### 6.1 报价到合同生效

```
销售创建报价单
  └─ export → fx.GetLatestRate()            [同步] 取汇率快照写进 quotations
  └─ export → masterdata.GetCustomer()      [同步] 校验客户有效
  └─ export → product.ListSkus()            [同步] 取产品、SKU、单位
  └─ export → masterdata.NextNumber()       [同步] 生成报价单号

客户接受报价
  └─ export 内部：报价 → 合同草稿（同一服务，单事务）

提交合同审批
  └─ export → approval.Submit(CONTRACT, contract_id)   [同步]
  └─ 合同 status = PENDING_APPROVAL

审批通过
  └─ approval 发 ApprovalApproved 事件 → Kafka
  └─ export 消费 → 合同 status = PENDING_SIGN

客户签署 → 合同生效
  └─ export 单事务内：
       contracts.status = EFFECTIVE
       生成 receivable_plans（收汇计划）
       写 outbox_events: ContractEffective
  └─ relay → Kafka erp.export.contract.v1

下游消费 ContractEffective：
  ├─ procurement → 生成 purchase_requirements
  ├─ reporting   → 初始化 contract_progress
  └─ audit       → 记录操作日志
```

### 6.2 出货计划确认与库存锁定（唯一的 saga）

```
创建出货计划（DRAFT）
  └─ export 单事务内校验：
       SELECT ... FROM contract_items WHERE id = ? FOR UPDATE   ← 行锁
       SUM(已有计划 planned_qty) + 本次 <= contract_items.qty
       不满足则拒绝

确认出货计划
  └─ status = PENDING_STOCK
  └─ export → inventory.LockStock({
                lock_ref_type: "SHIPMENT_PLAN",
                lock_ref_id:   plan_id,          ← 幂等键
                items: [...]
              })                                 [同步 gRPC]
       │
       ├─ 成功 → status = CONFIRMED
       │         写 outbox: ShipmentPlanConfirmed
       │
       └─ 库存不足 → status = STOCK_SHORTAGE
                    shortage_detail = 缺口明细
                    前端提示「建议采购或调拨」

取消出货计划（补偿）
  └─ export → inventory.ReleaseLock(lock_ref)    [同步，幂等]
```

`LockStock` 的幂等性由 `stock_locks` 的 `UNIQUE (lock_ref_type, lock_ref_id, sku_id)` 保证：重试时冲突即视为已锁定成功。这让超时重试完全安全，不需要复杂的 saga 状态机。

### 6.3 采购到入库

```
采购需求（来自 ContractEffective 事件）
  └─ 采购员创建采购单 → 提交审批
  └─ procurement → approval.Submit(PURCHASE_ORDER, po_id)
  └─ 审批通过 → PO status = APPROVED

供应商到货
  └─ procurement 创建收货单
  └─ procurement → inventory.CreateInbound()      [同步]
  └─ inventory 单事务内：
       inbounds + inbound_items
       stocks.on_hand_qty += qty
       stock_movements 流水
       outbox: StockChanged
  └─ Kafka → export 消费，检查是否有 STOCK_SHORTAGE 的计划可重试
           → reporting 更新 contract_progress
```

### 6.4 出库到装运

```
出货计划 CONFIRMED
  └─ inventory 创建出库任务（消费 ShipmentPlanConfirmed 事件）
  └─ 仓管拣货 → 确认出库
  └─ inventory 单事务内：
       stocks.on_hand_qty -= qty
       stock_locks.status = CONSUMED
       stock_movements 流水
       outbox: StockOutbound
  └─ Kafka → export 更新 shipment_plan_items.shipped_qty
           → shipping 更新待装运货物

shipping 装柜 → 更新 transport_nodes
  └─ outbox: NodeReached
  └─ Kafka → reporting 更新 contract_progress.voyage_status
```

---

## 7. 代码结构与骨架

### 7.1 仓库结构

```
erp-go/
├── proto/                          所有服务的 gRPC 契约，统一编译
│   ├── common/v1/
│   │   ├── pagination.proto
│   │   ├── operator.proto
│   │   └── money.proto
│   ├── export/v1/export.proto
│   ├── inventory/v1/inventory.proto
│   └── ...
├── gen/                            protoc 生成物（提交进仓库，避免环境差异）
│   └── go/...
├── services/
│   ├── gateway/
│   ├── iam/
│   ├── masterdata/
│   ├── product/
│   ├── fx/
│   ├── approval/
│   ├── export/
│   ├── procurement/
│   ├── inventory/
│   ├── shipping/
│   ├── reporting/
│   └── audit/
├── pkg/                            跨服务共享的基础库
│   ├── money/                      decimal 封装、货币换算
│   ├── outbox/                     outbox 写入与 relay
│   ├── kafkax/                     生产者、消费者、幂等中间件
│   ├── grpcx/                      拦截器：认证、日志、追踪、错误映射
│   ├── pgx/                        连接池、事务助手
│   ├── apierr/                     统一业务错误码
│   ├── blobstore/                  S3 兼容对象存储封装（本地 MinIO，云端 S3/OSS）
│   └── idempotency/
├── deploy/
│   ├── docker-compose.yml
│   ├── docker-compose.infra.yml    仅基础设施，本地开发用
│   └── Dockerfile                  多阶段，所有服务共用
├── frontend/                       Vue 3
├── docs/
│   └── ARCHITECTURE.md             本文档
├── Makefile
├── buf.yaml / buf.gen.yaml         proto 编译与 lint
└── go.work                         Go workspace，各服务独立 module
```

### 7.2 单个服务的内部结构

以 `export` 为例：

```
services/export/
├── cmd/main.go                     启动：配置、依赖装配、gRPC server、outbox relay
├── internal/
│   ├── config/config.go
│   ├── domain/                     领域层：聚合、值对象、不变量。无外部依赖
│   │   ├── contract.go
│   │   ├── shipment_plan.go
│   │   ├── fxsnapshot.go
│   │   └── errors.go
│   ├── app/                        用例编排：事务边界在这一层
│   │   ├── contract_service.go
│   │   ├── shipment_service.go
│   │   └── ports.go                依赖倒置：定义所需的外部接口
│   ├── adapter/
│   │   ├── grpcin/                 入站：gRPC handler，proto ↔ domain 转换
│   │   ├── grpcout/                出站：调用 fx / inventory / approval 的客户端
│   │   ├── store/                  sqlc 生成 + Repository 实现
│   │   └── kafkain/                事件消费者
│   └── event/                      本服务发布的事件定义
├── db/
│   ├── migrations/                 goose
│   │   ├── 00001_contracts.sql
│   │   └── ...
│   ├── queries/                    sqlc 输入
│   │   ├── contract.sql
│   │   └── ...
│   └── sqlc.yaml
└── go.mod
```

依赖方向：`adapter → app → domain`。`domain` 不 import 任何 adapter 或第三方库（decimal 除外）。

### 7.3 关键代码骨架

**领域层：汇率快照值对象**

```go
// services/export/internal/domain/fxsnapshot.go
package domain

import (
    "time"
    "github.com/shopspring/decimal"
)

// FxSnapshot 是单据签订时刻的汇率，一经写入永不变更。
// 它按值嵌入单据，不引用汇率表 —— 这样实时汇率变化无法影响历史单据。
type FxSnapshot struct {
    BaseCurrency string
    Rate         decimal.Decimal
    QuotedAt     time.Time
    Source       string
}

func NewFxSnapshot(base string, rate decimal.Decimal, quotedAt time.Time, source string) (FxSnapshot, error) {
    if rate.LessThanOrEqual(decimal.Zero) {
        return FxSnapshot{}, ErrInvalidFxRate
    }
    if source == "" {
        return FxSnapshot{}, ErrMissingFxSource
    }
    return FxSnapshot{BaseCurrency: base, Rate: rate, QuotedAt: quotedAt, Source: source}, nil
}

// ToBase 按快照汇率折算本币金额。
func (s FxSnapshot) ToBase(amount decimal.Decimal) decimal.Decimal {
    return amount.Mul(s.Rate).Round(2)
}
```

**领域层：合同状态机**

```go
// services/export/internal/domain/contract.go
package domain

type ContractStatus string

const (
    ContractDraft            ContractStatus = "DRAFT"
    ContractPendingApproval  ContractStatus = "PENDING_APPROVAL"
    ContractPendingSign      ContractStatus = "PENDING_SIGN"
    ContractEffective        ContractStatus = "EFFECTIVE"
    ContractExecuting        ContractStatus = "EXECUTING"
    ContractCompleted        ContractStatus = "COMPLETED"
    ContractCancelled        ContractStatus = "CANCELLED"
)

// 变更版本审批通过后，合同回到 PENDING_SIGN 等客户重新签署（业务确认），
// 因此 EFFECTIVE / EXECUTING 均可回退到 PENDING_SIGN。
var contractTransitions = map[ContractStatus][]ContractStatus{
    ContractDraft:           {ContractPendingApproval, ContractCancelled},
    ContractPendingApproval: {ContractPendingSign, ContractDraft, ContractCancelled},
    ContractPendingSign:     {ContractEffective, ContractCancelled},
    ContractEffective:       {ContractExecuting, ContractPendingSign, ContractCancelled},
    ContractExecuting:       {ContractPendingSign, ContractCompleted},
    ContractCompleted:       {},
    ContractCancelled:       {},
}

type Contract struct {
    ID             int64
    ContractNo     string
    CustomerID     int64
    Status         ContractStatus
    CurrentVersion *ContractVersion
}

func (c *Contract) TransitionTo(next ContractStatus) error {
    for _, allowed := range contractTransitions[c.Status] {
        if allowed == next {
            c.Status = next
            return nil
        }
    }
    return ErrIllegalTransition{From: c.Status, To: next}
}

// Amend 生效后的合同不能改字段，只能派生新版本重新审批。
func (c *Contract) Amend(reason string, base ContractVersion) (*ContractVersion, error) {
    if c.Status != ContractEffective && c.Status != ContractExecuting {
        return nil, ErrAmendNotAllowed
    }
    if reason == "" {
        return nil, ErrMissingChangeReason
    }
    next := base
    next.ID = 0
    next.VersionNo = base.VersionNo + 1
    next.Status = VersionDraft
    next.ChangeReason = reason
    return &next, nil
}
```

**用例层：出货计划确认（含库存锁定 saga）**

```go
// services/export/internal/app/shipment_service.go
package app

func (s *ShipmentService) Confirm(ctx context.Context, planID int64, op Operator) (*ConfirmResult, error) {
    var plan *domain.ShipmentPlan

    // 第一段事务：校验累计出货量并置为 PENDING_STOCK
    err := s.tx.Do(ctx, func(ctx context.Context, r Repos) error {
        var err error
        plan, err = r.Shipment.GetForUpdate(ctx, planID)
        if err != nil {
            return err
        }
        // 行锁住合同明细，避免并发计划同时通过校验
        items, err := r.Contract.LockItems(ctx, plan.ContractID)
        if err != nil {
            return err
        }
        planned, err := r.Shipment.SumPlannedQtyExcluding(ctx, plan.ContractID, planID)
        if err != nil {
            return err
        }
        if err := plan.ValidateAgainstContract(items, planned); err != nil {
            return err  // 累计出货量超过合同数量
        }
        return r.Shipment.UpdateStatus(ctx, planID, domain.PlanPendingStock)
    })
    if err != nil {
        return nil, err
    }

    // 跨服务调用：lock_ref 保证幂等，超时重试安全
    lockResp, err := s.inventory.LockStock(ctx, &inventoryv1.LockStockRequest{
        LockRefType: "SHIPMENT_PLAN",
        LockRefId:   planID,
        Items:       toLockItems(plan),
    })

    // 第二段事务：按锁定结果落地状态并写 outbox
    if err != nil || !lockResp.Success {
        _ = s.tx.Do(ctx, func(ctx context.Context, r Repos) error {
            return r.Shipment.MarkShortage(ctx, planID, lockResp.GetShortages())
        })
        return &ConfirmResult{Status: domain.PlanStockShortage,
            Shortages: lockResp.GetShortages()}, nil
    }

    err = s.tx.Do(ctx, func(ctx context.Context, r Repos) error {
        if err := r.Shipment.UpdateStatus(ctx, planID, domain.PlanConfirmed); err != nil {
            return err
        }
        return r.Outbox.Append(ctx, outbox.Event{
            AggregateType: "shipment_plan",
            AggregateID:   strconv.FormatInt(planID, 10),
            EventType:     "ShipmentPlanConfirmed",
            Payload:       plan.ToEventPayload(),
            TraceID:       op.TraceID,
        })
    })
    return &ConfirmResult{Status: domain.PlanConfirmed}, err
}
```

**仓储层：库存锁定的幂等实现**

```sql
-- services/inventory/db/queries/stock_lock.sql

-- name: TryLockStock :one
-- lock_ref 冲突表示已锁定过，返回既有记录即可，天然幂等。
INSERT INTO stock_locks (lock_ref_type, lock_ref_id, warehouse_id, sku_id, qty, status)
VALUES ($1, $2, $3, $4, $5, 'ACTIVE')
ON CONFLICT (lock_ref_type, lock_ref_id, sku_id) DO UPDATE
    SET qty = stock_locks.qty      -- 不改动，仅为触发 RETURNING
RETURNING *;

-- name: IncreaseLockedQty :exec
UPDATE stocks
SET locked_qty = locked_qty + $3,
    updated_at = now()
WHERE warehouse_id = $1 AND sku_id = $2
  AND on_hand_qty - locked_qty - frozen_qty >= $3;   -- 可用量不足则影响 0 行
```

**Outbox relay**

```go
// pkg/outbox/relay.go
package outbox

func (r *Relay) Run(ctx context.Context) error {
    ticker := time.NewTicker(r.interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            if err := r.publishBatch(ctx); err != nil {
                r.log.Error("outbox publish failed", "err", err)
            }
        }
    }
}

func (r *Relay) publishBatch(ctx context.Context) error {
    events, err := r.store.FetchUnpublished(ctx, r.batchSize)
    if err != nil || len(events) == 0 {
        return err
    }
    for _, e := range events {
        topic := r.topicFor(e.AggregateType)
        // 分区键用聚合 ID，保证同一业务对象的事件有序
        if err := r.producer.Publish(ctx, topic, []byte(e.AggregateID), e.Payload); err != nil {
            _ = r.store.MarkFailed(ctx, e.ID, err.Error())
            continue
        }
        _ = r.store.MarkPublished(ctx, e.ID)
    }
    return nil
}
```

**gRPC 拦截器：操作上下文与错误映射**

```go
// pkg/grpcx/interceptor.go
package grpcx

// UnaryOperatorInterceptor 从 metadata 还原操作人上下文，供审计与数据范围使用。
func UnaryOperatorInterceptor() grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
        handler grpc.UnaryHandler) (any, error) {
        md, _ := metadata.FromIncomingContext(ctx)
        op := Operator{
            EmployeeID: firstInt64(md, "x-employee-id"),
            Name:       first(md, "x-employee-name"),
            IP:         first(md, "x-forwarded-for"),
            TraceID:    first(md, "x-trace-id"),
        }
        return handler(WithOperator(ctx, op), req)
    }
}

// UnaryErrorInterceptor 把领域错误映射成带业务码的 gRPC status，
// 避免调用方靠字符串匹配判断错误类型。
func UnaryErrorInterceptor() grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req any, info *grpc.UnaryServerInfo,
        handler grpc.UnaryHandler) (any, error) {
        resp, err := handler(ctx, req)
        if err == nil {
            return resp, nil
        }
        return nil, apierr.ToStatus(err)
    }
}
```

---

## 8. Docker 部署

### 8.1 基础设施（本地开发）

```yaml
# deploy/docker-compose.infra.yml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: erp
      POSTGRES_PASSWORD: erp_dev_password
      POSTGRES_DB: postgres
    ports: ["5432:5432"]
    volumes:
      - pgdata:/var/lib/postgresql/data
      - ./init-databases.sh:/docker-entrypoint-initdb.d/init.sh:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U erp"]
      interval: 5s
      retries: 10

  kafka:
    image: confluentinc/cp-kafka:7.6.0
    environment:
      KAFKA_NODE_ID: 1
      KAFKA_PROCESS_ROLES: broker,controller
      KAFKA_CONTROLLER_QUORUM_VOTERS: 1@kafka:29093
      KAFKA_LISTENERS: PLAINTEXT://:9092,CONTROLLER://:29093
      KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://kafka:9092
      KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: PLAINTEXT:PLAINTEXT,CONTROLLER:PLAINTEXT
      KAFKA_CONTROLLER_LISTENER_NAMES: CONTROLLER
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
      CLUSTER_ID: erp-local-cluster-0001
    ports: ["9092:9092"]
    volumes: [kafkadata:/var/lib/kafka/data]
    healthcheck:
      test: ["CMD-SHELL", "kafka-broker-api-versions --bootstrap-server localhost:9092"]
      interval: 10s
      retries: 12

  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]

  minio:
    image: minio/minio:latest
    command: server /data --console-address ":9001"
    environment:
      MINIO_ROOT_USER: erp
      MINIO_ROOT_PASSWORD: erp_dev_password
    ports: ["9000:9000", "9001:9001"]
    volumes: [miniodata:/data]
    healthcheck:
      test: ["CMD", "mc", "ready", "local"]
      interval: 10s
      retries: 6

volumes: { pgdata: , kafkadata: , miniodata: }
```

`init-databases.sh` 为每个服务建库和独立账号：

```bash
#!/bin/sh
set -e
for svc in iam masterdata product fx approval export procurement inventory shipping reporting audit; do
  psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" <<SQL
    CREATE DATABASE erp_${svc};
    CREATE USER erp_${svc} WITH PASSWORD 'erp_${svc}_pw';
    GRANT ALL PRIVILEGES ON DATABASE erp_${svc} TO erp_${svc};
SQL
done
# reporting 额外建只读账号，驾驶舱与报表用它
psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d erp_reporting <<SQL
  CREATE USER erp_reporting_ro WITH PASSWORD 'erp_reporting_ro_pw';
  GRANT CONNECT ON DATABASE erp_reporting TO erp_reporting_ro;
  ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO erp_reporting_ro;
SQL
```

### 8.2 服务镜像

所有 Go 服务共用一个多阶段 Dockerfile，构建上下文为仓库根目录：

```dockerfile
# deploy/Dockerfile
ARG SERVICE
FROM golang:1.23-alpine AS build
ARG SERVICE
WORKDIR /src
COPY go.work go.work.sum ./
COPY pkg/ pkg/
COPY gen/ gen/
COPY services/ services/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
      -o /out/app ./services/${SERVICE}/cmd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
```

> 关键：构建上下文是**仓库根目录**，`COPY` 的路径都相对于它。上一版系统的 Dockerfile 写成 `COPY ../erp-common`，越出上下文导致整套镜像构建不了 —— 这里不重复该错误。

`docker-compose.yml` 中每个服务：

```yaml
  export:
    build:
      context: ..
      dockerfile: deploy/Dockerfile
      args: { SERVICE: export }
    environment:
      DB_DSN: postgres://erp_export:erp_export_pw@postgres:5432/erp_export?sslmode=disable
      KAFKA_BROKERS: kafka:9092
      GRPC_PORT: "9006"
      FX_ADDR: fx:9004
      INVENTORY_ADDR: inventory:9008
      APPROVAL_ADDR: approval:9005
    depends_on:
      postgres: { condition: service_healthy }
      kafka:    { condition: service_healthy }
```

### 8.3 迁移执行

迁移不在服务启动时自动跑（并发启动会互相冲突），用独立的一次性容器：

```yaml
  migrate-export:
    build: { context: .., dockerfile: deploy/Dockerfile.migrate }
    command: ["goose", "-dir", "/migrations/export", "postgres", "$DB_DSN", "up"]
    depends_on:
      postgres: { condition: service_healthy }
    restart: "no"
```

---

## 9. 分阶段任务清单

每个阶段都以「可独立验收」为标准，前一阶段验收通过再进下一阶段。

### 阶段 0：工程骨架（约 2 天）—— ✅ 完成（2026-07-27）

- [x] 初始化 `go.work`，各服务独立 module
- [x] `buf` 配置，proto lint 与生成流水线跑通
- [x] `pkg/` 基础库：`money`、`pgx`、`grpcx`、`apierr`、`outbox`、`kafkax`
      （后补 `idempotency`、`livefeed`）
- [x] `deploy/docker-compose.infra.yml`，一条命令起 PostgreSQL + Kafka + Redis + MinIO
- [x] Makefile：`make proto` / `make migrate` / `make up` / `make test`
- [x] CI：`.github/workflows/ci.yml`，含 `scripts/check-tenant-id.sh`

**验收**：`make up` 后基础设施健康；`make proto` 生成物无差异。

### 阶段 1：平台层（约 5 天）—— ✅ 完成（2026-07-28）

- [x] `iam`：员工、部门、角色、权限、数据范围、登录签发 JWT（数据范围 2026-07-28 接上）
- [x] `masterdata`：客户、供应商、业务选项、编码规则（含并发安全的取号）
- [x] `fx`：汇率抓取任务、历史存储、`GetLatestRate`、异常检测
- [x] `approval`：审批定义、提交、审批、我的待办、发 `ApprovalApproved` 事件（2026-07-27）
      —— 2026-07-28 追加：直属上级审批人、金额分档、流程配置界面
- [x] `gateway`：JWT 校验、路由到上述服务、统一错误响应、SSE 推送

**验收**：能登录拿到 token ✅；能配一条合同审批流 ✅；能取到实时汇率 ✅；
**取号并发 100 请求无重复 —— 未压测**（唯一还没验的一条）。

### 阶段 2：主数据（约 3 天）—— 🟡 差供应商界面

- [x] `product`：分类、产品、SKU、单位、附件（2026-07-27 完成）
  - 已推迟：单位换算 `uom_conversions`、包装 `packagings`、条码 `product_barcodes` —— 阶段 4 的库存与船期才会用到，届时增量迁移补上
  - 附件走**预签名直传**：服务端签发 URL，浏览器直接 PUT 到对象存储，文件字节不经过网关；库里只存 object key
- [x] 明确不提供 Delete，只有 Deactivate
- [x] 前端：产品管理、客户管理
- [ ] 前端：**供应商管理页面**（后端 `masterdata:supplier:*` 已就绪，界面没做）

**验收**：产品可维护 ✅；被引用后停用而非删除 ✅；前端主数据录入 —— 供应商这一条还缺。

### 阶段 3：出口业务核心（约 10 天，最大的一块）—— 🟡 进行中

- [x] `export` 服务骨架 + 数据库迁移（2026-07-27）
- [x] 报价单：创建、发送、接受/拒绝、汇率快照（2026-07-27；复制功能推迟到合同落地后一起做）
- [x] 合同：由报价生成、版本表、提交审批、签署、生效（2026-07-28）
- [x] outbox relay 投递 Kafka；合同生效发 `ContractEffective`（2026-07-28）
- [x] 消费 `erp.approval.task.v1` 推进合同状态——系统第一个 Kafka 消费者（2026-07-28）
- [x] 合同变更：新版本 + 重新审批 + 旧版本 SUPERSEDED（2026-07-28）
- [x] 前端：报价单、合同、合同审批、我的待办（2026-07-28）
- [x] 合同附件上传（预签名直传，DRAFT / SIGNED / OTHER）（2026-07-28）
- [x] 数据范围 + 审批人可读并集 + 负责人读写规则（2026-07-28，见 §4.6/§4.7）
- [ ] 合同生效时生成收汇计划（收汇表尚未建，随阶段 6 一起做）
- [x] **负责人转移**（2026-07-28，见 §4.8）——「生成合同必须本人」之后这是必需品
- [x] **客户签回·人工路径**（2026-07-28）：签署必须有该版本签回件，来源不可伪造，生效后凭证不可删
- [ ] **客户签回·平台路径**：电子签 webhook + 免登录签署链接（见 §5.6.1），随阶段 6 做
- [ ] 报价单 PDF、真实邮件发送、免登录令牌端点（随阶段 6 通知服务一起做）

**阶段 3 挂起的两件事（2026-07-28 决定推迟）**

| | 为什么推迟 | 什么时候做 |
|---|---|---|
| **报价单 PDF** | 卡在「照着现有模板做」——格式要贴业务在用的那份，凭空设计等于白做。做完也只是省销售一道排版，不打通新链路 | 拿到现行模板后随时可做，不依赖任何外部服务 |
| **真实邮件发送** | 需要 SMTP、通知服务、免登录令牌端点，是一整块 | 阶段 6 通知服务 |

两者可拆：PDF 不依赖邮件，先有 PDF 的话，做邮件时它已经是现成附件。

现状必须说清楚：「发送」按钮**只改状态**，系统不发邮件、不生成文件，
销售仍要自己在系统外排一份发出去。**同一份数据录两遍**，
两边对不上时以哪份为准是没有答案的——这是已知缺口，不是设计。

**验收**：报价 → 合同 → 审批 → 签署 → 生效全链路可走通；生效后改字段被拒绝；变更走新版本；`erp.export.contract.v1` 能收到 `ContractEffective`。

**验收结果（2026-07-28，本地实测）**

- CT-202607-0004：QT-20260728-0001（已接受）→ 合同 → 李娜/管理员两级审批 →
  Kafka 推到「待签署」→ 签署生效 → `ContractEffective` v1 落 topic。
- 同一合同发起变更（1200→1500 件、交期顺延）：新建 v2 草稿期间合同仍是「已生效」、
  `current_version_id` 停在 v1；**驳回后合同回到「已生效」而不是草稿**；
  重新审批通过并签署后 v2 生效、v1 置 SUPERSEDED、`ContractEffective` v2 落 topic。
- 直连数据库 `UPDATE contract_versions` / `UPDATE contract_items` / `DELETE contract_items`
  三条都被触发器拒绝。
- 消费组 `export.approval.v1` 启动即跳过 4 条历史压测事件，未卡住；
  outbox 3 条全部 published，attempts = 0。

### 阶段 4：仓储与采购（约 8 天）—— 🟡 进行中

> **2026-07-28 设计更正：采购不是由合同触发的。**
>
> 第一版把合同数量直接当成采购数量，完全没看库存——
> 合同要 1500 件、仓库躺着 2000 件，照样建一条 1500 件的采购需求，
> 买手照着这张单子会去买公司已经拥有的东西。
>
> 这是**建造顺序**造成的：先做了 procurement，而 inventory 还不存在，
> 于是毛需求成了唯一能算出来的数。
>
> 正确的链路是：
>
> ```
> 合同生效 → 占用库存 → 够：不采购
>                     → 不够：缺口才是采购需求
> ```
>
> 关键在于**占用而不是查询**。同一天签的两份合同各要 1500、仓库有 2000，
> 只查询的话两份都读到「可用 2000」、都认为不用采购，实际缺 1000 无人知晓；
> 占用则第一份拿走 1500、第二份只看到 500。并发下这个差别是决定性的。
>
> 合同和采购需求之间的**追溯**关系保留（`contract_id / required_date`），
> 只是它变成来源标注，不再是触发条件——买手要知道这批货是给谁的、什么时候要。

- [x] `inventory` 服务：仓库、两层库存（预留/锁定）、入库、流水（2026-07-28，端口 9008）
- [x] `inventory`：出库（预留转锁定 → 确认出库 → 扣在库）（2026-07-29，见 §5.6.11）
- [ ] `inventory`：库位、盘点
- [x] `procurement` 服务骨架 + 数据库（2026-07-28，端口 9007）
- [x] ~~消费 `ContractEffective` 生成采购需求~~ **改为消费 `StockAllocated` 生成缺口需求**（2026-07-28）
- [x] 前端：采购需求页（2026-07-28，`/requirements`）
- [x] 前端：库存页 + 出入库流水（2026-07-28，`/stocks`）
- [x] 采购需求可自行发起，不依赖合同（2026-07-28，见 §5.6.10）
- [x] **出库时校验预留数量**——缺口未补不得出库（2026-07-29）
- [x] 到货自动补预留，按交期最早优先；缺口归零则关闭采购需求（2026-07-29）
- [x] 合同改版释放旧版本预留（已锁定/已出库的留给人）（2026-07-29）
- [x] 前端：出库页（待出库 + 出库单）（2026-07-29，`/outbounds`）
- [x] `procurement`：采购单、需求合并、审批、分批收货（2026-07-29，见 §5.7.1）
- [ ] `procurement`：付款、供应商对账
- [x] 采购收货 → `PurchaseReceived` 事件 → inventory 入库（2026-07-29）
- [x] 到货自动关闭它覆盖的采购需求（含手工需求）（2026-07-29）
- [x] 采购需求实时推送（租户广播通道）（2026-07-29，见 §5.7.2）
- [x] 前端：采购单页（2026-07-29，`/purchase-orders`）
- [x] 采购需求：去下单 / 详情（相关采购单）/ 重新打开（2026-07-29，见 §5.7.3）
- [x] 出库回写合同发货进度，超发预警（2026-07-29，见 §5.6.12）
- [x] 角色：收回销售的采购权限，新建「物流管理」（2026-07-29）
- [x] 前端：库存查询、出入库、采购单、收货（2026-07-29，页面均已落地）

**验收**：合同生效后采购需求自动出现；收货后库存增加且有流水；`LockStock` 重复调用不重复锁定。

### 阶段 5：出货计划与船期（约 8 天）—— 🟡 出运单已落地

> 业务确认货直接上船、没有仓库这一环之后，**人工录入的出运单先做了**（§5.6.14），
> 它不依赖出货计划和库存锁定。`shipping` 服务那一套（订舱、节点、费用）仍未做。

- [x] **出运单**：拼柜多合同、确认开船写进度台账、超发拦截、作废回滚（§5.6.14）
- [x] 合同详情显示船期（船名 / 提单号 / 目的港 / ETD·ETA）
- [ ] `export`：出货计划、累计出货量校验（行锁）、库存锁定 saga
- [ ] `inventory`：消费 `ShipmentPlanConfirmed` 创建出库任务
- [ ] `shipping`：船期订舱、集装箱、运输节点、费用、异常
- [ ] 出库完成 → 回写 `shipped_qty` → 船期更新待装运
- [ ] 前端：出货计划

**验收**：出货计划确认时库存不足能返回缺口；累计出货量超合同被拒绝；取消计划自动释放锁定。
出运单部分的验收已通过，见 §5.6.14 末尾。

### 阶段 6：信用证、单证、退税、收汇（约 8 天）

> 本阶段同时落地**通知服务**（设计见 §5.12），它承载：
> 报价单发送到客户邮箱 + 客户点击链接答复（见 5.6.1）、
> 账号邀请链接（见「账号开通与初始密码」）、审批到达提醒，
> 以及**业务邮件与群发**六条需求（2026-07-29 业务确认）。

- [ ] 信用证：创建校验（仅 L/C 付款方式、金额覆盖）、修改件、审证结果
- [ ] 单证制作：7 类单证、版本、负责人、审核
- [ ] 退税：申报、跟踪、到账
- [x] **收汇：银行流水登记、拆分核销、手续费容差、冲销（append-only）**（§5.6.15）
- [ ] 收汇计划（按合同拆首付/尾款）、到账汇率快照、涉外收入申报
- [ ] PSP webhook 自动核销、银行对账单 CSV 导入（§5.6.15 末尾，接口位置已留）
- [ ] 前端对应页面

**通知服务（§5.12）**

- [ ] 发送服务商适配（SES / SendGrid），独立子域 + SPF/DKIM/DMARC
- [ ] `email_messages` 状态机 + 分类重试 + `message_key` 标签
- [ ] 投递 worker：`FOR UPDATE SKIP LOCKED` 取批 + 速率控制（§5.12.2.1，不走 Kafka）
- [ ] `SEND_UNKNOWN`：超时反查事件、判定窗口、按邮件类型选边（§5.12.3.1）
- [ ] 「发送失败待处理」页面（改地址重发 / 放弃 / 转人工）
- [ ] 一人一封发送（禁 Cc/Bcc），变量渲染 + 发送前预览
- [ ] 签名模板：公司级 + 个人级，支持变量
- [ ] 打开 / 点击追踪 + 代理预取识别，退信 webhook 回写
- [ ] `mail` 数据范围（SELF / DEPT / ALL）+ 查看审计
- [ ] 退订黑名单（租户级），事务性邮件与营销邮件分队列
- [ ] 地址可达性：录入时 MX 检查；退信 webhook 回写 `customer_contacts.email_status`；
      `INVALID` 跳过并提示更新（§5.12.9）

**验收**：非 L/C 合同无法创建信用证；已确认收汇无法删除只能冲销；数据库层 DELETE 权限已回收；
群发时每封 `To:` 只有一个地址；变量取不到值的收件人不发出、进人工队列；
断在 `SENDING` 的记录重启后不会重复投递。

### 阶段 7：读模型与驾驶舱（约 6 天）

- [ ] `reporting`：消费全部业务事件，维护 `contract_progress`、`risk_alerts`、`daily_metrics`
- [ ] 消费幂等（`processed_events`）
- [ ] 只读数据库账号
- [ ] `audit`：消费 `erp.audit.operation.v1`，分区表落库
- [ ] 前端：管理驾驶舱、合同执行进度、报表中心

**验收**：合同执行进度能实时反映各环节；驾驶舱只读账号无法写入；操作日志无法编辑删除。

### 阶段 8：加固（约 5 天）

- [ ] OpenTelemetry 全链路追踪
- [ ] 集成测试：`testcontainers-go` 覆盖 6 节的四条关键流程
- [ ] 压测取号、库存锁定的并发正确性
- [ ] 数据库权限收口脚本（REVOKE DELETE 等）
- [ ] 生产 compose / K8s 清单

### 阶段 9：需求单解析与产品召回（约 10 天）—— 设计见 §5.6.16

> **与阶段 1–8 相互独立**，随时可以插队做。但内部顺序不能颠倒：
> 9.1 是后面所有步骤的地基，先做完它，AI 那部分才有靶子可打。

**9.1 属性模板与产品结构化（不含 AI）** —— 🟡 后端已落地（2026-07-30）

- [x] `attribute_templates` / `attribute_defs`：租户 × 品类的属性定义，含
      `data_type`、`unit`、`enum_values`、`match_strategy`、`tolerance_pct`、`level`
- [x] 沿品类物化路径继承解析，子类可加字段或收紧公差；排序按「祖先在前、本级在后」
- [x] `products.attributes`（新增列）与 `skus.attributes` 按模板校验写入；GIN 索引
- [x] `skus.attr_signature` + 偏索引：同一产品下重复规格插不进去
- [x] 多路召回并集：结构化（EXACT / TOLERANCE）+ `pg_trgm` 全文，低分只排后面
- [x] **第 0 层跑通**：无模板、无属性值时全文照样出候选
- [ ] 预置行业模板库（`system_attribute_templates` + 复制式采用），做 SaaS 时补
- [x] 前端：属性模板编辑器（继承字段只读展示）、产品/规格属性表单按模板生成
- [ ] 前端：产品列表的结构化筛选（召回接口已就绪，界面未做）
- [ ] Excel / CSV 需求单导入 —— 格式固定的客户到这一步就够用了

**9.1 验证（2026-07-30，真实数据）**

```
继承      钢材(材质/工艺) → 冷轧板卷(厚/宽/表面)   解析出 5 条，前 2 条标来源为父品类
排序      父子模板 sort_order 都从 0 起            修正为按祖先深度优先，否则交错不可读
校验      材质填 Q345                              → PD_ATTR_NOT_IN_ENUM
          缺必填材质                                → PD_ATTR_REQUIRED
          厚度填「厚一点」                          → PD_ATTR_NOT_NUMBER
去重      把 SKU4 改成与 SKU2 同规格                → PD_SKU_SPEC_DUPLICATE
召回      3.0*1250 酸洗                            → 结构化命中 SKU-01；另两条由全文召回排后
公差      厚度 3.02（±3% = 2.91~3.09）             → 仍命中 3.0
          厚度 3.20                                 → 结构化 0 条
第 0 层    纺织品无模板                              → 全文仍返回候选
通用性    同一套表换成面料/克重/颜色/尺码            → 代码一行未改，ATTR 命中
```

**9.1b 模板反推（可选，省人力）**

- [ ] 拿存量 `spec` 自由文本喂 LLM，提议属性字段与取值范围，人审后落成模板

**9.2 解析管线**

- [ ] `rfq_documents` / `rfq_lines` / `rfq_line_candidates` / `rfq_line_decisions`
- [ ] 上传 → outbox → Kafka → 解析 worker（异步，同 §5.12.2.1）
- [ ] ① 文档解析服务适配；② LLM 抽行项，JSON Schema 强约束
- [ ] 解析失败可重试；同一文档重复解析不产生两套行项（幂等）

**9.3 多路召回（product 服务）**

- [ ] 五路取并集：结构化属性 / 全文 / 向量（`pgvector`）/ 别名 / 历史成交
- [ ] 结构化那一路**读属性模板生成查询**，钢材与日用品走同一套代码
- [ ] 每条候选带 `recall_source` 与 `reason`，审核时看得见为什么被召回
- [ ] 低分不砍，只排后面；每行候选给到 20 条量级
- [ ] `product_aliases`，并从 `MANUAL_SEARCH` 样本提炼别名

**9.4 审核界面**

- [ ] 左原文右解析，按 `source_bbox` 高亮
- [ ] 逐行：确认 / 换候选 / 自己搜 / 标记「库里没有」
- [ ] 置信度分档呈现（高自动选中 / 中预选 / 低留空）
- [ ] 审核通过 → 生成报价单

**验收**：解析结果不能不经审核变成报价单；每一行都能定位回原文的页与位置；
业务员绕过候选自己搜时记下 `picked_from = MANUAL_SEARCH`，
**召回率可以从这张表算出来**而不是靠感觉。

---

### 阶段 10：邮件服务（约 6 天）—— 🟡 除真实发信外全部完成（2026-07-30）

> 设计见 §5.12。**唯一没做的是真的把信发出去**——那需要先选服务商、验证域名、
> 申请解除沙箱，是三件账号层面的事，不是代码。发送走 `Provider` 接口，
> 现在接的是 `dev` 适配器：记录「本该发什么」而不真发，并用魔法域名
> （`@bounce.test` / `@defer.test` / `@timeout.test`）按需触发三条失败路径。

**10.1 服务与投递**

- [x] `mail` 服务（端口 9010）、`erp_mail` 库、五张表
- [x] 渲染引擎：`{{ }}` 定界，取不到值**报出来而不留空**；中文名不切姓氏（11 个单测）
- [x] 一人一封落库 → worker `FOR UPDATE SKIP LOCKED` 认领 → 逐封调用
- [x] 认领时先提交 `SENDING` 再调服务商（不变量 17b）
- [x] 四种结果分类（Accepted / Retryable / Permanent / Unknown）与退避 `1m→5m→30m→2h→6h`
- [x] 结果未知按邮件类型选边：事务性重试，营销停下等人（§5.12.3.1）
- [x] 硬退自动写入拒收名单；拒收地址不再进队列
- [x] 人工队列：重发（可改地址）/ 放弃（必须写原因）
- [x] 数据范围：列表在 SQL 过滤，单封读取与两个人工动作走 `mayRead`（不变量 34）
- [x] gRPC + 网关 14 个接口 + 三个权限（读 / 写 / 维护拒收名单）
- [x] 前端：撰写（联系人多选 + 变量插入 + 预览）、群发进度、待处理、拒收名单、签名模板
- [x] 三语文案（中 / 英 / 西）
- [ ] **真实服务商适配器**（`ses.go` 约 80 行）—— 等选定服务商 + DNS + 解除沙箱
- [ ] 打开 / 点击追踪像素与代理识别；退信 webhook；退订链接
- [ ] 事务性邮件触发（报价单、账号邀请）经 outbox → Kafka 进来
- [ ] 联系人 `email_status` 落地（§5.12.9 已设计，表未改）

**10.1 验证（2026-07-30，真实数据）**

```
群发      7 个收件人，含 3 个魔法域名与 1 个无姓名联系人
          → 6 入队，1 直接进待处理（取不到 contact_first_name，没发）
分类      @bounce.test  → HARD_BOUNCED，并自动加入拒收名单
          @defer.test   → 退回 QUEUED，1 分钟后 try=2（灰名单靠这个过）
          @timeout.test → SEND_UNKNOWN（营销不自动重发，交给人）
隔离      5 封群发 = 5 次独立 Provider 调用，各自的收件人与各自的主题
          日志逐条可见，接口里根本没有收件人列表这种参数
拒收      再次群发含已硬退地址 → 被跳过，并明确告知调用方跳了谁、为什么
重发      改成正确地址重发 → 新地址移出拒收名单，发送成功
放弃      不写原因 → NT_ABANDON_REASON_REQUIRED；写了原因 → 离开待办队列
权限      张三(SELF) 只看到自己那 1 封；李娜(ALL) 看到全部 9 封
越权      张三读管理员的邮件 / 放弃管理员的邮件 → 一律「邮件记录不存在」
          张三写拒收名单 → AUTH_PERMISSION_DENIED
预览      签名含 {{my_title}} 而管理员没填职位 → 报出缺失变量，发送按钮置灰
          换成只用 {{my_name}} 的签名 → 预览干净，发送放行，5 封全部送出
```

**过程中发现并修掉的三个问题**

| 问题 | 后果 | 修法 |
|---|---|---|
| 进度分桶正向枚举，漏了 `SEND_UNKNOWN` | 4+1+1≠7，**最需要人管的那条恰好不出现在汇总里** | 改成反向定义 `failed`，让三桶恒等于 total（不变量 35） |
| `attention_only` 含 `FAILED` | `FAILED` 正是「放弃」写入的状态，队列只增不减，红点永远消不掉 | 从筛选里去掉（不变量 36） |
| 插入变量后用 `requestAnimationFrame` 归位光标 | 帧回调早于 Vue 写 DOM，光标被重置到 0，**接着打的字全跑到开头** | 改用 `nextTick` |

---

## 10. 不变量清单

这份清单来自需求文档中所有「不能……」的表述，每条都必须有对应的强制手段。实现时逐条对照验收。

| # | 不变量 | 出处 | 强制手段 |
|---|---|---|---|
| 1 | 驾驶舱不能直接更改库存、合同或付款 | 1. | `reporting` 只暴露 Query 方法；只读数据库账号 |
| 2 | 报价单不能扣库存、不能创建采购单 | 2.1 | `export` 报价用例不调用 inventory/procurement 写接口 |
| 3 | 合同审批或生效后关键字段不能改，只能创建变更版本 | 2.2 | `contract_versions` 触发器拒绝对 APPROVED 版本 UPDATE |
| 4 | 用户只能看到分配给本人或本人角色的审批任务 | 2.3 | `approval_tasks.assignee_id` 过滤 + 数据范围 |
| 4b | 离职员工立即失去访问权，且系统不会失去管理员 | iam | 权限查询联 `employees.status='ACTIVE'`（已签发的 token 随之失效）；不能停用自己，也不能停用最后一个持 `iam:employee:write` 的人；停用可撤销（`ActivateEmployee`） |
| 5 | 合同执行进度不重复保存采购、库存或船期明细 | 2.4 | `contract_progress` 只存汇总数值与状态，无明细表 |
| 6 | 累计出货量不能超过合同数量 | 2.5 | 确认时对 `contract_items` 加 `FOR UPDATE` 行锁后校验 |
| 7 | 仅 L/C 付款方式才允许创建信用证 | 2.6 | `CreateLC` 用例校验合同 payment_method |
| 8 | 信用证金额需覆盖合同 | 2.6 | `amount * (1+tolerance) >= contract.total_amount` |
| 9 | 已确认收汇不能删除，只能冲销 | 2.9 | `REVOKE DELETE ON receipts`；冲销为反向记录 |
| 10 | 合同取消或计划取消自动释放库存锁定 | 4. | 取消用例调用 `inventory.ReleaseLock(lock_ref)` |
| 11 | 产品被引用后不能删除，只能停用 | 6. | `product` 服务不提供 Delete 方法 |
| 12 | 实时汇率变化后历史单据不能跟着变 | 8. | 汇率快照值对象内嵌，单据无外键指向汇率表 |
| 13 | 报表只能读取，不能反向修改原始单据 | 9. | 只读数据库账号 + 只有 Query 类 gRPC 方法 |
| 14 | 操作日志不允许编辑或删除 | 三. | `GRANT INSERT, SELECT` only |
| 15 | 每类数据只有一个负责模块 | 三. | 每服务独立 database 与账号，跨库无权限 |
| 16 | 群发时收件人不能看到还有其他收件人 | 5.12.4 | 一人一封；发送适配器强制 `To:` 单地址、`Cc`/`Bcc` 为空 |
| 17 | 重试不能把同一封信发两遍 | 5.12.3.1 | `message_key` 发送前落库并随请求带标签；`SENDING` / `SEND_UNKNOWN` 先反查事件，查不到按邮件类型选边，不盲目重发 |
| 17b | 落库必须早于调用服务商 | 5.12.3.1 | 先提交 `SENDING` 再调用；反序会在崩溃时留下无记录的静默丢失 |
| 18 | 变量渲染不出来的邮件不能发出去 | 5.12.5 | 渲染失败直接进 `NEEDS_ATTENTION`，不进发送队列 |
| 19 | 「打开」不能当作已读事实用于业务判断 | 5.12.7 | UI 措辞为「检测到打开」；代理预取默认排除；报表以点击为主 |
| 20 | 营销邮件拥堵不能影响事务性邮件 | 5.12.10 | 两者分队列、分子域、分速率配额 |
| 20b | 硬退过的地址不能再进发送队列 | 5.12.9 | `customer_contacts.email_status = 'INVALID'` 的收件人跳过并提示更新 |
| 21 | 出运数量不能超过合同未发数量 | 5.6.14 | `ConfirmSailing` 按产品比对 `ShipmentProgressOf`，超则 `EX_SHIP_EXCEEDS_CONTRACT` |
| 22 | 已开船的出运单不能修改 | 5.6.14 | `UpdateShipmentHeader` 带 `status = 'DRAFT'` 条件；要更正只能作废重开 |
| 23 | 作废出运单必须把货从进度台账里扣回去 | 5.6.14 | 同一事务里 `RemoveShipmentFromLedger` + 置 `CANCELLED`；单据保留 |
| 24 | 银行流水金额永不因核销而改变 | 5.6.15 | `bank_transactions.amount` 只由录入写入；核销只往 `receipt_allocations` 加行 |
| 25 | 核销不能删除，只能冲销 | 5.6.15 | 冲销 = 负数行 + `reversal_of`；`reversal_of` 上 partial unique 保证只能冲一次 |
| 26 | 跨币种不能核销 | 5.6.15 | 合同币种 ≠ 流水币种即拒（`EX_ALLOC_CURRENCY_MISMATCH`）——汇兑损益无处安放 |
| 27 | 分配总额不能超过流水金额 | 5.6.15 | 在 `FOR UPDATE` 锁内读余额后校验，防并发双分配 |
| 28 | 每一笔流水都必须有归宿 | 5.6.15 | 只有 `ALLOCATED` / `IRRELEVANT` 离开队列；`IRRELEVANT` 强制选类别 |
| 29 | 需求单解析结果不能不经审核变成报价单 | 5.6.16 | `rfq_documents` 必须走到 `CONFIRMED`；解析只写 `rfq_lines`，不写报价单 |
| 30 | 每一行解析结果都要能定位回原文 | 5.6.16 | `rfq_lines.source_page` + `source_bbox` 必填；缺了则人工审核等于盲信 AI |
| 31 | 候选召回只排序不过滤 | 5.6.16 | 多路取并集，低分排后面不删除；漏召的代价远大于多看几行 |
| 32 | 品类属性不能写死在代码里 | 5.6.16 | `attribute_defs` 是数据；加一个行业是加一条模板，召回引擎不改 |
| 33 | 没有属性模板时召回仍须可用 | 5.6.16 | 第 0 层退回全文 + 向量；结构化是增强项，不是前置条件 |
| 34 | 单封读取要和列表受同一数据范围约束 | 5.12.6 | 列表在 SQL 里过滤，`GetMessage` / `GetCampaign` / 重发 / 放弃在应用层调 `mayRead`；越权一律返回「不存在」，不泄露该邮件是否存在 |
| 35 | 群发进度的分桶必须刚好覆盖全部状态 | 5.12.3 | `failed` 用 `NOT IN (已发出, 排队中)` 反向定义，而非正向枚举；保证 `sent + pending + failed = total` 恒成立 |
| 36 | 人工处理过的邮件必须离开待办队列 | 5.12.3 | `attention_only` 不含 `FAILED`——那正是「放弃」写入的状态；含它会让队列只增不减 |
| 37 | HTML 正文必须同时带纯文本版 | 5.12.10 | `body_text` 与 `body` 同时落库、同时发出；只发 HTML 是可测量的垃圾邮件特征 |
| 38 | 变量替换的值在 HTML 里必须转义 | 5.12.10 | `RenderAs` 按 format 转义**值**而不转义模板；客户名叫 `Smith & Sons <Trading>` 不能打烂版式 |
| 39 | 外发 HTML 一律服务端白名单清洗 | 5.12.10 | `bluemonday` 在写入时清洗（不是读出时）——存下来的值同时要回显到界面，一次清洗堵两个洞 |
| 40 | 内嵌图片的 URL 必须永久有效 | 5.12.10 | 用不过期的 token 路由，不用预签名 URL；后者过期后已发出的每封信都会变裂图 |
| 41 | 附件大小以存储实测为准，不信客户端申报 | 5.12.10 | 直传绕过了服务，`Stat` 读回真实大小再判上限；按申报值判的上限不是上限 |
| 42 | 外发信的 Message-ID 生成规则不可更改 | 5.12.11 | `<{message_key}@发信域名>`；换规则会让此前发出的信永久无法与回信串联 |
| 43 | 入站邮件按 Message-ID 幂等 | 5.12.11 | `UNIQUE (tenant_id, message_id)`；服务商保证的是至少一次，重复推送是常态 |
| 44 | 草稿不进任何他人可见的查询 | 5.12.12 | 所有权写在 SQL `WHERE` 里；数据范围不放宽草稿，upsert 也校验 owner |

---

## 附录 A：与旧系统的关系

新仓库不复用旧代码。以下内容可作参考，但需重新验证：

- 旧 `infrastructure/docker/init-db.sql` 的 41 张表是旧系统唯一真正执行过的 schema，字段命名可参考
- 旧前端 Vue 3 页面（尤其销售订单）交互设计可参考，但接口按本文档重新定义
- 旧系统 93 个迁移脚本已转为合法 PostgreSQL，但其中 31 处引用不存在的表，**设计未闭环，不建议参考**

## 附录 B：已确认的业务决策

以下决策已由业务方确认，正文相应位置均已落实：

| # | 问题 | 决策 | 落点 |
|---|---|---|---|
| 1 | 本位币 | **USD** | 公共约定；所有 `base_amount` 折 USD |
| 2 | 合同变更是否重新签署 | **需要**，状态回退 `PENDING_SIGN` | 合同状态机（7.3）；5.6.2 不变量 |
| 3 | 出货计划是否跨仓库 | **先单仓库**，schema 预留多仓 | 5.8 说明 |
| 4 | 退税申报周期 | **按月**汇总申报 | `tax_refund_declarations`（5.6.6） |
| 5 | 信用证不符点改单 | **需要独立审批** | `lc_amendments.status`；biz_type `LC_AMENDMENT` |
| 6 | 数据范围自定义部门 | 枚举预留，**暂不实现** | `role_data_scopes` 注释 |
| 7 | 部署形态 | **预留多租户**（当前单租户运行） | 公共约定「多租户约定」 |
| 8 | 附件存储 | **MinIO（S3 兼容）**，云端换 S3/OSS 只改配置 | 公共约定；8.1 infra compose |
| 9 | 汇率源 | **仅免费 API（frankfurter.app）实时抓取**（2026-07-27 变更：取消手工录入，异常检测移至抓取时） | 5.4 说明 |
| 10 | 变更重签期间存量业务 | **继续执行，冲突预警**，不自动取消 | 5.6.2 不变量 |
| 11 | 邮件群发形态（2026-07-29） | **一人一封**，收件人互不可见；变量称呼；失败重试后转人工；签名模板；上级可查；打开检测（弱信号） | §5.12 全节；不变量 16–20 |
| 12 | 需求单解析（2026-07-30） | **召回优先**——宁可多给候选由人工删，不可漏；AI 与人工两条路并存，员工自选；绝不自动通过 | §5.6.16；阶段 9；不变量 29–33 |
| 13 | 数据出境（2026-07-30） | **公司主体在美国**，可直接调用海外 LLM / 文档解析 API，无需额外手续 | §5.6.16 选型 |

尚未决策、但不阻塞开发的事项：

- **云厂商**（阿里云 / AWS / 其他）：只影响阶段 8 的部署清单，届时确认
- **审批通知渠道**：站内「我的待办」已满足文档要求；邮件 / 企业微信提醒作为后续增强，事件总线已具备接入点
- **邮件发送服务商**（倾向 SES；SendGrid / 阿里云邮件推送同样满足要求）：随阶段 6 一起选。
  **不可选项是拿业务员的企业邮箱账号当群发通道**——注意不是「不能用 SMTP 协议」，
  服务商自己也提供 SMTP 端点。理由与对比见 §5.12.2

### 账号开通与初始密码（2026-07-27 记录，分两步落地）

现状：管理员在员工页设置初始密码，**口头或即时通讯告知本人**。系统没有邮件通道，
所以初始密码必然经过一次「人转述」，这是当前最薄弱的一环。

**不采用**：邮件发送明文密码。邮箱会被归档、转发、被他人翻阅，密码跟着长期留存。

**第一步（阶段 3 之后，独立小任务，约半天）——首次登录强制改密**

- `users` 加 `must_change_password BOOLEAN NOT NULL DEFAULT false`
- 管理员设的初始密码（`CreateEmployee` 带账号、`OpenAccount`、`ResetPassword`）一律置 true
- 登录响应带 `must_change_password`；前端在该标记为真时，除「修改密码」外不放行任何页面
- 自助 `ChangePassword` 成功后清零

价值：初始密码即使被旁人听见，有效窗口也只有一次登录。

**第二步（阶段 6，与邮件服务一起）——一次性邀请链接**

- 新表 `account_invites (token_hash, employee_id, expires_at, used_at)`，
  **只存 token 的哈希**，与密码同理：库被读走也无法直接使用
- **发送渠道（2026-07-27 业务确认）：发到员工的个人邮箱**（`employees.email`）。
  这意味着走邀请制开户时，邮箱从选填变成**必填**：没有邮箱就没有送达地址，
  此时退回到「管理员设初始密码 + 首次登录强制改」这条路径
- 建员工时不设密码，生成一次性 token（24 小时过期），通过邮件发链接
- 员工打开链接自行设置密码，token 立即作废；过期或已用的 token 返回统一的失败提示，
  不透露该员工是否存在
- 邮件通道复用阶段 6 的通知服务，事件驱动（`AccountInvited` 事件），
  避免 iam 直接依赖 SMTP：iam 只往 outbox 写事件，通知服务消费后发信。
  这样 SMTP 抖动不会让「建员工」这个动作失败，重试也由消费端负责
- 邮件正文只放链接，不放密码；链接被使用或过期后立即失效

---

## 附录：数据范围按职能划，不按单据负责人划（2026-07-29 确认）

| 模块 | 数据范围 | 理由 |
|---|---|---|
| 报价单 / 合同 | **有**（SELF / DEPT / ALL ∪ 我要审批的） | 销售是竞争性岗位——提成、客户关系 |
| 采购需求 / 采购单 | **没有**，看全量 | 跨合同合并下单是采购的核心价值 |
| 出库 / 库存 | **没有**，看全量 | 仓管要看到所有待发货才能干活 |

给采购加数据范围会**废掉这个岗位一半的功能**：三份合同各缺 500 要能并成
一张 1500 的单，按「自己的」过滤就并不起来。仓库同理。

**发现过一个后门（2026-07-29 已修）。** 采购需求页显示合同号和客户名，所以一个
数据范围是 SELF 的销售，只要被授予 `procurement:requirement:read`，就能从采购页
看到别人的客户——绕过了数据范围，不是穿过它。「销售只读」「销售主管」确实被
授予了这个权限（通过角色页手工加的，不是迁移脚本给的）。

修的是**权限**，不是给采购加范围（`00011_logistics_role.sql`）：

- 销售保留 `inventory:stock:read`——要看可用量才能判断能不能接单，
  而库存页不含客户信息，安全
- 销售收回 `inventory:stock:write`、`procurement:*`
- 新建 **物流管理（LOGISTICS）** 角色：入库、出库、采购收货、库存查询

物流管理拿 `procurement:order:read` 但**不给 `:write`**——
仓库核对到货的是哪张单，不决定买什么。数据范围给 `export ALL`：
仓库按销售负责人过滤就没法干活了。

治不了的情况：一个人同时是销售和采购（小公司常见）。那时客户名照样看得到，
只能靠「隐藏客户名」这类开关，属于后话。

`procurement:order:*` 从一开始就和 `procurement:requirement:*` 分开：
看要买什么、和把公司的钱承诺给供应商，是两件事，通常也是两个人。

---

## 附录：这套系统和 SAP / Oracle 差在哪，以及补的顺序（2026-07-29）

### 它现在还不是 ERP

严格说这是**出口贸易业务管理系统 / 供应链系统**。ERP 的 R 是 Resource
**Planning**，而 Planning 的完整含义包含钱的计划。

| | 已建 | 说明 |
|---|---|---|
| 销售与分销 | ✅ | 报价 · 合同 · 审批 · 版本冻结 |
| 采购管理 | ✅ | 需求 · 采购单 · 审批 · 分批收货 |
| 仓储管理 | ◐ | 有两层占用和流水；缺库位、批次、盘点 |
| 物料需求计划 | ◐ | 只有单级净需求；无 BOM 展开、提前期倒排、批量规则 |
| **总账 / 会计凭证** | ❌ | **SAP 的心脏，这里一格没有** |
| **成本核算** | ❌ | 库存表里没有任何一列跟钱有关 |
| 应收 / 应付 | ❌ | |
| 期末结账 | ❌ | 计提 · 结转 · 折旧 |
| 生产 / 质量 / 设备 / 人力 / 项目 | ❌ | 做外贸不一定需要 |

### 最大的区别：SAP 记的是钱，这里记的是货

SAP 每一笔业务动作自动生成会计凭证：

```
采购收货 3000 件  →  借 原材料 37,500   贷 应付暂估 37,500
销售出库 1500 件  →  借 主营业务成本     贷 库存商品
```

这套系统只动数量。所以「出库那 1500 件成本是多少」——**答不出来**。
这不是少个功能，这是财务报表的地基。

### 已确认的两个具体缺口

1. ~~**采购单价进不了库存。**~~ ✅ 已修：`PurchaseReceived` 现在带
   `unit_cost` 和 `currency`，收货按采购价入成本池。
2. **可用量不含在途。** `in_transit_qty` 这一列建了但没人写、也没进
   `available_qty` 公式。销售看到「可用 0」时，并不知道下周五有 5000 件到货。
   SAP 的 ATP 会把已下单未到货算进去。

### 补的顺序

| 顺序 | 做什么 | 为什么是这个位置 |
|---|---|---|
| ~~1~~ | ~~**库存计价**（移动加权平均）~~ | ✅ 2026-07-29 落地，见 §5.6.13 |
| 2 | 总账 + 自动记账凭证 | 只做一件事的话应该做这个，但它依赖 1 |
| 3 | 应收应付（合同 → 发票 → 收汇核销） | 出口企业的现金流命脉 |
| 4 | 期末结账 | 前三步的自然结果 |
| 旁支 | 在途量进 ATP | 便宜，且立刻能让销售少打几个电话 |

### 我们独立走到了和 SAP 一样的地方

| 这里叫 | SAP 叫 |
|---|---|
| 预留 / 锁定两层占用 | 需求预留 + 交货单 |
| 缺口 = 已售 − 已预留 | MRP 净需求 = 毛需求 − 可用 − 已订购 |
| 合同版本冻结 + 变更留痕 | 凭证不可更改原则 |
| 单据快照（存产品名而非只存 id） | 数据冗余存储 |

缺口计算就是**单级 MRP**。差的是深度，不是方向。

### 关于对外集成（回答「SAP 要不要调外部 API」）

分两类，界线很清楚：

**必须走 API 的 —— 凡是需要对方回一个号的：**
开增值税发票 / 全电发票（号码由税务系统生成并签名）、报关放行（海关回执
决定能不能上船）、各国电子发票强制平台（意大利 SdI、墨西哥 CFDI、巴西
NF-e、印度 IRP、沙特 ZATCA）。

**可以手工也可以自动的：**
银行对账单（下载 CAMT/MT940 导入，或银企直连）、供应商发票（人工录入 /
OCR / EDI）、物流跟踪。

**SAP 自己不直连。** 它只暴露 IDoc / BAPI / OData，协议翻译交给中间件。
中国的金税这段一定走服务商（航天信息、百望、诺诺），因为要认证资质：

```
SAP  ──IDoc/REST──▶  中间件 / 服务商  ──▶  金税 / 海关 / 银行
```

每个 SAP 项目都有一条独立的集成工作流，常占 20–30% 预算。

**对这套系统的含义：** outbox + Kafka 这个位置天然就是集成层的接口。
现在**不接**，但要保证每张对外单据有**稳定单号**和**不可变快照**——
外部系统引用的就是这两样。这也是为什么出库单、采购单的单号在校验通过后
才取、且不跳号：一个会跳的号在对账时就是事故。

## 邮箱登录面（2026-08）：绑定即登录，登录即验证

「邮箱」页前的门禁就是唯一的邮箱账号界面——原独立的「邮箱账号」设置页已并入并删除。
一张登录卡上同时给两条路，谁可用走谁：

```
┌──────────────────────────────┐
│  [G] 用 Google 登录 / 以 Google 身份进入   ← OAuth（Gmail/Workspace）
│  ───────────  或  ───────────
│  邮箱地址  [____________]
│  密码/授权码 [____________]     ← 传统邮局（263 等），一个框两用，
│  [用密码 / 授权码登录]            服务器认哪个就用哪个
└──────────────────────────────┘
```

**关键决定：登录动作本身就是绑定动作。** 没有单独的「保存账号」表单，
也没有任何直接写凭据的 API——进入存储的唯一通道是 /mailbox/verify。
- **先验后存，是不变量。** 密码路拿刚输的（地址, 码）去 IMAP 真登录一次，
  只有成功的登录才被允许碰存储：存入密文、盖 verified 章。第一版是
  先存后验，上线当天就付了学费——一次输错账号密码的尝试覆盖掉了能用的
  Google 授权（SetMailAccountSecret 会切 auth_kind 并清 refresh token）。
  失败的尝试必须零痕迹。
- 登对即存也意味着自愈：轮换过的应用专用密码在下一次成功登录时自动
  更新，不需要任何管理动作。
- 输入不同地址即换绑：服务端清掉旧邮箱的已同步邮件与 UID 水位
  （与 Google 换绑同一段逻辑），因为旧数据对新邮箱毫无意义。
- 成功的密码登录把 auth_kind 切回 PASSWORD 并清空 Google 授权——与
  SetMailAccountOAuth 清空密码密文互为镜像，任何时刻只有一种活凭据。
- Google 回调直接跳回 /emails，页面立即用新授权向 Google 验一次并放行：
  绑定与进入是一个动作，不是重定向之后再点一次按钮。

**删掉的东西：** 发测试邮件（RPC/路由/界面全链路移除——收发同步本身就是
持续的连通性证明）；PUT /my-mail-account（存凭据只能经过活体登录）；
/settings/mailbox 路由（留 redirect 到 /emails）；菜单项「邮箱账号」。
管理员的服务器设置（SMTP/IMAP 主机、配额）降为邮箱页上的一个对话框，
门禁上和邮箱侧栏各留一个入口。

**Gmail 特例要记住：** Google 自 2022 年起拒绝账号密码登录 IMAP/SMTP
（报错 "Application-specific password required"）。Gmail 走密码路只能填
应用专用密码；正路是 Google 按钮。263 等传统邮局才是密码/授权码的主场。

## 合并发送与回复/转发（2026-08）

写信页现在有两种发送方式，对应两种承诺，在界面上明说：

- **分别发送**（默认）：每人一封、变量个性化、互相不可见——原有的 campaign 模型不变。
- **合并发送**：一封信、一次 SMTP 事务、每人一条 RCPT TO，To/抄送名单公开。
  新表 email_message_recipients 记录每个 RCPT 的独立判决（主机常见"收下 a、
  拒掉 b"），部分拒收如实落在被拒的人身上而不是整封信上。上限 50 人——
  再多就是选错了模式。按收件人变化的变量在此模式下直接拒绝
  （NT_MERGED_VARS）：一份共享正文装不下每人一个称呼，渲染给"任意某人"
  是撒谎。抄送只在合并模式存在：分别发送里抄送意味着同一人收 N 封。

**回复/转发**：outbound 消息新增 in_reply_to / references_ids，发信时写成
对应邮件头（存储时无尖括号，出门时补上——严格的客户端认这个）；回复继承
原会话 thread_key，双方客户端都能把答串在问下面。转发不继承线程（新对话），
但原信附件随行——附件在收信时已提取到对象存储，转发就是把 file_key 挂进
新 campaign。两者都先验证"这封信在你自己的收件箱里"，越权即拒。

实测：回复 ACCEPTED 且 In-Reply-To 完整；合并信 To+CC 各自 ACCEPTED，
抄送给自己的副本经 Gmail 回流进收件箱——物理闭环。

### 5.12.13 与邮局双向同步：能对上的和对不上的（2026-08-03 落地）

ERP 不是邮件的存储方，是客户端。做到「在 ERP 里点一下，Gmail 里也变了」，
以及反过来，需要面对三个不能绕开的事实。

#### 一、写回队列是 Postgres，不是 Kafka

`mail_flag_ops` 一张表，`FOR UPDATE SKIP LOCKED` 认领，20 秒一轮。
选它而不是 Kafka 的理由是**重试语义**：延迟重试在这里是一列
（`next_try_at` + 递增退避），换成 Kafka 就是一整套重试 topic；
「哪些卡住了需要人管」在这里是一句 SQL。Kafka 在本系统的职责是跨服务触发，
不是单服务内的工作队列。

**顺序：标记先于移动。** 标记（已读/星标）是一条 STORE，连接反正要开；
移动要先把信找到。反过来排的代价是真实的——「清空回收站」一封一个 op，
会把之后的每一次星标压在几分钟的文件夹操作后面，实测 6 分钟。

**连接是稀缺资源。** 每个 op 各开一条 IMAP 连接的写法，在批量删除时是
四十封 × 两条 = 八十次「拨号-TLS-认证-登出」连发；Gmail 对此的回应是
`Invalid credentials` 或直接断开，然后触发退避，一次点击变成几分钟死寂。
彻底删除改成整批一条连接查完 Message-ID + 一条连接批量 expunge：80 → 2。

#### 二、`\Flagged` 要整个文件夹地问，不能跟着窗口走

读状态只能逐封问（几乎每封都在变，没有便宜的批量问法），所以
`ReconcileFlags` 拉最新 200 个 UID 的 flag。星标搭在这上面是错的：
200 个 UID 在活跃邮箱里约等于几天，给更早的信打星就永远读不回来。

`UID SEARCH FLAGGED` 一次往返报出整个文件夹的星标 UID，所以星标同步是
**整表赋值**而不是逐封比对。已救回的垃圾邮件（`not_junk`）排除在外——
那些行还带着旧的 JUNK 文件夹和 UID，搜索点不到，扫一遍会把星抹掉。

#### 三、已发送有两份记录，去重发生在入库而不是查询

一封发出的信被记录两份：ERP 自己的 `email_messages`（带每位收件人的投递
状态和打开追踪）+ 邮局「已发送」文件夹同步下来的 `email_inbound`。

**去重靠 Message-ID，而且已经在做了**：ERP 发信时把 `message_key`（UUID）
写进 Message-ID 头，`ingest` 对 SENT 文件夹的每一封提取这个 key，
在 `email_messages` 里查得到就直接跳过——邮局的副本不入库，
所以一次 ERP 发送在界面上只出现一次（`inbound.go` 的 SENT 分支）。

> **更正（2026-08-03）**：本节先前断言「Gmail 会改写 Message-ID，两边无法
> 关联，线上 18 封 0 匹配」。**这个结论是错的。**那次比对拿
> `provider_id`（存储带尖括号 `<uuid@domain>`）去比 `message_id`（存储不带
> 尖括号），是格式不匹配；而且真正能匹配上的那些副本**根本没有入库**——
> 正是被上面这段去重跳过了，所以库里当然一条都对不上。
> 实测：连发两封探针，SENT 水位从 539 走到 541（两封都抓到了），
> 一行没存，日志无告警——只有那条去重会这样。库里 523 封 `CAA…` 开头的
> Message-ID 是在 Gmail 网页版直接写的信，本来就该是 Gmail 生成的 id。

后果不是「无法关联」，而是**一次发送的规范记录是 ERP 那一份**。
代价：ERP 发出的信在 `email_inbound` 里没有对应行，所以星标 / 归档 /
删除这些**邮箱对象上的操作对它无从施加**——已发送列表里因此有两种行。
要让已发送变成一个真正的邮箱文件夹，只有两条路：取消入库去重、让邮局副本
落地（旧的发送补不回来），或者发信时由 ERP 自己写一行 SENT 记录。
两条都还没做。

#### 四、「对方是否已读」是三态，不是两态

| 状态 | 数据 | 含义 |
|---|---|---|
| 可能已打开 | `tracked ∧ opened_at` | 像素被拉取过 |
| 未检测到打开 | `tracked ∧ ¬opened_at` | 带了像素，没被拉取 |
| 未开启追踪 | `¬tracked` | 没带像素——**不是**关于收件人的判断 |

后两者在数据里都是空的 `opened_at`，只有第一种是在说收件人。
把「没在看」当「没读」报出来，等于系统凭空编造一件关于客户的事。

`tracked` 必须**发送时记录**（migration 00019），事后推导不出来：像素是注入
正文**副本**的（故意的——存下来的始终是人写的那份，草稿重开不会被污染），
所以存的正文里永远没有它；而决定它的两个条件，一个在消息上（HTML 正文）、
一个在部署上（`MAIL_PUBLIC_BASE_URL`），后者会变。

措辞一律「可能已打开」，绝不写「已读」：Apple Mail 给没打开的信预载图片
（误报），Outlook 给读了的信屏蔽图片（漏报）。客户回信才是确凿的已读。

#### 五、视图条件里，回收站是唯一的例外

所有列表共用一条文件夹条件 `folder='INBOX' OR (folder='JUNK' AND not_junk)`
——垃圾邮件在被救回之前不属于邮箱正文。**回收站必须例外**：删除动作从哪个
文件夹发起，信就该出现在回收站。少了这条例外，从垃圾箱删掉的信会在库里
soft-delete、在任何界面都看不到、同时在邮局回收站里明晃晃躺着；而
`ListTrashForPurge` 没有文件夹过滤，「清空回收站」照样会把它们彻底删除——
标题写着「共 0 条」。线上曾有 39 封处于这个状态。

### 5.12.14 换绑邮箱之后：哪些记录还算数（2026-08-04 落地）

一个员工把邮箱从 A 换成 B，再换回 A。换绑会清掉 `email_inbound` 和
`mail_sync_state`——那些 UID 现在指向别的东西，或者什么都不指向——但**不会**
清 `email_messages`，因为它是按 `sender_id`（员工）存的投递账本，里面有
`opened_at`、`tracked` 这类只有 ERP 知道的事实。这个取舍是对的，但它让两个
判断在换绑之后立刻失真。

#### 一、发件地址不能事后推算

`GetMessage` 原本 `LEFT JOIN mail_accounts ON employee_id`，也就是在问「这个人
**现在**绑的是哪个邮箱」。查询里当时留了一句自认的赌注：

> Reads today's binding […] not worth it until somebody actually rebinds
> mid-history.

赌输了。换绑之后，从中间那个地址发出的每一封信，发件人当场变成了新地址——
不是缺值，是**一个自信的错值**，出现在一块专门回答「什么信发给了谁」的界面上。
和 00019 从正文里嗅探 `tracked` 是同一类错误，修法也一样：在事实成立的那一刻
记下来。`SendResult.FromEmail` 由适配器回报（它握着刚刚认证过的绑定），
`MarkAccepted` 写进 `email_messages.from_email`。

**不回填。** 今天的绑定正是这一列要终结的错答案；HR 档案里的地址从来不保证
等于发信邮箱；从同步历史反推只能覆盖副本还在的那部分，而那部分恰恰不需要这
一列。所以历史留空，界面显示「发件地址未记录」。一个承认的空缺比一个像模像样
的编造值钱：看到「未记录」的人会去查，看到错地址的人不会。

#### 二、孤儿记录必须限定在当前邮箱

`ListSentUnified` 的 orphan 分支本意是兜底：主机不一定保留 Sent 副本，所以
超过 10 分钟还没等到副本的已发送记录也要显示。但它没有归属判断，于是换绑后
**上一个绑定发出的每一封信都会永久变成孤儿**——副本在一个没人登录的邮箱里，
永远不会来。

判据不用猜。`provider_id` 存的是 Message-ID `<key@domain>`，而 domain 是发信
时由 `domainOf(acct.Email)` 生成的。所以：`from_email` 有值就比地址，没值就比
Message-ID 的域名。线上验证——4 封 `columbia.edu` 的记录退出已发送，
`gmail.com` 的全部保留。

代价说清楚：域名判据分不开同一域名下的两个邮箱。这是当初没有及时记录地址的
残余成本，也正是这一列现在存在的理由。

### 5.12.15 作为附件转发：一条被自己的防线挡住的路（2026-08-04 落地）

转发一封信有两种意思。一种是**转述**：把原文引用进正文，原信的附件跟着走。
另一种是**转交原件**：把原始 MIME 整个作为 `.eml` 附件发出，收件人打开的是那封
信本身，信头一个不少。前者读起来方便，后者才能当证据——一段引用的正文证明
不了任何一封信从哪来。

#### 一、先撞上一道自己设的墙

附件入库有一条前缀检查，理由正当：

> The key came from the client […] Refusing anything outside this tenant's own
> prefix stops a caller registering somebody else's object as their attachment.

但收到的邮件不存在那个前缀下。上传件是 `mail-attachments/<tenant>/`，
收件是 `mail/inbound/<tenant>/<account>/`。而转发路径把原信附件的 key 直接塞进
待注册列表——于是**任何带附件的邮件，转发时都会以「文件标识无效」整体失败**。
库里有 398 条收件附件，这条路一直是断的。作为附件转发要用的
`raw_key` 走同一条路，会被同样挡下。

**不能靠放宽前缀解决。** `mail/inbound/` 是按 account 分目录的，一个能指名这个
前缀的调用方就能指名同事的 account，把别人的邮件附件挂到自己的信上——那不是
放宽，是拆掉。

所以区分的是**来源**而不是路径：`PendingAttachment.serverDerived` 未导出，
JSON、protobuf、网关翻译都设不了它，只有本包能设，而本包只在读过
`owner_id` 校验的行之后才设。测试同时钉住两头：客户端粘贴的 `mail/inbound/`
key 必须被拒，服务端解析出来的同一个 key 必须放行。

#### 二、顺手拆掉一颗地雷

注册失败时原本一律 `s.files.Remove(fileKey)`——对上传件是对的（没有行指向的
对象就是没人找得到的孤儿），对转发件是**灾难**：转发一封 11 MB 附件的邮件超限，
会把收件箱里那封信的附件真的删掉。现在只删自己的。

#### 三、`message/rfc822` + base64

附件按 `message/rfc822` 发出，收件人的客户端才会提供「作为邮件打开」，
原信头才有意义。编码用 base64——RFC 2046 §5.2.1 严格读是不允许的（只许
7bit/8bit/binary），这里明知故犯：归档 MIME 是任意字节，可能有超过 998 字节的
行或 8bit 内容，原样内联的风险是**被中途改坏**而不是被拒收；何况 Gmail 和
Outlook 的同名功能就是这么发的。

原信自带的附件不再单独随行——它们已经在 `.eml` 里面，再挂一份等于每个文件发两遍。

### 5.12.16 正文全文搜索：三个不能绕开的事实（2026-08-05 落地）

搜索原本只看主题和发件人，一个字的正文都不看。补上正文，撞上三件事——每一件
都是实测的，不是估计的。

#### 一、不能直接搜 body_text，27% 是空的

2482 封里 682 封 `body_text` 为空而 `body_html` 有内容：群发商发纯 HTML、不带
text/plain。搜 `body_text` 会**静默漏掉四分之一**，而「搜不到」在人眼里等于
「没这封信」——比没有搜索更糟。

也不能改搜 `body_html`：搜「content」会命中 `class="content"`，任何内嵌图片的
邮件都会命中 base64 里的半个字母表。

所以派生一列 `search_text`，用 snippet 已经在用的那个 `HTMLToText`——它正是在
这 682 封上验证过的（它们的 snippet 全是它生成的）。**回填在 Go 里做，不在
migration 里做**：那 682 行恰恰是「SQL 正则近似」和「真实实现」会分叉的地方，
而两套「这封信说了什么」的实现分叉，就是搜索「这条路找得到、那条路找不到」的
来源。

#### 二、Postgres 内建全文检索对中文不可用

```
to_tsvector('simple','这是本月的报价单和合同附件')
  → '这是本月的报价单和合同附件':1        整句一个 token
… @@ plainto_tsquery('simple','报价')  → false
```

英文正常分词并做词干还原，中文完全不分词。要做对需要 `zhparser` 或 `pg_jieba`，
而 `pg_available_extensions` 在这个镜像上只有 `btree_gin`、`pg_trgm`、
`unaccent`——加一个就意味着自建并长期维护一个带 C 扩展和词典的 Postgres 镜像。

`pg_trgm` 不需要词典，中英西一视同仁。它还买到一件 Gmail 的词索引做不到的事：
**子串匹配**——搜「报价」能命中「报价单」，搜订单号的一段能命中整个单号。
放弃的是词干还原和同义词，在几千封的邮箱里这个方向的取舍更划算。

#### 三、五列 OR 会让索引白建

第一版把正文加进原有的 `subject OR from_email OR from_name OR to_email OR
search_text` 里。能跑，而且慢——执行计划说得很清楚：**跨列的 OR 用不了
trigram 索引**，规划器直接全表扫。

| | |
|---|---|
| 五列 OR | Seq Scan，**100 ms** |
| 只查 search_text | Bitmap Index Scan，**1.6 ms** |

同一个邮箱、同一个只命中一封的短语。六十倍，而且差距随邮箱增长——因为其中
一边是扫描。

所以把主题和收发件地址**折进同一列**，谓词变成单条。这才是「搜索文档」该有的
样子：一个字段装下查询可能命中的一切，而不是照抄记录本身的结构。头部放在最
前，于是命中主题时给出的片段以主题开头，读起来是「为什么这行匹配」而不是一段
没头没尾的残句。

落地后：`online pharmacy` 1.8 ms、`invoice`（17 命中）1.4 ms、`lululemon`
（13 命中）3.3 ms。**两字中文「报价」91 ms**——少于三字符用不上 trigram 索引，
如实记录，而且这本来就是今天所有查询的处境。上界由 `MAIL_SYNC_HISTORY` 兜住。

#### 四、顺带：隐形字符会让邮件搜不到

群发商用软连字符和零宽字符做排版填充。它们在渲染后的邮件里看不见，在 snippet
里只是难看，但正文一旦进索引就是**真伤害**：一个零宽连接符落在短语中间，这封
信就再也搜不到那个短语了。2492 封里 342 封带零宽字符、19 封带软连字符。

`U+00AD` 和 `U+2060` 原本根本不在清理清单里，而且清理**只走 HTML 路径**——
text/plain 正文原样进库。两处都修了（migration 00024）。

#### 跨文件夹与命中片段

搜索跨全部文件夹，**排除垃圾邮件和回收站**，和 Gmail 一致：那两处装的是人已经
否决过的东西，混进结果里会让每次搜索都需要再确认一遍。从垃圾邮件里救回来的
（`not_junk`）是反方向的决定，包含在内。

命中片段在 `LIMIT` **之后**才切——把整封正文小写化以定位偏移，只对屏幕上那
五十行做，不对扫描碰到的每一行做。高亮在前端用文本节点拼 `<mark>`，不走
`v-html`：被高亮的字符串是别人在搜索框里敲的，片段是陌生人邮件里的文字，两者
都不该变成标记。
