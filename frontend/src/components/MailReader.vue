<template>
  <div v-if="mail" class="reader">
    <h3 class="subject">{{ mail.subject }}</h3>

    <div class="head">
      <div class="avatar">{{ initial(mail.senderName) }}</div>
      <div class="head-text">
        <div class="line1">
          <span class="from">{{ mail.senderName }}</span>
          <span class="dim">{{ t('reader.to') }}</span>
          <span class="to">{{ mail.toName || mail.toEmail }}</span>
          <span class="dim mono">&lt;{{ mail.toEmail }}&gt;</span>
        </div>
        <div class="line2">
          <el-tag size="small" :type="statusType(mail.status)" effect="plain">
            {{ t(`emails.statuses.${mail.status}`) }}
          </el-tag>
          <span class="dim">{{ stamp }}</span>
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
      <span class="rb-label">{{ t('reader.openedLabel') }}</span>
      <el-tooltip
        v-if="mail.openedAt"
        :content="t('emails.openedHint', { at: shortTime(mail.openedAt) })"
        placement="top"
        :show-after="0"
      >
        <span class="rb-yes">{{ t('emails.maybeOpened') }} · {{ shortTime(mail.openedAt) }}</span>
      </el-tooltip>
      <el-tooltip
        v-else-if="mail.trackingEnabled"
        :content="t('reader.noOpenHint')"
        placement="top"
        :show-after="0"
      >
        <span class="rb-no">{{ t('emails.noOpenYet') }}</span>
      </el-tooltip>
      <el-tooltip v-else :content="t('reader.noTrackingHint')" placement="top" :show-after="0">
        <span class="rb-off">{{ t('reader.noTracking') }}</span>
      </el-tooltip>
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
    <div v-if="mail.bodyFormat === 'HTML'" class="body html" v-html="mail.body" />
    <pre v-else class="body text">{{ mail.body }}</pre>

    <!-- The text alternative is folded away: it is what a reader with images
         off actually got, so it is worth being able to check, but it is not
         what you came to read. -->
    <el-collapse v-if="mail.bodyFormat === 'HTML' && mail.bodyText" class="alt">
      <el-collapse-item :title="t('emails.textAlternative')" name="alt">
        <pre class="body text">{{ mail.bodyText }}</pre>
      </el-collapse-item>
    </el-collapse>

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

    <h4 class="side-title">{{ t('emails.history') }}</h4>
    <el-timeline v-if="mail.events?.length">
      <el-timeline-item
        v-for="e in mail.events"
        :key="e.id"
        :timestamp="shortTime(e.occurredAt)"
        :type="e.kind === 'BOUNCE' || e.kind === 'COMPLAINT' ? 'danger' : 'primary'"
      >
        <span>{{ t(`emails.events.${e.kind}`, e.kind) }}</span>
        <el-tag v-if="e.isProxy" size="small" effect="plain" class="tagm">
          {{ t('emails.viaProxy') }}
        </el-tag>
        <div v-if="e.detail" class="dim mono">{{ e.detail }}</div>
      </el-timeline-item>
    </el-timeline>
    <el-empty v-else :description="t('emails.noEvents')" :image-size="50" />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

interface Attachment {
  id: string
  fileName: string
  fileSize: string
  contentType: string
}
interface Event {
  id: string
  kind: string
  occurredAt: string
  isProxy: boolean
  detail: string
}
export interface Mail {
  id: string
  senderName: string
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
  events: Event[]
}

const props = defineProps<{ mail: Mail | null }>()
const { t } = useI18n()

const stamp = computed(() => shortTime(props.mail?.sentAt || props.mail?.queuedAt || ''))

function initial(name: string) {
  return (name || '?').trim().charAt(0).toUpperCase()
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
.body.html :deep(img) {
  max-width: 100%;
}
.body.html :deep(table) {
  max-width: 100%;
}
.body.html :deep(a) {
  color: var(--el-color-primary);
}
.alt {
  margin-top: 18px;
}
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
.tagm {
  margin-left: 6px;
}
</style>
