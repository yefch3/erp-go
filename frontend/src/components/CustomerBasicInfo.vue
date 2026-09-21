<template>
  <section v-loading="loading">
    <div class="basic-heading"><h3>基本资料</h3><el-button v-if="canWrite" type="primary" plain @click="open">编辑基本资料</el-button></div>
    <div class="basic-grid">
      <div v-for="group in groups" :key="group.title" class="basic-card">
        <h4>{{ group.title }}</h4>
        <dl><div v-for="field in group.fields" :key="field.key"><dt>{{ field.label }}</dt><dd>{{ display(field.key) }}</dd></div></dl>
      </div>
      <div class="basic-card">
        <h4>主要联系人 <el-button link type="primary" @click="$emit('navigate', 'contacts')">管理联系人</el-button></h4>
        <dl><div v-for="field in contactFields" :key="field.key"><dt>{{ field.label }}</dt><dd>{{ primaryContact?.[field.key] || '—' }}</dd></div></dl>
      </div>
      <div class="basic-card">
        <h4>负责人 <el-button link type="primary" @click="$emit('navigate', 'owners')">管理负责人</el-button></h4>
        <p>{{ owners.length ? owners.map(o => o.employeeName).join('、') : '未分配' }}</p>
      </div>
    </div>
    <el-dialog v-model="editing" title="编辑基本资料" width="min(900px, 94vw)" top="5vh" append-to-body :close-on-click-modal="false">
      <el-scrollbar max-height="68vh">
        <div class="editor-content">
          <el-form label-position="top" :model="form">
            <section v-for="group in groups" :key="group.title" class="editor-section">
              <h4>{{ group.title }}</h4>
              <div class="edit-grid">
                <el-form-item v-for="field in group.fields" :key="field.key" :label="field.label" :required="field.key === 'name'">
                  <el-select v-if="field.key === 'creditGrade'" v-model="form.creditGrade" clearable><el-option v-for="grade in ['A','B','C','D']" :key="grade" :label="grade" :value="grade" /></el-select>
                  <el-input v-else v-model="form[field.key]" :disabled="field.key === 'code'" :maxlength="field.max" :placeholder="field.key === 'code' ? '客户代码创建后固定' : ''" />
                </el-form-item>
              </div>
            </section>
          </el-form>
          <section class="editor-section">
            <div class="basic-heading"><h4>联系人资料</h4><el-button link type="primary" @click="emit('edit-contact')">新增联系人</el-button></div>
            <p class="editor-hint">联系人在各业务页面共用，在联系人窗口中保存后立即生效。</p>
            <article v-for="contact in activeContacts" :key="contact.id" class="contact-editor-card">
              <div class="basic-heading"><strong>{{ contact.name }}<el-tag v-if="contact.isPrimary" size="small" type="info">主要联系人</el-tag></strong><el-button link type="primary" @click="emit('edit-contact', contact)">编辑联系人</el-button></div>
              <dl class="contact-editor-fields"><div v-for="field in contactFields" :key="field.key"><dt>联系人{{ field.label }}</dt><dd>{{ contact[field.key] || '—' }}</dd></div></dl>
            </article>
            <p v-if="!activeContacts.length" class="editor-hint">暂无联系人，请新增联系人</p>
          </section>
          <section class="editor-section">
            <div class="basic-heading"><h4>分管人（负责人）</h4><el-button link type="primary" @click="emit('edit-owner')">添加负责人</el-button></div>
            <p class="editor-hint">支持多个负责人，在负责人窗口中保存后立即生效。</p>
            <div v-for="owner in owners" :key="owner.id" class="owner-editor-row"><span>{{ owner.employeeName }}</span><el-button link type="primary" @click="emit('edit-owner', owner)">编辑</el-button></div>
            <p v-if="!owners.length" class="editor-hint">未分配负责人</p>
          </section>
        </div>
      </el-scrollbar>
      <template #footer><el-button @click="editing = false">关闭</el-button><el-button type="primary" :loading="saving" @click="save">保存基本资料</el-button></template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { get, put } from '../api'
import { countryName } from '../lib/countries'
import { resolveImportedCountry } from '../lib/customerImport'
const props = defineProps<{ customerId: string; canWrite: boolean }>()
const emit = defineEmits<{ updated: []; navigate: [tab: string]; tenant: [id: string]; 'edit-contact': [contact?: any]; 'edit-owner': [owner?: any] }>()
const loading = ref(false), saving = ref(false), editing = ref(false)
const basic = ref<Record<string, string>>({}), form = reactive<Record<string, string>>({})
const contacts = ref<any[]>([]), owners = ref<any[]>([])
const activeContacts = computed(() => contacts.value.filter(c => c.status === 'ACTIVE'))
const primaryContact = computed(() => contacts.value.find(c => c.isPrimary && c.status === 'ACTIVE') || contacts.value.find(c => c.status === 'ACTIVE'))
const fields = [
  { key: 'code', label: '客户代码', max: 50 }, { key: 'name', label: '客户名称', max: 200 },
  { key: 'shortName', label: '客户简称', max: 100 }, { key: 'countryRegion', label: '所属地区', max: 100 },
  { key: 'customerType', label: '客户类型', max: 50 }, { key: 'creditGrade', label: '客户等级', max: 32 },
  { key: 'source', label: '客户来源', max: 50 }, { key: 'archiveCreator', label: '建档人', max: 100 },
  { key: 'address', label: '通信地址', max: 500 }, { key: 'postalCode', label: '邮政编码', max: 30 },
  { key: 'companyPhone', label: '公司电话', max: 50 }, { key: 'faxNumber', label: '传真号码', max: 50 },
  { key: 'companyEmail', label: '电子信箱', max: 200 },
]
const groups = [{ title: '客户信息', fields: fields.slice(0, 8) }, { title: '公司联系资料', fields: fields.slice(8) }]
const contactFields = [{ key: 'name', label: '姓名' }, { key: 'email', label: '邮箱' }, { key: 'phone', label: '电话' }, { key: 'mobile', label: '手机' }]
function display(key: string) { return basic.value[key] || (key === 'countryRegion' && basic.value.countryCode ? countryName(basic.value.countryCode, 'zh-CN') : '—') }
async function load() {
  loading.value = true
  try {
    const [b, c, o] = await Promise.all([get<any>(`/customers/${props.customerId}/basic`), get<any>(`/customers/${props.customerId}/contacts`), get<any>(`/customers/${props.customerId}/owners`)])
    basic.value = b.basic ?? {}; contacts.value = c.contacts ?? []; owners.value = (o.owners ?? []).filter((o: any) => o.status === 'ACTIVE')
    emit('tenant', String(b.tenantId))
  } finally { loading.value = false }
}
async function open() { if (!props.canWrite) return; await load(); for (const field of fields) form[field.key] = field.key === 'countryRegion' && !basic.value.countryRegion && basic.value.countryCode ? countryName(basic.value.countryCode, 'zh-CN') : basic.value[field.key] ?? ''; editing.value = true }
async function save() {
  if (!props.canWrite) return;
  if (!form.name?.trim()) { ElMessage.warning('请填写客户名称'); return }
  const region = resolveImportedCountry(form.countryRegion ?? '')
  const countryCode = /^[A-Z]{2}$/.test(region.countryCode) ? region.countryCode : ''
  saving.value = true
  try {
    await put(`/customers/${props.customerId}/basic`, { basic: { ...form, countryCode, addressState: region.state } })
    editing.value = false; await load(); emit('updated'); ElMessage.success('基本资料已保存')
  } finally { saving.value = false }
}
onMounted(load)
defineExpose({ open, load })
</script>

<style scoped>
.basic-heading,.basic-card h4 { display:flex;align-items:center;justify-content:space-between;gap:12px; }
.basic-heading { margin-bottom:16px; }.basic-heading h3 { margin:0; }
.basic-grid,.edit-grid { display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:20px; }
.basic-card { border:1px solid var(--el-border-color-lighter);border-radius:12px;overflow:hidden; }
.basic-card h4 { margin:0;padding:16px 20px;background:var(--el-fill-color-light); }
.basic-card dl { margin:0;padding:8px 20px; }.basic-card dl>div { display:grid;grid-template-columns:110px 1fr;padding:12px 0;border-bottom:1px dashed var(--el-border-color-lighter); }
.basic-card dl>div:last-child { border-bottom:0; }.basic-card dt { color:var(--el-text-color-secondary); }.basic-card dd { margin:0;overflow-wrap:anywhere; }.basic-card p { padding:0 20px; }
.editor-content { padding-right:12px; }
.editor-section + .editor-section { margin-top:24px; }
.editor-section h4 { margin:0 0 14px; font-size:15px; }
.editor-section .basic-heading { margin-bottom:8px; }
.editor-section .basic-heading h4 { margin:0; }
.edit-grid { gap:16px 20px; }
.edit-grid :deep(.el-form-item) { margin-bottom:0; }
.edit-grid :deep(.el-select) { width:100%; }
.editor-hint { color:var(--el-text-color-secondary);font-size:13px;margin:8px 0; }
.contact-editor-card { padding:12px;border:1px solid var(--el-border-color-lighter);border-radius:8px;margin-top:12px; }
.contact-editor-card strong { display:flex;align-items:center;gap:8px;flex-wrap:wrap; }
.contact-editor-fields { display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;margin:12px 0 0; }
.contact-editor-fields dt { color:var(--el-text-color-secondary);font-size:13px; }
.contact-editor-fields dd { margin:4px 0 0;overflow-wrap:anywhere; }
.owner-editor-row { display:flex;align-items:center;justify-content:space-between;padding:8px 0;border-bottom:1px solid var(--el-border-color-lighter); }
@media(max-width:700px) { .basic-grid,.edit-grid,.contact-editor-fields { grid-template-columns:1fr; } }
</style>
