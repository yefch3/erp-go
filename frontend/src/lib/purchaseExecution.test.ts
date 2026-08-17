import { describe, expect, it } from 'vitest'
import { buildConfirmationLines, isProductionDelayed } from './purchaseExecution'

describe('purchase execution', () => {
  it('builds supplier confirmation lines with edited values', () => {
    expect(buildConfirmationLines([{ id: '7', qty: '10', unitPrice: '520' }], { '7': '9.5' }, { '7': '515' })).toEqual([
      { po_item_id: 7, confirmed_qty: '9.5', confirmed_unit_price: '515' },
    ])
  })

  it('falls back to the approved order values', () => {
    expect(buildConfirmationLines([{ id: '7', qty: '10', unitPrice: '520' }], {}, {})).toEqual([
      { po_item_id: 7, confirmed_qty: '10', confirmed_unit_price: '520' },
    ])
  })

  it('marks only unfinished past milestones delayed', () => {
    expect(isProductionDelayed('2026-08-16', '', '2026-08-17')).toBe(true)
    expect(isProductionDelayed('2026-08-16', '2026-08-17', '2026-08-17')).toBe(false)
    expect(isProductionDelayed('2026-08-18', '', '2026-08-17')).toBe(false)
  })
})
