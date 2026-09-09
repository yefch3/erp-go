<template>
  <div class="page">
    <WorkflowPageHeader :title="t('receivableDue.title')" :description="t('receivableDue.subtitle')">
      <template #actions><el-button v-if="canWrite" type="primary" @click="openManual">{{ t('receivableDue.addManual') }}</el-button></template>
    </WorkflowPageHeader>

    <el-dialog v-model="manualOpen" :title="t('receivableDue.addManual')" width="min(560px, 94vw)" destroy-on-close>
      <el-form label-position="top">
        <el-form-item :label="t('receivableDue.manualCustomer')" required>
          <el-select v-model="manualForm.customerName" filterable allow-create default-first-option clearable :placeholder="t('receivableDue.manualPickOrEnter')" style="width: 100%">
            <el-option v-for="name in manualCustomerOptions" :key="name" :label="name" :value="name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('receivableDue.manualContract')" required>
          <el-select v-model="manualForm.contractNo" filterable allow-create default-first-option clearable :placeholder="t('receivableDue.manualPickOrEnter')" style="width: 100%" @change="applyManualContract">
            <el-option v-for="item in manualContractOptions" :key="item.contractNo" :label="`${item.contractNo} · ${item.customerName}`" :value="item.contractNo" />
          </el-select>
        </el-form-item>
        <div class="manual-grid">
          <el-form-item :label="t('receivableDue.manualTotal')" required><el-input v-model="manualForm.totalAmount"><template #prepend><el-select v-model="manualForm.currency" filterable allow-create default-first-option :placeholder="t('receivableDue.manualCurrencyHint')" style="width: 110px"><el-option v-for="c in ['USD','CNY','EUR','GBP','JPY','CAD','AUD','HKD']" :key="c" :value="c" /></el-select></template></el-input></el-form-item>
          <el-form-item :label="t('receivableDue.manualReceived')"><el-input v-model="manualForm.receivedAmount" /></el-form-item>
        </div>
        <el-form-item :label="t('receivableDue.manualDate')"><el-date-picker v-model="manualForm.receivedAt" type="date" value-format="YYYY-MM-DD" style="width: 100%" /></el-form-item>
        <el-form-item :label="t('receivableDue.manualDueDate')"><el-date-picker v-model="manualForm.dueDate" type="date" value-format="YYYY-MM-DD" clearable style="width: 100%" /></el-form-item>
        <el-form-item :label="t('receivableDue.entryNote')"><el-input v-model="manualForm.note" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="manualOpen = false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="manualBusy" @click="saveManual">{{ t('common.save') }}</el-button></template>
    </el-dialog>

    <!-- 三个数字，回答财务开门第一句话：现在有多少钱该收而没收到。
         逾期额单独一栏并染红——总额里混着还没到期的，那个数字安慰人，
         逾期额才是要打电话的理由。 -->
    <section class="metrics">
      <div class="metric" :class="{ 'is-alarm': overdueCount > 0 }">
        <span class="metric-label">{{ t('receivableDue.overdueCount') }}</span>
        <strong class="metric-value">{{ overdueCount }}</strong>
        <span class="metric-hint">{{ t('receivableDue.overdueHint') }}</span>
      </div>
      <div class="metric">
        <span class="metric-label">{{ t('receivableDue.dueSoonCount') }}</span>
        <strong class="metric-value">{{ dueSoonCount }}</strong>
        <span class="metric-hint">{{ t('receivableDue.dueSoonHint') }}</span>
      </div>
      <div class="metric" :class="{ 'is-warn': unsetCount > 0 }">
        <span class="metric-label">{{ t('receivableDue.unsetCount') }}</span>
        <strong class="metric-value">{{ unsetCount }}</strong>
        <span class="metric-hint">{{ t('receivableDue.unsetHint') }}</span>
      </div>
    </section>

    <section class="panel">
      <div class="filters">
        <!-- 主页签：客户对账下面的两个子页。状态放在地址的 query 里，
             这样前进/后退和分享链接都对，而菜单高亮看的是 route.path、
             不受 query 影响，仍然稳稳停在「客户对账」上。 -->
        <el-radio-group :model-value="isDone ? 'done' : 'open'" class="tabs" @update:model-value="switchTab">
          <el-radio-button value="open">{{ t('receivableDue.tabOpen') }}</el-radio-button>
          <el-radio-button value="done">{{ t('receivableDue.tabDone') }}</el-radio-button>
        </el-radio-group>
        <!-- 次级筛子，只在待核销那一档下有意义。 -->
        <el-radio-group v-if="!isDone" v-model="view" @change="reload">
          <el-radio-button value="">{{ t('receivableDue.viewAll') }}</el-radio-button>
          <el-radio-button value="overdue">{{ t('receivableDue.viewOverdue') }}</el-radio-button>
          <el-radio-button value="unset">{{ t('receivableDue.viewUnset') }}</el-radio-button>
        </el-radio-group>
        <el-input
          v-model="keyword"
          clearable
          :placeholder="t('receivableDue.search')"
          style="max-width: 240px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table v-loading="loading" :data="rows" :row-class-name="rowClass" @expand-change="onExpand">
        <!-- 展开看这张合同收过哪几笔。明细按需加载：每行都预先拉一次，
             一页就是 50 次往返。 -->
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="entries">
              <el-table v-if="entriesOf(row).length" :data="entriesOf(row)" size="small">
                <el-table-column :label="t('receivableDue.entryDate')" width="120">
                  <template #default="{ row: e }">{{ e.receivedAt || '—' }}</template>
                </el-table-column>
                <el-table-column :label="t('receivableDue.entryAmount')" width="150" align="right">
                  <template #default="{ row: e }">
                    <span :class="{ negative: Number(e.amount) < 0 }">{{ e.currency }} {{ e.amount }}</span>
                    <div v-if="Number(e.feeAmount)" class="sub">
                      {{ t('receivableDue.entryFee', { n: e.feeAmount }) }}
                    </div>
                  </template>
                </el-table-column>
                <el-table-column :label="t('receivableDue.entryNote')" min-width="180">
                  <template #default="{ row: e }">
                    <span v-if="Number(e.reversalOf)">{{ t('receivableDue.entryReversal') }} · {{ e.reverseReason }}</span>
                    <span v-else>{{ e.note || '—' }}</span>
                  </template>
                </el-table-column>
                <el-table-column :label="t('receivableDue.entryBy')" width="160">
                  <template #default="{ row: e }">{{ e.allocatedByName }}<div class="sub">{{ e.allocatedAt }}</div></template>
                </el-table-column>
              </el-table>
              <p v-else class="sub">{{ t('receivableDue.entriesEmpty') }}</p>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.contract')" min-width="180">
          <template #default="{ row }">
            <router-link :to="`/contracts?id=${row.contractId}`" class="doc-link">{{ row.contractNo }}</router-link>
            <div class="sub">{{ row.customerName }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.dueDate')" width="150">
          <template #default="{ row }">
            <template v-if="row.dueUnset">
              <el-tag size="small" type="warning" effect="plain">{{ t('receivableDue.unsetTag') }}</el-tag>
            </template>
            <template v-else>
              <div>{{ row.dueDate }}</div>
              <div class="sub" :class="{ overdue: row.overdueDays > 0 }">{{ dueLabel(row) }}</div>
            </template>
          </template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.openAmount')" width="160" align="right">
          <template #default="{ row }">
            <span class="num money" :class="{ overdue: row.overdueDays > 0 && !row.dueUnset }">
              {{ row.currency }} {{ row.openAmount }}
            </span>
            <div class="sub">{{ t('receivableDue.ofTotal', { total: row.totalAmount }) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.received')" width="130" align="right">
          <template #default="{ row }">
            <span class="num" :class="{ dim: Number(row.receivedAmount) === 0 }">{{ row.receivedAmount }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.effectiveDate')" width="120">
          <template #default="{ row }">{{ row.effectiveDate || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.executionCondition')" min-width="180">
          <template #default="{ row }">
            <span v-if="row.executionConditionStatus === 'NOT_APPLICABLE'">—</span>
            <div v-else-if="row.executionConditionStatus === 'READY'" class="condition-ready">
              <el-tag type="success" effect="light">{{ t('receivableDue.executionReady') }}</el-tag>
              <div class="sub">{{ conditionLabel(row.executionConditionType) }}</div>
            </div>
            <el-button v-else-if="canWrite && !row.manuallyEntered" type="primary" @click="openCondition(row)">
              {{ t('receivableDue.confirmExecution') }}
            </el-button>
            <el-tag v-else type="warning" effect="plain">{{ t('receivableDue.executionWaiting') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('receivableDue.owner')" min-width="110">
          <template #default="{ row }">{{ row.salesEmployee || '—' }}</template>
        </el-table-column>
        <!-- 已结清视图多一列：为什么不催了、谁定的。 -->
        <el-table-column v-if="isDone" :label="t('receivableDue.closedWhy')" min-width="170">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ t(`receivableDue.closureCategories.${row.closedCategory}`) }}</el-tag>
            <div class="sub">{{ row.closedByName }}<template v-if="row.closedNote"> · {{ row.closedNote }}</template></div>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-if="isDone || !canWrite" type="primary" plain @click="handleRowCommand(row, 'detail')">
              {{ t('receivableDue.viewDetails') }}
            </el-button>
            <el-dropdown v-else trigger="click" @command="(command: string) => handleRowCommand(row, command)">
              <el-button type="primary" plain>{{ t('receivableDue.moreActions') }}<span class="drop-arrow">▼</span></el-button>
              <template #dropdown><el-dropdown-menu>
                <el-dropdown-item command="detail">{{ t('receivableDue.viewDetails') }}</el-dropdown-item>
                <el-dropdown-item v-if="row.manuallyEntered" command="edit">{{ t('receivableDue.editManual') }}</el-dropdown-item>
                <el-dropdown-item divided command="receipt">{{ t('receivableDue.addReceipt') }}</el-dropdown-item>
                <el-dropdown-item command="close">{{ t('receivableDue.close') }}</el-dropdown-item>
                <el-dropdown-item command="due">{{ t('receivableDue.dueEdit') }}</el-dropdown-item>
              </el-dropdown-menu></template>
            </el-dropdown>
          </template>
        </el-table-column>
        <template #empty>{{ emptyText }}</template>
      </el-table>

      <el-pagination
        class="pager"
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="(p: number) => { page = p; load() }"
      />
    </section>

    <el-dialog v-model="conditionOpen" :title="t('receivableDue.confirmExecutionTitle')" width="min(560px, 94vw)" destroy-on-close>
      <template v-if="conditionRow">
        <p class="close-target">{{ conditionRow.contractNo }} · {{ conditionRow.customerName }}</p>
        <el-alert type="info" :closable="false" :title="t('receivableDue.confirmExecutionHint')" />
        <el-form label-position="top" class="condition-form">
          <el-form-item :label="t('receivableDue.executionCondition')" required>
            <el-radio-group v-model="conditionForm.conditionType" class="condition-options">
              <el-radio v-for="type in EXECUTION_CONDITIONS" :key="type" :value="type" border>
                {{ conditionLabel(type) }}
              </el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item :label="t('receivableDue.conditionNote')">
            <el-input v-model="conditionForm.note" type="textarea" :rows="2" :placeholder="t('receivableDue.conditionNoteHint')" />
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button @click="conditionOpen=false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="conditionBusy" :disabled="!conditionForm.conditionType" @click="submitCondition">
          {{ t('receivableDue.confirmAndRelease') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailOpen" :title="t('receivableDue.detailTitle')" width="min(820px, 96vw)">
      <template v-if="detailRow">
        <p class="close-target">{{ detailRow.contractNo }} · {{ detailRow.customerName }}</p>
        <el-descriptions :column="3" border class="detail-summary">
          <el-descriptions-item :label="t('receivableDue.manualTotal')">{{ detailRow.currency }} {{ detailRow.totalAmount }}</el-descriptions-item>
          <el-descriptions-item :label="t('receivableDue.manualReceived')">{{ detailRow.receivedAmount }}</el-descriptions-item>
          <el-descriptions-item :label="t('receivableDue.openAmount')">{{ detailRow.openAmount }}</el-descriptions-item>
          <el-descriptions-item :label="t('receivableDue.dueDate')">{{ detailRow.dueDate || t('receivableDue.unsetTag') }}</el-descriptions-item>
          <el-descriptions-item :label="t('receivableDue.owner')">{{ detailRow.salesEmployee || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('receivableDue.detailStatus')">{{ isDone ? t('receivableDue.tabDone') : t('receivableDue.tabOpen') }}</el-descriptions-item>
        </el-descriptions>
        <h3 class="detail-heading">{{ t('receivableDue.detailHistory') }}</h3>
        <el-table v-loading="detailLoading" :data="entriesOf(detailRow)" size="small">
          <el-table-column :label="t('receivableDue.entryDate')" prop="receivedAt" width="120" />
          <el-table-column :label="t('receivableDue.entryAmount')" width="150"><template #default="{ row: e }">{{ e.currency }} {{ e.amount }}</template></el-table-column>
          <el-table-column :label="t('receivableDue.entryNote')" min-width="180"><template #default="{ row: e }">{{ Number(e.reversalOf) ? `${t('receivableDue.entryReversal')} · ${e.reverseReason}` : (e.note || '—') }}</template></el-table-column>
          <el-table-column :label="t('receivableDue.entryBy')" min-width="170"><template #default="{ row: e }">{{ e.allocatedByName || '—' }}<div class="sub">{{ e.allocatedAt }}</div></template></el-table-column>
          <el-table-column v-if="canWrite && !isDone" :label="t('common.actions')" width="90">
            <template #default="{ row: e }"><el-button v-if="!Number(e.reversalOf) && !reversedIds(detailRow).has(String(e.allocationId))" link type="danger" @click="reverseEntry(detailRow, e)">{{ t('receivableDue.entryReverse') }}</el-button></template>
          </el-table-column>
          <template #empty>{{ t('receivableDue.entriesEmpty') }}</template>
        </el-table>
      </template>
    </el-dialog>

    <el-dialog v-model="editOpen" :title="t('receivableDue.editManual')" width="min(520px, 94vw)" destroy-on-close>
      <el-form label-position="top">
        <el-form-item :label="t('receivableDue.manualCustomer')" required><el-input v-model="editForm.customerName" /></el-form-item>
        <el-form-item :label="t('receivableDue.manualContract')" required><el-input v-model="editForm.contractNo" /></el-form-item>
        <el-form-item :label="t('receivableDue.manualDueDate')"><el-date-picker v-model="editForm.dueDate" type="date" value-format="YYYY-MM-DD" clearable style="width:100%" /></el-form-item>
        <el-alert :closable="false" type="info" :title="t('receivableDue.editManualHint')" />
      </el-form>
      <template #footer><el-button @click="editOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="editBusy" @click="saveEdit">{{ t('common.save') }}</el-button></template>
    </el-dialog>

    <!-- 收款结清：这张合同的钱「不用再催了」。三个数并排亮着，员工看着差额
         做决定——这正是「完成由人确认」那条原则在合同侧的样子。只关催收的
         口，不关钱的门：结清的合同照样能核销，钱真的又来了就撤销。 -->
    <!-- 改应收到期日。这个日子决定这份合同算不算逾期，所以理由必填，
         改动连同旧值新值一起留痕，并把这份合同未读的催收提醒清掉——
         不清的话扫描器下一轮会按新日子把提醒整轮重发。 -->
    <el-dialog v-model="dueOpen" :title="t('receivableDue.dueEdit')" width="min(460px, 94vw)" destroy-on-close>
      <template v-if="dueRow">
        <p class="close-target">{{ dueRow.contractNo }} · {{ dueRow.customerName }}</p>
        <el-form label-position="top">
          <el-form-item :label="t('receivableDue.dueDate')">
            <el-date-picker
              v-model="dueForm.dueDate"
              type="date"
              value-format="YYYY-MM-DD"
              clearable
              :placeholder="t('receivableDue.dueEmpty')"
              style="width: 100%"
            />
          </el-form-item>
          <el-form-item :label="t('receivableDue.dueWhy')">
            <el-input
              v-model="dueForm.reason"
              type="textarea"
              :rows="2"
              :placeholder="t('receivableDue.dueWhyHint')"
            />
          </el-form-item>
        </el-form>
      </template>
      <template #footer>
        <el-button @click="dueOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="dueBusy" :disabled="!dueForm.reason.trim()" @click="submitDue">
          {{ t('common.save') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="closeOpen" :title="t('receivableDue.closeTitle')" width="min(520px, 94vw)" destroy-on-close>
      <template v-if="closing">
        <p class="close-target">{{ closing.contractNo }} · {{ closing.customerName }}</p>
        <div class="close-figures">
          <div><span class="sub">{{ t('receivableDue.figTotal') }}</span><span class="num">{{ closing.currency }} {{ closing.totalAmount }}</span></div>
          <div><span class="sub">{{ t('receivableDue.figReceived') }}</span><span class="num">{{ closing.receivedAmount }}</span></div>
          <div><span class="sub">{{ t('receivableDue.figOpen') }}</span><span class="num warn">{{ closing.openAmount }}</span></div>
        </div>
        <el-form label-position="top">
          <el-form-item :label="t('receivableDue.closeWhat')">
            <el-radio-group v-model="closeForm.category">
              <el-radio v-for="k in CLOSURE_CATEGORIES" :key="k" :value="k">
                {{ t(`receivableDue.closureCategories.${k}`) }}
              </el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item :label="t('receivableDue.closeNote')">
            <el-input v-model="closeForm.note" :placeholder="t('receivableDue.closeNoteHint')" />
          </el-form-item>
        </el-form>
        <el-alert type="info" :closable="false" show-icon :title="t('receivableDue.closeHint')" />
      </template>
      <template #footer>
        <el-button @click="closeOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="closingBusy" @click="submitClose">{{ t('receivableDue.closeConfirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 记一笔收款：选中的合同已经定了，员工只填「多少钱、哪天到的」。
         币种跟合同走，不问——只有一个正确答案的问题只会制造错答案。 -->
    <el-dialog v-model="entryOpen" :title="t('receivableDue.addReceiptTitle')" width="min(520px, 94vw)" destroy-on-close>
      <template v-if="entryRow">
        <p class="close-target">{{ entryRow.contractNo }} · {{ entryRow.customerName }}</p>
        <div class="close-figures">
          <div><span class="sub">{{ t('receivableDue.figTotal') }}</span><span class="num">{{ entryRow.currency }} {{ entryRow.totalAmount }}</span></div>
          <div><span class="sub">{{ t('receivableDue.figReceived') }}</span><span class="num">{{ entryRow.receivedAmount }}</span></div>
          <div><span class="sub">{{ t('receivableDue.figOpen') }}</span><span class="num warn">{{ entryRow.openAmount }}</span></div>
        </div>
        <el-form label-position="top">
          <el-form-item :label="t('receivableDue.entryKind')">
            <el-radio-group v-model="entryForm.isRefund">
              <el-radio-button :value="false">{{ t('receivableDue.kindReceipt') }}</el-radio-button>
              <el-radio-button :value="true">{{ t('receivableDue.kindRefund') }}</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item :label="t('receivableDue.entryAmount')">
            <el-input v-model="entryForm.amount" :placeholder="t('receivableDue.entryAmountHint')">
              <template #prepend>{{ entryRow.currency }}</template>
            </el-input>
          </el-form-item>
          <el-form-item :label="t('receivableDue.entryDate')">
            <el-date-picker v-model="entryForm.receivedAt" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
          </el-form-item>
          <el-form-item :label="t('receivableDue.entryNote')">
            <el-input v-model="entryForm.note" :placeholder="t('receivableDue.entryNoteHint')" />
          </el-form-item>
        </el-form>
        <el-alert type="info" :closable="false" show-icon :title="t('receivableDue.entryHint')" />
      </template>
      <template #footer>
        <el-button @click="entryOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="entryBusy" @click="submitEntry">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { get, patch, post } from '../api'
import { useAuthStore } from '../stores/auth'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = auth.can('export:receipt:write')
const manualOpen = ref(false)
const manualBusy = ref(false)
const manualForm = reactive({ customerName: '', contractNo: '', currency: 'USD', totalAmount: '', receivedAmount: '', receivedAt: '', dueDate: '', note: '' })

async function saveManual() {
  if (!manualForm.customerName.trim() || !manualForm.contractNo.trim() || !manualForm.totalAmount.trim()) {
    ElMessage.warning(t('receivableDue.manualRequired')); return
  }
  manualBusy.value = true
  try {
    await post('/receivable-due/manual', manualForm)
    manualOpen.value = false
    Object.assign(manualForm, { customerName: '', contractNo: '', currency: 'USD', totalAmount: '', receivedAmount: '', receivedAt: '', dueDate: '', note: '' })
    ElMessage.success(t('receivableDue.manualSaved'))
    reload()
  } finally { manualBusy.value = false }
}

interface Row {
  contractId: string
  contractNo: string
  customerId: string
  customerName: string
  salesEmployeeId: string
  salesEmployee: string
  dueDate: string
  effectiveDate: string
  currency: string
  totalAmount: string
  receivedAmount: string
  openAmount: string
  overdueDays: number
  dueUnset: boolean
  closedCategory: string
  closedNote: string
  closedByName: string
  closedAt: string
  manuallyEntered: boolean
  executionConditionStatus: string
  executionConditionType: string
  executionConditionConfirmedAt: string
  executionConditionConfirmedByName: string
  executionConditionNote: string
}

const EXECUTION_CONDITIONS = ['PREPAYMENT_RECEIVED', 'LETTER_OF_CREDIT_RECEIVED', 'NO_PREPAYMENT_REQUIRED', 'SPECIAL_APPROVAL'] as const
const conditionOpen = ref(false)
const conditionBusy = ref(false)
const conditionRow = ref<Row | null>(null)
const conditionForm = reactive({ conditionType: '', note: '' })

function conditionLabel(type: string) {
  return type ? t(`receivableDue.executionConditions.${type}`) : '—'
}
function openCondition(row: Row) {
  conditionRow.value = row
  Object.assign(conditionForm, { conditionType: '', note: '' })
  conditionOpen.value = true
}
async function submitCondition() {
  if (!conditionRow.value || !conditionForm.conditionType) return
  conditionBusy.value = true
  try {
    await post(`/receivable-due/${conditionRow.value.contractId}/execution-condition`, conditionForm)
    conditionOpen.value = false
    ElMessage.success(t('receivableDue.executionReleased'))
    await load()
  } finally { conditionBusy.value = false }
}

const manualSuggestionRows = ref<Row[]>([])
const manualCustomerOptions = computed(() => [...new Set(manualSuggestionRows.value.map((row) => row.customerName).filter(Boolean))])
const manualContractOptions = computed(() => {
  const customer = manualForm.customerName.trim()
  return customer ? manualSuggestionRows.value.filter((row) => row.customerName === customer) : manualSuggestionRows.value
})

function applyManualContract(contractNo: string) {
  const row = manualSuggestionRows.value.find((item) => item.contractNo === contractNo)
  if (!row) return
  Object.assign(manualForm, {
    customerName: row.customerName,
    currency: row.currency,
    totalAmount: row.totalAmount,
    receivedAmount: row.receivedAmount,
    dueDate: row.dueDate || '',
  })
}

interface Entry {
  allocationId: string
  amount: string
  // 老行才可能非零：那是「不从银行那一笔出、但要把合同补满」的补足额，
  // 行上的「已收」算的是 amount + feeAmount。不显示它，明细加起来会
  // 比行上的已收少，而页面不解释差在哪。新的手工记账恒为 0。
  feeAmount: string
  currency: string
  receivedAt: string
  note: string
  reversalOf: string
  reverseReason: string
  allocatedByName: string
  allocatedAt: string
}

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 50
const view = ref('')
// 待核销 / 已完成是两条独立地址指向同一个组件——菜单高亮按精确路径相等
// 判断，用一页带 query 的写法菜单不会亮。
// 「已完成」是 ?view=done。用 query 而不是第二条路径：两个子页属于同一个
// 服务（客户对账），菜单上只该有一项，而 route.path 不变高亮才不会掉。
const isDone = computed(() => route.query.view === 'done')

function switchTab(v: string | number | boolean | undefined) {
  const q: Record<string, string> = {}
  if (keyword.value) q.keyword = keyword.value
  if (v === 'done') q.view = 'done'
  void router.push({ path: route.path, query: q })
}
const emptyText = computed(() => {
  if (isDone.value) return t('receivableDue.emptyDone')
  return view.value === 'overdue' ? t('receivableDue.emptyOverdue') : t('receivableDue.empty')
})
const keyword = ref(String(route.query.keyword ?? ''))
const loading = ref(false)

// 三个数字算的是「当前这一页之外的全局」，所以各查一次 total——
// 页面上的合计如果只统计当前页，会在翻页时变化，那就不是合计了。
const overdueCount = ref(0)
const dueSoonCount = ref(0)
const unsetCount = ref(0)

const dueSoonDays = 30

function rowClass({ row }: { row: Row }) {
  // 未填日期已有橙色标签提示，不再把整行染黄。
  if (row.dueUnset) return ''
  return row.overdueDays > 0 ? 'row-overdue' : ''
}

function dueLabel(row: Row): string {
  if (row.overdueDays > 0) return t('receivableDue.overdueBy', { n: row.overdueDays })
  if (row.overdueDays === 0) return t('receivableDue.dueToday')
  return t('receivableDue.dueIn', { n: -row.overdueDays })
}

async function fetchPage(params: Record<string, string | number>) {
  return get<{ items: Row[]; meta: { total: string } }>('/receivable-due', params)
}

async function openManual() {
  manualOpen.value = true
  try {
    const [open, done] = await Promise.all([
      fetchPage({ page: 1, page_size: 200 }),
      fetchPage({ page: 1, page_size: 200, closed: '1' }),
    ])
    manualSuggestionRows.value = [...(open.items ?? []), ...(done.items ?? [])]
  } catch {
    // 建议加载失败不影响手工录入；所有字段始终允许直接输入。
    manualSuggestionRows.value = [...rows.value]
  }
}

async function load() {
  loading.value = true
  try {
    const d = await fetchPage({
      page: page.value, page_size: pageSize,
      overdue: !isDone.value && view.value === 'overdue' ? '1' : '',
      unset: !isDone.value && view.value === 'unset' ? '1' : '',
      closed: isDone.value ? '1' : '',
      keyword: keyword.value,
    })
    rows.value = d.items ?? []
    total.value = Number(d.meta?.total ?? 0)
  } finally {
    loading.value = false
  }
}

async function loadMetrics() {
  const [overdue, unset, all] = await Promise.all([
    fetchPage({ page: 1, page_size: 1, overdue: '1' }),
    fetchPage({ page: 1, page_size: 1, unset: '1' }),
    // 「即将到期」没有独立筛子（服务端只认逾期/未配置两个），所以拉一页
    // 算：够用且不必为一个提示数字再开一个接口。
    fetchPage({ page: 1, page_size: 200 }),
  ])
  overdueCount.value = Number(overdue.meta?.total ?? 0)
  unsetCount.value = Number(unset.meta?.total ?? 0)
  dueSoonCount.value = (all.items ?? []).filter(
    (r) => !r.dueUnset && r.overdueDays <= 0 && -r.overdueDays <= dueSoonDays,
  ).length
}

// ── 记一笔收款 ────────────────────────────────────────────
//
// 选中的合同已经定了，员工只填「多少钱、哪天到的」。**不连银行流水**：
// 那本账只用来存银行给的对账单，这里一根线都不牵。
const entryOpen = ref(false)
const entryBusy = ref(false)
const entryRow = ref<Row | null>(null)
const entryForm = ref({ amount: '', isRefund: false, receivedAt: '', note: '' })
// 展开行的明细，按合同缓存——展开一次拉一次，不预先拉。
const entries = ref<Record<string, Entry[]>>({})
const detailOpen = ref(false)
const detailLoading = ref(false)
const detailRow = ref<Row | null>(null)

function entriesOf(row: Row): Entry[] {
  return entries.value[String(row.contractId)] ?? []
}

// 冲销记录点名它冲的是谁，被点名的那条就不该再有冲销按钮。
function reversedIds(row: Row): Set<string> {
  return new Set(entriesOf(row).filter((e) => Number(e.reversalOf)).map((e) => String(e.reversalOf)))
}

async function loadEntries(row: Row) {
  const d = await get<{ receipts: Entry[] }>(`/contracts/${row.contractId}/receipts`)
  entries.value = { ...entries.value, [String(row.contractId)]: d.receipts ?? [] }
}

async function openDetails(row: Row) {
  detailRow.value = row
  detailOpen.value = true
  detailLoading.value = true
  try { await loadEntries(row) } finally { detailLoading.value = false }
}

function handleRowCommand(row: Row, command: string) {
  if (command === 'detail') void openDetails(row)
  else if (command === 'receipt') openEntry(row)
  else if (command === 'close') openClose(row)
  else if (command === 'reopen') void reopenRow(row)
  else if (command === 'due') openDue(row)
  else if (command === 'edit') openEdit(row)
}

const editOpen = ref(false)
const editBusy = ref(false)
const editRow = ref<Row | null>(null)
const editForm = reactive({ customerName: '', contractNo: '', dueDate: '' })
function openEdit(row: Row) {
  editRow.value = row
  Object.assign(editForm, { customerName: row.customerName, contractNo: row.contractNo, dueDate: row.dueDate || '' })
  editOpen.value = true
}
async function saveEdit() {
  if (!editRow.value || !editForm.customerName.trim() || !editForm.contractNo.trim()) return
  editBusy.value = true
  try {
    await patch(`/receivable-due/${editRow.value.contractId}/manual`, editForm)
    editOpen.value = false
    ElMessage.success(t('receivableDue.editManualSaved'))
    reload()
  } finally { editBusy.value = false }
}

function onExpand(row: Row, expanded: Row[]) {
  if (expanded.includes(row)) void loadEntries(row)
}

function openEntry(row: Row) {
  entryRow.value = row
  entryForm.value = { amount: '', isRefund: false, receivedAt: '', note: '' }
  entryOpen.value = true
}

async function submitEntry() {
  if (!entryRow.value) return
  if (!entryForm.value.amount.trim()) {
    ElMessage.warning(t('receivableDue.entryAmountRequired'))
    return
  }
  entryBusy.value = true
  try {
    await post(`/receivable-due/${entryRow.value.contractId}/receipts`, {
      amount: entryForm.value.amount,
      isRefund: entryForm.value.isRefund,
      receivedAt: entryForm.value.receivedAt,
      note: entryForm.value.note,
    })
    entryOpen.value = false
    ElMessage.success(t('receivableDue.entrySaved'))
    await loadEntries(entryRow.value)
    reload()
  } catch { /* surfaced by the api layer */ } finally {
    entryBusy.value = false
  }
}

async function reverseEntry(row: Row, e: Entry) {
  // 冲销必须给理由，和撤销完成同一条纪律。
  const { value } = await ElMessageBox.prompt(
    t('receivableDue.entryReverseWhy'), t('receivableDue.entryReverse'),
    { inputPlaceholder: t('receivableDue.entryReverseReason') },
  ).catch(() => ({ value: '' }))
  if (!value) return
  await post(`/contract-receipts/${e.allocationId}/reverse`, { reason: value })
  ElMessage.success(t('receivableDue.entryReversed'))
  await loadEntries(row)
  reload()
}

// ── 确认核销完成 ──────────────────────────────────────────
//
// 「正常收完」排在第一档：新模型下最常见的完成理由。其余四档是「算式说
// 还欠、人说不欠了」的各种情形。
const CLOSURE_CATEGORIES = ['SETTLED', 'LOSS', 'ROUNDING', 'CANCELLED', 'OTHER'] as const
// ── 改应收到期日 ──────────────────────────────────────────
//
// 到期日是建合同时填的，之后会变。合同的常规编辑口只对草稿开放、且只放
// 销售属主过，财务改不了——所以这个门开在这里，和「确认完成」并排，用的
// 是同一套围栏（租户 + export:receipt:write）。
const dueOpen = ref(false)
const dueRow = ref<Row | null>(null)
const dueBusy = ref(false)
const dueForm = reactive({ dueDate: '', reason: '' })

function openDue(row: Row) {
  dueRow.value = row
  dueForm.dueDate = row.dueDate || ''
  dueForm.reason = ''
  dueOpen.value = true
}

async function submitDue() {
  if (!dueRow.value || !dueForm.reason.trim()) return
  dueBusy.value = true
  try {
    await post(`/receivable-due/${dueRow.value.contractId}/due-date`, {
      dueDate: dueForm.dueDate || '',
      reason: dueForm.reason.trim(),
    })
    dueOpen.value = false
    ElMessage.success(t('receivableDue.dueSaved'))
    await Promise.all([load(), loadMetrics()])
  } catch {
    // 错误提示由 api 层统一弹
  } finally {
    dueBusy.value = false
  }
}

const closeOpen = ref(false)
const closingBusy = ref(false)
const closing = ref<Row | null>(null)
const closeForm = ref({ category: 'SETTLED', note: '' })

function openClose(row: Row) {
  closing.value = row
  closeForm.value = { category: 'SETTLED', note: '' }
  closeOpen.value = true
}

async function submitClose() {
  if (!closing.value) return
  closingBusy.value = true
  try {
    await post(`/receivable-due/${closing.value.contractId}/close`, {
      category: closeForm.value.category, note: closeForm.value.note,
    })
    closeOpen.value = false
    ElMessage.success(t('receivableDue.closed'))
    reload()
  } finally {
    closingBusy.value = false
  }
}

async function reopenRow(row: Row) {
  // 撤销必须给理由，和冲销、认差撤销同一条纪律。
  const { value } = await ElMessageBox.prompt(
    t('receivableDue.reopenWhy', { no: row.contractNo }), t('receivableDue.reopen'),
    { inputPlaceholder: t('receivableDue.reopenReason') },
  ).catch(() => ({ value: '' }))
  if (!value) return
  await post(`/receivable-due/${row.contractId}/reopen`, { reason: value })
  ElMessage.success(t('receivableDue.reopened'))
  reload()
}

function reload() {
  page.value = 1
  load()
  loadMetrics()
}

watch(() => route.query.keyword, (value) => {
  keyword.value = String(value ?? '')
  reload()
})

// 待核销 ⇄ 已完成走的是两条地址、**同一个组件实例**——vue-router 原地复用，
// setup 和 onMounted 都不会再跑一遍。不盯着 path 重新拉数据的话，切过去
// 表头和按钮都变了（isDone 是响应式的），表格里却还是上一页那批数据：
// 对着一张从没确认过的合同点「撤销完成」，或者对着已完成的点「确认完成」。
// 采购寻源那个列表页早就踩过同一个坑（SourcingCasesListPage 里有同款 watch）。
watch(isDone, () => {
  view.value = ''
  entries.value = {}
  reload()
})

onMounted(() => {
  load()
  loadMetrics()
})
</script>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 14px;
  width: 100%;
  max-width: 1680px;
  min-width: 0;
  margin: 0 auto;
}
.manual-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
@media (max-width: 640px) { .manual-grid { grid-template-columns: 1fr; } }
.metrics {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  overflow: hidden;
  border: 1px solid #dce8ed;
  border-radius: 12px;
  background: #fff;
  box-shadow: 0 6px 18px rgba(25, 72, 91, .035);
}
.metric {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: baseline;
  gap: 4px 12px;
  min-height: 62px;
  overflow: hidden;
  padding: 10px 16px 10px 30px;
  border: 0;
  border-radius: 0;
  background: transparent;
  box-shadow: none;
}
.metric + .metric { border-left: 1px solid #e5edf1; }
.metric::before {
  position: absolute;
  top: 17px;
  left: 16px;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #4ac1ff;
  content: '';
}
.metric.is-alarm {
  background: #fffafb;
}
.metric.is-alarm::before { background: #ff6b72; }
.metric.is-warn {
  background: #fffdf9;
}
.metric.is-warn::before { background: #e5a33b; }
.metric-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.metric-value {
  color: #141817;
  font-size: 22px;
  line-height: 1.15;
  font-variant-numeric: tabular-nums;
}
.metric-hint {
  grid-column: 1 / -1;
  overflow: hidden;
  font-size: 12px;
  line-height: 1.35;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--el-text-color-placeholder);
}
.panel {
  min-width: 0;
  padding: 14px 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-color: #dceaf0;
  border-radius: 12px;
  background: #fff;
  box-shadow: 0 8px 26px rgba(25, 72, 91, .05);
}
.page :deep(.el-table) { --el-table-header-bg-color: #eef9fe; --el-table-header-text-color: #24323a; --el-table-row-hover-bg-color: #f0fbf6; }
.page :deep(.el-table th.el-table__cell) { border-bottom-color: #d9edf5; font-weight: 650; }
.filters {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.sub.overdue,
.num.overdue {
  color: var(--el-color-danger);
}
.num {
  font-variant-numeric: tabular-nums;
}
.num.money {
  font-weight: 600;
}
.num.dim {
  color: var(--el-text-color-placeholder);
}
.doc-link {
  color: var(--el-color-primary);
  text-decoration: none;
}
.doc-link:hover {
  text-decoration: underline;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
:deep(.row-overdue) {
  background: rgba(245, 108, 108, 0.055);
}
.row-actions { display: flex; align-items: center; white-space: nowrap; }
.drop-arrow { margin-left: 7px; font-size: 10px; }
.detail-summary { margin-bottom: 18px; }
.detail-heading { margin: 0 0 10px; font-size: 15px; }
.close-target {
  margin: 0 0 10px;
  font-weight: 600;
}
.condition-form { margin-top: 16px; }
.condition-options { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; width: 100%; }
.condition-options :deep(.el-radio) { margin: 0; min-height: 42px; }
.condition-ready { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; }
.close-figures {
  display: flex;
  gap: 24px;
  margin-bottom: 14px;
}
.close-figures > div {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.close-figures .warn {
  color: var(--el-color-warning);
}
@media (max-width: 768px) {
  .metrics { grid-template-columns: 1fr; }
  .metric + .metric { border-top: 1px solid #e5edf1; border-left: 0; }
  .panel { padding: 12px; }
  .filters { align-items: stretch; }
  .filters :deep(.el-input) { width: 100%; max-width: none !important; }
  .pager { justify-content: flex-start; overflow-x: auto; }
  .condition-options { grid-template-columns: 1fr; }
}
</style>
