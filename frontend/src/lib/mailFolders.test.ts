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
  toggleFolderCollapse,
  parseCollapsedFolders,
  visibleRail,
  revealPathTo,
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

// 父文件夹能收起来（2026-09-17）：从前多层文件夹一律摊开，建了几十个的人
// 左栏要滚半天，而父子关系只靠 12px 的缩进说，看不出来。
describe('父文件夹的收起与展开', () => {
  const cf = (name: string, id = 1) => ({ id, accountId: 1, name, viewKey: `F:${name}`, role: 'CUSTOM' })
  const tree = () => nestFolders([cf('客户', 1), cf('客户/巴西', 2), cf('客户/巴西/2026', 3), cf('供应商', 4)])

  it('谁底下挂着东西、谁的爹是谁，排树的时候就标好', () => {
    expect(tree().map((i) => [i.name, i.hasChildren ?? false, i.parentKey ?? ''])).toEqual([
      ['供应商', false, ''],
      ['客户', true, ''],
      ['巴西', true, 'F:客户'],
      ['2026', false, 'F:客户/巴西'],
    ])
  })

  it('一个都没收起时，画的就是整棵树', () => {
    expect(visibleRail(tree(), []).map((i) => i.name)).toEqual(['供应商', '客户', '巴西', '2026'])
  })

  it('收起「客户」，它底下整支都不画——孙子也一样', () => {
    expect(visibleRail(tree(), ['F:客户']).map((i) => i.name)).toEqual(['供应商', '客户'])
  })

  it('只收起中间那一层，上面那层照画', () => {
    expect(visibleRail(tree(), ['F:客户/巴西']).map((i) => i.name)).toEqual(['供应商', '客户', '巴西'])
  })

  it('固定视图（收件箱那些）不受影响：它们没有层级', () => {
    const items = [{ key: 'inbox', custom: false }, ...tree()]
    expect(visibleRail(items, ['F:客户']).map((i) => i.name ?? i.key)).toEqual(['inbox', '供应商', '客户'])
  })

  // 换过去的那个**一定看得见**。和信箱那一层同一条规矩：藏起当前正在读的
  // 位置，人会以为文件夹没了。
  //
  // 只在**换**的时候放开，不是每次画都放开——后者会让「收起」这颗按钮在
  // 正读着它里面某封信时点下去毫无动静，看着像坏了。
  it('换到一个被收起来的文件夹：它头上那几级一起放开', () => {
    expect(revealPathTo(['F:客户', 'F:客户/巴西'], tree(), 'F:客户/巴西/2026')).toEqual([])
  })

  it('换到别处时，收起的照旧收着', () => {
    expect(revealPathTo(['F:客户'], tree(), 'F:供应商')).toEqual(['F:客户'])
    expect(revealPathTo(['F:客户'], tree(), '')).toEqual(['F:客户'])
    // 固定视图（收件箱那些）没有爹，什么都不放开。
    expect(revealPathTo(['F:客户'], tree(), 'inbox')).toEqual(['F:客户'])
  })

  it('只放开头上那几级，同一层的兄弟照旧收着', () => {
    expect(revealPathTo(['F:客户', 'F:供应商'], tree(), 'F:客户/巴西')).toEqual(['F:供应商'])
  })

  it('点一下收起、再点一下展开', () => {
    expect(toggleFolderCollapse([], 'F:客户')).toEqual(['F:客户'])
    expect(toggleFolderCollapse(['F:客户'], 'F:客户')).toEqual([])
    expect(toggleFolderCollapse(['F:客户'], 'F:供应商')).toEqual(['F:客户', 'F:供应商'])
  })

  it('存在 localStorage 里那串坏了就当没存过', () => {
    expect(parseCollapsedFolders('["1:F:客户"]')).toEqual(['1:F:客户'])
    expect(parseCollapsedFolders(null)).toEqual([])
    expect(parseCollapsedFolders('{oops')).toEqual([])
    expect(parseCollapsedFolders('"不是数组"')).toEqual([])
    // 混进来的非字符串扔掉，别让一个坏值把整栏弄白。
    expect(parseCollapsedFolders('["a", 3, null]')).toEqual(['a'])
  })
})
