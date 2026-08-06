<template>
  <el-popover v-model:visible="open" placement="bottom-end" :width="430" trigger="click" @show="load">
    <template #reference>
      <el-badge :value="unreadCount" :hidden="unreadCount === 0" :max="99">
        <el-button class="bell" text circle aria-label="到港提醒">
          <el-icon :size="20"><Bell /></el-icon>
        </el-button>
      </el-badge>
    </template>

    <div class="heading">
      <strong>到港提醒</strong>
      <span>{{ unreadCount }} 条未读</span>
    </div>
    <el-scrollbar max-height="420px">
      <el-empty v-if="!loading && reminders.length === 0" description="暂无到港提醒" :image-size="72" />
      <button
        v-for="item in reminders"
        :key="item.id"
        class="notice"
        :class="{ unread: !item.readAt }"
        type="button"
        @click="openReminder(item)"
      >
        <span v-if="!item.readAt" class="dot" />
        <span class="notice-copy">
          <strong>{{ item.title }}</strong>
          <span>{{ item.content }}</span>
          <small>{{ formatTime(item.sentAt) }}</small>
        </span>
      </button>
    </el-scrollbar>
  </el-popover>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { Bell } from '@element-plus/icons-vue'
import { useRouter } from 'vue-router'
import { get, post } from '../api'
import { onLive } from '../live'
import type { ShippingArrivalReminder } from '../shipping'

const router = useRouter()
const open = ref(false)
const loading = ref(false)
const reminders = ref<ShippingArrivalReminder[]>([])
const unreadCount = ref(0)

async function load() {
  loading.value = true
  try {
    const data = await get<{ reminders: ShippingArrivalReminder[]; unreadCount: string }>('/shipping/reminders')
    reminders.value = data.reminders ?? []
    unreadCount.value = Number(data.unreadCount ?? 0)
  } finally {
    loading.value = false
  }
}

async function openReminder(item: ShippingArrivalReminder) {
  if (!item.readAt) {
    const data = await post<{ reminder: ShippingArrivalReminder }>(`/shipping/reminders/${item.id}/read`)
    item.readAt = data.reminder.readAt
    unreadCount.value = Math.max(0, unreadCount.value - 1)
  }
  open.value = false
  await router.push(item.detailUrl || `/shipping/${item.scheduleId}`)
}

function formatTime(value: string) {
  return value ? new Date(value).toLocaleString() : '—'
}

let unsubscribe: (() => void) | undefined
onMounted(() => {
  void load()
  unsubscribe = onLive((event) => {
    if (event.type === 'shipping.arrival_reminder') void load()
  })
})
onUnmounted(() => unsubscribe?.())
</script>

<style scoped>
.bell { color: #475569; }
.heading { display: flex; align-items: center; justify-content: space-between; padding: 4px 6px 10px; border-bottom: 1px solid #e5e7eb; }
.heading span { color: #64748b; font-size: 12px; }
.notice { position: relative; display: flex; width: 100%; padding: 12px 10px; border: 0; border-bottom: 1px solid #f1f5f9; background: #fff; text-align: left; cursor: pointer; }
.notice:hover { background: #f8fafc; }
.notice.unread { background: #eff6ff; }
.dot { flex: 0 0 7px; width: 7px; height: 7px; margin: 7px 9px 0 0; border-radius: 50%; background: #409eff; }
.notice-copy { display: flex; min-width: 0; flex-direction: column; gap: 5px; color: #475569; font-size: 13px; line-height: 1.45; }
.notice-copy strong { color: #0f172a; font-size: 14px; }
.notice-copy small { color: #94a3b8; }
</style>
