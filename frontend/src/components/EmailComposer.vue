<template>
  <el-dialog
    :model-value="modelValue"
    :title="t('emails.compose')"
    width="1000px"
    top="4vh"
    :close-on-click-modal="false"
    :before-close="onBeforeClose"
    @update:model-value="close"
  >
    <!-- Stated once, plainly, where the person sending can see it: which of
         the two promises this send makes. Separate = nobody sees anybody
         else; merged = everybody sees everybody, on purpose. -->
    <el-alert
      :type="form.sendMode === 'MERGED' ? 'warning' : 'info'"
      :closable="false"
      class="privacy"
      show-icon
    >
      {{ form.sendMode === 'MERGED' ? t('emails.privacyNoteMerged') : t('emails.privacyNote') }}
    </el-alert>
    <!-- Two different promises, so two different sentences. A forward that
         carries the original's files and one that carries the original itself
         look identical in this composer — same empty body, same Fwd: subject
         — and the only place the difference can be stated is here. -->
    <el-alert
      v-if="replyCtx.forwardInboundId !== '0'"
      type="info"
      :closable="false"
      class="privacy"
      show-icon
    >
      {{
        replyCtx.forwardAsAttachment
          ? t('emails.forwardCarriesEml')
          : t('emails.forwardCarries')
      }}
    </el-alert>

    <el-form label-width="88px" class="compose-form">
      <el-form-item :label="t('emails.sendModeLabel')">
        <div class="body-box">
          <el-radio-group v-model="form.sendMode">
            <el-radio value="SEPARATE">{{ t('emails.modeSeparate') }}</el-radio>
            <el-radio value="MERGED">{{ t('emails.modeMerged') }}</el-radio>
          </el-radio-group>
          <div class="var-hint">
            {{ form.sendMode === 'MERGED' ? t('emails.modeMergedHint') : t('emails.modeSeparateHint') }}
          </div>
        </div>
      </el-form-item>

      <el-form-item :label="t('emails.recipients')">
        <RecipientField v-model="selected" />
      </el-form-item>

      <el-form-item v-if="form.sendMode === 'MERGED'" :label="t('emails.ccLabel')">
        <RecipientField v-model="ccSelected" />
      </el-form-item>

      <!-- Blind copies. Only in merged mode, like CC: in separate mode every
           recipient already gets their own copy, so a blind list would just
           mean one person receiving the mail N times. -->
      <el-form-item v-if="form.sendMode === 'MERGED'" :label="t('emails.bccLabel')">
        <div class="body-box">
          <RecipientField v-model="bccSelected" />
          <div class="var-hint">{{ t('emails.bccHint') }}</div>
        </div>
      </el-form-item>

      <el-form-item :label="t('emails.subject')">
        <el-input v-model="form.subject" :placeholder="t('emails.subjectPlaceholder')" />
      </el-form-item>

      <el-form-item :label="t('emails.body')">
        <div class="body-box">
          <div class="var-bar">
            <el-radio-group v-model="form.format" size="small" @change="onFormatChange">
              <el-radio-button value="HTML">{{ t('emails.rich') }}</el-radio-button>
              <el-radio-button value="TEXT">{{ t('emails.plain') }}</el-radio-button>
            </el-radio-group>
            <span class="grow" />
            <span v-if="form.sendMode === 'MERGED'" class="var-hint">
              {{ t('emails.mergedNoVars') }}
            </span>
            <template v-if="form.format === 'TEXT' && form.sendMode !== 'MERGED'">
              <span class="var-hint">{{ t('emails.insertVariable') }}</span>
              <el-button
                v-for="v in VARIABLES"
                :key="v"
                size="small"
                link
                type="primary"
                @click="insertVariable(v)"
              >
                {{ t(`emails.vars.${v}`) }}
              </el-button>
            </template>
          </div>

          <MailEditor
            v-if="form.format === 'HTML'"
            v-model="form.body"
            :placeholder="t('emails.bodyPlaceholder')"
            @insert-variable="variablePickerOpen = true"
          />
          <el-input
            v-else
            ref="bodyInput"
            v-model="form.body"
            type="textarea"
            :rows="9"
            :placeholder="t('emails.bodyPlaceholder')"
          />
          <!-- The quoted original, folded behind Gmail's ··· trimmer. It
               lives in its own region rather than inside the editor, which
               is what makes the fold reversible: the boundary between what
               you wrote and what you are quoting never has to be guessed.
               Part of the outgoing mail either way, open or shut. -->
          <template v-if="quoted">
            <button
              type="button"
              class="quote-trim"
              :class="{ on: quoteOpen }"
              :title="t(quoteOpen ? 'emails.hideQuoted' : 'emails.showQuoted')"
              @click="quoteOpen = !quoteOpen"
            >···</button>
            <!-- Editable while open, so the quote can be trimmed by hand.
                 Deliberately not v-html-bound: re-rendering on every
                 keystroke would drop the caret back to the start. -->
            <div
              v-show="quoteOpen"
              ref="quoteBox"
              class="quote-box"
              :class="{ plain: form.format === 'TEXT' }"
              contenteditable="true"
              @input="onQuoteInput"
            />
          </template>
        </div>
      </el-form-item>

      <el-form-item :label="t('emails.attachments')">
        <div class="body-box">
          <div class="attach-bar">
            <el-upload :show-file-list="false" :before-upload="uploadAttachment" multiple>
              <el-button size="small">{{ t('emails.addAttachment') }}</el-button>
            </el-upload>
            <span v-if="attachments.length" class="var-hint">
              {{ t('emails.attachmentTotal', { n: attachments.length, s: humanSize(totalBytes) }) }}
            </span>
            <span v-else class="var-hint">{{ t('emails.noAttachments') }}</span>
          </div>
          <div v-if="attachments.length" class="chips">
            <el-tag
              v-for="(a, i) in attachments"
              :key="a.fileKey"
              closable
              type="info"
              @close="attachments.splice(i, 1)"
            >
              {{ a.fileName }} · {{ humanSize(a.size) }}
            </el-tag>
          </div>
          <!-- The cost that is invisible while composing: a file is sent once
               per recipient, so it multiplies by the size of the list. -->
          <div v-if="egressWarning" class="egress">{{ egressWarning }}</div>
        </div>
      </el-form-item>

      <el-form-item :label="t('emails.signature')">
        <el-select v-model="form.signatureId" clearable style="width: 320px">
          <el-option :label="t('emails.noSignature')" :value="'0'" />
          <el-option
            v-for="s in signatures"
            :key="s.id"
            :label="s.ownerType === 'TENANT' ? `${s.name}（${t('emails.shared')}）` : s.name"
            :value="s.id"
          />
        </el-select>
      </el-form-item>
    </el-form>

    <!-- Preview is not optional decoration: an unresolved variable is only
         obvious when somebody sees the gap where the name should be. Send
         runs it automatically when it has not been run by hand, and an
         unclean result blocks the send with this panel open. -->
    <el-card v-if="preview" shadow="never" class="preview">
      <div class="preview-head">
        <strong>{{ t('emails.previewFor', { n: previewName }) }}</strong>
        <el-button link type="primary" @click="preview = null">{{ t('emails.closePreview') }}</el-button>
      </div>
      <div class="preview-subject">{{ preview.subject }}</div>
      <!-- Sanitised server-side before it was stored, and again before it was
           returned here; this is the same markup the recipient will get. -->
      <div v-if="preview.bodyFormat === 'HTML'" class="preview-html" v-html="preview.body" />
      <pre v-else class="preview-body">{{ preview.body }}</pre>
      <div v-if="preview.bodyFormat === 'HTML' && preview.bodyText" class="alt">
        <div class="alt-head">{{ t('emails.textAlternative') }}</div>
        <pre class="preview-body">{{ preview.bodyText }}</pre>
      </div>
      <el-alert
        v-if="preview.missingVariables?.length"
        type="warning"
        :closable="false"
        show-icon
        class="miss"
      >
        {{ t('emails.missingVars', { v: preview.missingVariables.join('、') }) }}
      </el-alert>
    </el-card>

    <template #footer>
      <span class="foot">
        <!-- Called with explicit parens: bare @click hands the handler a
             MouseEvent, which is how this button once emitted a truthy value
             and told the parent to stay open. -->
        <el-button @click="requestClose()">{{ common('cancel') }}</el-button>
        <el-button :loading="savingDraft" @click="saveDraft">
          {{ t('emails.saveDraft') }}
        </el-button>
        <el-button :disabled="!canPreview" :loading="previewing" @click="doPreview">
          {{ t('emails.preview') }}
        </el-button>
        <!-- Send and schedule sit together because they are one decision made
             twice a day: the same mail, now or at a better hour. Splitting
             them into separate places would hide the second behind a menu
             nobody opens. -->
        <el-button-group>
          <el-button
            type="primary"
            :disabled="!canPreview"
            :loading="sending"
            @click="doSend"
          >
            {{ t('emails.send', { n: selected.length }) }}
          </el-button>
          <el-button
            type="primary"
            :disabled="!canPreview || sending"
            :title="t('emails.scheduleSend')"
            @click="openSchedule"
          >
            <el-icon><Clock /></el-icon>
          </el-button>
        </el-button-group>
      </span>
    </template>
  </el-dialog>

  <!-- Scheduling asks two questions, and the second one is the point: a
       time without a zone is ambiguous the moment the customer is not in
       yours. Both the chosen moment and its local reading are shown back. -->
  <el-dialog
    v-model="scheduleOpen"
    :title="t('emails.scheduleTitle')"
    width="520px"
    append-to-body
  >
    <el-form label-width="96px">
      <el-form-item :label="t('emails.scheduleQuick')">
        <el-space wrap>
          <el-button
            v-for="p in quickPicks"
            :key="p.key"
            size="small"
            plain
            @click="applyQuick(p)"
          >
            {{ p.label }}
          </el-button>
        </el-space>
      </el-form-item>
      <el-form-item :label="t('emails.scheduleZone')">
        <el-select v-model="scheduleZone" filterable style="width: 100%">
          <el-option
            v-for="z in zones"
            :key="z.value"
            :value="z.value"
            :label="z.label"
          />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('emails.scheduleWhen')">
        <el-date-picker
          v-model="scheduleLocal"
          type="datetime"
          format="YYYY-MM-DD HH:mm"
          value-format="YYYY-MM-DD HH:mm"
          :placeholder="t('emails.scheduleWhen')"
          style="width: 100%"
        />
      </el-form-item>
      <el-alert v-if="scheduleError" type="error" :closable="false" show-icon>
        {{ scheduleError }}
      </el-alert>
      <el-alert v-else-if="schedulePreview" type="info" :closable="false" show-icon>
        {{ schedulePreview }}
      </el-alert>
    </el-form>
    <template #footer>
      <el-button @click="scheduleOpen = false">{{ common('cancel') }}</el-button>
      <el-button
        type="primary"
        :disabled="!scheduleAt || !!scheduleError"
        :loading="sending"
        @click="doSchedule"
      >
        {{ t('emails.scheduleConfirm') }}
      </el-button>
    </template>
  </el-dialog>

    <el-dialog v-model="variablePickerOpen" :title="t('emails.insertVariable')" width="420px" append-to-body>
      <div class="var-list">
        <el-button v-for="v in VARIABLES" :key="v" @click="insertVariableRich(v)">
          {{ t(`emails.vars.${v}`) }}
        </el-button>
      </div>
    </el-dialog>
</template>

<script setup lang="ts">
import { computed, nextTick, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Clock } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import MailEditor from './MailEditor.vue'
import RecipientField, { type Recipient } from './RecipientField.vue'
import {
  daysToWeekday,
  localZone,
  offsetLabel,
  wallAt,
  wallClockIn,
  zonedToInstant,
} from '../lib/zonedtime'

interface Signature {
  id: string
  ownerType: string
  name: string
  content: string
  isDefault: boolean
}
interface Preview {
  subject: string
  body: string
  bodyText: string
  bodyFormat: string
  missingVariables: string[]
}
interface PendingFile {
  fileName: string
  fileKey: string
  size: number
}
interface Skipped {
  email: string
  name: string
  reason: string
}
interface CreateResult {
  campaignNo: string
  queued: number
  suppressed: Skipped[]
  needsReview: Skipped[]
}

// The whole substitution vocabulary. It mirrors app.KnownVariables() in the
// notification service; the two are small and fixed, and the frontend needs
// its own copy anyway to label them in three languages.
const VARIABLES = [
  'contact_name',
  'contact_first_name',
  'company_name',
  'my_name',
  'my_title',
  'my_email',
  'my_phone',
] as const

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [boolean]; sent: []; saved: [] }>()

const { t, locale } = useI18n()
const common = (k: string) => t(`common.${k}`)

const form = reactive({
  subject: '',
  body: '',
  signatureId: '0',
  format: 'HTML',
  // SEPARATE: one copy per recipient, invisible to each other. MERGED: one
  // shared mail where To and CC are open — the mode for writing to the three
  // people at one customer, not for campaigns.
  sendMode: 'SEPARATE',
})
const attachments = ref<PendingFile[]>([])
const ccSelected = ref<Recipient[]>([])
const bccSelected = ref<Recipient[]>([])
// Set when this compose answers or forwards a mail from the inbox. '0' means
// a fresh mail. The server takes threading headers (reply) or the original's
// attachments (forward) from the referenced message.
const replyCtx = reactive({
  replyToInboundId: '0',
  forwardInboundId: '0',
  forwardAsAttachment: false,
})
const variablePickerOpen = ref(false)
const savingDraft = ref(false)
// Set once a draft has been saved or opened, so later saves update that row
// rather than leaving a trail of near-identical drafts behind.
const draftId = ref('0')
const signatures = ref<Signature[]>([])
const selected = ref<Recipient[]>([])
const preview = ref<Preview | null>(null)
const previewing = ref(false)
const sending = ref(false)
const bodyInput = ref()

// The content as it stood the last time walking away cost nothing: a fresh
// compose, a draft just loaded, or a draft just saved. Cancel compares against
// this rather than against emptiness, so reopening a saved draft and closing
// it again does not claim work is about to be lost when it is not.
let baseline = ''

// A reply's quoted original. Held apart from the editor for the whole life of
// the compose — that separation is the feature: because the two are never
// merged, the trimmer can fold as well as unfold, and nothing has to guess
// where your text ends and the quote begins. Always part of what is sent;
// see fullBody.
const quoted = ref('')
const quoteOpen = ref(false)
const quoteBox = ref<HTMLElement>()

// What actually goes out: the typed text plus the quote, open or shut. Every
// consumer of the body — preview, send, draft — reads this, never form.body,
// so folding the quote can never silently drop it from the mail.
function fullBody() {
  return quoted.value ? form.body + quoted.value : form.body
}

// The quote is written into the box once, when it opens, and read back on
// edit. Binding it reactively instead would rewrite the DOM under the cursor.
watch([quoteOpen, () => form.format], ([open]) => {
  if (!open) return
  nextTick(() => fillQuoteBox())
})

function fillQuoteBox() {
  const el = quoteBox.value
  if (!el) return
  if (form.format === 'TEXT') {
    el.textContent = quoted.value
  } else {
    el.innerHTML = quoted.value
  }
}

function onQuoteInput() {
  const el = quoteBox.value
  if (!el) return
  quoted.value = form.format === 'TEXT' ? el.innerText : el.innerHTML
}

// The rich editor leaves markup behind even when the box looks empty, so an
// emptiness test has to read the text rather than the tags. Reads the full
// body: dirty-tracking must not change when the quote expands into the editor.
function plainBody() {
  return fullBody().replace(/<[^>]*>/g, '').replace(/&nbsp;/g, ' ').trim()
}

function signature() {
  return JSON.stringify([
    form.subject.trim(),
    plainBody(),
    form.format,
    form.signatureId,
    form.sendMode,
    selected.value.map((r) => r.email).sort(),
    ccSelected.value.map((r) => r.email).sort(),
    attachments.value.map((a) => a.fileKey).sort(),
    replyCtx.replyToInboundId,
    replyCtx.forwardInboundId,
    replyCtx.forwardAsAttachment,
  ])
}

function hasContent() {
  return (
    form.subject.trim() !== '' ||
    plainBody() !== '' ||
    selected.value.length > 0 ||
    attachments.value.length > 0
  )
}

function markClean() {
  baseline = signature()
}

const isDirty = () => signature() !== baseline

const canPreview = computed(
  () => selected.value.length > 0 && form.subject.trim() !== '' && form.body.trim() !== '',
)
const previewName = computed(() =>
  form.sendMode === 'MERGED' ? t('emails.allRecipients') : (selected.value[0]?.name ?? ''),
)

// Any edit invalidates the preview: a stale one would vouch for text that is
// no longer what would be sent.
const totalBytes = computed(() => attachments.value.reduce((n, a) => n + a.size, 0))

// 500 recipients x 5 MB is 2.5 GB through the provider. Nothing on this
// screen otherwise hints at that, so it is spelled out once it matters.
const egressWarning = computed(() => {
  const n = selected.value.length
  if (!n || totalBytes.value === 0) return ''
  const total = totalBytes.value * n
  if (total < 200 * 1024 * 1024) return ''
  return t('emails.egressWarning', { n, s: humanSize(total) })
})

watch(
  () => [form.subject, form.body, form.signatureId, form.format, form.sendMode, selected.value.length, ccSelected.value.length, attachments.value.length],
  () => {
    preview.value = null
  },
)

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return
    loadSignatures()
  },
)

// Opening the composer with a draft restores everything that was saved.
async function openDraft(id: string) {
  const d = await get<{ draft: any }>(`/email-drafts/${id}`)
  const draft = d.draft
  draftId.value = draft.id
  form.subject = draft.subject ?? ''
  form.body = draft.body ?? ''
  form.format = draft.bodyFormat || 'HTML'
  form.signatureId = draft.signatureId ?? '0'
  form.sendMode = draft.sendMode === 'MERGED' ? 'MERGED' : 'SEPARATE'
  selected.value = draft.recipients ?? []
  ccSelected.value = draft.cc ?? []
  bccSelected.value = draft.bcc ?? []
  replyCtx.replyToInboundId = draft.replyToInboundId ?? '0'
  replyCtx.forwardInboundId = draft.forwardInboundId ?? '0'
  replyCtx.forwardAsAttachment = draft.forwardAsAttachment ?? false
  attachments.value = (draft.attachments ?? []).map((a: any) => ({
    fileName: a.fileName,
    fileKey: a.fileKey,
    size: 0,
  }))
  preview.value = null
  // Everything on screen is already stored, so closing now loses nothing.
  markClean()
}
// What reply and forward need from the mail being answered.
interface QuotedMail {
  id: string
  fromEmail: string
  fromName?: string
  subject?: string
  bodyHtml?: string
  bodyText?: string
}

function escapeText(s: string) {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/\n/g, '<br>')
}

// The original message, quoted the way every client quotes: attribution line,
// then the body in a blockquote. The HTML came sanitised from the server; the
// text fallback is escaped here before it becomes markup.
function quotedBlock(mail: QuotedMail) {
  const inner = mail.bodyHtml || `<p>${escapeText(mail.bodyText || '')}</p>`
  const who = escapeText(mail.fromName || mail.fromEmail)
  return (
    `<p>${who} &lt;${escapeText(mail.fromEmail)}&gt; ${escapeText(t('emails.wrote'))}</p>` +
    `<blockquote>${inner}</blockquote>`
  )
}

function prefixSubject(subject: string, tag: string) {
  const s = (subject || '').trim()
  return s.toLowerCase().startsWith(tag.toLowerCase()) ? s : `${tag} ${s}`
}

// Prefills a reply: the sender becomes the recipient, the subject gains Re:,
// the original is quoted (collapsed, Gmail-style), and the server threads it
// via replyToInboundId. markClean afterwards — the prefill is machine work,
// losing it costs nothing.
function openReply(mail: QuotedMail) {
  reset()
  replyCtx.replyToInboundId = mail.id
  selected.value = [{ name: mail.fromName || '', email: mail.fromEmail }]
  form.subject = prefixSubject(mail.subject || '', 'Re:')
  form.format = 'HTML'
  form.body = '<p><br></p>'
  quoted.value = quotedBlock(mail)
  markClean()
}

// Prefills a forward: no recipient yet, subject gains Fwd:, the original is
// quoted (collapsed) and its attachments travel along server-side.
function openForward(mail: QuotedMail) {
  reset()
  replyCtx.forwardInboundId = mail.id
  form.subject = prefixSubject(mail.subject || '', 'Fwd:')
  form.format = 'HTML'
  form.body = '<p><br></p>'
  quoted.value = quotedBlock(mail)
  markClean()
}

// The same forward, sent as the original message rather than as a quote of
// it. Deliberately no quoted block: the point is that the recipient opens the
// real thing, and pasting our rendering above it invites them to read that
// instead — which is the version whose headers cannot be trusted.
function openForwardAsAttachment(mail: QuotedMail) {
  reset()
  replyCtx.forwardInboundId = mail.id
  replyCtx.forwardAsAttachment = true
  form.subject = prefixSubject(mail.subject || '', 'Fwd:')
  form.format = 'HTML'
  form.body = '<p><br></p>'
  markClean()
}

defineExpose({ openDraft, openReply, openForward, openForwardAsAttachment })

// Contacts arrive from the address book with more on them than the wire
// message declares, and protojson refuses unknown fields outright. Every path
// that leaves this component shapes its recipients through here.
function asProto(r: Recipient) {
  return {
    contactId: r.contactId,
    name: r.name,
    email: r.email,
    customerId: r.customerId,
    customerName: r.customerName,
  }
}

// Everything the composer holds, in one place. Saving used to send the words
// and drop the rest - send mode, CC, what the mail was answering - so a saved
// reply came back as a plain new mail with the same text.
function draftPayload() {
  return {
    id: draftId.value,
    subject: form.subject,
    body: fullBody(),
    bodyFormat: form.format,
    signatureId: form.signatureId,
    kind: 'MARKETING',
    sendMode: form.sendMode,
    cc: form.sendMode === 'MERGED' ? ccSelected.value.map(asProto) : [],
    bcc: form.sendMode === 'MERGED' ? bccSelected.value.map(asProto) : [],
    replyToInboundId: replyCtx.replyToInboundId,
    forwardInboundId: replyCtx.forwardInboundId,
    forwardAsAttachment: replyCtx.forwardAsAttachment,
    recipients: selected.value.map(asProto),
    attachments: attachments.value.map((a) => ({
      fileName: a.fileName,
      fileKey: a.fileKey,
    })),
  }
}

async function saveDraft() {
  savingDraft.value = true
  try {
    const r = await post<{ id: string }>('/email-drafts', draftPayload())
    draftId.value = r.id
    ElMessage.success(t('emails.draftSaved'))
    emit('saved')
    close(false)
  } finally {
    savingDraft.value = false
  }
}

function reset() {
  draftId.value = '0'
  form.subject = ''
  form.body = ''
  quoted.value = ''
  quoteOpen.value = false
  form.format = 'HTML'
  form.signatureId = '0'
  form.sendMode = 'SEPARATE'
  attachments.value = []
  selected.value = []
  ccSelected.value = []
  bccSelected.value = []
  replyCtx.replyToInboundId = '0'
  replyCtx.forwardInboundId = '0'
  replyCtx.forwardAsAttachment = false
  preview.value = null
  scheduleLocal.value = ''
  scheduleOpen.value = false
  markClean()
}

async function loadSignatures() {
  const d = await get<{ signatures: Signature[] }>('/email-signatures')
  signatures.value = d.signatures ?? []
  const def = signatures.value.find((s) => s.isDefault)
  if (def) form.signatureId = def.id
  // Applying the tenant default is the app's doing, not the user's, so it must
  // not register as unsaved work. Guarded because this resolves asynchronously
  // and must never overwrite a baseline once somebody has started writing.
  if (!hasContent()) markClean()
}

// Inserts at the cursor rather than appending, so a variable can be dropped
// into the middle of a sentence that is already written.
function insertVariable(name: string) {
  const tag = `{{${name}}}`
  const el = bodyInput.value?.textarea as HTMLTextAreaElement | undefined
  if (!el) {
    form.body += tag
    return
  }
  const start = el.selectionStart ?? form.body.length
  const end = el.selectionEnd ?? start
  form.body = form.body.slice(0, start) + tag + form.body.slice(end)
  // nextTick, not requestAnimationFrame: the caret has to be placed after
  // Vue has written the new value into the DOM. A frame callback can run
  // first, in which case the range is set on the old text and the later
  // update drops the caret back to position 0 — so whatever you typed next
  // landed at the very start of the message.
  nextTick(() => {
    el.focus()
    el.setSelectionRange(start + tag.length, start + tag.length)
  })
}

// Switching format clears what somebody wrote, because silently rewriting
// their markup into text (or the reverse) loses work in a way that is hard to
// undo. Starting clean is blunter but honest.
//
// The quoted original is not their writing, though, and it used to go with
// it: switching format made the ··· vanish along with the mail being replied
// to. Converting it costs nothing — nobody typed it, so nothing of theirs is
// mangled — and losing the original is far worse than losing its formatting.
function onFormatChange() {
  const to = form.format
  if (form.body.trim() === '') {
    convertQuote(to)
    return
  }
  ElMessageBox.confirm(t('emails.formatSwitchHint'), t('emails.formatSwitch'), {
    type: 'warning',
  })
    .then(() => {
      form.body = ''
      convertQuote(to)
    })
    .catch(() => {
      form.format = to === 'HTML' ? 'TEXT' : 'HTML'
    })
}

// Rewrites the quote into the format now in force. HTML → text keeps the
// words and drops the tags; text → HTML re-wraps them in a blockquote. The
// round trip loses the original's markup, which is the same bargain the body
// makes, but the quote itself always survives.
function convertQuote(to: string) {
  if (!quoted.value) return
  if (to === 'TEXT') {
    const text = quoted.value
      .replace(/<br\s*\/?>/gi, '\n')
      .replace(/<\/(p|div|blockquote|tr|li|h[1-6])>/gi, '\n')
      .replace(/<[^>]*>/g, '')
      .replace(/&nbsp;/g, ' ')
      .replace(/&lt;/g, '<')
      .replace(/&gt;/g, '>')
      .replace(/&amp;/g, '&')
      .replace(/\n{3,}/g, '\n\n')
      .trim()
    // Prefixed the way every plain-text client quotes, so the recipient sees
    // a quote rather than an unmarked wall of somebody else's words.
    quoted.value = '\n\n' + text.split('\n').map((l) => `> ${l}`).join('\n')
    return
  }
  quoted.value = `<blockquote>${escapeText(quoted.value.replace(/^> ?/gm, '').trim())}</blockquote>`
}

function insertVariableRich(name: string) {
  variablePickerOpen.value = false
  if (form.sendMode === 'MERGED') {
    // One shared body cannot carry a per-recipient value; better refused at
    // the button than bounced by the server at send time.
    ElMessage.warning(t('emails.mergedNoVars'))
    return
  }
  document.execCommand('insertText', false, `{{${name}}}`)
}

function humanSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

// Bytes go browser-to-bucket; only the key comes back. The file is not
// registered yet — that happens inside the transaction that creates the send,
// so a message can never go out before its attachment row exists.
async function uploadAttachment(file: File) {
  try {
    const p = await post<{ fileKey: string; uploadUrl: string }>('/email-attachments/presign', {
      fileName: file.name,
    })
    const put = await fetch(p.uploadUrl, {
      method: 'PUT',
      body: file,
      headers: { 'Content-Type': file.type || 'application/octet-stream' },
    })
    if (!put.ok) throw new Error(String(put.status))
    attachments.value.push({ fileName: file.name, fileKey: p.fileKey, size: file.size })
  } catch {
    ElMessage.error(t('emails.uploadFailed', { n: file.name }))
  }
  return false
}

async function doPreview() {
  // Merged mode previews against nobody: whatever fails to resolve is
  // exactly the set of per-recipient variables, which one shared body
  // cannot carry — surfacing them here is what blocks the send.
  const r =
    form.sendMode === 'MERGED'
      ? { contactId: '0', name: '', email: '', customerId: '0', customerName: '' }
      : selected.value[0]
  previewing.value = true
  try {
    preview.value = await post<Preview>('/email-campaigns/preview', {
      subject: form.subject,
      body: fullBody(),
      bodyFormat: form.format,
      signatureId: form.signatureId,
      recipient: {
        contactId: r.contactId,
        name: r.name,
        email: r.email,
        customerId: r.customerId,
        customerName: r.customerName,
      },
    })
  } finally {
    previewing.value = false
  }
}

// ------------------------------------------------------------ scheduling
//
// The zone is the point of this dialog. "Nine in the morning" is a promise
// about the customer's morning, not ours, and the two are eight hours and a
// daylight-saving rule apart — so the person picks the zone they are thinking
// in and sees, before committing, what that means on their own clock.

const scheduleOpen = ref(false)
const scheduleZone = ref(localZone())
const scheduleLocal = ref('')

const zones = computed(() => {
  const mine = localZone()
  const list = [
    mine,
    'Asia/Shanghai',
    'Asia/Tokyo',
    'Asia/Dubai',
    'Europe/London',
    'Europe/Madrid',
    'Europe/Berlin',
    'America/New_York',
    'America/Chicago',
    'America/Los_Angeles',
    'America/Sao_Paulo',
    'America/Mexico_City',
    'Australia/Sydney',
  ].filter((z, i, all) => all.indexOf(z) === i)
  return list.map((z) => ({
    value: z,
    label: `${z} (${offsetLabel(z)})${z === mine ? ' · ' + t('emails.scheduleHere') : ''}`,
  }))
})

// Presets in the chosen zone, not ours: "tomorrow morning" means the
// customer's tomorrow, which is the whole reason the zone was picked.
const quickPicks = computed(() => {
  const nowThere = wallClockIn(new Date(), scheduleZone.value)
  return [
    { key: 'tonight', label: t('emails.quickTonight'), wall: wallAt(nowThere, 0, 18) },
    { key: 'tomorrow', label: t('emails.quickTomorrow'), wall: wallAt(nowThere, 1, 8) },
    {
      key: 'monday',
      label: t('emails.quickMonday'),
      wall: wallAt(nowThere, daysToWeekday(nowThere, 1), 8),
    },
  ]
})

const scheduleAt = computed(() => {
  if (!scheduleLocal.value) return ''
  const at = zonedToInstant(scheduleLocal.value, scheduleZone.value)
  return at ? at.toISOString() : ''
})

const scheduleError = computed(() => {
  if (!scheduleAt.value) return ''
  return new Date(scheduleAt.value).getTime() <= Date.now() ? t('emails.schedulePast') : ''
})

// What the chosen moment reads as here. Stated even when the zone is our own —
// the sentence is the confirmation, and a preview that vanishes for local
// sends would make the local case the untrustworthy one.
const schedulePreview = computed(() => {
  if (!scheduleAt.value) return ''
  return t('emails.scheduleReads', { when: readableLocal(new Date(scheduleAt.value)) })
})

function readableLocal(at: Date) {
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(at)
}

function applyQuick(p: { wall: string }) {
  scheduleLocal.value = p.wall
}

function openSchedule() {
  if (!scheduleLocal.value) {
    applyQuick(quickPicks.value[1])
  }
  scheduleOpen.value = true
}

async function doSchedule() {
  if (!scheduleAt.value || scheduleError.value) return
  if (!(await readyToSend())) return
  scheduleOpen.value = false
  await submitSend(scheduleAt.value)
}

// Guards the whole click-to-confirm stretch, not just the network call:
// without it a double click stacks two confirm dialogs, and confirming both
// sends the mail twice.
let sendFlowBusy = false

async function doSend() {
  if (sendFlowBusy) return
  sendFlowBusy = true
  try {
    await doSendInner()
  } catch {
    /* the person cancelled the confirm — not an error */
  } finally {
    sendFlowBusy = false
  }
}

async function doSendInner() {
  if (!(await readyToSend())) return
  const headCount =
    selected.value.length +
    (form.sendMode === 'MERGED' ? ccSelected.value.length + bccSelected.value.length : 0)
  await ElMessageBox.confirm(
    form.sendMode === 'MERGED'
      ? t('emails.confirmSendMerged', { n: headCount })
      : t('emails.confirmSend', { n: headCount }),
    t('emails.confirmSendTitle'),
    { type: 'warning' },
  )
  await submitSend('')
}

// The preview-before-send rule stands — "Dear {{contact_name}}," is not
// recoverable — but the machine can run the preview itself. Only a preview
// that fails to resolve stops the send, and then the panel says why.
//
// A scheduled send needs this every bit as much as an immediate one: the mail
// goes out unattended, so nobody will be watching when the gap where the name
// should be reaches the customer.
async function readyToSend() {
  if (!preview.value) {
    await doPreview()
    if (!preview.value) return false
  }
  if (preview.value.missingVariables?.length) {
    ElMessage.warning(t('emails.missingVars', { v: preview.value.missingVariables.join('、') }))
    return false
  }
  return true
}

// One path for both buttons: an empty `at` means now. Sending and scheduling
// differ by a timestamp and nothing else — the queue holds every mail either
// way, and only the moment it becomes due changes.
async function submitSend(at: string) {
  sending.value = true
  try {
    // A draft that is being sent goes through its own endpoint so the row is
    // removed once the mail is queued — otherwise every sent draft would
    // linger in the folder as a duplicate of something already gone out.
    if (draftId.value !== '0') {
      await post('/email-drafts', draftPayload())
      const wrapped = await post<{ result: CreateResult }>(
        `/email-drafts/${draftId.value}/send${at ? `?scheduled_at=${encodeURIComponent(at)}` : ''}`,
      )
      reportResult(wrapped.result, at)
      emit('sent')
      close(false)
      return
    }
    const res = await post<CreateResult>('/email-campaigns', {
      subject: form.subject,
      body: fullBody(),
      bodyFormat: form.format,
      signatureId: form.signatureId,
      kind: 'MARKETING',
      sendMode: form.sendMode,
      cc: form.sendMode === 'MERGED' ? ccSelected.value.map(asProto) : [],
      bcc: form.sendMode === 'MERGED' ? bccSelected.value.map(asProto) : [],
      replyToInboundId: replyCtx.replyToInboundId,
      forwardInboundId: replyCtx.forwardInboundId,
      forwardAsAttachment: replyCtx.forwardAsAttachment,
      scheduledAt: at,
      attachments: attachments.value.map((a) => ({
        fileName: a.fileName,
        fileKey: a.fileKey,
      })),
      recipients: selected.value.map(asProto),
    })
    reportResult(res, at)
    emit('sent')
    close(false)
  } finally {
    sending.value = false
  }
}

// A caller who asked for 40 and got 37 queued is told which three did not go
// and why, rather than being left to notice the number later.
function reportResult(res: CreateResult, at = '') {
  const skipped = (res.suppressed?.length ?? 0) + (res.needsReview?.length ?? 0)
  if (skipped === 0) {
    if (at) {
      ElMessage.success(
        t('emails.scheduledAll', { n: res.queued, when: readableLocal(new Date(at)) }),
      )
      return
    }
    ElMessage.success(t('emails.queuedAll', { no: res.campaignNo, n: res.queued }))
    return
  }
  const lines: string[] = []
  res.suppressed?.forEach((s) =>
    lines.push(`${s.email || s.name} — ${t(`emails.skipReasons.${s.reason}`, s.reason)}`),
  )
  res.needsReview?.forEach((s) => lines.push(`${s.email || s.name} — ${s.reason}`))
  ElMessageBox.alert(lines.join('\n'), t('emails.partialTitle', { n: res.queued, s: skipped }), {
    customClass: 'pre-alert',
  })
}

// Every way out of the composer funnels through here — the footer button, the
// X, and Escape — so the guard cannot be walked around.
//
// Wired to :before-close rather than @close. The close event fires while the
// dialog is already closing, and emitting from inside it wedges the dialog
// half-open; before-close is the hook designed to run first and decide.
// Closing resets. Two bugs lived here: the cancel button was wired as
// `@click="close"`, so Vue handed it the MouseEvent — a truthy value — and
// the composer emitted "stay open" instead of closing. And state was only
// reset when opening, so cancelling a draft left draftId set: the next fresh
// compose skipped its reset, opened showing the previous draft, and sending
// it would have deleted that draft.
function close(v: boolean) {
  if (!v) reset()
  emit('update:modelValue', v)
}

// Cancelling bins whatever is in the composer. Ask first — but only when there
// is something to lose, because confirming an untouched form is pure friction.
async function confirmDiscard() {
  if (!isDirty()) return true
  try {
    await ElMessageBox.confirm(t('emails.discardHint'), t('emails.discardTitle'), {
      confirmButtonText: t('emails.discard'),
      cancelButtonText: t('emails.keepEditing'),
      type: 'warning',
    })
    return true
  } catch {
    // Rejects on "keep editing" and on dismissing the confirm itself.
    return false
  }
}

async function requestClose() {
  if (await confirmDiscard()) close(false)
}

// Element Plus routes the ✕, Escape and modal clicks through before-close, but
// not the parent setting the prop false. Guarding here therefore catches every
// way the user can dismiss the dialog while leaving the programmatic exits —
// sending, saving a draft — free to close unconditionally.
async function onBeforeClose(done: () => void) {
  if (await confirmDiscard()) done()
}
</script>

<style scoped>
/* The composer follows the same rhythm as the list it opens over. Element's
   default form spacing is built for settings pages, where every row is a
   separate decision; a compose window is one continuous act and reads better
   tightened up. */
.privacy {
  margin-bottom: 12px;
  border-radius: var(--mail-radius);
}
.compose-form {
  margin-bottom: 4px;
}
.compose-form :deep(.el-form-item) {
  margin-bottom: 14px;
}
.compose-form :deep(.el-form-item__label) {
  font-size: var(--mail-sub);
  color: var(--el-text-color-secondary);
}
/* The footer is a bar, not a row of equals: send is the act, the rest are
   ways out of it. */
.foot {
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: flex-end;
}
.recip-box,
.body-box {
  width: 100%;
}
.recip-bar,
.var-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.grow {
  flex: 1;
}
.count,
.var-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.tagm {
  margin-left: 6px;
}
.preview {
  margin-top: 6px;
  background: var(--el-fill-color-lighter);
}
.preview-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
  font-size: 13px;
}
.preview-subject {
  padding-bottom: 8px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  font-weight: 600;
}
.preview-body {
  margin: 10px 0 0;
  max-height: 220px;
  overflow: auto;
  white-space: pre-wrap;
  font-family: inherit;
  font-size: 13px;
  line-height: 1.6;
}
.miss {
  margin-top: 10px;
}
.attach-bar {
  display: flex;
  align-items: center;
  gap: 10px;
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}
.egress {
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-color-warning);
}
.quote-trim {
  display: inline-block;
  margin-top: 8px;
  padding: 0 10px;
  line-height: 16px;
  font-size: 14px;
  letter-spacing: 2px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color);
  border: 1px solid var(--el-border-color);
  border-radius: 9px;
  cursor: pointer;
}
.quote-trim:hover {
  background: var(--el-fill-color-dark);
}
.quote-trim.on {
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
}
.quote-box {
  margin-top: 8px;
  padding: 8px 12px;
  max-height: 260px;
  overflow: auto;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
  background: var(--el-fill-color-lighter);
  font-size: 13px;
  line-height: 1.6;
  outline: none;
}
.quote-box :deep(blockquote) {
  margin: 0 0 0 8px;
  padding-left: 10px;
  border-left: 2px solid var(--el-border-color);
  color: var(--el-text-color-regular);
}
.quote-box :deep(img) {
  max-width: 100%;
}
.quote-box.plain {
  white-space: pre-wrap;
  font-family: inherit;
}
.preview-html {
  margin-top: 10px;
  max-height: 260px;
  overflow: auto;
  font-size: 13px;
  line-height: 1.6;
}
.preview-html :deep(img) {
  max-width: 100%;
}
.alt {
  margin-top: 12px;
  padding-top: 10px;
  border-top: 1px dashed var(--el-border-color);
}
.alt-head {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 4px;
}
.var-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
