export interface PurchaseImportCandidate {
  requirementId: string
  contractNo: string
  productName: string
  productCode: string
  spec: string
  uomCode: string
  requiredQty: string
  orderedQty: string
  openQty: string
  status: string
}

export interface PurchaseImportRow {
  rowNo: number
  product: string
  quantity: string
  quantityUnit: string
  unitPrice: string
  candidates: PurchaseImportCandidate[]
  requirementId: string
  result: string
  message?: string
  suggestedRequirementId?: string
}

export function evaluatePurchaseImportRow(row: PurchaseImportRow): PurchaseImportRow {
  const candidate = row.candidates.find((item) => String(item.requirementId) === String(row.requirementId))
  if (!candidate) return { ...row, result: row.candidates.length ? 'MULTIPLE' : 'NOT_FOUND' }
  if (candidate.uomCode.trim().toLowerCase() !== row.quantityUnit.trim().toLowerCase()) {
    return { ...row, result: 'UNIT_MISMATCH' }
  }
  const qty = Number(row.quantity)
  if (!Number.isFinite(qty) || qty <= 0 || qty > Number(candidate.openQty)) {
    return { ...row, result: 'QTY_EXCEEDED' }
  }
  const price = row.unitPrice === '' ? 0 : Number(row.unitPrice)
  if (!Number.isFinite(price) || price < 0) return { ...row, result: 'PRICE_INVALID' }
  return { ...row, result: 'MATCHED' }
}

export function validatePurchaseImportRows(rows: PurchaseImportRow[]): string {
  if (!rows.length || rows.some((row) => evaluatePurchaseImportRow(row).result !== 'MATCHED')) {
    return 'INVALID_LINE'
  }
  const requirements = rows.map((row) => String(row.requirementId))
  if (new Set(requirements).size !== requirements.length) return 'DUPLICATED'
  return ''
}

export function purchaseImportTotals(rows: PurchaseImportRow[]) {
  return rows.reduce((total, row) => {
    const qty = Number(row.quantity) || 0
    const price = Number(row.unitPrice) || 0
    return { quantity: total.quantity + qty, amount: total.amount + qty * price }
  }, { quantity: 0, amount: 0 })
}
