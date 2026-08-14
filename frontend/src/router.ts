import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: () => import('./pages/LoginPage.vue') },
    // Reached from a mail, by somebody who has no account yet — having one is
    // what they are here to arrange. Outside the shell and outside the guard.
    { path: '/activate', component: () => import('./pages/ActivatePage.vue') },
    {
      path: '/',
      component: () => import('./pages/Shell.vue'),
      children: [
        { path: '', redirect: '/basic/customers' },
        { path: 'todos', component: () => import('./pages/TodosPage.vue') },
        { path: 'customers', redirect: '/basic/customers' },
        { path: 'basic/customers', component: () => import('./pages/CustomersPage.vue') },
        { path: 'basic/customers/:id', component: () => import('./pages/CustomerDetailPage.vue') },
        { path: 'basic/employees', component: () => import('./pages/EmployeesPage.vue') },
        { path: 'basic/employees/departments', component: () => import('./pages/DepartmentsPage.vue') },
        { path: 'basic/employees/roles', component: () => import('./pages/RolesPage.vue') },
        { path: 'basic/ports', component: () => import('./pages/PortsPage.vue') },
        { path: 'basic/suppliers', component: () => import('./pages/BasicDataPlaceholderPage.vue'), meta: { titleKey: 'basicData.suppliersPending' } },
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
        { path: 'requirements', component: () => import('./pages/RequirementsPage.vue') },
        { path: 'sourcing-cases', component: () => import('./pages/SourcingCasesPage.vue') },
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
  const loggedIn = localStorage.getItem('token') !== null
  // Activation is the one page whose whole audience is logged out and has to
  // stay that way: sending them to /login would hide the only link they hold.
  if (to.path === '/activate') return true
  // Where they were trying to go travels with them, so opening a bookmarked
  // mail on a dead session lands on that mail after signing in rather than on
  // the home page with the reason forgotten.
  if (!loggedIn && to.path !== '/login') {
    return { path: '/login', query: to.fullPath === '/' ? {} : { redirect: to.fullPath } }
  }
  if (loggedIn && to.path === '/login') return '/'
})
