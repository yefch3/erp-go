import { describe, expect, it } from 'vitest'
import { blankProduct } from './inquiryWorkspace'
import { fillProductColumn, firstSelectedValue, rowsBetween } from './inquiryColumnFill'

describe('inquiry column fill', () => {
  it('selects only the visible range and fills only the chosen field', () => {
    const rows = Array.from({ length: 5 }, (_, index) => ({
      ...blankProduct(), product: `Product ${index + 1}`, delivery: String(index), quantity: String(index + 1),
    }))
    const visible = [rows[0], rows[2], rows[4]]
    const selected = rowsBetween(visible, rows[4], rows[0])
    expect(selected).toEqual(visible)
    expect(fillProductColumn(selected, 'delivery', '2026-10-20')).toBe(3)
    expect(rows.map(row => row.delivery)).toEqual(['2026-10-20', '1', '2026-10-20', '3', '2026-10-20'])
    expect(rows.map(row => row.quantity)).toEqual(['1', '2', '3', '4', '5'])
    expect(firstSelectedValue(visible, selected, 'delivery')).toBe('2026-10-20')
  })

  it('supports template fields while leaving other fields intact', () => {
    const first = { ...blankProduct(), customFields: { grade: 'Q235', coating: 'None' } }
    const second = { ...blankProduct(), customFields: { grade: 'Q345', coating: 'Zinc' } }
    fillProductColumn([first, second], 'grade', 'Q420')
    expect([first.customFields.grade, second.customFields.grade]).toEqual(['Q420', 'Q420'])
    expect(second.customFields.coating).toBe('Zinc')
  })
})
