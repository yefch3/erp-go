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
        { path: 'customers', redirect: '/basic/customers' },
        { path: 'basic/customers', component: () => import('./pages/CustomersPage.vue') },
        { path: 'basic/customers/:id', component: () => import('./pages/CustomerDetailPage.vue') },
        { path: 'basic/employees', component: () => import('./pages/EmployeesPage.vue') },
        { path: 'basic/employees/departments', component: () => import('./pages/DepartmentsPage.vue') },
        { path: 'basic/employees/roles', component: () => import('./pages/RolesPage.vue') },
        { path: 'basic/ports', component: () => import('./pages/PortsPage.vue') },
        { path: 'basic/suppliers', component: () => import('./pages/SuppliersPage.vue') },
        { path: 'basic/suppliers/factories', component: () => import('./pages/FactoriesPage.vue') },
        { path: 'basic/suppliers/factories/:id', component: () => import('./pages/FactoryDetailPage.vue') },
        { path: 'basic/suppliers/:id', component: () => import('./pages/SupplierDetailPage.vue') },
        { path: 'products', component: () => import('./pages/ProductsPage.vue') },
        { path: 'quotations', component: () => import('./pages/QuotationsPage.vue') },
        { path: 'contracts', component: () => import('./pages/ContractsPage.vue') },
        { path: 'shipments', component: () => import('./pages/ShipmentsPage.vue') },
        { path: 'shipping', component: () => import('./pages/ShippingPage.vue') },
        { path: 'shipping/:id', component: () => import('./pages/ShippingDetailPage.vue') },
        { path: 'receipts', component: () => import('./pages/ReceiptsPage.vue') },
        { path: 'stocks', component: () => import('./pages/StocksPage.vue') },
        { path: 'outbounds', component: () => import('./pages/OutboundsPage.vue') },
        { path: 'procurement', component: () => import('./pages/ProcurementPage.vue') },
        { path: 'procurement/intakes', component: () => import('./pages/ProcurementIntakesPage.vue') },
        { path: 'procurement/settings/inquiry-templates', component: () => import('./pages/InquiryTemplatesPage.vue') },
        { path: 'requirements', component: () => import('./pages/RequirementsPage.vue') },
        { path: 'sourcing-cases', component: () => import('./pages/SourcingCasesListPage.vue') },
        { path: 'sourcing-cases/:id', component: () => import('./pages/SourcingCaseDetailPage.vue') },
        { path: 'purchase-orders', component: () => import('./pages/PurchaseOrdersPage.vue') },
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
