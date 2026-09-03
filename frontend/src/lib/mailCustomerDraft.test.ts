import { describe, expect, it } from 'vitest'
import { customerDraftFromMail, customerDraftFromSender } from './mailCustomerDraft'

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

describe('customerDraftFromMail', () => {
  const own = ['sales@example.com', 'second@example.com']

  it('uses From for a customer reply, including later replies in a thread', () => {
    expect(customerDraftFromMail({
      fromName: 'Hans Weber', fromEmail: 'hans@example.com', toEmail: 'sales@example.com',
    }, own)).toEqual({ name: 'Hans Weber', email: 'hans@example.com' })
  })

  it('uses To when the newest conversation copy is our sent reply', () => {
    expect(customerDraftFromMail({
      fromName: 'Sales', fromEmail: 'sales@example.com', toName: 'Hans Weber', toEmail: 'hans@example.com',
    }, own)).toEqual({ name: 'Hans Weber', email: 'hans@example.com' })
  })

  it('does not offer an internal address as a customer', () => {
    expect(customerDraftFromMail({
      fromEmail: 'sales@example.com', toEmail: 'second@example.com',
    }, own)).toBeNull()
  })
})
