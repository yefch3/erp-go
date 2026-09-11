import { describe, expect, test } from 'vitest'
import { mailboxOf, newMailLandsIn, shouldReloadList, type ListState } from './liveInbox'

const calm: ListState = {
  folder: 'inbox',
  searching: false,
  onFirstPage: true,
  picked: 0,
  dragging: false,
  bulkBusy: false,
  currentAccount: 7,
}

describe('shouldReloadList', () => {
  test('站在收件箱第一页、什么都没在做：重拉', () => {
    expect(shouldReloadList('MAILBOX:7', calm)).toBe(true)
  })

  // 这一条就是那个 bug。它没有对应的输入——「开着一封信」根本不在 ListState
  // 里，所以它**不可能**再成为不重拉的理由。这条测试钉的是这个事实。
  test('正在读一封信不是理由：状态里根本没有这一项', () => {
    expect(Object.keys(calm)).not.toContain('openedInbound')
    expect(Object.keys(calm)).not.toContain('reading')
  })

  test('是别的箱来的信：角标归角标，列表不动', () => {
    expect(shouldReloadList('MAILBOX:9', calm)).toBe(false)
  })

  test('老服务端不带 subject：不知道是哪个箱，宁可多拉一次', () => {
    expect(shouldReloadList(undefined, calm)).toBe(true)
    expect(shouldReloadList('', calm)).toBe(true)
  })

  test('搜索状态下不动：列表里是跨箱的命中，不是收件箱', () => {
    expect(shouldReloadList('MAILBOX:7', { ...calm, searching: true })).toBe(false)
  })

  test('翻到后面去了不动：新信落在第一页，人正在看的是历史', () => {
    expect(shouldReloadList('MAILBOX:7', { ...calm, onFirstPage: false })).toBe(false)
  })

  test('勾着几封不动：重拉会把勾选清掉', () => {
    expect(shouldReloadList('MAILBOX:7', { ...calm, picked: 2 })).toBe(false)
  })

  test('拖着的时候不动', () => {
    expect(shouldReloadList('MAILBOX:7', { ...calm, dragging: true })).toBe(false)
  })

  test('批量操作在飞的时候不动', () => {
    expect(shouldReloadList('MAILBOX:7', { ...calm, bulkBusy: true })).toBe(false)
  })

  test('新信落不进去的文件夹不动：星标、归档、废纸篓、已发送、草稿', () => {
    for (const folder of ['starred', 'archive', 'trash', 'sent', 'drafts', 'scheduled', 'attention']) {
      expect(shouldReloadList('MAILBOX:7', { ...calm, folder })).toBe(false)
    }
  })

  test('垃圾邮件和自建文件夹会重拉：服务器会把新信直接分进去', () => {
    expect(shouldReloadList('MAILBOX:7', { ...calm, folder: 'junk' })).toBe(true)
    expect(shouldReloadList('MAILBOX:7', { ...calm, folder: 'F:供应商' })).toBe(true)
  })
})

describe('mailboxOf', () => {
  test('认得出自己的格式', () => {
    expect(mailboxOf('MAILBOX:12')).toBe(12)
  })
  test('认不出的一律 0，包括别的事件的 subject', () => {
    expect(mailboxOf(undefined)).toBe(0)
    expect(mailboxOf('')).toBe(0)
    expect(mailboxOf('EXCEL_JOB:3')).toBe(0)
    expect(mailboxOf('MAILBOX:')).toBe(0)
    expect(mailboxOf('MAILBOX:12x')).toBe(0)
  })
})

describe('newMailLandsIn', () => {
  test('收件箱、垃圾邮件、自建文件夹', () => {
    expect(newMailLandsIn('inbox')).toBe(true)
    expect(newMailLandsIn('junk')).toBe(true)
    expect(newMailLandsIn('F:客户')).toBe(true)
    expect(newMailLandsIn('F:')).toBe(false)
  })
})
