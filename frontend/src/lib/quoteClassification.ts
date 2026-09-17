export const procurementQuoteCategories = [
  {value: 'FOB_USD', currency: 'USD', labelKey: 'inquiryWorkspace.quotes.categories.fobUsd'},
  {value: 'FOB_CNY', currency: 'CNY', labelKey: 'inquiryWorkspace.quotes.categories.fobCny'},
  {value: 'ALL_IN_PORT_CNY', currency: 'CNY', labelKey: 'inquiryWorkspace.quotes.categories.allInPortCny'},
  {value: 'EX_FACTORY_CNY', currency: 'CNY', labelKey: 'inquiryWorkspace.quotes.categories.exFactoryCny'},
  {value: 'REPROCESSING_CNY', currency: 'CNY', labelKey: 'inquiryWorkspace.quotes.categories.reprocessingCny'},
  {value: 'DIRECT_CFR_USD', currency: 'USD', labelKey: 'inquiryWorkspace.quotes.categories.directCfrUsd'},
] as const

export type ProcurementQuoteCategory = typeof procurementQuoteCategories[number]['value']

export function procurementQuoteCategory(value: string | undefined) {
  return procurementQuoteCategories.find(category => category.value === value)
}

export function procurementQuoteCurrency(value: string | undefined): string | undefined {
  return procurementQuoteCategory(value)?.currency
}
