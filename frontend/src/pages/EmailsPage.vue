<template>
  <div class="mailbox">
    <!-- A folder rail, not tabs. The distinction matters: folders say "your
         mail lives in these places", tabs said "here are three reports". -->
    <aside class="rail">
      <el-button v-if="canWrite" type="primary" class="compose" @click="composing = true">
        {{ t('emails.compose') }}
      </el-button>
      <button
        v-for="f in folders"
        :key="f.key"
        class="folder"
        :class="{ on: folder === f.key }"
        type="button"
        @click="switchFolder(f.key)"
      >
        <span class="fname">{{ t(`emails.folders.${f.key}`) }}</span>
        <el-badge v-if="f.key === 'attention' && attentionCount > 0" :value="attentionCount" />
        <span v-else-if="f.key === 'drafts' && drafts.length" class="cnt">{{ drafts.length }}</span>
      </button>

      <div v-if="senders.length > 1" class="rail-scope">
        <div class="rail-label">{{ t('emails.viewing') }}</div>
        <el-select v-model="senderFilter" clearable size="small" :placeholder="t('emails.allSenders')" @change="reload">
          <el-option
            v-for="s in senders"
            :key="s.employeeId"
            :label="`${s.name}（${s.messageCount}）`"
            :value="s.employeeId"
          />
        </el-select>
      </div>
    </aside>

    <section class="pane">
      <div class="pane-head">
        <h2>{{ t(`emails.folders.${folder}`) }}</h2>
        <span class="grow" />
        <el-input
          v-if="folder !== 'inbox'"
          v-model="keyword"
          :placeholder="t(`emails.search.${searchKey}`)"
          clearable
          style="width: 260px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button v-if="folder !== 'inbox'" @click="reload">{{ common('query') }}</el-button>
        <el-button v-if="folder === 'suppressions' && canSuppress" @click="openSuppress">
          {{ t('emails.addSuppression') }}
        </el-button>
      </div>

      <!-- ---------------------------------------------------------- inbox -->
      <!-- Present and honest. Faking an empty inbox would suggest customers
           simply have not replied; saying the channel is not connected says
           what is actually true. -->
      <el-card v-if="folder === 'inbox'" shadow="never" class="notyet">
        <el-empty :description="t('emails.inboxNotConnected')">
          <div class="notyet-text">{{ t('emails.inboxExplain') }}</div>
        </el-empty>
      </el-card>

      <!-- --------------------------------------------------------- drafts -->
      <template v-else-if="folder === 'drafts'">
      <el-table
        :data="drafts"
        v-loading="loading"
        class="clickable"
        @row-click="openDraft"
      >
        <el-table-column :label="t('emails.subject')" min-width="300">
          <template #default="{ row }">
            <div class="strong ellipsis">{{ row.subject || t('emails.noSubject') }}</div>
            <div class="sub">{{ t('emails.draftRecipients', { n: row.recipientCount }) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.savedAt')" width="160">
          <template #default="{ row }">
            <span class="sub">{{ shortTime(row.updatedAt) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="common('actions')" width="90">
          <template #default="{ row }">
            <el-button link type="danger" @click.stop="dropDraft(row)">
              {{ common('delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && drafts.length === 0" :description="t('emails.noDrafts')" />
      </template>

      <!-- ----------------------------------------------------------- sent -->
      <el-table
        v-else-if="folder === 'sent'"
        :data="campaigns"
        v-loading="loading"
        class="clickable"
        @row-click="openCampaign"
      >
        <el-table-column :label="t('emails.to')" min-width="210">
          <template #default="{ row }">
            <div class="strong ellipsis" :title="row.toNames">{{ recipientLine(row) }}</div>
            <div class="sub">{{ row.senderName }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.subject')" min-width="280">
          <template #default="{ row }">
            <div class="ellipsis" :title="row.subject">{{ row.subject }}</div>
            <div class="sub">{{ row.campaignNo }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.progress')" width="210">
          <template #default="{ row }">
            <div class="counts">
              <span class="ok">{{ t('emails.cDelivered', { n: row.sentCount }) }}</span>
              <span v-if="row.pendingCount > 0" class="pend">
                {{ t('emails.cPending', { n: row.pendingCount }) }}
              </span>
              <span v-if="row.failedCount > 0" class="bad">
                {{ t('emails.cFailed', { n: row.failedCount }) }}
              </span>
            </div>
            <el-progress
              :percentage="donePercent(row)"
              :status="row.failedCount > 0 ? 'warning' : undefined"
              :stroke-width="6"
              :show-text="false"
            />
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.sentAt')" width="140">
          <template #default="{ row }">
            <span class="sub">{{ shortTime(row.createdAt) }}</span>
          </template>
        </el-table-column>
      </el-table>

      <!-- ------------------------------------------------------ attention -->
      <el-table
        v-else-if="folder === 'attention'"
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
        <el-table-column :label="t('emails.whyStuck')" min-width="270">
          <template #default="{ row }">
            <div>{{ row.attentionReason || '—' }}</div>
            <div v-if="row.lastError" class="sub ellipsis" :title="row.lastError">
              {{ row.lastError }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="common('actions')" width="150">
          <template #default="{ row }">
            <template v-if="canWrite">
              <el-button link type="primary" @click.stop="openRequeue(row)">
                {{ t('emails.requeue') }}
              </el-button>
              <el-button link type="danger" @click.stop="doAbandon(row)">
                {{ t('emails.abandon') }}
              </el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>

      <!-- --------------------------------------------------- suppressions -->
      <el-table v-else :data="suppressions" v-loading="loading">
        <el-table-column prop="email" :label="t('emails.email')" min-width="230" />
        <el-table-column :label="t('emails.suppressReason')" width="150">
          <template #default="{ row }">
            <el-tag size="small" effect="plain" :type="row.reason === 'COMPLAINT' ? 'danger' : 'info'">
              {{ t(`emails.skipReasons.${row.reason}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.suppressDetail')" min-width="260">
          <template #default="{ row }">
            <div class="ellipsis" :title="row.detail">{{ row.detail || '—' }}</div>
            <div class="sub">{{ shortTime(row.createdAt) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="common('actions')" width="100">
          <template #default="{ row }">
            <el-button v-if="canSuppress" link type="danger" @click="doUnsuppress(row)">
              {{ t('emails.unsuppress') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-if="folder === 'sent' || folder === 'attention'"
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        class="pager"
        @current-change="load"
      />
    </section>

    <EmailComposer ref="composer" v-model="composing" @sent="onSent" @saved="onDraftSaved" />

    <!-- Reading a sent mail: the message itself, then who it went to. That
         ordering is the point — the mail is the thing, the recipient list is
         the detail underneath it. -->
    <el-drawer v-model="readerOpen" size="58%" :with-header="false">
      <div class="drawer-body">
        <MailReader :mail="openMail" />
        <template v-if="folder === 'sent' && recipients.length">
          <h4 class="side-title">
            {{ t('emails.recipientsOfSend', { n: recipients.length }) }}
          </h4>
          <el-table :data="recipients" size="small" v-loading="recipientsLoading">
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
            <el-table-column :label="common('actions')" width="80">
              <template #default="{ row }">
                <el-button link type="primary" @click="openMessage(row)">
                  {{ t('emails.openOne') }}
                </el-button>
              </template>
            </el-table-column>
          </el-table>
        </template>
      </div>
    </el-drawer>

    <el-dialog v-model="requeueOpen" :title="t('emails.requeueTitle')" width="480px">
      <p class="hint">{{ t('emails.requeueHint') }}</p>
      <el-form label-width="90px">
        <el-form-item :label="t('emails.oldAddress')">
          <span class="mono">{{ requeueRow?.toEmail }}</span>
        </el-form-item>
        <el-form-item :label="t('emails.newAddress')">
          <el-input v-model="newEmail" :placeholder="t('emails.newAddressPlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="requeueOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="acting" @click="doRequeue">
          {{ common('confirm') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="suppressOpen" :title="t('emails.addSuppression')" width="480px">
      <el-form label-width="90px">
        <el-form-item :label="t('emails.email')">
          <el-input v-model="suppressForm.email" />
        </el-form-item>
        <el-form-item :label="t('emails.suppressReason')">
          <el-select v-model="suppressForm.reason" style="width: 100%">
            <el-option :label="t('emails.skipReasons.UNSUBSCRIBE')" value="UNSUBSCRIBE" />
            <el-option :label="t('emails.skipReasons.COMPLAINT')" value="COMPLAINT" />
            <el-option :label="t('emails.skipReasons.MANUAL')" value="MANUAL" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('emails.suppressDetail')">
          <el-input v-model="suppressForm.detail" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="suppressOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="acting" @click="doSuppress">
          {{ common('confirm') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { del, get, post } from '../api'
import { useAuthStore } from '../stores/auth'
import EmailComposer from '../components/EmailComposer.vue'
import MailReader, { type Mail } from '../components/MailReader.vue'

interface Campaign {
  id: string
  campaignNo: string
  subject: string
  toNames: string
  senderName: string
  createdAt: string
  totalCount: number
  sentCount: number
  failedCount: number
  pendingCount: number
}
interface Message {
  id: string
  campaignId: string
  senderName: string
  toEmail: string
  toName: string
  customerName: string
  subject: string
  status: string
  attemptCount: number
  lastError: string
  attentionReason: string
  sentAt: string
}
interface Draft {
  id: string
  subject: string
  recipientCount: number
  updatedAt: string
}
interface Sender {
  employeeId: string
  name: string
  messageCount: number
  failedCount: number
}
interface Suppression {
  id: string
  email: string
  reason: string
  detail: string
  createdAt: string
}

const { t } = useI18n()
const common = (k: string) => t(`common.${k}`)
const auth = useAuthStore()
const canWrite = computed(() => auth.can('notification:email:write'))
const canSuppress = computed(() => auth.can('notification:suppression:write'))

const folders = [
  { key: 'inbox' },
  { key: 'drafts' },
  { key: 'sent' },
  { key: 'attention' },
  { key: 'suppressions' },
]
// Sent is the landing folder, not Inbox: until the inbound channel exists,
// opening on an empty Inbox every time would be a daily lie.
const folder = ref('sent')
const searchKey = computed(() => (folder.value === 'sent' ? 'campaigns' : folder.value))

const keyword = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const loading = ref(false)
const campaigns = ref<Campaign[]>([])
const messages = ref<Message[]>([])
const suppressions = ref<Suppression[]>([])
const senders = ref<Sender[]>([])
const senderFilter = ref('')
const attentionCount = ref(0)
const composing = ref(false)
const composer = ref()
const drafts = ref<Draft[]>([])
const acting = ref(false)

const readerOpen = ref(false)
const openMail = ref<Mail | null>(null)
const recipients = ref<Message[]>([])
const recipientsLoading = ref(false)

const requeueOpen = ref(false)
const requeueRow = ref<Message | null>(null)
const newEmail = ref('')

const suppressOpen = ref(false)
const suppressForm = reactive({ email: '', reason: 'UNSUBSCRIBE', detail: '' })

onMounted(() => {
  load()
  refreshAttentionCount()
  loadSenders()
  loadDraftCount()
})

// The rail shows a count, so it has to be current whichever folder is open.
async function loadDraftCount() {
  try {
    const d = await get<{ drafts: Draft[] }>('/email-drafts')
    drafts.value = d.drafts ?? []
  } catch {
    drafts.value = []
  }
}

async function openDraft(row: Draft) {
  composing.value = true
  // Wait for the dialog to mount before handing it the draft, or the watch
  // that resets a fresh compose would wipe what we just loaded.
  await nextTick()
  await composer.value?.openDraft(row.id)
}

function onDraftSaved() {
  loadDraftCount()
  if (folder.value === 'drafts') load()
}

async function dropDraft(row: Draft) {
  await ElMessageBox.confirm(
    t('emails.dropDraftHint', { s: row.subject || t('emails.noSubject') }),
    common('delete'),
    { type: 'warning' },
  )
  await del(`/email-drafts/${row.id}`)
  ElMessage.success(t('emails.draftDropped'))
  load()
  loadDraftCount()
}

function switchFolder(key: string) {
  folder.value = key
  keyword.value = ''
  reload()
}

function reload() {
  page.value = 1
  load()
}

async function load() {
  if (folder.value === 'inbox') return
  loading.value = true
  try {
    if (folder.value === 'drafts') {
      const d = await get<{ drafts: Draft[] }>('/email-drafts')
      drafts.value = d.drafts ?? []
    } else if (folder.value === 'sent') {
      const d = await get<{ campaigns: Campaign[]; meta: { total: string } }>('/email-campaigns', {
        page: page.value,
        page_size: pageSize,
        keyword: keyword.value,
        sender_id: senderFilter.value || undefined,
      })
      campaigns.value = d.campaigns ?? []
      total.value = Number(d.meta?.total ?? 0)
    } else if (folder.value === 'attention') {
      const d = await get<{ messages: Message[]; meta: { total: string } }>('/email-messages', {
        page: page.value,
        page_size: pageSize,
        keyword: keyword.value,
        attention_only: true,
        sender_id: senderFilter.value || undefined,
      })
      messages.value = d.messages ?? []
      total.value = Number(d.meta?.total ?? 0)
      attentionCount.value = total.value
    } else {
      const d = await get<{ suppressions: Suppression[] }>('/email-suppressions', {
        keyword: keyword.value,
      })
      suppressions.value = d.suppressions ?? []
    }
  } finally {
    loading.value = false
  }
}

// The badge is what tells somebody there is work waiting, so it refreshes
// independently of whichever folder happens to be open.
async function refreshAttentionCount() {
  const d = await get<{ meta: { total: string } }>('/email-messages', {
    page: 1,
    page_size: 1,
    attention_only: true,
  })
  attentionCount.value = Number(d.meta?.total ?? 0)
}

async function loadSenders() {
  try {
    const d = await get<{ senders: Sender[] }>('/email-senders')
    senders.value = d.senders ?? []
  } catch {
    senders.value = []
  }
}

function onSent() {
  folder.value = 'sent'
  reload()
  refreshAttentionCount()
  loadDraftCount()
}

// "Hans Weber, Mike Chen and 3 others" — how a mailbox summarises a send
// without listing everybody.
function recipientLine(row: Campaign) {
  const names = (row.toNames || '').split(', ').filter(Boolean)
  if (names.length === 0) return '—'
  if (names.length <= 2) return names.join('、')
  return t('emails.andOthers', { a: names[0], b: names[1], n: names.length - 2 })
}

function donePercent(row: Campaign) {
  if (!row.totalCount) return 0
  return Math.round(((row.sentCount + row.failedCount) / row.totalCount) * 100)
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

// A bulk send has N copies of one mail. Opening it shows the mail — read from
// the first recipient's copy, which is the only place the rendered text
// exists — and lists the recipients underneath.
async function openCampaign(row: Campaign) {
  readerOpen.value = true
  openMail.value = null
  recipients.value = []
  recipientsLoading.value = true
  try {
    const d = await get<{ messages: Message[] }>('/email-messages', {
      campaign_id: row.id,
      page_size: 200,
    })
    recipients.value = d.messages ?? []
    if (recipients.value.length) {
      const full = await get<{ message: Mail }>(`/email-messages/${recipients.value[0].id}`)
      openMail.value = full.message
    }
  } finally {
    recipientsLoading.value = false
  }
}

async function openMessage(row: Message) {
  const d = await get<{ message: Mail }>(`/email-messages/${row.id}`)
  openMail.value = d.message
  if (folder.value !== 'sent') recipients.value = []
  readerOpen.value = true
}

function openRequeue(row: Message) {
  requeueRow.value = row
  newEmail.value = ''
  requeueOpen.value = true
}

async function doRequeue() {
  if (!requeueRow.value) return
  acting.value = true
  try {
    await post(`/email-messages/${requeueRow.value.id}/requeue`, { newEmail: newEmail.value })
    ElMessage.success(t('emails.requeued'))
    requeueOpen.value = false
    load()
    refreshAttentionCount()
  } finally {
    acting.value = false
  }
}

async function doAbandon(row: Message) {
  const { value } = await ElMessageBox.prompt(t('emails.abandonHint'), t('emails.abandonTitle'), {
    inputPlaceholder: t('emails.abandonReason'),
    inputPattern: /\S/,
    inputErrorMessage: t('emails.abandonReason'),
  })
  await post(`/email-messages/${row.id}/abandon`, { reason: value })
  ElMessage.success(t('emails.abandoned'))
  load()
  refreshAttentionCount()
}

function openSuppress() {
  suppressForm.email = ''
  suppressForm.reason = 'UNSUBSCRIBE'
  suppressForm.detail = ''
  suppressOpen.value = true
}

async function doSuppress() {
  acting.value = true
  try {
    await post('/email-suppressions', { ...suppressForm })
    ElMessage.success(t('emails.suppressed'))
    suppressOpen.value = false
    load()
  } finally {
    acting.value = false
  }
}

async function doUnsuppress(row: Suppression) {
  await ElMessageBox.confirm(t('emails.unsuppressHint', { e: row.email }), t('emails.unsuppress'), {
    type: 'warning',
  })
  await del(`/email-suppressions?email=${encodeURIComponent(row.email)}`)
  ElMessage.success(t('emails.unsuppressed'))
  load()
}
</script>

<style scoped>
.mailbox {
  display: flex;
  gap: 18px;
  align-items: flex-start;
}
.rail {
  flex: none;
  width: 178px;
  position: sticky;
  top: 12px;
}
.compose {
  width: 100%;
  margin-bottom: 14px;
}
.folder {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 9px 12px;
  margin-bottom: 2px;
  background: none;
  border: none;
  border-radius: 0 16px 16px 0;
  font-size: 14px;
  color: var(--el-text-color-regular);
  cursor: pointer;
  text-align: left;
}
.folder:hover {
  background: var(--el-fill-color-light);
}
.folder.on {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-weight: 600;
}
.rail-scope {
  margin-top: 22px;
  padding: 0 12px;
}
.rail-label {
  margin-bottom: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.pane {
  flex: 1;
  min-width: 0;
}
.pane-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}
.pane-head h2 {
  margin: 0;
  font-size: 19px;
}
.grow {
  flex: 1;
}
.strong {
  font-weight: 500;
}
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.counts {
  display: flex;
  gap: 8px;
  margin-bottom: 4px;
  font-size: 12px;
}
.ok {
  color: var(--el-color-success);
}
.pend {
  color: var(--el-color-info);
}
.bad {
  color: var(--el-color-danger);
  font-weight: 600;
}
.clickable :deep(.el-table__row) {
  cursor: pointer;
}
.cnt {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.notyet {
  padding: 30px 0;
}
.notyet-text {
  max-width: 460px;
  margin: 0 auto;
  font-size: 13px;
  line-height: 1.7;
  color: var(--el-text-color-secondary);
}
.drawer-body {
  padding: 4px 6px;
}
.side-title {
  margin: 26px 0 10px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}
.hint {
  margin: 0 0 14px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
</style>
