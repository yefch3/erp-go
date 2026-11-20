import type { Price } from './inquiryWorkspace'

export function quoteSelectionState(ids: string[], selected: ReadonlySet<string>) {
  const all = new Set(ids)
  const count = [...all].filter(id => selected.has(id)).length
  return { count, total: all.size, checked: all.size > 0 && count === all.size, indeterminate: count > 0 && count < all.size }
}

export function emptyProcurementPrice(productId: string): Price {
  return { productId, price: '', delivery: '', remark: '' }
}

export function procurementPricesForSave(rows: Price[], selected: ReadonlySet<string>, draft: boolean) {
  const byId = new Map(rows.map(row => [row.productId, row]))
  return {
    prices: Array.from(selected, id => byId.get(id) ?? emptyProcurementPrice(id)),
    draftPrices: draft
      ? rows.filter(row => !selected.has(row.productId) && [row.price, row.factoryPrice, row.fobPrice, row.slitting, row.delivery, row.remark].some(value => value?.trim()))
      : undefined,
  }
}
