import { describe, expect, it } from 'vitest'
import {
  isActionable,
  isListFolder,
  type PickableMail,
  pickedRows,
  selectAllState,
  selectableRows,
  toggleAll,
} from './mailSelection'

const inbound: PickableMail[] = [{ id: 'i1' }, { id: 'i2' }, { id: 'i3' }]
const sent: PickableMail[] = [
  { id: 's1', kind: 'HOST' },
  { id: 's2', kind: 'ERP' },
  { id: 's3', kind: 'HOST' },
]
const pools = { inbound, sent }

describe('哪批行能勾', () => {
  it('已发送算的是已发送那批，不是收件箱那批', () => {
    // 这条就是原来的 bug：选择集合写死成 inbound，于是在已发送里勾出来的 id
    // 一个都对不上，工具条永远不出现。
    expect(selectableRows('sent', pools).map((m) => m.id)).toEqual(['s1', 's3'])
  })

  it('收件箱、星标、归档、垃圾邮件、废纸篓算的是同一批', () => {
    for (const f of ['inbox', 'starred', 'archive', 'junk', 'trash']) {
      expect(selectableRows(f, pools).map((m) => m.id)).toEqual(['i1', 'i2', 'i3'])
    }
  })

  it('表格类的文件夹不走这套（它们用 el-table 自己的选择）', () => {
    for (const f of ['drafts', 'scheduled', 'attention', 'suppressions']) {
      expect(selectableRows(f, pools)).toEqual([])
      expect(isListFolder(f)).toBe(false)
    }
  })

  it('投递记录不给勾——它在服务器上没有正本，标记和删除都无处可写', () => {
    expect(isActionable({ id: 'x', kind: 'ERP' })).toBe(false)
    expect(isActionable({ id: 'x', kind: 'HOST' })).toBe(true)
    // 收件箱的行没有 kind，不能因此被当成不可操作。
    expect(isActionable({ id: 'x' })).toBe(true)
    expect(selectableRows('sent', pools).some((m) => m.kind === 'ERP')).toBe(false)
  })
})

describe('勾中的行', () => {
  it('只算还在屏幕上的：翻页后残留的 id 不该还能操作', () => {
    expect(pickedRows(inbound, ['i2', '已经翻过去的一封']).map((m) => m.id)).toEqual(['i2'])
  })
})

describe('全选框', () => {
  it('一封没勾：不是全选，也不是半选', () => {
    expect(selectAllState(inbound, [])).toEqual({ all: false, some: false })
  })

  it('勾了一部分：半选', () => {
    expect(selectAllState(inbound, ['i1'])).toEqual({ all: false, some: true })
  })

  it('全勾上了：全选，不再是半选', () => {
    expect(selectAllState(inbound, ['i1', 'i2', 'i3'])).toEqual({ all: true, some: false })
  })

  it('空列表不算全选——否则空文件夹的全选框会显示成打勾的', () => {
    expect(selectAllState([], [])).toEqual({ all: false, some: false })
  })

  it('已发送里全选，跳过那条投递记录', () => {
    const rows = selectableRows('sent', pools)
    expect(toggleAll(rows, [])).toEqual(['s1', 's3'])
    // 全选之后状态是「全选」，不能因为 s2 没被勾上就停在半选。
    expect(selectAllState(rows, ['s1', 's3'])).toEqual({ all: true, some: false })
  })
})

describe('点全选框', () => {
  it('一封没勾 → 全勾上', () => {
    expect(toggleAll(inbound, [])).toEqual(['i1', 'i2', 'i3'])
  })

  it('勾了一部分 → 清空，不是补齐剩下的', () => {
    expect(toggleAll(inbound, ['i1'])).toEqual([])
  })

  it('全勾上了 → 清空', () => {
    expect(toggleAll(inbound, ['i1', 'i2', 'i3'])).toEqual([])
  })
})
