<template>
  <div class="page">
    <WorkflowPageHeader :title="t('supplierRecon.title')" :description="t('supplierRecon.subtitle')">
      <template #actions><el-button v-if="canWrite" type="primary" @click="openManual">{{ t('supplierRecon.addManual') }}</el-button></template>
    </WorkflowPageHeader>

    <el-dialog v-model="manualOpen" :title="t('supplierRecon.addManual')" width="min(560px, 94vw)" destroy-on-close>
      <el-form label-position="top">
        <el-form-item :label="t('supplierRecon.manualSupplier')" required>
          <el-select v-model="manualForm.supplierName" filterable allow-create default-first-option clearable :placeholder="t('supplierRecon.manualPickOrEnter')" style="width: 100%">
            <el-option v-for="name in manualSupplierOptions" :key="name" :label="name" :value="name" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('supplierRecon.manualOrder')" required>
          <el-select v-model="manualForm.orderNo" filterable allow-create default-first-option clearable :placeholder="t('supplierRecon.manualPickOrEnter')" style="width: 100%" @change="applyManualOrder">
            <el-option v-for="item in manualOrderOptions" :key="item.poNo" :label="`${item.poNo} · ${item.supplierName}`" :value="item.poNo" />
          </el-select>
        </el-form-item>
        <div class="manual-grid">
          <el-form-item :label="t('supplierRecon.manualTotal')" required><el-input v-model="manualForm.totalAmount"><template #prepend><el-select v-model="manualForm.currency" filterable allow-create default-first-option :placeholder="t('supplierRecon.manualCurrencyHint')" style="width: 110px"><el-option v-for="c in ['CNY','USD','EUR','GBP','JPY','CAD','AUD','HKD']" :key="c" :value="c" /></el-select></template></el-input></el-form-item>
          <el-form-item :label="t('supplierRecon.manualPaid')"><el-input v-model="manualForm.paidAmount" /></el-form-item>
        </div>
        <el-form-item :label="t('supplierRecon.manualDate')"><el-date-picker v-model="manualForm.paidAt" type="date" value-format="YYYY-MM-DD" style="width: 100%" /></el-form-item>
        <el-form-item :label="t('supplierRecon.manualDueDate')"><el-date-picker v-model="manualForm.dueDate" type="date" value-format="YYYY-MM-DD" clearable style="width: 100%" /></el-form-item>
        <el-form-item :label="t('supplierRecon.entryNote')"><el-input v-model="manualForm.note" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="manualOpen = false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="manualBusy" @click="saveManual">{{ t('common.save') }}</el-button></template>
    </el-dialog>

    <!-- 三个数字回答「今天还有多少活」。全是全局合计，不是当前页——
         页面上的合计如果跟着翻页变，那就不是合计了。 -->
    <section class="metrics">
      <div class="metric" :class="{ 'is-alarm': overdueCount > 0 }">
        <span class="metric-label">{{ t('supplierRecon.overdueCount') }}</span>
        <strong class="metric-value">{{ overdueCount }}</strong>
        <span class="metric-hint">{{ t('supplierRecon.overdueHint') }}</span>
      </div>
      <div class="metric">
        <span class="metric-label">{{ t('supplierRecon.dueSoonCount') }}</span>
        <strong class="metric-value">{{ dueSoonCount }}</strong>
        <span class="metric-hint">{{ t('supplierRecon.dueSoonHint') }}</span>
      </div>
      <div class="metric" :class="{ 'is-warn': unsetCount > 0 }">
        <span class="metric-label">{{ t('supplierRecon.unsetCount') }}</span>
        <strong class="metric-value">{{ unsetCount }}</strong>
        <span class="metric-hint">{{ t('supplierRecon.unsetHint') }}</span>
      </div>
    </section>

    <section class="panel">
      <div class="filters">
        <!-- 主页签：供应商对账下面的两个子页。状态放在地址的 query 里，
             前进/后退和分享链接都对；菜单高亮看的是 route.path，不受
             query 影响，仍然稳稳停在「供应商对账」上。 -->
        <el-radio-group :model-value="isDone ? 'done' : 'open'" class="tabs" @update:model-value="switchTab">
          <el-radio-button value="open">{{ t('supplierRecon.tabOpen') }}</el-radio-button>
          <el-radio-button value="done">{{ t('supplierRecon.tabDone') }}</el-radio-button>
        </el-radio-group>
        <!-- 次级筛子，只在待核销那一档下有意义：已完成的单不用再催。 -->
        <el-radio-group v-if="!isDone" v-model="view" @change="reload">
          <el-radio-button value="">{{ t('supplierRecon.viewAll') }}</el-radio-button>
          <el-radio-button value="overdue">{{ t('supplierRecon.viewOverdue') }}</el-radio-button>
          <el-radio-button value="unset">{{ t('supplierRecon.viewUnset') }}</el-radio-button>
        </el-radio-group>
        <el-input
          v-model="keyword"
          clearable
          :placeholder="t('supplierRecon.search')"
          style="max-width: 260px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table v-loading="loading" :data="rows" :row-class-name="rowClass" @expand-change="onExpand">
        <!-- 展开看这张采购单付过哪几笔。明细按需加载：每行都预先拉一次，
             一页就是 20 次往返。 -->
        <el-table-column type="expand">
          <template #default="{ row }">
            <div class="entries">
              <el-table v-if="entriesOf(row).length" :data="entriesOf(row)" size="small">
                <el-table-column :label="t('supplierRecon.entryDate')" width="120">
                  <template #default="{ row: e }">{{ e.paidAt || '—' }}</template>
                </el-table-column>
                <el-table-column :label="t('supplierRecon.entryAmount')" width="160" align="right">
                  <template #default="{ row: e }">
                    <span :class="{ negative: Number(e.amount) < 0 }">{{ e.currency }} {{ e.amount }}</span>
                    <div v-if="Number(e.feeAmount)" class="sub">
                      {{ t('supplierRecon.entryFee', { n: e.feeAmount }) }}
                    </div>
                  </template>
                </el-table-column>
                <el-table-column :label="t('supplierRecon.entrySource')" width="180">
                  <template #default="{ row: e }">
                    <span v-if="e.paymentNo">{{ e.paymentNo }}</span>
                    <span v-else class="sub">{{ t('supplierRecon.entryByHand') }}</span>
                    <!-- 这笔钱核销在发票上，只是那张发票有行指向本单。它算进
                         本单的已付，但冲掉它会同时影响这张发票关联的其它单。 -->
                    <div v-if="e.invoiceNo" class="sub warn-note">
                      {{ t('supplierRecon.entryViaInvoice', { no: e.invoiceNo }) }}
                    </div>
                  </template>
                </el-table-column>
                <el-table-column :label="t('supplierRecon.entryNote')" min-width="180">
                  <template #default="{ row: e }">
                    <span v-if="Number(e.reversalOf)">{{ t('supplierRecon.entryReversal') }} · {{ e.reverseReason }}</span>
                    <span v-else>{{ e.note || '—' }}</span>
                  </template>
                </el-table-column>
                <el-table-column :label="t('supplierRecon.entryBy')" width="160">
                  <template #default="{ row: e }">{{ e.allocatedBy }}<div class="sub">{{ e.allocatedAt }}</div></template>
                </el-table-column>
              </el-table>
              <p v-else class="sub">{{ t('supplierRecon.entriesEmpty') }}</p>

              <!-- 凭证。发票页下线之后「留凭证」搬到了这儿——需求原话是
                   「供应商发票只是员工用来上传留凭证用的」，所以这里存的是
                   纸，不是有金额、参与运算的单据。一张单可以有好几份。 -->
              <div class="files">
                <div class="files-head">
                  <span class="files-title">{{ t('supplierRecon.files') }}</span>
                  <span class="sub">{{ t('supplierRecon.filesHint') }}</span>
                </div>
                <ul v-if="filesOf(row).length" class="file-list">
                  <li v-for="f in filesOf(row)" :key="f.id">
                    <a v-if="f.url" :href="f.url" target="_blank" rel="noopener" class="doc-link">{{ f.fileName }}</a>
                    <span v-else>{{ f.fileName }}</span>
                    <span v-if="f.note" class="sub"> · {{ f.note }}</span>
                    <span class="sub"> · {{ t('supplierRecon.fileBy', { name: f.uploadedByName, at: f.uploadedAt }) }}</span>
                    <el-button v-if="canWrite" link type="danger" size="small" @click="removeFile(row, f)">
                      {{ t('supplierRecon.fileRemove') }}
                    </el-button>
                  </li>
                </ul>
                <p v-else class="sub">{{ t('supplierRecon.filesEmpty') }}</p>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.order')" min-width="190">
          <template #default="{ row }">
            <!-- 只有真打得开采购订单页的人才给链接。本页的主要使用者是财务，
                 而财务没有 procurement:order:read——给所有人挂链接等于每一行
                 都放了一个点下去必然 403 的入口。 -->
            <router-link v-if="canOpenOrders" :to="`/purchase-orders?keyword=${row.poNo}`" class="doc-link">{{ row.poNo }}</router-link>
            <span v-else class="po-no">{{ row.poNo }}</span>
            <div class="sub">{{ row.supplierName }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.openAmount')" width="170" align="right">
          <template #default="{ row }">
            <span class="num money" :class="{ over: Number(row.openAmount) < 0 }">
              {{ row.currency }} {{ row.openAmount }}
            </span>
            <div v-if="Number(row.openAmount) < 0" class="sub over">{{ t('supplierRecon.overpaid') }}</div>
            <div v-else class="sub">{{ t('supplierRecon.ofTotal', { total: row.orderedAmount }) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.paid')" width="150" align="right">
          <template #default="{ row }">
            <span class="num" :class="{ dim: Number(row.paidAmount) === 0 }">{{ row.paidAmount }}</span>
            <!-- 走发票那条路的钱在明细里看不见（那些核销行挂在发票上，不挂
                 采购单）。不说明来处，这段差额会被当成漏记而重填一遍。 -->
            <div v-if="Number(row.invoicePaidAmount)" class="sub">
              {{ t('supplierRecon.viaInvoice', { n: row.invoicePaidAmount }) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.orderStatus')" width="130">
          <template #default="{ row }">
            <el-tag size="small" effect="plain" :type="row.orderStatus === 'CANCELLED' ? 'info' : undefined">
              {{ t(`supplierRecon.orderStatuses.${row.orderStatus}`) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.dueDate')" width="150">
          <template #default="{ row }">
            <!-- 没配账期不是「今天到期」，所以给一个标签而不是一个日子。 -->
            <el-tag v-if="row.dueUnset" size="small" type="warning" effect="plain">
              {{ t('supplierRecon.unsetTag') }}
            </el-tag>
            <template v-else>
              <div class="num-cell">{{ row.dueDate }}</div>
              <!-- 已完成页只留日子，不留「逾期多少天」——那笔账已经了结了。 -->
              <div v-if="!isDone" class="sub" :class="{ overdue: row.overdueDays > 0 }">
                {{ dueLabel(row) }}
              </div>
            </template>
          </template>
        </el-table-column>
        <el-table-column :label="t('supplierRecon.buyer')" min-width="110">
          <template #default="{ row }">{{ row.buyerName || '—' }}</template>
        </el-table-column>
        <!-- 已完成视图多一列：为什么算完了、谁说的。 -->
        <el-table-column v-if="isDone" :label="t('supplierRecon.closedWhy')" min-width="180">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ t(`supplierRecon.closureCategories.${row.closedCategory}`) }}</el-tag>
            <div class="sub">{{ row.closedByName }}<template v-if="row.closedNote"> · {{ row.closedNote }}</template></div>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-if="isDone || !canWrite" type="primary" plain @click="handleRowCommand(row, 'detail')">
              {{ t('supplierRecon.viewDetails') }}
            </el-button>
            <el-dropdown v-else trigger="click" @command="(command: string) => handleRowCommand(row, command)">
              <el-button type="primary" plain>{{ t('supplierRecon.moreActions') }}<span class="drop-arrow">▼</span></el-button>
              <template #dropdown><el-dropdown-menu>
                <el-dropdown-item command="detail">{{ t('supplierRecon.viewDetails') }}</el-dropdown-item>
                <el-dropdown-item v-if="row.manuallyEntered" command="edit">{{ t('supplierRecon.editManual') }}</el-dropdown-item>
                <el-dropdown-item divided command="payment">{{ t('supplierRecon.addPayment') }}</el-dropdown-item>
                <el-dropdown-item command="upload">{{ t('supplierRecon.fileUpload') }}</el-dropdown-item>
                <el-dropdown-item command="close">{{ t('supplierRecon.close') }}</el-dropdown-item>
                <el-dropdown-item command="due">{{ t('supplierRecon.dueEdit') }}</el-dropdown-item>
              </el-dropdown-menu></template>
            </el-dropdown>
          </template>
        </el-table-column>
        <template #empty>{{ emptyText }}</template>
      </el-table>

      <input ref="fileInput" type="file" accept="application/pdf,image/*" style="display: none" @change="onFilePicked" />

      <el-pagination
        class="pager"
        layout="total, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        @current-change="(p: number) => { page = p; load() }"
      />
    </section>

    <el-dialog v-model="detailOpen" :title="t('supplierRecon.detailTitle')" width="min(860px, 96vw)">
      <template v-if="detailRow">
        <p class="close-target">{{ detailRow.poNo }} · {{ detailRow.supplierName }}</p>
        <el-descriptions :column="3" border class="detail-summary">
          <el-descriptions-item :label="t('supplierRecon.manualTotal')">{{ detailRow.currency }} {{ detailRow.orderedAmount }}</el-descriptions-item>
          <el-descriptions-item :label="t('supplierRecon.manualPaid')">{{ detailRow.paidAmount }}</el-descriptions-item>
          <el-descriptions-item :label="t('supplierRecon.openAmount')">{{ detailRow.openAmount }}</el-descriptions-item>
          <el-descriptions-item :label="t('supplierRecon.dueDate')">{{ detailRow.dueDate || t('supplierRecon.unsetTag') }}</el-descriptions-item>
          <el-descriptions-item :label="t('supplierRecon.buyer')">{{ detailRow.buyerName || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('supplierRecon.detailStatus')">{{ isDone ? t('supplierRecon.tabDone') : t('supplierRecon.tabOpen') }}</el-descriptions-item>
        </el-descriptions>
        <h3 class="detail-heading">{{ t('supplierRecon.detailHistory') }}</h3>
        <el-table v-loading="detailLoading" :data="entriesOf(detailRow)" size="small">
          <el-table-column :label="t('supplierRecon.entryDate')" prop="paidAt" width="120" />
          <el-table-column :label="t('supplierRecon.entryAmount')" width="150"><template #default="{ row: e }">{{ e.currency }} {{ e.amount }}</template></el-table-column>
          <el-table-column :label="t('supplierRecon.entryNote')" min-width="180"><template #default="{ row: e }">{{ Number(e.reversalOf) ? `${t('supplierRecon.entryReversal')} · ${e.reverseReason}` : (e.note || '—') }}</template></el-table-column>
          <el-table-column :label="t('supplierRecon.entryBy')" min-width="180"><template #default="{ row: e }">{{ e.allocatedBy || '—' }}<div class="sub">{{ e.allocatedAt }}</div></template></el-table-column>
          <el-table-column v-if="canWrite && !isDone" :label="t('common.actions')" width="90">
            <template #default="{ row: e }"><el-button v-if="!Number(e.reversalOf) && !reversedIds(detailRow).has(String(e.allocationId))" link type="danger" @click="reverseEntry(detailRow, e)">{{ t('supplierRecon.entryReverse') }}</el-button></template>
          </el-table-column>
          <template #empty>{{ t('supplierRecon.entriesEmpty') }}</template>
        </el-table>
      </template>
    </el-dialog>

    <el-dialog v-model="editOpen" :title="t('supplierRecon.editManual')" width="min(520px, 94vw)" destroy-on-close>
      <el-form label-position="top">
        <el-form-item :label="t('supplierRecon.manualSupplier')" required><el-input v-model="editForm.supplierName" /></el-form-item>
        <el-form-item :label="t('supplierRecon.manualOrder')" required><el-input v-model="editForm.orderNo" /></el-form-item>
        <el-form-item :label="t('supplierRecon.manualDueDate')"><el-date-picker v-model="editForm.dueDate" type="date" value-format="YYYY-MM-DD" clearable style="width:100%" /></el-form-item>
        <el-alert :closable="false" type="info" :title="t('supplierRecon.editManualHint')" />
      </el-form>
      <template #footer><el-button @click="editOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="editBusy" @click="saveEdit">{{ t('common.save') }}</el-button></template>
    </el-dialog>

    <!-- 改应付到期日。这个日子决定这张单算不算逾期，所以理由必填，
         改动会连同旧值新值一起留痕。 -->
    <el-dialog v-model="dueOpen" :title="t('supplierRecon.dueEdit')" width="min(460px, 94vw)" destroy-on-close>
      <template v-if="dueRow">
        <p class="close-target">{{ dueRow.poNo }} · {{ dueRow.supplierName }}</p>
        <el-form label-position="top">
          <el-form-item :label="t('supplierRecon.dueDate')">
            <el-date-picker
              v-model="dueForm.dueDate"
              type="date"
              value-format="YYYY-MM-DD"
              clearable
              :placeholder="t('supplierRecon.dueEmpty')"
              style="width: 100%"
            />
          </el-form-item>
          <el-form-item :label="t('supplierRecon.dueWhy')">
            <el-input
              v-model="dueForm.reason"
              type="textarea"
              :rows="2"
              :placeholder="t('supplierRecon.dueWhyHint')"
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

    <!-- 确认核销完成。三个数并排亮着，员工看着差额自己拿主意——**系统不
         替他判断**：差 7000 也能确认完成，分毫不差也不会自动完成。 -->
    <el-dialog v-model="closeOpen" :title="t('supplierRecon.closeTitle')" width="min(520px, 94vw)" destroy-on-close>
      <template v-if="closing">
        <p class="close-target">{{ closing.poNo }} · {{ closing.supplierName }}</p>
        <div class="close-figures">
          <div><span class="sub">{{ t('supplierRecon.figOrdered') }}</span><span class="num">{{ closing.currency }} {{ closing.orderedAmount }}</span></div>
          <div><span class="sub">{{ t('supplierRecon.figPaid') }}</span><span class="num">{{ closing.paidAmount }}</span></div>
          <div><span class="sub">{{ t('supplierRecon.figOpen') }}</span><span class="num warn">{{ closing.openAmount }}</span></div>
        </div>
        <el-form label-position="top">
          <el-form-item :label="t('supplierRecon.closeWhat')">
            <el-radio-group v-model="closeForm.category">
              <el-radio v-for="k in CLOSURE_CATEGORIES" :key="k" :value="k">
                {{ t(`supplierRecon.closureCategories.${k}`) }}
              </el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item :label="t('supplierRecon.closeNote')">
            <el-input v-model="closeForm.note" :placeholder="t('supplierRecon.closeNoteHint')" />
          </el-form-item>
        </el-form>
        <el-alert type="info" :closable="false" show-icon :title="t('supplierRecon.closeHint')" />
      </template>
      <template #footer>
        <el-button @click="closeOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="closingBusy" @click="submitClose">{{ t('supplierRecon.closeConfirm') }}</el-button>
      </template>
    </el-dialog>

    <!-- 记一笔付款：选中的采购单已经定了，员工只填「多少钱、哪天付的」。
         币种跟采购单走，不问——只有一个正确答案的问题只会制造错答案。 -->
    <el-dialog v-model="entryOpen" :title="t('supplierRecon.addPaymentTitle')" width="min(520px, 94vw)" destroy-on-close>
      <template v-if="entryRow">
        <p class="close-target">{{ entryRow.poNo }} · {{ entryRow.supplierName }}</p>
        <div class="close-figures">
          <div><span class="sub">{{ t('supplierRecon.figOrdered') }}</span><span class="num">{{ entryRow.currency }} {{ entryRow.orderedAmount }}</span></div>
          <div><span class="sub">{{ t('supplierRecon.figPaid') }}</span><span class="num">{{ entryRow.paidAmount }}</span></div>
          <div><span class="sub">{{ t('supplierRecon.figOpen') }}</span><span class="num warn">{{ entryRow.openAmount }}</span></div>
        </div>
        <el-form label-position="top">
          <el-form-item :label="t('supplierRecon.entryKind')">
            <el-radio-group v-model="entryForm.isRefund">
              <el-radio-button :value="false">{{ t('supplierRecon.kindPayment') }}</el-radio-button>
              <el-radio-button :value="true">{{ t('supplierRecon.kindRefund') }}</el-radio-button>
            </el-radio-group>
          </el-form-item>
          <el-form-item :label="t('supplierRecon.entryAmount')">
            <el-input v-model="entryForm.amount" :placeholder="t('supplierRecon.entryAmountHint')">
              <template #prepend>{{ entryRow.currency }}</template>
            </el-input>
          </el-form-item>
          <el-form-item :label="t('supplierRecon.entryDate')">
            <el-date-picker v-model="entryForm.paidAt" type="date" value-format="YYYY-MM-DD" style="width: 100%" />
          </el-form-item>
          <el-form-item :label="t('supplierRecon.entryNote')">
            <el-input v-model="entryForm.note" :placeholder="t('supplierRecon.entryNoteHint')" />
          </el-form-item>
        </el-form>
        <el-alert type="info" :closable="false" show-icon :title="t('supplierRecon.entryHint')" />
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
import { backfillRequest, get, patch, post } from '../api'
import { newIdempotencySession, withIdempotency } from '../lib/idempotency'
import { useAuthStore } from '../stores/auth'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
// 和网关那行 s.perm("procurement:recon:write") 逐字一致。差一个字就是
// 「看得见按钮、点下去 403」，或者更糟，反过来。
const canWrite = auth.can('procurement:recon:write')
const manualOpen = ref(false)
const manualBusy = ref(false)
const manualForm = reactive({ supplierName: '', orderNo: '', currency: 'CNY', totalAmount: '', paidAmount: '', paidAt: '', dueDate: '', note: '' })

async function saveManual() {
  if (!manualForm.supplierName.trim() || !manualForm.orderNo.trim() || !manualForm.totalAmount.trim()) {
    ElMessage.warning(t('supplierRecon.manualRequired')); return
  }
  manualBusy.value = true
  try {
    await post('/supplier-recon/manual', manualForm)
    manualOpen.value = false
    Object.assign(manualForm, { supplierName: '', orderNo: '', currency: 'CNY', totalAmount: '', paidAmount: '', paidAt: '', dueDate: '', note: '' })
    ElMessage.success(t('supplierRecon.manualSaved'))
    reload()
  } finally { manualBusy.value = false }
}
// 采购订单页自己的门。财务通常没有，所以行上的单号对它是纯文本。
const canOpenOrders = auth.can('procurement:order:read')

interface Row {
  poId: string
  poNo: string
  supplierId: string
  supplierName: string
  currency: string
  orderStatus: string
  buyerName: string
  orderedDate: string
  expectedDate: string
  // 应付到期日 = 下单当天 + 供应商账期。空串 = 没配账期（dueUnset 为真），
  // 不是「今天到期」。
  dueDate: string
  overdueDays: number
  dueUnset: boolean
  orderedAmount: string
  paidAmount: string
  openAmount: string
  // paidAmount 里走「发票 → 付款单核销」那条老路进来的部分。
  invoicePaidAmount: string
  closedCategory: string
  closedNote: string
  closedByName: string
  closedAt: string
  manuallyEntered: boolean
}

const manualSuggestionRows = ref<Row[]>([])
const manualSupplierOptions = computed(() => [...new Set(manualSuggestionRows.value.map((row) => row.supplierName).filter(Boolean))])
const manualOrderOptions = computed(() => {
  const supplier = manualForm.supplierName.trim()
  return supplier ? manualSuggestionRows.value.filter((row) => row.supplierName === supplier) : manualSuggestionRows.value
})

function applyManualOrder(orderNo: string) {
  const row = manualSuggestionRows.value.find((item) => item.poNo === orderNo)
  if (!row) return
  Object.assign(manualForm, {
    supplierName: row.supplierName,
    currency: row.currency,
    totalAmount: row.orderedAmount,
    paidAmount: row.paidAmount,
    dueDate: row.dueDate || '',
  })
}

interface Entry {
  allocationId: string
  paidAt: string
  amount: string
  // 只有改造前从付款单分配来的老行才可能非零。行上的「已付」不含它，
  // 所以这里单独标一行说明，否则明细加起来会比行上的已付少，页面不解释
  // 差在哪。手填行恒为 0。
  feeAmount: string
  currency: string
  note: string
  allocatedAt: string
  allocatedBy: string
  reversalOf: string
  reverseReason: string
  // 老行带着付款单号；手填行为空。
  paymentNo: string
  // 非空 = 这笔钱核销在发票上，只是那张发票有行指向本单。
  invoiceNo: string
}

const rows = ref<Row[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
// 「已完成」是 ?view=done。用 query 而不是第二条路径：两个子页属于同一个
// 服务（供应商对账），菜单上只该有一项，而 route.path 不变高亮才不会掉。
const isDone = computed(() => route.query.view === 'done')

function switchTab(v: string | number | boolean | undefined) {
  const q: Record<string, string> = {}
  if (keyword.value) q.keyword = keyword.value
  if (v === 'done') q.view = 'done'
  void router.push({ path: route.path, query: q })
}

const emptyText = computed(() =>
  isDone.value ? t('supplierRecon.emptyDone') : t('supplierRecon.empty'),
)
const keyword = ref(String(route.query.keyword ?? ''))
const loading = ref(false)

// 三个数字算的是「当前这一页之外的全局」，所以各查一次 total——页面上的
// 合计如果只统计当前页，会在翻页时变化，那就不是合计了。
const overdueCount = ref(0)
const dueSoonCount = ref(0)
const unsetCount = ref(0)

const dueSoonDays = 30
const view = ref('')

// 已完成页上的到期日是历史，不是待办：一张 2023 年就结清的单不该顶着
// 「逾期 700 天」的红底。催的是没结的账，结了的只剩记录。
function rowClass({ row }: { row: Row }) {
  if (isDone.value) return ''
  // 未填日期已有橙色标签提示，不再把整行染黄，避免大量旧单铺满警告色。
  if (row.dueUnset) return ''
  return row.overdueDays > 0 ? 'row-overdue' : ''
}

function dueLabel(row: Row): string {
  if (isDone.value) return ''
  if (row.overdueDays > 0) return t('supplierRecon.overdueBy', { n: row.overdueDays })
  if (row.overdueDays === 0) return t('supplierRecon.dueToday')
  return t('supplierRecon.dueIn', { n: -row.overdueDays })
}

async function fetchPage(params: Record<string, string | number>) {
  return get<{ items: Row[]; total: string }>('/supplier-recon', params)
}

async function openManual() {
  manualOpen.value = true
  try {
    const [open, done] = await Promise.all([
      fetchPage({ page: 1, page_size: 200 }),
      fetchPage({ page: 1, page_size: 200, view: 'done' }),
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
      view: isDone.value ? 'done' : '',
      overdue: !isDone.value && view.value === 'overdue' ? '1' : '',
      unset: !isDone.value && view.value === 'unset' ? '1' : '',
      keyword: keyword.value,
    })
    rows.value = d.items ?? []
    total.value = Number(d.total ?? 0)
  } catch {
    // 失败时必须清空，不能把上一次的数据留在表上。表头和操作列已经按
    // 当前页签渲染了——留着旧数据，就会在「已完成」页里列出从没确认过的
    // 单，每行挂着一个「撤销完成」按钮。错误提示由 api 层统一弹。
    rows.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

async function loadMetrics() {
  const [overdue, unset, sample] = await Promise.all([
    fetchPage({ page: 1, page_size: 1, overdue: '1' }),
    fetchPage({ page: 1, page_size: 1, unset: '1' }),
    // 「30 天内到期」没有独立筛子（服务端只认逾期/未配置两个），所以拉一页
    // 算：够用且不必为一个提示数字再开一个接口。
    fetchPage({ page: 1, page_size: 200 }),
  ])
  overdueCount.value = Number(overdue.total ?? 0)
  unsetCount.value = Number(unset.total ?? 0)
  dueSoonCount.value = (sample.items ?? []).filter(
    (r) => !r.dueUnset && r.overdueDays <= 0 && -r.overdueDays <= dueSoonDays,
  ).length
}

// ── 改应付到期日 ──────────────────────────────────────────
//
// 到期日是建单时填的，之后会变：谈判改了付款条件，或者一开始就填错。
// 而采购单本身**已下单之后没有编辑入口**（表单只对草稿和被驳回的单开放），
// 所以这个门开在这里。
//
// 理由必填。它直接决定这张单算不算逾期——把日子往后推一个月，页面上的
// 「已逾期」就少一条，这件事不留痕就查不出来。
const dueOpen = ref(false)
const dueRow = ref<Row | null>(null)
const dueBusy = ref(false)
const dueForm = reactive({ dueDate: '', reason: '' })

function openDue(row: Row) {
  dueRow.value = row
  // 带出当前值，改的人看得见自己在改什么。清空也是合法的一次改动。
  dueForm.dueDate = row.dueDate || ''
  dueForm.reason = ''
  dueOpen.value = true
}

async function submitDue() {
  if (!dueRow.value || !dueForm.reason.trim()) return
  dueBusy.value = true
  try {
    await post(`/supplier-recon/${dueRow.value.poId}/due-date`, {
      dueDate: dueForm.dueDate || '',
      reason: dueForm.reason.trim(),
    })
    dueOpen.value = false
    ElMessage.success(t('supplierRecon.dueSaved'))
    // 用 load 不用 reload：改完还停在原来那一页。填完之后这一行可能不再
    // 属于当前筛子（比如刚从「只看未填」里填好），重拉一次自然就对了。
    await Promise.all([load(), loadMetrics()])
  } catch {
    // 错误提示由 api 层统一弹
  } finally {
    dueBusy.value = false
  }
}

// ── 记一笔付款 ────────────────────────────────────────────
//
// 选中的采购单已经定了，员工只填「多少钱、哪天付的」。**不连银行流水、
// 也不看发票**：流水那本账只用来存银行给的对账单，发票只是凭证。
const entryOpen = ref(false)
const entryBusy = ref(false)
const entryRow = ref<Row | null>(null)
const entryForm = ref({ amount: '', isRefund: false, paidAt: '', note: '' })
// 付款重复记一笔就是真金白银记两遍——网络抖一下重发，账上多一笔。
const payIdem = newIdempotencySession()
// 展开行的明细，按采购单缓存——展开一次拉一次，不预先拉。
const entries = ref<Record<string, Entry[]>>({})
const detailOpen = ref(false)
const detailLoading = ref(false)
const detailRow = ref<Row | null>(null)

function entriesOf(row: Row): Entry[] {
  return entries.value[String(row.poId)] ?? []
}

// 冲销记录点名它冲的是谁，被点名的那条就不该再有冲销按钮。
function reversedIds(row: Row): Set<string> {
  return new Set(entriesOf(row).filter((e) => Number(e.reversalOf)).map((e) => String(e.reversalOf)))
}

async function loadEntries(row: Row) {
  const d = await get<{ items: Entry[] }>(`/supplier-recon/${row.poId}/payments`)
  entries.value = { ...entries.value, [String(row.poId)]: d.items ?? [] }
}

async function openDetails(row: Row) {
  detailRow.value = row
  detailOpen.value = true
  detailLoading.value = true
  try { await loadEntries(row) } finally { detailLoading.value = false }
}

function handleRowCommand(row: Row, command: string) {
  if (command === 'detail') void openDetails(row)
  else if (command === 'payment') openEntry(row)
  else if (command === 'close') openClose(row)
  else if (command === 'reopen') void reopenRow(row)
  else if (command === 'due') openDue(row)
  else if (command === 'upload') pickFile(row)
  else if (command === 'edit') openEdit(row)
}

const editOpen = ref(false)
const editBusy = ref(false)
const editRow = ref<Row | null>(null)
const editForm = reactive({ supplierName: '', orderNo: '', dueDate: '' })
function openEdit(row: Row) {
  editRow.value = row
  Object.assign(editForm, { supplierName: row.supplierName, orderNo: row.poNo, dueDate: row.dueDate || '' })
  editOpen.value = true
}
async function saveEdit() {
  if (!editRow.value || !editForm.supplierName.trim() || !editForm.orderNo.trim()) return
  editBusy.value = true
  try {
    await patch(`/supplier-recon/${editRow.value.poId}/manual`, editForm)
    editOpen.value = false
    ElMessage.success(t('supplierRecon.editManualSaved'))
    reload()
  } finally { editBusy.value = false }
}

function onExpand(row: Row, expanded: Row[]) {
  if (expanded.includes(row)) {
    void loadEntries(row)
    void loadFiles(row)
  }
}

// ── 凭证 ──────────────────────────────────────────────────
//
// 发票、水单、退款回执。供应商发票页下线之后「留凭证」搬到了这里。
// 三步直传和银行流水那份对账单同一套（lib/statementUpload 那三步漏一步
// 都不报错、纸只是悄悄没上去），只是这边一张单可以挂好几份。
interface ReconFile {
  id: string
  fileName: string
  note: string
  url: string
  uploadedByName: string
  uploadedAt: string
}

const files = ref<Record<string, ReconFile[]>>({})
const fileInput = ref<HTMLInputElement | null>(null)
const uploadingPO = ref('')
const pendingRow = ref<Row | null>(null)

function filesOf(row: Row): ReconFile[] {
  return files.value[String(row.poId)] ?? []
}

async function loadFiles(row: Row) {
  const d = await get<{ items: ReconFile[] }>(`/supplier-recon/${row.poId}/files`)
  files.value = { ...files.value, [String(row.poId)]: d.items ?? [] }
}

function pickFile(row: Row) {
  pendingRow.value = row
  fileInput.value?.click()
}

async function onFilePicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  const row = pendingRow.value
  // input 先清空：同一个文件连选两次，不清的话 change 不会再触发。
  input.value = ''
  if (!file || !row) return
  // 说明这张纸是什么。取消（value 为 null）也照传——备注是锦上添花，
  // 不该拦住上传本身。
  const { value } = await ElMessageBox.prompt(
    t('supplierRecon.fileNotePlaceholder'), t('supplierRecon.fileUpload'),
    { inputValue: file.name.replace(/\.[^.]+$/, '') },
  ).catch(() => ({ value: '' }))
  uploadingPO.value = String(row.poId)
  try {
    const signed = await post<{ key: string; uploadUrl: string }>(
      `/supplier-recon/${row.poId}/files/presign`, { fileName: file.name })
    const put = await fetch(signed.uploadUrl, { method: 'PUT', body: file })
    // fetch 对 4xx/5xx 不 reject，只把 ok 置 false。不显式检查的话，一个
    // 403 的直传会一路走到登记那一步，把一个根本不存在的 key 挂上去——
    // 列表里于是出现一个点开是 404 的链接，而页面写着「已上传」。
    if (!put.ok) throw new Error(`upload failed: ${put.status}`)
    const d = await post<{ items: ReconFile[] }>(`/supplier-recon/${row.poId}/files`, {
      key: signed.key, fileName: file.name, note: value ?? '',
    })
    files.value = { ...files.value, [String(row.poId)]: d.items ?? [] }
    ElMessage.success(t('supplierRecon.fileUploaded'))
  } catch {
    ElMessage.error(t('supplierRecon.fileFailed'))
  } finally {
    uploadingPO.value = ''
    pendingRow.value = null
  }
}

async function removeFile(row: Row, f: ReconFile) {
  await ElMessageBox.confirm(t('supplierRecon.fileRemoveWhy'), t('supplierRecon.fileRemove'), {
    type: 'warning',
  }).catch(() => 'cancel').then(async (r) => {
    if (r === 'cancel') return
    const d = await post<{ items: ReconFile[] }>(`/supplier-recon/files/${f.id}/remove`, {})
    files.value = { ...files.value, [String(row.poId)]: d.items ?? [] }
    ElMessage.success(t('supplierRecon.fileRemoved'))
  })
}

function openEntry(row: Row) {
  entryRow.value = row
  entryForm.value = { amount: '', isRefund: false, paidAt: '', note: '' }
  // 换新键：一次对话框就是一次意图。只在成功后 reset 是不够的——上一次
  // 提交如果响应丢在路上（键还留着），下一笔**真的是新的一笔**付款会被
  // 网关当成重放静默丢掉，而页面照样弹「已记下」。
  payIdem.reset()
  entryOpen.value = true
}

async function submitEntry() {
  if (!entryRow.value) return
  if (!entryForm.value.amount.trim()) {
    ElMessage.warning(t('supplierRecon.entryAmountRequired'))
    return
  }
  entryBusy.value = true
  try {
    await post(`/supplier-recon/${entryRow.value.poId}/payments`, {
      amount: entryForm.value.amount,
      isRefund: entryForm.value.isRefund,
      paidAt: entryForm.value.paidAt,
      note: entryForm.value.note,
    }, withIdempotency(payIdem))
    payIdem.reset()
    entryOpen.value = false
    ElMessage.success(t('supplierRecon.entrySaved'))
    await loadEntries(entryRow.value)
    reload()
  } catch { /* surfaced by the api layer */ } finally {
    entryBusy.value = false
  }
}

async function reverseEntry(row: Row, e: Entry) {
  // 冲销必须给理由，和撤销完成同一条纪律。
  const { value } = await ElMessageBox.prompt(
    t('supplierRecon.entryReverseWhy'), t('supplierRecon.entryReverse'),
    { inputPlaceholder: t('supplierRecon.entryReverseReason') },
  ).catch(() => ({ value: '' }))
  if (!value) return
  await post(`/supplier-recon/${row.poId}/payments/${e.allocationId}/reverse`, { reason: value })
  ElMessage.success(t('supplierRecon.entryReversed'))
  await loadEntries(row)
  reload()
}

// ── 确认核销完成 ──────────────────────────────────────────
const CLOSURE_CATEGORIES = ['SETTLED', 'LOSS', 'ROUNDING', 'CANCELLED', 'OTHER'] as const
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
    await post(`/supplier-recon/${closing.value.poId}/close`, {
      category: closeForm.value.category, note: closeForm.value.note,
    })
    closeOpen.value = false
    ElMessage.success(t('supplierRecon.closed'))
    reload()
  } finally {
    closingBusy.value = false
  }
}

async function reopenRow(row: Row) {
  const { value } = await ElMessageBox.prompt(
    t('supplierRecon.reopenWhy', { no: row.poNo }), t('supplierRecon.reopen'),
    { inputPlaceholder: t('supplierRecon.reopenReason') },
  ).catch(() => ({ value: '' }))
  if (!value) return
  await post(`/supplier-recon/${row.poId}/reopen`, { reason: value })
  ElMessage.success(t('supplierRecon.reopened'))
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

// 待核销 ⇄ 已完成是**同一个组件实例**——vue-router 原地复用，setup 和
// onMounted 都不会再跑一遍。不盯着它重新拉数据的话，切过去表头和按钮都
// 变了（isDone 是响应式的），表格里却还是上一页那批数据：对着一张从没
// 确认过的单点「撤销完成」。ReceivableDuePage 和 SourcingCasesListPage
// 都踩过同一个坑。
watch(isDone, () => {
  view.value = ''
  entries.value = {}
  files.value = {}
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
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px;
}
.metric {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-height: 90px;
  padding: 14px 16px;
  border: 1px solid #dceaf0;
  border-radius: 12px;
  background: linear-gradient(145deg, #fff 15%, #f7fcff 100%);
  box-shadow: 0 7px 20px rgba(25, 72, 91, .04);
}
.metric.is-warn {
  border-color: var(--el-color-warning-light-5);
  background: var(--el-color-warning-light-9);
}
.metric-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.metric-value {
  font-size: 26px;
  font-variant-numeric: tabular-nums;
}
.metric-hint {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}
/* 卡片是 flex column，link 按钮默认撑满一行会让文字居中——按内容宽度
   靠左收住，和上面三行文字对齐。 */
.metric-action {
  align-self: flex-start;
  margin-top: 2px;
  font-size: 12px;
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
.sub.over,
.num.over {
  color: var(--el-color-warning);
}
.negative {
  color: var(--el-color-danger);
}
.files {
  margin-top: 12px;
  padding-top: 10px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.files-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 6px;
}
.files-title {
  font-size: 13px;
  font-weight: 600;
}
.file-list {
  margin: 0;
  padding-left: 18px;
  line-height: 1.9;
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
.num-cell {
  font-variant-numeric: tabular-nums;
}
.sub.overdue {
  color: var(--el-color-danger);
}
:deep(.row-overdue) {
  background: rgba(245, 108, 108, 0.055);
}
.row-actions { display: flex; align-items: center; white-space: nowrap; }
.drop-arrow { margin-left: 7px; font-size: 10px; }
.detail-summary { margin-bottom: 18px; }
.detail-heading { margin: 0 0 10px; font-size: 15px; }
.metric.is-alarm {
  border-color: var(--el-color-danger-light-5);
  background: var(--el-color-danger-light-9);
}
.po-no {
  font-variant-numeric: tabular-nums;
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
.close-target {
  margin: 0 0 10px;
  font-weight: 600;
}
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
  .panel { padding: 12px; }
  .filters { align-items: stretch; }
  .filters :deep(.el-input) { width: 100%; max-width: none !important; }
  .pager { justify-content: flex-start; overflow-x: auto; }
}
</style>
