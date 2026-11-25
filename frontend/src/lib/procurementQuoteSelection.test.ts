import { describe, expect, it } from 'vitest'
import { blankProduct, pastePrices, type Price } from './inquiryWorkspace'
import { emptyProcurementPrice, procurementPricesForSave, quoteSelectionState } from './procurementQuoteSelection'

const ids = ['1', '2', '3']
const row = (productId: string, price: string): Price => ({ productId, price, delivery: '', remark: '' })

describe('procurement quote selection', () => {
  it('counts selections independently of prices and ignores unrelated IDs', () => {
    expect(quoteSelectionState(ids, new Set(['2', 'unknown']))).toEqual({ count: 1, total: 3, checked: false, indeterminate: true })
    expect(quoteSelectionState(ids, new Set(ids)).checked).toBe(true)
    expect(quoteSelectionState([], new Set()).checked).toBe(false)
  })

  it('keeps unchecked entered values in drafts and skips untouched rows', () => {
    const rows = [row('1', '37.50'), { ...row('2', ''), delivery: '2027-01-02' }, emptyProcurementPrice('3')]
    expect(procurementPricesForSave(rows, new Set(['1']), true)).toEqual({
      prices: [rows[0]],
      draftPrices: [rows[1]],
    })
    expect(procurementPricesForSave(rows, new Set(), true).draftPrices).toEqual(rows.slice(0, 2))
  })

  it('submits only checked rows and leaves a checked empty row for existing validation', () => {
    const rows = [row('1', '37.50'), row('2', 'unfinished')]
    expect(procurementPricesForSave(rows, new Set(['1', '3']), false)).toEqual({
      prices: [rows[0], emptyProcurementPrice('3')],
      draftPrices: undefined,
    })
  })

  it('keeps remarks with their specification IDs across draft and submission', () => {
    const first = { ...row('1', '37.50'), remark: 'Moisture-proof packaging' }
    const second = { ...row('2', ''), remark: 'Separate pallet' }
    expect(procurementPricesForSave([second, first], new Set(['1']), true)).toEqual({
      prices: [first],
      draftPrices: [second],
    })
    const completedSecond = { ...second, price: '42' }
    expect(procurementPricesForSave([completedSecond, first], new Set(['1', '2']), false).prices)
      .toEqual([first, completedSecond])
  })

  it('pastes price and delivery values without including any unchecked row in submission', () => {
    const products = ids.map(id => ({ ...blankProduct(), id }))
    const rows = pastePrices('12\t2027-01-01\n15\t2027-01-02', products, [], '1')
    expect(procurementPricesForSave(rows, new Set(['2']), false).prices).toEqual([
      { ...row('2', '15'), delivery: '2027-01-02' },
    ])
    expect(procurementPricesForSave(rows, new Set(), true).draftPrices).toEqual(rows)
  })
})
