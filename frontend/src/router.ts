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
        { path: '', redirect: '/customers' },
        { path: 'todos', component: () => import('./pages/TodosPage.vue') },
        { path: 'customers', component: () => import('./pages/CustomersPage.vue') },
        { path: 'products', component: () => import('./pages/ProductsPage.vue') },
        { path: 'quotations', component: () => import('./pages/QuotationsPage.vue') },
        { path: 'contracts', component: () => import('./pages/ContractsPage.vue') },
        { path: 'shipments', component: () => import('./pages/ShipmentsPage.vue') },
        { path: 'shipping', component: () => import('./pages/ShippingPage.vue') },
        { path: 'shipping/:id', component: () => import('./pages/ShippingDetailPage.vue') },
        { path: 'receipts', component: () => import('./pages/ReceiptsPage.vue') },
        { path: 'stocks', component: () => import('./pages/StocksPage.vue') },
        { path: 'outbounds', component: () => import('./pages/OutboundsPage.vue') },
        { path: 'requirements', component: () => import('./pages/RequirementsPage.vue') },
        { path: 'purchase-orders', component: () => import('./pages/PurchaseOrdersPage.vue') },
        { path: 'emails', component: () => import('./pages/EmailsPage.vue') },
        { path: 'team-mail', component: () => import('./pages/TeamMailPage.vue') },
        { path: 'mail/export-log', component: () => import('./pages/MailExportLogPage.vue') },
        { path: 'fx', component: () => import('./pages/FxPage.vue') },
        { path: 'settings/employees', component: () => import('./pages/EmployeesPage.vue') },
        { path: 'settings/roles', component: () => import('./pages/RolesPage.vue') },
        { path: 'settings/approvals', component: () => import('./pages/ApprovalFlowsPage.vue') },
        { path: 'settings/signatures', component: () => import('./pages/EmailSignaturesPage.vue') },
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
