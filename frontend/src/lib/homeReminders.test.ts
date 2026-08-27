import { describe, expect, it } from 'vitest'
import { homeReminderTagType } from './homeReminders'

describe('homeReminderTagType', () => {
  it('keeps overdue and upcoming reminders visually distinct', () => {
    expect(homeReminderTagType('OVERDUE')).toBe('danger')
    expect(homeReminderTagType('UPCOMING')).toBe('warning')
    expect(homeReminderTagType('REMINDER')).toBe('info')
  })
})
