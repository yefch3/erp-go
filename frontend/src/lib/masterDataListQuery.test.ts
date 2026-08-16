import { describe, expect, it } from 'vitest'
import { masterDataListQuery, queryPage, queryText } from './masterDataListQuery'

describe('master data list query', () => {
  it('reads the first router query value and trims it', () => {
    expect(queryText(['  CN  ', 'US'])).toBe('CN')
  })

  it('falls back to page one for invalid values', () => {
    expect(queryPage('0')).toBe(1)
    expect(queryPage('wrong')).toBe(1)
    expect(queryPage('3')).toBe(3)
  })

  it('omits empty filters and the first page', () => {
    expect(masterDataListQuery({ keyword: ' ', page: 1, status: '' })).toEqual({})
  })

  it('keeps active filters and later pages', () => {
    expect(masterDataListQuery({
      keyword: 'demo',
      country: 'CN',
      businessType: 'CARRIER',
      page: 2,
    })).toEqual({
      keyword: 'demo',
      country: 'CN',
      business_type: 'CARRIER',
      page: '2',
    })
  })
})
