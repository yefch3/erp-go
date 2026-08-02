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
      <!-- The mailbox saying it is not receiving. Without this, a revoked
           authorisation fails every poll in silence while the page goes on
           showing the last successful sync as though it were current. -->
      <el-alert
        v-if="syncError"
        type="error"
        :closable="false"
        show-icon
        class="sync-error"
      >
        <div class="sync-error-body">
          <span>{{ t('emails.syncBroken', { e: syncError }) }}</span>
          <el-button size="small" type="primary" plain @click="reauth">
            {{ t('emails.reauth') }}
          </el-button>
        </div>
      </el-alert>

      <!-- ------------------------------------------------- reading a mail -->
      <!-- A page, not a drawer: the mail's id lives in the URL, so a refresh
           reopens the same mail and the browser's back button returns to the
           list, at the page it was on. -->
      <template v-if="openedInbound">
        <div class="detail-top">
          <el-button link class="back-btn" @click="backToList">
            ← {{ t('emails.backToList') }}
          </el-button>
        </div>
        <h2 class="in-subject">{{ openedInbound.subject || t('emails.noSubject') }}</h2>
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
          <el-button
            v-if="folder === 'junk'"
            size="small"
            type="warning"
            plain
            @click="markOpened({ notJunk: true })"
          >
            {{ t('emails.notJunk') }}
          </el-button>
          <template v-if="isInboundView && folder !== 'junk'">
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
            <el-button v-if="folder === 'trash'" size="small" type="danger" plain @click="purgeOpened">
              {{ t('emails.purge') }}
            </el-button>
            <el-button v-if="folder !== 'trash'" size="small" type="danger" plain @click="markOpened({ deleted: true })">
              {{ t('emails.toTrash') }}
            </el-button>
          </template>
        </div>
        <el-divider />
        <!-- The whole exchange when there is one, the single mail otherwise.
             All HTML here was sanitised server-side; see GetInbound and
             GetMailThread. -->
        <template v-if="threadItems.length > 1">
          <div class="thread-count">{{ t('emails.threadCount', { n: threadItems.length }) }}</div>
          <div
            v-for="it in threadItems"
            :key="it.direction + it.id"
            class="thread-item"
            :class="{ out: it.direction === 'OUT' }"
          >
            <button type="button" class="thread-head" @click="toggleThreadItem(it)">
              <el-tag size="small" :type="it.direction === 'OUT' ? 'info' : 'success'" effect="plain">
                {{ it.direction === 'OUT' ? t('emails.threadOut') : t('emails.threadIn') }}
              </el-tag>
              <span class="strong">{{ it.who || it.counterparty }}</span>
              <span class="sub ellipsis">{{ it.counterparty }}</span>
              <span class="grow" />
              <span class="sub">{{ shortTime(it.at) }}</span>
            </button>
            <div v-show="isThreadOpen(it)" class="thread-body">
              <div v-if="it.bodyFormat === 'HTML'" class="in-html" v-html="it.body" />
              <pre v-else class="in-text">{{ it.body }}</pre>
            </div>
          </div>
        </template>
        <template v-else>
          <div v-if="openedInbound.bodyHtml" class="in-html" v-html="openedInbound.bodyHtml" />
          <pre v-else class="in-text">{{ openedInbound.bodyText }}</pre>
        </template>
        <template v-if="openedInbound.attachments?.length">
          <el-divider />
          <h4 class="side-title">{{ t('emails.attachments') }}</h4>
          <div class="chips">
            <el-tag v-for="a in openedInbound.attachments" :key="a.id" type="info">
              {{ a.fileName }} · {{ humanSize(Number(a.fileSize)) }}
            </el-tag>
          </div>
        </template>
      </template>

      <template v-else>
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
        <!-- Clears the unread marks of this view only — the button sits above
             this list, so it does what this list shows. -->
        <el-button v-if="isInboundView" :loading="markingAll" @click="markAllRead">
          {{ t('emails.markAllRead') }}
        </el-button>
        <el-button v-if="folder === 'suppressions' && canSuppress" @click="openSuppress">
          {{ t('emails.addSuppression') }}
        </el-button>
      </div>

      <!-- ------------------------ inbox / starred / archive / junk / trash -->
      <template v-if="isInboundView">
        <!-- Spam is the host's verdict, shown read-only as a safety net: the
             mis-flagged customer inquiry is the one mail worth finding here. -->
        <el-alert
          v-if="folder === 'junk'"
          type="info"
          :closable="false"
          show-icon
          class="junk-note"
        >
          {{ t('emails.junkNote') }}
        </el-alert>
        <el-table
          :data="inbound"
          v-loading="loading"
          class="clickable"
          :row-class-name="inboundRowClass"
          @row-click="openInbound"
        >
          <el-table-column v-if="folder !== 'junk'" width="44">
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
                <!-- One row per conversation: this is how many messages it
                     holds. Opening the row shows the whole exchange. -->
                <span v-if="Number(row.threadCount) > 1" class="tcount">
                  {{ row.threadCount }}
                </span>
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
      <el-radio-group v-model="sentView" size="small" class="sent-toggle" @change="onSentViewChange">
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

      <!-- Cursor paging: 上一页 / 下一页 only, no page numbers. A jump to
           page 40 has no meaning when pages are positions in a list that
           grows at the top — Gmail's pager for the same reason. -->
      <div v-if="isInboundView && (total > 0 || cursorStack.length)" class="pager keyset">
        <span class="sub">{{ t('emails.totalMails', { n: total }) }}</span>
        <el-button size="small" :disabled="!cursorStack.length" @click="prevPage">
          {{ t('emails.prevPage') }}
        </el-button>
        <el-button size="small" :disabled="!nextCursor" @click="nextPage">
          {{ t('emails.nextPage') }}
        </el-button>
      </div>
      <el-pagination
        v-else-if="folder === 'sent' || folder === 'attention'"
        v-model:current-page="page"
        :page-size="pageSize"
        :total="total"
        layout="total, prev, pager, next"
        class="pager"
        @current-change="onPageChange"
      />
      </template>
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
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter, type LocationQuery } from 'vue-router'
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
  // Messages in this conversation; the list shows one row per conversation.
  threadCount?: number
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
const canWrite = computed(() => auth.can('mail:email:write'))
const canSuppress = computed(() => auth.can('mail:suppression:write'))
const isAdmin = computed(() => auth.can('iam:role:write'))

const folders = [
  { key: 'inbox' },
  { key: 'starred' },
  { key: 'drafts' },
  { key: 'sent' },
  { key: 'attention' },
  { key: 'archive' },
  { key: 'junk' },
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
  junk: 'JUNK',
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
// Where the next inbound page starts; empty means this is the last one.
const nextCursor = ref('')
const syncing = ref(false)
const markingAll = ref(false)
// What the server last said went wrong with this mailbox, empty when healthy.
const syncError = ref('')
// The mail being read full-page. Set from the URL, never directly: opening a
// mail is a navigation, so refresh reopens it and back returns to the list.
const openedInbound = ref<InboundMail | null>(null)

// ---------------------------------------------------------------- URL state
// The address bar is the source of truth for where the person is: folder,
// page, search, sent-list toggle and the opened mail all live in the query.
// Every click routes through pushState; the route watcher is the only thing
// that loads data. That single direction is what makes refresh keep the
// position and the browser's back button behave.
const route = useRoute()
const router = useRouter()

interface UrlState {
  folder: string
  page: number
  q: string
  sent: 'erp' | 'mailbox'
  mail: string
  // Where an inbound list page starts. Opaque server token; empty is the
  // first page. Offset paging (page) still drives sent/attention.
  cursor: string
}

// What the screen currently shows. null until the first applyRoute, so the
// initial navigation always loads.
let applied: UrlState | null = null

const FOLDER_KEYS = new Set(['inbox', 'starred', 'drafts', 'sent', 'attention', 'archive', 'junk', 'trash', 'suppressions'])

function parseQuery(q: LocationQuery): UrlState {
  const one = (v: unknown) => (Array.isArray(v) ? String(v[0] ?? '') : v == null ? '' : String(v))
  const f = one(q.folder)
  const p = Number(one(q.page))
  return {
    folder: FOLDER_KEYS.has(f) ? f : 'inbox',
    page: Number.isInteger(p) && p > 1 ? p : 1,
    q: one(q.q),
    sent: one(q.sent) === 'mailbox' ? 'mailbox' : 'erp',
    mail: /^\d+$/.test(one(q.mail)) ? one(q.mail) : '',
    cursor: one(q.c),
  }
}

// Defaults stay out of the address bar: /emails, not /emails?folder=inbox&page=1.
function toQuery(s: UrlState): Record<string, string> {
  const query: Record<string, string> = {}
  if (s.folder !== 'inbox') query.folder = s.folder
  if (s.page > 1) query.page = String(s.page)
  if (s.q) query.q = s.q
  if (s.folder === 'sent' && s.sent !== 'erp') query.sent = s.sent
  if (s.mail) query.mail = s.mail
  if (s.cursor) query.c = s.cursor
  return query
}

// The cursors of the pages walked through to reach this one, newest last.
// Keyset paging knows how to go forward, not back, so 上一页 replays the
// cursor it came from. Kept in history state rather than a component ref so
// it survives a refresh and so back/forward each restore the stack as it
// stood on that entry.
const cursorStack = ref<string[]>([])

function stackFromHistory(): string[] {
  const st = (history.state as { mailStack?: unknown } | null)?.mailStack
  return Array.isArray(st) ? (st as string[]) : []
}

function pushState(over: Partial<UrlState>, stack?: string[]) {
  const cur = applied ?? parseQuery(route.query)
  const next = { ...cur, ...over }
  // A page number and a cursor are two answers to the same question; setting
  // one has to clear the other or a stale cursor would survive a search.
  if (over.cursor === undefined && (over.folder !== undefined || over.q !== undefined || over.sent !== undefined || over.page !== undefined)) {
    next.cursor = ''
  }
  // Navigating to where we already are is a plain refresh, not a navigation:
  // pushing an identical route would be silently dropped by the router.
  if (applied && JSON.stringify(toQuery(next)) === JSON.stringify(toQuery(cur))) {
    load()
    return
  }
  router.push({ query: toQuery(next), state: { mailStack: stack ?? [] } })
}

// The one place the URL turns into screen state. Loads only what changed:
// arriving on a mail link fetches both the list underneath and the mail, but
// closing the mail afterwards refetches neither.
function applyRoute() {
  if (locked.value !== false) return
  if (route.path !== '/emails') return
  const s = parseQuery(route.query)
  const prev = applied
  applied = s
  cursorStack.value = stackFromHistory()
  folder.value = s.folder
  page.value = s.page
  keyword.value = s.q
  sentView.value = s.sent
  if (
    !prev ||
    prev.folder !== s.folder ||
    prev.page !== s.page ||
    prev.q !== s.q ||
    prev.sent !== s.sent ||
    prev.cursor !== s.cursor
  ) {
    load()
  }
  if (!prev || prev.mail !== s.mail) {
    if (s.mail) {
      openDetail(s.mail)
    } else {
      openedInbound.value = null
      threadItems.value = []
    }
  }
}

watch(() => route.query, applyRoute)

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
    // Through the router, not history.replaceState: the router must agree the
    // query is empty or the URL-state watcher would keep seeing oauth params.
    await router.replace({ query: {} })
    if (oauthResult === 'ok') {
      ElMessage.success(t('mailbox.googleOk', { email: boundEmail }))
      try {
        const resp = await http.post('/mailbox/verify', { secret: '' })
        const data = resp.data.data as { token: string }
        localStorage.setItem('mailUnlock', data.token)
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
    localStorage.removeItem('mailUnlock')
    locked.value = true
  }
}

function init() {
  applied = null
  applyRoute()
  checkSyncHealth()
  // Opening the mailbox pulls once rather than waiting up to two minutes for
  // the next poll. Somebody who just told a customer "resend it" opens this
  // page to look, and "already up to date" is the answer they need it to be.
  syncOnOpen()
  refreshAttentionCount()
  loadDraftCount()
  refreshUnread()
  // Ask once, from the mailbox itself — this is the page whose news the
  // desktop notification carries, so the browser's prompt makes sense here.
  // Shell.vue does the actual notifying, and only when the person is away.
  if ('Notification' in window && Notification.permission === 'default') {
    Notification.requestPermission()
  }
}

// New mail is pushed over the same SSE stream the rest of the app uses: the
// IMAP sync stores it, publishes a hint, and every open tab of the owner's
// hears it here. The re-fetch goes through the normal API, so this is only
// ever "go and look", never data.
onUnmounted(
  onLive((e) => {
    if (e.type !== 'mail.inbound') return
    // Not while reading a mail: yanking the list from under the detail page
    // would be invisible, and the unread badge covers the news.
    if (folder.value === 'inbox' && !openedInbound.value) {
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
  // Clear the box too, or clicking the current folder with an unsearched
  // keyword sitting in it would silently search for it.
  keyword.value = ''
  pushState({ folder: key, page: 1, q: '', mail: '' })
}

function reload() {
  pushState({ page: 1, q: keyword.value, mail: '' })
}

function onSentViewChange() {
  pushState({ sent: sentView.value, page: 1, mail: '' })
}

function onPageChange(p: number) {
  pushState({ page: p })
}

// Inbound lists page by cursor: forward hands back the token the server
// returned, back replays the one this page was reached with. Both are
// navigations, so the address bar and the browser's own buttons stay honest.
function nextPage() {
  if (!nextCursor.value) return
  pushState({ cursor: nextCursor.value, mail: '' }, [...cursorStack.value, applied?.cursor ?? ''])
}

function prevPage() {
  const stack = cursorStack.value
  if (!stack.length) return
  pushState({ cursor: stack[stack.length - 1], mail: '' }, stack.slice(0, -1))
}

function backToList() {
  pushState({ mail: '' })
}

async function load() {
  loading.value = true
  try {
    if (isInboundView.value) {
      const d = await get<{
        mails: InboundMail[]
        meta: { total: string }
        unreadCount: number
        nextCursor: string
      }>('/inbound-mails', {
        page_size: pageSize,
        keyword: keyword.value,
        view: INBOUND_VIEWS[folder.value],
        cursor: applied?.cursor ?? '',
      })
      inbound.value = d.mails ?? []
      total.value = Number(d.meta?.total ?? 0)
      unreadCount.value = Number(d.unreadCount ?? 0)
      nextCursor.value = d.nextCursor ?? ''
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

// A row click is a navigation; the route watcher does the fetching.
function openInbound(row: InboundMail) {
  pushState({ mail: row.id })
}

// Fetches the mail named in the URL. Opening marks it read server-side; the
// row (when the list is loaded) and the badge follow.
async function openDetail(id: string) {
  try {
    const d = await get<{ mail: InboundMail }>(`/inbound-mails/${id}`)
    openedInbound.value = d.mail
    const row = inbound.value.find((r) => r.id === id)
    if (row && !row.isRead) {
      row.isRead = true
      unreadCount.value = Math.max(0, unreadCount.value - 1)
    }
    loadThread(d.mail)
  } catch {
    // A dead link — deleted mail, somebody else's id — falls back to the
    // list rather than a blank page.
    pushState({ mail: '' })
  }
}

// The conversation around it, fetched after the mail itself is already on
// screen. Only a real exchange (more than this one message) switches the
// page into thread mode; the opened mail arrives expanded, history
// collapsed to one line each.
async function loadThread(mail: InboundMail) {
  threadItems.value = []
  expandedThread.value = new Set()
  if (!mail.threadKey) return
  try {
    const tr = await get<{ items: ThreadItem[] }>('/mail-threads', { key: mail.threadKey })
    if ((tr.items ?? []).length > 1) {
      threadItems.value = tr.items
      expandedThread.value = new Set([`IN:${mail.id}`])
    }
  } catch {
    /* the single-mail view already covers the failure */
  }
}

interface ThreadItem {
  direction: string
  id: string
  subject: string
  body: string
  bodyFormat: string
  counterparty: string
  who: string
  at: string
}
const threadItems = ref<ThreadItem[]>([])
const expandedThread = ref<Set<string>>(new Set())

function isThreadOpen(it: ThreadItem) {
  return expandedThread.value.has(`${it.direction}:${it.id}`)
}

function toggleThreadItem(it: ThreadItem) {
  const k = `${it.direction}:${it.id}`
  const next = new Set(expandedThread.value)
  if (next.has(k)) {
    next.delete(k)
  } else {
    next.add(k)
  }
  expandedThread.value = next
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
    // Whole conversation, like every other action on a list row: the row's
    // star reads as "starred if any message is", so unstarring has to clear
    // them all or the star would come straight back on the next load.
    await post(`/inbound-mails/${row.id}/mark`, {
      starred: row.isStarred,
      wholeThread: true,
    })
  } catch {
    row.isStarred = !row.isStarred
  }
  if (folder.value === 'starred' && !row.isStarred) load()
}

// The detail-page actions: mark unread, archive/unarchive, trash/restore.
// Each navigates back to the list and reloads — the mail just left this view.
//
// Applied to the whole conversation, because that is what this page shows and
// what the list row stands for: archiving here has to move the exchange, not
// leave it in the inbox one message lighter.
async function markOpened(flags: Record<string, boolean>) {
  if (!openedInbound.value) return
  await post(`/inbound-mails/${openedInbound.value.id}/mark`, {
    ...flags,
    wholeThread: true,
  })
  pushState({ mail: '' })
  load()
  refreshUnread()
}

// Permanent deletion, trash only. The confirm spells out the asymmetry that
// makes this safe to offer: the ERP copy dies, the mail host's original does
// not — the sync is one-way and nothing in the ERP can reach the real mailbox.
async function purgeOpened() {
  if (!openedInbound.value) return
  await ElMessageBox.confirm(t('emails.purgeHint'), t('emails.purge'), {
    type: 'warning',
    confirmButtonText: t('emails.purge'),
  })
  // Whole conversation, matching the trash list's one-row-per-conversation.
  await del(`/inbound-mails/${openedInbound.value.id}?whole_thread=true`)
  ElMessage.success(t('emails.purged'))
  pushState({ mail: '' })
  load()
}

// Reply and forward open the composer over the detail page — cancelling it
// lands back on the mail, not on the list. The composer prefills
// recipient/subject/quote; the server does the threading (reply) and carries
// the original's attachments (forward).
async function replyToInbound() {
  if (!openedInbound.value) return
  composing.value = true
  await nextTick()
  composer.value?.openReply(openedInbound.value)
}

async function forwardInbound() {
  if (!openedInbound.value) return
  composing.value = true
  await nextTick()
  composer.value?.openForward(openedInbound.value)
}

// Marks the current view read. Asked about first: unread is a to-do list, and
// clearing it wholesale cannot be undone mail by mail afterwards.
async function markAllRead() {
  await ElMessageBox.confirm(
    t('emails.markAllReadHint', { f: t(`emails.folders.${folder.value}`) }),
    t('emails.markAllRead'),
    { type: 'warning' },
  )
  markingAll.value = true
  try {
    const d = await post<{ marked: number }>(
      `/inbound-mails/mark-view-read?view=${INBOUND_VIEWS[folder.value]}`,
    )
    ElMessage.success(t('emails.markedAllRead', { n: d.marked ?? 0 }))
    load()
    refreshUnread()
  } finally {
    markingAll.value = false
  }
}

// Reads the account's recorded health. The credential can be dead while the
// unlock token is still valid — one is "may this browser see the mailbox",
// the other is "does the mailbox still answer" — so the gate letting somebody
// in says nothing about whether mail is still arriving.
async function checkSyncHealth() {
  try {
    const d = await get<{ account: { lastError: string } }>('/my-mail-account')
    syncError.value = d.account?.lastError ?? ''
  } catch {
    /* the banner is a courtesy; its absence must not break the page */
  }
}

// A quiet pull on open: no spinner, no toast. A failure shows up as the
// banner, which is where a persistent problem belongs — not in a toast that
// disappears before it is read.
async function syncOnOpen() {
  try {
    const d = await post<{ fetched: number; detail: string }>('/mailbox/sync')
    if (d.detail) {
      syncError.value = d.detail
      return
    }
    syncError.value = ''
    if ((d.fetched ?? 0) > 0 && !openedInbound.value) load()
  } catch {
    /* the poller keeps trying; checkSyncHealth reports what it finds */
  }
}

// The one repair that fits in a button: sign in to the mailbox again. For an
// OAuth account this is the Google round trip; for a password account the
// gate is the place to retype the code, so this locks and shows it.
async function reauth() {
  try {
    const d = await get<{ account: { authKind: string } }>('/my-mail-account')
    if (d.account?.authKind === 'OAUTH') {
      // Full-page departure, same as the gate: popups get blocked, and
      // Google's page is where the person should see themselves go.
      const r = await get<{ url: string }>('/oauth/google/start')
      window.location.href = r.url
      return
    }
  } catch {
    /* fall through to the gate, which can handle either kind */
  }
  await lockMailbox()
}

async function syncNow() {
  syncing.value = true
  try {
    const d = await post<{ fetched: number; detail: string }>('/mailbox/sync')
    if (d.detail) {
      syncError.value = d.detail
      ElMessage({ type: 'error', message: d.detail, duration: 0, showClose: true })
    } else {
      syncError.value = ''
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
  refreshAttentionCount()
  loadDraftCount()
  // A mail sent from inside another mail stays there, Gmail-style: the reply
  // appears in the conversation underneath. A fresh compose goes to the sent
  // folder to watch the delivery.
  if (openedInbound.value) {
    loadThread(openedInbound.value)
    return
  }
  pushState({ folder: 'sent', page: 1, q: '', sent: 'erp', mail: '' })
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
.pager.keyset {
  display: flex;
  align-items: center;
  gap: 10px;
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
.tcount {
  margin-left: 5px;
  font-size: 12px;
  font-weight: 400;
  color: var(--el-text-color-secondary);
}
:deep(.unread-row) {
  font-weight: 500;
}
.detail-top {
  margin-bottom: 8px;
}
.back-btn {
  padding-left: 0;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.in-subject {
  margin: 0 0 10px;
  font-size: 20px;
}
.in-meta {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.in-actions {
  margin-top: 10px;
}
.junk-note {
  margin-bottom: 12px;
}
.sync-error {
  margin-bottom: 14px;
}
.sync-error-body {
  display: flex;
  align-items: center;
  gap: 12px;
}
.thread-count {
  margin-bottom: 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.thread-item {
  margin-bottom: 6px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 6px;
  overflow: hidden;
}
.thread-item.out {
  background: var(--el-fill-color-lighter);
}
.thread-head {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 8px 10px;
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  font-size: 13px;
}
.thread-head:hover {
  background: var(--el-fill-color-light);
}
.thread-body {
  padding: 4px 12px 12px;
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
