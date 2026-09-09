<template>
  <div v-if="mail" class="reader">
    <h3 class="subject">{{ mail.subject }}</h3>

    <!-- Both addresses, the way every mail client shows a mail: who it came
         from and who it went to, each with the address under the name. The
         page used to name the sender without their address, which reads as
         half a header — you cannot tell which mailbox it left from. -->
    <div class="head">
      <div class="avatar">{{ initial(mail.senderName) }}</div>
      <div class="head-text">
        <div class="line1">
          <span class="from">{{ mail.senderName }}</span>
          <span v-if="mail.senderEmail" class="dim mono">&lt;{{ mail.senderEmail }}&gt;</span>
          <!-- Sent before the address was stamped on the row, so it is not
               known. Said out loud rather than left blank: a missing address
               on a sent mail looks like a rendering fault, and the previous
               behaviour here — resolving it from today's mailbox binding —
               filled the gap with a confident wrong answer. -->
          <el-tooltip v-else :content="t('reader.fromUnrecordedHint')" placement="top">
            <span class="dim unrecorded">{{ t('reader.fromUnrecorded') }}</span>
          </el-tooltip>
          <span class="dim" :title="stampFull">{{ stamp }}</span>
        </div>
        <div class="line2">
          <span class="dim">{{ t('reader.to') }}</span>
          <span class="to">{{ mail.toName || mail.toEmail }}</span>
          <span class="dim mono">&lt;{{ mail.toEmail }}&gt;</span>
          <span v-if="mail.customerName" class="dim">· {{ mail.customerName }}</span>
        </div>
      </div>
    </div>

    <!-- 对方是否已读.
         Placed above the body because it is what somebody opens a sent mail
         to find out, and stated in three states rather than two. "Not opened"
         and "we were not watching" look identical in the data — both are an
         empty timestamp — and only the first says anything about the
         recipient. Claiming the second as the first would be the system
         inventing a fact about a customer. -->
    <div class="readback">
      <el-tag size="small" :type="statusType(mail.status)" effect="plain">
        {{ t(`emails.statuses.${mail.status}`) }}
      </el-tag>
      <!-- 三档都**不挂 tooltip**：措辞本身已经把话说完了。「可能已打开」
           四个字就是那句提示的意思，「没带追踪」也是；再弹一块解释只是让
           鼠标扫过去时蹦一个气泡。完整时间还在，用浏览器自带的 title。 -->
      <span class="rb-label">{{ t('reader.openedLabel') }}</span>
      <span v-if="mail.openedAt" class="rb-yes" :title="zonedStamp(mail.openedAt)">
        {{ t('emails.maybeOpened') }} · {{ shortTime(mail.openedAt) }}
      </span>
      <span v-else-if="mail.trackingEnabled" class="rb-no">{{ t('emails.noOpenYet') }}</span>
      <span v-else class="rb-off">{{ t('reader.noTracking') }}</span>
    </div>

    <!-- Anything that needs a person is said here, above the mail, because
         somebody opening a stuck message came to find out why. -->
    <el-alert
      v-if="mail.attentionReason"
      type="warning"
      :closable="false"
      show-icon
      class="attn"
    >
      <div>{{ mail.attentionReason }}</div>
      <div v-if="mail.lastError" class="mono err">{{ mail.lastError }}</div>
    </el-alert>

    <!-- The body as the recipient received it. Sanitised server-side on the
         way in, so this is the same markup that was sent - rendering it any
         other way would show a wall of tags instead of the mail. -->
    <!-- Rendered in a sandboxed frame rather than injected here. The sender's
         stylesheet is kept — it is most of how a mail looks like itself — and
         keeping it is only safe because it cannot reach out of that frame.
         See MailBody.vue. -->
    <MailBody v-if="mail.bodyFormat === 'HTML'" class="body" :html="mail.body" />
    <pre v-else class="body text">{{ mail.body }}</pre>

    <div v-if="mail.attachments?.length" class="files">
      <div class="side-title">
        {{ t('reader.attachments', { n: mail.attachments.length }) }}
      </div>
      <div class="chips">
        <el-tag v-for="a in mail.attachments" :key="a.id" type="info" effect="plain">
          {{ a.fileName }} · {{ humanSize(Number(a.fileSize)) }}
        </el-tag>
      </div>
    </div>

  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { shortTime, zonedStamp } from '../lib/zonedtime'
import MailBody from './MailBody.vue'

interface Attachment {
  id: string
  fileName: string
  fileSize: string
  contentType: string
}
export interface Mail {
  id: string
  senderName: string
  senderEmail: string
  toEmail: string
  toName: string
  customerName: string
  subject: string
  body: string
  bodyText: string
  bodyFormat: string
  status: string
  attentionReason: string
  lastError: string
  queuedAt: string
  sentAt: string
  openedAt: string
  // Whether this mail actually carried a tracking pixel. Without it, an empty
  // openedAt means either "watched and nothing happened" or "never watched",
  // and only the first is a statement about the recipient.
  trackingEnabled: boolean
  attachments: Attachment[]
}

const props = defineProps<{ mail: Mail | null }>()
const { t } = useI18n()

const sentInstant = computed(() => props.mail?.sentAt || props.mail?.queuedAt || '')
const stamp = computed(() => shortTime(sentInstant.value))
const stampFull = computed(() => zonedStamp(sentInstant.value))

function initial(name: string) {
  return (name || '?').trim().charAt(0).toUpperCase()
}

function statusType(s: string): 'success' | 'warning' | 'danger' | 'info' {
  if (s === 'DELIVERED' || s === 'ACCEPTED') return 'success'
  if (s === 'QUEUED' || s === 'SENDING') return 'info'
  if (s === 'HARD_BOUNCED' || s === 'COMPLAINED' || s === 'FAILED') return 'danger'
  return 'warning'
}

function humanSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}
</script>

<style scoped>
.reader {
  padding: 2px 2px 20px;
}
.subject {
  margin: 0 0 16px;
  font-size: 19px;
  font-weight: 500;
  line-height: 1.4;
}
.head {
  display: flex;
  gap: 12px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.avatar {
  flex: none;
  width: 38px;
  height: 38px;
  border-radius: 50%;
  background: var(--el-color-primary-light-7);
  color: var(--el-color-primary);
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
}
.head-text {
  min-width: 0;
  flex: 1;
}
.line1 {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 6px;
}
.line2 {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 5px;
}
.from,
.to {
  font-weight: 500;
}
.dim {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
/* Italic, not red: an address nobody wrote down is a gap in the record, not
   a fault the reader has to act on. */
.unrecorded {
  font-style: italic;
  cursor: help;
}
/* A single quiet line, not a card. This is a weak signal and the styling
   should not lend it more weight than it has earned. */
.readback {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin: 14px 0 4px;
  font-size: 13px;
}
.rb-label {
  color: var(--el-text-color-secondary);
}
.rb-yes {
  color: var(--el-color-success);
  font-weight: 500;
  border-bottom: 1px dashed currentColor;
  cursor: help;
}
.rb-no,
.rb-off {
  color: var(--el-text-color-secondary);
  border-bottom: 1px dashed var(--el-border-color);
  cursor: help;
}
.attn {
  margin: 14px 0 0;
}
.err {
  margin-top: 4px;
  font-size: 12px;
}
.body {
  margin: 18px 0 0;
  font-size: 14px;
  line-height: 1.65;
}
.body.text {
  white-space: pre-wrap;
  font-family: inherit;
}
/* The HTML body's own styling now lives inside the frame. What used to be
   here — a 14px font and an ERP-blue link colour — cascaded into every
   received mail, shrinking a sender's display type and repainting their brand
   colour with ours. A link colour is a sender's asset, not our theme. */
.files {
  margin-top: 20px;
  padding-top: 14px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.side-title {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
h4.side-title {
  margin-top: 22px;
}
</style>
