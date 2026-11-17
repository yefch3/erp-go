import { describe, expect, it } from 'vitest'
import { claimExpenseType, expensePayload, expenseTypes, missingExpenseFields } from './reimbursementExpense'
import zh from '../locales/zh'
import en from '../locales/en'
import es from '../locales/es'

const base = { expenseType: 'TRAVEL', expenseDate: '', tripStart: '2026-11-16', tripEnd: '2026-11-17', origin: 'Shanghai', destination: 'Beijing', purpose: 'Customer visit', amount: '80', currency: 'CNY', paymentAccount: 'Test account', note: 'Keep me' }
describe('expense reimbursement types', () => {
  it('keeps old claims classified as travel', () => {
    expect(claimExpenseType(undefined)).toBe('TRAVEL')
    expect(claimExpenseType('')).toBe('TRAVEL')
    expect(claimExpenseType('OFFICE_SUPPLIES')).toBe('OFFICE_SUPPLIES')
  })
  it('requires travel fields only for travel', () => {
    expect(missingExpenseFields(base)).toBe(false)
    expect(missingExpenseFields({ ...base, tripStart: '' })).toBe(true)
    expect(missingExpenseFields({ ...base, destination: ' ' })).toBe(true)
    for (const expenseType of ['OFFICE_SUPPLIES', 'OTHER']) {
      const form = { ...base, expenseType, expenseDate: '2026-11-17', tripStart: '', tripEnd: '', origin: '', destination: '' }
      expect(missingExpenseFields(form)).toBe(false)
      expect(missingExpenseFields({ ...form, expenseDate: '' })).toBe(true)
      expect(missingExpenseFields({ ...form, purpose: ' ' })).toBe(true)
      expect(missingExpenseFields({ ...form, paymentAccount: '' })).toBe(true)
    }
    expect(missingExpenseFields({ ...base, expenseType: 'INVALID' })).toBe(true)
  })
  it('sends only relevant dates and locations, without mutating editing state or evidence', () => {
    const form = { ...base, expenseType: 'OFFICE_SUPPLIES', expenseDate: '2026-11-17', files: ['invoice.pdf'] }
    expect(expensePayload(form)).toEqual({ ...form, tripStart: '', tripEnd: '', origin: '', destination: '' })
    expect(form.tripStart).toBe(base.tripStart)
    expect(expensePayload({ ...form, expenseType: 'TRAVEL' })).toEqual({ ...form, expenseType: 'TRAVEL', expenseDate: '' })
  })
  it('provides all expense types and generic entry labels in Chinese, English and Spanish', () => {
    for (const locale of [zh, en, es]) {
      for (const key of [...expenseTypes, 'type', 'date', 'purpose', 'create', 'edit', 'empty', 'hint'] as const) {
        expect(locale.reimbursementExpense[key]).toBeTruthy()
      }
    }
    expect(zh.supplierRecon.travelTitle).toBe('报销')
    expect(zh.flows.biz.TRAVEL_REIMBURSEMENT).toBe('报销')
  })
})
