<template>
  <!-- Signatures live inside the mailbox rather than in a settings page of
       their own. A signature is not a system setting somebody configures once
       and forgets: it is part of writing mail, and it belongs beside the thing
       it appears on. The mailbox host settings moved here for the same reason.

       append-to-body because this dialog opens from the mailbox rail, which
       sits inside a flex layout that would otherwise clip it. -->
  <el-dialog
    :model-value="modelValue"
    :title="t('signatures.title')"
    width="min(900px, 94vw)"
    top="5vh"
    append-to-body
    @update:model-value="$emit('update:modelValue', $event)"
    @open="load"
  >
    <div class="sig-head">
      <span class="head-note">{{ t('signatures.subtitle') }}</span>
      <span class="grow" />
      <el-button type="primary" @click="openCreate">{{ t('signatures.create') }}</el-button>
    </div>

    <el-card shadow="never">
      <el-table :data="rows" v-loading="loading">
        <el-table-column :label="t('signatures.name')" min-width="200">
          <template #default="{ row }">
            <div class="prod">{{ row.name }}</div>
            <div class="sub">
              <!-- Whose block this is matters: a shared one changes what
                   every colleague sends, a personal one only your own mail. -->
              {{ row.ownerType === 'TENANT' ? t('signatures.shared') : t('signatures.personal') }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('signatures.content')" min-width="380">
          <template #default="{ row }">
            <!-- v-html on an HTML block, and only on an HTML block. The
                 content was put through the same whitelist the outgoing mail
                 uses (SanitizeHTML, on every write) — no script, no event
                 handlers, no javascript: URLs survive it. A TEXT block goes
                 through <pre>, because rendering it as markup would turn a
                 sign-off someone typed with angle brackets into markup. -->
            <div
              v-if="row.bodyFormat === 'HTML'"
              class="sig-preview sig-html"
              v-html="row.content"
            />
            <pre v-else class="sig-preview">{{ row.content }}</pre>
          </template>
        </el-table-column>
        <el-table-column :label="t('signatures.default')" width="110">
          <template #default="{ row }">
            <el-tag v-if="row.isDefault" size="small" type="success" effect="plain">
              {{ t('signatures.isDefault') }}
            </el-tag>
            <span v-else class="sub">—</span>
          </template>
        </el-table-column>
        <el-table-column :label="common('actions')" width="140" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">{{ common('edit') }}</el-button>
            <el-button link type="danger" @click="remove(row)">{{ common('delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && rows.length === 0" :description="t('signatures.empty')" />
    </el-card>

    <!-- One dialog for both. The fields are identical, and two dialogs drift:
         a field added to the create form and forgotten in the edit form is
         the usual way an editor stops being able to change something. -->
    <el-dialog
      v-model="open"
      :title="editingId ? t('signatures.edit') : t('signatures.create')"
      width="620px"
    >
      <el-form label-width="90px">
        <el-form-item :label="t('signatures.name')">
          <el-input v-model="form.name" :placeholder="t('signatures.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('signatures.scope')">
          <el-radio-group v-model="form.ownerType">
            <el-radio value="EMPLOYEE">{{ t('signatures.personal') }}</el-radio>
            <el-radio value="TENANT">{{ t('signatures.shared') }}</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="t('signatures.content')">
          <div class="body-box">
            <!-- The signature is appended to the body before rendering, so
                 its variables resolve in the same pass. That is why the same
                 tags are offered here as in the composer. -->
            <div class="var-bar">
              <span class="var-hint">{{ t('emails.insertVariable') }}</span>
              <el-button
                v-for="v in SIGNATURE_VARIABLES"
                :key="v"
                size="small"
                link
                type="primary"
                @click="insertVariable(v)"
              >
                {{ t(`emails.vars.${v}`) }}
              </el-button>
            </div>
            <!-- The same editor the composer uses, so a logo gets in here by
                 the same two routes: upload a file, or paste the address of
                 one already on the web. A plain textarea could only ever
                 produce text, which is why signatures had no pictures. -->
            <MailEditor
              ref="editor"
              v-model="form.content"
              :placeholder="t('signatures.contentPlaceholder')"
              @image-inserted="checkLogoSize"
            />
            <!-- Said before anything goes wrong, not only after: the person
                 choosing a logo file deserves the target sizes while the
                 picker is still open. -->
            <div class="var-hint">{{ t('signatures.sizeHint') }}</div>
          </div>
        </el-form-item>
        <el-form-item>
          <el-checkbox v-model="form.isDefault">{{ t('signatures.setDefault') }}</el-checkbox>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="open = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ common('save') }}</el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import MailEditor from '../components/MailEditor.vue'
import { del, get, post, put } from '../api'

interface Signature {
  id: string
  ownerType: string
  ownerId: string
  name: string
  content: string
  bodyFormat: string
  isDefault: boolean
}

// Only the sender's own details make sense in a signature: the recipient's
// name belongs in the greeting, not the sign-off.
const SIGNATURE_VARIABLES = ['my_name', 'my_title', 'my_email', 'my_phone'] as const

defineProps<{ modelValue: boolean }>()
defineEmits<{ 'update:modelValue': [boolean] }>()

const { t } = useI18n()
const common = (k: string) => t(`common.${k}`)

const rows = ref<Signature[]>([])
const loading = ref(false)
const open = ref(false)
const saving = ref(false)
// Empty means "creating". Holding the id rather than a boolean is what lets
// save() pick the verb without a second flag to keep in step.
const editingId = ref('')
const editor = ref<InstanceType<typeof MailEditor>>()
const form = reactive({ name: '', ownerType: 'EMPLOYEE', content: '', isDefault: false })

async function load() {
  loading.value = true
  try {
    const d = await get<{ signatures: Signature[] }>('/email-signatures')
    rows.value = d.signatures ?? []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = ''
  form.name = ''
  form.ownerType = 'EMPLOYEE'
  form.content = ''
  form.isDefault = false
  open.value = true
}

function openEdit(row: Signature) {
  editingId.value = row.id
  form.name = row.name
  form.ownerType = row.ownerType
  form.content = row.bodyFormat === 'HTML' ? row.content : textToHTML(row.content)
  form.isDefault = row.isDefault
  open.value = true
}

// A signature written before the editor existed is plain text, and the rich
// editor reads its value as markup. Handing it the raw string would collapse
// every line break — a two-line sign-off opening as one line, and saving it
// back that way. So the newlines become <br> and the angle brackets are
// escaped, which is exactly what the server does when it joins a text
// signature onto an HTML body.
function textToHTML(s: string) {
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML.replace(/\r?\n/g, '<br>')
}

function insertVariable(name: string) {
  editor.value?.insertText(`{{${name}}}`)
}

// A signature rides on every mail its owner ever sends, so an oversized
// logo is not one slow load — it is a permanent tax on every recipient, and
// heavy image-to-text ratios are a spam-score classic. Gmail's guidance
// (the requirement's reference): 300–400 wide by 70–100 tall. Warned, not
// blocked: a marketing banner in a campaign signature can be a deliberate
// choice, and the person on the spot knows things a threshold does not.
const LOGO_WARN_WIDTH = 1000
const LOGO_WARN_HEIGHT = 200
const LOGO_WARN_BYTES = 200 * 1024

function checkLogoSize(info: { width: number; height: number; bytes: number }) {
  if (
    info.width > LOGO_WARN_WIDTH ||
    info.height > LOGO_WARN_HEIGHT ||
    info.bytes > LOGO_WARN_BYTES
  ) {
    ElMessage.warning(t('signatures.imageTooBig', { w: info.width, h: info.height }))
  }
}

async function save() {
  saving.value = true
  try {
    // HTML unconditionally: the editor cannot produce anything else, and a
    // block saved as TEXT would have its markup escaped on the way out — the
    // logo arriving at the customer as the literal text of an <img> tag.
    // The server sanitises it against the outgoing-mail whitelist regardless.
    //
    // Editing an old TEXT block therefore converts it to HTML. One way, and
    // deliberate: it renders identically, and leaving it TEXT would mean the
    // editor could not add the one thing it was opened to add.
    const body = { ...form, bodyFormat: 'HTML' }
    if (editingId.value) {
      await put(`/email-signatures/${editingId.value}`, body)
    } else {
      await post('/email-signatures', body)
    }
    ElMessage.success(t('signatures.saved'))
    open.value = false
    load()
  } finally {
    saving.value = false
  }
}

async function remove(row: Signature) {
  await ElMessageBox.confirm(t('signatures.deleteHint', { n: row.name }), common('delete'), {
    type: 'warning',
  })
  await del(`/email-signatures/${row.id}`)
  ElMessage.success(t('signatures.deleted'))
  load()
}
</script>

<style scoped>
.sig-head {
  display: flex;
  align-items: baseline;
  gap: 14px;
  margin-bottom: 12px;
}
.grow {
  flex: 1;
}
.head-note,
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.prod {
  font-weight: 500;
}
.sig-preview {
  margin: 0;
  white-space: pre-wrap;
  font-family: inherit;
  font-size: 12px;
  line-height: 1.5;
  color: var(--el-text-color-regular);
}
.sig-html {
  white-space: normal;
}
/* A row is a summary, not a rendering surface. Without a ceiling a signature
   carrying a banner sets the height of the whole table. */
.sig-html :deep(img) {
  max-width: 200px;
  max-height: 60px;
  object-fit: contain;
}
.body-box {
  width: 100%;
}
.var-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.var-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
