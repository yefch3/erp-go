export interface PurchaseBatchSource {
  id: string
  quotationId?: string
  quotationNo?: string
  contractId?: string
  contractNo?: string
}

export function purchaseBatchKey(row: PurchaseBatchSource): string {
  const quotationId = String(row.quotationId ?? '').trim()
  if (quotationId && quotationId !== '0') return `QUOTATION-ID:${quotationId}`
  const quotationNo = String(row.quotationNo ?? '').trim()
  if (quotationNo) return `QUOTATION-NO:${quotationNo}`
  const contractId = String(row.contractId ?? '').trim()
  if (contractId && contractId !== '0') return `CONTRACT-ID:${contractId}`
  const contractNo = String(row.contractNo ?? '').trim()
  return contractNo ? `CONTRACT-NO:${contractNo}` : `MANUAL:${row.id}`
}
