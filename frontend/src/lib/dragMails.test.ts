import { describe, expect, it } from 'vitest'
import { canDropInto, draggedRows } from './dragMails'

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
