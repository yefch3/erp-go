import { describe, expect, it } from 'vitest'
import { isDateOnlyExpired, utcBusinessDate } from './dateOnlyValidity'

describe('date-only validity', () => {
  it('uses the same UTC business date as the backend', () => {
    const now = new Date('2026-09-01T00:05:00Z')
    expect(utcBusinessDate(now)).toBe('2026-09-01')
    expect(isDateOnlyExpired('2026-08-31', now)).toBe(true)
    expect(isDateOnlyExpired('2026-09-01', now)).toBe(false)
  })

  it('accepts empty and timestamp-shaped values safely', () => {
    const now = new Date('2026-09-01T12:00:00Z')
    expect(isDateOnlyExpired('', now)).toBe(false)
    expect(isDateOnlyExpired(null, now)).toBe(false)
    expect(isDateOnlyExpired('2026-09-02T00:00:00Z', now)).toBe(false)
  })
})
