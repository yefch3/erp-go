import { describe, it, expect } from 'vitest'
import { historicalLineHeaders, parseHistoricalLines } from './historicalLineImport'
describe('historical purchase line import', () => {
  it('accepts numeric Excel cells, optional prices and skips empty rows', () => {
    const r = parseHistoricalLines([historicalLineHeaders, ['', '钢卷', 'Q235', 12.5, 'MT', 0], [], ['', '钢管', '', 3, 'PCS', '']])
    expect(r.errors).toEqual([])
    expect(r.lines.map(l => [l.qty, l.unitPrice])).toEqual([['12.5', '0'], ['3', '']])
  })
  it('keeps valid rows and reports the original Excel row number', () => {
    const r = parseHistoricalLines([historicalLineHeaders, ['', '', '', -1, '', 'abc'], ['', '钢', '', 2, 'MT', 3]])
    expect(r.lines).toHaveLength(1)
    expect(r.errors[0]).toContain('第 2 行')
  })
  it('rejects incorrect headers', () => expect(() => parseHistoricalLines([['名称']])).toThrow('表头'))
})
