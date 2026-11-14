import { describe, expect, it } from 'vitest'
import { shippingHandoffStage, shippingTodoEntry } from './shippingTodoEntry'

describe('shipping todo source entry', () => {
  it('opens each inquiry at its own handoff ID rather than the contract list', () => {
    for (const id of ['201', '202', '9223372036854775807']) {
      expect(shippingTodoEntry({ id, status: 'WAITING_REQUOTE' })).toEqual({
        to: { path: '/shipping/requirements', query: { handoff: id } }, actionKey: 'shipping.d4Start',
      })
    }
  })
  it('labels returned work as a repeated inquiry', () => {
    expect(shippingTodoEntry({ id: '201', status: 'RETURNED' })?.actionKey).toBe('shipping.todoRetryInquiry')
  })
  it.each(['DRAFT', 'PENDING_APPROVAL', 'APPROVED', 'CONTRACT_UPLOADED', 'CONTRACT_VERIFIED', 'PAYMENT_REQUESTED', 'SCHEDULED'])('opens %s in the exact order, even if the source link was an inquiry', status => {
    expect(shippingTodoEntry({ id: '202', status })).toEqual({
      to: { path: '/shipping/orders', query: { handoff: '202' } }, actionKey: 'shipping.todoViewOrder',
    })
  })
  it('selects the correct order tab independently of the current list filter', () => {
    expect(shippingHandoffStage('DRAFT')?.filter).toBe('DRAFT')
    expect(shippingHandoffStage('PENDING_APPROVAL')?.filter).toBe('PENDING_APPROVAL')
    expect(shippingHandoffStage('APPROVED')?.filter).toBe('CONTRACT_AND_DELEGATED')
  })
  it('keeps ended tasks out of the inquiry editor', () => {
    expect(shippingHandoffStage('CANCELLED')?.path).toBe('/shipping/orders')
    expect(shippingHandoffStage('SUPERSEDED')?.path).toBe('/shipping/orders')
  })
  it('does not guess another task when the reference is invalid or unknown', () => {
    for (const id of ['', '0', '-1', 'x', '1/2']) expect(shippingTodoEntry({ id, status: 'WAITING_REQUOTE' })).toBeNull()
    expect(shippingTodoEntry({ id: '202', status: 'UNKNOWN' })).toBeNull()
  })
})
