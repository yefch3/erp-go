<template>
  <el-popover
    v-model:visible="open"
    placement="bottom-end"
    :width="400"
    trigger="click"
    popper-class="bl-reminder-popover"
    @show="load"
  >
    <template #reference>
      <el-badge :value="unread" :hidden="unread === 0" :max="99">
        <el-button class="bell" text circle :aria-label="t('blReminders.title')">
          <el-icon :size="20"><Ship /></el-icon>
        </el-button>
      </el-badge>
    </template>

    <div class="heading">
      <strong>{{ t('blReminders.title') }}</strong>
      <el-button v-if="unread > 0" link type="primary" size="small" @click="markAllRead">
        {{ t('blReminders.markAllRead') }}
      </el-button>
    </div>

    <el-scrollbar max-height="380px">
      <el-empty
        v-if="!loading && items.length === 0"
        :description="t('blReminders.empty')"
        :image-size="64"
      />
      <div
        v-for="item in items"
        :key="item.id"
        class="notice"
        :class="[{ unread: item.unread }, toneOf(item.periodNo)]"
        @click="openContract(item)"
      >
        <div class="notice-title">
          <span class="dot" :class="toneOf(item.periodNo)" />
          {{ item.title }}
        </div>
        <div class="notice-body">{{ item.content }}</div>
      </div>
    </el-scrollbar>
  </el-popover>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { Ship } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { get, post, quietErrors } from '../api'

interface Reminder {
  id: string
  scheduleId: string
  scheduleNo: string
  periodNo: number
  title: string
  content: string
  detailUrl: string
  unread: boolean
}

const { t } = useI18n()
const router = useRouter()
const open = ref(false)
const loading = ref(false)
const items = ref<Reminder[]>([])
const unread = ref(0)

// 催得越久越红：第一轮橙，催过三轮（约十天）还没签就是红的。
function toneOf(periodNo: number): string {
  return periodNo >= 3 ? 'tone-danger' : 'tone-warn'
}

async function load() {
  loading.value = true
  try {
    const d = await get<{ items: Reminder[]; unreadTotal: string }>(
      '/bl-reminders', { limit: 30 }, quietErrors)
    items.value = d.items ?? []
    unread.value = Number(d.unreadTotal ?? 0)
  } catch {
    /* 铃铛是附属信息，拉不到就安静地什么都不显示 */
  } finally {
    loading.value = false
  }
}

// 只取未读数，不拉列表——铃铛上的数字每分钟刷新一次，列表等点开再说。
async function pollUnread() {
  try {
    const d = await get<{ unreadTotal: string }>(
      '/bl-reminders', { unread: '1', limit: 1 }, quietErrors)
    unread.value = Number(d.unreadTotal ?? 0)
  } catch {
    /* 同上 */
  }
}

async function markAllRead() {
  await post('/bl-reminders/read', { ids: [] }, quietErrors)
  items.value = items.value.map((r) => ({ ...r, unread: false }))
  unread.value = 0
}

function openContract(item: Reminder) {
  open.value = false
  // 点进去要落在能干活的地方：催收清单，而不是一条只读通知。
  router.push(item.detailUrl || '/shipping')
}

let timer: number | undefined
onMounted(() => {
  pollUnread()
  timer = window.setInterval(pollUnread, 60_000)
})
onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<style scoped>
.bell {
  color: var(--el-text-color-regular);
}
.heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  margin-bottom: 6px;
}
.notice {
  padding: 9px 8px;
  border-radius: 7px;
  cursor: pointer;
}
.notice:hover {
  background: var(--el-fill-color-light);
}
.notice.unread {
  background: var(--el-color-primary-light-9);
}
.notice-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
}
.notice-body {
  margin-top: 3px;
  margin-left: 14px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
}
.dot.tone-danger {
  background: var(--el-color-danger);
}
.dot.tone-warn {
  background: var(--el-color-warning);
}
.dot.tone-info {
  background: var(--el-color-info);
}
</style>
