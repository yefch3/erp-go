import { describe, expect, it } from 'vitest'
import { customerDecisionPayload } from './customerDecisionPayload'

describe('customer decision payload', () => {
  it('uses the protobuf customer_decided_at field', () => {
    const payload = customerDecisionPayload({
      selectionId: 9,
      accepted: true,
      customerContact: 'Customer A',
      decisionNote: 'accepted',
      decidedAt: '2026-08-31T23:02:27-04:00',
      itemPrices: [],
      shipmentPrices: [],
    })

    expect(payload.customer_decided_at).toBe('2026-08-31T23:02:27-04:00')
    expect(payload).not.toHaveProperty('decided_at')
  })
})
