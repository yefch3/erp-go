<template>
  <div>
    <div class="page-head">
      <h2>{{ t('orders.title') }}</h2>
      <span class="head-note">{{ t('orders.subtitle') }}</span>
      <span class="grow" />
      <el-button @click="router.push('/procurement')">← {{ t('procurementNav.backToWorkbench') }}</el-button>
      <el-button v-if="canWrite" type="primary" @click="openCreate">{{ t('orders.create') }}</el-button>
    </div>

    <el-card shadow="never">
      <el-radio-group v-model="status" class="tabs" @change="reload">
        <el-radio-button value="">{{ t('orders.allStatuses') }}</el-radio-button>
        <el-radio-button value="DRAFT">{{ t('orders.statuses.DRAFT') }}</el-radio-button>
        <el-radio-button value="PENDING_APPROVAL">{{ t('orders.statuses.PENDING_APPROVAL') }}</el-radio-button>
        <el-radio-button value="UNSENT">{{ t('orders.unsentTab') }}</el-radio-button>
        <el-radio-button value="ORDERED">{{ t('orders.statuses.ORDERED') }}</el-radio-button>
        <el-radio-button value="PARTIALLY_RECEIVED">{{ t('orders.statuses.PARTIALLY_RECEIVED') }}</el-radio-button>
        <el-radio-button value="RECEIVED">{{ t('orders.statuses.RECEIVED') }}</el-radio-button>
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
        <el-table-column :label="t('orders.amount')" width="150" align="right">
          <template #default="{ row }">
            <span class="num money">{{ row.currency }} {{ row.totalAmount }}</span>
            <div class="sub">{{ t('orders.lines', { n: row.itemCount }) }}</div>
          </template>
        </el-table-column>
        <!-- Progress only means something once part of it has arrived; a bar
             at zero on every open order is noise. -->
        <el-table-column :label="t('orders.received')" width="130" align="right">
          <template #default="{ row }">
            <template v-if="Number(row.receivedQty) > 0">
              <span class="num">{{ trim(row.receivedQty) }} / {{ trim(row.totalQty) }}</span>
            </template>
            <span v-else class="num dim">{{ trim(row.totalQty) }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.expected')" width="110">
          <template #default="{ row }">{{ row.expectedDate || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="190">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)" effect="plain">
              {{ t(`orders.statuses.${row.status}`) }}
            </el-tag>
            <el-tag
              v-if="['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status)"
              size="small" effect="plain" style="margin-left: 4px"
              :type="row.sendStatus === 'SENT' ? 'success' : row.sendStatus === 'FAILED' ? 'danger' : 'info'"
            >{{ row.sendStatus === 'SENT' ? t('orders.sentTag') : row.sendStatus === 'FAILED' ? t('orders.sendFailedTag') : t('orders.unsentTag') }}</el-tag>
            <el-tag v-if="confirmTag(row)" size="small" effect="plain" :type="confirmTag(row)!.type" style="margin-left: 4px">
              {{ confirmTag(row)!.label }}
            </el-tag>
            <el-tag v-if="row.closedAt" size="small" type="success" style="margin-left: 4px">
              {{ t('orders.closedTag') }}
            </el-tag>
            <div v-if="row.rejectReason || row.cancelReason" class="sub reason">
              {{ row.rejectReason || row.cancelReason }}
            </div>
          </template>
        </el-table-column>
        <!-- 一行只亮「当前该做的那一个动作」，其余收进「更多」（B5）。
             七个按钮同排的年代，采购员点错的不是手，是布局。 -->
        <el-table-column :label="t('common.actions')" width="235" fixed="right">
          <template #default="{ row }">
            <div class="row-actions">
              <el-button link type="primary" @click="openDetail(row)">{{ t('common.detail') }}</el-button>
              <el-button
                v-if="primaryAction(row)"
                link :type="primaryAction(row)!.tone"
                :loading="primaryAction(row)!.key === 'download' && downloadingId === Number(row.id)"
                @click="primaryAction(row)!.run()"
              >{{ primaryAction(row)!.label }}</el-button>
              <el-dropdown v-if="moreActions(row).length" trigger="click" @command="(key: string) => runMoreAction(row, key)">
                <el-button link>{{ t('orders.more') }} ▾</el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item v-for="action in moreActions(row)" :key="action.key" :command="action.key">
                      {{ action.label }}
                    </el-dropdown-item>
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
            <el-select v-model="form.supplierId" filterable style="width: 320px">
              <el-option
                v-for="s in suppliers"
                :key="s.id"
                :value="Number(s.id)"
                :label="`${s.code} · ${s.name}`"
              />
            </el-select>
            <!-- There is no supplier page yet, and a purchase order without a
                 supplier cannot exist. Creating one here beats blocking the
                 whole feature on a screen nobody asked for. -->
            <el-button v-if="canManageSupplier" link type="primary" @click="supplierOpen = true">
              {{ t('orders.newSupplier') }}
            </el-button>
          </div>
        </el-form-item>
        <el-form-item :label="t('orders.expected')">
          <el-date-picker v-model="form.expectedDate" type="date" value-format="YYYY-MM-DD" style="width: 200px" />
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
        <el-button type="primary" :loading="saving" @click="submitCreate">{{ common('save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="supplierOpen" :title="t('orders.newSupplier')" width="460px">
      <el-form label-width="80px">
        <el-form-item :label="t('orders.supplierCode')">
          <el-input v-model="supplierForm.code" :placeholder="t('orders.autoCode')" style="width: 200px" />
        </el-form-item>
        <el-form-item :label="t('orders.supplierName')" required>
          <el-input v-model="supplierForm.name" />
        </el-form-item>
        <el-form-item :label="t('orders.country')">
          <el-input v-model="supplierForm.country" style="width: 200px" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="supplierOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="createSupplier">{{ common('save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailOpen" :title="detail?.poNo" width="820px">
      <!-- B5 尾巴：看完单子不用回列表找按钮——下一步就在眼前。 -->
      <div v-if="detailNext" class="next-step">
        <span class="next-step-label">{{ t('orders.nextStep') }}</span>
        <el-button size="small" :type="detailNext.tone" @click="runDetailNext">{{ detailNext.label }}</el-button>
      </div>
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
        <el-descriptions-item :label="t('orders.confirmations')">
          <template v-if="detail && confirmTag(detail)">{{ confirmTag(detail)!.label }}</template>
          <template v-else>—</template>
        </el-descriptions-item>
      </el-descriptions>

      <el-table :data="detailItems" size="small">
        <el-table-column :label="t('orders.product')" min-width="180">
          <template #default="{ row }">
            <div>{{ row.productName }}</div>
            <div class="sub">
              <template v-if="row.source === 'MANUAL'">{{ t('orders.manual') }}</template>
              <template v-else>{{ row.contractNo }} · {{ row.customerName }}<template v-if="row.contractOwner"> · {{ t('orders.contractOwner') }} {{ row.contractOwner }}</template></template>
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

    <el-dialog v-model="sendOpen" :title="t('orders.sendFor', { no: sending?.poNo })" width="560px">
      <el-alert type="info" :closable="false" show-icon class="alert">{{ t('orders.sendHint') }}</el-alert>
      <el-form label-width="110px">
        <el-form-item :label="t('orders.sender')">
          <el-radio-group v-model="sendForm.senderMode">
            <el-radio value="PUBLIC">{{ t('orders.publicMailbox') }}</el-radio>
            <el-radio value="ME">{{ t('orders.myMailbox') }}</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item :label="t('orders.recipient')">
          <el-input v-model="sendForm.recipientEmail" :placeholder="t('orders.supplierDefaultEmail')" />
        </el-form-item>
        <el-form-item :label="t('orders.subject')"><el-input v-model="sendForm.subject" /></el-form-item>
        <el-form-item :label="t('orders.mailBody')"><el-input v-model="sendForm.body" type="textarea" :rows="5" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="sendOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitSend">{{ t('orders.sendOrder') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="executionOpen" :title="t('orders.executionFor', { no: executing?.poNo })" width="960px">
      <el-tabs v-model="executionTab">
        <el-tab-pane :label="t('orders.confirmations')" name="confirmation">
          <el-form v-if="canProduction" label-width="110px" inline>
            <el-form-item :label="t('orders.confirmedDate')"><el-date-picker v-model="confirmationForm.confirmedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
            <el-form-item :label="t('orders.confirmedExpected')"><el-date-picker v-model="confirmationForm.expectedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
          </el-form>
          <el-table :data="executionItems" size="small">
            <el-table-column prop="productName" :label="t('orders.product')" min-width="180" />
            <el-table-column :label="t('orders.orderQty')" width="110"><template #default="{row}">{{ trim(row.qty) }} {{ row.uomCode }}</template></el-table-column>
            <el-table-column :label="t('orders.confirmedQty')" width="130"><template #default="{row}"><el-input v-model="confirmationQty[row.id]" size="small" :disabled="!canProduction" /></template></el-table-column>
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
          <el-form v-if="canProduction" label-width="100px" inline>
            <el-form-item :label="t('orders.productionNode')"><el-select v-model="milestoneForm.node" style="width:190px"><el-option v-for="node in productionNodes" :key="node" :value="node" :label="productionNodeLabel(node)" /></el-select></el-form-item>
            <el-form-item :label="t('orders.plannedDate')"><el-date-picker v-model="milestoneForm.plannedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
            <el-form-item :label="t('orders.actualDate')"><el-date-picker v-model="milestoneForm.actualDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
            <el-form-item :label="t('orders.owner')"><el-input v-model="milestoneForm.ownerName" style="width:180px" /></el-form-item>
            <el-form-item :label="t('orders.attachmentName')"><el-input v-model="milestoneForm.fileName" style="width:180px" /></el-form-item>
            <el-form-item :label="t('orders.attachmentUrl')"><el-input v-model="milestoneForm.fileUrl" style="width:260px" /></el-form-item>
          </el-form>
          <el-input v-if="canProduction" v-model="milestoneForm.remark" type="textarea" :rows="2" :placeholder="t('orders.remark')" class="execution-note" />
          <el-button v-if="canProduction" type="primary" :loading="saving" @click="submitMilestone">{{ t('orders.saveMilestone') }}</el-button>
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
          <el-form v-if="canException" label-width="100px" inline>
            <el-form-item :label="t('orders.exceptionType')"><el-select v-model="exceptionForm.type" style="width:180px"><el-option v-for="kind in exceptionTypes" :key="kind" :value="kind" :label="t(`orders.exceptionTypes.${kind}`)" /></el-select></el-form-item>
            <el-form-item :label="t('orders.receiptNo')"><el-select v-model="exceptionForm.receiptId" clearable style="width:180px"><el-option v-for="receipt in executionReceipts" :key="receipt.id" :value="Number(receipt.id)" :label="receipt.receiptNo" /></el-select></el-form-item>
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

        <el-tab-pane :label="t('orders.inspections')" name="inspections">
          <el-form v-if="canException" label-width="100px" inline>
            <el-form-item :label="t('orders.receiptNo')" required><el-select v-model="inspectionForm.receiptId" style="width:180px"><el-option v-for="receipt in executionReceipts" :key="receipt.id" :value="Number(receipt.id)" :label="receipt.receiptNo" /></el-select></el-form-item>
            <el-form-item :label="t('orders.product')"><el-select v-model="inspectionForm.itemId" clearable style="width:200px"><el-option v-for="item in executionItems" :key="item.id" :value="Number(item.id)" :label="item.productName" /></el-select></el-form-item>
            <el-form-item :label="t('orders.inspectionResult')"><el-select v-model="inspectionForm.result" style="width:130px"><el-option value="PASS" :label="t('orders.inspectionResults.PASS')" /><el-option value="FAIL" :label="t('orders.inspectionResults.FAIL')" /></el-select></el-form-item>
            <el-form-item :label="t('orders.inspectedQty')"><el-input v-model="inspectionForm.inspectedQty" style="width:120px" /></el-form-item>
            <el-form-item v-if="inspectionForm.result === 'FAIL'" :label="t('orders.defectQty')"><el-input v-model="inspectionForm.defectQty" style="width:120px" /></el-form-item>
            <el-form-item :label="t('orders.attachmentName')"><el-input v-model="inspectionForm.fileName" style="width:180px" /></el-form-item>
            <el-form-item :label="t('orders.attachmentUrl')"><el-input v-model="inspectionForm.fileUrl" style="width:260px" /></el-form-item>
          </el-form>
          <el-input v-if="canException" v-model="inspectionForm.note" type="textarea" :rows="2" :placeholder="t('orders.inspectionNote')" class="execution-note" />
          <el-button v-if="canException" type="primary" :loading="saving" @click="submitInspection">{{ t('orders.recordInspection') }}</el-button>
          <el-table :data="execution.inspections" size="small" class="history-table">
            <el-table-column prop="receiptNo" :label="t('orders.receiptNo')" width="150" />
            <el-table-column :label="t('orders.inspectionResult')" width="90"><template #default="{row}"><el-tag size="small" effect="plain" :type="row.result === 'PASS' ? 'success' : 'danger'">{{ t(`orders.inspectionResults.${row.result}`) }}</el-tag></template></el-table-column>
            <el-table-column prop="inspectedQty" :label="t('orders.inspectedQty')" width="90" />
            <el-table-column prop="defectQty" :label="t('orders.defectQty')" width="90" />
            <el-table-column prop="note" :label="t('orders.remark')" min-width="150" />
            <el-table-column :label="t('orders.attachments')" min-width="130"><template #default="{row}"><a v-for="file in row.attachments" :key="file.fileUrl" :href="file.fileUrl" target="_blank">{{ file.fileName }}</a></template></el-table-column>
            <el-table-column :label="t('common.status')" width="90"><template #default="{row}">{{ t(`orders.inspectionStatuses.${row.status}`) }}</template></el-table-column>
            <el-table-column :label="t('orders.disposition')" min-width="140"><template #default="{row}"><template v-if="row.disposition">{{ t(`orders.dispositions.${row.disposition}`) }}<span v-if="row.dispositionNote" class="sub"> {{ row.dispositionNote }}</span></template><span v-else>—</span></template></el-table-column>
            <el-table-column :label="t('common.actions')" width="90"><template #default="{row}"><el-button v-if="canException && row.status === 'OPEN'" link type="primary" @click="openDisposition(row)">{{ t('orders.dispose') }}</el-button></template></el-table-column>
          </el-table>
        </el-tab-pane>
      </el-tabs>
    </el-dialog>

    <el-dialog v-model="dispositionOpen" :title="t('orders.dispose')" width="440px">
      <el-form label-width="90px">
        <el-form-item :label="t('orders.disposition')" required>
          <el-select v-model="dispositionForm.disposition" style="width:100%">
            <el-option v-for="kind in dispositionKinds" :key="kind" :value="kind" :label="t(`orders.dispositions.${kind}`)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('orders.remark')">
          <el-input v-model="dispositionForm.note" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dispositionOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitDisposition">{{ common('save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { download, get, post, put, saveBlob } from '../api'
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
  closedAt: string
  closedBy: string
  confirmStatus: string
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
  contractOwner: string
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
}
interface Supplier { id: string; code: string; name: string }
interface Warehouse { id: string; code: string; name: string; whType: string }
interface SupplierConfirmation { id: string; status: string; confirmedDate: string; confirmedExpectedDate: string; remark: string; createdBy: string }
interface ProductionAttachment { fileName: string; fileUrl: string; contentType: string }
interface ProductionMilestone { id: string; node: string; plannedDate: string; actualDate: string; ownerName: string; remark: string; delayed: boolean; attachments: ProductionAttachment[] }
interface ReceiptException { id: string; exceptionType: string; qty: string; description: string; status: string; resolution: string }
interface ProductionReminder { id: string; node: string; plannedDate: string; buyerName: string; relatedContracts: string; status: string }
interface PurchaseInspection { id: string; receiptNo: string; result: string; inspectedQty: string; defectQty: string; note: string; attachments: ProductionAttachment[]; status: string; disposition: string; dispositionNote: string }
interface Execution { confirmations: SupplierConfirmation[]; milestones: ProductionMilestone[]; exceptions: ReceiptException[]; reminders: ProductionReminder[]; inspections: PurchaseInspection[] }

const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const canWrite = auth.can('procurement:order:write')
const canSubmit = auth.can('procurement:order:submit')
const canCancel = auth.can('procurement:order:cancel')
const canReceive = auth.can('procurement:receipt:write')
const canSend = auth.can('procurement:order:send')
const canProduction = auth.can('procurement:production:write')
const canException = auth.can('procurement:exception:write')
const canClose = auth.can('procurement:order:close')
const canManageSupplier = auth.can('masterdata:supplier:write')

const rows = ref<Order[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const status = ref('')
const keyword = ref('')
const loading = ref(false)
const saving = ref(false)
const downloadingId = ref(0)

const createOpen = ref(false)
const editing = ref<Order | null>(null)
const pending = ref<Requirement[]>([])
const suppliers = ref<Supplier[]>([])
const qtyOf = reactive<Record<string, string>>({})
const priceOf = reactive<Record<string, string>>({})
const form = reactive({ supplierId: 0, currency: 'CNY', expectedDate: '', remark: '' })

const supplierOpen = ref(false)
const supplierForm = reactive({ code: '', name: '', country: '' })

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

const sendOpen = ref(false)
const sending = ref<Order | null>(null)
const sendForm = reactive({ senderMode: 'PUBLIC', recipientEmail: '', subject: '', body: '' })

const executionOpen = ref(false)
const executionTab = ref('confirmation')
const executing = ref<Order | null>(null)
const executionItems = ref<OrderItem[]>([])
const executionReceipts = ref<Receipt[]>([])
const execution = reactive<Execution>({ confirmations: [], milestones: [], exceptions: [], reminders: [], inspections: [] })
const confirmationQty = reactive<Record<string, string>>({})
const confirmationPrice = reactive<Record<string, string>>({})
const confirmationForm = reactive({ confirmedDate: '', expectedDate: '', remark: '' })
const productionNodes = ['PENDING_SCHEDULE', 'SCHEDULED', 'IN_PRODUCTION', 'QUALITY_INSPECTION', 'READY_TO_SHIP', 'SENT_TO_PORT']
const milestoneForm = reactive({ node: 'PENDING_SCHEDULE', plannedDate: '', actualDate: '', ownerName: '', fileName: '', fileUrl: '', remark: '' })
const exceptionTypes = ['WRONG_PRODUCT', 'UNIT_MISMATCH', 'SHORT_SHIPMENT', 'DAMAGE', 'QUALITY_DISPUTE', 'RETURN']
const exceptionForm = reactive({ type: 'SHORT_SHIPMENT', receiptId: 0, itemId: 0, qty: '0', actualProduct: '', actualUom: '', description: '' })
const inspectionForm = reactive({ receiptId: 0, itemId: 0, result: 'PASS', inspectedQty: '0', defectQty: '0', fileName: '', fileUrl: '', note: '' })
const dispositionKinds = ['RETURN', 'DEDUCTION', 'CONCESSION', 'REWORK']
const dispositionOpen = ref(false)
const dispositionTarget = ref<PurchaseInspection | null>(null)
const dispositionForm = reactive({ disposition: 'DEDUCTION', note: '' })

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

// 当前主动作按订单状态推导（B5）：草稿去提交、批完去发单、发完去收货、
// 收完看履约。其余动作全部收进「更多」。
interface RowAction { key: string; label: string; tone: 'primary' | 'success' | 'warning' | 'danger'; run: () => void }
function primaryAction(row: Order): RowAction | null {
  if ((row.status === 'DRAFT' || row.status === 'REJECTED') && canSubmit)
    return { key: 'submit', label: t('orders.submit'), tone: 'success', run: () => submit(row) }
  if ((row.status === 'DRAFT' || row.status === 'REJECTED') && canWrite)
    return { key: 'edit', label: common('edit'), tone: 'primary', run: () => openEdit(row) }
  if (['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status) && row.sendStatus !== 'SENT' && canSend)
    return { key: 'send', label: row.sendStatus === 'FAILED' ? t('orders.retrySend') : t('orders.sendOrder'), tone: 'success', run: () => openSend(row) }
  if (['ORDERED', 'PARTIALLY_RECEIVED'].includes(row.status) && canReceive)
    return { key: 'receive', label: t('orders.receive'), tone: 'warning', run: () => openReceive(row) }
  // 收满、发过、还没结案——当前该做的就是宣布这单到此为止（A3）。
  if (row.status === 'RECEIVED' && !row.closedAt && canClose)
    return { key: 'close', label: t('orders.closeOrder'), tone: 'primary', run: () => void closeOrder(row) }
  if (['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status))
    return { key: 'execution', label: t('orders.execution'), tone: 'warning', run: () => openExecution(row) }
  return null
}
function moreActions(row: Order): { key: string; label: string }[] {
  const primary = primaryAction(row)?.key
  const out: { key: string; label: string }[] = []
  const add = (key: string, label: string, allowed: boolean) => { if (allowed && key !== primary) out.push({ key, label }) }
  add('edit', common('edit'), canWrite && (row.status === 'DRAFT' || row.status === 'REJECTED'))
  add('submit', t('orders.submit'), canSubmit && (row.status === 'DRAFT' || row.status === 'REJECTED'))
  add('send', row.sendStatus === 'FAILED' ? t('orders.retrySend') : t('orders.sendOrder'),
    canSend && ['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status) && row.sendStatus !== 'SENT')
  add('receive', t('orders.receive'), canReceive && ['ORDERED', 'PARTIALLY_RECEIVED'].includes(row.status))
  add('execution', t('orders.execution'), ['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status))
  add('downloadXlsx', t('orders.downloadExcel'), ['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status))
  add('downloadPdf', t('orders.downloadPdf'), ['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status))
  add('close', t('orders.closeOrder'), canClose && row.status === 'RECEIVED' && !row.closedAt)
  add('cancel', common('cancel'), canCancel && ['DRAFT', 'REJECTED', 'ORDERED'].includes(row.status))
  return out
}
// 工厂回签状态的列表子标签（B5 尾巴）。只在已发单之后才有意义：
// 没发出去的单谈不上「工厂还没回」，那时未发单标签已经说明了一切。
function confirmTag(row: Order): { label: string; type: 'success' | 'warning' | 'danger' | 'info' } | null {
  if (row.sendStatus !== 'SENT') return null
  if (!['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status)) return null
  const s = row.confirmStatus
  if (s === 'MATCHED' || s === 'APPROVED') return { label: t('orders.confirmTags.CONFIRMED'), type: 'success' }
  if (s === 'PENDING_APPROVAL') return { label: t('orders.confirmTags.PENDING_APPROVAL'), type: 'warning' }
  if (s === 'REJECTED') return { label: t('orders.confirmTags.REJECTED'), type: 'danger' }
  return { label: t('orders.confirmTags.NONE'), type: 'info' }
}

// 详情页的「下一步」与列表主动作同一套推导——两处永远说同一句话。
const detailNext = computed(() => (detail.value ? primaryAction(detail.value) : null))
function runDetailNext() {
  const next = detailNext.value
  if (!next) return
  detailOpen.value = false
  next.run()
}

function runMoreAction(row: Order, key: string) {
  switch (key) {
    case 'edit': openEdit(row); break
    case 'submit': void submit(row); break
    case 'send': openSend(row); break
    case 'receive': openReceive(row); break
    case 'execution': openExecution(row); break
    case 'downloadXlsx': void downloadOrder(row, 'xlsx'); break
    case 'downloadPdf': void downloadOrder(row, 'pdf'); break
    case 'close': void closeOrder(row); break
    case 'cancel': openCancel(row); break
  }
}

async function load() {
  loading.value = true
  try {
    // UNSENT 是前端造的伪状态：ORDERED 且未发单，服务端用 unsent 参数过滤。
    const unsent = status.value === 'UNSENT'
    const d = await get<{ orders: Order[]; meta: { total: number } }>('/purchase-orders', {
      page: page.value, page_size: pageSize,
      status: unsent ? 'ORDERED' : status.value, unsent: unsent ? '1' : '',
      keyword: keyword.value,
    })
    rows.value = d.orders ?? []
    total.value = Number(d.meta?.total ?? 0)
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
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
  Object.keys(qtyOf).forEach((k) => delete qtyOf[k])
  Object.keys(priceOf).forEach((k) => delete priceOf[k])
  const [reqs, sups] = await Promise.all([
    get<{ requirements: Requirement[] }>('/requirements', { status: 'PENDING', page_size: 200 }),
    get<{ suppliers: Supplier[] }>('/suppliers', { page_size: 200 }),
  ])
  // Partially ordered ones belong here too: what is left is still owed.
  const partial = await get<{ requirements: Requirement[] }>('/requirements', {
    status: 'PARTIALLY_ORDERED', page_size: 200,
  })
  pending.value = [...(reqs.requirements ?? []), ...(partial.requirements ?? [])]
  suppliers.value = sups.suppliers ?? []
  // Arriving from the requirements page with lines already chosen: pre-fill
  // exactly those and leave the rest blank, so the buyer does not have to
  // find them again by product name.
  if (preselect?.length) {
    const wanted = new Set(preselect)
    pending.value.forEach((r) => {
      if (wanted.has(String(r.id))) qtyOf[r.id] = String(Number(r.requiredQty) - Number(r.orderedQty))
    })
  }
  createOpen.value = true
}

async function openEdit(row: Order) {
  const [detailData, reqs, partial, sups] = await Promise.all([
    get<{ order: Order; items: OrderItem[] }>(`/purchase-orders/${row.id}`),
    get<{ requirements: Requirement[] }>('/requirements', { status: 'PENDING', page_size: 200 }),
    get<{ requirements: Requirement[] }>('/requirements', { status: 'PARTIALLY_ORDERED', page_size: 200 }),
    get<{ suppliers: Supplier[] }>('/suppliers', { page_size: 200 }),
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
  Object.keys(qtyOf).forEach((key) => delete qtyOf[key])
  Object.keys(priceOf).forEach((key) => delete priceOf[key])
  pending.value = [...(reqs.requirements ?? []), ...(partial.requirements ?? [])]
  suppliers.value = sups.suppliers ?? []
  for (const item of detailData.items ?? []) {
    qtyOf[item.requirementId] = item.qty
    priceOf[item.requirementId] = item.unitPrice
  }
  createOpen.value = true
}

async function createSupplier() {
  if (!supplierForm.name.trim()) {
    ElMessage.warning(t('orders.supplierNameRequired'))
    return
  }
  saving.value = true
  try {
    await post('/suppliers', {
      code: supplierForm.code, name: supplierForm.name, country: supplierForm.country,
    })
    suppliers.value = (await get<{ suppliers: Supplier[] }>('/suppliers', { page_size: 200 })).suppliers ?? []
    const created = suppliers.value.find((s) => s.name === supplierForm.name)
    if (created) form.supplierId = Number(created.id)
    supplierForm.code = ''
    supplierForm.name = ''
    supplierForm.country = ''
    supplierOpen.value = false
  } finally {
    saving.value = false
  }
}

async function submitCreate() {
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
  saving.value = true
  try {
    const payload = {
      supplier_id: Number(supplier.id),
      currency: form.currency,
      expected_date: form.expectedDate,
      remark: form.remark,
      lines,
    }
    const res = editing.value
      ? await put<{ poNo: string }>(`/purchase-orders/${editing.value.id}`, payload)
      : await post<{ poNo: string }>('/purchase-orders', payload)
    ElMessage.success(editing.value
      ? t('orders.updated', { no: res.poNo })
      : t('orders.created', { no: res.poNo }))
    createOpen.value = false
    editing.value = null
    reload()
  } finally {
    saving.value = false
  }
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

function openSend(row: Order) {
  sending.value = row
  sendForm.senderMode = 'PUBLIC'
  sendForm.recipientEmail = ''
  sendForm.subject = `Purchase Order ${row.poNo}`
  sendForm.body = ''
  sendOpen.value = true
}

async function submitSend() {
  saving.value = true
  try {
    await post(`/purchase-orders/${sending.value?.id}/send`, {
      sender_mode: sendForm.senderMode,
      recipient_email: sendForm.recipientEmail,
      subject: sendForm.subject,
      body: sendForm.body,
    })
    ElMessage.success(t('orders.sent'))
    sendOpen.value = false
    await load()
  } finally { saving.value = false }
}

async function loadExecution() {
  if (!executing.value) return
  const data = await get<Execution>(`/purchase-orders/${executing.value.id}/execution`)
  execution.confirmations = data.confirmations ?? []
  execution.milestones = data.milestones ?? []
  execution.exceptions = data.exceptions ?? []
  execution.reminders = data.reminders ?? []
  execution.inspections = data.inspections ?? []
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
  // 质检默认对着最新一张收货单——多数时候检的就是刚到的那批。
  inspectionForm.receiptId = Number(executionReceipts.value[0]?.id ?? 0)
  inspectionForm.result = 'PASS'
  inspectionForm.inspectedQty = '0'
  inspectionForm.defectQty = '0'
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

async function submitInspection() {
  saving.value = true
  try {
    const attachments = inspectionForm.fileName && inspectionForm.fileUrl ? [{ file_name: inspectionForm.fileName, file_url: inspectionForm.fileUrl }] : []
    await post(`/purchase-orders/${executing.value?.id}/inspections`, {
      receipt_id: inspectionForm.receiptId, po_item_id: inspectionForm.itemId, result: inspectionForm.result,
      inspected_qty: inspectionForm.inspectedQty, defect_qty: inspectionForm.result === 'FAIL' ? inspectionForm.defectQty : '0',
      note: inspectionForm.note, attachments,
    })
    ElMessage.success(t('orders.inspectionSaved'))
    inspectionForm.note = ''
    inspectionForm.fileName = ''
    inspectionForm.fileUrl = ''
    await loadExecution()
  } finally { saving.value = false }
}

function openDisposition(row: PurchaseInspection) {
  dispositionTarget.value = row
  dispositionForm.disposition = 'DEDUCTION'
  dispositionForm.note = ''
  dispositionOpen.value = true
}

async function submitDisposition() {
  if (!dispositionTarget.value) return
  saving.value = true
  try {
    await post(`/purchase-orders/${executing.value?.id}/inspections/${dispositionTarget.value.id}/resolve`, {
      disposition: dispositionForm.disposition, disposition_note: dispositionForm.note,
    })
    ElMessage.success(t('orders.inspectionResolved'))
    dispositionOpen.value = false
    await loadExecution()
  } finally { saving.value = false }
}

async function closeOrder(row: Order) {
  await ElMessageBox.confirm(t('orders.closeConfirm', { no: row.poNo }), t('orders.closeOrder'), {
    type: 'warning',
    confirmButtonText: t('orders.closeOrder'),
    cancelButtonText: common('cancel'),
  })
  await post(`/purchase-orders/${row.id}/close`, {})
  ElMessage.success(t('orders.closedOk'))
  load()
}

async function submit(row: Order) {
  await ElMessageBox.confirm(t('orders.submitWarning', { no: row.poNo }), t('orders.submit'), {
    type: 'warning',
    confirmButtonText: t('orders.submit'),
    cancelButtonText: common('cancel'),
  })
  await post(`/purchase-orders/${row.id}/submit`, {})
  ElMessage.success(t('orders.submitted'))
  load()
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
  // 工作台的指标点进来要落在筛好的列表上（B4），不是全量。
  const qsStatus = String(route.query.status ?? '')
  const qsUnsent = String(route.query.unsent ?? '')
  if (qsUnsent === '1') {
    status.value = 'UNSENT'
    reload()
  } else if (qsStatus) {
    status.value = qsStatus
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
.next-step {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  margin-bottom: 12px;
  border-radius: 6px;
  background: var(--el-fill-color-light);
}
.next-step-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
</style>
