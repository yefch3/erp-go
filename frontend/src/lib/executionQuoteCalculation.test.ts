import { describe, expect, it } from 'vitest'
import { calculateExecutionReferencePrice, type ExecutionCategoryCalculation } from './executionQuoteCalculation'

const input: ExecutionCategoryCalculation = {
  exchangeRate: '10',
  portCharge: '10',
  inlandFreight: '20',
  loss: '5',
  interestRate: '6',
  interestDays: '60',
}

describe('execution quote calculation', () => {
  it.each([
    ['FOB_USD', 101],
    ['FOB_CNY', 10.1],
    ['ALL_IN_PORT_CNY', 11.11],
    ['EX_FACTORY_CNY', 13.13],
    ['REPROCESSING_CNY', 13.635],
    ['DIRECT_CFR_USD', 101],
  ])('calculates %s using its displayed trade-term formula', (category, expected) => {
    expect(calculateExecutionReferencePrice(category, '100', input)).toBeCloseTo(expected, 6)
  })

  it('requires category inputs instead of silently treating blanks as zero', () => {
    expect(() => calculateExecutionReferencePrice('FOB_CNY', '100', { ...input, exchangeRate: '' })).toThrow('请填写有效的汇率')
  })
})
