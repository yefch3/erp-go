<template>
  <el-popover
    v-model:visible="open"
    placement="bottom-end"
    :width="430"
    trigger="click"
    popper-class="shipping-reminder-popover"
    @show="load"
  >
    <template #reference>
      <el-badge :value="unreadCount" :hidden="unreadCount === 0" :max="99">
        <el-button class="bell" text circle :aria-label="t('shippingReminders.title')">
          <el-icon :size="20"><Bell /></el-icon>
        </el-button>
      </el-badge>
    </template>

    <div class="heading">
      <strong>{{ t('shippingReminders.title') }}</strong>
      <div class="heading-actions">
        <el-button link type="danger" size="small" :loading="cleaning" @click="cleanupExpiredReminders">
          {{ t('shippingReminders.cleanup') }}
        </el-button>
        <el-switch
          v-model="popupEnabled"
          :active-text="t('shippingReminders.popupOn')"
          :inactive-text="t('shippingReminders.popupOff')"
          active-color="#22c55e"
          style="--el-switch-on-color: #22c55e"
          @change="setPopupEnabled"
        />
        <span>{{ t('shippingReminders.unread', { count: unreadCount }) }}</span>
      </div>
    </div>
    <div v-if="loadFailed" class="load-error">
      <span>{{ t('shippingReminders.loadFailed') }}</span>
      <el-button link type="primary" size="small" :loading="loading" @click="load(false)">{{ t('shippingReminders.reload') }}</el-button>
    </div>
    <el-scrollbar max-height="420px">
      <el-empty v-if="!loading && reminders.length === 0" :description="t('shippingReminders.empty')" :image-size="72" />
      <div
        v-for="item in reminders"
        :key="item.id"
        class="notice"
        :class="{ unread: !item.readAt }"
      >
        <button class="notice-open" type="button" @click="openReminder(item)">
          <span v-if="!item.readAt" class="dot" />
          <span class="notice-copy">
            <strong>{{ reminderTitle(item) }}</strong>
            <span>{{ reminderContent(item) }}</span>
            <small>{{ formatTime(item.sentAt) }}</small>
          </span>
        </button>
        <div class="notice-toggle" @click.stop>
          <span>{{ t('shippingReminders.itemPopup') }}</span>
          <el-switch
            :model-value="isItemPopupEnabled(item)"
            active-color="#22c55e"
            style="--el-switch-on-color: #22c55e"
            @update:model-value="(value) => setItemPopupEnabled(item, value)"
          />
        </div>
      </div>
    </el-scrollbar>
  </el-popover>
</template>

<script setup lang="ts">
import { h, onMounted, onUnmounted, ref, watch } from 'vue'
import { Bell } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox, ElNotification, ElSwitch } from 'element-plus'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { del, get, post, quietErrors } from '../api'
import { onLive } from '../live'
import type { ShippingArrivalReminder } from '../shipping'
import { announceReminderChanged, onReminderChanged } from '../lib/homeReminders'

const router = useRouter()
const { t, locale } = useI18n()
const open = ref(false)
const loading = ref(false)
const loadFailed = ref(false)
const cleaning = ref(false)
const reminders = ref<ShippingArrivalReminder[]>([])
const unreadCount = ref(0)
const activePopups = new Map<string, { close: () => void }>()
const pendingPopups = new Set<string>()
const popupPreferenceKey = 'shipping-arrival-popup-enabled'
const itemPopupPreferencePrefix = 'shipping-arrival-item-popup:'
const popupEnabled = ref(localStorage.getItem(popupPreferenceKey) !== '0')
const disabledItemPopups = ref<Record<string, boolean>>({})

// 已读状态只控制铃铛上的未读数量；是否自动弹窗由每条提醒自己的弹窗开关决定。
// 只要“本条弹窗”仍然开启，每次进入船期模块都应再次显示该提醒。
async function load(autoPopup = false) {
  loading.value = true
  try {
    const data = await get<{ reminders: ShippingArrivalReminder[]; unreadCount: string }>('/shipping/reminders', undefined, quietErrors)
    reminders.value = data.reminders ?? []
    disabledItemPopups.value = Object.fromEntries(
      reminders.value.map((item) => [String(item.id), localStorage.getItem(`${itemPopupPreferencePrefix}${item.id}`) === '0']),
    )
    unreadCount.value = Number(data.unreadCount ?? 0)
    loadFailed.value = false
    if (autoPopup && popupEnabled.value) showAutomaticPopups(reminders.value)
  } catch {
    // 顶栏会在进入系统和每分钟轮询时后台读取提醒。服务刚启动或短暂繁忙时，
    // 保留上一次成功结果并等待下一次重试；用户打开铃铛后能看到重试入口。
    loadFailed.value = true
  } finally {
    loading.value = false
  }
}

function showAutomaticPopups(items: ShippingArrivalReminder[]) {
  const unread = items
    .filter(
      (item) =>
        isItemPopupEnabled(item) &&
        !activePopups.has(String(item.id)) &&
        !pendingPopups.has(String(item.id)),
    )
    .slice(0, 3)

  unread.forEach((item, index) => {
    const popupKey = String(item.id)
    pendingPopups.add(popupKey)
    window.setTimeout(() => {
      pendingPopups.delete(popupKey)
      if (!popupEnabled.value || !isItemPopupEnabled(item) || activePopups.has(popupKey)) return
      const notice = ElNotification({
        title: reminderTitle(item),
        message: h('div', [
          h('p', { style: 'margin: 0 0 12px; line-height: 1.6;' }, reminderContent(item)),
          h('div', { style: 'display: flex; gap: 8px;' }, [
            h(
              'button',
              {
                type: 'button',
                style: 'padding: 6px 14px; border: 1px solid #409eff; border-radius: 4px; background: #409eff; color: #fff; cursor: pointer;',
                onClick: (event: MouseEvent) => {
                  event.stopPropagation()
                  notice.close()
                  void openReminder(item)
                },
              },
              t('shippingReminders.details'),
            ),
            h('div', { style: 'display: flex; align-items: center; gap: 6px;', onClick: (event: MouseEvent) => event.stopPropagation() }, [
              h('span', { style: 'color: #606266;' }, t('shippingReminders.itemPopup')),
              h(ElSwitch, {
                modelValue: isItemPopupEnabled(item),
                activeColor: '#22c55e',
                style: '--el-switch-on-color: #22c55e',
                'onUpdate:modelValue': (enabled: string | number | boolean) => setItemPopupEnabled(item, enabled),
              }),
            ]),
          ]),
        ]),
        type: 'warning',
        position: 'top-right',
        offset: 72,
        duration: 0,
        showClose: true,
        onClose: () => activePopups.delete(popupKey),
      })
      activePopups.set(popupKey, notice)
    }, index * 180)
  })
}

function reminderTitle(item: ShippingArrivalReminder) {
  const title = item.title?.trim()
  let match = title?.match(/^船期预计\s*(\d+)\s*天后(到港|开船)$/)
  if (match) return t(match[2] === '开船' ? 'shippingReminders.departureInDays' : 'shippingReminders.arrivalInDays', { days: match[1] })
  match = title?.match(/^船期预计今天(到港|开船)$/)
  if (match) return t(match[1] === '开船' ? 'shippingReminders.departureToday' : 'shippingReminders.arrivalToday')
  match = title?.match(/^船期预计已逾期\s*(\d+)\s*天$/)
  if (match) return t('shippingReminders.overdueDays', { days: match[1] })
  if (title === '船期即将到港') return t('shippingReminders.arrivalSoon')
  if (title === '船期即将开船') return t('shippingReminders.departureSoon')
  return title || t('shippingReminders.title')
}

function reminderContent(item: ShippingArrivalReminder) {
  const labels: Record<string, string> = {
    船期编号: 'scheduleNo', 合同编号: 'contractNo', 客户: 'customer', '船名/航次': 'vesselVoyage',
    港口: 'port', 目的港: 'destinationPort', '最新 ETA': 'latestEta', '预计到港（ETA）': 'expectedArrival', '预计离港（ETD）': 'expectedDeparture',
  }
  return (item.content || '').split('；').map((part) => {
    const separator = part.indexOf('：')
    if (separator < 0) return part
    const label = part.slice(0, separator).trim()
    const key = labels[label]
    return key ? `${t(`shippingReminders.fields.${key}`)}: ${part.slice(separator + 1).trim()}` : part
  }).join('; ')
}

function isItemPopupEnabled(item: ShippingArrivalReminder) {
  return !disabledItemPopups.value[String(item.id)]
}

// 每条提醒拥有独立开关；关闭一条不会影响其他提醒或总开关。
function setItemPopupEnabled(item: ShippingArrivalReminder, value: string | number | boolean) {
  const popupKey = String(item.id)
  const enabled = value === true || value === 1 || value === '1' || value === 'true'
  disabledItemPopups.value = { ...disabledItemPopups.value, [popupKey]: !enabled }
  localStorage.setItem(`${itemPopupPreferencePrefix}${popupKey}`, enabled ? '1' : '0')
  if (!enabled) {
    pendingPopups.delete(popupKey)
    activePopups.get(popupKey)?.close()
    return
  }
  if (popupEnabled.value && router.currentRoute.value.path.startsWith('/shipping')) showAutomaticPopups([item])
}

// 关闭开关后记住用户选择并立即收起所有运输提醒弹窗；重新开启时立即拉取一次。
function setPopupEnabled(value: string | number | boolean) {
  const enabled = Boolean(value)
  popupEnabled.value = enabled
  localStorage.setItem(popupPreferenceKey, enabled ? '1' : '0')
  if (!enabled) {
    Array.from(activePopups.values()).forEach((popup) => popup.close())
    return
  }
  if (router.currentRoute.value.path.startsWith('/shipping')) void load(true)
}

// 清理只删除当前员工已经过期的通知，不会删除对应船期。
async function cleanupExpiredReminders() {
  await ElMessageBox.confirm(
    t('shippingReminders.cleanupMessage'),
    t('shippingReminders.cleanupTitle'),
    { confirmButtonText: t('shippingReminders.confirmCleanup'), cancelButtonText: t('shippingReminders.cancel'), type: 'warning' },
  )
  cleaning.value = true
  try {
    const data = await del<{ deletedCount: string }>('/shipping/reminders/expired')
    await load(false)
    ElMessage.success(t('shippingReminders.cleaned', { count: Number(data.deletedCount ?? 0) }))
  } finally {
    cleaning.value = false
  }
}

async function openReminder(item: ShippingArrivalReminder) {
  activePopups.get(String(item.id))?.close()
  if (!item.readAt) {
    const data = await post<{ reminder: ShippingArrivalReminder }>(`/shipping/reminders/${item.id}/read`)
    item.readAt = data.reminder.readAt
    unreadCount.value = Math.max(0, unreadCount.value - 1)
    announceReminderChanged()
  }
  open.value = false
  await router.push(item.detailUrl || `/shipping/${item.scheduleId}`)
}

function formatTime(value: string) {
  return value ? new Date(value).toLocaleString(locale.value) : '—'
}

let unsubscribe: (() => void) | undefined
let stopReminderListening: (() => void) | undefined
let timer: number | undefined
onMounted(() => {
  void load(router.currentRoute.value.path.startsWith('/shipping'))
  unsubscribe = onLive((event) => {
    if (event.type === 'shipping.arrival_reminder') {
      void load(router.currentRoute.value.path.startsWith('/shipping'))
    }
  })
  stopReminderListening = onReminderChanged(() => { void load(false) })
  timer = window.setInterval(() => { void load(false) }, 60_000)
})
onUnmounted(() => {
  unsubscribe?.()
  stopReminderListening?.()
  if (timer) window.clearInterval(timer)
  pendingPopups.clear()
  Array.from(activePopups.values()).forEach((popup) => popup.close())
  ElNotification.closeAll()
  activePopups.clear()
})

watch(
  () => router.currentRoute.value.path,
  (path, previousPath) => {
    if (path.startsWith('/shipping') && !previousPath.startsWith('/shipping')) void load(true)
  },
)

defineExpose({ refreshAndPopup: () => load(true) })
</script>

<style scoped>
.bell { color: #475569; }
.heading { display: flex; align-items: center; justify-content: space-between; padding: 4px 6px 10px; border-bottom: 1px solid #e5e7eb; }
.heading span { color: #64748b; font-size: 12px; }
.heading-actions { display: flex; align-items: center; gap: 12px; }
.load-error { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin: 10px 6px 4px; padding: 8px 10px; border-radius: 6px; color: #92400e; background: #fffbeb; font-size: 12px; }
.notice { position: relative; display: flex; align-items: center; width: 100%; border-bottom: 1px solid #f1f5f9; background: #fff; }
.notice:hover { background: #f8fafc; }
.notice.unread { background: #eff6ff; }
.notice-open { display: flex; flex: 1; min-width: 0; padding: 12px 10px; border: 0; background: transparent; text-align: left; cursor: pointer; }
.notice-toggle { display: flex; flex: 0 0 auto; flex-direction: column; align-items: center; gap: 4px; padding: 10px; }
.notice-toggle span { color: #64748b; font-size: 12px; }
.dot { flex: 0 0 7px; width: 7px; height: 7px; margin: 7px 9px 0 0; border-radius: 50%; background: #409eff; }
.notice-copy { display: flex; min-width: 0; flex-direction: column; gap: 5px; color: #475569; font-size: 13px; line-height: 1.45; }
.notice-copy strong { color: #0f172a; font-size: 14px; }
.notice-copy small { color: #94a3b8; }
:global(.shipping-reminder-popover) { z-index: 5000 !important; }
</style>
