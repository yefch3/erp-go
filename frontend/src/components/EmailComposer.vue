<template>
  <el-dialog
    :model-value="modelValue"
    :title="t('emails.compose')"
    width="1000px"
    top="4vh"
    @update:model-value="close"
  >
    <!-- Stated once, plainly, where the person sending can see it. The
         one-message-per-recipient guarantee is invisible in the UI otherwise,
         and the whole reason this feature exists is that people have been
         burned by BCC lists leaking. -->
    <el-alert type="info" :closable="false" class="privacy" show-icon>
      {{ t('emails.privacyNote') }}
    </el-alert>

    <el-form label-width="88px" class="compose-form">
      <el-form-item :label="t('emails.recipients')">
        <RecipientField v-model="selected" />
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
            <template v-if="form.format === 'TEXT'">
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

    <!-- Preview is not optional decoration. An unresolved variable is only
         obvious when somebody sees the gap where the name should be, so the
         send button stays disabled until one has been rendered. -->
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

    <el-dialog v-model="variablePickerOpen" :title="t('emails.insertVariable')" width="420px" append-to-body>
      <div class="var-list">
        <el-button v-for="v in VARIABLES" :key="v" @click="insertVariableRich(v)">
          {{ t(`emails.vars.${v}`) }}
        </el-button>
      </div>
    </el-dialog>

    <template #footer>
      <span class="foot">
        <el-button @click="close">{{ common('cancel') }}</el-button>
        <el-button :loading="savingDraft" @click="saveDraft">
          {{ t('emails.saveDraft') }}
        </el-button>
        <el-button :disabled="!canPreview" :loading="previewing" @click="doPreview">
          {{ t('emails.preview') }}
        </el-button>
        <el-button
          type="primary"
          :disabled="!canSend"
          :loading="sending"
          @click="doSend"
        >
          {{ t('emails.send', { n: selected.length }) }}
        </el-button>
      </span>
    </template>
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

const form = reactive({ subject: '', body: '', signatureId: '0', format: 'HTML' })
const attachments = ref<PendingFile[]>([])
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

const canPreview = computed(
  () => selected.value.length > 0 && form.subject.trim() !== '' && form.body.trim() !== '',
)
// Sending needs a preview to have been rendered first, and that preview to
// have resolved cleanly. Sending "Dear {{contact_name}}," is not recoverable.
const canSend = computed(
  () => canPreview.value && preview.value !== null && preview.value.missingVariables.length === 0,
)
const previewName = computed(() => selected.value[0]?.name ?? '')

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
  () => [form.subject, form.body, form.signatureId, form.format, selected.value.length, attachments.value.length],
  () => {
    preview.value = null
  },
)

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return
    // A draft being opened sets its own state right after this fires, so
    // only a fresh compose starts from blank.
    if (draftId.value === '0') reset()
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
}
defineExpose({ openDraft })

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
  attachments.value = []
  selected.value = []
  preview.value = null
}

async function loadSignatures() {
  const d = await get<{ signatures: Signature[] }>('/email-signatures')
  signatures.value = d.signatures ?? []
  const def = signatures.value.find((s) => s.isDefault)
  if (def) form.signatureId = def.id
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
  const r = selected.value[0]
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

async function doSend() {
  await ElMessageBox.confirm(
    t('emails.confirmSend', { n: selected.value.length }),
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
    const res = await post<CreateResult>('/email-campaigns', {
      subject: form.subject,
      body: form.body,
      bodyFormat: form.format,
      signatureId: form.signatureId,
      kind: 'MARKETING',
      attachments: attachments.value.map((a) => ({
        fileName: a.fileName,
        fileKey: a.fileKey,
      })),
      recipients: selected.value.map((r) => ({
        contactId: r.contactId,
        name: r.name,
        email: r.email,
        customerId: r.customerId,
        customerName: r.customerName,
      })),
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

function close(v: boolean) {
  emit('update:modelValue', v)
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
