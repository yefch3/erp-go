import { describe, expect, it } from 'vitest'

import {
  DEFAULT_LIST_MODE,
  mergesThreads,
  listModeChange,
  normalizeListMode,
} from './mailListMode'

describe('normalizeListMode', () => {
  it('认得出单封档', () => {
    expect(normalizeListMode('MESSAGE')).toBe('MESSAGE')
  })

  it('认得出合并档', () => {
    expect(normalizeListMode('THREAD')).toBe('THREAD')
  })

  // 换版本那几秒里后端先发、前端后发是反的：旧服务端的回包里根本没有这个
  // 字段。那时必须是合并档——一直以来的样子，也是这些行真实的含义。
  it('回包里没有这个字段时按合并档算', () => {
    expect(normalizeListMode(undefined)).toBe('THREAD')
    expect(normalizeListMode('')).toBe('THREAD')
    expect(normalizeListMode(null)).toBe('THREAD')
  })

  // 退回默认而不是抛错：认不出来的档位最坏的后果只是看到一直以来的样子，
  // 不值得让整个收件箱打不开。
  it('认不出来的值退回合并档', () => {
    expect(normalizeListMode('BANANA')).toBe('THREAD')
    expect(normalizeListMode(42)).toBe('THREAD')
    expect(normalizeListMode({ listMode: 'MESSAGE' })).toBe('THREAD')
  })

  it('大小写不含糊：只认大写的那一个', () => {
    expect(normalizeListMode('message')).toBe('THREAD')
  })
})

describe('mergesThreads', () => {
  it('合并档要合', () => {
    expect(mergesThreads('THREAD')).toBe(true)
  })

  it('单封档不合', () => {
    expect(mergesThreads('MESSAGE')).toBe(false)
  })

  // 这一条是防着将来加第三档时漏改：新档位默认应该落在"合并"那一侧，
  // 因为那是一直以来的样子；真要不合，加的人得回来改这里。
  it('默认档是合并的', () => {
    expect(mergesThreads(DEFAULT_LIST_MODE)).toBe(true)
  })
})

describe('listModeChange', () => {
  it('点另一项就切过去', () => {
    expect(listModeChange('THREAD', 'MESSAGE')).toBe('MESSAGE')
    expect(listModeChange('MESSAGE', 'THREAD')).toBe('THREAD')
  })

  // 这一条就是那次误会：从前点已选中的那一项会把它**取消**，于是「再点一次
  // 确认一下」正好关掉了刚开的功能，而屏幕上除了列表悄悄变回去没有别的话。
  it('点已经选中的那一项，什么都不发生', () => {
    expect(listModeChange('THREAD', 'THREAD')).toBeNull()
    expect(listModeChange('MESSAGE', 'MESSAGE')).toBeNull()
  })

  it('认不出来的命令按兵不动', () => {
    expect(listModeChange('THREAD', 'BANANA')).toBeNull()
    expect(listModeChange('THREAD', '')).toBeNull()
    expect(listModeChange('MESSAGE', 'message')).toBeNull()
  })
})
