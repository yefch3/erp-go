export type NavigationPreferences = {
  modules: string[]
  children: Record<string, string[]>
}

export function normalizeOrder(saved: unknown, defaults: string[]): string[] {
  const values = Array.isArray(saved) ? saved.filter((value): value is string => typeof value === 'string') : []
  const known = new Set(defaults)
  const result: string[] = []
  for (const value of values) {
    if (known.has(value) && !result.includes(value)) result.push(value)
  }
  for (const value of defaults) if (!result.includes(value)) result.push(value)
  return result
}

export function sortByOrder<T extends { key: string }>(items: T[], order: string[]): T[] {
  const positions = new Map(order.map((key, index) => [key, index]))
  return [...items].sort((a, b) => (positions.get(a.key) ?? order.length) - (positions.get(b.key) ?? order.length))
}

export function moveItem(order: string[], from: number, to: number): string[] {
  if (from < 0 || from >= order.length || to < 0 || to >= order.length || from === to) return [...order]
  const next = [...order]
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}

export function mergeVisibleOrder(all: string[], visible: string[]): string[] {
  const visibleSet = new Set(visible)
  let cursor = 0
  return all.map((key) => visibleSet.has(key) ? visible[cursor++] : key)
}

export function readNavigationPreferences(key: string, moduleDefaults: string[], childDefaults: Record<string, string[]>): NavigationPreferences {
  let saved: Partial<NavigationPreferences> = {}
  try {
    const parsed = JSON.parse(localStorage.getItem(key) ?? '{}')
    if (parsed && typeof parsed === 'object') saved = parsed
  } catch { /* damaged local preference falls back to defaults */ }
  const children: Record<string, string[]> = {}
  for (const [module, defaults] of Object.entries(childDefaults)) {
    children[module] = normalizeOrder(saved.children?.[module], defaults)
  }
  return { modules: normalizeOrder(saved.modules, moduleDefaults), children }
}

export function writeNavigationPreferences(key: string, preferences: NavigationPreferences) {
  localStorage.setItem(key, JSON.stringify(preferences))
}
