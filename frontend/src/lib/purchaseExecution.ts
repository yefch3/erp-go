export interface ConfirmationItem { id: string; qty: string; unitPrice: string }

export function buildConfirmationLines(
  items: ConfirmationItem[],
  quantities: Record<string, string>,
  prices: Record<string, string>,
) {
  return items.map((item) => ({
    po_item_id: Number(item.id),
    confirmed_qty: quantities[item.id] ?? item.qty,
    confirmed_unit_price: prices[item.id] ?? item.unitPrice,
  }))
}

export function isProductionDelayed(plannedDate: string, actualDate: string, today: string): boolean {
  return Boolean(plannedDate && !actualDate && plannedDate < today)
}
