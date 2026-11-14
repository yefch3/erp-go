export function inquiryStatusKey(status: string) {
  if (status === 'WAITING_REQUOTE') return 'shipping.inquiryAwaiting'
  if (status === 'RETURNED') return 'shipping.inquiryReturned'
  return `shipping.handoffStatuses.${status}`
}

export function cargoQuantityTotals(items: readonly { quantity: string; uomCode: string }[]) {
  const groups = new Map<string, number>()
  for (const item of items) {
    const qty = Number(item.quantity)
    if (!item.quantity.trim() || !Number.isFinite(qty) || qty < 0) continue
    const unit = item.uomCode || '—'
    groups.set(unit, (groups.get(unit) || 0) + qty)
  }
  return [...groups].map(([unit, quantity]) => ({ unit, quantity }))
}

export function presaleReferenceCount(json?: string) {
  try {
    const value = JSON.parse(json || 'null')
    if (Array.isArray(value)) return value.filter(item => item && typeof item === 'object' && !Array.isArray(item)).length
    return value && typeof value === 'object' ? 1 : 0
  } catch { return 0 }
}

// Existing quotes may contain both fields. Show both in one editor without
// migrating or overwriting either stored field merely by opening the editor.
export function quoteNoteText(serviceOption?: string, remark?: string) {
  const description = (serviceOption || '').trim()
  const note = (remark || '').trim()
  return description && note && description !== note ? `${description}\n${note}` : description || note
}
