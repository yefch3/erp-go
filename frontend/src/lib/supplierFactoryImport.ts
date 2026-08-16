export type ImportKind = 'supplier' | 'factory'

export const importHeaders: Record<ImportKind, string[]> = {
  supplier: ['code', 'name_zh', 'name_en', 'short_name', 'country_code', 'currency', 'payment_term', 'business_types', 'contact_name', 'contact_phone', 'contact_email', 'address', 'remark'],
  factory: ['code', 'supplier_code', 'name_zh', 'name_en', 'short_name', 'country_code', 'timezone', 'state_province', 'city', 'district', 'postal_code', 'address', 'status', 'remark'],
}

function parseLine(line: string): string[] {
  const cells: string[] = []
  let value = ''
  let quoted = false
  for (let i = 0; i < line.length; i += 1) {
    const char = line[i]
    if (char === '"') {
      if (quoted && line[i + 1] === '"') {
        value += '"'
        i += 1
      } else {
        quoted = !quoted
      }
    } else if (char === ',' && !quoted) {
      cells.push(value.trim())
      value = ''
    } else {
      value += char
    }
  }
  cells.push(value.trim())
  return cells
}

// parseImportCSV 只接受系统模板的字段，避免列错位后把资料写进错误字段。
export function parseImportCSV(text: string, kind: ImportKind): Record<string, unknown>[] {
  const lines = text.replace(/^\uFEFF/, '').split(/\r?\n/).filter((line) => line.trim())
  if (lines.length < 2) throw new Error('EMPTY')
  const header = parseLine(lines[0]).map((value) => value.toLowerCase())
  const expected = importHeaders[kind]
  if (header.length !== expected.length || header.some((value, index) => value !== expected[index])) throw new Error('HEADER')
  return lines.slice(1).map((line, index) => {
    const cells = parseLine(line)
    const row: Record<string, unknown> = { rowNumber: index + 2 }
    expected.forEach((key, cellIndex) => {
      const value = cells[cellIndex] || ''
      row[key.replace(/_([a-z])/g, (_, letter: string) => letter.toUpperCase())] = key === 'business_types'
        ? value.split(/[|;]/).map((item) => item.trim()).filter(Boolean)
        : value
    })
    return row
  })
}

export function importTemplate(kind: ImportKind): string {
  return `\uFEFF${importHeaders[kind].join(',')}\r\n`
}
