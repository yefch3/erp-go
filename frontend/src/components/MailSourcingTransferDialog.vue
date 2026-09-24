<!-- 把「生成 Excel」的结果转入待复核询盘：先选客户（联系人可选），再建案。

     从 MailExcelConverter 里拆出来只是因为那个文件过了 800 行，逻辑原样。
     打开前那道检查（有没有行、预览全不全）留在那边：它看的是预览。 -->
<template>
  <!-- 客户来自基础数据；联系人按客户联动但可留空，邮箱只显示主数据快照。 -->
  <el-dialog
    v-model="open"
    :title="t('emails.sourcingTransferTitle')"
    width="min(460px, 92vw)"
    append-to-body
  >
    <p class="sourcing-hint">{{ t('emails.sourcingTransferHint') }}</p>
    <el-form label-position="top">
      <el-form-item :label="t('emails.sourcingCustomer')" required>
        <CustomerSelect
          v-model="form.customerId"
          :placeholder="t('emails.sourcingCustomerPlaceholder')"
          @selected="selectCustomer"
        />
      </el-form-item>
      <el-form-item :label="t('emails.sourcingContactOptional')">
        <el-select
          v-model="form.contactId"
          filterable
          :loading="contactsLoading"
          :disabled="!form.customerId"
          :placeholder="form.customerId ? t('emails.sourcingContactPlaceholder') : t('emails.sourcingSelectCustomerFirst')"
          style="width:100%"
        >
          <el-option
            v-for="contact in contacts"
            :key="contact.id"
            :value="contact.id"
            :label="contactLabel(contact)"
          />
        </el-select>
        <div v-if="form.customerId && !contactsLoading && !contacts.length" class="sourcing-contact-help">
          {{ t('emails.sourcingNoActiveContacts') }}
        </div>
      </el-form-item>
      <el-form-item :label="t('emails.sourcingContactEmail')">
        <el-input
          :model-value="selectedContact?.email || ''"
          readonly
          :placeholder="t('emails.sourcingContactEmailAuto')"
        />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="open = false">{{ t('emails.close') }}</el-button>
      <el-button
        type="primary"
        :loading="creating"
        :disabled="!form.customerId"
        @click="create"
      >
        {{ t('emails.createSourcingCase') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import CustomerSelect from './masterdata/CustomerSelect.vue'
import { customerDisplayName } from '../lib/customerDisplay'
import type { ExcelResult } from '../lib/excelJobResult'
import { newIdempotencySession, withIdempotency } from '../lib/idempotency'
import { optionalSourcingContact } from '../lib/sourcingTransfer'

const props = defineProps<{
  result: ExcelResult | null
  // 这份表是从哪封信里转出来的，记在询盘上。
  sourceMailId: string
}>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ transferred: [caseId: string] }>()

const { t } = useI18n()

interface SourcingCustomerContact {
  id: string
  name: string
  department: string
  title: string
  email: string
  isPrimary: boolean
}

const creating = ref(false)
// 转询盘是一次创建动作。请求成功但响应丢失时，重试继续使用同一个键，
// 网关会重放第一次的结果，不会再开一张重复询盘。
const idem = newIdempotencySession()
const contactsLoading = ref(false)
const contacts = ref<SourcingCustomerContact[]>([])
// 转入采购必须关联主数据中的客户和联系人，姓名与邮箱只作为后端保存的快照。
const form = reactive({ customerId: '', customerName: '', contactId: '' })
const selectedContact = computed(() =>
  contacts.value.find((contact) => contact.id === form.contactId) ?? null,
)

// 每次打开都从空白开始：上一次选的客户不该带到另一封信的询盘上。
watch(open, (isOpen) => {
  if (!isOpen) return
  form.customerId = ''
  form.customerName = ''
  form.contactId = ''
  contacts.value = []
})

function contactLabel(contact: SourcingCustomerContact) {
  const role = [contact.department, contact.title].filter(Boolean).join(' / ')
  const primary = contact.isPrimary ? ` · ${t('emails.sourcingPrimaryContact')}` : ''
  const email = contact.email || t('emails.sourcingContactEmailMissing')
  return `${contact.name}${role ? ` · ${role}` : ''} · ${email}${primary}`
}

async function selectCustomer(customer?: { id: string | number; name: string; shortName?: string }) {
  form.customerName = customerDisplayName(customer)
  form.contactId = ''
  contacts.value = []
  const customerId = String(customer?.id ?? form.customerId ?? '')
  if (!customerId) {
    contactsLoading.value = false
    return
  }
  contactsLoading.value = true
  try {
    const data = await get<{ contacts: SourcingCustomerContact[] }>(`/sourcing-customer-options/${customerId}/contacts`)
    if (String(form.customerId) !== customerId) return
    contacts.value = data.contacts ?? []
  } finally {
    if (String(form.customerId) === customerId) contactsLoading.value = false
  }
}

async function create() {
  const result = props.result
  const sheet = result?.sheets[0]
  if (!result || !props.sourceMailId || !sheet?.rows.length) return
  if (!form.customerId) {
    ElMessage.warning(t('emails.sourcingCustomerRequired'))
    return
  }

  const fieldByColumn: Record<string, string> = {
    '产品': 'product', '材质/标准': 'materialStandard', '牌号/等级': 'grade',
    '厚度': 'thickness', '宽度': 'width', '长度/形式': 'lengthOrForm',
    '表面要求': 'surfaceRequirement', '涂层/镀层': 'coating', '公差': 'tolerance',
    '卷重': 'coilWeight', '卷内径': 'coilId', '包装': 'packaging', '交期': 'delivery',
    '付款条件': 'paymentTerms', '贸易术语': 'incoterm', '港口': 'port',
    '单位': 'quantityUnit', '备注': 'remarks', '数量': 'quantity',
  }
  // LLM 结果每列携带模板字段标识（snake_case），按标识对齐——公司用模板
  // 改过表头也不受影响；本地直读的结果没有标识，退回表头映射。价格列不
  // 属于采购明细事实（由工厂报价产生），custom.* 自定义列进 custom_fields。
  const columnKeys = sheet.columnKeys ?? []
  const lines = sheet.rows.map((row) => {
    const line: Record<string, string> & { customFields?: Record<string, string> } = {}
    sheet.columns.forEach((column, index) => {
      const key = columnKeys[index] ?? fieldByColumn[column]
      if (!key || key === 'unit_price' || key === 'total_price') return
      const cell = row.cells[index] ?? ''
      if (key.startsWith('custom.')) {
        ;(line.customFields ??= {})[key] = cell
      } else {
        line[key] = cell
      }
    })
    return line
  })
  creating.value = true
  try {
    const response = await post<{ sourcingCase: { id: string; caseNo: string } }>('/sourcing-cases', {
      title: result.fileName.replace(/\.xlsx$/i, ''),
      customerId: form.customerId,
      customerName: form.customerName,
      // protojson 的 int64 不接受空字符串。联系人可选时必须彻底省略字段，
      // 不能把 el-select 的空值 "" 原样送到网关。
      ...optionalSourcingContact(form.contactId),
      sourceMailId: props.sourceMailId,
      inquiryTemplateId: result.inquiryTemplateId || '0',
      inquiryTemplateCode: result.inquiryTemplateCode || '',
      inquiryTemplateVersion: result.inquiryTemplateVersion || 0,
      lines,
    }, withIdempotency(idem))
    idem.reset()
    ElMessage.success(t('procurementIntakes.autoTransferred', { no: response.sourcingCase.caseNo }))
    open.value = false
    emit('transferred', response.sourcingCase.id)
  } finally {
    creating.value = false
  }
}
</script>

<style scoped>
.sourcing-hint {
  margin: 0 0 14px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.sourcing-contact-help {
  margin-top: 6px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.45;
}
</style>
