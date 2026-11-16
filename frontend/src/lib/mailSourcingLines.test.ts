import { describe, expect, it } from 'vitest'
import { sourcingLinesFromSheet } from './mailSourcingLines'
import type { ExcelSheet } from './excelJobResult'

describe('Excel inquiry transfer', () => {
  const description = 'PREPAINTED GALVALUME COILS (PPGL) ASTM A792 CS-B / AZ25 WITH GUARD FILM (POLYPROPYLENE), AZM 25 (25 gr/m2 only positive tolerance), CHROMED, DRY, UNOILED, EYE TO SKY; TOP COAT: 10+5 MICRONS PAINT (paint+primer), BACK COAT 7 MICRONS; DIMENSIONAL TOLERANCES IS JIS G3302 Grade SGCH; WEIGHT COIL: 4-6 MT; BackSide RAL 9003/or mill gray (TDC F254 v4); BL ALZN PP ROJO A792 0.40 X 1220MM, Red (RAL 3020), SKU 102646'
  it('retains the full product description and every row from the screenshot format', () => {
    const sheet: ExcelSheet = {
      name: '询价明细', summary: '客户询价包含10项产品，明细总量3580 MT',
      columns: ['产品', '计量单位(MT)', '数量', '单价', '总价'],
      columnKeys: ['product', 'quantity_unit', 'quantity', 'unit_price', 'total_price'],
      rows: Array.from({ length: 10 }, () => ({ cells: [description, 'MT', '470', '', ''] })), totalRows: '10',
    }
    const lines = sourcingLinesFromSheet(sheet)
    expect(lines).toHaveLength(10)
    expect(lines.every((line) => line.product === description && line.quantity_unit === 'MT')).toBe(true)
    expect(lines[0]).toEqual({ product: description, quantity_unit: 'MT', quantity: '470' })
  })
  it('falls back from empty keys and keeps populated supplementary columns', () => {
    expect(sourcingLinesFromSheet({
      name: '询价明细', summary: '', totalRows: '1',
      columns: ['产品', '计量单位(MT)', '数量', '规格', '重量', '客户要求'],
      columnKeys: ['', '', '', 'specification', 'weight', ''],
      rows: [{ cells: [description, 'MT', '0', '0.40 X 1220MM', '470', '保留原始包装'] }],
    })[0]).toEqual({
      product: description, quantity_unit: 'MT', quantity: '',
      customFields: { specification: '0.40 X 1220MM', weight: '470', 'custom.source_column_6': '保留原始包装' },
    })
  })
})
