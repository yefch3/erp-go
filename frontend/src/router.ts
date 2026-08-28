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
        // 我的资料。不设权限守卫：每个登录的人都有一份自己的资料，
        // 而这页读写的对象永远是调用者本人（接口里没有「员工 id」这个入参）。
        { path: 'me', component: () => import('./pages/MyProfilePage.vue') },
        { path: 'customers', redirect: '/basic/customers' },
        { path: 'basic/customers', component: () => import('./pages/CustomersPage.vue') },
        { path: 'basic/customers/:id', component: () => import('./pages/CustomerDetailPage.vue') },
        { path: 'basic/employees', component: () => import('./pages/EmployeesPage.vue') },
        { path: 'basic/employees/departments', component: () => import('./pages/DepartmentsPage.vue') },
        { path: 'basic/employees/roles', component: () => import('./pages/RolesPage.vue') },
        { path: 'basic/employees/org', component: () => import('./pages/OrgChartPage.vue') },
        { path: 'basic/ports', component: () => import('./pages/PortsPage.vue') },
        { path: 'basic/suppliers', component: () => import('./pages/SuppliersPage.vue') },
        { path: 'basic/suppliers/factories', component: () => import('./pages/FactoriesPage.vue') },
        { path: 'basic/suppliers/factories/:id', component: () => import('./pages/FactoryDetailPage.vue') },
        { path: 'basic/suppliers/:id', component: () => import('./pages/SupplierDetailPage.vue') },
        { path: 'products', component: () => import('./pages/ProductsPage.vue') },
        // SP1 只调整模块入口与页面视角：底层仍复用同一套询盘、寻源和报价数据。
        { path: 'sales/intakes', component: () => import('./pages/ProcurementIntakesPage.vue') },
        { path: 'sales/inquiries', component: () => import('./pages/SourcingCasesListPage.vue') },
        { path: 'sales/inquiries/:id', component: () => import('./pages/SourcingCaseDetailPage.vue') },
        { path: 'sales/quotations', component: () => import('./pages/QuotationsPage.vue') },
        { path: 'sales/settings/inquiry-templates', component: () => import('./pages/InquiryTemplatesPage.vue') },
        // 旧书签保留一个兼容版本，并把查询条件一并带到新入口。
        { path: 'quotations', redirect: (to) => ({ path: '/sales/quotations', query: to.query }) },
        { path: 'contracts', component: () => import('./pages/ContractsPage.vue') },
        { path: 'basic/excel-usage', component: () => import('./pages/ExcelUsagePage.vue') },
        { path: 'platform/tenants', component: () => import('./pages/PlatformTenantsPage.vue') },
        { path: 'contract-execution', component: () => import('./pages/ContractExecutionPage.vue') },
        { path: 'shipments', component: () => import('./pages/ShipmentsPage.vue') },
        { path: 'shipping', component: () => import('./pages/ShippingPage.vue') },
        { path: 'shipping/:id', component: () => import('./pages/ShippingDetailPage.vue') },
        { path: 'receipts', component: () => import('./pages/ReceiptsPage.vue') },
        { path: 'receivable-due', component: () => import('./pages/ReceivableDuePage.vue') },
        { path: 'stocks', component: () => import('./pages/StocksPage.vue') },
        { path: 'outbounds', component: () => import('./pages/OutboundsPage.vue') },
        { path: 'warehouses', component: () => import('./pages/WarehouseWorkbenchPage.vue') },
        { path: 'warehouses/profiles', component: () => import('./pages/WarehouseProfilesPage.vue') },
        { path: 'warehouses/arrivals', component: () => import('./pages/WarehouseArrivalsPage.vue') },
        { path: 'warehouses/receipts', component: () => import('./pages/WarehouseReceiptsPage.vue') },
        { path: 'warehouses/imports', component: () => import('./pages/WarehouseImportsPage.vue') },
        { path: 'warehouses/settings', component: () => import('./pages/WarehouseSettingsPage.vue') },
        { path: 'procurement', component: () => import('./pages/ProcurementPage.vue') },
        { path: 'procurement/intakes', redirect: (to) => ({ path: '/sales/intakes', query: to.query }) },
        { path: 'procurement/settings/inquiry-templates', redirect: (to) => ({ path: '/sales/settings/inquiry-templates', query: to.query }) },
        { path: 'procurement/sourcing/pending', component: () => import('./pages/SourcingCasesListPage.vue') },
        { path: 'procurement/sourcing', component: () => import('./pages/SourcingCasesListPage.vue') },
        { path: 'procurement/sourcing/:id', component: () => import('./pages/SourcingCaseDetailPage.vue') },
        { path: 'requirements', component: () => import('./pages/RequirementsPage.vue') },
        { path: 'sourcing-cases', redirect: (to) => ({ path: '/procurement/sourcing', query: to.query }) },
        { path: 'sourcing-cases/:id', redirect: (to) => ({ path: `/procurement/sourcing/${String(to.params.id)}`, query: to.query }) },
        { path: 'purchase-orders', component: () => import('./pages/PurchaseOrdersPage.vue') },
        { path: 'supplier-invoices', component: () => import('./pages/SupplierInvoicesPage.vue') },
        { path: 'supplier-payments', component: () => import('./pages/SupplierPaymentsPage.vue') },
        { path: 'supplier-statements', component: () => import('./pages/SupplierStatementsPage.vue') },
        { path: 'bank-transactions', component: () => import('./pages/BankTransactionsPage.vue') },
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
})
