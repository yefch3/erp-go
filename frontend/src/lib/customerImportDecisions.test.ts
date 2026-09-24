import { describe, expect, it } from 'vitest'
import { applyCustomerImportActions } from './customerImportDecisions'

describe('customer import batch actions', () => {
  it('only applies existing-customer and duplicate-contact actions where needed', () => {
    const rows = [{ name: 'new' }, { name: 'existing' }, { name: 'existing with new contact' }]
    const verdicts = [{}, { existingCustomer: true, duplicateContact: true }, { existingCustomer: true }]

    expect(applyCustomerImportActions(rows, verdicts, 'UPDATE', 'SKIP')).toEqual([
      { name: 'new' },
      { name: 'existing', customerAction: 'UPDATE', contactAction: 'SKIP' },
      { name: 'existing with new contact', customerAction: 'UPDATE' },
    ])
  })
})
