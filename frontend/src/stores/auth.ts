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
    // Needed to tell "my documents" from "documents I can see". The data
    // scope decides what is listed; ownership decides what can be acted on,
    // and the two are deliberately not the same set.
    employeeId: localStorage.getItem('employeeId') ?? '',
    employeeName: localStorage.getItem('employeeName') ?? '',
    permissions: JSON.parse(localStorage.getItem('permissions') ?? '[]') as string[],
  }),
  getters: {
    isLoggedIn: (s) => s.token !== '',
    can: (s) => (code: string) => s.permissions.includes(code),
    /** True when this person owns the document, i.e. may act on it. */
    owns: (s) => (ownerId: string) => ownerId !== '' && ownerId === s.employeeId,
  },
  actions: {
    async login(username: string, password: string) {
      const data = await post<LoginData>('/auth/login', { username, password })
      this.token = data.accessToken
      this.employeeId = data.employee.id
      this.employeeName = data.employee.name
      this.permissions = data.permissionCodes
      localStorage.setItem('token', data.accessToken)
      localStorage.setItem('employeeId', data.employee.id)
      localStorage.setItem('employeeName', data.employee.name)
      localStorage.setItem('permissions', JSON.stringify(data.permissionCodes))
    },
    logout() {
      this.$reset()
      localStorage.removeItem('token')
      localStorage.removeItem('employeeId')
      localStorage.removeItem('employeeName')
      localStorage.removeItem('permissions')
    },
  },
})
