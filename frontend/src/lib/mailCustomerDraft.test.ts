import { describe, expect, it } from 'vitest'
import {
  classifyMailCustomerDuplicates,
  customerDraftFromMail,
  customerDraftFromSender,
  existingCompanyForMailContact,
} from './mailCustomerDraft'

describe('customerDraftFromSender', () => {
  it('uses the sender display name and email from the mail header', () => {
    expect(customerDraftFromSender(
      'Hans Weber', ' hans@example.com ',
    )).toEqual({ name: 'Hans Weber', email: 'hans@example.com' })
  })

  it('leaves the name empty when the display name is unavailable', () => {
    expect(customerDraftFromSender('', 'new.customer@example.com')).toEqual({
      name: '', email: 'new.customer@example.com',
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

describe('classifyMailCustomerDuplicates', () => {
  it('精确邮箱已存在时优先阻止新建', () => {
    const owner = { id: '7', code: 'C0007', name: 'Acme', matchFields: ['EMAIL', 'NAME'] }
    expect(classifyMailCustomerDuplicates([owner])).toEqual({
      emailOwner: owner,
      existingCompany: null,
      suggestions: [],
    })
  })

  it('同一家公司时改为添加联系人', () => {
    const company = { id: '7', code: 'C0007', name: 'Acme', matchFields: ['NAME'] }
    const similar = { id: '8', code: 'C0008', name: 'Acme Trading', matchFields: ['SIMILAR_NAME'] }
    expect(classifyMailCustomerDuplicates([company, similar])).toEqual({
      emailOwner: null,
      existingCompany: company,
      suggestions: [similar],
    })
  })

  it('相似名称只作候选，不自动归入', () => {
    const similar = { id: '8', code: 'C0008', name: 'Acme Trading', matchFields: ['SIMILAR_NAME'] }
    expect(classifyMailCustomerDuplicates([similar])).toEqual({
      emailOwner: null,
      existingCompany: null,
      suggestions: [similar],
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
