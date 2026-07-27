# 前端开发文档

> 配套 [ARCHITECTURE.md](ARCHITECTURE.md) 阅读:后端服务拆分、接口语义、权限码定义以架构文档为准。
> 本文档回答三个问题:前端怎么写(约定)、要写什么(页面清单)、什么时候写(与后端的同步节奏)。

## 1. 技术栈

| 层 | 选型 | 备注 |
|---|---|---|
| 框架 | Vue 3.5 + TypeScript | `<script setup>` 组合式 API |
| 构建 | Vite 5 | dev 端口 5173,`/api` 代理到 gateway:8080 |
| 组件库 | Element Plus 2.9 | 通过 `el-config-provider` 跟随应用语言 |
| 状态 | Pinia | 目前仅 auth store;服务端数据不进 store,页面内 ref 管理 |
| 路由 | vue-router 4 | history 模式,`beforeEach` 做登录守卫 |
| HTTP | axios | 统一封装在 `src/api.ts` |
| 国际化 | vue-i18n 10 | 中文 / English / Español,localStorage 记忆 |

运行:`npm run dev`(需先 `make up && make migrate && make services-up` 起后端)。

## 2. 目录结构

```
frontend/src/
├── api.ts               HTTP 封装:信封解包、token 注入、401 跳登录、错误 toast
├── i18n.ts              语言注册与切换
├── router.ts            路由表 + 登录守卫
├── locales/{zh,en,es}.ts
├── stores/auth.ts       登录态:token、姓名、权限码数组、can() 判断
├── components/          跨页面复用组件(LangSwitcher 等)
└── pages/               一页一文件;复杂页面可建子目录放局部组件
    ├── LoginPage.vue
    ├── Shell.vue        侧边栏 + 顶栏布局,业务页面都是它的子路由
    ├── CustomersPage.vue
    ├── TodosPage.vue
    └── FxPage.vue
```

## 3. 硬性约定

**这些约定和后端结构一一对应,违反会直接产生 bug:**

1. **信封**:所有接口返回 `{success, data}` 或 `{success:false, code, message}`。
   永远用 `api.ts` 的 `get/post/put/del` 辅助函数(已解包 data),不要裸调 axios。
2. **int64 是字符串**:后端经 protojson 序列化,所有 id、数量、`expiresInSeconds`
   等 64 位整数到达前端时是 **string**。TypeScript 接口里写 `id: string`,
   比较用 `===` 字符串比较,勿 `parseInt`(精度风险)。
3. **字段名 camelCase**:proto 的 `payment_term` 在 JSON 里是 `paymentTerm`。
   请求体同理(protojson 两种都收,统一发 camelCase)。
4. **错误处理**:`api.ts` 拦截器已统一 toast + 401 踢回登录。页面内只在
   需要**分支逻辑**时捕获,按 `code` 判断(如 `MD_CUSTOMER_CODE_TAKEN`),
   **禁止匹配 message 字符串**(它是给人看的,可能变、可能是别的语言)。
5. **权限渲染**:菜单和写操作控件一律 `v-if="auth.can('模块:资源:动作')"`。
   权限码清单见架构文档;前端隐藏只是 UX,后端 gateway 会再拦一次。
6. **三语同步**:新增任何用户可见文案,必须同时进 `zh.ts / en.ts / es.ts`,
   key 按 `页面.用途` 组织(如 `customers.create`)。缺 key 时回退英文。
7. **金额与汇率显示**:后端给的金额/汇率是精确小数字符串,直接展示,
   **不要 parseFloat 再计算**;需要计算的场景让后端算。
8. **单据编码**:新建表单的编码字段**留空提交**,由后端在保存事务里取号;
   前端不要预先调 `/numbering/next`(打开即取号会让取消的表单白烧号码)。
   字段保持可填,用户手填则以手填为准。
9. **国家与电话**:一律用 `src/constants.ts` 的 `COUNTRY_NAMES`(全称,不用
   USA/UK 这类缩写)和 `DIAL_CODES`(区号→国家,+1 这类共用区号已合并)。
   国家存英文全称(跨语言稳定),电话按 `"+49 211 5566 7788"` 存成一个字段;
   国家选好后自动带出区号,用户改过的区号不再被覆盖。
   区号下拉收起时只显示号码(`#label` 插槽),展开时显示"区号 + 国家"便于搜索。

## 4. 页面清单

状态:✅ 已完成 🔜 后端就绪即可开发 ⏳ 等待对应后端阶段

### 已完成

| 页面 | 路由 | 权限 | 接口 |
|---|---|---|---|
| 登录 | /login | - | POST /auth/login |
| 客户管理 | /customers | masterdata:customer:read(写按钮 :write) | /customers 增改查、停用/启用、/options |
| 我的待办 | /todos | approval:task:act | /approvals/todos、/approvals/tasks/{id}/act |
| 汇率中心 | /fx | fx:rate:read | /fx/rates、/fx/anomalies(只读,汇率仅来自 API 抓取) |

### 阶段 1 收尾

| 页面 | 路由 | 权限 | 说明 |
|---|---|---|---|
| 供应商管理 | /suppliers | masterdata:supplier:read/write | 复制客户页模式,后端接口已就绪,随时可做 |
| 员工与角色 | /settings/employees、/settings/roles | iam:employee:*、iam:role:* | 员工 CRUD+开账号、角色授权矩阵;后端接口已就绪 |

### 阶段 2(product 服务)

| 页面 | 路由 | 权限 | 说明 |
|---|---|---|---|
| 产品列表 | /products | product:product:read | 列表+搜索+分类筛选 |
| 产品详情/编辑 | /products/:id | product:product:write | 基础信息、SKU 子表、单位换算、包装、附件(走 MinIO 预签名) |

### 阶段 3(export 服务,核心业务)

| 页面 | 路由 | 权限(新增码) | 说明 |
|---|---|---|---|
| 报价单列表/编辑 | /quotations | export:quotation:* | 客户+产品明细+实时汇率换算;“接受”后一键生成合同草稿 |
| 合同列表 | /contracts | export:contract:read | 状态筛选(草稿/审批中/待签署/生效/执行中/完成) |
| 合同详情 | /contracts/:id | export:contract:* | **本系统最复杂页面**:基础信息、明细、汇率快照展示、版本历史、状态机操作按钮(提交审批/签署/变更)、执行进度标签页 |
| 出货计划 | /shipments | export:shipment:* | 合同拆批次、锁库存申请、累计数量校验提示 |
| 信用证 | /lcs | export:lc:* | L/C 台账、期限提醒、不符点改单(独立审批) |
| 单证制作 | /documents | export:doc:* | 按出货批次的单证清单、版本、审核状态 |
| 退税管理 | /tax-refunds | export:tax:* | 月度申报批次 + 明细 |
| 收汇管理 | /receivables | export:receivable:* | 应收计划、到账登记(含汇率快照)、水单上传、冲销 |

### 阶段 4(procurement / inventory / shipping)

采购单列表+详情、收货跟踪、付款;库存查询(现存/锁定/可用)、出入库单、
锁定管理;船期列表+详情(节点时间线、集装箱、费用、异常)。权限码按
`procurement:* / inventory:* / shipping:*` 规划。

### 阶段 5(读模型)

| 页面 | 说明 |
|---|---|
| 管理驾驶舱 /dashboard | 经营总览指标卡 + 我的待办 + 风险预警,登录后默认首页 |
| 合同执行进度 /contracts/:id/progress | 采购/备货/出库/船期/单证/退税/收汇七段进度条 |
| 报表中心 /reports | 履约率、汇兑损益等,只读 |

## 5. 与后端的同步开发工作流

每个后端服务落地时,前端在**同一轮**完成对应页面,节奏固定:

```
1. 后端:proto + 服务实现 + gateway 路由 + 权限码种子   ← 后端先行半步
2. 前端:页面 + 三语文案 + 菜单项(带权限 v-if)+ 路由
3. 联调:浏览器实测(见验收清单)
4. 一起提交:一个功能 = 后端 + 前端 + 文案,一个 commit
```

### 每页验收清单(照抄执行)

- [ ] admin 登录:功能完整可用,数据真实往返(创建的数据刷新后还在)
- [ ] 受限账号(如 zhangsan):无权限的菜单不可见;直接输 URL 访问时接口 403,页面给出友好提示
- [ ] 三语切换:所有文案跟随,无漏翻的 key 名裸露
- [ ] 错误路径:触发一个业务错误(如重复编码),确认提示的是后端 message 而非"网络错误"
- [ ] 列表页:分页正确(total 对得上)、搜索生效、空态文案正常

## 6. 待补的横向能力(排期在阶段 3 前)

- **修改密码**:后端 iam 尚无接口,admin 初始密码目前改不掉,上线前必须补
- **附件上传组件**:封装 MinIO 预签名直传(阶段 2 产品附件首次用到)
- **数字输入组件**:金额/数量输入,前端只做格式校验,精确计算交后端
- **表格导出**:批量导出需求文档有提及,统一做成组合式函数
