import type { ExcelSheet } from './excelJobResult'

// A missing quantity may appear as 0 in an extracted workbook. During intake it
// means "needs review", never a request to buy zero units.
export function quantityForIntake(value: string): string {
  const quantity = value.trim()
  return /^\+?0+(?:\.0+)?$/.test(quantity) ? '' : quantity
}

export function missingQuantityRows(sheet: ExcelSheet): number[] {
  const keyedIndex = sheet.columnKeys?.indexOf('quantity') ?? -1
  const index = keyedIndex >= 0 ? keyedIndex : sheet.columns.findIndex((column) =>
    ['数量', '需求数量', '询价数量', 'Quantity', 'Qty'].includes(column.trim()),
  )
  return sheet.rows.flatMap((row, rowIndex) =>
    !quantityForIntake(index < 0 ? '' : row.cells[index] ?? '') ? [rowIndex + 1] : [],
  )
}
