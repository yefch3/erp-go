import axios, { type AxiosRequestConfig } from 'axios'
import { ElMessage } from 'element-plus'
import { i18n } from './i18n'
import { router } from './router'

// 后端统一信封：{ success, data } 或 { success:false, code, message }
export interface Envelope<T> {
  success: boolean
  data?: T
  code?: string
  message?: string
}

export const http = axios.create({ baseURL: '/api', timeout: 15000 })

// Anything that talks to the mail host needs its own budget. Fifteen seconds
// is a sensible ceiling for a database query and far too short for an IMAP
// round trip: opening a folder, searching it and fetching new mail is normal
// work that regularly runs past it, and the service itself is allowed 90
// seconds (MAIL_SYNC_TIMEOUT). Giving up first produced "timeout of 15000ms
// exceeded" on a sync that was going perfectly well — and, worse, went on
// finishing on the server after the browser had called it failed.
export const mailHostRequest: AxiosRequestConfig = { timeout: 120000 }

// The model service itself may use the full two-minute backend deadline.
// Leave transport time for the gateway and gRPC response instead of having
// the browser abandon a conversion just before the server can return it.
export const mailExcelRequest: AxiosRequestConfig = { timeout: 150000 }

// Opt out of the automatic error toast, for calls whose failure the caller
// shows in place.
//
// Toasting every failure is right for the ordinary case and wrong for the
// sign-in gate, which catches its own failures and prints them next to the
// field that produced them. Both fired, so a wrong code was answered twice —
// and, worse, an error a caller had deliberately swallowed still reached the
// screen anyway.
//
// A config property rather than a header: this is a decision about our own UI
// and has no business travelling to the server.
declare module 'axios' {
  interface AxiosRequestConfig {
    quiet?: boolean
  }
}
export const quietErrors: AxiosRequestConfig = { quiet: true }

function shouldToast(cfg?: AxiosRequestConfig): boolean {
  return !cfg?.quiet
}

// The login token no longer passes through here — it lives in an httpOnly
// cookie the browser attaches on its own, which is the point: code that
// cannot see the token cannot leak it, and "code" includes anything a
// hostile mail might have managed to run.
//
// What the page does still owe is the CSRF echo: the erp_csrf cookie is
// script-readable ON PURPOSE, and repeating its value in a header on every
// state-changing request is what proves the request was made by this page
// rather than by some other site borrowing the browser's cookies.
function cookieValue(name: string): string {
  const m = document.cookie.match(new RegExp('(?:^|;\\s*)' + name + '=([^;]*)'))
  return m ? decodeURIComponent(m[1]) : ''
}

const MUTATING = new Set(['post', 'put', 'patch', 'delete'])

http.interceptors.request.use((cfg) => {
  if (MUTATING.has((cfg.method ?? 'get').toLowerCase())) {
    const csrf = cookieValue('erp_csrf')
    if (csrf) cfg.headers['X-CSRF-Token'] = csrf
  }
  // The mailbox unlock proof. localStorage, same as before: the real
  // boundaries are the server-side 12-hour expiry and 退出邮箱, which
  // revokes the token immediately.
  const unlock = localStorage.getItem('mailUnlock')
  if (unlock) cfg.headers['X-Mail-Unlock'] = unlock
  return cfg
})

// Renewal needs nothing from this file any more. The gateway extends a
// half-spent session with a Set-Cookie on whatever response was already on
// its way — a channel no script can read, which closed the last place a
// fresh token used to be visible to page code (the old X-Renewed-Token
// header was readable by anything that had hooked XMLHttpRequest).

http.interceptors.response.use(
  (resp) => {
    // A download is not an envelope. Everything else this API returns is
    // { success, data }; an exported conversation is the document itself, and
    // checking .success on a Blob finds undefined and rejects a response that
    // arrived perfectly.
    if (resp.config.responseType === 'blob') return resp
    const env = resp.data as Envelope<unknown>
    if (!env.success) {
      if (shouldToast(resp.config)) {
        ElMessage.error(env.message || i18n.global.t('common.requestFailed'))
      }
      return Promise.reject(env)
    }
    return resp
  },
  async (err) => {
    let env = err.response?.data as Envelope<unknown> | undefined
    // A download that failed still failed with an envelope — the server does
    // not know yet that it is about to write bytes. responseType turned it
    // into a Blob on the way in, and without reading it back the toast would
    // say "Request failed with status code 403" where the server had written
    // 请先验证邮箱授权码.
    if (env instanceof Blob) {
      try {
        env = JSON.parse(await env.text()) as Envelope<unknown>
      } catch {
        env = undefined
      }
    }
    if (err.response?.status === 401) {
      expired()
      return Promise.reject(env ?? err)
    }
    // The unlock expired server-side; holding the dead token would keep
    // every mail request failing quietly. Dropping it and saying so lets the
    // mailbox put its sign-in gate back up in place of the list, which is the
    // screen that can actually do something about it.
    if (env?.code === 'MAIL_LOCKED') {
      localStorage.removeItem('mailUnlock')
      window.dispatchEvent(new CustomEvent('mail-locked'))
      return Promise.reject(env ?? err)
    }
    if (shouldToast(err.config)) {
      ElMessage.error(env?.message || err.message || i18n.global.t('common.networkError'))
    }
    return Promise.reject(env ?? err)
  },
)

// The session is over: go to the login page instead of announcing it.
//
// A toast reading "登录凭证无效或已过期" on top of a page that can no longer
// load anything is a notification about a problem the reader cannot act on
// where they are standing. The login form is both the message and the remedy,
// so it is what they get; where they were is remembered so signing back in
// returns them there rather than to the home page.
//
// Guarded because an expiry usually arrives as a burst — every request the
// page had in flight fails at once, and each one would otherwise try to
// navigate.
let redirecting = false
function expired() {
  // The signed-in signal, not the credential: the credential is an httpOnly
  // cookie this code cannot touch, already refused server-side. What must go
  // is everything that makes the router and the pages believe a session
  // still exists.
  for (const k of ['employeeId', 'employeeName', 'employeeEmail', 'permissions', 'mailUnlock']) {
    localStorage.removeItem(k)
  }
  if (redirecting || location.pathname === '/login') return
  redirecting = true
  // The address bar, not router.currentRoute. An expiry is usually detected by
  // a request the page fired as it loaded, and at that moment the router may
  // still be resolving its first navigation and reporting "/" — which would
  // lose the very page the person was opening. The URL is already correct.
  const from = location.pathname + location.search
  router
    .push({ path: '/login', query: from === '/' ? {} : { redirect: from } })
    .finally(() => {
      redirecting = false
    })
}

// 便捷方法：直接取信封里的 data
export async function get<T>(url: string, params?: object, cfg?: AxiosRequestConfig): Promise<T> {
  const resp = await http.get<Envelope<T>>(url, { ...cfg, params })
  return resp.data.data as T
}

export async function post<T>(url: string, body?: object, cfg?: AxiosRequestConfig): Promise<T> {
  const resp = await http.post<Envelope<T>>(url, body, cfg)
  return resp.data.data as T
}

export async function put<T>(url: string, body?: object, cfg?: AxiosRequestConfig): Promise<T> {
  const resp = await http.put<Envelope<T>>(url, body, cfg)
  return resp.data.data as T
}

export async function del<T>(url: string, body?: object): Promise<T> {
  const resp = await http.delete<Envelope<T>>(url, { data: body })
  return resp.data.data as T
}

export interface Downloaded {
  blob: Blob
  fileName: string
  text: () => Promise<string>
}

/**
 * A file, fetched rather than linked.
 *
 * `<a href download>` would be simpler and cannot be used: the mailbox unlock
 * travels in a header, and a browser navigation carries no headers of ours.
 * So the page fetches the bytes and hands them to the user itself.
 */
export async function download(url: string, params?: object): Promise<Downloaded> {
  const resp = await http.get<Blob>(url, { params, responseType: 'blob' })
  return {
    blob: resp.data,
    fileName: fileNameFrom(String(resp.headers['content-disposition'] ?? '')),
    text: () => resp.data.text(),
  }
}

/**
 * The name the server gave the file.
 *
 * RFC 6266 sends it twice — `filename*` percent-encoded as UTF-8 for anything
 * modern, `filename` in plain ASCII as the fallback. The extended form is the
 * one that still says 邮件会话 rather than ____, so it is read first.
 */
export function fileNameFrom(disposition: string): string {
  const extended = /filename\*=UTF-8''([^;]+)/i.exec(disposition)
  if (extended) {
    try {
      return decodeURIComponent(extended[1].trim())
    } catch {
      /* a malformed encoding falls through to the plain form */
    }
  }
  const plain = /filename="?([^";]+)"?/i.exec(disposition)
  return plain ? plain[1].trim() : ''
}

/** Hand a fetched file to the user as a download. */
export function saveBlob(blob: Blob, fileName: string) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = fileName
  document.body.appendChild(a)
  a.click()
  a.remove()
  // Revoked on the next tick rather than immediately: Safari has not finished
  // reading the object URL when click() returns, and freeing it there gives a
  // zero-byte file.
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}
