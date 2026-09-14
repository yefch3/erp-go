import { describe, expect, it } from 'vitest'
import {
  MAILBOX_FOLDER_KEYS,
  nestFolders,
  splitFolderPath,
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

  it('只有拒收名单不挂在信箱底下——它整家公司一份，发信时也是按公司拦的', () => {
    const { shared } = splitFolders(ALL)
    expect(shared.map((f) => f.key)).toEqual(['suppressions'])
    expect(isMailboxFolder('suppressions')).toBe(false)
  })

  it('待处理跟着信箱走：失败的信记着自己从哪个箱发的', () => {
    expect(isMailboxFolder('attention')).toBe(true)
    expect(MAILBOX_FOLDER_KEYS).toContain('attention')
  })

  it('草稿箱跟着信箱走', () => {
    // 草稿行上记着是从哪个箱写的（迁移 00047），列表也照着筛。
    expect(isMailboxFolder('drafts')).toBe(true)
    expect(MAILBOX_FOLDER_KEYS).toContain('drafts')
  })

  it('顺序照清单里的，不重排', () => {
    const { perMailbox } = splitFolders(ALL)
    expect(perMailbox.map((f) => f.key)).toEqual([...MAILBOX_FOLDER_KEYS])
    expect(SHARED_FOLDER_KEYS).toEqual(['suppressions'])
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

describe('多层文件夹', () => {
  const cf = (name: string, id = 1) => ({ id, accountId: 1, name, viewKey: `F:${name}`, role: 'CUSTOM' })

  it('路径切成父和最后一段', () => {
    expect(splitFolderPath('客户')).toEqual({ parent: '', leaf: '客户' })
    expect(splitFolderPath('客户/巴西')).toEqual({ parent: '客户', leaf: '巴西' })
    expect(splitFolderPath('客户/巴西/2026')).toEqual({ parent: '客户/巴西', leaf: '2026' })
    // 有些服务器拿点分层。
    expect(splitFolderPath('客户.巴西')).toEqual({ parent: '客户', leaf: '巴西' })
  })

  it('父在前、孩子跟着，缩进一级，行上只写最后一段', () => {
    const out = nestFolders([cf('客户/巴西', 2), cf('客户', 1), cf('供应商', 3)])
    expect(out.map((i) => [i.name, i.depth])).toEqual([
      ['供应商', 0],
      ['客户', 0],
      ['巴西', 1],
    ])
    // 点进去用的 key 还是整条路径——服务器上的文件夹就叫这个。
    expect(out[2].key).toBe('F:客户/巴西')
    expect(out[2].hostName).toBe('客户/巴西')
  })

  it('三层也排得下去', () => {
    const out = nestFolders([cf('客户/巴西/2026', 3), cf('客户', 1), cf('客户/巴西', 2)])
    expect(out.map((i) => [i.name, i.depth])).toEqual([
      ['客户', 0],
      ['巴西', 1],
      ['2026', 2],
    ])
  })

  it('父不存在时照样是顶层的一行，写全名——不凭空造一级点不进去的空目录', () => {
    const out = nestFolders([cf('客户/巴西', 2)])
    expect(out.map((i) => [i.name, i.depth])).toEqual([['客户/巴西', 0]])
  })

  it('名字里带点但没有那个父文件夹的，不算嵌套', () => {
    const out = nestFolders([cf('2026.09 报价', 1)])
    expect(out.map((i) => [i.name, i.depth])).toEqual([['2026.09 报价', 0]])
  })
})
