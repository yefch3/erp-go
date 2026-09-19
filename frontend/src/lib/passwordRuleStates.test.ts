import { describe, expect, it } from 'vitest'

import { ruleStates } from './passwordPolicy'

// 清单常驻，检查从打第一个字才开始（2026-09-18）。这里钉的是「还没打字时
// 一条都不打勾」——第一版在这一步就给「不是常见密码」打了勾，看着像在夸一个
// 还没写的密码。

describe('ruleStates', () => {
  it('还没打字：四条全是 ○，一个勾都没有', () => {
    const states = ruleStates('', ['李娜'])
    expect(states).toHaveLength(4)
    expect(states.every((r) => r.state === 'idle')).toBe(true)
    expect(states.every((r) => r.detail === undefined)).toBe(true)
  })

  it('打了字：过的打勾，没过的打叉', () => {
    const byKey = Object.fromEntries(ruleStates('abc').map((r) => [r.key, r.state]))
    expect(byKey.length).toBe('bad')
    expect(byKey.notCommon).toBe('ok')
    expect(byKey.notRepetitive).toBe('ok')
    expect(byKey.notIdentity).toBe('ok')
  })

  it('全过了：四个勾', () => {
    expect(ruleStates('今天想吃小笼包', ['李娜']).every((r) => r.state === 'ok')).toBe(true)
  })

  it('撞上本人信息时把撞上的词带出来', () => {
    const hit = ruleStates('lina-2026-xyz', ['lina@example.com']).find((r) => r.key === 'notIdentity')
    expect(hit?.state).toBe('bad')
    expect(hit?.detail).toBe('lina')
  })
})
