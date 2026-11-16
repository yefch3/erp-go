import { productTemplateValue, setProductTemplateValue, type Product } from './inquiryWorkspace'

export function rowsBetween<T>(visibleRows: T[], first: T, last: T): T[] {
  const start = visibleRows.indexOf(first)
  const end = visibleRows.indexOf(last)
  if (start < 0 || end < 0) return []
  return visibleRows.slice(Math.min(start, end), Math.max(start, end) + 1)
}

export function fillProductColumn(rows: Product[], fieldKey: string, value: string): number {
  for (const row of rows) setProductTemplateValue(row, fieldKey, value)
  return rows.length
}

export function firstSelectedValue(visibleRows: Product[], selectedRows: Product[], fieldKey: string): string {
  const first = visibleRows.find(row => selectedRows.includes(row))
  return first ? productTemplateValue(first, fieldKey) : ''
}
