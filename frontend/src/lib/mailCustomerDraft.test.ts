import { describe, expect, it } from 'vitest'
import { customerDraftFromMail, customerDraftFromSender, existingCompanyForMailContact } from './mailCustomerDraft'

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

describe('existingCompanyForMailContact', () => {
  it('returns the single exact company match for a different contact', () => {
    const company = { id: '7', code: 'C0007', name: 'Acme Steel', matchFields: ['NAME'] }
    expect(existingCompanyForMailContact([
      company,
      { id: '8', code: 'C0008', name: 'Acme', matchFields: ['SIMILAR_NAME'] },
    ])).toEqual(company)
  })

  it('treats an exact tax id as the same company', () => {
    const company = { id: '7', code: 'C0007', name: 'Acme Steel Ltd.', matchFields: ['TAX_ID'] }
    expect(existingCompanyForMailContact([company])).toEqual(company)
  })

  it('does not attach on a similar or ambiguous company name', () => {
    expect(existingCompanyForMailContact([
      { id: '7', code: 'C0007', name: 'Acme Steel', matchFields: ['SIMILAR_NAME'] },
    ])).toBeNull()
    expect(existingCompanyForMailContact([
      { id: '7', code: 'C0007', name: 'Acme Steel', matchFields: ['NAME'] },
      { id: '8', code: 'C0008', name: 'Acme Steel', matchFields: ['NAME'] },
    ])).toBeNull()
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
