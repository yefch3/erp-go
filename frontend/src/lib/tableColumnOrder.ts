export interface OrderedColumn {
  key: string
}

export interface TableColumnPreference {
  version: 2
  userId: string
  pageKey: string
  columnOrder: string[]
  updatedAt: string
}

export function tableColumnPreferenceStorageKey(userId: string, pageKey: string, version: 1 | 2 = 2) {
  return `erp:table-columns:v${version}:${userId}:${pageKey}`
}

export function preferenceColumnOrder(saved: unknown) {
  if (Array.isArray(saved)) return saved
  if (!saved || typeof saved !== 'object') return []
  const candidate = saved as Partial<TableColumnPreference>
  return Array.isArray(candidate.columnOrder) ? candidate.columnOrder : []
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

export function moveTableColumnBy(order: string[], key: string, direction: -1 | 1) {
  const index = order.indexOf(key)
  const target = index + direction
  if (index < 0 || target < 0 || target >= order.length) return order
  const next = [...order]
  ;[next[index], next[target]] = [next[target], next[index]]
  return next
}
