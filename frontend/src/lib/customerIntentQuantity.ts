export type IntentQuantityError = '' | 'required' | 'exceeds_available'

export function defaultIntentQuantity(demand: unknown, available: unknown): number {
  const demandQty = Number(demand || 0)
  const availableQty = Number(available || 0)
  if (!(availableQty > 0)) return 0
  return Math.min(demandQty > 0 ? demandQty : availableQty, availableQty)
}

export function validateIntentQuantity(quantity: unknown, available: unknown): IntentQuantityError {
  const intentQty = Number(quantity || 0)
  const availableQty = Number(available || 0)
  if (!(intentQty > 0)) return 'required'
  if (!(availableQty > 0) || intentQty > availableQty) return 'exceeds_available'
  return ''
}

export function intentLineSubtotal(unitPrice: unknown, quantity: unknown): string {
  return (Number(unitPrice || 0) * Number(quantity || 0)).toFixed(2)
}
