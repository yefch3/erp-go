import { describe, expect, it } from 'vitest'

import { batchRolesVerdict, singleRolesVerdict } from './roleAssignment'

// 2026-09-18：批量分配角色，替换模式，一个角色没勾就保存——四个销售的角色
// 被清空。这里钉的是从那天起的规矩。

describe('batchRolesVerdict', () => {
  it('一个角色都没勾：不管什么模式都不许保存', () => {
    expect(batchRolesVerdict('replace', [])).toBe('pickOne')
    expect(batchRolesVerdict('append', [])).toBe('pickOne')
  })

  it('替换模式勾了角色：先确认，因为会拿掉原有的', () => {
    expect(batchRolesVerdict('replace', ['27'])).toBe('confirmReplace')
  })

  it('追加模式勾了角色：直接保存', () => {
    expect(batchRolesVerdict('append', ['27'])).toBe('ok')
  })
})

describe('singleRolesVerdict', () => {
  it('把一个人的角色全取消：确认，不禁止', () => {
    expect(singleRolesVerdict([])).toBe('confirmClear')
  })

  it('还留着角色：直接保存', () => {
    expect(singleRolesVerdict(['27', '28'])).toBe('ok')
  })
})
