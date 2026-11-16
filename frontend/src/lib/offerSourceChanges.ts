import type { CategorySelection, OfferBody } from './customerOffer'
import type { Quote } from './inquiryWorkspace'

// JSONB changes object key order. Preserve array order and every field value;
// only object key ordering is irrelevant to the recorded quote content.
function canonical(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${canonical((value as Record<string, unknown>)[key])}`).join(',')}}`
  }
  return JSON.stringify(value) ?? 'undefined'
}

export function offerSourceChanges(body: Pick<OfferBody, 'categorySelections' | 'customerSelections' | 'customerLogistics'>, quotes: Quote[]) {
  const basis = (s: CategorySelection) => canonical([s.productId, s.category, s.quoteId, s.freightQuoteId || '', s.freightQuoteVersion || 0, s.cfrUnitPrice || ''])
  const selections = body.categorySelections || []
  const staged = body.customerSelections || []
  const freightIds = [...new Set(selections.filter(s => !!s.freightQuoteVersion).map(s => s.freightQuoteId).filter(Boolean))].sort()
  const selectionChanged = canonical(selections.map(basis).sort()) !== canonical(staged.map(basis).sort()) ||
    canonical(freightIds) !== canonical((body.customerLogistics || []).map(q => q.id).sort())
  const oldQuotes = new Map<string, Quote>()
  for (const q of [...staged.map(s => s.quote), ...(body.customerLogistics || [])]) oldQuotes.set(q.id, q)
  const sources = [...oldQuotes.values()].flatMap(old => {
    const current = quotes.find(q => q.id === old.id)
    if (current && current.version === old.version && canonical(current.body) === canonical(old.body)) return []
    return [{ id: old.id, company: old.body.company || old.id, oldVersion: old.version,
      newVersion: current?.version, removed: !current,
      productIds: [...new Set(staged.filter(s => s.quoteId === old.id || s.freightQuoteId === old.id).map(s => s.productId))] }]
  })
  return { selectionChanged, sources }
}
