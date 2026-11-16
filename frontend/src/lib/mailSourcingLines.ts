import type { ExcelSheet } from './excelJobResult'
import { quantityForIntake } from './mailSourcingQuantity'

const fieldByColumn: Record<string, string> = {
  '产品': 'product', '材质/标准': 'material_standard', '牌号/等级': 'grade',
  '厚度': 'thickness', '宽度': 'width', '长度/形式': 'length_or_form',
  '表面要求': 'surface_requirement', '涂层/镀层': 'coating', '公差': 'tolerance',
  '卷重': 'coil_weight', '卷内径': 'coil_id', '包装': 'packaging', '交期': 'delivery',
  '付款条件': 'payment_terms', '贸易术语': 'incoterm', '港口': 'port',
  '单位': 'quantity_unit', '计量单位': 'quantity_unit', '备注': 'remarks',
  '数量': 'quantity', '需求数量': 'quantity', '询价数量': 'quantity',
  'quantity': 'quantity', 'qty': 'quantity',
  'product': 'product', 'product description': 'product', 'description': 'product',
  '规格': 'specification', '重量': 'weight', '体积': 'volume', '包装数量': 'package_quantity',
  '单价': 'unit_price', '总价': 'total_price', 'unit price': 'unit_price', 'total price': 'total_price',
}
const lineFields = new Set([
  'raw_text', 'product', 'material_standard', 'grade', 'thickness', 'width',
  'length_or_form', 'surface_requirement', 'coating', 'tolerance', 'coil_weight',
  'coil_id', 'packaging', 'delivery', 'payment_terms', 'incoterm', 'port',
  'quantity_unit', 'remarks', 'quantity',
])

// Preserve full cell text. Workspace-only fields also need to survive the
// older sourcing-line API, whose scalar fields cover only the original template.
export function sourcingLinesFromSheet(sheet: ExcelSheet) {
  return sheet.rows.map((row) => {
    const line: Record<string, unknown> = {}
    const customFields: Record<string, string> = {}
    sheet.columns.forEach((column, index) => {
      const header = column.trim().replace(/[（(].*?[）)]/g, '').trim()
      const key = sheet.columnKeys?.[index]?.trim()
        || fieldByColumn[header] || fieldByColumn[header.toLowerCase()]
      const cell = row.cells[index] ?? ''
      if (key === 'unit_price' || key === 'total_price') return
      if (key && lineFields.has(key)) {
        line[key] = key === 'quantity' ? quantityForIntake(cell) : cell
      } else if (cell.trim()) {
        // Unrecognized populated columns must not silently disappear.
        customFields[key || `custom.source_column_${index + 1}`] = cell
      }
    })
    if (Object.keys(customFields).length) line.customFields = customFields
    return line
  })
}
