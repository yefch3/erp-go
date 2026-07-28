<template>
  <div>
    <div class="page-head">
      <h2>{{ t('contracts.title') }}</h2>
      <el-button v-if="canWrite" type="primary" @click="openGenerate">{{ t('contracts.generate') }}</el-button>
    </div>

    <el-card shadow="never">
      <div class="filters">
        <el-input
          v-model="keyword"
          :placeholder="t('contracts.searchPlaceholder')"
          clearable
          style="width: 240px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-select v-model="status" :placeholder="t('contracts.allStatus')" clearable style="width: 170px" @change="reload">
          <el-option v-for="s in STATUSES" :key="s" :value="s" :label="t(`contracts.statuses.${s}`)" />
        </el-select>
        <el-button @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table :data="contracts" v-loading="loading">
        <el-table-column prop="contractNo" :label="t('contracts.contractNo')" width="145" />
        <el-table-column prop="customerName" :label="t('contracts.customer')" min-width="150" />
        <el-table-column :label="t('contracts.amount')" width="140" align="right">
          <template #default="{ row }">{{ row.totalAmount }} {{ row.currency }}</template>
        </el-table-column>
        <el-table-column :label="t('contracts.version')" width="70" align="center">
          <template #default="{ row }">v{{ row.versionNo }}</template>
        </el-table-column>
        <el-table-column :label="t('contracts.owner')" width="90">
          <template #default="{ row }">{{ row.salesEmployee || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('contracts.fromQuote')" width="130">
          <template #default="{ row }"><span class="sub">{{ row.quoteNo || '—' }}</span></template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)">{{ t(`contracts.statuses.${row.status}`) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="200" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row.id)">{{ t('contracts.detail') }}</el-button>
            <template v-if="canWrite">
              <el-button v-if="row.status === 'PENDING_SIGN'" link type="success" @click="sign(row)">
                {{ t('contracts.sign') }}
              </el-button>
              <el-button v-else-if="row.status === 'DRAFT'" link type="primary" @click="submit(row)">
                {{ t('contracts.submit') }}
              </el-button>
            </template>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        class="pager"
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="(p: number) => { page = p; load() }"
      />
    </el-card>

    <!-- Generate from an accepted quotation -->
    <el-dialog v-model="generateOpen" :title="t('contracts.generate')" width="620px">
      <el-alert :title="t('contracts.generateHint')" type="info" :closable="false" show-icon class="alert" />
      <el-form label-width="110px">
        <el-form-item :label="t('contracts.quotation')" required>
          <el-select v-model="generateForm.quotationId" filterable style="width: 100%" :placeholder="t('contracts.pickQuotation')">
            <el-option
              v-for="q in acceptedQuotes"
              :key="q.id"
              :value="q.id"
              :label="`${q.quoteNo} · ${q.customerName} · ${q.totalAmount} ${q.currency}`"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('contracts.deliveryDate')">
          <el-date-picker v-model="generateForm.deliveryDate" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
          <div class="hint">{{ t('contracts.deliveryHint') }}</div>
        </el-form-item>
        <el-form-item :label="t('contracts.terms')">
          <el-input v-model="generateForm.terms" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="generateOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="generate">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- Detail: terms, lines, version history -->
    <el-drawer v-model="detailOpen" :size="880" :title="detail?.contract.contractNo ?? ''">
      <div v-if="detail" v-loading="loadingDetail">
        <div class="detail-head">
          <div>
            <el-tag :type="statusType(detail.contract.status)">
              {{ t(`contracts.statuses.${detail.contract.status}`) }}
            </el-tag>
            <span class="version-chip">
              v{{ detail.version.versionNo }} · {{ t(`contracts.versionStatuses.${detail.version.status}`) }}
            </span>
            <span v-if="isInForce" class="in-force">{{ t('contracts.inForce') }}</span>
          </div>
          <div v-if="canWrite" class="detail-actions">
            <el-button v-if="editable" size="small" @click="openTerms">{{ t('contracts.editDraft') }}</el-button>
            <el-button v-if="editable" size="small" type="primary" @click="submit(detail.contract)">
              {{ t('contracts.submit') }}
            </el-button>
            <el-button v-if="detail.contract.status === 'PENDING_SIGN'" size="small" type="success" @click="sign(detail.contract)">
              {{ t('contracts.sign') }}
            </el-button>
            <el-button v-if="changeable" size="small" @click="openChange">{{ t('contracts.change') }}</el-button>
            <el-button v-if="cancellable" size="small" type="danger" plain @click="cancel(detail.contract)">
              {{ t('contracts.cancel') }}
            </el-button>
          </div>
        </div>

        <el-descriptions :column="2" border size="small" class="desc">
          <el-descriptions-item :label="t('contracts.buyer')">
            {{ detail.version.buyerName }}
            <div class="sub">{{ detail.version.buyerAddress || '—' }}</div>
          </el-descriptions-item>
          <el-descriptions-item :label="t('contracts.seller')">
            {{ detail.version.sellerName }}
            <div class="sub">{{ detail.version.sellerAddress || '—' }}</div>
          </el-descriptions-item>
          <el-descriptions-item :label="t('contracts.incoterm')">{{ detail.version.incoterm }}</el-descriptions-item>
          <el-descriptions-item :label="t('contracts.paymentMethod')">{{ detail.version.paymentMethod || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('contracts.portOfLoading')">{{ detail.version.portOfLoading || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('contracts.portOfDischarge')">{{ detail.version.portOfDischarge || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('contracts.deliveryDate')">
            <span :class="{ missing: !detail.version.deliveryDate }">{{ detail.version.deliveryDate || t('contracts.notSet') }}</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('contracts.fromQuote')">{{ detail.contract.quoteNo || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('contracts.terms')" :span="2">
            <div class="terms">{{ detail.version.terms || '—' }}</div>
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.version.changeReason" :label="t('contracts.changeReason')" :span="2">
            {{ detail.version.changeReason }}
          </el-descriptions-item>
        </el-descriptions>

        <el-divider content-position="left">{{ t('contracts.items') }}</el-divider>
        <el-table :data="detail.items" size="small">
          <el-table-column prop="lineNo" label="#" width="45" />
          <el-table-column :label="t('contracts.product')" min-width="200">
            <template #default="{ row }">
              {{ row.productName }}
              <div class="sub">{{ row.productCode }}<template v-if="row.spec"> · {{ row.spec }}</template></div>
            </template>
          </el-table-column>
          <el-table-column :label="t('contracts.hsCode')" width="110">
            <template #default="{ row }"><span class="sub">{{ row.hsCode || '—' }}</span></template>
          </el-table-column>
          <el-table-column :label="t('contracts.qty')" width="120" align="right">
            <template #default="{ row }">{{ trimZeros(row.qty) }} {{ row.uomCode }}</template>
          </el-table-column>
          <el-table-column :label="t('contracts.unitPrice')" width="100" align="right">
            <template #default="{ row }">{{ row.unitPrice }}</template>
          </el-table-column>
          <el-table-column :label="t('contracts.lineAmount')" width="110" align="right">
            <template #default="{ row }">{{ row.amount }}</template>
          </el-table-column>
        </el-table>
        <div class="totals">
          <span>{{ t('contracts.total') }}</span>
          <strong>{{ detail.version.totalAmount }} {{ detail.version.currency }}</strong>
          <span class="sub">≈ {{ detail.version.baseAmount }} {{ detail.version.fx.baseCurrency }}</span>
        </div>
        <div class="snapshot">
          {{ t('contracts.fxSnapshot') }}:
          1 {{ detail.version.fx.baseCurrency }} = {{ detail.version.fx.rate }} {{ detail.version.currency }}
          · {{ detail.version.fx.source }} · {{ formatTime(detail.version.fx.rateAt) }}
          <span class="hint">{{ t('contracts.fxInherited') }}</span>
        </div>

        <template v-if="canSeeApproval && approvals.length">
          <el-divider content-position="left">
            {{ t('contracts.approval') }}
            <span class="hint">{{ t('contracts.approvalHint') }}</span>
          </el-divider>
          <div v-for="(inst, i) in approvals" :key="inst.instance.id" class="approval-round">
            <div class="round-head">
              <span class="round-no">{{ t('contracts.round', { n: i + 1 }) }}</span>
              <el-tag size="small" :type="instanceTagType(inst.instance.status)" effect="plain">
                {{ t(`todos.doc.${inst.instance.status}`) }}
              </el-tag>
              <span class="sub">{{ inst.instance.submitterName }} · {{ formatTime(inst.instance.submittedAt) }}</span>
            </div>
            <el-steps
              :active="activeStep(inst)"
              align-center
              finish-status="success"
              :process-status="inst.instance.status === 'RUNNING' ? 'process' : 'wait'"
            >
              <el-step
                v-for="task in inst.tasks"
                :key="task.id"
                :title="task.nodeName"
                :status="stepStatus(task.status)"
              >
                <template #description>
                  <div class="step-desc">
                    <div>{{ employeeName(task.assigneeId) }}</div>
                    <div class="sub">{{ t(`todos.task.${task.status}`) }}</div>
                    <div v-if="task.comment" class="sub comment-line">{{ task.comment }}</div>
                    <div v-if="task.actedAt" class="sub">{{ formatTime(task.actedAt) }}</div>
                  </div>
                </template>
              </el-step>
            </el-steps>
          </div>
        </template>

        <el-divider content-position="left">
          {{ t('contracts.files') }}
          <span class="hint">{{ t('contracts.filesHint') }}</span>
        </el-divider>
        <div v-if="canWrite" class="upload-bar">
          <el-select v-model="uploadKind" style="width: 150px">
            <el-option v-for="k in FILE_KINDS" :key="k" :value="k" :label="t(`contracts.fileKinds.${k}`)" />
          </el-select>
          <input ref="fileInput" type="file" hidden @change="upload" />
          <el-button :loading="uploading" @click="fileInput?.click()">{{ t('contracts.upload') }}</el-button>
          <span class="hint">{{ t('contracts.uploadHint', { version: detail.version.versionNo }) }}</span>
        </div>
        <el-table :data="files" size="small">
          <el-table-column :label="t('contracts.fileKind')" width="100">
            <template #default="{ row }">
              <el-tag size="small" :type="row.kind === 'SIGNED' ? 'success' : 'info'" effect="plain">
                {{ t(`contracts.fileKinds.${row.kind}`) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('contracts.fileName')" min-width="220">
            <template #default="{ row }">
              <a v-if="row.downloadUrl" :href="row.downloadUrl" target="_blank" class="doc-link">{{ row.fileName }}</a>
              <span v-else>{{ row.fileName }}</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('contracts.version')" width="70" align="center">
            <template #default="{ row }">v{{ row.versionNo }}</template>
          </el-table-column>
          <el-table-column :label="t('contracts.fileSize')" width="90" align="right">
            <template #default="{ row }">{{ humanSize(row.sizeBytes) }}</template>
          </el-table-column>
          <el-table-column :label="t('contracts.uploadedAt')" width="130">
            <template #default="{ row }">{{ formatTime(row.uploadedAt) }}</template>
          </el-table-column>
          <el-table-column :label="t('contracts.uploader')" width="90">
            <template #default="{ row }">{{ row.uploaderName || '—' }}</template>
          </el-table-column>
          <el-table-column v-if="canWrite" width="70">
            <template #default="{ row }">
              <el-button link type="danger" @click="removeFile(row)">{{ t('common.delete') }}</el-button>
            </template>
          </el-table-column>
          <template #empty>{{ t('contracts.noFiles') }}</template>
        </el-table>

        <el-divider content-position="left">
          {{ t('contracts.versions') }}
          <span class="hint">{{ t('contracts.versionsHint') }}</span>
        </el-divider>
        <el-table :data="detail.versions" size="small" @row-click="(row: VersionRow) => openDetail(detail!.contract.id, row.id)">
          <el-table-column :label="t('contracts.version')" width="70">
            <template #default="{ row }">v{{ row.versionNo }}</template>
          </el-table-column>
          <el-table-column :label="t('common.status')" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="versionTagType(row.status)">
                {{ t(`contracts.versionStatuses.${row.status}`) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('contracts.amount')" width="140" align="right">
            <template #default="{ row }">{{ row.totalAmount }} {{ row.currency }}</template>
          </el-table-column>
          <el-table-column :label="t('contracts.deliveryDate')" width="110">
            <template #default="{ row }">{{ row.deliveryDate || '—' }}</template>
          </el-table-column>
          <el-table-column :label="t('contracts.changeReason')" min-width="180">
            <template #default="{ row }"><span class="sub">{{ row.changeReason || '—' }}</span></template>
          </el-table-column>
        </el-table>
      </div>
    </el-drawer>

    <!-- Edit the working draft: terms and lines -->
    <el-dialog v-model="termsOpen" :title="t('contracts.editDraft')" width="860px">
      <el-form label-width="110px">
        <div class="grid">
          <el-form-item :label="t('contracts.buyer')">
            <el-input v-model="termsForm.buyerName" />
          </el-form-item>
          <el-form-item :label="t('contracts.seller')">
            <el-input v-model="termsForm.sellerName" />
          </el-form-item>
          <el-form-item :label="t('contracts.buyerAddress')">
            <el-input v-model="termsForm.buyerAddress" />
          </el-form-item>
          <el-form-item :label="t('contracts.sellerAddress')">
            <el-input v-model="termsForm.sellerAddress" />
          </el-form-item>
          <el-form-item :label="t('contracts.incoterm')">
            <el-select v-model="termsForm.incoterm" style="width: 100%">
              <el-option v-for="i in INCOTERMS" :key="i" :value="i" :label="i" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('contracts.paymentMethod')">
            <el-select v-model="termsForm.paymentMethod" clearable style="width: 100%">
              <el-option v-for="o in paymentOptions" :key="o.code" :value="o.code" :label="o.label" />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('contracts.portOfLoading')">
            <el-input v-model="termsForm.portOfLoading" />
          </el-form-item>
          <el-form-item :label="t('contracts.portOfDischarge')">
            <el-input v-model="termsForm.portOfDischarge" />
          </el-form-item>
          <el-form-item :label="t('contracts.deliveryDate')" required>
            <el-date-picker v-model="termsForm.deliveryDate" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
          </el-form-item>
        </div>
        <el-form-item :label="t('contracts.terms')">
          <el-input v-model="termsForm.terms" type="textarea" :rows="4" />
        </el-form-item>
      </el-form>

      <el-divider content-position="left">
        {{ t('contracts.items') }}
        <span class="hint">{{ t('contracts.editItemsHint') }}</span>
      </el-divider>
      <el-table :data="termsForm.items" size="small">
        <el-table-column :label="t('contracts.product')" min-width="220">
          <template #default="{ row }">
            <el-select v-model="row.productId" filterable style="width: 100%">
              <el-option v-for="p in products" :key="p.id" :value="p.id" :label="`${p.code} · ${p.name}`" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column :label="t('contracts.spec')" width="150">
          <template #default="{ row }"><el-input v-model="row.spec" /></template>
        </el-table-column>
        <el-table-column :label="t('contracts.qty')" width="110">
          <template #default="{ row }"><el-input v-model="row.qty" /></template>
        </el-table-column>
        <el-table-column :label="t('contracts.unitPrice')" width="110">
          <template #default="{ row }"><el-input v-model="row.unitPrice" /></template>
        </el-table-column>
        <el-table-column :label="t('contracts.lineAmount')" width="110" align="right">
          <template #default="{ row }">{{ lineAmount(row) }}</template>
        </el-table-column>
        <el-table-column width="60">
          <template #default="{ $index }">
            <el-button link type="danger" @click="termsForm.items.splice($index, 1)">
              {{ t('common.delete') }}
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="items-foot">
        <el-button @click="termsForm.items.push({ productId: '', spec: '', qty: '', unitPrice: '' })">
          {{ t('contracts.addItem') }}
        </el-button>
        <div class="totals">
          <span class="hint">{{ t('contracts.totalPreview') }}</span>
          <strong>{{ previewTotal(termsForm.items) }} {{ detail?.version.currency }}</strong>
        </div>
      </div>
      <template #footer>
        <el-button @click="termsOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTerms">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- Open a change version -->
    <el-dialog v-model="changeOpen" :title="t('contracts.change')" width="820px">
      <el-alert :title="t('contracts.changeHint')" type="warning" :closable="false" show-icon class="alert" />
      <el-form label-width="110px">
        <el-form-item :label="t('contracts.changeReason')" required>
          <el-input v-model="changeForm.reason" type="textarea" :rows="2" :placeholder="t('contracts.changeReasonHint')" />
        </el-form-item>
        <el-form-item :label="t('contracts.deliveryDate')">
          <el-date-picker v-model="changeForm.deliveryDate" type="date" value-format="YYYY-MM-DD" style="width: 220px" />
        </el-form-item>
      </el-form>
      <el-divider content-position="left">
        {{ t('contracts.items') }}
        <span class="hint">{{ t('contracts.changeItemsHint') }}</span>
      </el-divider>
      <el-table :data="changeForm.items" size="small">
        <el-table-column :label="t('contracts.product')" min-width="220">
          <template #default="{ row }">
            <el-select v-model="row.productId" filterable style="width: 100%">
              <el-option v-for="p in products" :key="p.id" :value="p.id" :label="`${p.code} · ${p.name}`" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column :label="t('contracts.spec')" width="150">
          <template #default="{ row }"><el-input v-model="row.spec" /></template>
        </el-table-column>
        <el-table-column :label="t('contracts.qty')" width="110">
          <template #default="{ row }"><el-input v-model="row.qty" /></template>
        </el-table-column>
        <el-table-column :label="t('contracts.unitPrice')" width="110">
          <template #default="{ row }"><el-input v-model="row.unitPrice" /></template>
        </el-table-column>
        <el-table-column width="60">
          <template #default="{ $index }">
            <el-button link type="danger" @click="changeForm.items.splice($index, 1)">{{ t('common.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="items-foot">
        <el-button @click="changeForm.items.push({ productId: '', spec: '', qty: '', unitPrice: '' })">
          {{ t('contracts.addItem') }}
        </el-button>
      </div>
      <template #footer>
        <el-button @click="changeOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveChange">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { del, get, post, put } from '../api'
import { useAuthStore } from '../stores/auth'

interface Fx { rate: string; rateAt: string; source: string; baseCurrency: string }
interface Contract {
  id: string
  contractNo: string
  quoteNo: string
  customerName: string
  currentVersionId: string
  status: string
  currency: string
  totalAmount: string
  versionNo: number
  salesEmployee: string
}
interface Version {
  id: string
  versionNo: number
  buyerName: string
  buyerAddress: string
  sellerName: string
  sellerAddress: string
  currency: string
  incoterm: string
  portOfLoading: string
  portOfDischarge: string
  paymentMethod: string
  deliveryDate: string
  terms: string
  totalAmount: string
  baseAmount: string
  fx: Fx
  changeReason: string
  status: string
}
type VersionRow = Pick<Version, 'id' | 'versionNo' | 'status' | 'currency' | 'totalAmount' | 'deliveryDate' | 'changeReason'>
interface Item {
  lineNo: number
  productId: string
  productCode: string
  productName: string
  spec: string
  qty: string
  uomCode: string
  unitPrice: string
  amount: string
  hsCode: string
}
interface Detail { contract: Contract; version: Version; items: Item[]; versions: VersionRow[] }
interface ApprovalTask {
  id: string
  nodeSeq: number
  nodeName: string
  assigneeId: string
  status: string
  comment: string
  actedAt: string
}
interface ApprovalInstance {
  id: string
  status: string
  currentSeq: number
  submitterName: string
  submittedAt: string
}
interface ApprovalRound { instance: ApprovalInstance; tasks: ApprovalTask[] }
interface ContractFile {
  id: string
  kind: string
  fileName: string
  versionNo: number
  sizeBytes: string
  uploadedAt: string
  uploaderName: string
  downloadUrl: string
}
interface Quote { id: string; quoteNo: string; customerName: string; currency: string; totalAmount: string }
interface Product { id: string; code: string; name: string }
interface OptionItem { code: string; label: string }
interface ChangeLine { productId: string; spec: string; qty: string; unitPrice: string }

const STATUSES = ['DRAFT', 'PENDING_APPROVAL', 'PENDING_SIGN', 'EFFECTIVE', 'EXECUTING', 'COMPLETED', 'CANCELLED']
const INCOTERMS = ['FOB', 'CIF', 'CFR', 'EXW', 'DDP']
// DRAFT is what we sent out, SIGNED is what came back with a signature on it.
const FILE_KINDS = ['DRAFT', 'SIGNED', 'OTHER']

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = auth.can('export:contract:write')

const contracts = ref<Contract[]>([])
const acceptedQuotes = ref<Quote[]>([])
const products = ref<Product[]>([])
const paymentOptions = ref<OptionItem[]>([])
const detail = ref<Detail | null>(null)
const total = ref(0)
const page = ref(1)
const pageSize = 10
const keyword = ref('')
const status = ref('')
const loading = ref(false)
const loadingDetail = ref(false)
const saving = ref(false)
const detailOpen = ref(false)
const generateOpen = ref(false)
const termsOpen = ref(false)
const changeOpen = ref(false)

const generateForm = reactive({ quotationId: '', deliveryDate: '', terms: '' })
const termsForm = reactive({
  buyerName: '', buyerAddress: '', sellerName: '', sellerAddress: '',
  incoterm: 'FOB', portOfLoading: '', portOfDischarge: '', paymentMethod: '',
  deliveryDate: '', terms: '', items: [] as ChangeLine[],
})
const changeForm = reactive({ reason: '', deliveryDate: '', items: [] as ChangeLine[] })
const approvals = ref<ApprovalRound[]>([])
const employees = ref<Record<string, string>>({})
const canSeeApproval = auth.can('approval:instance:read')
const files = ref<ContractFile[]>([])
const fileInput = ref<HTMLInputElement | null>(null)
const uploadKind = ref('SIGNED')
const uploading = ref(false)

// Only the newest version can be worked on, and only while it is a draft.
// An older version opened from the history is always read-only.
const editable = computed(
  () => detail.value?.version.status === 'DRAFT' && detail.value.version.id === detail.value.versions[0]?.id,
)
const changeable = computed(
  () => ['EFFECTIVE', 'EXECUTING'].includes(detail.value?.contract.status ?? '') &&
    detail.value?.version.status === 'APPROVED',
)
// A rejected contract is precisely what voiding is for; a draft nobody
// submitted and an unsigned one can go too. Anything that has ever been in
// force needs a termination, which is a different act.
const cancellable = computed(
  () => ['DRAFT', 'REJECTED', 'PENDING_SIGN'].includes(detail.value?.contract.status ?? '') &&
    detail.value?.contract.currentVersionId === '0',
)
const isInForce = computed(() => detail.value?.contract.currentVersionId === detail.value?.version.id)

async function load() {
  loading.value = true
  try {
    const data = await get<{ contracts: Contract[]; meta: { total: string } }>('/contracts', {
      page: page.value, page_size: pageSize, keyword: keyword.value, status: status.value,
    })
    contracts.value = data.contracts ?? []
    total.value = Number(data.meta.total)
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

// Fetch first, open second. A link from elsewhere (an approval todo, a
// bookmark) can point at a contract that no longer exists, and opening the
// drawer before knowing that leaves it wedged half-open when we close it
// again in the same frame.
async function openDetail(id: string, versionId?: string) {
  loadingDetail.value = true
  try {
    detail.value = await get<Detail>(`/contracts/${id}`, versionId ? { version_id: versionId } : undefined)
    detailOpen.value = true
    await Promise.all([loadFiles(id), loadApprovals(id)])
  } catch {
    // The interceptor has already told the user why.
    detail.value = null
    detailOpen.value = false
  } finally {
    loadingDetail.value = false
  }
}

// Deep link: /contracts?id=4 opens that contract, which is how an approval
// todo hands over to the document it is about.
watch(
  () => route.query.id,
  (id) => {
    if (typeof id === 'string' && id) openDetail(id)
  },
  { immediate: true },
)

// Drop the query when the drawer closes, so a refresh does not reopen it.
watch(detailOpen, (open) => {
  if (!open && route.query.id) router.replace({ path: route.path })
})

async function openGenerate() {
  Object.assign(generateForm, { quotationId: '', deliveryDate: '', terms: '' })
  generateOpen.value = true
  // Offers that already have a live contract are excluded server-side; the
  // database refuses a second one anyway, and offering it would be a trap.
  acceptedQuotes.value =
    (await get<{ quotations: Quote[] }>('/quotations', {
      status: 'ACCEPTED', without_contract: true, page_size: 100,
    })).quotations ?? []
}

async function generate() {
  if (!generateForm.quotationId) {
    ElMessage.warning(t('contracts.quotationRequired'))
    return
  }
  saving.value = true
  try {
    const data = await post<Detail>('/contracts', {
      quotationId: generateForm.quotationId,
      terms: { deliveryDate: generateForm.deliveryDate, terms: generateForm.terms },
    })
    ElMessage.success(t('contracts.created'))
    generateOpen.value = false
    load()
    openDetail(data.contract.id)
  } finally {
    saving.value = false
  }
}

function openTerms() {
  const v = detail.value!.version
  Object.assign(termsForm, {
    buyerName: v.buyerName, buyerAddress: v.buyerAddress,
    sellerName: v.sellerName, sellerAddress: v.sellerAddress,
    incoterm: v.incoterm, portOfLoading: v.portOfLoading, portOfDischarge: v.portOfDischarge,
    paymentMethod: v.paymentMethod, deliveryDate: v.deliveryDate, terms: v.terms,
    items: toLines(detail.value!.items),
  })
  termsOpen.value = true
}

async function saveTerms() {
  if (termsForm.items.length === 0) {
    ElMessage.warning(t('contracts.itemsRequired'))
    return
  }
  const { items, ...terms } = termsForm
  saving.value = true
  try {
    await put(`/contracts/${detail.value!.contract.id}`, { terms, items })
    ElMessage.success(t('contracts.updated'))
    termsOpen.value = false
    await openDetail(detail.value!.contract.id)
    load()
  } finally {
    saving.value = false
  }
}

function openChange() {
  changeForm.reason = ''
  changeForm.deliveryDate = detail.value!.version.deliveryDate
  // Prefilled with the lines in force, so a change starts from what is
  // actually being executed rather than a blank sheet.
  changeForm.items = toLines(detail.value!.items)
  changeOpen.value = true
}

// ---------------------------------------------------------------- approval

// Every round this contract has been through, newest last. A rejected round
// stays visible: "who turned it down and what did they say" is the first
// thing anyone asks.
async function loadApprovals(contractId: string) {
  approvals.value = []
  if (!canSeeApproval) return
  const listed = await get<{ instances: ApprovalInstance[] }>('/approvals/instances', {
    biz_type: 'CONTRACT', biz_id: contractId,
  })
  const rounds = await Promise.all(
    (listed.instances ?? [])
      .slice()
      .sort((a, b) => Number(a.id) - Number(b.id))
      .map((i) => get<ApprovalRound>(`/approvals/instances/${i.id}`)),
  )
  approvals.value = rounds
}

// el-steps counts completed steps; a finished round has them all behind it.
function activeStep(round: ApprovalRound): number {
  if (round.instance.status !== 'RUNNING') return round.tasks.length
  return round.tasks.filter((task) => task.status !== 'PENDING').length
}

function stepStatus(taskStatus: string): 'wait' | 'process' | 'finish' | 'error' | 'success' {
  return ({
    APPROVED: 'success', REJECTED: 'error', RETURNED: 'error',
    PENDING: 'process', SKIPPED: 'finish', CANCELLED: 'wait',
  } as const)[taskStatus] ?? 'wait'
}

function instanceTagType(status: string): 'success' | 'danger' | 'warning' | 'info' {
  return ({ APPROVED: 'success', REJECTED: 'danger', RETURNED: 'warning', RUNNING: 'warning' } as const)[status] ?? 'info'
}

// The approval service stores assignees as ids only; it has no business
// knowing anyone's name. The directory fills them in here.
function employeeName(id: string): string {
  return employees.value[id] ?? `#${id}`
}

// ---------------------------------------------------------------- files

async function loadFiles(contractId: string) {
  files.value = (await get<{ files: ContractFile[] }>(`/contracts/${contractId}/files`)).files ?? []
}

// Three steps on purpose: ask for a signed URL, PUT the bytes straight to
// object storage, then tell the server what landed. The file never travels
// through the gateway, so a 30 MB scan is not a 30 MB request body.
async function upload(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file || !detail.value) return
  const contractId = detail.value.contract.id
  uploading.value = true
  try {
    const signed = await post<{ fileKey: string; uploadUrl: string }>(
      `/contracts/${contractId}/files/presign`,
      { fileName: file.name, contentType: file.type },
    )
    const put = await fetch(signed.uploadUrl, { method: 'PUT', body: file })
    if (!put.ok) throw new Error(`upload failed: ${put.status}`)
    await post(`/contracts/${contractId}/files`, {
      fileKey: signed.fileKey,
      fileName: file.name,
      contentType: file.type,
      sizeBytes: String(file.size),
      kind: uploadKind.value,
      contractVersionId: detail.value.version.id,
    })
    ElMessage.success(t('contracts.uploaded'))
    await loadFiles(contractId)
  } catch {
    ElMessage.error(t('contracts.uploadFailed'))
  } finally {
    uploading.value = false
    input.value = ''
  }
}

async function removeFile(row: ContractFile) {
  await ElMessageBox.confirm(t('contracts.removeFileConfirm', { name: row.fileName }), t('common.delete'), {
    type: 'warning',
  })
  await del(`/contract-files/${row.id}`)
  ElMessage.success(t('contracts.fileRemoved'))
  await loadFiles(detail.value!.contract.id)
}

function humanSize(bytes: string): string {
  const n = Number(bytes)
  if (!Number.isFinite(n) || n <= 0) return '—'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function toLines(items: Item[]): ChangeLine[] {
  return items.map((i) => ({
    productId: i.productId, spec: i.spec, qty: trimZeros(i.qty), unitPrice: i.unitPrice,
  }))
}

// Preview only. The server recomputes every amount and its numbers are the
// ones stored; this just keeps the editor oriented while typing.
function lineAmount(row: ChangeLine): string {
  const qty = Number(row.qty)
  const price = Number(row.unitPrice)
  if (!Number.isFinite(qty) || !Number.isFinite(price)) return '—'
  return (Math.round(qty * price * 100) / 100).toFixed(2)
}

function previewTotal(lines: ChangeLine[]): string {
  return lines
    .reduce((sum, row) => {
      const amount = Number(lineAmount(row))
      return Number.isFinite(amount) ? sum + amount : sum
    }, 0)
    .toFixed(2)
}

async function saveChange() {
  if (!changeForm.reason.trim()) {
    ElMessage.warning(t('contracts.changeReasonRequired'))
    return
  }
  saving.value = true
  try {
    await post(`/contracts/${detail.value!.contract.id}/change`, {
      changeReason: changeForm.reason,
      terms: { deliveryDate: changeForm.deliveryDate },
      items: changeForm.items,
    })
    ElMessage.success(t('contracts.changeCreated'))
    changeOpen.value = false
    await openDetail(detail.value!.contract.id)
    load()
  } finally {
    saving.value = false
  }
}

async function submit(row: { id: string }) {
  await post(`/contracts/${row.id}/submit`)
  ElMessage.success(t('contracts.submitted'))
  if (detailOpen.value) await openDetail(row.id)
  load()
}

async function sign(row: { id: string }) {
  await ElMessageBox.confirm(t('contracts.signConfirm'), t('contracts.sign'), { type: 'warning' })
  await post(`/contracts/${row.id}/sign`)
  ElMessage.success(t('contracts.signed'))
  if (detailOpen.value) await openDetail(row.id)
  load()
}

async function cancel(row: { id: string }) {
  await ElMessageBox.confirm(t('contracts.cancelConfirm'), t('contracts.cancel'), { type: 'warning' })
  await post(`/contracts/${row.id}/cancel`)
  ElMessage.success(t('contracts.cancelled'))
  if (detailOpen.value) await openDetail(row.id)
  load()
}

function statusType(s: string): 'success' | 'danger' | 'warning' | 'info' | 'primary' {
  return ({
    EFFECTIVE: 'success', EXECUTING: 'success', COMPLETED: 'info',
    CANCELLED: 'danger', REJECTED: 'danger', PENDING_APPROVAL: 'warning', PENDING_SIGN: 'primary',
  } as const)[s] ?? 'info'
}

function versionTagType(s: string): 'success' | 'warning' | 'info' | 'danger' {
  return ({ APPROVED: 'success', PENDING_APPROVAL: 'warning', REJECTED: 'danger' } as const)[s] ?? 'info'
}

// Quantities come back as NUMERIC(18,4); trailing zeros are noise to a reader.
function trimZeros(n: string): string {
  return n.includes('.') ? n.replace(/0+$/, '').replace(/\.$/, '') : n
}

function formatTime(iso: string): string {
  return iso ? iso.replace('T', ' ').slice(0, 16) : ''
}

onMounted(async () => {
  load()
  products.value = (await get<{ products: Product[] }>('/products', { page_size: 200 })).products ?? []
  paymentOptions.value = (await get<{ options: OptionItem[] }>('/options', { category: 'PAYMENT_METHOD' })).options ?? []
  // Names for the approval timeline. Skipped when the user cannot read the
  // directory: the timeline then shows ids, which is worse than names but
  // better than hiding who a contract is waiting on.
  if (auth.can('iam:employee:read')) {
    const staff = await get<{ employees: { id: string; name: string }[] }>('/employees', { page_size: 200 })
    employees.value = Object.fromEntries((staff.employees ?? []).map((e) => [e.id, e.name]))
  }
})
</script>

<style scoped>
.approval-round {
  margin-bottom: 18px;
}
.round-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}
.round-no {
  font-size: 13px;
  color: var(--el-text-color-regular);
}
.step-desc {
  font-size: 12px;
  line-height: 1.6;
}
.comment-line {
  max-width: 180px;
  margin: 0 auto;
}

.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.page-head h2 {
  font-size: 18px;
  font-weight: 500;
  margin: 0;
}
.filters {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  column-gap: 16px;
}
.sub {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.hint {
  margin-left: 8px;
  font-weight: 400;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.alert {
  margin-bottom: 16px;
}
.detail-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
  flex-wrap: wrap;
  gap: 10px;
}
.version-chip {
  margin-left: 10px;
  font-size: 13px;
  color: var(--el-text-color-regular);
}
.in-force {
  margin-left: 8px;
  padding: 1px 8px;
  border-radius: 10px;
  font-size: 12px;
  background: var(--el-color-success-light-9);
  color: var(--el-color-success);
}
.desc {
  margin-bottom: 8px;
}
.terms {
  white-space: pre-wrap;
}
.missing {
  color: var(--el-color-warning);
}
.totals {
  display: flex;
  align-items: baseline;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 12px;
}
.totals strong {
  font-size: 16px;
}
.snapshot {
  margin-top: 12px;
  padding: 8px 12px;
  border-radius: 6px;
  background: var(--el-fill-color-light);
  font-size: 13px;
}
.items-foot {
  margin-top: 10px;
}
</style>
