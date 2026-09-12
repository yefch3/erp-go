import { describe, expect, it } from 'vitest'
import {
  DEFAULT_SORT,
  type MailSort,
  defaultDir,
  parseSort,
  sortFieldsFor,
  sortFor,
  sortFromCommand,
  sortParam,
} from './mailSort'

describe('地址栏里的排序', () => {
  it('默认排序不进地址栏', () => {
    expect(sortParam(DEFAULT_SORT)).toBe('')
    expect(sortParam({ by: 'date', dir: 'desc' })).toBe('')
  })

  it('其余都写成 列:方向，而且解回来是同一个', () => {
    for (const s of [
      { by: 'size', dir: 'desc' },
      { by: 'subject', dir: 'asc' },
      { by: 'date', dir: 'asc' },
      { by: 'to', dir: 'desc' },
    ] as const) {
      expect(parseSort(sortParam(s))).toEqual(s)
    }
  })

  it('认不出的一律回默认，不白屏', () => {
    expect(parseSort('')).toEqual(DEFAULT_SORT)
    expect(parseSort(undefined)).toEqual(DEFAULT_SORT)
    expect(parseSort('sender:asc')).toEqual(DEFAULT_SORT)
    expect(parseSort('size:down')).toEqual(DEFAULT_SORT)
    expect(parseSort(':')).toEqual(DEFAULT_SORT)
  })

  it('只给列不给方向时用那列的自然方向', () => {
    expect(parseSort('size')).toEqual({ by: 'size', dir: 'desc' })
    expect(parseSort('subject')).toEqual({ by: 'subject', dir: 'asc' })
  })
})

describe('排序菜单', () => {
  it('换一列，按那一列的自然方向：日期和大小是大的在前，文字是 A 到 Z', () => {
    expect(defaultDir('date')).toBe('desc')
    expect(defaultDir('size')).toBe('desc')
    expect(defaultDir('from')).toBe('asc')
    expect(defaultDir('to')).toBe('asc')
    expect(defaultDir('subject')).toBe('asc')
    expect(sortFromCommand(DEFAULT_SORT, 'by:size')).toEqual({ by: 'size', dir: 'desc' })
    expect(sortFromCommand(DEFAULT_SORT, 'by:subject')).toEqual({ by: 'subject', dir: 'asc' })
  })

  it('点的是当前这一列，什么都不变——方向是菜单里另外两项的事', () => {
    const cur: MailSort = { by: 'size', dir: 'asc' }
    expect(sortFromCommand(cur, 'by:size')).toEqual(cur)
  })

  it('方向是明说的，不是靠再点一次猜出来的', () => {
    expect(sortFromCommand({ by: 'size', dir: 'desc' }, 'dir:asc')).toEqual({ by: 'size', dir: 'asc' })
    expect(sortFromCommand({ by: 'size', dir: 'asc' }, 'dir:desc')).toEqual({ by: 'size', dir: 'desc' })
    // 已经是这个方向了：原样，不翻。
    expect(sortFromCommand(DEFAULT_SORT, 'dir:desc')).toEqual(DEFAULT_SORT)
  })

  it('认不出的命令什么都不改', () => {
    expect(sortFromCommand(DEFAULT_SORT, 'by:nosuch')).toEqual(DEFAULT_SORT)
    expect(sortFromCommand(DEFAULT_SORT, 'dir:sideways')).toEqual(DEFAULT_SORT)
    expect(sortFromCommand(DEFAULT_SORT, 'date')).toEqual(DEFAULT_SORT)
    expect(sortFromCommand(DEFAULT_SORT, '')).toEqual(DEFAULT_SORT)
  })
})

describe('收件箱和已发送各认各的列', () => {
  it('收件箱有发件人没收件人，已发送反过来', () => {
    expect(sortFieldsFor('inbox')).toEqual(['from', 'subject', 'date', 'size'])
    expect(sortFieldsFor('sent')).toEqual(['to', 'subject', 'date', 'size'])
  })

  it('把「按发件人」硬套到已发送上时按默认排，而不是报错或乱排', () => {
    expect(sortFor('sent', { by: 'from', dir: 'asc' })).toEqual(DEFAULT_SORT)
    expect(sortFor('inbox', { by: 'to', dir: 'asc' })).toEqual(DEFAULT_SORT)
    expect(sortFor('inbox', { by: 'size', dir: 'asc' })).toEqual({ by: 'size', dir: 'asc' })
  })
})
