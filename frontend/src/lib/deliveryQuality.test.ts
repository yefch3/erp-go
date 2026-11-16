import { describe, expect, it } from 'vitest'
import { hasPassedDeliveryInspection, type DeliveryQualityTask } from './deliveryQuality'

const passed = (): DeliveryQualityTask => ({
  status: 'COMPLETED',
  lines: [{ requestedQty: '10.0000', qualifiedQty: '10', finalResult: 'PASS' }],
})

describe('delivery inspection feedback', () => {
  it('does not label missing or empty inspections as passed', () => {
    expect(hasPassedDeliveryInspection([])).toBe(false)
    expect(hasPassedDeliveryInspection([{ status: 'COMPLETED', lines: [] }])).toBe(false)
  })

  it.each(['WAITING', 'IN_PROGRESS', 'REINSPECTION'])('blocks %s even with stale passed lines', status => {
    expect(hasPassedDeliveryInspection([{ ...passed(), status }])).toBe(false)
  })

  it('blocks unresolved defects and administrative completion', () => {
    expect(hasPassedDeliveryInspection([{ ...passed(), procurementHandlingStatus: 'PENDING' }])).toBe(false)
    for (const result of ['PENDING', 'PARTIAL', 'FAIL']) {
      expect(hasPassedDeliveryInspection([{ status: 'COMPLETED', lines: [
        { requestedQty: '10', qualifiedQty: '8', finalResult: result },
      ] }])).toBe(false)
    }
  })

  it('requires every task and its full inspected quantity to pass', () => {
    expect(hasPassedDeliveryInspection([passed()])).toBe(true)
    expect(hasPassedDeliveryInspection([passed(), { ...passed(), status: 'WAITING' }])).toBe(false)
    expect(hasPassedDeliveryInspection([{ status: 'COMPLETED', lines: [
      { requestedQty: '10', qualifiedQty: '9', finalResult: 'PASS' },
    ] }])).toBe(false)
  })
})
