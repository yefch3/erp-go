import { describe, expect, it } from 'vitest'
import { deletionWindow, deleteSelections, type MasterDeleteRow } from './masterdataBulkDelete'
describe('bulk deletion selection and dates', () => {
  it('includes both calendar dates, with an exclusive next-day boundary', () => {
    const result = deletionWindow(['2026-09-24', '2026-09-24'])
    expect(new Date(result.startAt).getDate()).toBe(24)
    expect(new Date(result.endAt).getDate()).toBe(25)
    expect(new Date(result.startAt).getHours()).toBe(0)
  })
  it('rejects missing, inverted and invalid calendar dates', () => {
    for (const dates of [[], ['2026-09-25', '2026-09-24'], ['2026-02-30', '2026-03-01']]) expect(() => deletionWindow(dates)).toThrow()
  })
  it('only sends selected eligible preview rows, including rows on later pages', () => {
    const rows = Array.from({ length: 120 }, (_, i) => ({ id: String(i), version: `v${i}`, blockedReason: i === 3 ? 'used' : '' } as MasterDeleteRow))
    expect(deleteSelections(rows, ['0', '3', '110', '999'])).toEqual([{ id: '0', version: 'v0' }, { id: '110', version: 'v110' }])
  })
})
