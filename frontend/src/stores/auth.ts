import { defineStore } from 'pinia'
import { get, post, quietErrors } from '../api'

interface Employee {
  id: string
  name: string
  email: string
  departmentName: string
}

interface SessionProfile extends Employee {
  avatarUrl?: string
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

// Several browser signals can ask for a permission refresh at the same time
// (focus, visibility, the periodic check, or a 403 response). Share one
// request so returning to a tab never creates a burst against IAM.
let permissionRefreshInFlight: Promise<void> | null = null

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
    // 顶栏那个小头像的地址。**故意不进 localStorage**：它是带签名的短命
    // 链接，十几分钟就过期，存下来的下场是刷新之后一张裂图。每次进壳子的时候
    // 现取一遍，代价是一个请求。
    avatarUrl: '',
    permissions: JSON.parse(localStorage.getItem('permissions') ?? '[]') as string[],
    // Persisted so a refresh mid-obligation does not shake the debt off —
    // the shell keeps its blocking dialog up until the change happens.
    mustChangePassword: localStorage.getItem('mustChangePassword') === '1',
  }),
  getters: {
    isLoggedIn: (s) => s.employeeName !== '',
    can: (s) => (code: string) => s.permissions.includes(code),
    /** True when this person owns the document, i.e. may act on it. */
    owns: (s) => (ownerId: string | number) => {
      const normalizedOwner = String(ownerId ?? '')
      return normalizedOwner !== '' && normalizedOwner === String(s.employeeId)
    },
  },
  actions: {
    // account：用户名或邮箱，服务端按有没有 @ 分。
    async login(account: string, password: string) {
      // The login page renders authentication failures beside the form. Keep
      // the global interceptor quiet so a rejected password is not announced
      // twice (and so the page can translate the stable error code itself).
      // The response's Set-Cookie pair IS the session — nothing to store
      // here. accessToken no longer appears in the body at all, so there is
      // nothing a script hooked into fetch could have captured either.
      const data = await post<LoginData>('/auth/login', { account, password }, quietErrors)
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
    // Re-read the small permission list independently from the profile. The
    // router uses this before its first navigation, so a role change made
    // while the browser was closed cannot be overwritten by stale
    // localStorage and incorrectly send the user back to 我的待办.
    async refreshPermissionCodes() {
      if (!this.isLoggedIn) return
      if (permissionRefreshInFlight) return permissionRefreshInFlight
      const employeeID = this.employeeId
      permissionRefreshInFlight = (async () => {
        const data = await get<{ permissionCodes: string[] }>('/me/permissions', undefined, {
          ...quietErrors,
          // This request runs before the first route resolves. A short retry
          // budget keeps a temporarily unavailable IAM from holding the
          // entire application behind a blank screen.
          timeout: 5000,
        })
        // A slow response from an older session must not paint permissions
        // over a different person who signed in while it was in flight.
        if (!this.isLoggedIn || this.employeeId !== employeeID) return
        this.permissions = data.permissionCodes ?? []
        localStorage.setItem('permissions', JSON.stringify(this.permissions))
      })().finally(() => {
        permissionRefreshInFlight = null
      })
      return permissionRefreshInFlight
    },
    async refreshProfile() {
      if (!this.isLoggedIn) return
      const employeeID = this.employeeId
      const data = await get<{ profile: SessionProfile }>('/me/profile', undefined, quietErrors)
      if (!this.isLoggedIn || this.employeeId !== employeeID || !data.profile?.id) return
      const profile = data.profile
      this.employeeId = String(profile.id)
      this.employeeName = profile.name ?? ''
      this.employeeEmail = profile.email ?? ''
      this.employeeDepartment = profile.departmentName ?? ''
      this.avatarUrl = profile.avatarUrl ?? ''
      localStorage.setItem('employeeId', this.employeeId)
      localStorage.setItem('employeeName', this.employeeName)
      localStorage.setItem('employeeEmail', this.employeeEmail)
      localStorage.setItem('employeeDepartment', this.employeeDepartment)
    },
    // Permissions and the harmless identity cache are both refreshed on boot.
    // The cookie is the real session; localStorage may outlive an older login
    // and must never be trusted for an ownership decision such as who may sign
    // a contract. Each request fails independently so one unavailable endpoint
    // does not prevent the other cache from being repaired.
    async refreshPermissions() {
      if (!this.isLoggedIn) return
      await Promise.allSettled([this.refreshPermissionCodes(), this.refreshProfile()])
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
