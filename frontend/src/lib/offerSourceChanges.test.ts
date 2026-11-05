import { describe, expect, it } from 'vitest'
import { blankProduct, blankQuote, type Quote } from './inquiryWorkspace'
import { offerSourceChanges } from './offerSourceChanges'

function fixture() {
  const quote: Quote = { id: 'q1', kind: 'PROCUREMENT', version: 1, body: { ...blankQuote(), company: 'Factory A', prices: [{ productId: 'p1', price: '500', delivery: '2026-12-24', remark: '' }] }, authorId: '1', author: 'Buyer', submittedAt: '2026-11-05', updatedBy: '1', updatedAt: '', canEdit: false }
  const selection = { productId: 'p1', category: 'DIRECT_CFR_USD', quoteId: 'q1', cfrUnitPrice: '500' }
  const body = { categorySelections: [selection], customerSelections: [{ ...selection, product: { ...blankProduct(), id: 'p1', product: 'Steel', quantity: '10' }, quote: structuredClone(quote) }], customerLogistics: [] as Quote[] }
  return { quote, body }
}

function reverseKeys(value: any): any {
  if (Array.isArray(value)) return value.map(reverseKeys)
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).reverse().map(([k, v]) => [k, reverseKeys(v)]))
  return value
}

describe('recorded quote source comparison', () => {
  it('does not report JSONB object ordering as a source change, including nested prices', () => {
    const { quote, body } = fixture()
    body.customerSelections[0].quote.body = reverseKeys(quote.body)
    expect(JSON.stringify(body.customerSelections[0].quote.body)).not.toBe(JSON.stringify(quote.body))
    expect(offerSourceChanges(body, [quote])).toEqual({ selectionChanged: false, sources: [] })
  })
  it.each(['price', 'delivery', 'payment', 'version'])('preserves real %s changes and the recorded snapshot', field => {
    const { quote, body } = fixture()
    if (field === 'price') quote.body.prices[0].price = '505'
    if (field === 'delivery') quote.body.delivery = '2026-12-30'
    if (field === 'payment') quote.body.paymentTerms = 'New terms'
    if (field === 'version') quote.version = 2
    expect(offerSourceChanges(body, [quote])).toMatchObject({ selectionChanged: false, sources: [{ id: 'q1', company: 'Factory A', productIds: ['p1'], oldVersion: 1, removed: false }] })
    expect(body.customerSelections[0].quote.body.prices[0].price).toBe('500')
    expect(body.customerSelections[0].quote.version).toBe(1)
  })
  it('names all affected specifications once per supplier quote', () => {
    const { quote, body } = fixture()
    const second = { ...body.customerSelections[0], productId: 'p2' }
    body.customerSelections.push(second)
    body.categorySelections.push({ ...body.categorySelections[0], productId: 'p2' })
    quote.version = 2
    expect(offerSourceChanges(body, [quote]).sources).toHaveLength(1)
    expect(offerSourceChanges(body, [quote]).sources[0].productIds).toEqual(['p1', 'p2'])
  })
  it('identifies a removed source without changing the saved options', () => {
    const { body } = fixture()
    expect(offerSourceChanges(body, []).sources[0]).toMatchObject({ id: 'q1', removed: true, newVersion: undefined })
    expect(body.customerSelections).toHaveLength(1)
  })
  it('distinguishes an unapplied calculation from a supplier change', () => {
    const { quote, body } = fixture()
    body.categorySelections[0] = { ...body.categorySelections[0], cfrUnitPrice: '510' }
    expect(offerSourceChanges(body, [quote])).toEqual({ selectionChanged: true, sources: [] })
  })
  it('does not classify customer negotiations or quote metadata as changed source content', () => {
    const { quote, body } = fixture()
    const negotiating = { ...body, negotiations: [{ proposedPrice: '520', status: 'AGREED' }] }
    quote.author = 'Another buyer'
    expect(offerSourceChanges(negotiating, [quote])).toEqual({ selectionChanged: false, sources: [] })
  })
  it('checks selected logistics content and distinguishes a deselected freight option', () => {
    const { quote, body } = fixture()
    const freight = { ...structuredClone(quote), id: 'f1', kind: 'LOGISTICS', body: { ...blankQuote(), company: 'Forwarder', route: 'A → B' } }
    Object.assign(body.categorySelections[0], { freightQuoteId: 'f1', freightQuoteVersion: 1 })
    Object.assign(body.customerSelections[0], { freightQuoteId: 'f1', freightQuoteVersion: 1 })
    body.customerLogistics = [structuredClone(freight)]
    freight.body.route = 'A → C'
    expect(offerSourceChanges(body, [quote, freight])).toMatchObject({ selectionChanged: false, sources: [{ id: 'f1', productIds: ['p1'] }] })
    Object.assign(body.categorySelections[0], { freightQuoteId: '', freightQuoteVersion: 0 })
    expect(offerSourceChanges(body, [quote, freight]).selectionChanged).toBe(true)
  })
})
