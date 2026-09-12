import { describe, expect, it } from 'vitest'
import { SEP_LIST, SEP_RAIL, clampCol, clearWidth, readWidth, writeWidth } from './paneWidths'

function fakeStore(seed: Record<string, string> = {}) {
  const box: Record<string, string> = { ...seed }
  return {
    box,
    getItem: (k: string) => (k in box ? box[k] : null),
    setItem: (k: string, v: string) => {
      box[k] = v
    },
    removeItem: (k: string) => {
      delete box[k]
    },
  }
}

describe('拖出来的宽度', () => {
  it('想收多窄就多窄——没有下限', () => {
    expect(clampCol(40, 1000)).toBe(40)
    expect(clampCol(3, 1000)).toBe(3)
    expect(clampCol(0, 1000)).toBe(0)
  })

  it('往左拖过了头停在 0，不会变成负数', () => {
    expect(clampCol(-200, 1000)).toBe(0)
  })

  it('往右拖到头就是这块地方本身：再宽就把右边那栏挤出容器', () => {
    expect(clampCol(5000, 900)).toBe(900)
  })

  it('取整', () => {
    expect(clampCol(337.4, 1000)).toBe(337)
  })

  it('两条分隔条的宽度就是 CSS 里那两组留白加起来', () => {
    expect(SEP_RAIL).toBe(4 + 10 + 4)
    expect(SEP_LIST).toBe(3 + 10 + 3)
  })
})

describe('存和读', () => {
  it('没拖过就是没有，让 CSS 里的默认值继续生效', () => {
    expect(readWidth('list', fakeStore())).toBeNull()
  })

  it('存进去的读得回来', () => {
    const s = fakeStore()
    writeWidth('list', 321.6, s)
    expect(s.box['mailCol.list']).toBe('322')
    expect(readWidth('list', s)).toBe(322)
  })

  it('手改坏的值当没存过，不是当 0', () => {
    expect(readWidth('list', fakeStore({ 'mailCol.list': 'abc' }))).toBeNull()
    expect(readWidth('list', fakeStore({ 'mailCol.list': '-5' }))).toBeNull()
    expect(readWidth('list', fakeStore({ 'mailCol.list': '' }))).toBeNull()
  })

  it('读的时候不收进屏幕：那时还没有 DOM，装不装得下画出来才知道', () => {
    expect(readWidth('list', fakeStore({ 'mailCol.list': '9999' }))).toBe(9999)
  })

  it('双击清掉之后就回到没拖过的状态', () => {
    const s = fakeStore()
    writeWidth('rail', 300, s)
    clearWidth('rail', s)
    expect(readWidth('rail', s)).toBeNull()
  })
})
