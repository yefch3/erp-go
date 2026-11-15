export type HistoricalShippingContext = {exportContractNo?: string; destinationPort?: string}

// The metadata editor must never send the order object (prices/status/etc.).
// Missing fields from old responses remain genuinely optional, not "undefined".
export function historicalShippingContext(value?: HistoricalShippingContext | null) {
  return {
    exportContractNo: value?.exportContractNo?.trim() ?? '',
    destinationPort: value?.destinationPort?.trim() ?? '',
  }
}
