import { parseTableFile, type DirectWorkbook } from './attachmentExcel'

export async function parseCustomerImportWorkbook(name: string, data: ArrayBuffer): Promise<DirectWorkbook> {
  if (data.byteLength > 20 * 1024 * 1024) throw new Error('导入文件不能超过 20 MB')
  if (!name.toLowerCase().endsWith('.xls')) return parseTableFile(name, data, { maxRows: 5000 })
  const XLSX = await import('xlsx')
  const workbook = XLSX.read(data, { type: 'array', cellText: true, sheetRows: 5002 })
  if (workbook.SheetNames.length > 20) throw new Error('最多支持 20 个工作表')
  return { sheets: workbook.SheetNames.map(name => {
    const sheet = workbook.Sheets[name]
    const range = XLSX.utils.decode_range(sheet['!fullref'] || sheet['!ref'] || 'A1')
    if (range.e.r > 5000) throw new Error(`工作表“${name}”超过 5000 行，请拆分后再导入`)
    if (range.e.c >= 80) throw new Error('最多支持 80 列')
    const values = XLSX.utils.sheet_to_json<string[]>(sheet, { header: 1, raw: false, defval: '', blankrows: true })
    const columns = (values.shift() ?? []).map(String)
    while (columns.length && !columns[columns.length - 1].trim() && values.every(row => !String(row[columns.length - 1] ?? '').trim())) columns.pop()
    const rows = values.map(row => columns.map((_, index) => String(row[index] ?? '')))
    while (rows.length && rows[rows.length - 1].every(value => !value.trim())) rows.pop()
    return { name, columns, rows, totalRows: rows.length }
  }) }
}
