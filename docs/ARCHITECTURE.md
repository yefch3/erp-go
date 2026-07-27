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

---

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

### 阶段 0：工程骨架（约 2 天）

- [ ] 初始化 `go.work`，各服务独立 module
- [ ] `buf` 配置，proto lint 与生成流水线跑通
- [ ] `pkg/` 基础库：`money`、`pgx`、`grpcx`、`apierr`、`outbox`、`kafkax`
- [ ] `deploy/docker-compose.infra.yml`，一条命令起 PostgreSQL + Kafka + Redis + MinIO
- [ ] Makefile：`make proto` / `make migrate` / `make up` / `make test`
- [ ] CI：lint + `go test ./...` + proto breaking change 检查 + 迁移文件 `tenant_id` 检查

**验收**：`make up` 后基础设施健康；`make proto` 生成物无差异。

### 阶段 1：平台层（约 5 天）

- [ ] `iam`：员工、部门、角色、权限、数据范围、登录签发 JWT
- [ ] `masterdata`：客户、供应商、业务选项、编码规则（含并发安全的取号）
- [ ] `fx`：汇率抓取任务、历史存储、`GetLatestRate`、异常检测
- [ ] `approval`：审批定义、提交、审批、我的待办、发 `ApprovalApproved` 事件
- [ ] `gateway`：JWT 校验、路由到上述服务、统一错误响应

**验收**：能登录拿到 token；能配一条合同审批流；能取到实时汇率；取号在并发 100 请求下无重复。

### 阶段 2：主数据（约 3 天）

- [ ] `product`：分类、产品、SKU、单位换算、包装、条码、附件
- [ ] 明确不提供 Delete，只有 Deactivate
- [ ] 前端：产品管理、客户管理、供应商管理页面

**验收**：产品可维护；被引用后停用而非删除；前端能完整走通主数据录入。

### 阶段 3：出口业务核心（约 10 天，最大的一块）

- [ ] `export` 服务骨架 + 数据库迁移
- [ ] 报价单：创建、发送、接受/拒绝、复制、汇率快照
- [ ] 合同：由报价生成、版本表、提交审批、签署、生效
- [ ] 合同生效时生成收汇计划 + 写 outbox
- [ ] outbox relay 投递 Kafka
- [ ] 合同变更：新版本 + 重新审批 + 旧版本 SUPERSEDED
- [ ] 前端：报价单、合同、合同审批、我的待办

**验收**：报价 → 合同 → 审批 → 签署 → 生效全链路可走通；生效后改字段被拒绝；变更走新版本；`erp.export.contract.v1` 能收到 `ContractEffective`。

### 阶段 4：仓储与采购（约 8 天）

- [ ] `inventory`：仓库、库位、库存、锁定（幂等）、入库、出库、流水
- [ ] `procurement`：消费 `ContractEffective` 生成采购需求；采购单、审批、收货、付款
- [ ] 采购收货 → 调 `inventory.CreateInbound`
- [ ] 前端：库存查询、出入库、采购单、收货

**验收**：合同生效后采购需求自动出现；收货后库存增加且有流水；`LockStock` 重复调用不重复锁定。

### 阶段 5：出货计划与船期（约 8 天）

- [ ] `export`：出货计划、累计出货量校验（行锁）、库存锁定 saga
- [ ] `inventory`：消费 `ShipmentPlanConfirmed` 创建出库任务
- [ ] `shipping`：船期订舱、集装箱、运输节点、费用、异常
- [ ] 出库完成 → 回写 `shipped_qty` → 船期更新待装运
- [ ] 前端：出货计划、船期管理

**验收**：出货计划确认时库存不足能返回缺口；累计出货量超合同被拒绝；取消计划自动释放锁定。

### 阶段 6：信用证、单证、退税、收汇（约 8 天）

- [ ] 信用证：创建校验（仅 L/C 付款方式、金额覆盖）、修改件、审证结果
- [ ] 单证制作：7 类单证、版本、负责人、审核
- [ ] 退税：申报、跟踪、到账
- [ ] 收汇：计划、登记、冲销（append-only）、到账汇率快照
- [ ] 前端对应页面

**验收**：非 L/C 合同无法创建信用证；已确认收汇无法删除只能冲销；数据库层 DELETE 权限已回收。

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

---

## 10. 不变量清单

这份清单来自需求文档中所有「不能……」的表述，每条都必须有对应的强制手段。实现时逐条对照验收。

| # | 不变量 | 出处 | 强制手段 |
|---|---|---|---|
| 1 | 驾驶舱不能直接更改库存、合同或付款 | 1. | `reporting` 只暴露 Query 方法；只读数据库账号 |
| 2 | 报价单不能扣库存、不能创建采购单 | 2.1 | `export` 报价用例不调用 inventory/procurement 写接口 |
| 3 | 合同审批或生效后关键字段不能改，只能创建变更版本 | 2.2 | `contract_versions` 触发器拒绝对 APPROVED 版本 UPDATE |
| 4 | 用户只能看到分配给本人或本人角色的审批任务 | 2.3 | `approval_tasks.assignee_id` 过滤 + 数据范围 |
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

尚未决策、但不阻塞开发的事项：

- **云厂商**（阿里云 / AWS / 其他）：只影响阶段 8 的部署清单，届时确认
- **审批通知渠道**：站内「我的待办」已满足文档要求；邮件 / 企业微信提醒作为后续增强，事件总线已具备接入点
