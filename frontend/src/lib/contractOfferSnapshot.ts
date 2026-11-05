type OfferProduct = { id: string; product: string; unit: string }
type OfferTransport = { quoteId: string; title: string; currency: string; price: string; accepted: boolean; quantities: Record<string, string> }
type ContractOfferSnapshot = { contact: string; transports: OfferTransport[]; lines: OfferProduct[] }

// Older/category confirmations legitimately store transports:null: freight is
// already included in the price. Normalize only the read model, not the deal.
export function parseContractOfferSnapshot(raw?: string): ContractOfferSnapshot | null {
  try {
    const value = JSON.parse(raw || 'null')
    if (!value || typeof value !== 'object' || Array.isArray(value)) return null
    return {
      contact: typeof value.contact === 'string' ? value.contact : '',
      transports: Array.isArray(value.transports) ? value.transports : [],
      lines: Array.isArray(value.lines) ? value.lines : [],
    }
  } catch { return null }
}
