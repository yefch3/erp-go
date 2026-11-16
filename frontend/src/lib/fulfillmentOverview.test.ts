import { describe, expect, it } from 'vitest'
import { fulfillmentTotals, fulfillmentNumber } from './fulfillmentOverview'

describe('fulfillment quantity overview', () => {
  it('shows remaining delivery and preserves separate units', () => {
    expect(fulfillmentTotals([
      { qty: '10', receivedQty: '8', uomCode: 'MT' },
      { qty: '5', receivedQty: '1', uomCode: 'MT' },
      { qty: '3', receivedQty: '3', uomCode: 'PCS' },
    ])).toEqual([
      { unit: 'MT', ordered: 15, delivered: 9, remaining: 6 },
      { unit: 'PCS', ordered: 3, delivered: 3, remaining: 0 },
    ])
  })
  it('does not count excess receipt as negative outstanding goods', () => {
    expect(fulfillmentTotals([{ qty: '2', receivedQty: '3', uomCode: 'MT' }])[0].remaining).toBe(0)
  })
  it('avoids a misleading numeric total for missing or invalid goods', () => {
    expect(fulfillmentTotals([])).toEqual([])
    expect(fulfillmentTotals([{ qty: 'bad', receivedQty: '0', uomCode: 'MT' }])).toEqual([])
  })
  it('removes insignificant zeros while retaining actual precision', () => {
    expect(fulfillmentNumber('10.0000')).toBe('10')
    expect(fulfillmentNumber('30.1250')).toBe('30.125')
    expect(fulfillmentNumber('bad')).toBe('—')
  })
})
