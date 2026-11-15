import { describe, expect, it } from 'vitest'
import { blankProduct, pastePrices, type Price } from './inquiryWorkspace'
import { createQuoteSelection, quoteSelectionState } from './procurementQuoteSelection'

const ids = Array.from({ length: 48 }, (_, i) => String(i + 1))
const filled = (productId: string): Price => ({ productId, price: '37.50', factoryPrice: '4300', fobPrice: '620', slitting: '15', delivery: '2027-01-02', remark: `line ${productId}` })

describe('procurement quote selection', () => {
  it('selects every original ID across pages/groups without setting any prices', () => {
    const session = createQuoteSelection()
    const rows = session.set([], ids, true)
    expect(rows).toHaveLength(48)
    expect(rows.every(row => row.price === '')).toBe(true)
    expect(quoteSelectionState(ids, rows)).toEqual({ count: 48, total: 48, checked: true, indeterminate: false })
  })
  it('preserves existing values on repeated select-all and deduplicates target IDs', () => {
    const session = createQuoteSelection(), original = filled('3')
    const rows = session.set([original], [...ids, '3'], true)
    expect(session.set(rows, ids, true)).toEqual(rows)
    expect(rows).toHaveLength(48)
    expect(rows.find(row => row.productId === '3')).toEqual(original)
  })
  it('reports no selection, partial selection and empty inquiries using IDs not group counts', () => {
    expect(quoteSelectionState(ids, []).count).toBe(0)
    expect(quoteSelectionState(ids, [filled('1'), filled('1'), filled('unknown')])).toEqual({ count: 1, total: 48, checked: false, indeterminate: true })
    expect(quoteSelectionState([], [])).toEqual({ count: 0, total: 0, checked: false, indeterminate: false })
  })
  it('selects and deselects a full group without touching other groups', () => {
    const session = createQuoteSelection(), group = ids.slice(0, 4)
    let rows = session.set([filled('1'), filled('48')], group, true)
    expect(quoteSelectionState(group, rows).checked).toBe(true)
    rows = session.set(rows, group, false)
    expect(rows).toEqual([filled('48')])
    expect(session.set(rows, ['1'], true).find(row => row.productId === '1')).toEqual(filled('1'))
  })
  it('suspends all fields on individual deselection and restores the latest edit', () => {
    const session = createQuoteSelection()
    let rows = session.set([filled('1')], ['1'], false)
    expect(rows).toEqual([])
    rows = session.set(rows, ['1'], true)
    rows[0].price = '99'
    rows = session.set(rows, ['1'], false)
    expect(session.set(rows, ['1'], true)[0]).toEqual({ ...filled('1'), price: '99' })
  })
  it('keeps suspended rows out of draft and submission payloads; repeated deselection preserves cache', () => {
    const session = createQuoteSelection()
    let rows = session.set([filled('1'), filled('2')], ['1'], false)
    rows = session.set(rows, ids, false)
    rows = session.set(rows, ids, false)
    expect(JSON.parse(JSON.stringify({ prices: rows }))).toEqual({ prices: [] })
    expect(session.set(rows, ['2', '1'], true)).toEqual([filled('2'), filled('1')])
  })
  it('clears cache on editor end and never shares it between quotes/inquiries', () => {
    const session = createQuoteSelection()
    session.set([filled('1')], ['1'], false)
    expect(createQuoteSelection().set([], ['1'], true)[0].price).toBe('')
    session.clear()
    expect(session.set([], ['1'], true)[0].price).toBe('')
  })
  it('preserves Excel paste order, per-row values and cached reprocessing fields', () => {
    const session = createQuoteSelection(), products = ids.map(id => ({ ...blankProduct(), id }))
    let rows = session.set([filled('1')], ids, true)
    rows = pastePrices('501\t2027-02-01\tA\n502\t2027-02-02\tB', products, rows, '20')
    expect(rows.find(row => row.productId === '20')).toMatchObject({ price: '501', delivery: '2027-02-01', remark: 'A' })
    expect(rows.find(row => row.productId === '21')).toMatchObject({ price: '502', delivery: '2027-02-02', remark: 'B' })
    rows = session.set(rows, ids, false)
    rows = session.set(rows, ids, true)
    expect(rows.find(row => row.productId === '1')).toEqual(filled('1'))
    expect(rows.find(row => row.productId === '21')?.price).toBe('502')
  })
})
