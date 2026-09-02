import { describe, expect, it } from 'vitest'
import { customerDraftFromSender } from './mailCustomerDraft'

describe('customerDraftFromSender', () => {
  it('uses the sender display name and email from the mail header', () => {
    expect(customerDraftFromSender(
      'Hans Weber', ' hans@example.com ',
    )).toEqual({ name: 'Hans Weber', email: 'hans@example.com' })
  })

  it('falls back to the email local part when the display name is empty', () => {
    expect(customerDraftFromSender('', 'new.customer@example.com')).toEqual({
      name: 'new.customer', email: 'new.customer@example.com',
    })
  })
})
