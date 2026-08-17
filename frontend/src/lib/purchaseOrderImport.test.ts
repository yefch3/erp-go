import { describe, expect, it } from 'vitest'
import { evaluatePurchaseImportRow, purchaseImportTotals, validatePurchaseImportRows, type PurchaseImportRow } from './purchaseOrderImport'

function row(overrides: Partial<PurchaseImportRow> = {}): PurchaseImportRow {
  return {
    rowNo: 2, product: '镀锌卷', quantity: '10', quantityUnit: 'TON', unitPrice: '',
    candidates: [{ requirementId: '7', contractNo: 'CT-1', productName: '镀锌卷', productCode: 'P1', spec: '', uomCode: 'TON', requiredQty: '10', orderedQty: '0', openQty: '10', status: 'PENDING' }],
    requirementId: '7', result: 'MATCHED', ...overrides,
  }
}

describe('purchase order import validation', () => {
  it('accepts an empty unit price as a zero-price draft', () => {
    expect(evaluatePurchaseImportRow(row()).result).toBe('MATCHED')
  })

  it('blocks unit mismatch and quantity overflow', () => {
    expect(evaluatePurchaseImportRow(row({ quantityUnit: 'KG' })).result).toBe('UNIT_MISMATCH')
    expect(evaluatePurchaseImportRow(row({ quantity: '11' })).result).toBe('QTY_EXCEEDED')
  })

  it('blocks the same requirement selected twice', () => {
    expect(validatePurchaseImportRows([row(), row({ rowNo: 3 })])).toBe('DUPLICATED')
  })

  it('calculates confirmation totals', () => {
    expect(purchaseImportTotals([row({ unitPrice: '5' })])).toEqual({ quantity: 10, amount: 50 })
  })
})
