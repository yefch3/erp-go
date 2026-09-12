import { describe, expect, it } from 'vitest'
import { purchaseBatchKey } from './requirements'

describe('purchaseBatchKey', () => {
  it('treats quotation id 0 as missing and keeps different contracts separate', () => {
    const first = purchaseBatchKey({ id: '1', quotationId: '0', contractId: '4', contractNo: 'CT-0001' })
    const second = purchaseBatchKey({ id: '2', quotationId: '0', contractId: '5', contractNo: 'CT-0002' })

    expect(first).toBe('CONTRACT-ID:4')
    expect(second).toBe('CONTRACT-ID:5')
    expect(first).not.toBe(second)
  })

  it('groups requirements from the same valid customer quotation', () => {
    expect(purchaseBatchKey({ id: '1', quotationId: '27', contractId: '4' }))
      .toBe(purchaseBatchKey({ id: '2', quotationId: '27', contractId: '5' }))
  })
})
