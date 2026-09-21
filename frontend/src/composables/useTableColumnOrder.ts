import { computed, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { useAuthStore } from '../stores/auth'
import { moveTableColumnBy, normalizeTableColumnOrder, preferenceColumnOrder, tableColumnPreferenceStorageKey, type TableColumnPreference } from '../lib/tableColumnOrder'

export interface TableColumnDefinition {
  key: string
  label: string
  minWidth?: number
  width?: number
  align?: 'left' | 'center' | 'right'
  className?: string
  showOverflowTooltip?: boolean
}

export function useTableColumnOrder(storageID: MaybeRefOrGetter<string>, defaults: MaybeRefOrGetter<TableColumnDefinition[]>) {
  const auth = useAuthStore()
  const order = ref<string[]>([])
  const userId = computed(() => String(auth.employeeId || 'anonymous'))
  const pageKey = computed(() => toValue(storageID))
  const storageKey = computed(() => tableColumnPreferenceStorageKey(userId.value, pageKey.value))
  const legacyStorageKey = computed(() => tableColumnPreferenceStorageKey(userId.value, pageKey.value, 1))

  function load() {
    const columns = toValue(defaults)
    let saved: unknown = []
    try {
      const current = localStorage.getItem(storageKey.value)
      const legacy = localStorage.getItem(legacyStorageKey.value)
      saved = JSON.parse(current || legacy || '[]')
    } catch { saved = [] }
    order.value = normalizeTableColumnOrder(preferenceColumnOrder(saved), columns)
  }

  function persist() {
    const preference: TableColumnPreference = { version: 2, userId: userId.value, pageKey: pageKey.value, columnOrder: order.value, updatedAt: new Date().toISOString() }
    try { localStorage.setItem(storageKey.value, JSON.stringify(preference)) } catch { /* defaults remain usable when browser storage is unavailable */ }
  }

  function moveBy(key: string, direction: -1 | 1) {
    const next = moveTableColumnBy(order.value, key, direction)
    if (next === order.value) return
    order.value = next
    persist()
  }

  function reset() {
    order.value = toValue(defaults).map(column => column.key)
    try { localStorage.removeItem(storageKey.value); localStorage.removeItem(legacyStorageKey.value) } catch { /* in-memory defaults still apply */ }
  }

  const columns = computed(() => {
    const source = toValue(defaults)
    const byKey = new Map(source.map(column => [column.key, column]))
    return normalizeTableColumnOrder(order.value, source).flatMap(key => byKey.get(key) || [])
  })
  const customized = computed(() => order.value.join('|') !== toValue(defaults).map(column => column.key).join('|'))
  const canMoveLeft = (key: string) => order.value.indexOf(key) > 0
  const canMoveRight = (key: string) => { const index = order.value.indexOf(key); return index >= 0 && index < order.value.length - 1 }

  watch([storageKey, () => toValue(defaults).map(column => column.key).join('|')], load, { immediate: true })
  return { columns, customized, moveBy, canMoveLeft, canMoveRight, reset, storageKey }
}
