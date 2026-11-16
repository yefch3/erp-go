export type FulfillmentGoods = { qty: string; receivedQty: string; uomCode: string }

// Quantities with different units cannot be combined into one misleading total.
export function fulfillmentTotals(goods: readonly FulfillmentGoods[]) {
  const totals = new Map<string, { unit: string; ordered: number; delivered: number; remaining: number }>()
  for (const item of goods) {
    const unit = item.uomCode || '—'
    const total = totals.get(unit) || { unit, ordered: 0, delivered: 0, remaining: 0 }
    const qty = Number(item.qty), received = Number(item.receivedQty)
    if (!Number.isFinite(qty) || !Number.isFinite(received)) continue
    total.ordered += qty
    total.delivered += received
    total.remaining += Math.max(0, qty - received)
    totals.set(unit, total)
  }
  return [...totals.values()]
}

export function fulfillmentNumber(value: string | number) {
  const number = Number(value)
  return Number.isFinite(number) ? number.toLocaleString('en-US', { maximumFractionDigits: 4 }) : '—'
}
