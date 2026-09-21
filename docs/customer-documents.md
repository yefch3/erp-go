# 客户资料与到期提醒

客户详情的“客户资料”使用普通附件列表。上传只需选择文件（单文件 10 MB），可选一个提醒日期；留空不提醒，选定后当天在“我的待办”提醒。支持点击文件预览及下载，列表可修改提醒日期。不再展示资料标题、备注、版本和高级有效期设置。PDF 使用 PDF.js 本地 worker 渲染。

简单界面复用既有后端：文件名作为 title，提醒日期作为 expires_on，remind_days 固定为 0。已有附件按原 expires_on 减 remind_days 显示实际提醒日期，避免改变已有提醒时间。后台保存旧版本以保护数据，但前台不呈现版本操作。

## 权限与租户

复用 CustomerService 的 IAM / 负责人访问拦截器，HTTP 入口复用客户 read/write 权限。上传、列表、预览、下载、历史版本、待办与标记已读均校验当前租户和当前负责人关系。SUPER_ADMIN 仅放开当前 tenant 的负责人限制。附件不暴露公开 URL / 存储 key，文件请求每次经过鉴权并设置 no-store；已经下载的本地副本不可能被撤回。

## 存储与更换

迁移 `00024_customer_documents.sql` 新建 `customer_documents`（版本快照）与 `customer_document_reads`（tenant + revision + employee 已读状态）。编号 23 在本地已有迁移历史中被占用，故使用 24，保留既有迁移记录。

对象存储复用 masterdata 的 MinIO/S3，key 前缀为 `customer-documents/{tenant_id}/{customer_id}/`。数据库不保存文件字节。每次更新追加版本；仅调整有效期时复用原文件。替换请求携带当前版本 ID，过期版本提交返回冲突，避免覆盖他人更新。保留全部旧版，附件可通过“更多操作 ▼ → 删除”删除。删除权限与客户读取权限相同，不要求管理员、写权限或上传人身份。删除采用 deleted_at 标记全部版本，列表、提醒、下载和后续更新均排除已删除附件；不删除客户。

## 提醒

接入现有 `/api/home/reminders` 与已读接口，来源 `CUSTOMER_DOCUMENT`。进入提前提醒日期后，当前版作为一条稳定待办显示，不每天重复生成消息。日期采用 UTC 日历日，与倒计时一致。每个负责人分别标记已读；当前版更换后旧版提醒消失，新版按其设置产生新的未读提醒。负责人变更直接反映在查询中，无需同步收件人快照。提醒在用户打开 / 刷新现有待办时计算，不发送邮件或离线推送。

## 接口

- GET `/api/customers/{id}/documents`
- POST `/api/customers/{id}/documents`（multipart，新增或替换资料）
- GET `/api/customers/{id}/documents/{revisionId}/file`
- DELETE `/api/customers/{id}/documents/{revisionId}`（复用客户 read 权限及负责人校验）
- GET `/api/home/reminders?source=CUSTOMER_DOCUMENT`
- 现有首页提醒已读接口接受来源 `CUSTOMER_DOCUMENT`

## 验证

包含资料有效期边界测试、真实 PostgreSQL 版本/提醒/负责人撤销/跨租户测试、迁移 up/down/up，前端类型检查及构建。浏览器使用 tenant 1 的销售测试账号，客户 75 放有合成 PDF 与 PNG 样例。

迁移 `00025_customer_document_delete.sql` 增加附件删除标记。回归测试覆盖非上传负责人删除、撤销负责人后拒绝、跨租户拒绝、删除后所有版本下载失效及提醒消失。
