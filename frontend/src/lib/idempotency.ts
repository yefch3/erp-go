// HTTP 写入的防重键（配网关的 Idempotency-Key 中间件）。
//
// 一个键代表**一次表单会话**：对话框打开时领一个，提交失败重试还是它——
// 于是「响应丢在路上、用户再点一次」拿回的是第一次的结果，不会开出第二张单。
// 提交**成功后必须换新键**，不然下一次真心想再开一张时会被重放挡住。
//
// 键在提交那一刻才取（takeIdempotencyKey），不在打开对话框时生成：这样没有
// 「忘了重置」这种状态可言——同一次提交重试拿同一个键，成功后 reset。

import type { AxiosRequestConfig } from 'axios'

/** 一次表单会话的防重键。用法见 ReceiptsPage 的 submitRecord。 */
export interface IdempotencySession {
  /** 取当前键（第一次调用时生成）。失败重试会拿到同一个。 */
  take(): string
  /** 提交成功后调用：下一次提交是新的操作，配新键。 */
  reset(): void
}

// randomKey 优先用 crypto.randomUUID，没有就退回时间戳加随机数。
//
// 兜底不是给测试环境开的方便之门：crypto.randomUUID **只在 HTTPS 页面存在**，
// 内网用 http 直接打开时它是 undefined——没有兜底，登记流水、开合同这些提交
// 会当场抛错。防重键不需要密码学强度，只要「同一个人短时间内不撞车」，
// 时间戳（纳秒级）加两段随机数绰绰有余。
function randomKey(): string {
  const c = globalThis.crypto
  if (c && typeof c.randomUUID === 'function') return c.randomUUID()
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`
}

export function newIdempotencySession(): IdempotencySession {
  let key = ''
  return {
    take() {
      if (!key) key = randomKey()
      return key
    },
    reset() {
      key = ''
    },
  }
}

/** 拼进 post() 的 cfg：`await post(url, body, withIdempotency(session))` */
export function withIdempotency(s: IdempotencySession, cfg?: AxiosRequestConfig): AxiosRequestConfig {
  return { ...cfg, headers: { ...(cfg?.headers as object), 'Idempotency-Key': s.take() } }
}
