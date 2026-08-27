import { describe, expect, it } from 'vitest'
import { newIdempotencySession, withIdempotency } from './idempotency'

describe('防重键的表单会话', () => {
  it('失败重试拿到同一个键——这是「重试不开第二张单」的前提', () => {
    const s = newIdempotencySession()
    const first = s.take()
    expect(first).toBeTruthy()
    // 提交失败，用户再点一次：还是同一个键，网关才认得出是同一次操作。
    expect(s.take()).toBe(first)
    expect(s.take()).toBe(first)
  })

  it('成功 reset 之后换新键——否则下一张真心要开的单会被重放挡住', () => {
    const s = newIdempotencySession()
    const first = s.take()
    s.reset()
    const second = s.take()
    expect(second).toBeTruthy()
    expect(second).not.toBe(first)
  })

  it('两个会话互不相干——登记流水和核销各是各的操作', () => {
    expect(newIdempotencySession().take()).not.toBe(newIdempotencySession().take())
  })

  it('withIdempotency 把键放进请求头，且不覆盖调用方自己的 cfg', () => {
    const s = newIdempotencySession()
    const cfg = withIdempotency(s, { timeout: 5000, headers: { 'X-Custom': 'a' } })
    expect(cfg.timeout).toBe(5000)
    expect((cfg.headers as Record<string, string>)['X-Custom']).toBe('a')
    expect((cfg.headers as Record<string, string>)['Idempotency-Key']).toBe(s.take())
  })
})
