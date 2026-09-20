import { computed, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { useAuthStore } from '../stores/auth'
import { moveTableColumn, normalizeTableColumnOrder } from '../lib/tableColumnOrder'

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
  const dragging = ref('')
  const storageKey = computed(() => `erp:table-columns:v1:${auth.employeeId || 'anonymous'}:${toValue(storageID)}`)

  function load() {
    const columns = toValue(defaults)
    let saved: unknown = []
    try { saved = JSON.parse(localStorage.getItem(storageKey.value) || '[]') } catch { saved = [] }
    order.value = normalizeTableColumnOrder(saved, columns)
  }

  function persist() {
    localStorage.setItem(storageKey.value, JSON.stringify(order.value))
  }

  function start(key: string, event: DragEvent) {
    dragging.value = key
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'move'
      event.dataTransfer.setData('text/plain', key)
    }
  }

  function move(target: string, event?: DragEvent) {
    event?.preventDefault()
    const source = dragging.value || event?.dataTransfer?.getData('text/plain') || ''
    const next = moveTableColumn(order.value, source, target)
    if (next === order.value) return
    order.value = next
    dragging.value = ''
    persist()
  }

  function finish() { dragging.value = '' }
  function reset() { order.value = toValue(defaults).map(column => column.key); persist() }

  const columns = computed(() => {
    const source = toValue(defaults)
    const byKey = new Map(source.map(column => [column.key, column]))
    return normalizeTableColumnOrder(order.value, source).flatMap(key => byKey.get(key) || [])
  })
  const customized = computed(() => order.value.join('|') !== toValue(defaults).map(column => column.key).join('|'))

  watch([storageKey, () => toValue(defaults).map(column => column.key).join('|')], load, { immediate: true })
  return { columns, customized, dragging, start, move, finish, reset }
}
