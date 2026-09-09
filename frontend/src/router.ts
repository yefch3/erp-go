import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: () => import('./pages/LoginPage.vue') },
    // Reached from a mail, by somebody who has no account yet — having one is
    // what they are here to arrange. Outside the shell and outside the guard.
    { path: '/activate', component: () => import('./pages/ActivatePage.vue') },
    { path: '/reset', component: () => import('./pages/ResetPage.vue') },
    {
      path: '/',
      component: () => import('./pages/Shell.vue'),
      children: [
        // Landing on 我的待办, not on any business module: it is the one page
        // every employee owns regardless of roles, so a freshly activated
        // account with no permissions yet arrives somewhere that can greet it
        // instead of a wall of 没有权限.
        { path: '', redirect: '/todos' },
        { path: 'todos', component: () => import('./pages/TodosPage.vue') },
        // 我的资料的老地址。顶栏下拉菜单一直指着它，留作跳转。
        { path: 'me', redirect: '/basic/employees/me' },
        { path: 'customers', redirect: '/basic/customers' },
        { path: 'basic/customers', component: () => import('./pages/CustomersPage.vue') },
        { path: 'basic/customers/:id', component: () => import('./pages/CustomerDetailPage.vue') },
        // 我的信息和员工列表、部门管理同级。**不设权限守卫**：每个登录的人都有
        // 一份自己的资料，而这页读写的对象永远是调用者本人（接口里没有「员工 id」
        // 这个入参）。写在 :id(\\d+) 之前只是为了读起来顺，'me' 不是数字，
        // 两条路由不可能撞上。
        { path: 'basic/employees/me', component: () => import('./pages/MyProfilePage.vue') },
        { path: 'basic/employees', component: () => import('./pages/EmployeesPage.vue') },
        { path: 'basic/employees/departments', component: () => import('./pages/DepartmentsPage.vue') },
        { path: 'basic/employees/roles', component: () => import('./pages/RolesPage.vue') },
        { path: 'basic/employees/org', component: () => import('./pages/OrgChartPage.vue') },
        // 员工详情有自己的地址，和客户详情（上面那行）一样。**限定数字**，
        // 否则 :id 会把 departments / roles / org 这三个静态路径也一起吃掉——
        // vue-router 虽然让静态段优先，但把「不可能撞上」写进路由本身，
        // 比依赖优先级可靠。
        { path: 'basic/employees/:id(\\d+)', component: () => import('./pages/EmployeeDetailPage.vue') },
        { path: 'basic/ports', component: () => import('./pages/PortsPage.vue') },
        { path: 'basic/suppliers', component: () => import('./pages/SuppliersPage.vue') },
        { path: 'basic/suppliers/factories', component: () => import('./pages/FactoriesPage.vue') },
        { path: 'basic/suppliers/factories/:id', component: () => import('./pages/FactoryDetailPage.vue') },
        { path: 'basic/suppliers/:id', component: () => import('./pages/SupplierDetailPage.vue') },
        { path: 'products', component: () => import('./pages/ProductsPage.vue') },
        // 客户询盘、报价接收与部门报价使用独立页面视角。
        { path: 'sales/intakes', redirect: (to) => ({ path: '/sales/inquiries', query: to.query }) },
        { path: 'sales/inquiries', component: () => import('./pages/InquiryWorkspacePage.vue'), props: { view: 'SALES' } },
        { path: 'sales/inquiries/:id', component: () => import('./pages/InquiryWorkspacePage.vue'), props: { view: 'SALES' } },
        // 客户报价直接接收已提交源报价。
        { path: 'sales/quotations', component: () => import('./pages/InquiryWorkspacePage.vue'), props: { view: 'QUOTATIONS' } },
        { path: 'sales/settings/inquiry-templates', component: () => import('./pages/InquiryTemplatesPage.vue') },
        // 旧书签保留一个兼容版本，并把查询条件一并带到新入口。
        { path: 'quotations', redirect: '/sales/quotations' },
        { path: 'contracts', component: () => import('./pages/ContractsPage.vue') },
        { path: 'basic/excel-usage', component: () => import('./pages/ExcelUsagePage.vue') },
        { path: 'platform/tenants', component: () => import('./pages/PlatformTenantsPage.vue') },
        { path: 'contract-execution', component: () => import('./pages/ContractExecutionPage.vue') },
        // 旧“出运单”入口已并入船期管理，保留重定向兼容历史书签。
        { path: 'shipments', redirect: '/shipping/schedules' },
        // 船运操作台只属于能够维护船期的人员。销售和采购查看售前结果时
        // 走各自案件详情里的只读“船运协作”，不直接进入这里。
        { path: 'shipping', redirect: '/shipping/sourcing' },
        { path: 'shipping/sourcing', component: () => import('./pages/InquiryWorkspacePage.vue'), props: { view: 'LOGISTICS' } },
        { path: 'shipping/requirements', component: () => import('./pages/ShippingRequirementsPage.vue'), meta: { permission: 'shipping:schedule:read' } },
        { path: 'shipping/schedules', component: () => import('./pages/ShippingPage.vue'), meta: { permission: 'shipping:schedule:read' } },
        { path: 'shipping/:id', component: () => import('./pages/ShippingDetailPage.vue'), meta: { permission: 'shipping:schedule:read' } },
        // 客户对账：待核销 / 已完成是它下面的两个子页，靠 ?view=done 分。
        // **一个服务一个菜单项**——两条路径会在菜单上排成两行，而它们是
        // 同一件事的两个视图。query 不影响 route.path，所以菜单高亮照常。
        { path: 'customer-recon', component: () => import('./pages/ReceivableDuePage.vue') },
        // 退役地址一律重定向，不能直接删：路由表没有兜底路由，不匹配的
        // 地址会让 matched 变成空——连外面那层 Shell 都不渲染，老书签点
        // 进去是一整块白，没有菜单也没有返回入口。
        { path: 'receipts', redirect: '/customer-recon' },
        { path: 'receivable-cases', redirect: (to) => ({ path: '/customer-recon', query: to.query }) },
        { path: 'receivable-cases-done', redirect: (to) => ({ path: '/customer-recon', query: { ...to.query, view: 'done' } }) },
        // 历史提醒里存的 detailUrl 还指着它，带 query 过去，
        // ?keyword=合同号 仍然能把人送到那一行上。
        { path: 'receivable-due', redirect: (to) => ({ path: '/customer-recon', query: to.query }) },
        { path: 'stocks', component: () => import('./pages/StocksPage.vue') },
        { path: 'outbounds', component: () => import('./pages/OutboundsPage.vue') },
        { path: 'warehouses', component: () => import('./pages/WarehouseWorkbenchPage.vue') },
        { path: 'warehouses/profiles', component: () => import('./pages/WarehouseProfilesPage.vue') },
        { path: 'warehouses/arrivals', component: () => import('./pages/WarehouseArrivalsPage.vue') },
        { path: 'warehouses/receipts', component: () => import('./pages/WarehouseReceiptsPage.vue') },
        { path: 'warehouses/imports', component: () => import('./pages/WarehouseImportsPage.vue') },
        { path: 'warehouses/settings', component: () => import('./pages/WarehouseSettingsPage.vue') },
        // 兼容旧书签；采购现在直接进入售前询价，不再展示虚构的工作台。
        { path: 'procurement', redirect: '/procurement/sourcing' },
        { path: 'procurement/intakes', redirect: (to) => ({ path: '/sales/intakes', query: to.query }) },
        { path: 'procurement/settings/inquiry-templates', redirect: (to) => ({ path: '/sales/settings/inquiry-templates', query: to.query }) },
        { path: 'procurement/sourcing/pending', component: () => import('./pages/InquiryWorkspacePage.vue'), props: { view: 'PROCUREMENT' } },
        { path: 'procurement/sourcing', component: () => import('./pages/InquiryWorkspacePage.vue'), props: { view: 'PROCUREMENT' } },
        { path: 'procurement/sourcing/:id', component: () => import('./pages/InquiryWorkspacePage.vue'), props: { view: 'PROCUREMENT' } },
        { path: 'requirements', component: () => import('./pages/RequirementsPage.vue') },
        { path: 'sourcing-cases', redirect: (to) => ({ path: '/procurement/sourcing', query: to.query }) },
        { path: 'sourcing-cases/:id', redirect: (to) => ({ path: `/procurement/sourcing/${String(to.params.id)}`, query: to.query }) },
        { path: 'purchase-orders', component: () => import('./pages/PurchaseOrdersPage.vue') },
        // 供应商这边只剩这一页。待核销 / 已完成是它下面的两个子页，靠
        // ?view=done 分——一个服务一个菜单项，query 不影响 route.path，
        // 所以菜单高亮照常。
        { path: 'supplier-recon', component: () => import('./pages/SupplierReconPage.vue') },
        // 三条退役地址一律重定向，不能直接删：路由表没有兜底路由，不匹配的
        // 地址会让 matched 变成空——连外面那层 Shell 都不渲染，老书签点进去
        // 是一整块白，没有菜单也没有返回入口。query 一起带过去，老链接里的
        // ?keyword= 仍然管用。
        { path: 'supplier-invoices', redirect: (to) => ({ path: '/supplier-recon', query: to.query }) },
        { path: 'supplier-payments', redirect: (to) => ({ path: '/supplier-recon', query: to.query }) },
        { path: 'supplier-statements', redirect: (to) => ({ path: '/supplier-recon', query: to.query }) },
        // 银行流水已并进入账/出账，不再保留独立业务页面。旧书签落到入账，
        // 避免历史链接进入一个已经退役的界面。
        { path: 'bank-transactions', redirect: (to) => ({ path: '/customer-recon', query: to.query }) },
        { path: 'emails', component: () => import('./pages/EmailsPage.vue') },
        { path: 'team-mail', component: () => import('./pages/TeamMailPage.vue') },
        { path: 'mail/export-log', component: () => import('./pages/MailExportLogPage.vue') },
        { path: 'fx', component: () => import('./pages/FxPage.vue') },
        { path: 'settings/employees', redirect: '/basic/employees' },
        { path: 'settings/roles', redirect: '/basic/employees/roles' },
        { path: 'settings/approvals', component: () => import('./pages/ApprovalFlowsPage.vue') },
        // Signatures moved into the mailbox itself (the ✍️ entry on its
        // rail), for the same reason the host settings did: they are part of
        // writing mail, not a system setting. Kept as a redirect so old
        // bookmarks and menu links land somewhere sensible.
        { path: 'settings/signatures', redirect: '/emails' },
        // The old /settings/mailbox page merged into /emails: the sign-in
        // gate is the account surface now. Kept as a redirect so old
        // bookmarks and the OAuth-era links still land somewhere sensible.
        { path: 'settings/mailbox', redirect: '/emails' },
      ],
    },
  ],
})

router.beforeEach((to) => {
  // The credential is an httpOnly cookie this code cannot see; what the
  // guard reads is the signed-in signal login writes and expiry clears. A
  // stale signal only means one extra round trip — the API answers 401 and
  // the interceptor routes to /login regardless.
  const loggedIn = localStorage.getItem('employeeName') !== null
  // Activation and password reset are the pages whose whole audience is
  // logged out and has to stay that way: sending them to /login would hide
  // the only link they hold.
  if (to.path === '/activate' || to.path === '/reset') return true
  // Where they were trying to go travels with them, so opening a bookmarked
  // mail on a dead session lands on that mail after signing in rather than on
  // the home page with the reason forgotten.
  if (!loggedIn && to.path !== '/login') {
    return { path: '/login', query: to.fullPath === '/' ? {} : { redirect: to.fullPath } }
  }
  if (loggedIn && to.path === '/login') return '/'
  const required = typeof to.meta.permission === 'string' ? to.meta.permission : ''
  if (loggedIn && required) {
    try {
      const permissions = JSON.parse(localStorage.getItem('permissions') ?? '[]') as string[]
      if (!permissions.includes(required)) return '/todos'
    } catch {
      return '/todos'
    }
  }
})
