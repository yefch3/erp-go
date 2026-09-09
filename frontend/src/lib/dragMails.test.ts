import { describe, expect, it } from 'vitest'
import { canDropInto, draggedRows, dropTargetFor } from './dragMails'

interface Row {
  id: string
  record?: boolean
}
const movable = (r: Row) => !r.record

const rows: Row[] = [
  { id: '1' },
  { id: '2' },
  { id: '3' },
  { id: '4', record: true },
]

describe('draggedRows', () => {
  it('拖一行已经勾上的，拖的是整批', () => {
    expect(draggedRows(rows[0], rows, ['1', '2'], movable).map((r) => r.id))
      .toEqual(['1', '2'])
  })

  it('拖一行没勾的，只拖它——而且勾选不受影响', () => {
    // 这一条是给「拖完回头发现刚勾的十封没了」钉的。函数本身不碰勾选，
    // 所以要钉的是它**不会**把已勾的一起带走。
    expect(draggedRows(rows[2], rows, ['1', '2'], movable).map((r) => r.id))
      .toEqual(['3'])
  })

  it('一封都没勾时就是拖那一行', () => {
    expect(draggedRows(rows[1], rows, [], movable).map((r) => r.id)).toEqual(['2'])
  })

  it('批里混着挪不动的，剔掉它，别的照挪', () => {
    // 投递记录（邮箱服务器上没有正本）挪无处可挪。为它整次失败是不对的：
    // 勾了三封里有一封是记录，另外两封应该照挪。
    expect(draggedRows(rows[0], rows, ['1', '3', '4'], movable).map((r) => r.id))
      .toEqual(['1', '3'])
  })

  it('拖的就是那封挪不动的，什么都不拖', () => {
    expect(draggedRows(rows[3], rows, [], movable)).toEqual([])
  })

  it('拖一封挪不动的、而它还勾着，也什么都不拖', () => {
    // 先挡住被拖的那一行，再谈整批：否则按住一封挪不动的信一拖，
    // 整批就跟着走了，而人以为自己拖的是这一封。
    expect(draggedRows(rows[3], rows, ['1', '4'], movable)).toEqual([])
  })
})

describe('canDropInto', () => {
  it('没在拖的时候，哪一格都不接', () => {
    expect(canDropInto([], 7)).toBe(false)
  })

  it('同一个箱的接得住', () => {
    expect(canDropInto([7], 7)).toBe(true)
    expect(canDropInto([7, 7], 7)).toBe(true)
  })

  it('别的箱接不住', () => {
    expect(canDropInto([7], 8)).toBe(false)
  })

  it('一次拖了两个箱的信，哪一格都不接', () => {
    // 搜索结果横跨信箱，所以这是真会发生的。放到任何一个箱的文件夹上都会
    // 挪一半失败——一格都不亮，比亮了再报「成功 3 失败 2」清楚。
    expect(canDropInto([7, 8], 7)).toBe(false)
    expect(canDropInto([7, 8], 8)).toBe(false)
  })
})

describe('dropTargetFor', () => {
  it('收件箱是「挪」，用 0 这个约定', () => {
    expect(dropTargetFor('inbox')).toEqual({ kind: 'move', folderId: 0 })
  })

  it('自建文件夹是「挪」，用它自己的 id', () => {
    expect(dropTargetFor('F:项目A', { folderId: 12 })).toEqual({ kind: 'move', folderId: 12 })
  })

  it('自建文件夹没带 id 就不接', () => {
    // 服务端要靠 id 找那个文件夹。让它亮起来再报「文件夹不存在」是最差的。
    expect(dropTargetFor('F:项目A')).toBeNull()
  })

  it('垃圾邮件是「挪」，id 从这个箱的垃圾箱来', () => {
    // 真的挪进服务器的垃圾箱——服务商的过滤器靠这个学，只打个标记它学不到。
    expect(dropTargetFor('junk', { junkFolderId: 9 })).toEqual({ kind: 'move', folderId: 9 })
  })

  it('这个箱没有垃圾箱时不接', () => {
    expect(dropTargetFor('junk')).toBeNull()
  })

  it('回收站和归档是「标」，不是「挪」', () => {
    // 视图看的是 deleted_at / archived_at，不是 folder。写成「挪」的话，信会
    // 从收件箱消失却不出现在回收站里——这是这两条最要紧的地方。
    expect(dropTargetFor('trash')).toEqual({ kind: 'mark', flags: { deleted: true } })
    expect(dropTargetFor('archive')).toEqual({ kind: 'mark', flags: { archived: true } })
  })

  it('已发送、草稿箱、星标、待处理、拒收名单都不接', () => {
    // 已发送：挪进去之后一封收到的信会显示成「我发出的」。
    // 草稿箱：ERP 的草稿是另一张表。
    // 星标：是标签不是位置。
    // 后两个根本不是邮件文件夹。
    for (const key of ['sent', 'drafts', 'starred', 'scheduled', 'attention', 'suppressions']) {
      expect(dropTargetFor(key, { folderId: 3, junkFolderId: 9 })).toBeNull()
    }
  })
})
