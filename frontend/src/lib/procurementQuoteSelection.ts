import type { Price } from './inquiryWorkspace'

export function quoteSelectionState(ids: string[], prices: Price[]) {
  const all = new Set(ids)
  const count = new Set(prices.filter(row => all.has(row.productId)).map(row => row.productId)).size
  return { count, total: all.size, checked: all.size > 0 && count === all.size, indeterminate: count > 0 && count < all.size }
}

// One instance per editor. Only active prices belong in the request body;
// unchecked rows live here until the editor is saved, cancelled or replaced.
export function createQuoteSelection() {
  const suspended = new Map<string, Price>()
  return {
    clear() { suspended.clear() },
    set(prices: Price[], ids: string[], selected: boolean): Price[] {
      const targets = new Set(ids)
      if (!selected) {
        for (const row of prices) if (targets.has(row.productId)) suspended.set(row.productId, { ...row })
        return prices.filter(row => !targets.has(row.productId))
      }
      const result = [...prices]
      const active = new Set(prices.map(row => row.productId))
      for (const productId of targets) {
        if (active.has(productId)) continue
        const saved = suspended.get(productId)
        result.push(saved ? { ...saved } : { productId, price: '', delivery: '', remark: '' })
        suspended.delete(productId)
        active.add(productId)
      }
      return result
    },
  }
}
