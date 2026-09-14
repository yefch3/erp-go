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
  topParam,
} from './mailSort'

// 四个字段每次都写全太吵，而吵起来之后真正在验的那一个就看不见了。
const S = (by: MailSort['by'], dir: MailSort['dir'], top: Partial<MailSort> = {}): MailSort => ({
  by,
  dir,
  starFirst: false,
  unreadFirst: false,
  ...top,
})

describe('地址栏里的排序', () => {
  it('默认排序不进地址栏', () => {
    expect(sortParam(DEFAULT_SORT)).toBe('')
    expect(sortParam(S('date', 'desc'))).toBe('')
  })

  it('其余都写成 列:方向，而且解回来是同一个', () => {
    for (const s of [S('size', 'desc'), S('subject', 'asc'), S('date', 'asc'), S('to', 'desc')]) {
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
    expect(parseSort('size')).toEqual(S('size', 'desc'))
    expect(parseSort('subject')).toEqual(S('subject', 'asc'))
  })
})

describe('地址栏里的「优先显示」', () => {
  it('都没开时不进地址栏', () => {
    expect(topParam(DEFAULT_SORT)).toBe('')
  })

  it('写成 star / unread / star,unread，解回来是同一个', () => {
    for (const s of [
      S('date', 'desc', { starFirst: true }),
      S('date', 'desc', { unreadFirst: true }),
      S('size', 'asc', { starFirst: true, unreadFirst: true }),
    ]) {
      expect(parseSort(sortParam(s), topParam(s))).toEqual(s)
    }
  })

  it('分档和列是两件事，各走各的参数', () => {
    // 按大小排 + 星标优先：两个参数都在，互不影响。
    const s = S('size', 'asc', { starFirst: true })
    expect(sortParam(s)).toBe('size:asc')
    expect(topParam(s)).toBe('star')
    // 分档开着，列仍然是默认的那一档——那时 sort 参数是空的，分档不能跟着丢。
    const onlyTop = S('date', 'desc', { unreadFirst: true })
    expect(sortParam(onlyTop)).toBe('')
    expect(parseSort('', 'unread')).toEqual(onlyTop)
  })

  it('认不出的档名忽略掉，不整条作废', () => {
    expect(parseSort('', 'star,typo')).toEqual(S('date', 'desc', { starFirst: true }))
    expect(parseSort('', 'typo')).toEqual(DEFAULT_SORT)
    expect(parseSort('', '')).toEqual(DEFAULT_SORT)
    expect(parseSort('', undefined)).toEqual(DEFAULT_SORT)
  })
})

describe('排序菜单', () => {
  it('换一列，按那一列的自然方向：日期和大小是大的在前，文字是 A 到 Z', () => {
    expect(defaultDir('date')).toBe('desc')
    expect(defaultDir('size')).toBe('desc')
    expect(defaultDir('from')).toBe('asc')
    expect(defaultDir('to')).toBe('asc')
    expect(defaultDir('subject')).toBe('asc')
    expect(sortFromCommand(DEFAULT_SORT, 'by:size')).toEqual(S('size', 'desc'))
    expect(sortFromCommand(DEFAULT_SORT, 'by:subject')).toEqual(S('subject', 'asc'))
  })

  it('点的是当前这一列，什么都不变——方向是菜单里另外两项的事', () => {
    const cur = S('size', 'asc')
    expect(sortFromCommand(cur, 'by:size')).toEqual(cur)
  })

  it('方向是明说的，不是靠再点一次猜出来的', () => {
    expect(sortFromCommand(S('size', 'desc'), 'dir:asc')).toEqual(S('size', 'asc'))
    expect(sortFromCommand(S('size', 'asc'), 'dir:desc')).toEqual(S('size', 'desc'))
    // 已经是这个方向了：原样，不翻。
    expect(sortFromCommand(DEFAULT_SORT, 'dir:desc')).toEqual(DEFAULT_SORT)
  })

  it('换列换方向时，开着的分档要跟着走，不能被顺手清掉', () => {
    const cur = S('date', 'desc', { starFirst: true, unreadFirst: true })
    expect(sortFromCommand(cur, 'by:size')).toEqual(
      S('size', 'desc', { starFirst: true, unreadFirst: true }),
    )
    expect(sortFromCommand(cur, 'dir:asc')).toEqual(
      S('date', 'asc', { starFirst: true, unreadFirst: true }),
    )
  })

  it('分档那两项是开关：点一下开，再点一下关，两个能同时开', () => {
    const on = sortFromCommand(DEFAULT_SORT, 'top:star')
    expect(on).toEqual(S('date', 'desc', { starFirst: true }))
    expect(sortFromCommand(on, 'top:star')).toEqual(DEFAULT_SORT)
    const both = sortFromCommand(on, 'top:unread')
    expect(both).toEqual(S('date', 'desc', { starFirst: true, unreadFirst: true }))
    // 关掉一个，另一个还在。
    expect(sortFromCommand(both, 'top:star')).toEqual(S('date', 'desc', { unreadFirst: true }))
  })

  it('认不出的命令什么都不改', () => {
    expect(sortFromCommand(DEFAULT_SORT, 'by:nosuch')).toEqual(DEFAULT_SORT)
    expect(sortFromCommand(DEFAULT_SORT, 'dir:sideways')).toEqual(DEFAULT_SORT)
    expect(sortFromCommand(DEFAULT_SORT, 'top:nosuch')).toEqual(DEFAULT_SORT)
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
    expect(sortFor('sent', S('from', 'asc'))).toEqual(DEFAULT_SORT)
    expect(sortFor('inbox', S('to', 'asc'))).toEqual(DEFAULT_SORT)
    expect(sortFor('inbox', S('size', 'asc'))).toEqual(S('size', 'asc'))
  })

  it('已发送没有星标也没有未读，分档在那一侧不生效', () => {
    expect(sortFor('sent', S('to', 'asc', { starFirst: true, unreadFirst: true }))).toEqual(
      S('to', 'asc'),
    )
  })

  it('列对不上时退回默认，但分档留着——那是人自己开的，不该被一个手写的地址顺手关掉', () => {
    expect(sortFor('inbox', S('to', 'asc', { starFirst: true }))).toEqual(
      S('date', 'desc', { starFirst: true }),
    )
  })
})
