// 文件中转：浏览器把文件交给我们的服务器，由服务器转存到对象存储；读取同理。
//
// 系统里的附件原本是「直传」：先向业务服务要一个签名地址，浏览器自己把文件
// PUT 进对象存储。2026-11-09 天津办公室的 F12 给出了直传在国内的真相：
// `net::ERR_CONNECTION_TIMED_OUT`——国内到美国 S3 的连接时通时不通；而到本站
// 的连接他们每天都在用，一直是通的。桶里从上线到那天，国内员工直传成功的次
// 数是 0（质检资料、报销凭证各 0 份）；走服务器中转的供应商资料却都传上去了。
//
// 所以这里不自己去存储，而是把文件连同签名地址一起交给网关 /api/storage/relay，
// 网关替浏览器连存储。业务服务不用改：页面看到的还是「签地址 → 登记」两步，
// 只是中间那一跳换了个走法。
//
// 签名地址放在请求头而不是 query：query 会原样进 nginx 的 access log。
import type { AxiosRequestConfig } from 'axios'

import { http, saveBlob } from '../api'

export const RELAY_PATH = '/storage/relay'
export const RELAY_HEADER = 'X-Storage-Url'

// 跨太平洋传一份几十 MB 的扫描件，api.ts 那 15 秒的默认预算远远不够。网关自己
// 对存储那一跳另有上限；这里只管浏览器到网关的路。
export const RELAY_UPLOAD_TIMEOUT_MS = 10 * 60 * 1000
export const RELAY_DOWNLOAD_TIMEOUT_MS = 5 * 60 * 1000

/** 和 axios 实例对得上的最小接口；测试里换成替身。 */
export interface RelayTransport {
  put: (url: string, body: Blob, cfg: AxiosRequestConfig) => Promise<unknown>
  get: (url: string, cfg: AxiosRequestConfig) => Promise<{ data: Blob }>
}

const live: RelayTransport = {
  put: (url, body, cfg) => http.put(url, body, cfg),
  get: (url, cfg) => http.get<Blob>(url, cfg),
}

/**
 * 把一份文件经网关送进签名地址指向的位置。
 *
 * 任一步失败都抛出——拒绝的信封、网络错误原样往上冒，调用方决定怎么跟人说
 * （用 relayFailureReason 取原因）。quiet 是因为调用方要自己说：这一处的失败
 * 要连着文件名一起讲，api.ts 那个通用 toast 做不到。
 */
export async function uploadViaRelay(
  uploadUrl: string,
  file: Blob,
  transport: RelayTransport = live,
): Promise<void> {
  if (!uploadUrl) throw new Error('缺少上传地址')
  await transport.put(RELAY_PATH, file, {
    headers: { [RELAY_HEADER]: uploadUrl, 'Content-Type': file.type || 'application/octet-stream' },
    timeout: RELAY_UPLOAD_TIMEOUT_MS,
    quiet: true,
  })
}

/** 经网关取回签名地址指向的文件。 */
export async function fetchViaRelay(url: string, transport: RelayTransport = live): Promise<Blob> {
  if (!url) throw new Error('缺少文件地址')
  const resp = await transport.get(RELAY_PATH, {
    headers: { [RELAY_HEADER]: url },
    responseType: 'blob',
    timeout: RELAY_DOWNLOAD_TIMEOUT_MS,
    quiet: true,
  })
  return resp.data
}

/**
 * 取回文件并交给用户保存。
 *
 * 从前是 `<a href target=_blank>` 直接开存储的地址；中转需要带请求头，浏览器
 * 的链接跳转带不了，所以改成先取字节再「另存为」。名字由调用方给——存储里的
 * 键是随机串，页面知道的那个才是人认得的名字。
 */
export async function saveViaRelay(
  url: string,
  fileName: string,
  transport: RelayTransport = live,
  save: (blob: Blob, fileName: string) => void = saveBlob,
): Promise<void> {
  const blob = await fetchViaRelay(url, transport)
  save(blob, fileName || '文件')
}

/**
 * 失败里能给人看的那句话。
 *
 * api.ts 的拦截器把服务端的信封 `{ success:false, code, message }` 原样 reject
 * 出来，网络层的错误则是 Error；两种都有 message。什么都没有就返回空串，由
 * 调用方决定兜底文案。
 */
export function relayFailureReason(err: unknown): string {
  if (err && typeof err === 'object' && 'message' in err) {
    const message = (err as { message?: unknown }).message
    if (typeof message === 'string') return message.trim()
  }
  return ''
}

/**
 * 是不是「等到超时」的那种失败。
 *
 * 一批文件里有一份等满了时限，多半是整条路不通；调用方据此停下，不让后面每一
 * 份都再等一轮。axios 的超时带 code ECONNABORTED（开了 clarifyTimeoutError 的
 * 版本是 ETIMEDOUT）。服务端回了话的失败——哪怕是 502——不算：那是很快就知道的。
 */
export function relayTimedOut(err: unknown): boolean {
  if (!err || typeof err !== 'object' || !('code' in err)) return false
  const code = (err as { code?: unknown }).code
  return code === 'ECONNABORTED' || code === 'ETIMEDOUT'
}
