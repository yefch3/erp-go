export interface ProductDemandLine {
  id?: string | number
  lineNo?: string | number
  productId?: string | number
  extracted?: Record<string, unknown>
}

export interface ProductDemandGroup<T extends ProductDemandLine = ProductDemandLine> {
  key: string
  lines: T[]
}

function normalized(value: unknown): string {
  return String(value ?? '').trim().toLocaleLowerCase()
}

// Once a line has been linked to product master data, that stable identity is
// the grouping boundary. Unlinked imports fall back to their product text so
// repeated rows from one spreadsheet still fold together.
export function productDemandKey(line: ProductDemandLine): string {
  const productID = String(line.productId ?? '').trim()
  if (productID && productID !== '0') return `product:${productID}`
  const product = normalized(line.extracted?.product)
  return product ? `name:${product}` : `line:${line.id ?? line.lineNo ?? ''}`
}

export function groupProductDemands<T extends ProductDemandLine>(lines: T[]): ProductDemandGroup<T>[] {
  const groups = new Map<string, ProductDemandGroup<T>>()
  for (const line of lines) {
    const key = productDemandKey(line)
    const group = groups.get(key)
    if (group) group.lines.push(line)
    else groups.set(key, { key, lines: [line] })
  }
  return [...groups.values()]
}

function exactDecimalSum(values: string[]): string | null {
  const parsed = values.map(value => value.trim().match(/^(-?)(\d+)(?:\.(\d+))?$/))
  if (parsed.some(value => !value)) return null
  const scale = Math.max(0, ...parsed.map(value => value?.[3]?.length ?? 0))
  let total = 0n
  for (const value of parsed) {
    if (!value) return null
    const fraction = (value[3] ?? '').padEnd(scale, '0')
    const integer = BigInt(`${value[1]}${value[2]}${fraction}`)
    total += integer
  }
  const negative = total < 0n
  const digits = (negative ? -total : total).toString().padStart(scale + 1, '0')
  const rendered = scale ? `${digits.slice(0, -scale)}.${digits.slice(-scale)}` : digits
  return negative ? `-${rendered}` : rendered
}

export function demandQuantitySummary(lines: ProductDemandLine[], fallback = '—'): string {
  const byUnit = new Map<string, string[]>()
  for (const line of lines) {
    const quantity = String(line.extracted?.quantity ?? '').trim()
    if (!quantity) continue
    const unit = String(line.extracted?.quantityUnit ?? line.extracted?.quantity_unit ?? '').trim().toUpperCase()
    byUnit.set(unit, [...(byUnit.get(unit) ?? []), quantity])
  }
  if (!byUnit.size) return fallback
  return [...byUnit].map(([unit, values]) => {
    const total = exactDecimalSum(values)
    const quantity = total ?? values.join(' + ')
    return `${quantity}${unit ? ` ${unit}` : ''}`
  }).join(' + ')
}
