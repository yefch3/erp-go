import { defineStore } from 'pinia'
import { post } from '../api'

interface Employee {
  id: string
  name: string
  departmentName: string
}

interface LoginData {
  accessToken: string
  expiresInSeconds: string
  employee: Employee
  permissionCodes: string[]
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    token: localStorage.getItem('token') ?? '',
    employeeName: localStorage.getItem('employeeName') ?? '',
    permissions: JSON.parse(localStorage.getItem('permissions') ?? '[]') as string[],
  }),
  getters: {
    isLoggedIn: (s) => s.token !== '',
    can: (s) => (code: string) => s.permissions.includes(code),
  },
  actions: {
    async login(username: string, password: string) {
      const data = await post<LoginData>('/auth/login', { username, password })
      this.token = data.accessToken
      this.employeeName = data.employee.name
      this.permissions = data.permissionCodes
      localStorage.setItem('token', data.accessToken)
      localStorage.setItem('employeeName', data.employee.name)
      localStorage.setItem('permissions', JSON.stringify(data.permissionCodes))
    },
    logout() {
      this.$reset()
      localStorage.removeItem('token')
      localStorage.removeItem('employeeName')
      localStorage.removeItem('permissions')
    },
  },
})
