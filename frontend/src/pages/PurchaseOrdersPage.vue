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
        <el-radio-button value="DRAFT">{{ t('orders.statuses.DRAFT') }}</el-radio-button>
        <el-radio-button value="PENDING_APPROVAL">{{ t('orders.statuses.PENDING_APPROVAL') }}</el-radio-button>
        <el-radio-button value="REJECTED">{{ t('orders.statuses.REJECTED') }}</el-radio-button>
        <el-radio-button value="ORDERED">{{ t('orders.statuses.ORDERED') }}</el-radio-button>
        <el-radio-button value="PARTIALLY_RECEIVED">{{ t('orders.statuses.PARTIALLY_RECEIVED') }}</el-radio-button>
        <el-radio-button value="RECEIVED_OPEN">{{ t('orders.statuses.RECEIVED') }}</el-radio-button>
        <el-radio-button value="HISTORY">{{ t('orders.statuses.HISTORY') }}</el-radio-button>
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
        <el-table-column :label="t('common.status')" width="190">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)" effect="plain">
              {{ orderStatusLabel(row) }}
            </el-tag>
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
        <!-- 审批人最关心的是直接决策；审批中的本人待办把通过/驳回放在列表上，
             其他低频动作仍收进菜单，避免误把“有读取权限”当成“可以审批”。 -->
        <el-table-column :label="t('common.actions')" width="190" align="center" fixed="right">
          <template #default="{ row }">
            <div class="row-actions">
              <template v-if="canActOnOrderApproval(row)">
                <el-button link type="success" @click.stop="actOnOrderApproval(row, 'APPROVE')">{{ t('todos.approve') }}</el-button>
                <el-button link type="danger" @click.stop="actOnOrderApproval(row, 'REJECT')">{{ t('todos.reject') }}</el-button>
              </template>
              <el-dropdown trigger="click" @command="(key: string) => runOrderAction(row, key)">
                <el-button class="action-trigger" size="small" text circle :aria-label="t('common.actions')">
                  <span aria-hidden="true">•••</span>
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item
                      v-for="action in rowMenuActions(row)"
                      :key="action.key"
                      :command="action.key"
                      :divided="action.divided"
                    >
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
            <el-select v-model="form.supplierId" filterable :disabled="supplierLocked" style="width: 420px">
              <el-option
                v-for="s in suppliers"
                :key="s.id"
                :value="Number(s.id)"
                :label="`${s.code} · ${s.name}`"
              />
            </el-select>
            <el-button v-if="approvalEntry && scenarioSupplierName" link type="primary" @click="toggleSupplierSwitch">
              {{ switchSupplier ? `改回${scenarioSupplierName}` : '改向其他供应商' }}
            </el-button>
          </div>
          <div v-if="scenarioSupplierName" class="form-note">
            比价确认的方案定的是「{{ scenarioSupplierName }}」<template v-if="switchSupplier">，换一家要在下面写明原因</template>
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
        <el-form-item label="调整原因" :required="reasonRequired">
          <el-input v-model="form.sourceChangeReason" :placeholder="reasonPlaceholder" />
          <div v-if="isReorder && !switchSupplier" class="form-note">
            这批之前已经下过单，本次买的是剩下的数量
          </div>
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
          {{ approvalEntry ? t('orders.createDraft') : common('save') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="detailOpen" :title="detail?.poNo" width="820px">
      <!-- B5 尾巴：看完单子不用回列表找按钮——下一步就在眼前。 -->
      <div v-if="detail && approvalTaskFor(detail)" class="next-step">
        <span class="next-step-label">{{ t('orders.approvalDecision') }}</span>
        <el-button size="small" type="success" @click="actOnOrderApproval(detail, 'APPROVE')">{{ t('todos.approve') }}</el-button>
        <el-button size="small" type="danger" @click="actOnOrderApproval(detail, 'REJECT')">{{ t('todos.reject') }}</el-button>
      </div>
      <div v-else-if="detailNext" class="next-step">
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

    <!-- 货没到齐还要关单：人得先说清剩下的怎么办（A5）。 -->
    <el-dialog v-model="closeShortOpen" :title="t('orders.closeShortTitle')" width="520px">
      <el-alert v-if="closingOrder" type="warning" :closable="false" show-icon class="alert">
        {{ t('orders.closeShortHint', {
          no: closingOrder.poNo,
          ordered: trim(closingOrder.totalQty),
          received: trim(closingOrder.receivedQty),
          gap: trim(String(Number(closingOrder.totalQty) - Number(closingOrder.receivedQty))),
        }) }}
      </el-alert>
      <el-form label-width="120px">
        <el-form-item :label="t('orders.shortfallAction')" required>
          <el-radio-group v-model="closeForm.shortfallAction">
            <el-radio value="REORDER">{{ t('orders.shortfallReorder') }}</el-radio>
            <el-radio value="DROPPED">{{ t('orders.shortfallDropped') }}</el-radio>
          </el-radio-group>
          <div class="sub">
            {{ closeForm.shortfallAction === 'REORDER'
              ? t('orders.shortfallReorderHint') : t('orders.shortfallDroppedHint') }}
          </div>
        </el-form-item>
        <el-form-item :label="t('orders.closeNote')" required>
          <el-input v-model="closeForm.note" type="textarea" :rows="3"
            :placeholder="t('orders.closeNotePlaceholder')" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="closeShortOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitCloseShort">
          {{ t('orders.closeOrder') }}
        </el-button>
      </template>
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
import { download, get, post, put, quietErrors, saveBlob } from '../api'
import { onLive } from '../live'
import { isDialogDismissed } from '../lib/dialogActions'
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
  closedAt: string
  closedBy: string
  confirmStatus: string
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
interface ApprovalTodo {
  task: { id: string; status: string }
  instance: { bizType: string; bizId: string }
}
interface ApprovalInstance { id: string; status: string }
interface ApprovalTask { id: string; status: string }
interface ApprovalRound { instance: ApprovalInstance; tasks: ApprovalTask[] }
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
  reservedQty: string
  availableQty: string
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
interface PurchaseInspection { id: string; receiptNo: string; result: string; inspectedQty: string; defectQty: string; note: string; attachments: ProductionAttachment[]; status: string; disposition: string; dispositionNote: string }
interface Execution { confirmations: SupplierConfirmation[]; milestones: ProductionMilestone[]; exceptions: ReceiptException[]; reminders: ProductionReminder[]; inspections: PurchaseInspection[] }

const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const canWrite = auth.can('procurement:order:write')
const canSubmit = auth.can('procurement:order:submit')
const canApprove = auth.can('approval:task:act')
const canCancel = auth.can('procurement:order:cancel')
const canReceive = auth.can('procurement:receipt:write')
const canProduction = auth.can('procurement:production:write')
const canException = auth.can('procurement:exception:write')
const canClose = auth.can('procurement:order:close')
const canManageSupplier = auth.can('masterdata:supplier:write')
const canReadPorts = auth.can('masterdata:port:read')
const canReadWarehouses = auth.can('inventory:stock:read')

const rows = ref<Order[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
// 采购单页面同时承接制单、审批状态查看和审批后的履约跟踪。
// 真正的批准/驳回仍由个人审批任务执行，避免只凭采购单读取权限越权审批。
const status = ref('ORDERED')
const keyword = ref('')
const loading = ref(false)
const saving = ref(false)
const downloadingId = ref(0)
const approvalTasks = ref<Record<string, string>>({})
const isSuperAdmin = ref(false)
let superAdminResolved = false

const createOpen = ref(false)
const approvalEntry = ref(false)
// 明确要求换一家供应商——不是随手改下拉框，是一次要留痕的偏离。
const switchSupplier = ref(false)
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
const execution = reactive<Execution>({ confirmations: [], milestones: [], exceptions: [], reminders: [], inspections: [] })
const confirmationQty = reactive<Record<string, string>>({})
const confirmationPrice = reactive<Record<string, string>>({})
const confirmationForm = reactive({ confirmedDate: '', expectedDate: '', remark: '' })
const productionNodes = ['PENDING_SCHEDULE', 'SCHEDULED', 'IN_PRODUCTION', 'QUALITY_INSPECTION', 'READY_TO_SHIP', 'SENT_TO_PORT']
const milestoneForm = reactive({ node: 'PENDING_SCHEDULE', plannedDate: '', actualDate: '', ownerName: '', fileName: '', fileUrl: '', remark: '' })
const exceptionTypes = ['WRONG_PRODUCT', 'UNIT_MISMATCH', 'SHORT_SHIPMENT', 'DAMAGE', 'QUALITY_DISPUTE', 'RETURN']
const exceptionForm = reactive<{ type: string; receiptId?: number; itemId?: number; qty: string; actualProduct: string; actualUom: string; description: string }>({ type: 'SHORT_SHIPMENT', receiptId: undefined, itemId: undefined, qty: '', actualProduct: '', actualUom: '', description: '' })
const inspectionForm = reactive({ receiptId: 0, itemId: 0, result: 'PASS', inspectedQty: '0', defectQty: '0', fileName: '', fileUrl: '', note: '' })
const dispositionKinds = ['RETURN', 'DEDUCTION', 'CONCESSION', 'REWORK']
const closeShortOpen = ref(false)
const closingOrder = ref<Order | null>(null)
const closeForm = reactive({ shortfallAction: 'REORDER', note: '' })
const dispositionOpen = ref(false)
const dispositionTarget = ref<PurchaseInspection | null>(null)
const dispositionForm = reactive({ disposition: 'DEDUCTION', note: '' })

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

// 报价转来的待采购行（A5）。
//
// 这条路上供应商和数量在询价比价阶段就定死了，所以默认锁住下拉框——这是
// 常态，不该每次都让人重新挑一遍。但工厂这批只供得了一部分是常事，剩下的
// 要么另找一家、要么过些天再向同一家追加，两条路都得走得通。做法是把
// 「偏离已确认方案」变成一个明确动作：点一下解锁，写明原因才放行。
const quotationLines = computed(() =>
  pending.value.filter((r) => r.source === 'CUSTOMER_QUOTATION' && Number(qtyOf[r.id] ?? 0) > 0),
)
const scenarioSupplierName = computed(() => quotationLines.value[0]?.supplierName || '')
// 这批之前已经下过单，本次买的是剩下的数量。
const isReorder = computed(() => quotationLines.value.some((r) => Number(r.orderedQty ?? 0) > 0))
const supplierLocked = computed(() => approvalEntry.value && !switchSupplier.value)
const reasonRequired = computed(() =>
  quotationLines.value.length > 0 && (switchSupplier.value || isReorder.value),
)
const reasonPlaceholder = computed(() => {
  if (switchSupplier.value && scenarioSupplierName.value)
    return `为什么不向「${scenarioSupplierName.value}」采购？如：本批只能供 80 吨，余量转其他工厂`
  if (isReorder.value) return '这批之前已经下过单，请说明为什么再开一张。如：首批只排到 80 吨，余量本月底补齐'
  return '如修改了已确认报价的供应商单价或币种，请说明原因'
})

function toggleSupplierSwitch() {
  switchSupplier.value = !switchSupplier.value
  if (!switchSupplier.value) {
    // 改回原厂：把成本方案里的供应商和单价一并还原，免得留下半改不改的单子。
    const line = quotationLines.value[0]
    if (line) form.supplierId = Number(line.supplierId)
    quotationLines.value.forEach((r) => { priceOf[r.id] = r.sourceUnitPrice || '0' })
  }
}

interface RowAction { key: string; label: string; tone: 'primary' | 'success' | 'warning' | 'danger'; run: () => void }
function primaryAction(row: Order): RowAction | null {
  // 已结案与已作废采购单是只读历史，不再出现任何履约动作。
  if (row.closedAt || row.status === 'CANCELLED') return null
  if (canActOnOrderApproval(row))
    return { key: 'approve', label: t('orders.reviewApproval'), tone: 'success', run: () => void openApprovalReview(row) }
  if (row.status === 'DRAFT' && canSubmit)
    return { key: 'submit', label: t('orders.submit'), tone: 'primary', run: () => void submit(row) }
  if (row.status === 'REJECTED' && canWrite)
    return { key: 'edit', label: t('orders.editAndResubmit'), tone: 'warning', run: () => void openEdit(row) }
  if (['ORDERED', 'PARTIALLY_RECEIVED'].includes(row.status) && canReceive)
    return { key: 'receive', label: t('orders.receive'), tone: 'warning', run: () => openReceive(row) }
  // 收满、还没结案——当前该做的就是宣布这单到此为止（A3）。
  if (row.status === 'RECEIVED' && !row.closedAt && canClose)
    return { key: 'close', label: t('orders.closeOrder'), tone: 'primary', run: () => void closeOrder(row) }
  if (['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status))
    return { key: 'execution', label: t('orders.execution'), tone: 'warning', run: () => openExecution(row) }
  return null
}

function approvalTaskFor(row: Order): string {
  return approvalTasks.value[String(row.id)] ?? ''
}

async function resolveSuperAdmin() {
  if (superAdminResolved) return
  superAdminResolved = true
  if (!canApprove || !auth.can('iam:role:read')) return
  try {
    const roleData = await get<{ roles: { id: string; code: string; status: string }[] }>('/roles', undefined, quietErrors)
    const role = (roleData.roles ?? []).find((item) => item.code === 'SUPER_ADMIN' && item.status !== 'INACTIVE')
    if (!role) return
    const memberData = await get<{ members: { employeeId: string }[] }>(`/roles/${role.id}/members`, undefined, quietErrors)
    isSuperAdmin.value = (memberData.members ?? []).some((member) => String(member.employeeId) === String(auth.employeeId))
  } catch {
    isSuperAdmin.value = false
  }
}

async function loadPendingApprovalTask(row: Order): Promise<string> {
  try {
    const listed = await get<{ instances: ApprovalInstance[] }>('/approvals/instances', {
      biz_type: 'PURCHASE_ORDER', biz_id: row.id,
    }, quietErrors)
    const running = (listed.instances ?? []).find((instance) => instance.status === 'RUNNING')
    if (!running) return ''
    const round = await get<ApprovalRound>(`/approvals/instances/${running.id}`, undefined, quietErrors)
    return String((round.tasks ?? []).find((task) => task.status === 'PENDING')?.id ?? '')
  } catch {
    return ''
  }
}

function canActOnOrderApproval(row: Order): boolean {
  return row.status === 'PENDING_APPROVAL' && canApprove && Boolean(approvalTaskFor(row))
}

async function openApprovalReview(row: Order) {
  await openDetail(row)
}

async function actOnOrderApproval(row: Order, action: 'APPROVE' | 'REJECT') {
  const taskID = approvalTaskFor(row)
  if (!taskID) return
  let comment = ''
  try {
    if (action === 'APPROVE') {
      await ElMessageBox.confirm(t('orders.approveConfirm', { no: row.poNo }), t('todos.approve'), {
        type: 'warning', confirmButtonText: t('todos.approve'), cancelButtonText: common('cancel'),
      })
    } else {
      const result = await ElMessageBox.prompt(t('orders.rejectReasonHint'), t('todos.reject'), {
        inputType: 'textarea', inputValidator: (value) => Boolean(String(value).trim()) || t('todos.commentRequired'),
        confirmButtonText: t('todos.reject'), cancelButtonText: common('cancel'),
      })
      comment = result.value.trim()
    }
  } catch (err) {
    if (isDialogDismissed(err)) return
    throw err
  }
  await post(`/approvals/tasks/${taskID}/act`, { action, comment })
  ElMessage.success(t('todos.acted'))
  detailOpen.value = false
  const nextStatus = action === 'APPROVE' ? 'ORDERED' : 'REJECTED'
  status.value = nextStatus
  await router.replace({ path: '/purchase-orders', query: { status: nextStatus, order: row.id } })
  await reload()
}
function moreActions(row: Order): { key: string; label: string }[] {
  const primary = primaryAction(row)?.key
  const out: { key: string; label: string }[] = []
  const add = (key: string, label: string, allowed: boolean) => { if (allowed && key !== primary) out.push({ key, label }) }
  add('edit', t('orders.edit'), canWrite && ['DRAFT', 'REJECTED'].includes(row.status))
  add('submit', t('orders.submit'), canSubmit && ['DRAFT', 'REJECTED'].includes(row.status))
  add('receive', t('orders.receive'), canReceive && ['ORDERED', 'PARTIALLY_RECEIVED'].includes(row.status))
  add('execution', t('orders.execution'), !row.closedAt && ['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status))
  add('downloadXlsx', t('orders.downloadExcel'), ['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status))
  add('downloadPdf', t('orders.downloadPdf'), ['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status))
  add('close', t('orders.closeOrder'), canClose && ['RECEIVED', 'PARTIALLY_RECEIVED'].includes(row.status) && !row.closedAt)
  add('cancel', common('cancel'), canCancel && ['DRAFT', 'REJECTED', 'ORDERED'].includes(row.status))
  return out
}

function allActions(row: Order): { key: string; label: string; divided?: boolean }[] {
  const primary = primaryAction(row)
  return [
    { key: 'detail', label: t('common.detail') },
    ...(primary ? [{ key: primary.key, label: primary.label, divided: true }] : []),
    ...moreActions(row).map((action, index) => ({ ...action, divided: !primary && index === 0 })),
  ]
}

function rowMenuActions(row: Order): { key: string; label: string; divided?: boolean }[] {
  return allActions(row).filter((action) => !(canActOnOrderApproval(row) && action.key === 'approve'))
}
// 工厂回签状态的列表子标签（B5 尾巴）。只有实际录入过回签时才显示，
// 不再用邮件发送状态制造第二个“是否下单”的业务门槛。
function confirmTag(row: Order): { label: string; type: 'success' | 'warning' | 'danger' | 'info' } | null {
  if (!['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status)) return null
  const s = row.confirmStatus
  if (!s) return null
  if (s === 'MATCHED' || s === 'APPROVED') return { label: t('orders.confirmTags.CONFIRMED'), type: 'success' }
  if (s === 'PENDING_APPROVAL') return { label: t('orders.confirmTags.PENDING_APPROVAL'), type: 'warning' }
  if (s === 'REJECTED') return { label: t('orders.confirmTags.REJECTED'), type: 'danger' }
  return { label: t('orders.confirmTags.NONE'), type: 'info' }
}

// 详情页的「下一步」与列表主动作同一套推导——两处永远说同一句话。
const detailNext = computed(() => (detail.value && !approvalTaskFor(detail.value) ? primaryAction(detail.value) : null))
function runDetailNext() {
  const next = detailNext.value
  if (!next) return
  detailOpen.value = false
  next.run()
}

function runOrderAction(row: Order, key: string) {
  if (key === 'detail') {
    void openDetail(row)
    return
  }
  const primary = primaryAction(row)
  if (primary?.key === key) {
    primary.run()
    return
  }
  switch (key) {
    case 'edit': openEdit(row); break
    case 'submit': void submit(row); break
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
    const d = await get<{ orders: Order[]; meta: { total: number } }>('/purchase-orders', {
      page: page.value, page_size: pageSize,
      status: status.value,
      keyword: keyword.value,
    })
    rows.value = d.orders ?? []
    total.value = Number(d.meta?.total ?? 0)
    approvalTasks.value = {}
    if (canApprove && status.value === 'PENDING_APPROVAL') {
      const todoData = await get<{ todos: ApprovalTodo[] }>('/approvals/todos', {
        page: 1, page_size: 200, status: '',
      }, quietErrors)
      approvalTasks.value = Object.fromEntries((todoData.todos ?? [])
        .filter((todo) => todo.instance.bizType === 'PURCHASE_ORDER' && todo.task.status === 'PENDING')
        .map((todo) => [String(todo.instance.bizId), String(todo.task.id)]))

	  // 最高权限管理员可以处理历史遗留或分配给他人的采购审批。
	  // 本人待办接口只返回自己的任务，因此管理员还需从审批实例中解析
	  // 当前待处理任务；后端会再次按 SUPER_ADMIN 角色做强制校验。
	  await resolveSuperAdmin()
	  if (isSuperAdmin.value) {
	    const missing = rows.value.filter((row) => !approvalTaskFor(row))
	    const resolved = await Promise.all(missing.map(async (row) => [row.id, await loadPendingApprovalTask(row)] as const))
	    for (const [orderID, taskID] of resolved) {
	      if (taskID) approvalTasks.value[String(orderID)] = taskID
	    }
	  }
    }
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  return load()
}

function openOf(r: Requirement): string {
  // 编辑已有草稿时要把该草稿自己的数量留给员工修改；新建采购单才扣除所有草稿预占。
  if (editing.value) return String(Number(r.requiredQty ?? 0) - Number(r.orderedQty ?? 0))
  return r.availableQty ?? String(Number(r.requiredQty ?? 0) - Number(r.orderedQty ?? 0))
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
  switchSupplier.value = false
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
    .filter((r) => Number(r.availableQty ?? 0) > 0)
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
      qtyOf[r.id] = openOf(r)
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
  switchSupplier.value = false
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
  // 换供应商或补购都是对已确认方案的偏离，原因必填。后端也会拦，这里先说
  // 一声，省一趟白跑。
  if (reasonRequired.value && !form.sourceChangeReason.trim()) {
    ElMessage.warning(switchSupplier.value ? '换供应商请填写调整原因' : '补购剩余数量请填写调整原因')
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
        // 同一报价与供应商已有草稿时，再次从待采购进入应继续原单，
        // 不能既隐藏草稿又阻止采购员完成制单。
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
      }
    }
    ElMessage.success(editing.value
      ? t('orders.updated', { no: res.poNo })
      : t('orders.created', { no: res.poNo }))
    createOpen.value = false
    approvalEntry.value = false
    editing.value = null
    if (wasApprovalEntry) {
      // 待采购只负责生成草稿；采购员核对原单后再显式提交审批。
      status.value = 'DRAFT'
      await router.push({ path: '/purchase-orders', query: { status: 'DRAFT', order: res.id } })
      await reload()
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
  // 货没到齐的单要人先决定「剩下的怎么办」，所以走一个有选项的弹框；
  // 收齐的单还是一句确认就够。
  if (Number(row.receivedQty) < Number(row.totalQty)) {
    closingOrder.value = row
    closeForm.shortfallAction = 'REORDER'
    closeForm.note = ''
    closeShortOpen.value = true
    return
  }
  await ElMessageBox.confirm(t('orders.closeConfirm', { no: row.poNo }), t('orders.closeOrder'), {
    type: 'warning',
    confirmButtonText: t('orders.closeOrder'),
    cancelButtonText: common('cancel'),
  })
  await post(`/purchase-orders/${row.id}/close`, {})
  ElMessage.success(t('orders.closedOk'))
  load()
}

async function submitCloseShort() {
  if (!closingOrder.value) return
  if (!closeForm.note.trim()) {
    ElMessage.warning(t('orders.closeNoteRequired'))
    return
  }
  saving.value = true
  try {
    await post(`/purchase-orders/${closingOrder.value.id}/close`, {
      shortfall_action: closeForm.shortfallAction,
      close_note: closeForm.note,
    })
    ElMessage.success(closeForm.shortfallAction === 'REORDER'
      ? t('orders.closedReorder') : t('orders.closedOk'))
    closeShortOpen.value = false
    load()
  } finally {
    saving.value = false
  }
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
  justify-content: center;
  white-space: nowrap;
}
.row-actions :deep(.el-button) {
  margin-left: 0;
}
.action-trigger {
  width: 30px;
  color: var(--el-text-color-secondary);
  letter-spacing: 1px;
}
.action-trigger:hover {
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
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
.form-note {
  margin-top: 2px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--el-text-color-secondary);
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
@media (max-width: 760px) {
  .execution-section-head { align-items: flex-start; flex-direction: column; }
  .overall-progress { width: 100%; }
  .execution-form-grid { grid-template-columns: 1fr; }
  .span-all { grid-column: auto; }
}
</style>
