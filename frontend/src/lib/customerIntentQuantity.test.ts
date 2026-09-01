import { describe, expect, it } from 'vitest'
import { defaultIntentQuantity, intentLineSubtotal, validateIntentQuantity } from './customerIntentQuantity'

describe('customer intent quantity', () => {
  it('defaults to demand without exceeding supplier availability', () => {
    expect(defaultIntentQuantity('20', '25')).toBe(20)
    expect(defaultIntentQuantity('20', '12')).toBe(12)
  })

  it('requires a positive quantity within availability', () => {
    expect(validateIntentQuantity('', '20')).toBe('required')
    expect(validateIntentQuantity('21', '20')).toBe('exceeds_available')
    expect(validateIntentQuantity('12.5', '20')).toBe('')
  })

  it('recalculates the customer-facing product subtotal', () => {
    expect(intentLineSubtotal('2.5', '12')).toBe('30.00')
  })
})
