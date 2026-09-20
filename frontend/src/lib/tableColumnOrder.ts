export interface OrderedColumn {
  key: string
}

export function normalizeTableColumnOrder(saved: unknown, defaults: OrderedColumn[]) {
  const available = new Set(defaults.map(column => column.key))
  const restored = Array.isArray(saved)
    ? saved.map(String).filter((key, index, values) => available.has(key) && values.indexOf(key) === index)
    : []
  return [...restored, ...defaults.map(column => column.key).filter(key => !restored.includes(key))]
}

export function moveTableColumn(order: string[], source: string, target: string) {
  if (!source || source === target) return order
  const next = [...order]
  const from = next.indexOf(source)
  if (from < 0 || !next.includes(target)) return order
  next.splice(from, 1)
  next.splice(next.indexOf(target), 0, source)
  return next
}
