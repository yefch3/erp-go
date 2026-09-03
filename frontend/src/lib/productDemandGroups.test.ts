import { describe, expect, it } from 'vitest'
import { demandQuantitySummary, groupProductDemands } from './productDemandGroups'

describe('groupProductDemands', () => {
  it('folds different template specifications linked to the same product', () => {
    const groups = groupProductDemands([
      { id: 1, lineNo: 1, productId: 9, extracted: { product: '冷轧钢卷', thickness: '0.8', quantity: '25.0000', quantityUnit: 'MT' } },
      { id: 2, lineNo: 2, productId: 9, extracted: { product: '冷轧钢卷', thickness: '1.0', quantity: '30.0000', quantityUnit: 'MT' } },
      { id: 3, lineNo: 3, productId: 10, extracted: { product: '镀锌钢卷', quantity: '18.0000', quantityUnit: 'MT' } },
    ])
    expect(groups.map(group => group.lines.map(line => line.id))).toEqual([[1, 2], [3]])
  })

  it('uses normalized product text for unlinked imported lines', () => {
    const groups = groupProductDemands([
      { id: 1, productId: 0, extracted: { product: ' Cold Rolled Coil ' } },
      { id: 2, productId: 0, extracted: { product: 'cold rolled coil' } },
    ])
    expect(groups).toHaveLength(1)
  })
})

describe('demandQuantitySummary', () => {
  it('adds quantities exactly and preserves source precision', () => {
    expect(demandQuantitySummary([
      { extracted: { quantity: '25.0000', quantityUnit: 'MT' } },
      { extracted: { quantity: '30.0000', quantityUnit: 'mt' } },
    ])).toBe('55.0000 MT')
  })

  it('keeps unlike units separate', () => {
    expect(demandQuantitySummary([
      { extracted: { quantity: '2', quantityUnit: 'MT' } },
      { extracted: { quantity: '3', quantityUnit: 'PCS' } },
    ])).toBe('2 MT + 3 PCS')
  })
})
