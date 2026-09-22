export const historicalLineHeaders = ['产品编码（选填）', '产品名称', '规格', '本次下单数量', '单位', '单价']
export type ImportedHistoricalLine = { productCode: string; productName: string; spec: string; qty: string; uom: string; unitPrice: string }
export function parseHistoricalLines(rows: unknown[][]) {
  if (!rows.length || historicalLineHeaders.some((h, i) => String(rows[0][i] ?? '').trim() !== h)) throw new Error('表头不符合模板，请下载模板后填写。')
  const lines: ImportedHistoricalLine[] = [], errors: string[] = []
  for (let i = 1; i < rows.length; i++) {
    const cells = historicalLineHeaders.map((_, c) => String(rows[i][c] ?? '').trim())
    if (cells.every(c => !c)) continue
    const [productCode, productName, spec, qty, uom, unitPrice] = cells
    const problems: string[] = []
    if (!productName) problems.push('缺少产品名称')
    if (!uom) problems.push('缺少单位')
    if (!/^\d+(\.\d{1,4})?$/.test(qty) || Number(qty) <= 0 || Number(qty) >= 1e14) problems.push('数量须大于零且最多四位小数')
    if (unitPrice && (!/^\d+(\.\d{1,4})?$/.test(unitPrice) || Number(unitPrice) >= 1e14)) problems.push('单价须为非负数且最多四位小数')
    if (productName.length > 200 || spec.length > 300 || uom.length > 32) problems.push('名称、规格或单位过长')
    if (problems.length) errors.push(`第 ${i + 1} 行：${problems.join('；')}`)
    else lines.push({ productCode, productName, spec, qty, uom, unitPrice })
  }
  return { lines, errors }
}
