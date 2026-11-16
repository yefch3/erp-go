import { describe, expect, it } from 'vitest'
import { parseContractOfferSnapshot } from './contractOfferSnapshot'

describe('contract offer snapshots', () => {
  it('opens category contracts with null optional arrays and preserves the contact', () => {
    const snapshot = parseContractOfferSnapshot('{"contact":"Buyer","transports":null,"lines":null}')
    expect(snapshot).toEqual({ contact: 'Buyer', transports: [], lines: [] })
    expect(snapshot?.transports.length).toBe(0)
  })
  it('handles old missing arrays and invalid input', () => {
    expect(parseContractOfferSnapshot('{}')).toEqual({ contact: '', transports: [], lines: [] })
    for (const raw of ['', 'null', '[]', 'invalid']) expect(parseContractOfferSnapshot(raw)).toBeNull()
  })
  it('keeps populated transport and product snapshots intact', () => {
    const snapshot = { contact: 'Buyer', transports: [{ quoteId: '7', title: 'Ocean', accepted: true, quantities: { a: '10' } }], lines: [{ id: 'a', product: 'Steel', unit: 'MT' }] }
    expect(parseContractOfferSnapshot(JSON.stringify(snapshot))).toEqual(snapshot)
  })
})
