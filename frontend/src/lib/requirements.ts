export interface PurchaseBatchSource {
  id: string
  quotationId?: string
  quotationNo?: string
  contractId?: string
  contractNo?: string
  status?: string
}

export function purchaseBatchKey(row: PurchaseBatchSource): string {
  // Finance-released purchasing belongs to the effective contract. A single
  // presale quotation can lead to more than one contract/version, which must
  // not be combined into one execution decision.
  if (row.status === 'WAITING_REQUOTE') {
    const contractId = String(row.contractId ?? '').trim()
    if (contractId && contractId !== '0') return `CONTRACT-ID:${contractId}`
    const contractNo = String(row.contractNo ?? '').trim()
    if (contractNo) return `CONTRACT-NO:${contractNo}`
  }
  const quotationId = String(row.quotationId ?? '').trim()
  if (quotationId && quotationId !== '0') return `QUOTATION-ID:${quotationId}`
  const quotationNo = String(row.quotationNo ?? '').trim()
  if (quotationNo) return `QUOTATION-NO:${quotationNo}`
  const contractId = String(row.contractId ?? '').trim()
  if (contractId && contractId !== '0') return `CONTRACT-ID:${contractId}`
  const contractNo = String(row.contractNo ?? '').trim()
  return contractNo ? `CONTRACT-NO:${contractNo}` : `MANUAL:${row.id}`
}
