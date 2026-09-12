import { describe, expect, test } from 'vitest'
import { moreStatus, shouldLoadMore, type MoreState } from './infiniteList'

const ready: MoreState = {
  hasMore: true,
  loading: false,
  loadingMore: false,
  failed: false,
  supported: true,
}

describe('shouldLoadMore', () => {
  test('还有下一页、什么都没在做：取', () => {
    expect(shouldLoadMore(ready)).toBe(true)
  })

  test('没有下一页了：不取——这是终点', () => {
    expect(shouldLoadMore({ ...ready, hasMore: false })).toBe(false)
  })

  test('整份列表正在重新拉：不取，否则两份名单的数据混在一起', () => {
    expect(shouldLoadMore({ ...ready, loading: true })).toBe(false)
  })

  test('已经在取了：不取。哨兵在滚动中会反复进出视口，不挡住就是同一页取好几遍', () => {
    expect(shouldLoadMore({ ...ready, loadingMore: true })).toBe(false)
  })

  test('上一次失败了：停下来等人点，不自动重试', () => {
    expect(shouldLoadMore({ ...ready, failed: true })).toBe(false)
  })

  test('这份列表不支持往下接（表格类的还是翻页）：不取', () => {
    expect(shouldLoadMore({ ...ready, supported: false })).toBe(false)
  })
})

describe('moreStatus', () => {
  test('还有下一页、正常滚着：底下什么都不显示', () => {
    expect(moreStatus(ready, true)).toBe('none')
  })

  test('正在取下一页：显示加载中', () => {
    expect(moreStatus({ ...ready, loadingMore: true }, true)).toBe('loading')
  })

  test('失败盖过加载中：人要看到的是那个重试按钮', () => {
    expect(moreStatus({ ...ready, loadingMore: true, failed: true }, true)).toBe('failed')
  })

  test('到底了：显示「共 N 封」', () => {
    expect(moreStatus({ ...ready, hasMore: false }, true)).toBe('end')
  })

  test('整份列表还在拉的时候不说「共 N 封」——那一刻行数是 0，说出来是假的', () => {
    expect(moreStatus({ ...ready, hasMore: false, loading: true }, false)).toBe('none')
  })

  test('一行都没有（空文件夹）不说「共 0 封」：空状态自己会说话', () => {
    expect(moreStatus({ ...ready, hasMore: false }, false)).toBe('none')
  })

  test('不支持往下接的列表：底下交给翻页器，这里什么都不显示', () => {
    expect(moreStatus({ ...ready, supported: false, failed: true }, true)).toBe('none')
  })
})
