import { describe, expect, it } from 'vitest'
import { cargoQuantityTotals, inquiryStatusKey, presaleReferenceCount, quoteNoteText } from './shippingInquiryView'

describe('shipping inquiry presentation', () => {
  it('distinguishes the first execution inquiry from returned work', () => {
    expect(inquiryStatusKey('WAITING_REQUOTE')).toBe('shipping.inquiryAwaiting')
    expect(inquiryStatusKey('RETURNED')).toBe('shipping.inquiryReturned')
    expect(inquiryStatusKey('APPROVED')).toBe('shipping.handoffStatuses.APPROVED')
  })
  it('summarizes the current batch without combining different units', () => {
    expect(cargoQuantityTotals([
      { quantity: '4.0000', uomCode: 'MT' }, { quantity: '6', uomCode: 'MT' },
      { quantity: '20', uomCode: 'PCS' },
    ])).toEqual([{ unit: 'MT', quantity: 10 }, { unit: 'PCS', quantity: 20 }])
  })
  it('does not invent a quantity from missing or invalid data', () => {
    expect(cargoQuantityTotals([{ quantity: '', uomCode: 'MT' }, { quantity: 'bad', uomCode: 'MT' }])).toEqual([])
  })
  it('counts both current array snapshots and older object snapshots', () => {
    expect(presaleReferenceCount('[{"id":"1"},{"id":"2"}]')).toBe(2)
    expect(presaleReferenceCount('{"id":"1"}')).toBe(1)
  })
  it('does not claim malformed or empty snapshots contain a reference', () => {
    for (const value of [undefined, '', 'null', '[]', 'bad', '3', '"text"']) expect(presaleReferenceCount(value)).toBe(0)
  })
  it('preserves both the previous scheme description and remark in one note', () => {
    expect(quoteNoteText('Shanghai direct', 'Call before pickup')).toBe('Shanghai direct\nCall before pickup')
    expect(quoteNoteText('Same note', 'Same note')).toBe('Same note')
  })
  it('allows entirely blank notes and legacy single-field notes', () => {
    expect(quoteNoteText()).toBe('')
    expect(quoteNoteText('', 'Optional note')).toBe('Optional note')
    expect(quoteNoteText('Legacy description', '')).toBe('Legacy description')
  })
})
