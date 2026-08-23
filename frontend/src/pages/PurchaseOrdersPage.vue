<template>
  <div>
    <div class="page-head">
      <h2>{{ t('orders.title') }}</h2>
      <span class="head-note">{{ t('orders.subtitle') }}</span>
      <span class="grow" />
      <el-button @click="router.push('/procurement')">← {{ t('procurementNav.backToWorkbench') }}</el-button>
    </div>

    <el-card shadow="never">
      <el-radio-group v-model="status" class="tabs" @change="reload">
        <el-radio-button value="ORDERED">{{ t('orders.activeOrders') }}</el-radio-button>
        <el-radio-button value="PARTIALLY_RECEIVED">{{ t('orders.statuses.PARTIALLY_RECEIVED') }}</el-radio-button>
        <el-radio-button value="RECEIVED">{{ t('orders.statuses.RECEIVED') }}</el-radio-button>
        <el-radio-button value="CANCELLED">{{ t('orders.statuses.CANCELLED') }}</el-radio-button>
      </el-radio-group>

      <div class="filters">
        <el-input
          v-model="keyword"
          :placeholder="t('orders.searchPlaceholder')"
          clearable
          style="width: 260px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table :data="rows" v-loading="loading">
        <el-table-column :label="t('orders.poNo')" width="160">
          <template #default="{ row }">
            <div class="prod">{{ row.poNo }}</div>
            <div class="sub">{{ formatTime(row.createdAt) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.supplier')" min-width="170">
          <template #default="{ row }">
            <div>{{ row.supplierName }}</div>
            <div class="sub">{{ t('orders.buyer') }} {{ row.buyerName || '—' }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.purchaseBatch')" min-width="170">
          <template #default="{ row }">
            <div>{{ row.sourceQuotationNo || '—' }}</div>
            <div class="sub">{{ t('orders.lines', { n: row.itemCount }) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.amount')" width="150" align="right">
          <template #default="{ row }">
            <span class="num money">{{ row.currency }} {{ row.totalAmount }}</span>
            <div class="sub">{{ t('orders.lines', { n: row.itemCount }) }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.requiredArrivalDate')" width="125">
          <template #default="{ row }">{{ row.expectedDate || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="170">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)" effect="plain">
              {{ orderStatusLabel(row) }}
            </el-tag>
            <div v-if="row.rejectReason || row.cancelReason" class="sub reason">
              {{ row.rejectReason || row.cancelReason }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="160" fixed="right">
          <template #default="{ row }">
            <div class="row-actions">
              <el-button link type="primary" @click="openDetail(row)">{{ t('common.detail') }}</el-button>
              <el-dropdown
                v-if="['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status)"
                trigger="click"
                @command="(command: string) => handleOrderAction(row, command)"
              >
                <el-button link type="primary" :loading="downloadingId === Number(row.id)">
                  {{ t('common.more') }}
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="xlsx">{{ t('orders.downloadExcel') }}</el-dropdown-item>
                    <el-dropdown-item command="pdf">{{ t('orders.downloadPdf') }}</el-dropdown-item>
                    <el-dropdown-item
                      v-if="canReceive && ['ORDERED', 'PARTIALLY_RECEIVED'].includes(row.status)"
                      command="receive"
                    >{{ t('orders.receive') }}</el-dropdown-item>
                    <el-dropdown-item command="execution">{{ t('orders.execution') }}</el-dropdown-item>
                    <el-dropdown-item
                      v-if="canCancel && row.status === 'ORDERED'"
                      command="cancel"
                      divided
                    >{{ common('cancel') }}</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </template>
        </el-table-column>
        <template #empty>{{ t('orders.empty') }}</template>
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

    <el-dialog v-model="createOpen" :title="editing ? t('orders.editFor', { no: editing.poNo }) : t('orders.create')" width="900px">
      <el-alert type="info" :closable="false" show-icon class="alert">
        {{ t('orders.createHint') }}
      </el-alert>
      <el-form label-width="90px" class="head-form">
        <el-form-item :label="t('orders.supplier')" required>
          <div class="supplier-row">
            <el-select v-model="form.supplierId" filterable :disabled="approvalEntry" style="width: 420px">
              <el-option
                v-for="s in suppliers"
                :key="s.id"
                :value="Number(s.id)"
                :label="`${s.code} · ${s.name}`"
              />
            </el-select>
          </div>
        </el-form-item>
        <el-form-item :label="approvalEntry ? t('orders.requiredArrivalDate') : t('orders.expected')" :required="approvalEntry">
          <el-date-picker v-model="form.expectedDate" type="date" value-format="YYYY-MM-DD" style="width: 200px" />
        </el-form-item>
        <el-form-item label="履约方式" required>
          <el-radio-group v-model="form.fulfillmentMode">
            <el-radio-button value="DIRECT_SHIP">直接发往港口/指定地点</el-radio-button>
            <el-radio-button value="WAREHOUSE">先入库再发货</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <template v-if="form.fulfillmentMode === 'DIRECT_SHIP'">
          <el-form-item label="收货地点" required>
            <el-radio-group v-model="form.deliveryLocationType">
              <el-radio value="PORT">港口</el-radio>
              <el-radio value="CUSTOM">自定义地址</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="form.deliveryLocationType === 'PORT'" label="收货港口" required>
            <el-select v-model="form.deliveryPortId" filterable style="width: 420px" @change="selectDeliveryPort">
              <el-option
                v-for="p in deliveryPorts"
                :key="p.id"
                :value="Number(p.id)"
                :label="portLabel(p)"
              />
            </el-select>
          </el-form-item>
          <el-form-item v-else label="收货地址" required><el-input v-model="form.deliveryAddress" /></el-form-item>
        </template>
        <el-form-item v-else label="入库仓库" required>
          <el-select v-model="form.warehouseId" style="width: 420px" @change="selectOrderWarehouse">
            <el-option v-for="w in warehouses" :key="w.id" :value="Number(w.id)" :label="w.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="调整原因">
          <el-input
            v-model="form.sourceChangeReason"
            placeholder="如修改了已确认报价的供应商单价或币种，请说明原因"
          />
        </el-form-item>
        <el-form-item :label="t('orders.remark')">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>

      <div class="side-title">{{ t('orders.pickRequirements') }}</div>
      <el-table :data="pending" size="small" max-height="320">
        <el-table-column :label="t('orders.product')" min-width="180">
          <template #default="{ row }">
            <div>{{ row.productName }}</div>
            <div class="sub">
              <template v-if="row.source === 'MANUAL'">
                <el-tag size="small" type="info" effect="plain">{{ t('orders.manual') }}</el-tag>
              </template>
              <template v-else>{{ row.contractNo }} · {{ row.customerName }}</template>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.open')" width="110" align="right">
          <template #default="{ row }">
            <span class="num">{{ trim(openOf(row)) }}</span> {{ row.uomCode }}
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.requiredDate')" width="110">
          <template #default="{ row }">{{ row.requiredDate || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('orders.orderQty')" width="130">
          <template #default="{ row }">
            <el-input v-model="qtyOf[row.id]" size="small" />
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.unitPrice')" width="130">
          <template #default="{ row }">
            <el-input v-model="priceOf[row.id]" size="small" />
          </template>
        </el-table-column>
        <template #empty>{{ t('orders.noPending') }}</template>
      </el-table>
      <div class="total-row">
        {{ t('orders.estimated') }}<span class="num money">{{ estimated }}</span>
      </div>

      <template #footer>
        <el-button @click="createOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitCreate">
          {{ approvalEntry ? t('orders.createAndSubmit') : common('save') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailOpen" :title="detail?.poNo" width="820px">
      <el-descriptions :column="3" border size="small" class="desc">
        <el-descriptions-item :label="t('orders.supplier')">{{ detail?.supplierName }}</el-descriptions-item>
        <el-descriptions-item :label="t('orders.amount')">
          {{ detail?.currency }} {{ detail?.totalAmount }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('common.status')">
          {{ detail ? t(`orders.statuses.${detail.status}`) : '' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('orders.buyer')">{{ detail?.buyerName || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('orders.expected')">{{ detail?.expectedDate || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('orders.remark')">{{ detail?.remark || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('orders.sendStatus')">
          {{ detail ? t(`orders.sendStatuses.${detail.sendStatus || 'NOT_SENT'}`) : '' }}
          <span v-if="detail?.sentTo" class="sub"> · {{ detail.sentTo }}</span>
        </el-descriptions-item>
      </el-descriptions>

      <el-table :data="detailItems" size="small">
        <el-table-column :label="t('orders.product')" min-width="180">
          <template #default="{ row }">
            <div>{{ row.productName }}</div>
            <div class="sub">
              <template v-if="row.source === 'MANUAL'">{{ t('orders.manual') }}</template>
              <template v-else>{{ row.contractNo }} · {{ row.customerName }}</template>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.orderQty')" width="100" align="right">
          <template #default="{ row }"><span class="num">{{ trim(row.qty) }}</span></template>
        </el-table-column>
        <el-table-column :label="t('orders.unitPrice')" width="100" align="right">
          <template #default="{ row }"><span class="num">{{ trim(row.unitPrice) }}</span></template>
        </el-table-column>
        <el-table-column :label="t('orders.amount')" width="110" align="right">
          <template #default="{ row }"><span class="num">{{ row.amount }}</span></template>
        </el-table-column>
        <el-table-column :label="t('orders.received')" width="100" align="right">
          <template #default="{ row }">
            <span class="num" :class="{ dim: Number(row.receivedQty) === 0 }">{{ trim(row.receivedQty) }}</span>
          </template>
        </el-table-column>
      </el-table>

      <template v-if="detailReceipts.length">
        <div class="side-title">{{ t('orders.receipts') }}</div>
        <el-table :data="detailReceipts" size="small">
          <el-table-column :label="t('orders.receiptNo')" width="170">
            <template #default="{ row }">{{ row.receiptNo }}</template>
          </el-table-column>
          <el-table-column :label="t('orders.receivedAt')" width="150">
            <template #default="{ row }">{{ formatTime(row.receivedAt) }}</template>
          </el-table-column>
          <el-table-column :label="t('orders.qty')" width="100" align="right">
            <template #default="{ row }"><span class="num">{{ trim(row.totalQty) }}</span></template>
          </el-table-column>
          <el-table-column :label="t('orders.operator')" min-width="120">
            <template #default="{ row }">{{ row.operatorName || '—' }}</template>
          </el-table-column>
        </el-table>
      </template>
    </el-dialog>

    <el-dialog v-model="receiveOpen" :title="t('orders.receiveFor', { no: receiving?.poNo })" width="760px">
      <el-alert type="info" :closable="false" show-icon class="alert">
        {{ t('orders.receiveHint') }}
      </el-alert>
      <el-form label-width="80px">
        <el-form-item :label="t('orders.warehouse')" required>
          <el-select v-model="receiveWarehouse" style="width: 220px">
            <el-option v-for="w in portWarehouses" :key="w.id" :value="Number(w.id)" :label="w.name" />
          </el-select>
        </el-form-item>
      </el-form>
      <el-table :data="receiveItems" size="small">
        <el-table-column :label="t('orders.product')" min-width="180">
          <template #default="{ row }">{{ row.productName }}</template>
        </el-table-column>
        <el-table-column :label="t('orders.orderQty')" width="100" align="right">
          <template #default="{ row }"><span class="num">{{ trim(row.qty) }}</span></template>
        </el-table-column>
        <el-table-column :label="t('orders.alreadyReceived')" width="110" align="right">
          <template #default="{ row }">
            <span class="num" :class="{ dim: Number(row.receivedQty) === 0 }">{{ trim(row.receivedQty) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.receiveNow')" width="140">
          <template #default="{ row }">
            <el-input v-model="receiveQty[row.id]" size="small" :disabled="outstandingOf(row) <= 0" />
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="receiveOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitReceive">{{ t('orders.confirmReceipt') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="cancelOpen" :title="t('orders.cancelTitle')" width="460px">
      <el-alert type="warning" :closable="false" show-icon class="alert">
        {{ t('orders.cancelWarning') }}
      </el-alert>
      <el-input v-model="cancelReason" type="textarea" :rows="3" :placeholder="t('orders.cancelReason')" />
      <template #footer>
        <el-button @click="cancelOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="danger" :loading="saving" @click="submitCancel">{{ common('confirm') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="executionOpen" :title="t('orders.executionFor', { no: executing?.poNo })" width="960px">
      <el-tabs v-model="executionTab">
        <el-tab-pane :label="t('orders.confirmations')" name="confirmation">
          <div class="execution-section-head">
            <div>
              <strong>{{ t('orders.confirmationProgress') }}</strong>
              <div class="sub">{{ t('orders.confirmationProgressHint') }}</div>
            </div>
            <div class="overall-progress">
              <el-progress :percentage="confirmationProgress" :stroke-width="10" />
            </div>
          </div>
          <el-form v-if="canProduction" label-width="110px" inline>
            <el-form-item :label="t('orders.confirmedDate')"><el-date-picker v-model="confirmationForm.confirmedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
            <el-form-item :label="t('orders.confirmedExpected')"><el-date-picker v-model="confirmationForm.expectedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
          </el-form>
          <el-table :data="executionItems" size="small">
            <el-table-column prop="productName" :label="t('orders.product')" min-width="180" />
            <el-table-column :label="t('orders.orderQty')" width="110"><template #default="{row}">{{ trim(row.qty) }} {{ row.uomCode }}</template></el-table-column>
            <el-table-column :label="t('orders.confirmedQty')" width="130"><template #default="{row}"><el-input v-model="confirmationQty[row.id]" size="small" :disabled="!canProduction" /></template></el-table-column>
            <el-table-column :label="t('orders.completionProgress')" width="150">
              <template #default="{ row }"><el-progress :percentage="lineConfirmationProgress(row)" :stroke-width="8" /></template>
            </el-table-column>
            <el-table-column :label="t('orders.unitPrice')" width="110"><template #default="{row}">{{ trim(row.unitPrice) }}</template></el-table-column>
            <el-table-column :label="t('orders.confirmedPrice')" width="130"><template #default="{row}"><el-input v-model="confirmationPrice[row.id]" size="small" :disabled="!canProduction" /></template></el-table-column>
          </el-table>
          <el-input v-if="canProduction" v-model="confirmationForm.remark" type="textarea" :rows="2" :placeholder="t('orders.confirmRemark')" class="execution-note" />
          <el-button v-if="canProduction" type="primary" :loading="saving" @click="submitConfirmation">{{ t('orders.saveConfirmation') }}</el-button>
          <el-table :data="execution.confirmations" size="small" class="history-table">
            <el-table-column prop="confirmedDate" :label="t('orders.confirmedDate')" width="120" />
            <el-table-column :label="t('common.status')" width="170"><template #default="{row}">{{ t(`orders.confirmStatuses.${row.status}`) }}</template></el-table-column>
            <el-table-column prop="confirmedExpectedDate" :label="t('orders.confirmedExpected')" width="130" />
            <el-table-column prop="remark" :label="t('orders.remark')" min-width="180" />
            <el-table-column prop="createdBy" :label="t('orders.operator')" width="120" />
          </el-table>
        </el-tab-pane>

        <el-tab-pane :label="t('orders.production')" name="production">
          <el-alert v-for="reminder in execution.reminders.filter(r => r.status === 'OPEN')" :key="reminder.id" type="warning" :closable="false" class="alert">
            {{ t('orders.delayReminder', { node: productionNodeLabel(reminder.node), date: reminder.plannedDate, contracts: reminder.relatedContracts || '—' }) }}
          </el-alert>
          <section v-if="canProduction" class="execution-editor">
            <div class="execution-section-head">
              <div><strong>{{ t('orders.updateProduction') }}</strong><div class="sub">{{ t('orders.updateProductionHint') }}</div></div>
            </div>
            <el-form label-position="top" class="execution-form-grid">
              <el-form-item :label="t('orders.productionNode')"><el-select v-model="milestoneForm.node"><el-option v-for="node in productionNodes" :key="node" :value="node" :label="productionNodeLabel(node)" /></el-select></el-form-item>
              <el-form-item :label="t('orders.plannedDate')"><el-date-picker v-model="milestoneForm.plannedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
              <el-form-item :label="t('orders.actualDate')"><el-date-picker v-model="milestoneForm.actualDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
              <el-form-item :label="t('orders.owner')"><el-input v-model="milestoneForm.ownerName" /></el-form-item>
              <el-form-item class="span-all" :label="t('orders.remark')"><el-input v-model="milestoneForm.remark" type="textarea" :rows="2" /></el-form-item>
              <el-collapse class="span-all optional-fields">
                <el-collapse-item :title="t('orders.optionalAttachment')" name="attachment">
                  <div class="execution-form-grid attachment-grid">
                    <el-form-item :label="t('orders.attachmentName')"><el-input v-model="milestoneForm.fileName" /></el-form-item>
                    <el-form-item :label="t('orders.attachmentUrl')"><el-input v-model="milestoneForm.fileUrl" /></el-form-item>
                  </div>
                </el-collapse-item>
              </el-collapse>
            </el-form>
            <el-button type="primary" :loading="saving" @click="submitMilestone">{{ t('orders.saveMilestone') }}</el-button>
          </section>
          <el-table :data="execution.milestones" size="small" class="history-table">
            <el-table-column :label="t('orders.productionNode')" width="150"><template #default="{row}">{{ productionNodeLabel(row.node) }}</template></el-table-column>
            <el-table-column prop="plannedDate" :label="t('orders.plannedDate')" width="110" />
            <el-table-column prop="actualDate" :label="t('orders.actualDate')" width="110" />
            <el-table-column prop="ownerName" :label="t('orders.owner')" width="120" />
            <el-table-column prop="remark" :label="t('orders.remark')" min-width="160" />
            <el-table-column :label="t('orders.attachments')" min-width="160"><template #default="{row}"><a v-for="file in row.attachments" :key="file.fileUrl" :href="file.fileUrl" target="_blank">{{ file.fileName }}</a></template></el-table-column>
          </el-table>
        </el-tab-pane>

        <el-tab-pane :label="t('orders.exceptions')" name="exceptions">
          <el-form v-if="canException" label-position="top" class="execution-form-grid execution-editor">
            <el-form-item :label="t('orders.exceptionType')"><el-select v-model="exceptionForm.type" style="width:180px"><el-option v-for="kind in exceptionTypes" :key="kind" :value="kind" :label="t(`orders.exceptionTypes.${kind}`)" /></el-select></el-form-item>
            <el-form-item :label="t('orders.optionalReceiptNo')"><el-select v-model="exceptionForm.receiptId" clearable :placeholder="t('orders.optionalReceiptNoHint')"><el-option v-for="receipt in executionReceipts" :key="receipt.id" :value="Number(receipt.id)" :label="receipt.receiptNo" /></el-select></el-form-item>
            <el-form-item :label="t('orders.product')"><el-select v-model="exceptionForm.itemId" clearable style="width:200px"><el-option v-for="item in executionItems" :key="item.id" :value="Number(item.id)" :label="item.productName" /></el-select></el-form-item>
            <el-form-item :label="t('orders.qty')"><el-input v-model="exceptionForm.qty" style="width:120px" /></el-form-item>
            <el-form-item :label="t('orders.actualProduct')"><el-input v-model="exceptionForm.actualProduct" style="width:180px" /></el-form-item>
            <el-form-item :label="t('orders.actualUom')"><el-input v-model="exceptionForm.actualUom" style="width:120px" /></el-form-item>
          </el-form>
          <el-input v-if="canException" v-model="exceptionForm.description" type="textarea" :rows="2" :placeholder="t('orders.exceptionDescription')" class="execution-note" />
          <el-button v-if="canException" type="primary" :loading="saving" @click="submitException">{{ t('orders.reportException') }}</el-button>
          <el-table :data="execution.exceptions" size="small" class="history-table">
            <el-table-column :label="t('orders.exceptionType')" width="140"><template #default="{row}">{{ t(`orders.exceptionTypes.${row.exceptionType}`) }}</template></el-table-column>
            <el-table-column prop="description" :label="t('orders.exceptionDescription')" min-width="180" />
            <el-table-column prop="qty" :label="t('orders.qty')" width="90" />
            <el-table-column :label="t('common.status')" width="100"><template #default="{row}">{{ t(`orders.exceptionStatuses.${row.status}`) }}</template></el-table-column>
            <el-table-column prop="resolution" :label="t('orders.resolution')" min-width="160" />
            <el-table-column :label="t('common.actions')" width="90"><template #default="{row}"><el-button v-if="canException && row.status === 'OPEN'" link type="primary" @click="resolveException(row)">{{ t('orders.resolve') }}</el-button></template></el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { download, get, post, put, quietErrors, saveBlob } from '../api'
import { onLive } from '../live'
import { buildConfirmationLines } from '../lib/purchaseExecution'
import { useAuthStore } from '../stores/auth'

interface Order {
  id: string
  poNo: string
  supplierId: string
  supplierCode: string
  supplierName: string
  currency: string
  totalAmount: string
  expectedDate: string
  status: string
  buyerName: string
  remark: string
  rejectReason: string
  cancelReason: string
  itemCount: number
  totalQty: string
  receivedQty: string
  createdAt: string
  sendStatus: string
  sentTo: string
  sentAt: string
  sentBy: string
  sendError: string
  fulfillmentMode: string
  deliveryLocationType: string
  deliveryPortId: string
  deliveryPortCode: string
  deliveryPortName: string
  warehouseId: string
  warehouseName: string
  deliveryAddress: string
  sourceQuotationId: string
  sourceQuotationNo: string
  sourceCostScenarioId: string
  factoryId: string
  factoryCode: string
  factoryName: string
}
interface OrderItem {
  id: string
  requirementId: string
  productName: string
  uomCode: string
  qty: string
  unitPrice: string
  amount: string
  receivedQty: string
  contractNo: string
  customerName: string
  source: string
}
interface Receipt {
  id: string
  receiptNo: string
  operatorName: string
  totalQty: string
  receivedAt: string
}
interface Requirement {
  id: string
  contractNo: string
  customerName: string
  productName: string
  uomCode: string
  requiredQty: string
  orderedQty: string
  requiredDate: string
  source: string
  quotationId: string
  supplierId: string
  supplierName: string
  sourceCurrency: string
  sourceUnitPrice: string
}
interface Supplier { id: string; code: string; name: string }
interface ApiFailure { code?: string; message?: string }
interface Warehouse { id: string; code: string; name: string; whType: string }
interface SupplierConfirmationLine { poItemId: string; confirmedQty: string; confirmedUnitPrice: string }
interface SupplierConfirmation { id: string; status: string; confirmedDate: string; confirmedExpectedDate: string; remark: string; createdBy: string; lines: SupplierConfirmationLine[] }
interface ProductionAttachment { fileName: string; fileUrl: string; contentType: string }
interface ProductionMilestone { id: string; node: string; plannedDate: string; actualDate: string; ownerName: string; remark: string; delayed: boolean; attachments: ProductionAttachment[] }
interface ReceiptException { id: string; exceptionType: string; qty: string; description: string; status: string; resolution: string }
interface ProductionReminder { id: string; node: string; plannedDate: string; buyerName: string; relatedContracts: string; status: string }
interface Execution { confirmations: SupplierConfirmation[]; milestones: ProductionMilestone[]; exceptions: ReceiptException[]; reminders: ProductionReminder[] }

const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const canWrite = auth.can('procurement:order:write')
const canCancel = auth.can('procurement:order:cancel')
const canReceive = auth.can('procurement:receipt:write')
const canProduction = auth.can('procurement:production:write')
const canException = auth.can('procurement:exception:write')
const canReadPorts = auth.can('masterdata:port:read')
const canReadWarehouses = auth.can('inventory:stock:read')

const rows = ref<Order[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
// 采购单页面只展示已经通过采购审批的执行单据。草稿、审批中和驳回
// 都属于“待采购并审批”的过程状态，不在这里形成第二个审批入口。
const status = ref('ORDERED')
const keyword = ref('')
const loading = ref(false)
const saving = ref(false)
const downloadingId = ref(0)

const createOpen = ref(false)
const approvalEntry = ref(false)
const editing = ref<Order | null>(null)
const pending = ref<Requirement[]>([])
const suppliers = ref<Supplier[]>([])
const qtyOf = reactive<Record<string, string>>({})
const priceOf = reactive<Record<string, string>>({})
const form = reactive({ supplierId: 0, currency: 'CNY', expectedDate: '', remark: '', fulfillmentMode: 'DIRECT_SHIP', deliveryLocationType: 'PORT', deliveryPortId: 0, deliveryPortCode: '', deliveryPortName: '', warehouseId: 0, warehouseName: '', deliveryAddress: '', sourceChangeReason: '' })
const deliveryPorts = ref<{ id: string; unLocode: string; nameZh: string; nameEn: string }[]>([])

const detailOpen = ref(false)
const detail = ref<Order | null>(null)
const detailItems = ref<OrderItem[]>([])
const detailReceipts = ref<Receipt[]>([])

const receiveOpen = ref(false)
const receiving = ref<Order | null>(null)
const receiveItems = ref<OrderItem[]>([])
const receiveQty = reactive<Record<string, string>>({})
const receiveWarehouse = ref(0)
const warehouses = ref<Warehouse[]>([])
const portWarehouses = computed(() => warehouses.value.filter((w) => w.whType === 'PORT_TERMINAL'))

const cancelOpen = ref(false)
const cancelling = ref<Order | null>(null)
const cancelReason = ref('')

const executionOpen = ref(false)
const executionTab = ref('confirmation')
const executing = ref<Order | null>(null)
const executionItems = ref<OrderItem[]>([])
const executionReceipts = ref<Receipt[]>([])
const execution = reactive<Execution>({ confirmations: [], milestones: [], exceptions: [], reminders: [] })
const confirmationQty = reactive<Record<string, string>>({})
const confirmationPrice = reactive<Record<string, string>>({})
const confirmationForm = reactive({ confirmedDate: '', expectedDate: '', remark: '' })
const productionNodes = ['PENDING_SCHEDULE', 'SCHEDULED', 'IN_PRODUCTION', 'QUALITY_INSPECTION', 'READY_TO_SHIP', 'SENT_TO_PORT']
const milestoneForm = reactive({ node: 'PENDING_SCHEDULE', plannedDate: '', actualDate: '', ownerName: '', fileName: '', fileUrl: '', remark: '' })
const exceptionTypes = ['WRONG_PRODUCT', 'UNIT_MISMATCH', 'SHORT_SHIPMENT', 'DAMAGE', 'QUALITY_DISPUTE', 'RETURN']
const exceptionForm = reactive<{ type: string; receiptId?: number; itemId?: number; qty: string; actualProduct: string; actualUom: string; description: string }>({ type: 'SHORT_SHIPMENT', receiptId: undefined, itemId: undefined, qty: '', actualProduct: '', actualUom: '', description: '' })

function lineConfirmationProgress(item: OrderItem): number {
  const ordered = Number(item.qty)
  if (!(ordered > 0)) return 0
  const latest = execution.confirmations[0]?.lines?.find((line) => Number(line.poItemId) === Number(item.id))
  const confirmed = latest ? Number(latest.confirmedQty) : 0
  return Math.max(0, Math.min(100, Math.round((confirmed / ordered) * 1000) / 10))
}

// 不同产品可能使用 MT、PCS、SET 等单位，不能直接把数量相加。
// 总体完成进度取每条产品确认比例的平均值。
const confirmationProgress = computed(() => {
  if (!executionItems.value.length) return 0
  const sum = executionItems.value.reduce((total, item) => total + lineConfirmationProgress(item), 0)
  return Math.round((sum / executionItems.value.length) * 10) / 10
})

const common = (k: string) => t(`common.${k}`)

// What this order will cost, recomputed as the buyer types. The server is the
// authority; this is so nobody submits a number they have not seen.
const estimated = computed(() => {
  let sum = 0
  for (const r of pending.value) {
    sum += Number(qtyOf[r.id] ?? 0) * Number(priceOf[r.id] ?? 0)
  }
  return sum.toFixed(2)
})

async function load() {
  loading.value = true
  try {
    const d = await get<{ orders: Order[]; meta: { total: number } }>('/purchase-orders', {
      page: page.value, page_size: pageSize, status: status.value, keyword: keyword.value,
    })
    rows.value = d.orders ?? []
    total.value = Number(d.meta?.total ?? 0)
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  return load()
}

function openOf(r: Requirement): string {
  return String(Number(r.requiredQty ?? 0) - Number(r.orderedQty ?? 0))
}

async function openCreate(preselect?: string[]) {
	editing.value = null
  form.supplierId = 0
  form.currency = 'CNY'
  form.expectedDate = ''
  form.remark = ''
  form.fulfillmentMode = 'DIRECT_SHIP'
  form.deliveryLocationType = canReadPorts ? 'PORT' : 'CUSTOM'
  form.deliveryPortId = 0
  form.deliveryPortCode = ''
  form.deliveryPortName = ''
  form.deliveryAddress = ''
  form.warehouseId = 0
  form.warehouseName = ''
  form.sourceChangeReason = ''
  Object.keys(qtyOf).forEach((k) => delete qtyOf[k])
  Object.keys(priceOf).forEach((k) => delete priceOf[k])
  const [reqs, sups, ports, whs] = await Promise.all([
    get<{ requirements: Requirement[] }>('/requirements', { status: 'PENDING', page_size: 200 }),
    get<{ suppliers: Supplier[] }>('/suppliers', { page_size: 200 }),
    canReadPorts
      ? get<{ ports: typeof deliveryPorts.value }>('/ports', { page_size: 200, status: 'ACTIVE' })
      : Promise.resolve({ ports: [] }),
    canReadWarehouses
      ? get<{ warehouses: Warehouse[] }>('/warehouses')
      : Promise.resolve({ warehouses: [] }),
  ])
  // Partially ordered ones belong here too: what is left is still owed.
  const partial = await get<{ requirements: Requirement[] }>('/requirements', {
    status: 'PARTIALLY_ORDERED', page_size: 200,
  })
  pending.value = [...(reqs.requirements ?? []), ...(partial.requirements ?? [])]
  suppliers.value = sups.suppliers ?? []
  deliveryPorts.value = ports.ports ?? []
  warehouses.value = whs.warehouses ?? []
  // Arriving from the requirements page with lines already chosen: pre-fill
  // exactly those and leave the rest blank, so the buyer does not have to
  // find them again by product name.
  if (preselect?.length) {
    const wanted = new Set(preselect)
    // 从待采购审批进入时严格只显示员工勾选的产品，避免其它待采购行混入本次审批。
    pending.value = pending.value.filter((r) => wanted.has(String(r.id)))
    pending.value.forEach((r) => {
      qtyOf[r.id] = String(Number(r.requiredQty) - Number(r.orderedQty))
      if (r.source === 'CUSTOMER_QUOTATION') {
        form.supplierId = Number(r.supplierId)
        form.currency = r.sourceCurrency || 'USD'
        priceOf[r.id] = r.sourceUnitPrice || '0'
      }
    })
    const requiredDates = pending.value.map((r) => r.requiredDate).filter(Boolean).sort()
    form.expectedDate = requiredDates[0] || ''
  }
  createOpen.value = true
}

async function openEdit(row: Order) {
  const [detailData, reqs, partial, sups, ports, whs] = await Promise.all([
    get<{ order: Order; items: OrderItem[] }>(`/purchase-orders/${row.id}`),
    get<{ requirements: Requirement[] }>('/requirements', { status: 'PENDING', page_size: 200 }),
    get<{ requirements: Requirement[] }>('/requirements', { status: 'PARTIALLY_ORDERED', page_size: 200 }),
    get<{ suppliers: Supplier[] }>('/suppliers', { page_size: 200 }),
    canReadPorts
      ? get<{ ports: typeof deliveryPorts.value }>('/ports', { page_size: 200, status: 'ACTIVE' })
      : Promise.resolve({ ports: [] }),
    canReadWarehouses
      ? get<{ warehouses: Warehouse[] }>('/warehouses')
      : Promise.resolve({ warehouses: [] }),
  ])
  const current = detailData.order
  if (!['DRAFT', 'REJECTED'].includes(current.status)) {
    ElMessage.warning(t('orders.notEditable'))
    await load()
    return
  }
  editing.value = current
  form.supplierId = Number(current.supplierId)
  form.currency = current.currency || 'CNY'
  form.expectedDate = current.expectedDate || ''
  form.remark = current.remark || ''
  form.fulfillmentMode = current.fulfillmentMode || 'DIRECT_SHIP'
  form.deliveryLocationType = current.deliveryLocationType || 'PORT'
  form.deliveryPortId = Number(current.deliveryPortId || 0)
  form.deliveryPortCode = current.deliveryPortCode || ''
  form.deliveryPortName = current.deliveryPortName || ''
  form.warehouseId = Number(current.warehouseId || 0)
  form.warehouseName = current.warehouseName || ''
  form.deliveryAddress = current.deliveryAddress || ''
  form.sourceChangeReason = ''
  Object.keys(qtyOf).forEach((key) => delete qtyOf[key])
  Object.keys(priceOf).forEach((key) => delete priceOf[key])
  pending.value = [...(reqs.requirements ?? []), ...(partial.requirements ?? [])]
  suppliers.value = sups.suppliers ?? []
  deliveryPorts.value = ports.ports ?? []
  warehouses.value = whs.warehouses ?? []
  for (const item of detailData.items ?? []) {
    qtyOf[item.requirementId] = item.qty
    priceOf[item.requirementId] = item.unitPrice
  }
  createOpen.value = true
}

async function submitCreate() {
  const wasApprovalEntry = approvalEntry.value
  const supplier = suppliers.value.find((s) => Number(s.id) === form.supplierId)
  if (!supplier) {
    ElMessage.warning(t('orders.supplierRequired'))
    return
  }
  const lines = pending.value
    .filter((r) => Number(qtyOf[r.id] ?? 0) > 0)
    .map((r) => ({
      requirement_id: Number(r.id),
      qty: qtyOf[r.id],
      unit_price: priceOf[r.id] || '0',
    }))
  if (!lines.length) {
    ElMessage.warning(t('orders.pickSomething'))
    return
  }
  if (form.fulfillmentMode === 'WAREHOUSE' && !form.warehouseId) {
    ElMessage.warning('请选择入库仓库')
    return
  }
  if (approvalEntry.value && !form.expectedDate) {
    ElMessage.warning(t('orders.requiredArrivalDateRequired'))
    return
  }
  if (form.fulfillmentMode === 'DIRECT_SHIP' && form.deliveryLocationType === 'PORT' &&
      !form.deliveryPortId && !form.deliveryPortName.trim()) {
    ElMessage.warning('请选择收货港口')
    return
  }
  if (form.fulfillmentMode === 'DIRECT_SHIP' && form.deliveryLocationType === 'CUSTOM' &&
      !form.deliveryAddress.trim()) {
    ElMessage.warning('请填写收货地址')
    return
  }
  saving.value = true
  try {
    const payload = {
      supplier_id: Number(supplier.id),
      currency: form.currency,
      expected_date: form.expectedDate,
      remark: form.remark,
      fulfillment_mode: form.fulfillmentMode,
      delivery_location_type: form.fulfillmentMode === 'WAREHOUSE' ? 'WAREHOUSE' : form.deliveryLocationType,
      delivery_port_id: form.deliveryPortId,
      delivery_port_code: form.deliveryPortCode,
      delivery_port_name: form.deliveryPortName,
      warehouse_id: form.warehouseId,
      warehouse_name: form.warehouseName,
      delivery_address: form.deliveryAddress,
      source_change_reason: form.sourceChangeReason,
      lines,
    }
    let res: { id?: string; poNo: string }
    let recoveredLegacyDraft = false
    if (editing.value) {
      res = await put<{ id?: string; poNo: string }>(`/purchase-orders/${editing.value.id}`, payload)
    } else {
      try {
        res = await post<{ id: string; poNo: string }>(
          '/purchase-orders', payload, wasApprovalEntry ? quietErrors : undefined,
        )
      } catch (failure) {
        const apiFailure = failure as ApiFailure
        if (!wasApprovalEntry || apiFailure.code !== 'PO_QUOTATION_ALREADY_ORDERED') {
          ElMessage.error(apiFailure.message || t('common.requestFailed'))
          throw failure
        }
        // 上次建单成功但审批提交失败时会留下草稿。再次操作应继续这张草稿，
        // 更新为员工本次确认的内容后提交，不能既隐藏草稿又阻止员工继续办理。
        const quotationID = Number(pending.value.find((item) => Number(item.quotationId) > 0)?.quotationId || 0)
        const existingLists = await Promise.all(['DRAFT', 'REJECTED'].map((draftStatus) =>
          get<{ orders: Order[] }>('/purchase-orders', {
            status: draftStatus, keyword: supplier.name, page_size: 200,
          }, quietErrors),
        ))
        const existing = existingLists
          .flatMap((item) => item.orders ?? [])
          .find((order) => Number(order.sourceQuotationId) === quotationID && Number(order.supplierId) === Number(supplier.id))
        if (!existing) {
          ElMessage.error(apiFailure.message || t('common.requestFailed'))
          throw failure
        }
        const updated = await put<{ poNo: string }>(`/purchase-orders/${existing.id}`, payload)
        res = { id: existing.id, poNo: updated.poNo }
        recoveredLegacyDraft = true
      }
    }
    if (wasApprovalEntry && recoveredLegacyDraft && res.id) {
      // 仅旧版本遗留的草稿需要补做一次提交；新流程在创建事务内已经直接转为 ORDERED。
      await post(`/purchase-orders/${res.id}/submit`, {})
    }
    ElMessage.success(editing.value
      ? t('orders.updated', { no: res.poNo })
      : wasApprovalEntry
        ? t('orders.createdAndSubmitted', { no: res.poNo })
        : t('orders.created', { no: res.poNo }))
    createOpen.value = false
    approvalEntry.value = false
    editing.value = null
    if (wasApprovalEntry) {
      // 这里就是唯一的人工审批点。后端会在同一事务中生成采购单并将其
      // 转为 ORDERED，因此返回列表后能够直接看到“已下单”。
      await router.push('/requirements')
    } else {
      reload()
    }
  } finally {
    saving.value = false
  }
}

function selectDeliveryPort(id: number) {
  const port = deliveryPorts.value.find((item) => Number(item.id) === Number(id))
  form.deliveryPortCode = port?.unLocode ?? ''
  form.deliveryPortName = port?.nameZh || port?.nameEn || ''
}

function portLabel(port: { unLocode?: string; nameZh?: string; nameEn?: string }) {
  return [port.unLocode, port.nameZh || port.nameEn].filter(Boolean).join(' · ')
}

function selectOrderWarehouse(id: number) {
  form.warehouseName = warehouses.value.find((item) => Number(item.id) === Number(id))?.name ?? ''
}

async function openDetail(row: Order) {
  const d = await get<{ order: Order; items: OrderItem[]; receipts: Receipt[] }>(`/purchase-orders/${row.id}`)
  detail.value = d.order
  detailItems.value = d.items ?? []
  detailReceipts.value = d.receipts ?? []
  detailOpen.value = true
}

async function downloadOrder(row: Order, format: string) {
  if (format !== 'xlsx' && format !== 'pdf') return
  downloadingId.value = Number(row.id)
  try {
    const file = await download(`/purchase-orders/${row.id}/documents`, { format })
    saveBlob(file.blob, file.fileName)
    ElMessage.success(t('orders.downloaded', { name: file.fileName }))
  } finally {
    downloadingId.value = 0
  }
}

async function loadExecution() {
  if (!executing.value) return
  const data = await get<Execution>(`/purchase-orders/${executing.value.id}/execution`)
  execution.confirmations = data.confirmations ?? []
  execution.milestones = data.milestones ?? []
  execution.exceptions = data.exceptions ?? []
  execution.reminders = data.reminders ?? []
}

async function openExecution(row: Order) {
  executing.value = row
  const detailData = await get<{ order: Order; items: OrderItem[]; receipts: Receipt[] }>(`/purchase-orders/${row.id}`)
  executionItems.value = detailData.items ?? []
  executionReceipts.value = detailData.receipts ?? []
  Object.keys(confirmationQty).forEach((key) => delete confirmationQty[key])
  Object.keys(confirmationPrice).forEach((key) => delete confirmationPrice[key])
  executionItems.value.forEach((item) => { confirmationQty[item.id] = item.qty; confirmationPrice[item.id] = item.unitPrice })
  confirmationForm.confirmedDate = new Date().toISOString().slice(0, 10)
  confirmationForm.expectedDate = row.expectedDate
  confirmationForm.remark = ''
  await loadExecution()
  executionOpen.value = true
}

async function submitConfirmation() {
  saving.value = true
  try {
    await post(`/purchase-orders/${executing.value?.id}/supplier-confirmations`, {
      confirmed_date: confirmationForm.confirmedDate,
      confirmed_expected_date: confirmationForm.expectedDate,
      remark: confirmationForm.remark,
      lines: buildConfirmationLines(executionItems.value, confirmationQty, confirmationPrice),
    })
    ElMessage.success(t('orders.confirmationSaved'))
    await loadExecution()
  } finally { saving.value = false }
}

function productionNodeLabel(node: string): string { return t(`orders.productionNodes.${node}`) }

async function submitMilestone() {
  saving.value = true
  try {
    const attachments = milestoneForm.fileName && milestoneForm.fileUrl ? [{ file_name: milestoneForm.fileName, file_url: milestoneForm.fileUrl }] : []
    await post(`/purchase-orders/${executing.value?.id}/production-milestones`, {
      node: milestoneForm.node, planned_date: milestoneForm.plannedDate, actual_date: milestoneForm.actualDate,
      owner_name: milestoneForm.ownerName, remark: milestoneForm.remark, attachments,
    })
    ElMessage.success(t('orders.milestoneSaved'))
    await loadExecution()
  } finally { saving.value = false }
}

async function submitException() {
  saving.value = true
  try {
    await post(`/purchase-orders/${executing.value?.id}/exceptions`, {
      receipt_id: exceptionForm.receiptId, po_item_id: exceptionForm.itemId, exception_type: exceptionForm.type,
      qty: exceptionForm.qty, actual_product: exceptionForm.actualProduct, actual_uom: exceptionForm.actualUom,
      description: exceptionForm.description,
    })
    ElMessage.success(t('orders.exceptionReported'))
    exceptionForm.receiptId = undefined
    exceptionForm.itemId = undefined
    exceptionForm.qty = ''
    exceptionForm.actualProduct = ''
    exceptionForm.actualUom = ''
    exceptionForm.description = ''
    await loadExecution()
  } finally { saving.value = false }
}

async function resolveException(row: ReceiptException) {
  const { value } = await ElMessageBox.prompt(t('orders.resolutionPrompt'), t('orders.resolve'), { inputType: 'textarea' })
  await post(`/purchase-orders/${executing.value?.id}/exceptions/${row.id}/resolve`, { resolution: value })
  ElMessage.success(t('orders.exceptionResolved'))
  await loadExecution()
}

function openCancel(row: Order) {
  cancelling.value = row
  cancelReason.value = ''
  cancelOpen.value = true
}

async function submitCancel() {
  if (!cancelReason.value.trim()) {
    ElMessage.warning(t('orders.cancelReasonRequired'))
    return
  }
  saving.value = true
  try {
    await post(`/purchase-orders/${cancelling.value?.id}/cancel`, { reason: cancelReason.value })
    ElMessage.success(t('orders.cancelled'))
    cancelOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

function outstandingOf(item: OrderItem): number {
  return Number(item.qty ?? 0) - Number(item.receivedQty ?? 0)
}

async function openReceive(row: Order) {
  receiving.value = row
  const d = await get<{ items: OrderItem[] }>(`/purchase-orders/${row.id}`)
  receiveItems.value = d.items ?? []
  Object.keys(receiveQty).forEach((k) => delete receiveQty[k])
  // Pre-fill with everything still outstanding: a full delivery is the common
  // case, and a short one is edited down.
  receiveItems.value.forEach((it) => {
    const left = outstandingOf(it)
    receiveQty[it.id] = left > 0 ? String(left) : ''
  })
  if (!warehouses.value.length) {
    warehouses.value = (await get<{ warehouses: Warehouse[] }>('/warehouses')).warehouses ?? []
  }
  receiveWarehouse.value = Number(portWarehouses.value[0]?.id ?? 0)
  receiveOpen.value = true
}

async function submitReceive() {
  const lines = receiveItems.value
    .filter((it) => Number(receiveQty[it.id] ?? 0) > 0)
    .map((it) => ({ po_item_id: Number(it.id), qty: receiveQty[it.id] }))
  if (!lines.length) {
    ElMessage.warning(t('orders.receiveSomething'))
    return
  }
  saving.value = true
  try {
    const res = await post<{ receiptNo: string }>(`/purchase-orders/${receiving.value?.id}/receive`, {
      warehouse_id: receiveWarehouse.value,
      lines,
    })
    ElMessage.success(t('orders.receiptDone', { no: res.receiptNo }))
    receiveOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

function statusType(s: string): 'info' | 'warning' | 'primary' | 'success' | 'danger' {
  if (s === 'DRAFT') return 'info'
  if (s === 'PENDING_APPROVAL') return 'warning'
  if (s === 'ORDERED') return 'primary'
  if (s === 'PARTIALLY_RECEIVED') return 'warning'
  if (s === 'RECEIVED') return 'success'
  if (s === 'REJECTED') return 'danger'
  return 'info'
}

// 采购单列表仅保留“详情”为主操作，其余动作统一放进下拉菜单，
// 避免操作列随业务功能增加而持续横向膨胀。
function handleOrderAction(row: Order, command: string) {
  if (command === 'xlsx' || command === 'pdf') {
    void downloadOrder(row, command)
    return
  }
  if (command === 'receive') {
    void openReceive(row)
    return
  }
  if (command === 'execution') {
    void openExecution(row)
    return
  }
  if (command === 'cancel') openCancel(row)
}

// 人工审批通过并生成采购单后，业务状态统一显示为“已下单”。
// 供应商邮件仅是辅助沟通记录，不再构成第二次“下单”动作或业务状态门槛。
function orderStatusLabel(row: Order): string {
  if (row.status === 'ORDERED') return t('orders.businessStatuses.ORDERED')
  return t(`orders.statuses.${row.status}`)
}

function trim(v: string): string {
  if (!v) return '0'
  if (!v.includes('.')) return v
  return v.replace(/0+$/, '').replace(/\.$/, '')
}

function formatTime(v: string): string {
  return v ? v.replace('T', ' ').slice(0, 16) : '—'
}

// An approval decision lands asynchronously, so an order can go from
// "waiting" to "placed" while this page is open and nobody touched it.
const stopListening = onLive((event) => {
  if (event.type !== 'requirement.changed' && event.type !== 'doc.changed') return
  if (createOpen.value || receiveOpen.value || cancelOpen.value) return
  load()
})
onUnmounted(stopListening)

onMounted(async () => {
  await load()
  // ?requirements=1,2,3 — the buyer picked lines next door and came here to
  // turn them into an order.
  const picked = String(route.query.requirements ?? '').split(',').filter(Boolean)
  if (picked.length && canWrite) {
    approvalEntry.value = String(route.query.approval ?? '') === '1'
    await openCreate(picked)
    router.replace({ path: '/purchase-orders' })
  }
  const importedOrder = String(route.query.order ?? '')
  if (importedOrder) {
    try {
      const detailResponse = await get<{ order: Order; items: OrderItem[]; receipts: Receipt[] }>(`/purchase-orders/${importedOrder}`)
      detail.value = detailResponse.order
      detailItems.value = detailResponse.items ?? []
      detailReceipts.value = detailResponse.receipts ?? []
      detailOpen.value = true
      router.replace({ path: '/purchase-orders' })
    } catch {
      // The normal API error toast already explains an invalid or stale id.
    }
  }
  const kw = String(route.query.keyword ?? '')
  if (kw) {
    keyword.value = kw
    reload()
  }
})
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: baseline;
  gap: 14px;
  margin-bottom: 16px;
}
.page-head h2 {
  margin: 0;
  font-size: 20px;
}
.grow {
  flex: 1;
}
.head-note,
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.tabs {
  margin-bottom: 14px;
}
.execution-note {
  margin: 10px 0;
}
.history-table {
  margin-top: 16px;
}
.execution-section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 14px;
}
.overall-progress {
  width: 240px;
}
.execution-editor {
  padding: 16px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-extra-light);
}
.execution-form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 18px;
}
.execution-form-grid :deep(.el-form-item),
.execution-form-grid :deep(.el-select),
.execution-form-grid :deep(.el-date-editor) {
  width: 100%;
}
.span-all {
  grid-column: 1 / -1;
}
.optional-fields {
  margin-bottom: 14px;
}
.attachment-grid {
  padding-top: 8px;
}
.history-table a + a {
  margin-left: 8px;
}
.filters {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
}
.row-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  white-space: nowrap;
}
.row-actions :deep(.el-button) {
  margin-left: 0;
}
.prod {
  font-weight: 500;
}
.num {
  font-variant-numeric: tabular-nums;
}
.money {
  font-weight: 600;
}
.dim {
  color: var(--el-text-color-placeholder);
}
.reason {
  margin-top: 2px;
  line-height: 1.4;
}
.alert {
  margin-bottom: 14px;
}
.head-form {
  margin-bottom: 6px;
}
.supplier-row {
  display: flex;
  align-items: center;
  gap: 12px;
}
.side-title {
  margin: 14px 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
.total-row {
  margin-top: 10px;
  text-align: right;
  font-size: 13px;
}
.total-row .money {
  margin-left: 8px;
  font-size: 16px;
}
.desc {
  margin-bottom: 14px;
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
@media (max-width: 760px) {
  .execution-section-head { align-items: flex-start; flex-direction: column; }
  .overall-progress { width: 100%; }
  .execution-form-grid { grid-template-columns: 1fr; }
  .span-all { grid-column: auto; }
}
</style>
