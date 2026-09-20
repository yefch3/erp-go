import { describe, expect, it } from 'vitest'
import { moveTableColumn, normalizeTableColumnOrder, type OrderedColumn } from './tableColumnOrder'

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
})
