import { describe, expect, it } from 'vitest'

import {
  BACKOFF_MS,
  afterFailure,
  afterSuccess,
  backoffMs,
  idleGate,
  mayAttempt,
  shouldWarn,
} from './refreshBackoff'

describe('backoffMs', () => {
  it('按次数一级一级往上退', () => {
    expect(backoffMs(1)).toBe(5_000)
    expect(backoffMs(2)).toBe(15_000)
    expect(backoffMs(3)).toBe(30_000)
    expect(backoffMs(4)).toBe(60_000)
    expect(backoffMs(5)).toBe(120_000)
  })

  // 到顶之后一直用最后那个值，不是继续翻倍。两分钟以上人会以为它又死了。
  it('到顶之后不再往上涨', () => {
    expect(backoffMs(6)).toBe(120_000)
    expect(backoffMs(50)).toBe(120_000)
    expect(backoffMs(9999)).toBe(120_000)
  })

  it('没失败过就不用等', () => {
    expect(backoffMs(0)).toBe(0)
    expect(backoffMs(-1)).toBe(0)
  })
})

describe('mayAttempt', () => {
  it('没失败过随时可以试', () => {
    expect(mayAttempt(idleGate(), 1_000)).toBe(true)
  })

  // 这一条是整个修复的核心：**退避窗口过完之后必须能再试**。
  // 从前的代码在这里永远返回 false——那就是「一直没数据」的根子。
  it('等够了就能再试', () => {
    const gate = afterFailure(idleGate(), 1_000)
    expect(mayAttempt(gate, 1_000)).toBe(false)
    expect(mayAttempt(gate, 5_999)).toBe(false)
    expect(mayAttempt(gate, 6_000)).toBe(true)
    expect(mayAttempt(gate, 60_000)).toBe(true)
  })
})

describe('连续失败再恢复', () => {
  it('一路失败下去，等待越来越长', () => {
    let gate = idleGate()
    let now = 0
    const waits: number[] = []
    for (let i = 0; i < 6; i++) {
      const before = now
      gate = afterFailure(gate, now)
      waits.push(gate.nextAttemptAt - before)
      now = gate.nextAttemptAt
    }
    expect(waits).toEqual([...BACKOFF_MS, 120_000])
  })

  // 成功一次就彻底归零，下一次抖动重新从 5 秒开始——而不是接着上一轮的
  // 两分钟。网络好了之后还罚人等两分钟，是这类退避最常见的写错方式。
  it('成功一次就回到原状', () => {
    let gate = idleGate()
    for (let i = 0; i < 5; i++) gate = afterFailure(gate, i * 1_000)
    expect(gate.failures).toBe(5)

    gate = afterSuccess()
    expect(gate.failures).toBe(0)
    expect(mayAttempt(gate, 0)).toBe(true)

    gate = afterFailure(gate, 0)
    expect(gate.nextAttemptAt).toBe(5_000)
  })
})

describe('shouldWarn', () => {
  // 只在第一次失败时弹。网络不好的那半小时里，人不该收到一串一模一样的红条。
  it('第一次失败弹，之后不弹', () => {
    let gate = afterFailure(idleGate(), 0)
    expect(shouldWarn(gate)).toBe(true)
    gate = afterFailure(gate, 0)
    expect(shouldWarn(gate)).toBe(false)
    gate = afterFailure(gate, 0)
    expect(shouldWarn(gate)).toBe(false)
  })

  it('没失败时不弹', () => {
    expect(shouldWarn(idleGate())).toBe(false)
  })

  // 恢复之后再坏，要重新提醒一次——那是一轮新的故障，人有权知道。
  it('恢复之后再坏会重新提醒', () => {
    let gate = afterFailure(idleGate(), 0)
    gate = afterFailure(gate, 0)
    expect(shouldWarn(gate)).toBe(false)
    gate = afterSuccess()
    gate = afterFailure(gate, 0)
    expect(shouldWarn(gate)).toBe(true)
  })
})
