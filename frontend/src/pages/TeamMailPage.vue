<template>
  <MailboxGate v-if="locked === true" @unlocked="onUnlocked" />
  <div v-else-if="locked === false">
    <div class="page-head">
      <h2>{{ t('teamMail.title') }}</h2>
      <span class="head-note">{{ t('teamMail.subtitle') }}</span>
    </div>

    <!-- Pick a person first. That order is the point: reading a colleague's
         correspondence is a deliberate act, not a wider default on your own
         mailbox. Whoever appears here is decided by the notification data
         scope, so it changes with the org chart rather than a second list. -->
    <el-card v-if="!chosen" shadow="never">
      <el-table :data="senders" v-loading="loading" class="clickable" @row-click="choose">
        <el-table-column :label="t('teamMail.colleague')" min-width="200">
          <template #default="{ row }">
            <div class="strong">{{ row.name }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('teamMail.volume')" width="160">
          <template #default="{ row }">
            <span class="num">{{ t('teamMail.messages', { n: row.messageCount }) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('teamMail.stuck')" width="160">
          <template #default="{ row }">
            <span v-if="row.failedCount > 0" class="bad">
              {{ t('emails.cFailed', { n: row.failedCount }) }}
            </span>
            <span v-else class="sub">—</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('teamMail.lastSent')" width="160">
          <template #default="{ row }">
            <span class="sub">{{ shortTime(row.lastSentAt) }}</span>
          </template>
        </el-table-column>
      </el-table>
      <el-empty
        v-if="!loading && senders.length === 0"
        :description="t('teamMail.nobody')"
      />
    </el-card>

    <template v-else>
      <el-card shadow="never" class="who">
        <el-button link type="primary" @click="chosen = null">
          ← {{ t('teamMail.backToList') }}
        </el-button>
        <span class="who-name">{{ chosen.name }}</span>
        <!-- Stated plainly. Somebody reading a colleague's mail should know
             they are a spectator here: resending or abandoning is the
             salesperson's call about their own customer relationship. -->
        <el-tag size="small" type="info" effect="plain">{{ t('teamMail.readOnly') }}</el-tag>
        <span class="grow" />
        <el-input
          v-model="keyword"
          :placeholder="t('teamMail.search')"
          clearable
          style="width: 240px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button @click="reload">{{ common('query') }}</el-button>
      </el-card>

      <el-card shadow="never">
        <el-radio-group v-model="view" class="tabs" @change="reload">
          <el-radio-button value="sent">{{ t('emails.folders.sent') }}</el-radio-button>
          <el-radio-button value="attention">{{ t('emails.folders.attention') }}</el-radio-button>
        </el-radio-group>

        <el-table
          v-if="view === 'sent'"
          :data="campaigns"
          v-loading="loading"
          class="clickable"
          @row-click="openCampaign"
        >
          <el-table-column :label="t('emails.to')" min-width="200">
            <template #default="{ row }">
              <div class="strong ellipsis" :title="row.toNames">{{ recipientLine(row) }}</div>
              <div class="sub">{{ shortTime(row.createdAt) }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('emails.subject')" min-width="280">
            <template #default="{ row }">
              <div class="ellipsis" :title="row.subject">{{ row.subject }}</div>
              <div class="sub">{{ row.campaignNo }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('emails.progress')" width="200">
            <template #default="{ row }">
              <div class="counts">
                <span class="ok">{{ t('emails.cDelivered', { n: row.sentCount }) }}</span>
                <span v-if="row.failedCount > 0" class="bad">
                  {{ t('emails.cFailed', { n: row.failedCount }) }}
                </span>
                <span class="sub">{{ t('emails.cTotal', { n: row.totalCount }) }}</span>
              </div>
            </template>
          </el-table-column>
        </el-table>

        <el-table
          v-else
          :data="messages"
          v-loading="loading"
          class="clickable"
          @row-click="openMessage"
        >
          <el-table-column :label="t('emails.recipient')" min-width="190">
            <template #default="{ row }">
              <div class="strong">{{ row.toName || '—' }}</div>
              <div class="sub">{{ row.toEmail }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="common('status')" width="140">
            <template #default="{ row }">
              <el-tag size="small" :type="statusType(row.status)" effect="plain">
                {{ t(`emails.statuses.${row.status}`) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('emails.whyStuck')" min-width="280">
            <template #default="{ row }">
              <div>{{ row.attentionReason || '—' }}</div>
            </template>
          </el-table-column>
          <!-- No actions column. Deliberately: a supervisor watching should
               talk to the salesperson, not silently resend on their behalf. -->
        </el-table>

        <!-- The campaign list still numbers its pages; the message list pages
             by cursor, because it grows at the top and a page number there
             names a different row each time somebody sends. -->
        <el-pagination
          v-if="view === 'sent'"
          v-model:current-page="page"
          :page-size="pageSize"
          :total="total"
          layout="total, prev, pager, next"
          class="pager"
          @current-change="load"
        />
        <div v-else-if="total > 0 || msgStack.length" class="pager keyset">
          <span class="sub">{{ t('emails.totalMails', { n: total }) }}</span>
          <el-button size="small" :disabled="!msgStack.length" @click="prevMessages">
            {{ t('emails.prevPage') }}
          </el-button>
          <el-button size="small" :disabled="!msgCursor" @click="nextMessages">
            {{ t('emails.nextPage') }}
          </el-button>
        </div>
      </el-card>
    </template>

    <el-drawer v-model="readerOpen" size="58%" :with-header="false">
      <div class="drawer-body">
        <MailReader :mail="openMail" />
        <template v-if="view === 'sent' && recipients.length">
          <h4 class="side-title">
            {{ t('emails.recipientsOfSend', { n: recipients.length }) }}
          </h4>
          <el-table :data="recipients" size="small">
            <el-table-column :label="t('emails.recipient')" min-width="180">
              <template #default="{ row }">
                <div>{{ row.toName || '—' }}</div>
                <div class="sub">{{ row.toEmail }}</div>
              </template>
            </el-table-column>
            <el-table-column :label="common('status')" width="130">
              <template #default="{ row }">
                <el-tag size="small" :type="statusType(row.status)" effect="plain">
                  {{ t(`emails.statuses.${row.status}`) }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </template>
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'
import MailReader, { type Mail } from '../components/MailReader.vue'
import MailboxGate from '../components/MailboxGate.vue'
import { useAuthStore } from '../stores/auth'

interface Sender {
  employeeId: string
  name: string
  messageCount: number
  failedCount: number
  lastSentAt: string
}
interface Campaign {
  id: string
  campaignNo: string
  subject: string
  toNames: string
  createdAt: string
  totalCount: number
  sentCount: number
  failedCount: number
}
interface Message {
  id: string
  toEmail: string
  toName: string
  status: string
  attentionReason: string
}

const { t } = useI18n()
const common = (k: string) => t(`common.${k}`)
const auth = useAuthStore()

const senders = ref<Sender[]>([])
const chosen = ref<Sender | null>(null)
const view = ref('sent')
const keyword = ref('')
const page = ref(1)
// The message list is keyset: msgCursor is where the next page starts, and
// msgStack remembers the cursors walked through so 上一页 can replay one.
const msgCursor = ref('')
const msgStack = ref<string[]>([])
const msgAt = ref('')
const pageSize = 20
const total = ref(0)
const loading = ref(false)
const campaigns = ref<Campaign[]>([])
const messages = ref<Message[]>([])
const readerOpen = ref(false)
const openMail = ref<Mail | null>(null)
const recipients = ref<Message[]>([])

const locked = ref<boolean | null>(null)

onMounted(async () => {
  try {
    const d = await get<{ unlocked: boolean }>('/mailbox/lock-status')
    locked.value = !d.unlocked
  } catch {
    locked.value = true
  }
  if (locked.value === false) loadSenders()
})

function onUnlocked() {
  locked.value = false
  loadSenders()
}

async function loadSenders() {
  loading.value = true
  try {
    const d = await get<{ senders: Sender[] }>('/email-senders')
    // Only colleagues. Your own mailbox is the other page, and listing
    // yourself here would blur the distinction this page exists to draw.
    senders.value = (d.senders ?? []).filter((s) => s.employeeId !== auth.employeeId)
  } finally {
    loading.value = false
  }
}

function choose(row: Sender) {
  chosen.value = row
  view.value = 'sent'
  keyword.value = ''
  reload()
}

function reload() {
  page.value = 1
  msgAt.value = ''
  msgStack.value = []
  load()
}

// Forward remembers where this page started so 上一页 can replay it; there is
// no way to page backwards through a keyset list except by retracing.
function nextMessages() {
  if (!msgCursor.value) return
  msgStack.value = [...msgStack.value, msgAt.value]
  msgAt.value = msgCursor.value
  load()
}

function prevMessages() {
  if (!msgStack.value.length) return
  msgAt.value = msgStack.value[msgStack.value.length - 1]
  msgStack.value = msgStack.value.slice(0, -1)
  load()
}

async function load() {
  if (!chosen.value) return
  loading.value = true
  try {
    if (view.value === 'sent') {
      const d = await get<{ campaigns: Campaign[]; meta: { total: string } }>('/email-campaigns', {
        page: page.value,
        page_size: pageSize,
        keyword: keyword.value,
        sender_id: chosen.value.employeeId,
      })
      campaigns.value = d.campaigns ?? []
      total.value = Number(d.meta?.total ?? 0)
    } else {
      const d = await get<{
        messages: Message[]
        meta: { total: string }
        nextCursor: string
      }>('/email-messages', {
        page_size: pageSize,
        keyword: keyword.value,
        attention_only: true,
        sender_id: chosen.value.employeeId,
        cursor: msgAt.value,
      })
      messages.value = d.messages ?? []
      total.value = Number(d.meta?.total ?? 0)
      msgCursor.value = d.nextCursor ?? ''
    }
  } finally {
    loading.value = false
  }
}

async function openCampaign(row: Campaign) {
  readerOpen.value = true
  openMail.value = null
  recipients.value = []
  const d = await get<{ messages: Message[] }>('/email-messages', {
    campaign_id: row.id,
    page_size: 200,
  })
  recipients.value = d.messages ?? []
  if (recipients.value.length) {
    const full = await get<{ message: Mail }>(`/email-messages/${recipients.value[0].id}`)
    openMail.value = full.message
  }
}

async function openMessage(row: Message) {
  const d = await get<{ message: Mail }>(`/email-messages/${row.id}`)
  openMail.value = d.message
  recipients.value = []
  readerOpen.value = true
}

function recipientLine(row: Campaign) {
  const names = (row.toNames || '').split(', ').filter(Boolean)
  if (names.length === 0) return '—'
  if (names.length <= 2) return names.join('、')
  return t('emails.andOthers', { a: names[0], b: names[1], n: names.length - 2 })
}

function statusType(s: string): 'success' | 'warning' | 'danger' | 'info' {
  if (s === 'DELIVERED' || s === 'ACCEPTED') return 'success'
  if (s === 'QUEUED' || s === 'SENDING') return 'info'
  if (s === 'HARD_BOUNCED' || s === 'COMPLAINED' || s === 'FAILED') return 'danger'
  return 'warning'
}

function shortTime(v: string) {
  if (!v) return ''
  return v.replace('T', ' ').replace('Z', '').slice(0, 16)
}
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: baseline;
  gap: 14px;
  margin-bottom: 16px;
}
.page-head h2 {
  margin: 0;
  font-size: 20px;
}
.head-note,
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.who {
  margin-bottom: 12px;
}
.who :deep(.el-card__body) {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
}
.who-name {
  font-size: 16px;
  font-weight: 600;
}
.grow {
  flex: 1;
}
.tabs {
  margin-bottom: 14px;
}
.strong {
  font-weight: 500;
}
.num {
  font-variant-numeric: tabular-nums;
}
.ok {
  color: var(--el-color-success);
}
.bad {
  color: var(--el-color-danger);
  font-weight: 600;
}
.counts {
  display: flex;
  gap: 10px;
  font-size: 12px;
}
.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.clickable :deep(.el-table__row) {
  cursor: pointer;
}
.drawer-body {
  padding: 4px 6px;
}
.side-title {
  margin: 26px 0 10px;
  font-size: 13px;
  font-weight: 600;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
/* The keyset pager is our own markup, not el-pagination's, so it needs the
   layout el-pagination brought with it. */
.pager.keyset {
  display: flex;
  align-items: center;
  gap: 10px;
}
</style>
