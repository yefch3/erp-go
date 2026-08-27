import { defineStore } from 'pinia'
import { get, post, quietErrors } from '../api'

interface Employee {
  id: string
  name: string
  email: string
  departmentName: string
}

interface LoginData {
  accessToken: string
  expiresInSeconds: string
  employee: Employee
  permissionCodes: string[]
  // True when the password that just worked was typed by an administrator,
  // not chosen by this person. The session is real but owes an immediate
  // change; the shell blocks everything else until it happens.
  mustChangePassword?: boolean
}

export const useAuthStore = defineStore('auth', {
  state: () => ({
    // No token here, and none anywhere script can reach: the credential is
    // an httpOnly cookie the browser manages alone. What the page keeps is
    // the harmless half of a session — who is signed in, for greetings and
    // guards; the API answers 401 either way if the cookie is gone.
    // Needed to tell "my documents" from "documents I can see". The data
    // scope decides what is listed; ownership decides what can be acted on,
    // and the two are deliberately not the same set.
    employeeId: localStorage.getItem('employeeId') ?? '',
    employeeName: localStorage.getItem('employeeName') ?? '',
    employeeDepartment: localStorage.getItem('employeeDepartment') ?? '',
    // The address they signed in with. The mailbox gate shows it rather than
    // asking, because the mailbox somebody binds is the one they signed in as.
    employeeEmail: localStorage.getItem('employeeEmail') ?? '',
    permissions: JSON.parse(localStorage.getItem('permissions') ?? '[]') as string[],
    // Persisted so a refresh mid-obligation does not shake the debt off —
    // the shell keeps its blocking dialog up until the change happens.
    mustChangePassword: localStorage.getItem('mustChangePassword') === '1',
  }),
  getters: {
    isLoggedIn: (s) => s.employeeName !== '',
    can: (s) => (code: string) => s.permissions.includes(code),
    /** True when this person owns the document, i.e. may act on it. */
    owns: (s) => (ownerId: string) => ownerId !== '' && ownerId === s.employeeId,
  },
  actions: {
    async login(email: string, password: string) {
      // The login page renders authentication failures beside the form. Keep
      // the global interceptor quiet so a rejected password is not announced
      // twice (and so the page can translate the stable error code itself).
      // The response's Set-Cookie pair IS the session — nothing to store
      // here. accessToken no longer appears in the body at all, so there is
      // nothing a script hooked into fetch could have captured either.
      const data = await post<LoginData>('/auth/login', { email, password }, quietErrors)
      this.employeeId = data.employee.id
      this.employeeName = data.employee.name
      this.employeeEmail = data.employee.email ?? ''
      this.employeeDepartment = data.employee.departmentName ?? ''
      this.permissions = data.permissionCodes
      this.mustChangePassword = data.mustChangePassword === true
      localStorage.setItem('employeeId', data.employee.id)
      localStorage.setItem('employeeName', data.employee.name)
      localStorage.setItem('employeeEmail', data.employee.email ?? '')
      localStorage.setItem('employeeDepartment', data.employee.departmentName ?? '')
      localStorage.setItem('permissions', JSON.stringify(data.permissionCodes))
      if (this.mustChangePassword) {
        localStorage.setItem('mustChangePassword', '1')
      } else {
        localStorage.removeItem('mustChangePassword')
      }
    },
    // The moment the owner chooses their own password, the debt is paid.
    passwordChanged() {
      this.mustChangePassword = false
      localStorage.removeItem('mustChangePassword')
    },
    // Permission codes are cached in localStorage so the first paint is not
    // gated on a round trip, but a cache that only refills at login goes
    // stale the moment an administrator changes a role — or the moment codes
    // are renamed, as mail:* were. Refreshing on boot means a permission
    // change takes effect on the next page load instead of the next login.
    // Failure is silent on purpose: the cached list still works, and the API
    // is the real gate either way.
    async refreshPermissions() {
      if (!this.isLoggedIn) return
      try {
        const data = await get<{ permissionCodes: string[] }>('/me/permissions')
        this.permissions = data.permissionCodes ?? []
        localStorage.setItem('permissions', JSON.stringify(this.permissions))
      } catch {
        /* keep what we have; every API call is still checked server-side */
      }
    },
    logout() {
      // The server must clear the cookie — httpOnly means this code cannot.
      // Fire-and-forget: whatever happens to the request, the local half of
      // the session is gone and the router sends the person to the login
      // page; a cookie that survived a network blip is invalid to keep using
      // anyway once a different account signs in over it.
      post('/auth/logout', {}, quietErrors).catch(() => {})
      this.$reset()
      localStorage.removeItem('employeeId')
      localStorage.removeItem('employeeName')
      localStorage.removeItem('employeeEmail')
      localStorage.removeItem('employeeDepartment')
      localStorage.removeItem('permissions')
      localStorage.removeItem('mustChangePassword')
    },
  },
})
