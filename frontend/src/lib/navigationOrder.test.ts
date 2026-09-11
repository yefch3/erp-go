import { beforeEach, describe, expect, it } from 'vitest'
import { mergeVisibleOrder, moveItem, normalizeOrder, readNavigationPreferences, sortByOrder } from './navigationOrder'

class MemoryStorage {
  private data = new Map<string, string>()
  getItem(key: string) { return this.data.get(key) ?? null }
  setItem(key: string, value: string) { this.data.set(key, value) }
  clear() { this.data.clear() }
}
;(globalThis as { localStorage?: unknown }).localStorage = new MemoryStorage()

beforeEach(() => localStorage.clear())

describe('personal navigation ordering', () => {
  it('keeps known saved keys once and appends newly introduced modules', () => {
    expect(normalizeOrder(['b', 'b', 'removed', 'a'], ['a', 'b', 'c'])).toEqual(['b', 'a', 'c'])
  })

  it('sorts visible items without losing their payload', () => {
    expect(sortByOrder([{ key: 'a', label: 'A' }, { key: 'b', label: 'B' }], ['b', 'a']).map((x) => x.label)).toEqual(['B', 'A'])
  })

  it('moves one item and preserves hidden-module positions when visible order is saved', () => {
    expect(moveItem(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b'])
    expect(mergeVisibleOrder(['a', 'hidden', 'b', 'c'], ['c', 'a', 'b'])).toEqual(['c', 'hidden', 'a', 'b'])
  })

  it('falls back safely when local storage is damaged', () => {
    localStorage.setItem('nav', '{bad')
    expect(readNavigationPreferences('nav', ['a', 'b'], { a: ['x', 'y'] })).toEqual({ modules: ['a', 'b'], children: { a: ['x', 'y'] } })
  })
})
