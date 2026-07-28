import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: () => import('./pages/LoginPage.vue') },
    {
      path: '/',
      component: () => import('./pages/Shell.vue'),
      children: [
        { path: '', redirect: '/customers' },
        { path: 'todos', component: () => import('./pages/TodosPage.vue') },
        { path: 'customers', component: () => import('./pages/CustomersPage.vue') },
        { path: 'products', component: () => import('./pages/ProductsPage.vue') },
        { path: 'quotations', component: () => import('./pages/QuotationsPage.vue') },
        { path: 'fx', component: () => import('./pages/FxPage.vue') },
        { path: 'settings/employees', component: () => import('./pages/EmployeesPage.vue') },
        { path: 'settings/roles', component: () => import('./pages/RolesPage.vue') },
      ],
    },
  ],
})

router.beforeEach((to) => {
  const loggedIn = localStorage.getItem('token') !== null
  if (!loggedIn && to.path !== '/login') return '/login'
  if (loggedIn && to.path === '/login') return '/'
})
