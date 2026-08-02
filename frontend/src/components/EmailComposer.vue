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
    <el-alert
      v-if="replyCtx.forwardInboundId !== '0'"
      type="info"
      :closable="false"
      class="privacy"
      show-icon
    >
      {{ t('emails.forwardCarries') }}
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
        <el-button
          type="primary"
          :disabled="!canPreview"
          :loading="sending"
          @click="doSend"
        >
          {{ t('emails.send', { n: selected.length }) }}
        </el-button>
      </span>
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
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import MailEditor from './MailEditor.vue'
import RecipientField, { type Recipient } from './RecipientField.vue'

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

const { t } = useI18n()
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
// Set when this compose answers or forwards a mail from the inbox. '0' means
// a fresh mail. The server takes threading headers (reply) or the original's
// attachments (forward) from the referenced message.
const replyCtx = reactive({ replyToInboundId: '0', forwardInboundId: '0' })
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

// The rich editor leaves markup behind even when the box looks empty, so an
// emptiness test has to read the text rather than the tags.
function plainBody() {
  return form.body.replace(/<[^>]*>/g, '').replace(/&nbsp;/g, ' ').trim()
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
  selected.value = draft.recipients ?? []
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
    `<p><br></p><p>${who} &lt;${escapeText(mail.fromEmail)}&gt; ${escapeText(t('emails.wrote'))}</p>` +
    `<blockquote>${inner}</blockquote>`
  )
}

function prefixSubject(subject: string, tag: string) {
  const s = (subject || '').trim()
  return s.toLowerCase().startsWith(tag.toLowerCase()) ? s : `${tag} ${s}`
}

// Prefills a reply: the sender becomes the recipient, the subject gains Re:,
// the original is quoted, and the server threads it via replyToInboundId.
// markClean afterwards — the prefill is machine work, losing it costs nothing.
function openReply(mail: QuotedMail) {
  reset()
  replyCtx.replyToInboundId = mail.id
  selected.value = [{ name: mail.fromName || '', email: mail.fromEmail }]
  form.subject = prefixSubject(mail.subject || '', 'Re:')
  form.format = 'HTML'
  form.body = quotedBlock(mail)
  markClean()
}

// Prefills a forward: no recipient yet, subject gains Fwd:, the original is
// quoted and its attachments travel along server-side.
function openForward(mail: QuotedMail) {
  reset()
  replyCtx.forwardInboundId = mail.id
  form.subject = prefixSubject(mail.subject || '', 'Fwd:')
  form.format = 'HTML'
  form.body = quotedBlock(mail)
  markClean()
}

defineExpose({ openDraft, openReply, openForward })

async function saveDraft() {
  savingDraft.value = true
  try {
    const r = await post<{ id: string }>('/email-drafts', {
      id: draftId.value,
      subject: form.subject,
      body: form.body,
      bodyFormat: form.format,
      signatureId: form.signatureId,
      kind: 'MARKETING',
      recipients: selected.value,
      attachments: attachments.value.map((a) => ({
        fileName: a.fileName,
        fileKey: a.fileKey,
      })),
    })
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
  form.format = 'HTML'
  form.signatureId = '0'
  form.sendMode = 'SEPARATE'
  attachments.value = []
  selected.value = []
  ccSelected.value = []
  replyCtx.replyToInboundId = '0'
  replyCtx.forwardInboundId = '0'
  preview.value = null
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

// Switching format does not attempt to convert: silently rewriting somebody's
// markup into text (or the reverse) loses work in a way that is hard to undo.
// Starting clean is blunter but honest.
function onFormatChange() {
  if (form.body.trim() !== '') {
    ElMessageBox.confirm(t('emails.formatSwitchHint'), t('emails.formatSwitch'), {
      type: 'warning',
    })
      .then(() => {
        form.body = ''
      })
      .catch(() => {
        form.format = form.format === 'HTML' ? 'TEXT' : 'HTML'
      })
  }
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
      body: form.body,
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
  // The preview-before-send rule stands — "Dear {{contact_name}}," is not
  // recoverable — but the machine can run the preview itself. Only a preview
  // that fails to resolve stops the send, and then the panel says why.
  if (!preview.value) {
    await doPreview()
    if (!preview.value) return
  }
  if (preview.value.missingVariables?.length) {
    ElMessage.warning(t('emails.missingVars', { v: preview.value.missingVariables.join('、') }))
    return
  }
  const headCount =
    selected.value.length + (form.sendMode === 'MERGED' ? ccSelected.value.length : 0)
  await ElMessageBox.confirm(
    form.sendMode === 'MERGED'
      ? t('emails.confirmSendMerged', { n: headCount })
      : t('emails.confirmSend', { n: headCount }),
    t('emails.confirmSendTitle'),
    { type: 'warning' },
  )
  sending.value = true
  try {
    // A draft that is being sent goes through its own endpoint so the row is
    // removed once the mail is queued — otherwise every sent draft would
    // linger in the folder as a duplicate of something already gone out.
    if (draftId.value !== '0') {
      await post('/email-drafts', {
        id: draftId.value,
        subject: form.subject,
        body: form.body,
        bodyFormat: form.format,
        signatureId: form.signatureId,
        kind: 'MARKETING',
        recipients: selected.value,
        attachments: attachments.value.map((a) => ({
          fileName: a.fileName,
          fileKey: a.fileKey,
        })),
      })
      const wrapped = await post<{ result: CreateResult }>(
        `/email-drafts/${draftId.value}/send`,
      )
      reportResult(wrapped.result)
      emit('sent')
      close(false)
      return
    }
    const asProto = (r: Recipient) => ({
      contactId: r.contactId,
      name: r.name,
      email: r.email,
      customerId: r.customerId,
      customerName: r.customerName,
    })
    const res = await post<CreateResult>('/email-campaigns', {
      subject: form.subject,
      body: form.body,
      bodyFormat: form.format,
      signatureId: form.signatureId,
      kind: 'MARKETING',
      sendMode: form.sendMode,
      cc: form.sendMode === 'MERGED' ? ccSelected.value.map(asProto) : [],
      replyToInboundId: replyCtx.replyToInboundId,
      forwardInboundId: replyCtx.forwardInboundId,
      attachments: attachments.value.map((a) => ({
        fileName: a.fileName,
        fileKey: a.fileKey,
      })),
      recipients: selected.value.map(asProto),
    })
    reportResult(res)
    emit('sent')
    close(false)
  } finally {
    sending.value = false
  }
}

// A caller who asked for 40 and got 37 queued is told which three did not go
// and why, rather than being left to notice the number later.
function reportResult(res: CreateResult) {
  const skipped = (res.suppressed?.length ?? 0) + (res.needsReview?.length ?? 0)
  if (skipped === 0) {
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
.privacy {
  margin-bottom: 14px;
}
.compose-form {
  margin-bottom: 4px;
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
