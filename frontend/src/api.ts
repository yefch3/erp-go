import axios from 'axios'
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

http.interceptors.request.use((cfg) => {
  const token = localStorage.getItem('token')
  if (token) cfg.headers.Authorization = `Bearer ${token}`
  // The mailbox unlock proof. sessionStorage on purpose: closing the browser
  // locks the mailbox again, which is the behaviour a lock should have.
  const unlock = sessionStorage.getItem('mailUnlock')
  if (unlock) cfg.headers['X-Mail-Unlock'] = unlock
  return cfg
})

http.interceptors.response.use(
  (resp) => {
    const env = resp.data as Envelope<unknown>
    if (!env.success) {
      ElMessage.error(env.message || i18n.global.t('common.requestFailed'))
      return Promise.reject(env)
    }
    return resp
  },
  (err) => {
    const env = err.response?.data as Envelope<unknown> | undefined
    if (err.response?.status === 401) {
      localStorage.removeItem('token')
      router.push('/login')
    }
    // The unlock expired server-side; holding the dead token would keep
    // every mail request failing quietly. Dropping it makes the gate
    // reappear on the next visit to the mailbox.
    if (env?.code === 'MAIL_LOCKED') {
      sessionStorage.removeItem('mailUnlock')
    }
    ElMessage.error(env?.message || err.message || i18n.global.t('common.networkError'))
    return Promise.reject(env ?? err)
  },
)

// 便捷方法：直接取信封里的 data
export async function get<T>(url: string, params?: object): Promise<T> {
  const resp = await http.get<Envelope<T>>(url, { params })
  return resp.data.data as T
}

export async function post<T>(url: string, body?: object): Promise<T> {
  const resp = await http.post<Envelope<T>>(url, body)
  return resp.data.data as T
}

export async function put<T>(url: string, body?: object): Promise<T> {
  const resp = await http.put<Envelope<T>>(url, body)
  return resp.data.data as T
}

export async function del<T>(url: string): Promise<T> {
  const resp = await http.delete<Envelope<T>>(url)
  return resp.data.data as T
}
