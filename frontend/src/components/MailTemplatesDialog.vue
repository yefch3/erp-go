<template>
  <!-- The template library (C5). Same home as signatures and for the same
       reason: a phrase you reuse is part of writing mail, not a system
       setting. append-to-body because the mailbox rail sits inside a flex
       layout that would otherwise clip the dialog. -->
  <el-dialog
    :model-value="modelValue"
    :title="t('templates.title')"
    width="min(960px, 94vw)"
    top="5vh"
    append-to-body
    @update:model-value="$emit('update:modelValue', $event)"
    @open="load"
  >
    <div class="tpl-head">
      <span class="head-note">{{ t('templates.subtitle') }}</span>
      <span class="grow" />
      <el-button type="primary" @click="openCreate">{{ t('templates.create') }}</el-button>
    </div>

    <el-card shadow="never">
      <el-table :data="rows" v-loading="loading">
        <el-table-column :label="t('templates.name')" min-width="180">
          <template #default="{ row }">
            <div class="prod">
              {{ row.name }}
              <el-tag v-if="row.lang" size="small" effect="plain" class="lang-tag">
                {{ t(`templates.langs.${row.lang}`) }}
              </el-tag>
            </div>
            <div class="sub">
              {{ row.ownerType === 'TENANT' ? t('templates.shared') : t('templates.personal') }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('templates.subject')" min-width="200">
          <template #default="{ row }">
            <span v-if="row.subject">{{ row.subject }}</span>
            <span v-else class="sub">—</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('templates.content')" min-width="320">
          <template #default="{ row }">
            <!-- Same rule as the signature list: HTML was sanitised on every
                 write (SanitizeHTML), TEXT renders as text. -->
            <div
              v-if="row.bodyFormat === 'HTML'"
              class="tpl-preview tpl-html"
              v-html="row.content"
            />
            <pre v-else class="tpl-preview">{{ row.content }}</pre>
          </template>
        </el-table-column>
        <el-table-column :label="common('actions')" width="140" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">{{ common('edit') }}</el-button>
            <el-button link type="danger" @click="remove(row)">{{ common('delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && rows.length === 0" :description="t('templates.empty')" />
    </el-card>

    <!-- One dialog for create and edit, so the two forms cannot drift. -->
    <el-dialog
      v-model="open"
      :title="editingId ? t('templates.edit') : t('templates.create')"
      width="680px"
      append-to-body
    >
      <el-form label-width="90px">
        <el-form-item :label="t('templates.name')">
          <el-input v-model="form.name" :placeholder="t('templates.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('templates.scope')">
          <el-radio-group v-model="form.ownerType">
            <el-radio value="EMPLOYEE">{{ t('templates.personal') }}</el-radio>
            <el-radio value="TENANT">{{ t('templates.shared') }}</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="t('templates.lang')">
          <!-- Which language the CONTENT is written in — a picker filter, so
               the person writing to a Spanish customer is not offered 中文
               话术. Not the UI language, which is the reader's own choice. -->
          <el-radio-group v-model="form.lang">
            <el-radio value="">{{ t('templates.langs.all') }}</el-radio>
            <el-radio value="zh">{{ t('templates.langs.zh') }}</el-radio>
            <el-radio value="en">{{ t('templates.langs.en') }}</el-radio>
            <el-radio value="es">{{ t('templates.langs.es') }}</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="t('templates.subject')">
          <el-input v-model="form.subject" :placeholder="t('templates.subjectPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('templates.content')">
          <div class="body-box">
            <!-- The full vocabulary, contact variables included: a greeting
                 is exactly what a template opens with. They resolve at send
                 time, per recipient, by the same pass a campaign uses. -->
            <div class="var-bar">
              <span class="var-hint">{{ t('emails.insertVariable') }}</span>
              <el-button
                v-for="v in TEMPLATE_VARIABLES"
                :key="v"
                size="small"
                link
                type="primary"
                @click="insertVariable(v)"
              >
                {{ t(`emails.vars.${v}`) }}
              </el-button>
            </div>
            <MailEditor
              ref="editor"
              v-model="form.content"
              :placeholder="t('templates.contentPlaceholder')"
            />
          </div>
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

export interface EmailTemplate {
  id: string
  ownerType: string
  ownerId: string
  name: string
  lang: string
  subject: string
  content: string
  bodyFormat: string
}

// The whole vocabulary (render.go KnownVariables), unlike the signature
// dialog's sender-only subset: a template's first line is usually the
// greeting, which is exactly where the contact variables live.
const TEMPLATE_VARIABLES = [
  'contact_name',
  'contact_first_name',
  'company_name',
  'my_name',
  'my_title',
  'my_email',
  'my_phone',
] as const

defineProps<{ modelValue: boolean }>()
defineEmits<{ 'update:modelValue': [boolean] }>()

const { t } = useI18n()
const common = (k: string) => t(`common.${k}`)

const rows = ref<EmailTemplate[]>([])
const loading = ref(false)
const open = ref(false)
const saving = ref(false)
const editingId = ref('')
const editor = ref<InstanceType<typeof MailEditor>>()
const form = reactive({ name: '', ownerType: 'EMPLOYEE', lang: '', subject: '', content: '' })

async function load() {
  loading.value = true
  try {
    const d = await get<{ templates: EmailTemplate[] }>('/email-templates')
    rows.value = d.templates ?? []
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = ''
  form.name = ''
  form.ownerType = 'EMPLOYEE'
  form.lang = ''
  form.subject = ''
  form.content = ''
  open.value = true
}

function openEdit(row: EmailTemplate) {
  editingId.value = row.id
  form.name = row.name
  form.ownerType = row.ownerType
  form.lang = row.lang
  form.subject = row.subject
  form.content = row.bodyFormat === 'HTML' ? row.content : textToHTML(row.content)
  open.value = true
}

// Same conversion the signature dialog does, for the same reason: the rich
// editor reads its value as markup, and a plain-text template handed over
// raw would have its line breaks collapsed.
function textToHTML(s: string) {
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML.replace(/\r?\n/g, '<br>')
}

function insertVariable(name: string) {
  editor.value?.insertText(`{{${name}}}`)
}

async function save() {
  saving.value = true
  try {
    // HTML unconditionally — the editor cannot produce anything else, and
    // the server sanitises against the outgoing-mail whitelist regardless.
    const body = { ...form, bodyFormat: 'HTML' }
    if (editingId.value) {
      await put(`/email-templates/${editingId.value}`, body)
    } else {
      await post('/email-templates', body)
    }
    ElMessage.success(t('templates.saved'))
    open.value = false
    load()
  } finally {
    saving.value = false
  }
}

async function remove(row: EmailTemplate) {
  await ElMessageBox.confirm(t('templates.deleteHint', { n: row.name }), common('delete'), {
    type: 'warning',
  })
  await del(`/email-templates/${row.id}`)
  ElMessage.success(t('templates.deleted'))
  load()
}
</script>

<style scoped>
.tpl-head {
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
.lang-tag {
  margin-left: 6px;
}
.tpl-preview {
  margin: 0;
  white-space: pre-wrap;
  font-family: inherit;
  font-size: 12px;
  line-height: 1.5;
  color: var(--el-text-color-regular);
  /* A row is a summary. Without a ceiling one long template sets the height
     of the whole table. */
  max-height: 72px;
  overflow: hidden;
}
.tpl-html {
  white-space: normal;
}
.tpl-html :deep(img) {
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
  flex-wrap: wrap;
}
.var-hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
