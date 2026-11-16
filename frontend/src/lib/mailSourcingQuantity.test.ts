import { describe, expect, it } from 'vitest'
import { missingQuantityRows, quantityForIntake } from './mailSourcingQuantity'
import type { ExcelSheet } from './excelJobResult'

describe('mail sourcing quantity', () => {
  it('treats blank and zero as missing while retaining positive quantities', () => {
    expect(['', '  ', '0', '0.0000', ' 0.0 ', '20', '0.5'].map(quantityForIntake))
      .toEqual(['', '', '', '', '', '20', '0.5'])
  })

  it('finds missing quantities by template key even when the column label changes', () => {
    const sheet: ExcelSheet = {
      name: '询价明细', summary: '', columns: ['产品', '计量单位(MT)', '需求吨位'],
      columnKeys: ['product', 'quantity_unit', 'quantity'], totalRows: '4',
      rows: [
        { cells: ['A', 'MT', '40'] }, { cells: ['B', 'MT', ''] },
        { cells: ['C', 'MT', '0'] }, { cells: ['D', 'MT', '0.5'] },
      ],
    }
    expect(missingQuantityRows(sheet)).toEqual([2, 3])
  })

  it('reads the standard column label when a directly opened file has no template keys', () => {
    const sheet: ExcelSheet = {
      name: '询价明细', summary: '', columns: ['产品', '数量'], totalRows: '2',
      rows: [{ cells: ['A', '0'] }, { cells: ['B', '12'] }],
    }
    expect(missingQuantityRows(sheet)).toEqual([1])
  })
})
