import {describe,expect,it} from 'vitest'
import {procurementQuoteCategories,procurementQuoteCategory,procurementQuoteCurrency} from './quoteClassification'

describe('procurement quote classifications',()=>{
  it('keeps the six agreed categories in display order',()=>{
    expect(procurementQuoteCategories.map(category=>category.value)).toEqual([
      'FOB_USD','FOB_CNY','ALL_IN_PORT_CNY','EX_FACTORY_CNY','REPROCESSING_CNY','DIRECT_CFR_USD',
    ])
  })

  it('locks each category to its agreed currency',()=>{
    expect(procurementQuoteCurrency('FOB_USD')).toBe('USD')
    for(const category of procurementQuoteCategories.slice(1,5))expect(category.currency).toBe('CNY')
    expect(procurementQuoteCurrency('DIRECT_CFR_USD')).toBe('USD')
    expect(procurementQuoteCategory('FOB')).toBeUndefined()
  })
})
