import { describe, expect, it } from 'vitest'
import {
  MAILBOX_FOLDER_KEYS,
  SHARED_FOLDER_KEYS,
  expandedAfterSwitch,
  isMailboxFolder,
  parseExpanded,
  splitFolders,
  toggleExpanded,
} from './mailFolders'

// 左栏用的那十个键，顺序照页面上的。
const ALL = [
  'inbox', 'starred', 'drafts', 'scheduled', 'sent',
  'attention', 'archive', 'junk', 'trash', 'suppressions',
].map((key) => ({ key, icon: key }))

describe('哪些文件夹跟着信箱走', () => {
  it('十个键一个不少地分成两堆，没有重复也没有漏', () => {
    const { perMailbox, shared } = splitFolders(ALL)
    const keys = [...perMailbox, ...shared].map((f) => f.key).sort()
    expect(keys).toEqual(ALL.map((f) => f.key).sort())
    expect(perMailbox.length + shared.length).toBe(ALL.length)
  })

  it('待处理和拒收名单不挂在信箱底下——后端那两条查询根本不按信箱筛', () => {
    const { shared } = splitFolders(ALL)
    expect(shared.map((f) => f.key)).toEqual(['attention', 'suppressions'])
    expect(isMailboxFolder('attention')).toBe(false)
    expect(isMailboxFolder('suppressions')).toBe(false)
  })

  it('草稿箱跟着信箱走', () => {
    // 草稿行上记着是从哪个箱写的（迁移 00047），列表也照着筛。
    expect(isMailboxFolder('drafts')).toBe(true)
    expect(MAILBOX_FOLDER_KEYS).toContain('drafts')
  })

  it('顺序照清单里的，不重排', () => {
    const { perMailbox } = splitFolders(ALL)
    expect(perMailbox.map((f) => f.key)).toEqual([...MAILBOX_FOLDER_KEYS])
    expect(SHARED_FOLDER_KEYS).toEqual(['attention', 'suppressions'])
  })

  it('清单里没有的键不会凭空造出来', () => {
    const { perMailbox, shared } = splitFolders([{ key: 'inbox', icon: 'x' }])
    expect(perMailbox.map((f) => f.key)).toEqual(['inbox'])
    expect(shared).toEqual([])
  })
})

describe('展开和收起', () => {
  it('切过去的那个一定是展开的', () => {
    expect(expandedAfterSwitch([], 7)).toEqual([7])
    expect(expandedAfterSwitch([4], 7)).toEqual([4, 7])
  })

  it('已经开着的不会被加两遍', () => {
    expect(expandedAfterSwitch([4, 7], 7)).toEqual([4, 7])
  })

  it('其余保持人上次留下的样子', () => {
    expect(expandedAfterSwitch([4, 9], 7)).toEqual([4, 9, 7])
  })

  it('没有当前信箱（一个都没绑）时不动', () => {
    expect(expandedAfterSwitch([4], 0)).toEqual([4])
  })

  it('点三角是开合互换', () => {
    expect(toggleExpanded([4], 7)).toEqual([4, 7])
    expect(toggleExpanded([4, 7], 7)).toEqual([4])
    // 收起当前这个是允许的：Foxmail 也允许，下次切回来时会自己再展开。
    expect(toggleExpanded([7], 7)).toEqual([])
  })
})

describe('存在 localStorage 里的那串', () => {
  it('来回一趟不变样', () => {
    expect(parseExpanded(JSON.stringify([4, 7]))).toEqual([4, 7])
  })

  it('坏了就当没存过，不让左栏白屏', () => {
    expect(parseExpanded(null)).toEqual([])
    expect(parseExpanded('')).toEqual([])
    expect(parseExpanded('not json')).toEqual([])
    expect(parseExpanded('{"a":1}')).toEqual([])
    expect(parseExpanded('[0,-3,"x",null]')).toEqual([])
    expect(parseExpanded('[4,"7"]')).toEqual([4, 7])
  })
})
