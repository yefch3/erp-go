<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ t('inquiryTemplates.eyebrow') }}</div>
        <h1>{{ t('inquiryTemplates.title') }}</h1>
        <p>{{ t('inquiryTemplates.subtitle') }}</p>
      </div>
      <div class="head-actions">
        <el-button @click="router.push('/sales/inquiries')">← {{ t('salesNav.inquiries') }}</el-button>
        <el-button v-if="canWrite" type="primary" @click="openCreate()">{{ t('inquiryTemplates.create') }}</el-button>
      </div>
    </header>

    <section class="panel">
      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column :label="t('inquiryTemplates.colName')" min-width="200">
          <template #default="{ row }">
            {{ row.name }}
            <el-tag v-if="row.isSystem" size="small" effect="plain" type="info">{{ t('inquiryTemplates.systemTag') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="templateCode" :label="t('inquiryTemplates.colCode')" min-width="150" />
        <el-table-column :label="t('inquiryTemplates.colVersion')" width="80" align="center">
          <template #default="{ row }">v{{ row.version }}</template>
        </el-table-column>
        <el-table-column prop="fieldCount" :label="t('inquiryTemplates.colFields')" width="80" align="center" />
        <el-table-column :label="t('inquiryTemplates.colDefault')" width="110" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.isDefault" type="success" effect="plain">{{ t('inquiryTemplates.inUse') }}</el-tag>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('inquiryTemplates.colStatus')" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'ACTIVE' ? 'primary' : 'warning'" effect="plain">
              {{ t(`inquiryTemplates.statuses.${row.status}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" min-width="330" fixed="right">
          <template #default="{ row }">
            <div class="row-actions">
              <el-button link type="primary" @click="downloadTemplate(row)">{{ t('inquiryTemplates.download') }}</el-button>
              <el-button v-if="canWrite && row.status === 'ACTIVE'" link type="primary" @click="openEdit(row)">{{ t('common.edit') }}</el-button>
              <el-button v-if="canWrite && !row.isDefault && row.status === 'ACTIVE'" link type="success" @click="makeDefault(row)">{{ t('inquiryTemplates.makeDefault') }}</el-button>
              <el-button v-if="canWrite && row.status === 'ACTIVE' && !row.isDefault" link type="warning" @click="disableTemplate(row)">{{ t('inquiryTemplates.disable') }}</el-button>
              <el-button v-if="canWrite && row.status === 'DISABLED'" link type="success" @click="enableTemplate(row)">{{ t('inquiryTemplates.enable') }}</el-button>
              <el-button v-if="canWrite" link @click="openCreate(row)">{{ t('inquiryTemplates.copyCustomize') }}</el-button>
            </div>
          </template>
        </el-table-column>
        <template #empty>{{ t('inquiryTemplates.empty') }}</template>
      </el-table>
    </section>

    <el-dialog
      v-model="editorOpen"
      :title="editor.id ? t('inquiryTemplates.editTitle') : t('inquiryTemplates.createTitle')"
      width="min(1150px, 95vw)"
      top="4vh"
      destroy-on-close
      :close-on-click-modal="false"
    >
      <el-alert type="info" :closable="false" show-icon class="editor-notice">
        {{ t(editor.id ? 'inquiryTemplates.versionNotice' : 'inquiryTemplates.createNotice') }}
      </el-alert>
      <el-form label-width="110px" class="editor-form" inline>
        <el-form-item :label="t('inquiryTemplates.fieldCode')" required>
          <el-input
            v-model="editor.templateCode"
            :disabled="!!editor.id"
            :placeholder="t('inquiryTemplates.codePlaceholder')"
            style="width: 240px"
            @input="editor.templateCode = editor.templateCode.toUpperCase()"
          />
        </el-form-item>
        <el-form-item :label="t('inquiryTemplates.fieldName')" required>
          <el-input v-model="editor.name" style="width: 280px" />
        </el-form-item>
        <el-form-item :label="t('inquiryTemplates.fieldDefault')">
          <el-switch v-model="editor.isDefault" :disabled="editor.wasDefault" />
        </el-form-item>
        <el-form-item :label="t('inquiryTemplates.fieldDescription')" class="description-item">
          <el-input v-model="editor.description" style="width: 100%" maxlength="500" />
        </el-form-item>
      </el-form>

      <div class="fields-head">
        <span class="fields-title">{{ t('inquiryTemplates.fieldsTitle') }}</span>
        <span class="fields-hint">{{ t('inquiryTemplates.fieldsHint') }}</span>
        <el-button size="small" @click="addCustomColumn">{{ t('inquiryTemplates.addCustom') }}</el-button>
      </div>
      <el-table :data="editor.fields" size="small" border max-height="46vh">
        <el-table-column :label="t('inquiryTemplates.colOrder')" width="76" align="center">
          <template #default="{ $index }">
            <el-button link icon="Top" :disabled="$index === 0" @click="moveField(editor.fields, $index, -1)" />
            <el-button link icon="Bottom" :disabled="$index === editor.fields.length - 1" @click="moveField(editor.fields, $index, 1)" />
          </template>
        </el-table-column>
        <el-table-column :label="t('inquiryTemplates.colKey')" min-width="185">
          <template #default="{ row }">
            <el-input v-if="row.isCustom" v-model="row.fieldKey" size="small" />
            <code v-else>{{ row.fieldKey }}</code>
          </template>
        </el-table-column>
        <el-table-column :label="t('inquiryTemplates.colHeader')" min-width="150">
          <template #default="{ row }"><el-input v-model="row.displayName" size="small" /></template>
        </el-table-column>
        <el-table-column :label="t('inquiryTemplates.colType')" width="115">
          <template #default="{ row }">
            <el-select v-model="row.dataType" size="small">
              <el-option value="TEXT" label="Text" />
              <el-option value="NUMBER" label="Number" />
              <el-option value="DATE" label="Date" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column :label="t('inquiryTemplates.colRequired')" width="70" align="center">
          <template #default="{ row }"><el-checkbox v-model="row.isRequired" :disabled="row.isCore" /></template>
        </el-table-column>
        <el-table-column :label="t('inquiryTemplates.colDefaultValue')" width="130">
          <template #default="{ row }"><el-input v-model="row.defaultValue" size="small" /></template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="95" align="center">
          <template #default="{ row, $index }">
            <el-tag v-if="row.isCore" size="small" effect="plain">{{ t('inquiryTemplates.coreTag') }}</el-tag>
            <el-button v-else link type="danger" size="small" @click="editor.fields.splice($index, 1)">{{ t('common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <template #footer>
        <el-button @click="editorOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { get, post, put } from '../api'
import { useAuthStore } from '../stores/auth'
import {
  moveField, nextCustomFieldKey, requiredFieldKeys, validateTemplateFields,
  type InquiryTemplate, type TemplateField,
} from '../lib/inquiryTemplates'

const { t } = useI18n()
const router = useRouter()
const auth = useAuthStore()
const canWrite = auth.can('sales:inquiry:write')

const loading = ref(false)
const saving = ref(false)
const rows = ref<InquiryTemplate[]>([])
const editorOpen = ref(false)

const editor = reactive({
  id: '',
  wasDefault: false,
  templateCode: '',
  name: '',
  description: '',
  isDefault: false,
  fields: [] as TemplateField[],
})

async function load() {
  loading.value = true
  try {
    const data = await get<{ templates: InquiryTemplate[] }>('/inquiry-templates')
    rows.value = data.templates ?? []
  } finally {
    loading.value = false
  }
}

function cloneFields(template: InquiryTemplate): TemplateField[] {
  return (template.fields ?? [])
    .map((field) => ({ ...field }))
    .sort((a, b) => a.sortOrder - b.sortOrder)
}

async function openEdit(row: InquiryTemplate) {
  const data = await get<{ template: InquiryTemplate }>(`/inquiry-templates/${row.id}`)
  Object.assign(editor, {
    id: data.template.id,
    wasDefault: data.template.isDefault,
    templateCode: data.template.templateCode,
    name: data.template.name,
    description: data.template.description,
    isDefault: data.template.isDefault,
    fields: cloneFields(data.template),
  })
  editorOpen.value = true
}

// 新建与「复制并自定义」共用同一个弹窗。新建只要核心字段起步（产品、
// 数量、单位、单价、总价），其余列按需添加；复制则以选中的模板为完整
// 底稿。编码只在新建时可填，保存后不可改。
async function openCreate(source?: InquiryTemplate) {
  let base = source
  if (!base) {
    base = rows.value.find((row) => row.isSystem) ?? rows.value.find((row) => row.isDefault)
  }
  let fields: TemplateField[] = []
  if (base) {
    const data = await get<{ template: InquiryTemplate }>(`/inquiry-templates/${base.id}`)
    fields = cloneFields(data.template)
  }
  if (!source) fields = fields.filter((field) => field.isCore)
  Object.assign(editor, {
    id: '', wasDefault: false, isDefault: false,
    templateCode: '', name: base ? `${base.name} · ${t('inquiryTemplates.copySuffix')}` : '',
    description: base?.description ?? '', fields,
  })
  editorOpen.value = true
}

function addCustomColumn() {
  editor.fields.push({
    fieldKey: nextCustomFieldKey(editor.fields),
    displayName: '',
    sortOrder: editor.fields.length + 1,
    isRequired: false,
    defaultValue: '',
    dataType: 'TEXT',
    isCustom: true,
    isCore: false,
  })
}

async function save() {
  if (!editor.id && !/^[A-Z][A-Z0-9_]{1,59}$/.test(editor.templateCode)) {
    ElMessage.warning(t('inquiryTemplates.errors.codeInvalid'))
    return
  }
  if (!editor.name.trim()) {
    ElMessage.warning(t('inquiryTemplates.errors.nameEmpty'))
    return
  }
  editor.fields.forEach((field, i) => { field.sortOrder = i + 1 })
  const invalid = validateTemplateFields(editor.fields)
  if (invalid) {
    ElMessage.warning(t(invalid))
    return
  }
  const body = {
    name: editor.name.trim(),
    description: editor.description.trim(),
    is_default: editor.isDefault,
    fields: editor.fields.map((field) => ({
      field_key: field.fieldKey.trim(),
      display_name: field.displayName.trim(),
      sort_order: field.sortOrder,
      is_required: field.isCore ? requiredFieldKeys.includes(field.fieldKey) : field.isRequired,
      default_value: field.defaultValue.trim(),
      data_type: field.dataType,
    })),
  }
  saving.value = true
  try {
    if (editor.id) {
      await put(`/inquiry-templates/${editor.id}`, body)
      ElMessage.success(t('inquiryTemplates.savedNewVersion'))
    } else {
      await post('/inquiry-templates', { ...body, template_code: editor.templateCode.trim() })
      ElMessage.success(t('inquiryTemplates.created'))
    }
    editorOpen.value = false
    await load()
  } finally {
    saving.value = false
  }
}

async function makeDefault(row: InquiryTemplate) {
  await ElMessageBox.confirm(t('inquiryTemplates.defaultConfirm', { name: row.name }), t('inquiryTemplates.makeDefault'), { type: 'warning' })
  await post(`/inquiry-templates/${row.id}/default`)
  ElMessage.success(t('inquiryTemplates.defaultSet'))
  await load()
}

async function disableTemplate(row: InquiryTemplate) {
  await ElMessageBox.confirm(t('inquiryTemplates.disableConfirm', { name: row.name }), t('inquiryTemplates.disable'), { type: 'warning' })
  await post(`/inquiry-templates/${row.id}/status`, { status: 'DISABLED' })
  await load()
}

async function enableTemplate(row: InquiryTemplate) {
  await post(`/inquiry-templates/${row.id}/status`, { status: 'ACTIVE' })
  await load()
}

// 下载走浏览器直连：网关用会话 Cookie 鉴权，不需要把文件内容读进前端。
function downloadTemplate(row: InquiryTemplate) {
  window.open(`/api/inquiry-templates/${row.id}/download`, '_blank')
}

onMounted(load)
</script>

<style scoped>
.page{padding:28px;background:#f4f7f7;min-height:100%}.page-head{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:18px}.head-actions{display:flex;gap:10px}.eyebrow{color:#16766b;font-size:12px;font-weight:700;letter-spacing:.12em;text-transform:uppercase}.page-head h1{margin:5px 0 4px;font-size:26px;color:#173042}.page-head p{margin:0;color:#71808b}.panel{background:#fff;border:1px solid #dfe8e6;border-radius:12px;padding:18px}.row-actions{display:flex;align-items:center;gap:4px;flex-wrap:wrap}.editor-notice{margin-bottom:16px}.editor-form{display:flex;flex-wrap:wrap;gap:0 12px}.description-item{width:100%}.fields-head{display:flex;align-items:baseline;gap:12px;margin:6px 0 10px}.fields-title{font-weight:600;color:#173042}.fields-hint{flex:1;color:#7b8992;font-size:12px}@media(max-width:850px){.page-head{flex-direction:column;gap:14px}.head-actions{flex-wrap:wrap}}
</style>
