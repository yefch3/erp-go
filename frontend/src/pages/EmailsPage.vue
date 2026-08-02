<template>
  <MailboxGate v-if="locked === true" @unlocked="onUnlocked" @host-settings="hostOpen = true" />
  <div v-else-if="locked === false" class="mailbox">
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
        <el-badge v-else-if="f.key === 'inbox' && unreadCount > 0" :value="unreadCount" />
        <span v-else-if="f.key === 'drafts' && drafts.length" class="cnt">{{ drafts.length }}</span>
      </button>

      <!-- No colleague picker here. This page is "my mail" and stays that
           way; reading somebody else's is a separate, read-only surface. A
           filter here also let a supervisor requeue or abandon a colleague's
           message, which is the salesperson's call, not theirs. -->

      <span class="rail-grow" />
      <!-- Locks the mailbox, not the ERP: the token dies server-side, so the
           next visitor to this workstation faces the gate again. -->
      <el-button link class="rail-lock" @click="lockMailbox">
        🔒 {{ t('mailGate.signOut') }}
      </el-button>
      <el-button v-if="isAdmin" link class="rail-lock rail-host" @click="hostOpen = true">
        ⚙️ {{ t('mailGate.hostSettings') }}
      </el-button>
    </aside>

    <section class="pane">
      <div class="pane-head">
        <h2>{{ t(`emails.folders.${folder}`) }}</h2>
        <span class="grow" />
        <el-input
          v-model="keyword"
          :placeholder="t(`emails.search.${searchKey}`)"
          clearable
          style="width: 260px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button @click="reload">{{ common('query') }}</el-button>
        <!-- The mailbox is polled every couple of minutes; this is for the
             person who just told a customer "resend it" and is waiting. -->
        <el-button v-if="folder === 'inbox'" :loading="syncing" @click="syncNow">
          {{ t('emails.syncNow') }}
        </el-button>
        <el-button v-if="folder === 'suppressions' && canSuppress" @click="openSuppress">
          {{ t('emails.addSuppression') }}
        </el-button>
      </div>

      <!-- ------------------------------- inbox / starred / archive / trash -->
      <template v-if="isInboundView">
        <el-table
          :data="inbound"
          v-loading="loading"
          class="clickable"
          :row-class-name="inboundRowClass"
          @row-click="openInbound"
        >
          <el-table-column width="44">
            <template #default="{ row }">
              <span
                class="star"
                :class="{ on: row.isStarred }"
                :title="t(row.isStarred ? 'emails.unstar' : 'emails.star')"
                @click.stop="toggleStar(row)"
              >{{ row.isStarred ? '★' : '☆' }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('emails.from')" min-width="200">
            <template #default="{ row }">
              <div class="strong ellipsis">
                <span v-if="!row.isRead" class="unread-dot" />
                {{ row.fromName || row.fromEmail }}
              </div>
              <div class="sub ellipsis">{{ row.fromEmail }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('emails.subject')" min-width="320">
            <template #default="{ row }">
              <div class="ellipsis" :class="{ strong: !row.isRead }" :title="row.subject">
                {{ row.subject || t('emails.noSubject') }}
                <span v-if="row.hasAttachments" class="clip">📎</span>
              </div>
              <div class="sub ellipsis">{{ row.snippet }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('emails.receivedAt')" width="150">
            <template #default="{ row }">
              <span class="sub">{{ shortTime(row.receivedAt) }}</span>
            </template>
          </el-table-column>
        </el-table>
        <el-empty
          v-if="!loading && inbound.length === 0"
          :description="t(folder === 'inbox' ? 'emails.emptyInbox' : 'emails.emptyFolder')"
        />
      </template>

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
      <template v-else-if="folder === 'sent'">
      <el-radio-group v-model="sentView" size="small" class="sent-toggle" @change="reload">
        <el-radio-button value="erp">{{ t('emails.sentViaErp') }}</el-radio-button>
        <el-radio-button value="mailbox">{{ t('emails.sentViaMailbox') }}</el-radio-button>
      </el-radio-group>

      <!-- The mailbox's own Sent folder: every client, the whole synced
           history, no delivery status because the host records none. -->
      <el-table
        v-if="sentView === 'mailbox'"
        :data="mailboxSent"
        v-loading="loading"
        class="clickable"
        @row-click="openInbound"
      >
        <el-table-column :label="t('emails.recipient')" min-width="200">
          <template #default="{ row }">
            <div class="strong ellipsis">{{ row.toEmail || '—' }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.subject')" min-width="320">
          <template #default="{ row }">
            <div class="ellipsis" :title="row.subject">
              {{ row.subject || t('emails.noSubject') }}
              <span v-if="row.hasAttachments" class="clip">📎</span>
            </div>
            <div class="sub ellipsis">{{ row.snippet }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('emails.sentAt')" width="150">
          <template #default="{ row }">
            <span class="sub">{{ shortTime(row.sentAt || row.receivedAt) }}</span>
          </template>
        </el-table-column>
      </el-table>

      <el-table
        v-else
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
      </template>

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
        v-if="folder === 'sent' || folder === 'attention' || isInboundView"
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
    <!-- Received mail gets its own reader: MailReader narrates a delivery
         attempt (status, retries, events), none of which a message somebody
         sent US has. Showing that chrome around a customer's mail would be
         confusing at best. -->
    <el-drawer v-model="inboundOpen" size="58%" :with-header="false">
      <div v-if="openedInbound" class="drawer-body">
        <h3 class="in-subject">{{ openedInbound.subject || t('emails.noSubject') }}</h3>
        <div class="in-meta">
          <span class="strong">{{ openedInbound.fromName || openedInbound.fromEmail }}</span>
          <span class="sub">&lt;{{ openedInbound.fromEmail }}&gt;</span>
          <span class="grow" />
          <span class="sub">{{ shortTime(openedInbound.sentAt || openedInbound.receivedAt) }}</span>
        </div>
        <div class="in-meta sub">{{ t('emails.inboundTo', { to: openedInbound.toEmail }) }}</div>
        <div class="in-actions">
          <template v-if="canWrite">
            <el-button size="small" type="primary" plain @click="replyToInbound">
              ↩ {{ t('emails.reply') }}
            </el-button>
            <el-button size="small" plain @click="forwardInbound">
              ↪ {{ t('emails.forward') }}
            </el-button>
          </template>
          <template v-if="isInboundView">
            <el-button v-if="folder !== 'trash'" size="small" plain @click="markOpened({ read: false })">
              {{ t('emails.markUnread') }}
            </el-button>
            <el-button v-if="folder === 'archive'" size="small" plain @click="markOpened({ archived: false })">
              {{ t('emails.unarchive') }}
            </el-button>
            <el-button v-else-if="folder !== 'trash'" size="small" plain @click="markOpened({ archived: true })">
              {{ t('emails.archive') }}
            </el-button>
            <el-button v-if="folder === 'trash'" size="small" plain @click="markOpened({ deleted: false })">
              {{ t('emails.restore') }}
            </el-button>
            <el-button v-else size="small" type="danger" plain @click="markOpened({ deleted: true })">
              {{ t('emails.toTrash') }}
            </el-button>
          </template>
        </div>
        <el-divider />
        <!-- Sanitised server-side before it got here; see GetInbound. -->
        <div v-if="openedInbound.bodyHtml" class="in-html" v-html="openedInbound.bodyHtml" />
        <pre v-else class="in-text">{{ openedInbound.bodyText }}</pre>
        <template v-if="openedInbound.attachments?.length">
          <el-divider />
          <h4 class="side-title">{{ t('emails.attachments') }}</h4>
          <div class="chips">
            <el-tag v-for="a in openedInbound.attachments" :key="a.id" type="info">
              {{ a.fileName }} · {{ humanSize(Number(a.fileSize)) }}
            </el-tag>
          </div>
        </template>
      </div>
    </el-drawer>

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
            <!-- "可能已打开", never "已读". The pixel over-reports for Apple
                 Mail (it pre-fetches images unopened) and under-reports for
                 Outlook (it blocks them when genuinely read). A vague label
                 that is honest beats a precise one that is wrong. -->
            <el-table-column :label="t('emails.openedCol')" width="120">
              <template #default="{ row }">
                <el-tooltip
                  v-if="row.openedAt"
                  :content="t('emails.openedHint', { at: shortTime(row.openedAt) })"
                  placement="top"
                >
                  <span class="opened">{{ t('emails.maybeOpened') }}</span>
                </el-tooltip>
                <span v-else class="sub">—</span>
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
  <!-- Outside the locked/unlocked branches: an administrator may need the
       host settings before anybody can sign in at all. -->
  <MailHostDialog v-model="hostOpen" />
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { del, get, http, post } from '../api'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'
import EmailComposer from '../components/EmailComposer.vue'
import MailReader, { type Mail } from '../components/MailReader.vue'
import MailboxGate from '../components/MailboxGate.vue'
import MailHostDialog from '../components/MailHostDialog.vue'

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
  openedAt?: string
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
interface InboundMail {
  id: string
  fromEmail: string
  fromName: string
  toEmail: string
  subject: string
  snippet: string
  threadKey: string
  isRead: boolean
  isStarred: boolean
  hasAttachments: boolean
  receivedAt: string
  sentAt: string
  bodyHtml?: string
  bodyText?: string
  attachments?: { id: string; fileName: string; contentType: string; fileSize: string }[]
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
const isAdmin = computed(() => auth.can('iam:role:write'))

const folders = [
  { key: 'inbox' },
  { key: 'starred' },
  { key: 'drafts' },
  { key: 'sent' },
  { key: 'attention' },
  { key: 'archive' },
  { key: 'trash' },
  { key: 'suppressions' },
]
// The inbox is the landing folder now that mail actually arrives in it.
const folder = ref('inbox')
// The four slices of the same mailbox: what differs is only the filter the
// server applies, so they share one table, one search box and one pager.
const INBOUND_VIEWS: Record<string, string> = {
  inbox: 'INBOX',
  starred: 'STARRED',
  archive: 'ARCHIVE',
  trash: 'TRASH',
}
const isInboundView = computed(() => folder.value in INBOUND_VIEWS)
const searchKey = computed(() => {
  if (folder.value === 'sent') return 'campaigns'
  if (isInboundView.value) return 'inbox'
  return folder.value
})

const keyword = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const loading = ref(false)
const campaigns = ref<Campaign[]>([])
const messages = ref<Message[]>([])
const suppressions = ref<Suppression[]>([])
const attentionCount = ref(0)
const composing = ref(false)
const composer = ref()
const drafts = ref<Draft[]>([])
const acting = ref(false)

const inbound = ref<InboundMail[]>([])
// Which sent list is showing: what the ERP sent (with delivery status and
// open tracking) or the mailbox's own Sent folder (plain history, any client).
const sentView = ref<'erp' | 'mailbox'>('erp')
const mailboxSent = ref<InboundMail[]>([])
const unreadCount = ref(0)
const syncing = ref(false)
const inboundOpen = ref(false)
const openedInbound = ref<InboundMail | null>(null)

const readerOpen = ref(false)
const openMail = ref<Mail | null>(null)
const recipients = ref<Message[]>([])
const recipientsLoading = ref(false)

const requeueOpen = ref(false)
const requeueRow = ref<Message | null>(null)
const newEmail = ref('')

const suppressOpen = ref(false)
const suppressForm = reactive({ email: '', reason: 'UNSUBSCRIBE', detail: '' })

// null = still asking; the page renders nothing rather than flashing the
// gate at somebody who is already unlocked.
const locked = ref<boolean | null>(null)
const hostOpen = ref(false)

onMounted(async () => {
  // Back from Google's login page. On success the fresh grant is verified
  // right away, so binding and entering the mailbox is one motion instead of
  // a redirect followed by a second button.
  const q = new URLSearchParams(location.search)
  const oauthResult = q.get('oauth')
  if (oauthResult) {
    const boundEmail = q.get('email') ?? ''
    const reason = q.get('reason') ?? ''
    history.replaceState(null, '', location.pathname)
    if (oauthResult === 'ok') {
      ElMessage.success(t('mailbox.googleOk', { email: boundEmail }))
      try {
        const resp = await http.post('/mailbox/verify', { secret: '' })
        const data = resp.data.data as { token: string }
        sessionStorage.setItem('mailUnlock', data.token)
        locked.value = false
        init()
        return
      } catch {
        /* fall through: the gate appears and says why in place */
      }
    } else {
      ElMessage({
        type: 'error',
        message: t('mailbox.googleErr', { reason }),
        duration: 0,
        showClose: true,
      })
    }
  }
  try {
    const d = await get<{ unlocked: boolean }>('/mailbox/lock-status')
    locked.value = !d.unlocked
  } catch {
    locked.value = true
  }
  if (locked.value === false) init()
})

function onUnlocked() {
  locked.value = false
  init()
}

async function lockMailbox() {
  try {
    await post('/mailbox/lock')
  } finally {
    // Locked locally regardless: a failed revoke call must not leave the
    // screen open while the person walks away believing it is shut.
    sessionStorage.removeItem('mailUnlock')
    locked.value = true
  }
}

function init() {
  load()
  refreshAttentionCount()
  loadDraftCount()
  refreshUnread()
}

// New mail is pushed over the same SSE stream the rest of the app uses: the
// IMAP sync stores it, publishes a hint, and every open tab of the owner's
// hears it here. The re-fetch goes through the normal API, so this is only
// ever "go and look", never data.
onUnmounted(
  onLive((e) => {
    if (e.type !== 'mail.inbound') return
    if (folder.value === 'inbox') {
      load()
    } else {
      refreshUnread()
    }
  }),
)

// The rail badge has to be current whichever folder is open, so it has its
// own cheap fetch rather than riding on the inbox list load.
async function refreshUnread() {
  try {
    const d = await get<{ unreadCount: number }>('/inbound-mails', { page: 1, page_size: 1 })
    unreadCount.value = Number(d.unreadCount ?? 0)
  } catch {
    /* the badge going stale is not worth an error toast */
  }
}

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
  loading.value = true
  try {
    if (isInboundView.value) {
      const d = await get<{ mails: InboundMail[]; meta: { total: string }; unreadCount: number }>(
        '/inbound-mails',
        {
          page: page.value,
          page_size: pageSize,
          keyword: keyword.value,
          view: INBOUND_VIEWS[folder.value],
        },
      )
      inbound.value = d.mails ?? []
      total.value = Number(d.meta?.total ?? 0)
      unreadCount.value = Number(d.unreadCount ?? 0)
    } else if (folder.value === 'drafts') {
      const d = await get<{ drafts: Draft[] }>('/email-drafts')
      drafts.value = d.drafts ?? []
    } else if (folder.value === 'sent' && sentView.value === 'mailbox') {
      const d = await get<{ mails: InboundMail[]; meta: { total: string } }>('/mailbox-sent', {
        page: page.value,
        page_size: pageSize,
        keyword: keyword.value,
      })
      mailboxSent.value = d.mails ?? []
      total.value = Number(d.meta?.total ?? 0)
    } else if (folder.value === 'sent') {
      const d = await get<{ campaigns: Campaign[]; meta: { total: string } }>('/email-campaigns', {
        page: page.value,
        page_size: pageSize,
        keyword: keyword.value,
      })
      campaigns.value = d.campaigns ?? []
      total.value = Number(d.meta?.total ?? 0)
    } else if (folder.value === 'attention') {
      const d = await get<{ messages: Message[]; meta: { total: string } }>('/email-messages', {
        page: page.value,
        page_size: pageSize,
        keyword: keyword.value,
        attention_only: true,
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

// Opening marks it read server-side, so the row and the badge both update
// from what the server actually stored rather than optimistically.
async function openInbound(row: InboundMail) {
  const d = await get<{ mail: InboundMail }>(`/inbound-mails/${row.id}`)
  openedInbound.value = d.mail
  inboundOpen.value = true
  if (!row.isRead) {
    row.isRead = true
    unreadCount.value = Math.max(0, unreadCount.value - 1)
  }
}

function inboundRowClass({ row }: { row: InboundMail }) {
  return row.isRead ? '' : 'unread-row'
}

// Housekeeping calls. All of them are ERP-side state only — nothing is ever
// written back to the mail host, so none of these can touch the real mailbox.
async function toggleStar(row: InboundMail) {
  // Optimistic: a star that waits for the network feels broken.
  row.isStarred = !row.isStarred
  try {
    await post(`/inbound-mails/${row.id}/mark`, { starred: row.isStarred })
  } catch {
    row.isStarred = !row.isStarred
  }
  if (folder.value === 'starred' && !row.isStarred) load()
}

// The reader-drawer actions: mark unread, archive/unarchive, trash/restore.
// Each closes the drawer and reloads — the row just left this view.
async function markOpened(flags: Record<string, boolean>) {
  if (!openedInbound.value) return
  await post(`/inbound-mails/${openedInbound.value.id}/mark`, flags)
  inboundOpen.value = false
  load()
  refreshUnread()
}

// Reply and forward hand the opened mail to the composer, which prefills
// recipient/subject/quote; the server does the threading (reply) and carries
// the original's attachments (forward).
async function replyToInbound() {
  if (!openedInbound.value) return
  const mail = openedInbound.value
  inboundOpen.value = false
  composing.value = true
  await nextTick()
  composer.value?.openReply(mail)
}

async function forwardInbound() {
  if (!openedInbound.value) return
  const mail = openedInbound.value
  inboundOpen.value = false
  composing.value = true
  await nextTick()
  composer.value?.openForward(mail)
}

async function syncNow() {
  syncing.value = true
  try {
    const d = await post<{ fetched: number; detail: string }>('/mailbox/sync')
    if (d.detail) {
      ElMessage({ type: 'error', message: d.detail, duration: 0, showClose: true })
    } else {
      ElMessage.success(t('emails.syncDone', { n: d.fetched ?? 0 }))
      reload()
    }
  } finally {
    syncing.value = false
  }
}

function humanSize(bytes: number) {
  if (!bytes) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
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

.unread-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--el-color-primary);
  margin-right: 6px;
  vertical-align: middle;
}
.clip {
  margin-left: 4px;
  color: var(--el-text-color-secondary);
  vertical-align: middle;
}
:deep(.unread-row) {
  font-weight: 500;
}
.in-subject {
  margin: 0 0 10px;
  font-size: 18px;
}
.in-meta {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.in-actions {
  margin-top: 10px;
}
.star {
  font-size: 15px;
  color: var(--el-text-color-placeholder);
  cursor: pointer;
}
.star.on {
  color: var(--el-color-warning);
}
.star:hover {
  color: var(--el-color-warning);
}
.in-html {
  line-height: 1.6;
  word-break: break-word;
}
.in-html :deep(img) {
  max-width: 100%;
}
.in-text {
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
  margin: 0;
}

.opened {
  color: var(--el-color-success);
  font-size: 12px;
  cursor: default;
}

.sent-toggle {
  margin-bottom: 12px;
}

.rail-grow {
  flex: 1;
}
.rail-lock {
  margin-top: 14px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.rail-host {
  display: block;
  margin: 6px 0 0;
}
</style>
