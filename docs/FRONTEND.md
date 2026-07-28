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
    ├── ProductsPage.vue
    ├── EmployeesPage.vue
    ├── RolesPage.vue
    ├── ApprovalFlowsPage.vue
    ├── QuotationsPage.vue
    ├── ContractsPage.vue
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
10. **不要把注定失败的选项摆给用户**:如果某个候选项一提交必然被后端拒绝
    (例如已生成过合同的报价单、已停用的产品),就在**后端加过滤参数**把它筛掉,
    而不是靠错误提示补救。参见 `/quotations?without_contract=true`。
11. **状态机按钮按状态出**:操作区不要长期摆着 disabled 的按钮,
    当前状态不允许的动作直接不渲染。合同详情页的提交/签署/变更/作废就是这样,
    审批中的合同一个按钮都不显示。
12. **待办要能点进原单**:审批列表的单据编号是链接
    (`DOC_ROUTES` 把 `bizType` 映射到路由,未映射的类型渲染成纯文本)。
    这不只是方便——审批引擎不认识业务类型,列表里没法校验单据还在不在,
    **一条点开报「不存在」的链接,比一条静默无效的记录显眼得多**。
13. **抽屉/弹窗要等数据到手再开**。先 `open = true` 再 await,请求失败时在同一帧关掉,
    Element Plus 的过渡会卡在 `leave-active` 上,留下一个空白遮罩盖住整页。
    正确顺序:取数据 → 成功才 `open = true`。
14. **深链接用 query 而不是新路由**:`/contracts?id=4` 打开对应合同的抽屉,
    列表仍在背后。`watch(route.query.id, ..., {immediate:true})` 负责进入,
    抽屉关闭时 `router.replace` 把 query 抹掉,免得刷新又弹出来。
15. **实时推送用 `src/live.ts`,不要用 `EventSource`**。
    `EventSource` 设不了 Authorization 头,唯一的绕法是把 JWT 塞进 query string,
    那样它会进访问日志、代理日志和浏览器历史。改用 `fetch` + `ReadableStream`
    读 SSE 流,token 留在请求头里。
    连接在 `Shell.vue` 挂载时打开、卸载和登出时关闭,整个会话共用一条。
    页面用 `onLive(handler)` 订阅,记得在 `onUnmounted` 里退订。
    **推送只是提示,收到后照常调接口重新读**——不要直接拿推送里的数据渲染,
    否则权限校验和加载逻辑就有了两条会互相矛盾的路径。
    断线自动重连,退避到最长 60 秒;连不上时页面退化成手动刷新,功能不受影响。

## 4. 页面清单

状态:✅ 已完成 🔜 后端就绪即可开发 ⏳ 等待对应后端阶段

### 已完成

| 页面 | 路由 | 权限 | 接口 |
|---|---|---|---|
| 登录 | /login | - | POST /auth/login |
| 客户管理 | /customers | masterdata:customer:read(写按钮 :write) | /customers 增改查、停用/启用、/options |
| 我的待办 | /todos | approval:task:act | /approvals/todos、/approvals/tasks/{id}/act |
| 产品管理 | /products | product:product:read(写按钮 :write) | /products 增改查、/product-categories、/uoms、SKU、附件预签名上传 |
| 员工管理 | /settings/employees | iam:employee:read(写按钮 :write，分配角色 iam:role:write) | /employees、/departments、开账号、重置密码、分配角色 |
| 角色权限 | /settings/roles | iam:role:read(写按钮 :write) | /roles、/permissions、授权矩阵 + 数据范围(/data-scopes) |
| 审批流配置 | /settings/approvals | approval:flow:read(写按钮 :write) | /approval-flows 增删节点、保存为新版本、版本历史 |
| 报价单 | /quotations | export:quotation:read(写按钮 :write) | /quotations 增改查、发送、客户答复 |
| 出口合同 | /contracts | export:contract:read(写按钮 :write) | 列表 + 抽屉式详情:条款、明细、汇率快照、版本历史、提交审批/签署/变更/作废 |
| 汇率中心 | /fx | fx:rate:read | /fx/rates、/fx/anomalies(只读,汇率仅来自 API 抓取) |

### 阶段 1 收尾

| 页面 | 路由 | 权限 | 说明 |
|---|---|---|---|
| 供应商管理 | /suppliers | masterdata:supplier:read/write | 复制客户页模式;gRPC 6 个方法齐全,网关还只暴露了列表和新建两条路由 |

### 阶段 3(export 服务,核心业务)

| 页面 | 路由 | 权限(新增码) | 说明 |
|---|---|---|---|
| ~~报价单→合同~~ | /contracts | export:contract:write | ✅ 已完成。入口放在合同页的「由报价生成」,下拉只列**尚无存活合同**的已接受报价(`without_contract=true`) |
| ~~合同列表~~ | /contracts | export:contract:read | ✅ 已完成 |
| ~~合同详情~~ | /contracts(抽屉) | export:contract:* | ✅ 已完成。做成**抽屉而非独立路由**:合同和列表几乎总是一起看,弹出比跳走再回来少一次加载。执行进度标签页待阶段 5 出货计划落地后再补 |
| ~~合同文件~~ | /contracts(抽屉内) | export:contract:* | ✅ 已完成。三步直传(presign → PUT 对象存储 → register),按「我方拟稿/客户签回/其他附件」分类,每份文件标注归属版本 |
| 合同在线签署落地页 | /public/contracts/sign/:token | 无(免登录) | 阶段 6。客户从邮件点进来看合同 PDF 并确认;失效链接一律同一句中性提示 |
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

### 公开页面（无需登录，阶段 6）

| 页面 | 路由 | 说明 |
|---|---|---|
| 报价单答复落地页 | /public/quotations/respond/:token | 客户从邮件点进来的页面:显示单号、客户名、金额、有效期,一个确认按钮完成接受/拒绝。**无侧边栏、无导航、无登录**;失效链接一律显示同一句中性提示。三语按邮件里的语言参数决定 |

## 6. 待补的横向能力(排期在阶段 3 前)

- ~~修改密码~~:已完成(顶栏用户菜单;另有管理员重置密码与开通账号)
- ~~附件上传组件~~:已在产品页落地(预签名 → 浏览器直传 → 回写 key),后续页面照抄这三步
- **首次登录强制改密**:登录响应带 `mustChangePassword` 时,路由守卫只放行修改密码
  (后端第一步落地后同步做;详见架构文档「账号开通与初始密码」)
- **数字输入组件**:金额/数量输入,前端只做格式校验,精确计算交后端
- **报价单发送**:发送按钮改为真正发信(PDF 预览 + 收件人确认 + 发送结果),
  客户答复回流后列表状态自动变化(阶段 6)
- **表格导出**:批量导出需求文档有提及,统一做成组合式函数
