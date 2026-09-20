import { describe, expect, it } from 'vitest'
import { moveTableColumn, moveTableColumnBy, normalizeTableColumnOrder, preferenceColumnOrder, tableColumnPreferenceStorageKey, type OrderedColumn } from './tableColumnOrder'

const defaults: OrderedColumn[] = [
  { key: 'number' },
  { key: 'customer' },
  { key: 'status' },
]

describe('table column ordering', () => {
  it('restores valid saved columns, removes stale entries and appends new defaults', () => {
    expect(normalizeTableColumnOrder(['status', 'removed', 'status', 'number'], defaults))
      .toEqual(['status', 'number', 'customer'])
  })

  it('moves a dragged column before the drop target without mutating the source', () => {
    const source = ['number', 'customer', 'status']
    expect(moveTableColumn(source, 'status', 'number')).toEqual(['status', 'number', 'customer'])
    expect(source).toEqual(['number', 'customer', 'status'])
  })

  it('keeps the current order when the source or target is invalid', () => {
    const source = ['number', 'customer', 'status']
    expect(moveTableColumn(source, 'missing', 'number')).toBe(source)
    expect(moveTableColumn(source, 'number', 'missing')).toBe(source)
  })

  it('moves a column one position and keeps boundary columns in place', () => {
    const source = ['number', 'customer', 'status']
    expect(moveTableColumnBy(source, 'customer', -1)).toEqual(['customer', 'number', 'status'])
    expect(moveTableColumnBy(source, 'customer', 1)).toEqual(['number', 'status', 'customer'])
    expect(moveTableColumnBy(source, 'number', -1)).toBe(source)
    expect(moveTableColumnBy(source, 'status', 1)).toBe(source)
  })

  it('reads current preference objects and legacy arrays', () => {
    expect(preferenceColumnOrder(['status', 'number'])).toEqual(['status', 'number'])
    expect(preferenceColumnOrder({ version: 2, userId: '7', pageKey: 'inquiry-list', columnOrder: ['customer'], updatedAt: '2026-09-20T00:00:00Z' })).toEqual(['customer'])
    expect(preferenceColumnOrder({ columnOrder: 'broken' })).toEqual([])
  })

  it('isolates preferences by user and page', () => {
    expect(tableColumnPreferenceStorageKey('user-a', 'inquiry-list')).not.toBe(tableColumnPreferenceStorageKey('user-b', 'inquiry-list'))
    expect(tableColumnPreferenceStorageKey('user-a', 'inquiry-list')).not.toBe(tableColumnPreferenceStorageKey('user-a', 'purchase-order-list'))
  })
})
