# 客户负责人权限调整

分支：`codex/master-data-rework`。

## 规则与租户边界

沿用已有多负责人关系，不新增协作人、共享销售或授权关系。客户访问同时受功能权限、当前登录公司的 `tenant_id`、有效负责人关系限制。IAM 在 `customer` 数据范围下只对当前租户内的有效 `SUPER_ADMIN` 角色返回 ALL；普通用户返回 SELF，部门范围或其他模块的 ALL 不会扩大客户范围。

最高权限的“全部客户”始终指当前公司。租户身份来自已验证的登录上下文和服务间签名，不从浏览器提交的客户或负责人字段取值。

## 用户要求的十项说明

1. **存储**：复用 `customer_owners`，每条关系包含 `tenant_id`、`customer_id`、`employee_id`、职责、状态、生效日期等。不同员工可以同时关联同一客户。`is_primary` 只标记主要负责人，不决定访问权限。
2. **自动负责人**：创建客户时，在同一数据库事务内插入当前登录员工的 SALES 负责人关系；负责人写入失败则整个创建回滚。Excel 导入同样加入导入人，保留表格指定的负责人，避免同一员工重复插入。
3. **查询限制**：主数据 CustomerService 的统一 RPC 拦截器向 IAM 查询当前员工的客户范围。普通用户的列表、搜索、分组、查重和邮件通讯录 SQL 使用当前租户内 `customer_owners` 的 EXISTS 判断，在分页和总数统计前过滤。按 ID 的客户、地址、联系人、负责人、变更记录及信用评级请求先检查客户访问权。
4. **增加负责人**：沿用添加负责人接口，写入 ACTIVE 关系后，下一个请求按新关系获得访问权限。新增弹窗支持多选员工；每次调用沿用现有校验和审计。员工选择和导入中的员工 ID 必须在当前公司内有效。
5. **移除负责人**：沿用移除接口停用关系。每次请求实时查询，不缓存客户授权；移除后下一次详情、搜索、编辑或业务使用请求会被限制。仍有其他有效职责关系的同一员工继续拥有访问权。原有开始/结束日期规则保留。
6. **最高权限访问**：复用 IAM 的 `SUPER_ADMIN` 角色代码和有效状态判断，不使用中文角色名称，也不把任意 ALL 数据范围当作最高权限。所有查询仍保留 `tenant_id` 条件。
7. **管理负责人**：最高权限用户可以进入当前公司任意客户详情，使用原负责人区域多选添加、编辑职责/日期/主要标记或移除。仅改变负责人表及客户变更日志，不改写历史询盘、报价或合同快照。
8. **删除**：客户管理桌面和移动端的更多操作仅在后端返回 `canDelete=true` 时显示删除。点击仅确认“确定删除该客户吗？”。主数据 RPC 拦截器和应用层均限制删除为最高权限。继续调用已有 DELETE 逻辑，实际是停用客户（软删除），保留历史资料；普通销售即便是创建人或负责人也不能删除。
9. **同步入口**：见下一节。询盘工作台还补充保存客户 ID 的后端校验，不能通过 JSON 绕过选择器；客户报价保存、确认也校验客户 ID。报价/合同列表和销售端询盘列表按当前客户范围过滤，同时保留原业务单据权限。
10. **文件清单**：见文末。Vue 修改三页，后端涉及 IAM、主数据、Gateway、采购询盘和出口报价合同服务。

## 查询与选择入口

- `/api/customers`：客户列表、搜索和复用该接口的客户选择器，包括合同、订单、邮件转客户等页面。
- `/api/customers/countries`：客户管理国家分组及数量。
- `/api/customers/duplicates`：名称、税号、邮箱重复检查。
- `/api/customers/{id}` 及地址、联系人、负责人、变更记录、信用评级子资源：客户权限检查。
- `/api/sourcing-customer-options`、`/api/sourcing-customer-options/{id}/contacts`：询盘客户和联系人选择。
- `/api/mailing-contacts`、`/api/customer-countries`、`/api/mailing-contacts/by-country`：邮件通讯录、按国家选择收件人。
- 内部 `ListCustomerIdsByOwnerEmployees`、`ListOwnerCountries`：限制到当前客户范围，不能传其他员工 ID 扩权。
- `InquiryWorkspace` 销售/客户报价视图：列表和详情读取当前客户权限；保存与修改客户时重新校验。
- `ListQuotations`、`ListContracts`、客户报价汇总：在原业务权限范围上叠加客户范围。

仓库没有独立的 CRM 服务/客户跟进接口；客户详情中已有联系人、负责人和变更历史均经上述统一检查。任何复用客户接口的页面自动获得相同限制。

## 验证

- 主数据 Go 测试在独立测试库中运行，包含现有创建、地址、联系人、Excel 导入等数据库测试。
- 新增实际数据库权限测试覆盖自动负责人、多负责人、增删即时生效、直接 ID 越权、邮件/国家/查重过滤、普通用户删除拒绝、管理员删除、跨租户列表/详情/删除拒绝。
- IAM 测试使用相同员工/角色 ID 分属不同租户的临时表，验证最高角色不串公司，停用角色/员工不保留最高权限。
- 询盘实际数据库测试覆盖伪造客户拒绝、移除负责人后的列表/详情/保存拒绝、恢复负责人及历史记录不变。
- 报价合同实际数据库测试覆盖分页前过滤、总数、空授权集合、恢复授权、最高权限、跨租户拒绝及历史快照不变。
- Gateway、采购与出口服务 Go 测试通过；Vue 类型检查和生产构建通过；客户表单、Excel 导入、邮件转客户、客户报价共 28 个前端测试通过。

没有新增数据库表或迁移。主数据服务新增 `IAM_ADDR` 依赖，本地和生产 Compose 配置已补齐。历史上没有任何负责人的客户仍可由当前公司的最高权限用户查看并分配负责人，不自动推断历史负责人。

本次完成代码与本地验证；未发布或重启现有业务服务。

## 完整修改文件

### frontend/

- `frontend/src/pages/CustomerDetailPage.vue`
- `frontend/src/pages/CustomersPage.vue`
- `frontend/src/pages/InquiryWorkspacePage.vue`

### services/iam/

- `services/iam/internal/app/customer_scope_integration_test.go`
- `services/iam/internal/app/scope.go`

### services/masterdata/

- `services/masterdata/cmd/main.go`
- `services/masterdata/db/queries/masterdata.sql`
- `services/masterdata/internal/adapter/grpcin/customer_access.go`
- `services/masterdata/internal/adapter/grpcin/customer_access_test.go`
- `services/masterdata/internal/app/customer_access.go`
- `services/masterdata/internal/app/customer_import.go`
- `services/masterdata/internal/app/customer_relationship.go`
- `services/masterdata/internal/app/customer_relationship_integration_test.go`
- `services/masterdata/internal/app/duplicate.go`
- `services/masterdata/internal/app/service.go`
- `services/masterdata/internal/config/config.go`
- `services/masterdata/internal/store/masterdata.sql.go`

### services/gateway/

- `services/gateway/internal/httpapi/customer_access.go`
- `services/gateway/internal/httpapi/customer_access_test.go`
- `services/gateway/internal/httpapi/customer_relationship_handlers.go`
- `services/gateway/internal/httpapi/server.go`

### services/procurement/

- `services/procurement/cmd/main.go`
- `services/procurement/internal/adapter/grpcout/customer_access.go`
- `services/procurement/internal/app/customer_access.go`
- `services/procurement/internal/app/customer_access_test.go`
- `services/procurement/internal/app/inquiry_workspace.go`
- `services/procurement/internal/app/service.go`
- `services/procurement/internal/app/sourcing.go`

### services/export/

- `services/export/cmd/main.go`
- `services/export/db/queries/contract.sql`
- `services/export/db/queries/quotation.sql`
- `services/export/internal/adapter/grpcout/deps.go`
- `services/export/internal/app/contract.go`
- `services/export/internal/app/customer_access.go`
- `services/export/internal/app/customer_access_test.go`
- `services/export/internal/app/customer_offer.go`
- `services/export/internal/app/offer_summaries.go`
- `services/export/internal/app/quotation.go`
- `services/export/internal/store/contract.sql.go`
- `services/export/internal/store/quotation.sql.go`

### deploy/

- `deploy/docker-compose.prod.yml`
- `deploy/docker-compose.services.yml`

