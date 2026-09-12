import { describe, expect, it } from 'vitest'
import { LIMITS, RESERVE, clampCol, clearWidth, readWidth, writeWidth } from './paneWidths'

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

describe('拖出来的宽度收进范围', () => {
  it('拖过头就停在上下限上', () => {
    expect(clampCol('list', 10)).toBe(LIMITS.list.min)
    expect(clampCol('list', 5000)).toBe(LIMITS.list.max)
    expect(clampCol('rail', 0)).toBe(LIMITS.rail.min)
    expect(clampCol('rail', 5000)).toBe(LIMITS.rail.max)
  })

  it('范围内的原样收下，只是取整', () => {
    expect(clampCol('list', 337.4)).toBe(337)
  })

  it('屏幕装不下时让位的是这一栏，不是阅读区', () => {
    // 总共 900：列表拉到 640 的话阅读区剩不到下限。留给阅读区的那份（连同
    // 中间那条分隔条）先扣掉，剩下的才是列表能占的。
    expect(clampCol('list', 640, 900)).toBe(900 - RESERVE.list)
    // 文件夹栏右边还要装下列表和阅读区两个下限，外加两条分隔条。
    expect(clampCol('rail', 380, 800)).toBe(800 - RESERVE.rail)
  })

  it('窗口窄到连下限都装不下时，下限赢——宁可横向滚，不要一栏消失', () => {
    expect(clampCol('list', 400, 300)).toBe(LIMITS.list.min)
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

  it('读回来的也要收进范围：上一块屏幕上拖的数，换了屏幕不一定还合适', () => {
    expect(readWidth('list', fakeStore({ 'mailCol.list': '9999' }))).toBe(LIMITS.list.max)
  })

  it('双击清掉之后就回到没拖过的状态', () => {
    const s = fakeStore()
    writeWidth('rail', 300, s)
    clearWidth('rail', s)
    expect(readWidth('rail', s)).toBeNull()
  })
})
