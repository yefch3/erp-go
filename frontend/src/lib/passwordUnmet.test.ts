import { describe, expect, it } from 'vitest'

import { unmetRules } from './passwordPolicy'

// 界面只在密码不合格时冒出来、只说差的那几条（2026-09-18）。这里钉的是
// 「什么时候算没差」：一个字没打不算，全过了不算；差的要一条不落。

describe('unmetRules', () => {
  it('一个字没打：不催', () => {
    expect(unmetRules('')).toEqual([])
  })

  it('全过了：什么都不列', () => {
    expect(unmetRules('今天想吃小笼包', ['李娜'])).toEqual([])
  })

  it('只列没过的，过了的不陪跑', () => {
    const keys = unmetRules('abc').map((r) => r.key)
    expect(keys).toContain('length')
    expect(keys).not.toContain('notCommon')
    expect(keys).not.toContain('notIdentity')
  })

  it('撞上本人信息时把撞上的词带出来', () => {
    const hit = unmetRules('lina-2026-xyz', ['lina@example.com']).find((r) => r.key === 'notIdentity')
    expect(hit?.ok).toBe(false)
    expect(hit?.detail).toBe('lina')
  })
})
