<template>
  <div class="purchase-orders-page">
    <WorkflowPageHeader :title="t('orders.title')" :description="t('orders.subtitle')"><template #actions><el-button v-if="canWrite" type="primary" @click="historyId='';historyOpen=true">补录历史采购单</el-button></template></WorkflowPageHeader>

    <HistoricalPurchaseOrderDialog v-model="historyOpen" :order-id="historyId" @saved="(s:string)=>{status=s;reload()}" />
    <HistoricalShippingContextDialog v-model="tradeContextOpen" :order="tradeContextOrder" @saved="reload" />
    <OrderBuyerDialog v-model="buyerOpen" :order="buyerOrder" @saved="reload" />
    <el-card shadow="never">
      <el-radio-group v-model="status" class="tabs" @change="reload">
        <el-radio-button value="DRAFT">{{ t('orders.statuses.DRAFT') }}</el-radio-button>
        <el-radio-button value="PENDING_APPROVAL">{{ t('orders.statuses.PENDING_APPROVAL') }}</el-radio-button>
        <el-radio-button value="PENDING_CONTRACT">{{ t('orders.statuses.PENDING_CONTRACT') }}</el-radio-button>
        <el-radio-button value="ORDERED">{{ t('orders.statuses.ORDERED') }}</el-radio-button>
        <el-radio-button value="PARTIALLY_RECEIVED">{{ t('purchaseFulfillment.partialFilter') }}</el-radio-button>
        <el-radio-button value="RECEIVED_OPEN">{{ t('purchaseFulfillment.completeFilter') }}</el-radio-button>
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
        <el-button v-if="orderColumns.customized.value" link @click="orderColumns.reset">{{ t('common.restoreColumnOrder') }}</el-button>
      </div>

      <el-table :data="rows" v-loading="loading" class="orders-table">
        <el-table-column v-for="column in orderColumns.columns.value" :key="column.key" :min-width="column.minWidth" :width="column.width" :align="column.align" :show-overflow-tooltip="column.showOverflowTooltip">
          <template #header><ReorderableTableHeader :label="column.label" :hint="t('common.dragColumnHint')" :move-left-label="t('common.moveColumnLeft')" :move-right-label="t('common.moveColumnRight')" :can-move-left="orderColumns.canMoveLeft(column.key)" :can-move-right="orderColumns.canMoveRight(column.key)" @move-left="orderColumns.moveBy(column.key,-1)" @move-right="orderColumns.moveBy(column.key,1)" /></template>
          <template #default="{ row }">
           <template v-if="column.key==='poNo'">
            <div class="prod">{{ row.poNo }} <el-tag v-if="row.historical" size="small" type="info">历史补录</el-tag></div><div v-if="row.historical" class="sub">原下单：{{row.originalDate}}</div>
            <div class="sub">{{ formatTime(row.createdAt) }}</div>
           </template>
           <template v-else-if="column.key==='supplier'">
            <div>{{ row.supplierName || (row.historical ? '待补充供应商' : '—') }}</div>
            <div class="sub">{{ t('orders.buyer') }} {{ row.buyerName || '—' }}</div>
           </template>
           <template v-else-if="column.key==='batch'">
            <div>{{ row.sourceQuotationNo || '—' }}</div>
            <div class="sub">{{ t('orders.lines', { n: row.itemCount }) }}</div>
           </template>
           <template v-else-if="column.key==='amount'">
            <span class="num money">{{ row.currency }} {{ row.historical && !row.pricesComplete ? '价格未补齐' : row.totalAmount }}</span>
           </template>
           <template v-else-if="column.key==='expectedDate'">{{ row.expectedDate || '—' }}</template>
           <template v-else-if="column.key==='status'">
            <el-tag v-if="showOrderStage(row)" size="small" :type="statusType(row.status)" effect="plain">
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
          </template>
        </el-table-column>
        <!-- 所有行操作统一收在更多操作中。 -->
        <el-table-column :label="t('common.actions')" width="115" align="center" fixed="right">
          <template #default="{ row }">
            <div class="row-actions">
              <el-dropdown
                v-if="rowMenuActions(row).length"
                trigger="click"
                @visible-change="loadQualityMenu(row,$event)"
                @command="(key: string) => runOrderAction(row, key)"
              >
                <el-button class="more-actions-trigger" size="small" type="primary" plain @click.stop>
                  {{ t('orders.moreActions') }}<span class="drop-arrow" aria-hidden="true">▼</span>
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

    <el-dialog
      v-model="createOpen"
      :title="editing ? t('orders.editFor', { no: editing.poNo }) : t('orders.create')"
      class="purchase-draft-dialog"
      :width="negotiatedDraftItems ? 'min(940px, calc(100vw - 32px))' : 'min(900px, calc(100vw - 32px))'"
      top="5vh"
    >
      <el-alert v-if="editing?.status === 'REJECTED' && editing.rejectReason" :title="t('orders.returnedReason', { reason: editing.rejectReason })" type="error" :closable="false" show-icon class="alert" />
      <el-alert v-if="!editing" type="info" :closable="false" show-icon class="alert">
        {{ t('orders.createHint') }}
      </el-alert>
      <el-form class="purchase-draft-form" label-position="top" size="small">
        <el-form-item class="draft-supplier" :label="t('orders.supplier')" required>
          <el-input v-if="negotiatedDraftItems || (editing && !canReadSuppliers)" :model-value="existingSupplierLabel" readonly :aria-label="t('orders.supplier')" />
          <div v-else class="supplier-row">
            <SupplierSelect
              :key="editing?.id || 'new-purchase-order'"
              :model-value="form.supplierId"
              :pinned-option="pinnedSupplier"
              :disabled="supplierLocked || !canReadSuppliers"
              :aria-label="t('orders.supplier')"
              @update:model-value="form.supplierId = String($event || '')"
              @selected="selectedSupplierName = $event?.name || ''"
            />
            <el-button v-if="approvalEntry && scenarioSupplierName" link type="primary" @click="toggleSupplierSwitch">
              {{ switchSupplier ? `改回${scenarioSupplierName}` : '改向其他供应商' }}
            </el-button>
          </div>
          <div v-if="draftSupplierActive === false && editing?.supplierId === form.supplierId" class="form-note">
            {{ t('orders.originalSupplierInactive') }}
          </div>
          <div v-if="scenarioSupplierName" class="form-note">
            比价确认的方案定的是「{{ scenarioSupplierName }}」<template v-if="switchSupplier">，换一家要在下面写明原因</template>
          </div>
        </el-form-item>
        <el-form-item class="draft-date" :label="approvalEntry ? t('orders.requiredArrivalDate') : t('orders.expected')" :required="approvalEntry">
          <el-date-picker v-model="form.expectedDate" type="date" value-format="YYYY-MM-DD" />
        </el-form-item>
        <el-form-item class="draft-date" :label="t('orders.payableDue')">
          <el-date-picker v-model="form.payableDueDate" type="date" value-format="YYYY-MM-DD" clearable
                          placeholder="YYYY-MM-DD" :aria-label="t('orders.payableDue')" />
        </el-form-item>
        <el-form-item class="draft-note draft-note-full" :label="t('orders.paymentTerms')">
          <div class="payment-terms-picker" @keydown.shift.enter.capture.stop>
            <el-autocomplete v-model="form.paymentTerms" type="textarea" :autosize="{ minRows: 1, maxRows: 4 }"
                             :fetch-suggestions="purchasePaymentSuggestions" :debounce="0" fit-input-width
                             :placeholder="t('orders.paymentTermsHint')" :aria-label="t('orders.paymentTerms')" />
          </div>
        </el-form-item>
        <el-form-item class="draft-fulfillment" label="履约方式" required>
          <el-select v-if="negotiatedDraftItems" v-model="form.fulfillmentMode" aria-label="履约方式">
            <el-option value="DIRECT_SHIP" label="直接发往港口/指定地点" />
            <el-option value="WAREHOUSE" label="先入库再发货" />
          </el-select>
          <el-radio-group v-else v-model="form.fulfillmentMode">
            <el-radio-button value="DIRECT_SHIP">直接发往港口/指定地点</el-radio-button>
            <el-radio-button value="WAREHOUSE">先入库再发货</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <template v-if="form.fulfillmentMode === 'DIRECT_SHIP'">
          <el-form-item class="draft-location" label="收货地点" required>
            <el-select v-if="negotiatedDraftItems" v-model="form.deliveryLocationType" aria-label="收货地点">
              <el-option value="PORT" label="港口" />
              <el-option value="CUSTOM" label="自定义地址" />
            </el-select>
            <el-radio-group v-else v-model="form.deliveryLocationType">
              <el-radio value="PORT">港口</el-radio>
              <el-radio value="CUSTOM">自定义地址</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="form.deliveryLocationType === 'PORT'" class="draft-destination" label="收货港口" required>
            <el-select
              v-if="negotiatedDraftItems" :model-value="form.deliveryPortId || form.deliveryPortName || undefined"
              aria-label="收货港口" filterable allow-create default-first-option clearable
              placeholder="选择港口，或输入后按回车" @change="setNegotiatedDeliveryPort"
            >
              <el-option v-for="port in deliveryPorts" :key="port.id" :value="Number(port.id)" :label="portLabel(port)" />
              <el-option
                v-if="form.deliveryPortId && !deliveryPorts.some(port => Number(port.id) === form.deliveryPortId)"
                :value="form.deliveryPortId" :label="form.deliveryPortName || form.deliveryPortCode || String(form.deliveryPortId)"
              />
            </el-select>
            <el-select v-else v-model="form.deliveryPortId" filterable @change="selectDeliveryPort">
              <el-option
                v-for="p in deliveryPorts"
                :key="p.id"
                :value="Number(p.id)"
                :label="portLabel(p)"
              />
            </el-select>
          </el-form-item>
          <el-form-item v-else class="draft-destination" label="收货地址" required><el-input v-model="form.deliveryAddress" /></el-form-item>
        </template>
        <el-form-item v-else class="draft-destination draft-warehouse" label="入库仓库" required>
          <el-select v-model="form.warehouseId" @change="selectOrderWarehouse">
            <el-option v-for="w in warehouses" :key="w.id" :value="Number(w.id)" :label="w.name" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="showAdjustmentReason" class="draft-note" label="调整原因" :required="reasonRequired">
          <el-input v-model="form.sourceChangeReason" :placeholder="reasonPlaceholder" />
          <div v-if="isReorder && !switchSupplier" class="form-note">
            这批之前已经下过单，本次买的是剩下的数量
          </div>
        </el-form-item>
        <el-form-item class="draft-note" :class="{ 'draft-note-full': !showAdjustmentReason }" :label="t('orders.remark')">
          <el-input v-model="form.remark" type="textarea" :autosize="{ minRows: 1, maxRows: 4 }" />
        </el-form-item>
      </el-form>

      <div class="side-title" :class="{ 'negotiated-draft-heading': negotiatedDraftItems }">{{ negotiatedDraftItems ? '采购明细' : t('orders.pickRequirements') }}</div>
      <el-table v-if="negotiatedDraftItems" :data="negotiatedDraftItems" row-key="id" size="small">
        <el-table-column label="产品规格" min-width="220">
          <template #default="{ row }">
            <ComparisonSpecification :product-name="row.productName" :text="orderProductDescription(row)" hide-name />
            <div v-if="row.negotiationRemark" class="sub specification-remark">规格备注：{{ row.negotiationRemark }}</div>
          </template>
        </el-table-column>
        <el-table-column label="数量 / 单位" width="155">
          <template #default="{ row }">
            <el-input
              :model-value="draftCellValue('qty', row.requirementId)" size="small" inputmode="decimal" :aria-label="`数量 ${row.productName}`"
              @blur="editedDraftCell = ''" @update:model-value="updateDraftCell('qty', row.requirementId, $event)"
            >
              <template #append>{{ row.uomCode }}</template>
            </el-input>
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.unitPrice')" width="130">
          <template #default="{ row }">
            <el-input
              :model-value="draftCellValue('price', row.requirementId)" size="small" inputmode="decimal" :aria-label="`单价 ${row.productName}`"
              @blur="editedDraftCell = ''" @update:model-value="updateDraftCell('price', row.requirementId, $event)"
            />
          </template>
        </el-table-column>
      </el-table>
      <el-table v-else :data="pending" size="small" max-height="320">
        <el-table-column :label="t('orders.product')" min-width="180">
          <template #default="{ row }">
            <ComparisonSpecification :product-name="row.productName" :text="orderProductDescription(row)" hide-name/>
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
            <span class="num">{{ openOf(row) === '' ? '—' : trim(openOf(row)) }}</span> {{ row.uomCode }}
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
      <div v-if="!negotiatedDraftItems" class="total-row">
        {{ t('orders.estimated') }}<span class="num money">{{ estimated }}</span>
      </div>

      <template #footer>
        <div :class="{ 'negotiated-draft-footer': negotiatedDraftItems }">
          <div v-if="negotiatedDraftItems" class="total-row">
            {{ t('orders.estimated') }}<strong class="num money">{{ draftTotalFormat.format(Number(estimated)) }}</strong> {{ form.currency }}
          </div>
          <div>
            <el-button @click="createOpen = false">{{ common('cancel') }}</el-button>
            <el-button type="primary" :loading="saving" @click="submitCreate">
              {{ approvalEntry ? t('orders.createDraft') : common('save') }}
            </el-button>
          </div>
        </div>
      </template>
    </el-dialog>

    <el-dialog
      v-model="detailOpen" :title="detail?.poNo"
      :width="detail?.historical ? 'min(1040px,94vw)' : compactDraftDetail ? 'min(880px, calc(100vw - 32px))' : '820px'"
      :top="detail?.historical || compactDraftDetail ? '5vh' : '15vh'"
      :class="{ 'historical-order-detail': detail?.historical, 'compact-draft-detail': compactDraftDetail }"
    >
      <!-- B5 尾巴：看完单子不用回列表找按钮——下一步就在眼前。 -->
      <div v-if="detail && approvalTaskFor(detail)" class="next-step">
        <span class="next-step-label">{{ t('orders.approvalDecision') }}</span>
        <el-button size="small" type="success" @click="actOnOrderApproval(detail, 'APPROVE')">{{ t('todos.approve') }}</el-button>
        <el-button size="small" type="danger" @click="actOnOrderApproval(detail, 'REJECT')">{{ t('todos.reject') }}</el-button>
      </div>
      <div v-else-if="detailNext && !compactDraftDetail" class="next-step">
        <span class="next-step-label">{{ t('orders.nextStep') }}</span>
        <el-button size="small" :type="detailNext.tone" @click="runDetailNext">{{ detailNext.label }}</el-button>
      </div>
      <section v-if="compactDraftDetail && detail" class="draft-detail-summary">
        <header class="draft-detail-heading">
          <el-tag size="small" :type="detail.status === 'REJECTED' ? 'danger' : 'info'">{{ orderStatusLabel(detail) }}</el-tag>
          <el-button v-if="detailNext" size="small" :type="detailNext.tone" @click="runDetailNext">{{ detailNext.label }}</el-button>
        </header>
        <dl class="draft-detail-facts">
          <div><dt>{{ t('orders.supplier') }}</dt><dd>{{ detail.supplierName || '—' }}</dd></div>
          <div><dt>{{ t('orders.buyer') }}</dt><dd>{{ detail.buyerName || '—' }}</dd></div>
          <div><dt>{{ t('orders.expected') }}</dt><dd>{{ detail.expectedDate || '—' }}</dd></div>
          <div><dt>{{ t('orders.payableDue') }}</dt><dd>{{ detail.payableDueDate || '—' }}</dd></div>
          <div><dt>{{ t('orders.amount') }}</dt><dd class="num">{{ draftTotalFormat.format(Number(detail.totalAmount)) }} {{ detail.currency }}</dd></div>
          <div class="draft-payment-terms"><dt>{{ t('orders.paymentTerms') }}</dt><dd>{{ detail.paymentTerms || '—' }}</dd></div>
        </dl>
      </section>
      <el-descriptions v-else :column="detail?.historical ? 2 : 3" :label-width="detail?.historical ? 110 : undefined" border size="small" class="desc">
        <el-descriptions-item :label="t('orders.supplier')">{{ detail?.supplierName || (detail?.historical ? '待补充供应商' : '—') }}</el-descriptions-item>
        <el-descriptions-item :label="t('orders.amount')">
          {{ detail?.currency }} {{ detail?.totalAmount }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('common.status')">
          {{ detail ? orderStatusLabel(detail) : '' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('orders.buyer')">{{ detail?.buyerName || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('orders.expected')">{{ detail?.expectedDate || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('orders.payableDue')">{{ detail?.payableDueDate || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('orders.paymentTerms')">{{ detail?.paymentTerms || '—' }}</el-descriptions-item>
        <template v-if="detail?.historical"><el-descriptions-item :label="t('historicalShipping.contract')">{{ detail.exportContractNo || '—' }}</el-descriptions-item><el-descriptions-item :label="t('historicalShipping.port')">{{ detail.destinationPort || '—' }}</el-descriptions-item></template>
        <template v-if="detail?.historical"><el-descriptions-item label="来源">历史补录</el-descriptions-item><el-descriptions-item label="原下单日期">{{detail.originalDate}}</el-descriptions-item><el-descriptions-item label="录入人 / 时间">{{detail.recordedBy}} / {{formatTime(detail.recordedAt||'')}}</el-descriptions-item></template><el-descriptions-item :label="t('orders.remark')">{{ detail?.remark || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('orders.confirmations')">
          <template v-if="detail && confirmTag(detail)">{{ confirmTag(detail)!.label }}</template>
          <template v-else>—</template>
        </el-descriptions-item>
      </el-descriptions>

      <section v-if="detail?.signedContractUploadedAt" class="order-draft-files">
        <div class="side-title">双方签署的采购合同</div>
        <p><a v-if="detail.signedContractUrl" :href="detail.signedContractUrl" target="_blank" rel="noopener noreferrer">{{detail.signedContractName}} · 查看合同附件</a><span v-else>{{detail.signedContractName}}（附件链接暂不可用，请刷新）</span></p>
        <p v-if="detail.remark && detail.remark !== detail.paymentTerms" class="sub">{{ t('orders.remark') }}：{{ detail.remark }}</p>
        <p class="sub">{{detail.contractVerifiedAt?'已完成财务核验':'等待财务在“出账 → 待合同核验”中核验签署合同。'}}</p>
        <p v-if="detail.contractVerifiedAt" class="sub">核验人：{{detail.contractVerifiedByName||'—'}}；核验时间：{{formatTime(detail.contractVerifiedAt)}}</p>
      </section>

      <el-alert v-if="detail?.status === 'REJECTED' && detail.rejectReason" :title="detail.rejectReason" type="error" :closable="false" show-icon />
      <section v-if="compactDraftDetail && detail?.remark" class="draft-detail-remark">
        <h3 class="side-title">{{ t('orders.remark') }}</h3>
        <p>{{ detail.remark }}</p>
      </section>
      <div id="purchase-order-detail-attachments"></div>

      <div v-if="compactDraftDetail" class="side-title draft-detail-table-heading">采购明细</div>
      <el-table :data="detailItems" size="small">
        <el-table-column :label="compactDraftDetail ? '产品规格' : t('orders.product')" min-width="180">
          <template #default="{ row }">
            <ComparisonSpecification :product-name="row.productName" :text="orderProductDescription(row)" hide-name/>
            <div v-if="!compactDraftDetail || row.source === 'MANUAL' || row.contractNo || row.customerName || row.contractOwner" class="sub">
              <template v-if="row.source === 'MANUAL'">{{ t('orders.manual') }}</template>
              <template v-else>{{ row.contractNo }} · {{ row.customerName }}<template v-if="row.contractOwner"> · {{ t('orders.contractOwner') }} {{ row.contractOwner }}</template></template>
            </div>
            <div v-if="row.negotiationRemark" class="sub specification-remark">规格备注：{{ row.negotiationRemark }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('orders.orderQty')" width="100" align="right">
          <template #default="{ row }"><span class="num">{{ compactDraftDetail && row.uomCode ? `${trim(row.qty)} ${row.uomCode}` : trim(row.qty) }}</span></template>
        </el-table-column>
        <el-table-column :label="t('orders.unitPrice')" width="100" align="right">
          <template #default="{ row }"><span class="num">{{ compactDraftDetail && row.unitPrice ? draftUnitPriceFormat.format(Number(row.unitPrice)) : trim(row.unitPrice) }}</span></template>
        </el-table-column>
        <el-table-column :label="t('orders.amount')" width="110" align="right">
          <template #default="{ row }"><span class="num">{{ compactDraftDetail && row.amount ? draftTotalFormat.format(Number(row.amount)) : row.amount }}</span></template>
        </el-table-column>
        <el-table-column v-if="!editableDraftDetail" :label="t(detail?.fulfillmentMode==='WAREHOUSE'?'purchaseFulfillment.completeWarehouse':'purchaseFulfillment.completeDelivery')" width="100" align="right">
          <template #default="{ row }">
            <span class="num" :class="{ dim: Number(row.receivedQty) === 0 }">{{ trim(row.receivedQty) }}</span>
          </template>
        </el-table-column>
      </el-table>

      <Teleport defer to="#purchase-order-detail-attachments" :disabled="compactDraftDetail">
        <section v-if="detailDraftFiles.length || canEditDraftFiles" class="order-draft-files" :class="{ 'draft-detail-files': compactDraftDetail }">
          <div :class="{ 'draft-detail-heading': compactDraftDetail }">
            <div class="side-title">采购资料</div>
            <label v-if="canEditDraftFiles" class="draft-upload">
              <input type="file" multiple :disabled="uploadingDraftFiles" @change="uploadDraftFiles" />
              {{ uploadingDraftFiles ? '上传中…' : '补充采购资料' }}
            </label>
          </div>
          <div v-for="file in detailDraftFiles" :key="file.id" class="order-draft-file">
            <span><strong>{{ file.fileName }}</strong><small>{{ formatDraftFileSize(file.sizeBytes) }} · {{ file.uploadedByName }} · {{ formatTime(file.uploadedAt) }}</small></span>
            <span><el-button link type="primary" :loading="previewingDraftFileId===file.id" @click="previewDraftFile(file)">预览</el-button><el-button link type="primary" @click="downloadDraftFile(file)">下载</el-button><el-button v-if="canEditDraftFiles" link type="danger" @click="deleteDraftFile(file)">删除</el-button></span>
          </div>
        </section>
      </Teleport>
      <FilePreviewDialog v-model="draftFilePreviewOpen" :source="draftFilePreviewSource" :title="draftFilePreviewTitle" :content-type="draftFilePreviewType" />

      <PurchaseQualityPanel v-if="detailOpen && detail && !editableDraftDetail" :order-id="detail.id" :key="detail.id+':'+qualityRevision" :can-request="canRequestQuality(detail)" :request-hint="qualityRequestHint(detail)" @request="openQualityApplication(detail!)" />

      <template v-if="!editableDraftDetail && detailReceipts.length">
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

    <el-dialog v-model="executionOpen" class="fulfillment-dialog" top="6vh" :title="t('purchaseFulfillment.title', { no: executing?.poNo })" width="min(1100px, 94vw)">
      <el-tabs v-model="executionTab">
        <el-tab-pane :label="t('purchaseFulfillment.overview')" name="overview">
          <PurchaseFulfillmentProgress v-if="executionOpen && executing" :order-id="executing.id" :key="executing.id+':'+qualityRevision" :can-request="canRequestQuality(executing)" :request-hint="qualityRequestHint(executing)" @request="openQualityApplication(executing!)" @delivery-saved="refreshAfterDelivery">
          <template #supplier>
          <section class="confirmation-section">
            <div class="execution-section-head">
              <h3>{{ t('purchaseFulfillment.feedback') }}</h3>
              <el-button v-if="canEditConfirmation && latestConfirmation && !editingConfirmation" type="primary" plain @click="startConfirmationEdit">{{ t('purchaseFulfillment.editConfirmation') }}</el-button>
            </div>
            <div class="confirmation-facts">
              <div><span>{{ t('purchaseFulfillment.requiredDate') }}</span><strong>{{ executing?.expectedDate || '—' }}</strong></div>
              <div><span>{{ t('purchaseFulfillment.supplierDate') }}</span><strong :class="{ 'pending-date': !latestConfirmation?.confirmedExpectedDate }">{{ latestConfirmation?.confirmedExpectedDate || t('purchaseFulfillment.datePending') }}</strong></div>
              <div><span>{{ t('common.status') }}</span><strong>{{ latestConfirmation ? confirmationStatusLabel(latestConfirmation.status) : t('orders.confirmTags.NONE') }}</strong></div>
              <div v-if="latestConfirmation"><span>{{ t('orders.confirmedDate') }}</span><strong>{{ latestConfirmation.confirmedDate }}</strong></div>
              <div v-if="latestConfirmation"><span>{{ t('orders.operator') }}</span><strong>{{ latestConfirmation.createdBy || '—' }}</strong></div>
            </div>
            <el-alert v-if="!latestConfirmation" :title="t('purchaseFulfillment.unconfirmedHint')" type="info" :closable="false" class="alert" />
            <el-form v-if="editingConfirmation" label-width="120px" inline>
              <el-form-item :label="t('orders.confirmedDate')"><el-date-picker v-model="confirmationForm.confirmedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
              <el-form-item :label="t('purchaseFulfillment.supplierDate')"><el-date-picker v-model="confirmationForm.expectedDate" type="date" value-format="YYYY-MM-DD" clearable :placeholder="t('purchaseFulfillment.datePending')" /></el-form-item>
            </el-form>
            <el-table :data="executionItems" size="small">
              <el-table-column :label="t('inquiryWorkspace.list.productDetails')" min-width="240"><template #default="{row}"><ComparisonSpecification :product-name="row.productName" :text="orderProductDescription(row)" hide-name/></template></el-table-column>
              <el-table-column :label="t('purchaseFulfillment.quantity')" width="115" align="right"><template #default="{row}">{{ trim(row.qty) }} {{ row.uomCode }}</template></el-table-column>
              <el-table-column :label="t('orders.confirmedQty')" width="145" align="right"><template #default="{row}"><el-input v-if="editingConfirmation" v-model="confirmationQty[row.id]" size="small" /><span v-else>{{ confirmedLine(row) ? trim(confirmedLine(row)!.confirmedQty) : '—' }} {{ confirmedLine(row) ? row.uomCode : '' }}</span></template></el-table-column>
              <el-table-column :label="`采购单价 (${executing?.currency || '—'})`" width="155" align="right"><template #default="{row}">{{ confirmationMoney(row.unitPrice) }}</template></el-table-column>
              <el-table-column :label="`确认单价 (${executing?.currency || '—'})`" width="155" align="right"><template #default="{row}"><el-input v-if="editingConfirmation" v-model="confirmationPrice[row.id]" size="small" /><span v-else>{{ confirmedLine(row) ? confirmationMoney(confirmedLine(row)!.confirmedUnitPrice) : '—' }}</span></template></el-table-column>
            </el-table>
            <template v-if="editingConfirmation">
              <el-input v-model="confirmationForm.remark" type="textarea" :rows="2" :placeholder="t('orders.confirmRemark')" class="execution-note" />
              <div class="confirmation-actions"><el-button type="primary" :loading="saving" @click="submitConfirmation">{{ t('orders.saveConfirmation') }}</el-button><el-button v-if="latestConfirmation" :disabled="saving" @click="editingConfirmation=false">{{ common('cancel') }}</el-button></div>
            </template>
            <p v-else-if="latestConfirmation?.remark" class="follow-up-text">{{ latestConfirmation.remark }}</p>
            <el-collapse class="confirmation-history">
              <el-collapse-item :title="`${t('purchaseFulfillment.confirmationHistory')} (${execution.confirmations.length})`" name="confirmations">
                <el-table :data="execution.confirmations" size="small">
                  <el-table-column prop="confirmedDate" :label="t('orders.confirmedDate')" width="120" />
                  <el-table-column :label="t('common.status')" width="130"><template #default="{row}">{{ confirmationStatusLabel(row.status) }}</template></el-table-column>
                  <el-table-column :label="t('purchaseFulfillment.supplierDate')" width="150"><template #default="{row}">{{ row.confirmedExpectedDate || t('purchaseFulfillment.datePending') }}</template></el-table-column>
                  <el-table-column prop="remark" :label="t('orders.remark')" min-width="220" />
                  <el-table-column prop="createdBy" :label="t('orders.operator')" width="150" />
                </el-table>
              </el-collapse-item>
            </el-collapse>
          </section>
          </template>
          </PurchaseFulfillmentProgress>
          <el-collapse class="legacy-history">
            <el-collapse-item :title="t('purchaseFulfillment.historicalMaterials')" name="legacy">
          <el-alert :title="t('purchaseFulfillment.historyHint')" type="info" :closable="false" />
          <h3>{{t('purchaseFulfillment.productionHistory')}}</h3>
          <el-table :data="execution.milestones" size="small" class="history-table">
            <el-table-column :label="t('orders.productionNode')" width="150"><template #default="{row}">{{ productionNodeLabel(row.node) }}</template></el-table-column>
            <el-table-column prop="plannedDate" :label="t('orders.plannedDate')" width="110" />
            <el-table-column prop="actualDate" :label="t('orders.actualDate')" width="110" />
            <el-table-column prop="ownerName" :label="t('orders.owner')" width="120" />
            <el-table-column prop="remark" :label="t('orders.remark')" min-width="160" />
            <el-table-column :label="t('orders.attachments')" min-width="160"><template #default="{row}"><a v-for="file in row.attachments" :key="file.fileUrl" :href="file.fileUrl" target="_blank">{{ file.fileName }}</a></template></el-table-column>
          </el-table>
          <h3>{{t('purchaseFulfillment.inspectionHistory')}}</h3>
          <el-table :data="execution.inspections" size="small" class="history-table">
            <el-table-column prop="receiptNo" :label="t('orders.receiptNo')" width="150" />
            <el-table-column :label="t('orders.inspectionResult')" width="90"><template #default="{row}"><el-tag size="small" effect="plain" :type="row.result === 'PASS' ? 'success' : 'danger'">{{ t(`orders.inspectionResults.${row.result}`) }}</el-tag></template></el-table-column>
            <el-table-column prop="inspectedQty" :label="t('orders.inspectedQty')" width="90" />
            <el-table-column prop="defectQty" :label="t('orders.defectQty')" width="90" />
            <el-table-column prop="note" :label="t('orders.remark')" min-width="150" />
            <el-table-column :label="t('orders.attachments')" min-width="130"><template #default="{row}"><a v-for="file in row.attachments" :key="file.fileUrl" :href="file.fileUrl" target="_blank">{{ file.fileName }}</a></template></el-table-column>
            <el-table-column :label="t('common.status')" width="90"><template #default="{row}">{{ t(`orders.inspectionStatuses.${row.status}`) }}</template></el-table-column>
            <el-table-column :label="t('orders.disposition')" min-width="140"><template #default="{row}"><template v-if="row.disposition">{{ t(`orders.dispositions.${row.disposition}`) }}<span v-if="row.dispositionNote" class="sub"> {{ row.dispositionNote }}</span></template><span v-else>—</span></template></el-table-column>
          </el-table>
          <h3>{{ t('purchaseFulfillment.exceptionHistory') }}</h3>
          <el-table :data="execution.exceptions" size="small" class="history-table" :empty-text="t('purchaseFulfillment.noExceptionHistory')">
            <el-table-column :label="t('orders.exceptionType')" width="110"><template #default="{row}">{{ t(`orders.exceptionTypes.${row.exceptionType}`) }}</template></el-table-column>
            <el-table-column prop="description" :label="t('orders.exceptionDescription')" min-width="220" />
            <el-table-column prop="qty" :label="t('orders.qty')" width="90" />
            <el-table-column :label="t('common.status')" width="100"><template #default="{row}">{{ t(`orders.exceptionStatuses.${row.status}`) }}</template></el-table-column>
            <el-table-column prop="resolution" :label="t('orders.resolution')" min-width="220" />
            <el-table-column v-if="execution.exceptions.some(row => Number(row.receiptId))" :label="t('orders.receiptNo')" width="170"><template #default="{row}">{{ executionReceipts.find(receipt => Number(receipt.id) === Number(row.receiptId))?.receiptNo || (Number(row.receiptId) ? row.receiptId : '—') }}</template></el-table-column>
            <el-table-column v-if="execution.exceptions.some(row => Number(row.poItemId))" :label="t('orders.product')" min-width="200"><template #default="{row}">{{ executionItems.find(item => Number(item.id) === Number(row.poItemId))?.productName || (Number(row.poItemId) ? row.poItemId : '—') }}</template></el-table-column>
            <el-table-column v-if="execution.exceptions.some(row => row.actualProduct)" prop="actualProduct" :label="t('orders.actualProduct')" min-width="180" />
            <el-table-column v-if="execution.exceptions.some(row => row.actualUom)" prop="actualUom" :label="t('orders.actualUom')" width="100" />
            <el-table-column v-if="execution.exceptions.some(row => row.reportedBy)" prop="reportedBy" :label="t('purchaseFulfillment.reportedBy')" width="150" />
            <el-table-column v-if="execution.exceptions.some(row => row.reportedAt)" :label="t('purchaseFulfillment.reportedAt')" width="185"><template #default="{row}">{{ formatFollowUpTime(row.reportedAt) }}</template></el-table-column>
            <el-table-column v-if="execution.exceptions.some(row => row.resolvedBy)" prop="resolvedBy" :label="t('purchaseFulfillment.resolvedBy')" width="150" />
            <el-table-column v-if="execution.exceptions.some(row => row.resolvedAt)" :label="t('purchaseFulfillment.resolvedAt')" width="185"><template #default="{row}">{{ formatFollowUpTime(row.resolvedAt) }}</template></el-table-column>
          </el-table>
            </el-collapse-item>
          </el-collapse>
        </el-tab-pane>

        <el-tab-pane :label="t('purchaseFulfillment.followUps')" name="followUps">
          <template v-if="canProduction && !executing?.closedAt && executing?.status!=='CANCELLED'">
            <el-input v-model="followUpNote" type="textarea" :rows="3" maxlength="4000" show-word-limit :placeholder="t('purchaseFulfillment.notePlaceholder')" />
            <el-button class="execution-note" type="primary" :loading="saving" @click="saveFollowUp">{{t('purchaseFulfillment.addNote')}}</el-button>
          </template>
          <el-table :data="execution.followUps" size="small" class="history-table" :empty-text="t('purchaseFulfillment.noNotes')">
            <el-table-column :label="t('purchaseFulfillment.time')" width="180"><template #default="{row}">{{formatFollowUpTime(row.createdAt)}}</template></el-table-column>
            <el-table-column prop="createdBy" :label="t('purchaseFulfillment.author')" width="150" />
            <el-table-column prop="note" :label="t('purchaseFulfillment.note')" min-width="350"><template #default="{row}"><span class="follow-up-text">{{row.note}}</span></template></el-table-column>
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

    <el-dialog v-model="contractOpen" title="采购合同" width="560px">
      <el-alert type="info" :closable="false" class="alert">采购方案批准后上传双方签署合同；上传后仍在本页显示“待财务审核”，财务通过后转为“已下单”。</el-alert>
      <el-form label-position="top">
        <el-form-item label="采购合同号" required><el-input v-model="contractForm.contractNo" /></el-form-item>
        <el-form-item :label="t('orders.paymentTerms')" required><el-input v-model="contractForm.paymentTerms" type="textarea" :rows="2" /></el-form-item>
        <el-form-item label="双方签署合同" required><el-upload :auto-upload="false" :limit="1" :on-change="selectContractFile" :file-list="contractFiles"><el-button>选择文件</el-button></el-upload></el-form-item>
      </el-form>
      <div v-if="contractOrder?.signedContractName" class="contract-state">当前文件：<a v-if="contractOrder.signedContractUrl" :href="contractOrder.signedContractUrl" target="_blank">{{contractOrder.signedContractName}}</a><span v-else>{{contractOrder.signedContractName}}</span></div>
      <template #footer><el-button @click="contractOpen=false">取消</el-button><el-button type="primary" :loading="saving" @click="saveContract">保存签署合同</el-button></template>
    </el-dialog>
    <el-dialog v-model="qualityOpen" :title="t('quality.applyTitle')" width="920px">
      <el-alert type="info" :closable="false" show-icon class="alert">{{t('quality.applyHint')}}</el-alert>
      <el-form label-position="top">
        <div class="quality-apply-head">
          <el-form-item :label="t('quality.expectedDate')"><el-date-picker v-model="qualityForm.expectedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
          <el-form-item :label="t('quality.location')"><el-input v-model="qualityForm.location" /></el-form-item>
          <el-form-item :label="t('quality.contactName')"><el-input v-model="qualityForm.contactName" /></el-form-item>
          <el-form-item :label="t('quality.contactPhone')"><el-input v-model="qualityForm.contactPhone" /></el-form-item>
        </div>
        <el-table :data="qualityItems" border>
          <el-table-column prop="productName" :label="t('quality.product')" min-width="240"><template #default="{row}"><ComparisonSpecification :product-name="row.productName" :text="orderProductDescription(row)" hide-name/></template></el-table-column>
          <el-table-column :label="t('quality.orderedQty')" width="160"><template #default="{row}">{{trim(row.qty)}} {{row.uomCode}}</template></el-table-column>
          <el-table-column label="本次送检" width="190"><template #default="{row}"><el-tag type="success" effect="plain">整单 {{trim(row.qty)}} {{row.uomCode}}</el-tag></template></el-table-column>
        </el-table>
        <el-form-item :label="t('quality.remark')" style="margin-top:14px"><el-input v-model="qualityForm.remark" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="qualityOpen=false">{{common('cancel')}}</el-button><el-button type="primary" :loading="saving" @click="submitQualityApplication">{{t('quality.submitApplication')}}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import ComparisonSpecification from '../components/ComparisonSpecification.vue'
import {formatProductDescription} from '../lib/productDescription'

import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { del, download, get, post, put, quietErrors, saveBlob } from '../api'
import { onLive } from '../live'
import { isDialogDismissed } from '../lib/dialogActions'
import { buildConfirmationLines } from '../lib/purchaseExecution'
import PurchaseQualityPanel from '../components/PurchaseQualityPanel.vue'
import {deliveryStatusKey} from '../lib/purchaseDelivery'
import PurchaseFulfillmentProgress from '../components/PurchaseFulfillmentProgress.vue'
import { hasPassedDeliveryInspection } from '../lib/deliveryQuality'
import { getPurchaseOrderApprovalActions } from '../lib/approvalAction'
import { useAuthStore } from '../stores/auth'
import HistoricalPurchaseOrderDialog from '../components/HistoricalPurchaseOrderDialog.vue'
import SupplierSelect from '../components/masterdata/SupplierSelect.vue'
import { paymentTermOptions } from '../lib/paymentTermOptions'
import HistoricalShippingContextDialog from '../components/HistoricalShippingContextDialog.vue'
import OrderBuyerDialog from '../components/OrderBuyerDialog.vue'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
import FilePreviewDialog from '../components/FilePreviewDialog.vue'
import ReorderableTableHeader from '../components/ReorderableTableHeader.vue'
import { useTableColumnOrder, type TableColumnDefinition } from '../composables/useTableColumnOrder'

interface Order {
 destinationPort?: string
 historical?:boolean
 originalDate?:string
 contactName?:string
 contactPhone?:string
 recordedBy?:string
 recordedAt?:string
 pricesComplete?:boolean
  id: string
  poNo: string
  supplierId: string
  supplierCode: string
  supplierName: string
  currency: string
  totalAmount: string
  expectedDate: string
  payableDueDate: string
  status: string
  buyerId?: string
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
  businessType: string
  sourceBusinessId: string
  exportContractNo: string
  businessDocumentNo: string
  paymentTerms: string
  signedContractName: string
  signedContractUrl: string
  signedContractUploadedAt: string
  contractVerifiedAt: string
  contractVerifiedByName: string
  paymentRequestedAt: string
  paymentRequestedByName: string
}
function orderProductDescription(row:{productName:string;spec?:string;contractItemRemark?:string}){
 return formatProductDescription(row.productName,[
  {key:'specification',label:t('inquiryProducts.fields.specification'),value:row.spec},
  {key:'contractItemRemark',label:'合同明细备注',value:row.contractItemRemark},
 ])
}
interface OrderItem {
  id: string
  requirementId: string
  productName: string
	spec: string
  uomCode: string
  qty: string
  unitPrice: string
  amount: string
  receivedQty: string
  contractNo: string
  customerName: string
  source: string
  contractItemRemark?:string
  negotiationRemark?: string
  contractOwner: string
}
interface Receipt {
  id: string
  receiptNo: string
  operatorName: string
  totalQty: string
  receivedAt: string
}
interface OrderDraftFile { id:string; poId:string; fileName:string; contentType:string; sizeBytes:string; uploadedByName:string; uploadedAt:string }
interface Requirement {
  id: string
  contractNo: string
  customerName: string
  productName: string
  spec: string
	status: string
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
  sourcePaymentTerms: string
  sourceIncoterm: string
  sourceValidUntil: string
}
interface ApiFailure { code?: string; message?: string }
interface Warehouse { id: string; code: string; name: string; whType: string }
interface SupplierConfirmationLine { poItemId: string; confirmedQty: string; confirmedUnitPrice: string }
interface SupplierConfirmation { id: string; status: string; confirmedDate: string; confirmedExpectedDate: string; remark: string; createdBy: string; lines: SupplierConfirmationLine[] }
interface ProductionAttachment { fileName: string; fileUrl: string; contentType: string }
interface ProductionMilestone { id: string; node: string; plannedDate: string; actualDate: string; ownerName: string; remark: string; delayed: boolean; attachments: ProductionAttachment[] }
interface ReceiptException { id: string; receiptId: string; poItemId: string; exceptionType: string; qty: string; actualProduct: string; actualUom: string; description: string; status: string; resolution: string; reportedBy: string; reportedAt: string; resolvedBy: string; resolvedAt: string }
interface ProductionReminder { id: string; node: string; plannedDate: string; buyerName: string; relatedContracts: string; status: string }
interface PurchaseInspection { id: string; receiptNo: string; result: string; inspectedQty: string; defectQty: string; note: string; attachments: ProductionAttachment[]; status: string; disposition: string; dispositionNote: string }
interface OrderFollowUp { id:string;note:string;createdBy:string;createdAt:string }
interface Execution { followUps:OrderFollowUp[]; confirmations: SupplierConfirmation[]; milestones: ProductionMilestone[]; exceptions: ReceiptException[]; reminders: ProductionReminder[]; inspections: PurchaseInspection[] }

const { t } = useI18n()
const auth = useAuthStore()
const orderColumnDefaults = computed<TableColumnDefinition[]>(() => [
  { key: 'poNo', label: t('orders.poNo'), minWidth: 155 },
  { key: 'supplier', label: t('orders.supplier'), minWidth: 155 },
  { key: 'batch', label: t('orders.purchaseBatch'), minWidth: 120 },
  { key: 'amount', label: t('orders.amount'), minWidth: 120, align: 'right' },
  { key: 'expectedDate', label: t('orders.requiredArrivalDate'), minWidth: 120, align: 'center' },
  { key: 'status', label: t('common.status'), minWidth: 145, align: 'center' },
])
const orderColumns = useTableColumnOrder('purchase-order-list', orderColumnDefaults)
const route = useRoute()
const router = useRouter()
const historyOpen=ref(false), historyId=ref('')
const tradeContextOpen=ref(false), tradeContextOrder=ref<Order|null>(null)
const buyerOpen=ref(false), buyerOrder=ref<Order|null>(null)
const canWrite = auth.can('procurement:order:write')
const canSubmit = auth.can('procurement:order:submit')
const canCancel = auth.can('procurement:order:cancel')
const canProduction = auth.can('procurement:production:write')
const canClose = auth.can('procurement:order:close')
const canManageSupplier = auth.can('masterdata:supplier:write')
const canReadPorts = auth.can('masterdata:port:read')
const canReadSuppliers = auth.can('masterdata:supplier:read')
const canReadWarehouses = auth.can('inventory:stock:read')
function qualityRequestHint(order:Order){
 if(order.businessType!=='PROCUREMENT')return t('quality.purchaseOnly')
 if(order.closedAt||order.status==='CANCELLED')return t('quality.inactiveOrder')
 if(!['ORDERED','PARTIALLY_RECEIVED','RECEIVED'].includes(order.status))return t('quality.afterOrder')
 return ''
}
function canRequestQuality(order:Order){return auth.can('procurement:order:read')&&!qualityRequestHint(order)}
const qualityMenuTasks=reactive<Record<string,boolean>>({})
async function loadQualityMenu(order:Order,visible:boolean){
 if(!visible||order.businessType!=='PROCUREMENT')return
 delete qualityMenuTasks[order.id]
 try{
  const result=await get<{tasks:QualityDetail[]}>(`/purchase-orders/${order.id}/quality-inspections`,{},quietErrors)
  qualityMenuTasks[order.id]=Boolean(result.tasks?.length)
 }catch{/* Details can still be opened to retry. */}
}

const rows = ref<Order[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 100
// 采购单页面同时承接制单、审批状态查看和审批后的履约跟踪。
// 真正的批准/驳回仍由个人审批任务执行，避免只凭采购单读取权限越权审批。
function listStatusFor(orderStatus: string) {
  return orderStatus === 'REJECTED' ? 'DRAFT' : orderStatus
}
const status = ref(listStatusFor(String(route.query.status || 'ORDERED')))
const keyword = ref('')
const loading = ref(false)
const saving = ref(false)
const downloadingId = ref(0)
const approvalTasks = ref<Record<string, string>>({})
const approvalOverrides = ref<Record<string, boolean>>({})
const withdrawalInstances = ref<Record<string, string>>({})
const withdrawing = ref(false)

const createOpen = ref(false)
const approvalEntry = ref(false)
// 明确要求换一家供应商——不是随手改下拉框，是一次要留痕的偏离。
const switchSupplier = ref(false)
const editing = ref<Order | null>(null)
const draftSupplierActive = ref<boolean>()
const selectedSupplierName = ref('')
const pending = ref<Requirement[]>([])
const negotiatedDraftItems = ref<OrderItem[] | null>(null)
const draftAdjustmentShown = ref(false)
const draftAdjustmentRequired = ref(false)
const editedDraftCell = ref('')
const existingSupplierLabel = computed(() => [editing.value?.supplierCode, editing.value?.supplierName].filter(Boolean).join(' · '))
const editorRequirementIds = computed(() => negotiatedDraftItems.value
  ? negotiatedDraftItems.value.map(item => item.requirementId)
  : pending.value.map(requirement => requirement.id))
const qtyOf = reactive<Record<string, string>>({})
const priceOf = reactive<Record<string, string>>({})
const draftTotalFormat = new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
const draftUnitPriceFormat = new Intl.NumberFormat('en-US', { useGrouping: false, minimumFractionDigits: 2, maximumFractionDigits: 2 })

function draftCellValue(field: 'qty' | 'price', id: string) {
  const value = (field === 'qty' ? qtyOf[id] : priceOf[id]) ?? ''
  // Formatting must never replace the value used in the save payload.
  if (!value || editedDraftCell.value === `${field}:${id}` || !Number.isFinite(Number(value))) return value
  if (field === 'qty') return /^[+-]?\d+\.\d+$/.test(value) ? trim(value) : value
  return draftUnitPriceFormat.format(Number(value))
}

function updateDraftCell(field: 'qty' | 'price', id: string, value: string) {
  const values = field === 'qty' ? qtyOf : priceOf
  values[id] = value
  editedDraftCell.value = `${field}:${id}`
}
const form = reactive({ supplierId: '', currency: 'CNY', expectedDate: '', payableDueDate: '', paymentTerms: '', remark: '', fulfillmentMode: 'DIRECT_SHIP', deliveryLocationType: 'PORT', deliveryPortId: 0, deliveryPortCode: '', deliveryPortName: '', warehouseId: 0, warehouseName: '', deliveryAddress: '', sourceChangeReason: '' })
const purchasePaymentSuggestions = paymentTermOptions.map(value => ({ value }))
const deliveryPorts = ref<{ id: string; unLocode: string; nameZh: string; nameEn: string }[]>([])

const detailOpen = ref(false)
const detail = ref<Order | null>(null)
const detailItems = ref<OrderItem[]>([])
const detailReceipts = ref<Receipt[]>([])
const detailDraftFiles = ref<OrderDraftFile[]>([])
const editableDraftDetail = computed(() => ['DRAFT', 'REJECTED'].includes(detail.value?.status ?? ''))
const compactDraftDetail = computed(() => editableDraftDetail.value && !detail.value?.historical)
const draftFilePreviewOpen = ref(false)
const draftFilePreviewSource = ref<Blob | null>(null)
const draftFilePreviewTitle = ref('')
const draftFilePreviewType = ref('')
const previewingDraftFileId = ref('')
interface QualityDetail {id:string;taskNo:string;status:string;lines:Array<{productName:string;requestedQty:string;qualifiedQty:string;uomCode:string;finalResult:string;issueDescription:string}>;files:Array<{id:string;fileName:string;downloadUrl:string}>;procurementHandlingStatus:string;procurementHandlingAction:string;procurementHandlingNote:string;procurementHandledByName:string}

const warehouses = ref<Warehouse[]>([])
const portWarehouses = computed(() => warehouses.value.filter((w) => w.whType === 'PORT_TERMINAL'))

const cancelOpen = ref(false)
const cancelling = ref<Order | null>(null)
const cancelReason = ref('')

const executionOpen = ref(false)
const executionTab = ref('overview')
const editingConfirmation = ref(false)
const followUpNote=ref('')
const executing = ref<Order | null>(null)
const executionItems = ref<OrderItem[]>([])
const executionReceipts = ref<Receipt[]>([])
const execution = reactive<Execution>({ followUps:[], confirmations: [], milestones: [], exceptions: [], reminders: [], inspections: [] })
const confirmationQty = reactive<Record<string, string>>({})
const confirmationPrice = reactive<Record<string, string>>({})
const confirmationForm = reactive({ confirmedDate: '', expectedDate: '', remark: '' })
const dispositionKinds = ['RETURN', 'DEDUCTION', 'CONCESSION', 'REWORK']
const closeShortOpen = ref(false)
const closingOrder = ref<Order | null>(null)
const closeForm = reactive({ shortfallAction: 'REORDER', note: '' })
const dispositionOpen = ref(false)
const dispositionTarget = ref<PurchaseInspection | null>(null)
const dispositionForm = reactive({ disposition: 'DEDUCTION', note: '' })
const contractOpen = ref(false)
const contractOrder = ref<Order | null>(null)
const contractFile = ref<File | null>(null)
const contractFiles = ref<any[]>([])
const contractForm = reactive({ contractNo: '', paymentTerms: '' })
const qualityRevision=ref(0)
const qualityOpen = ref(false)
const qualityOrder = ref<Order | null>(null)
const qualityItems = ref<OrderItem[]>([])
const qualityForm = reactive({ expectedDate:'', location:'', contactName:'', contactPhone:'', remark:'' })

const latestConfirmation = computed(() => execution.confirmations[0])
const canEditConfirmation = computed(() => canProduction && Boolean(executing.value) && !executing.value?.closedAt && canViewFulfillment(executing.value!))
function confirmationStatusLabel(value: string) {
  return value === 'MATCHED' ? t('purchaseFulfillment.recorded') : t(`orders.confirmStatuses.${value}`)
}
function confirmedLine(item: OrderItem) {
  return latestConfirmation.value?.lines.find(line => Number(line.poItemId) === Number(item.id))
}
function startConfirmationEdit() {
  if (!canEditConfirmation.value) return
  Object.keys(confirmationQty).forEach(key => delete confirmationQty[key])
  Object.keys(confirmationPrice).forEach(key => delete confirmationPrice[key])
  executionItems.value.forEach(item => {
    confirmationQty[item.id] = confirmedLine(item)?.confirmedQty ?? item.qty
    confirmationPrice[item.id] = confirmedLine(item)?.confirmedUnitPrice ?? item.unitPrice
  })
  confirmationForm.confirmedDate = latestConfirmation.value?.confirmedDate || new Date().toISOString().slice(0, 10)
  confirmationForm.expectedDate = latestConfirmation.value?.confirmedExpectedDate || ''
  confirmationForm.remark = latestConfirmation.value?.remark || ''
  editingConfirmation.value = true
}
function canViewFulfillment(row: Order) {
  return ['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status) && (!Number(row.sourceBusinessId) || Boolean(row.contractVerifiedAt))
}
function showOrderStage(row: Order) {
  if (['PARTIALLY_RECEIVED','RECEIVED'].includes(row.status)) return true
  return row.status !== (status.value === 'RECEIVED_OPEN' ? 'RECEIVED' : status.value) || !canViewFulfillment(row)
}


const common = (k: string) => t(`common.${k}`)

// What this order will cost, recomputed as the buyer types. The server is the
// authority; this is so nobody submits a number they have not seen.
const estimated = computed(() => {
  let sum = 0
  for (const id of editorRequirementIds.value) {
    sum += Number(qtyOf[id] ?? 0) * Number(priceOf[id] ?? 0)
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
const pinnedSupplier = computed(() => {
  const order = editing.value
  if (order?.supplierId && order.supplierId !== '0') {
    return { id: order.supplierId, code: order.supplierCode, name: order.supplierName }
  }
  const quote = quotationLines.value[0]
  if (quote?.supplierId && quote.supplierId !== '0') {
    return { id: quote.supplierId, code: '', name: quote.supplierName }
  }
})
// 这批之前已经下过单，本次买的是剩下的数量。
const isReorder = computed(() => quotationLines.value.some((r) => Number(r.orderedQty ?? 0) > 0))
const supplierLocked = computed(() => approvalEntry.value && !switchSupplier.value)
const reasonRequired = computed(() =>
  negotiatedDraftItems.value ? draftAdjustmentRequired.value : quotationLines.value.length > 0 && (switchSupplier.value || isReorder.value),
)
const showAdjustmentReason = computed(() => !negotiatedDraftItems.value || draftAdjustmentShown.value || reasonRequired.value)
const reasonPlaceholder = computed(() => {
  if (negotiatedDraftItems.value) return '请说明本次调整的原因'
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
    if (line) form.supplierId = String(line.supplierId || '')
    quotationLines.value.forEach((r) => { priceOf[r.id] = r.sourceUnitPrice || '0' })
  }
}

interface RowAction { key: string; label: string; tone: 'primary' | 'success' | 'warning' | 'danger'; run: () => void }
function primaryAction(row: Order): RowAction | null {
 if(row.historical && row.status==='DRAFT')return canWrite?{key:'history',label:'继续补录',tone:'primary',run:()=>{historyId.value=row.id;historyOpen.value=true}}:null
  // 已结案与已作废采购单是只读历史，不再出现任何履约动作。
  if (row.closedAt || row.status === 'CANCELLED') return null
  if (canActOnOrderApproval(row))
    return { key: 'approve', label: t('orders.reviewApproval'), tone: 'success', run: () => void openApprovalReview(row) }
  if (row.status === 'DRAFT' && canSubmit)
    return { key: 'submit', label: t('orders.submit'), tone: 'primary', run: () => void submit(row) }
  if (row.status === 'REJECTED' && canWrite)
    return { key: 'edit', label: t('orders.editAndResubmit'), tone: 'warning', run: () => void openEdit(row) }
	if (Number(row.sourceBusinessId) > 0 && row.status === 'ORDERED') {
		if (!row.signedContractUploadedAt && canWrite) return { key:'contract', label:'上传签署合同', tone:'primary', run:()=>openContract(row) }
		if (!row.contractVerifiedAt) return { key:'detail', label:'查看合同状态', tone:'primary', run:()=>void openDetail(row) }
	}
  // 收满、还没结案——当前该做的就是宣布这单到此为止（A3）。
  if (row.status === 'RECEIVED' && !row.closedAt && canClose)
    return { key: 'close', label: t('orders.closeOrder'), tone: 'primary', run: () => void closeOrder(row) }
  if (['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status))
    return { key: 'execution', label: t('purchaseFulfillment.view'), tone: 'warning', run: () => openExecution(row) }
  return null
}

function approvalTaskFor(row: Order): string {
  return approvalTasks.value[String(row.id)] ?? ''
}
function approvalOverrideFor(row: Order): boolean {
  return Boolean(approvalOverrides.value[String(row.id)])
}

function canActOnOrderApproval(row: Order): boolean {
  return row.status === 'PENDING_APPROVAL' && Boolean(approvalTaskFor(row))
}

async function openApprovalReview(row: Order) {
  await openDetail(row)
}

async function actOnOrderApproval(row: Order, action: 'APPROVE' | 'REJECT') {
  const taskID = approvalTaskFor(row)
  if (!taskID) return
  let comment = ''
  try {
    if (!approvalOverrideFor(row) && action === 'APPROVE') {
      await ElMessageBox.confirm(t('orders.approveConfirm', { no: row.poNo }), t('todos.approve'), {
        type: 'warning', confirmButtonText: t('todos.approve'), cancelButtonText: common('cancel'),
      })
    } else if (!approvalOverrideFor(row)) {
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
  const nextStatus = action === 'APPROVE' ? 'PENDING_CONTRACT' : 'DRAFT'
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
  add('withdrawApproval', t('orders.withdrawApproval'), canWithdrawApproval(row))
  if(qualityMenuTasks[row.id]===true)add('quality',t('quality.viewProgress'),true)
  else if(qualityMenuTasks[row.id]===false)add('quality',t('quality.apply'),canRequestQuality(row))
	if (Number(row.sourceBusinessId)>0 && row.status==='ORDERED') {
		add('contract','上传/更换签署合同',canWrite&&!row.paymentRequestedAt)
		if (!row.contractVerifiedAt) return out
	}
  add('execution', t('purchaseFulfillment.view'), canViewFulfillment(row))
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

function canWithdrawApproval(row: Order) {
  return canSubmit && row.status === 'PENDING_APPROVAL' && Boolean(withdrawalInstances.value[row.id])
}

function rowMenuActions(row: Order): { key: string; label: string; divided?: boolean }[] {
 if(row.historical && row.status==='DRAFT')return [{key:'detail',label:t('common.detail')},...(canWrite?[{key:'history',label:'继续补录'},{key:'buyer',label:t('orderBuyer.action')}]:[])]

  if (canActOnOrderApproval(row)) {
    return [
      { key: 'detail', label: t('common.detail') },
      { key: 'approveDecision', label: t('todos.approve'), divided: true },
      { key: 'rejectDecision', label: t('todos.reject') },
      ...(canWithdrawApproval(row) ? [{ key: 'withdrawApproval', label: t('orders.withdrawApproval'), divided: true }] : []),
    ]
  }
  const actions = allActions(row)
 if(row.historical && row.status==='ORDERED' && canWrite && !row.paymentRequestedAt)actions.push({key:'history',label:'补齐资料 / 价格'})
 if(row.historical && canWrite)actions.push({key:'tradeContext',label:t('historicalShipping.edit')})
 // 历史补录的单可以换采购员（#643）：一个人统一录入，再分给真正负责的采购。已取消的不需要。
 if(row.historical && canWrite && row.status!=='CANCELLED')actions.push({key:'buyer',label:t('orderBuyer.action')})
  if (['DRAFT', 'REJECTED'].includes(row.status)) {
    const order = ['detail', 'edit', 'submit', 'cancel']
    return actions.sort((left, right) => order.indexOf(left.key) - order.indexOf(right.key))
  }
  return actions
}
// 工厂回签状态的列表子标签（B5 尾巴）。只有实际录入过回签时才显示，
// 不再用邮件发送状态制造第二个“是否下单”的业务门槛。
function confirmTag(row: Order): { label: string; type: 'success' | 'warning' | 'danger' | 'info' } | null {
  if (!['ORDERED', 'PARTIALLY_RECEIVED', 'RECEIVED'].includes(row.status)) return null
  const s = row.confirmStatus
  if (!s) return { label: t('orders.confirmTags.NONE'), type: 'info' }
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
 if(key==='tradeContext'){tradeContextOrder.value=row;tradeContextOpen.value=true;return}
 if(key==='buyer'){buyerOrder.value=row;buyerOpen.value=true;return}
 if(key==='history'){historyId.value=row.id;historyOpen.value=true;return}
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
    case 'approveDecision': void actOnOrderApproval(row, 'APPROVE'); break
    case 'rejectDecision': void actOnOrderApproval(row, 'REJECT'); break
    case 'edit': openEdit(row); break
    case 'submit': void submit(row); break
    case 'withdrawApproval': void withdrawApproval(row); break
    case 'receive': if(row.fulfillmentMode==='WAREHOUSE')void router.push('/warehouses/arrivals');else void openExecution(row); break
    case 'execution': openExecution(row); break
    case 'downloadXlsx': void downloadOrder(row, 'xlsx'); break
    case 'downloadPdf': void downloadOrder(row, 'pdf'); break
    case 'close': void closeOrder(row); break
    case 'cancel': openCancel(row); break
	case 'contract': openContract(row); break
	case 'quality': void openQualityApplication(row); break
	}
}

async function openQualityApplication(row: Order) {
	const [d,q]=await Promise.all([get<{order:Order;items:OrderItem[]}>(`/purchase-orders/${row.id}`),get<{tasks:QualityDetail[]}>(`/purchase-orders/${row.id}/quality-inspections`)])
	if((q.tasks||[]).length){ElMessage.info('该采购单已有整单质检任务，已打开原质检资料');await openDetail(row);return}
	if(!canRequestQuality(d.order)){ElMessage.info(qualityRequestHint(d.order));return}
	qualityOrder.value=d.order;qualityItems.value=d.items||[]
	qualityForm.expectedDate='';qualityForm.location=row.factoryName||row.supplierName;qualityForm.contactName='';qualityForm.contactPhone='';qualityForm.remark=''
	qualityOpen.value=true
}
async function submitQualityApplication(){
	if(!qualityOrder.value)return
	if(!qualityItems.value.length){ElMessage.warning('采购单没有可质检的产品');return}
	const lines=qualityItems.value.map(i=>({po_item_id:Number(i.id),qty:i.qty}))
	saving.value=true
	try{await post(`/purchase-orders/${qualityOrder.value.id}/quality-inspections`,{expected_date:qualityForm.expectedDate,inspection_location:qualityForm.location,contact_name:qualityForm.contactName,contact_phone:qualityForm.contactPhone,remark:qualityForm.remark,lines});ElMessage.success(t('quality.applied'));qualityMenuTasks[qualityOrder.value.id]=true;qualityRevision.value++;qualityOpen.value=false}
	finally{saving.value=false}
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
    approvalOverrides.value = {}
    withdrawalInstances.value = {}
    if (status.value === 'PENDING_APPROVAL') {
      const resolved = await Promise.all(rows.value.map(async row => [row.id, await getPurchaseOrderApprovalActions(row.id, canSubmit)] as const))
      for (const [orderID, { approval, withdrawalInstanceId }] of resolved) {
        if (withdrawalInstanceId) withdrawalInstances.value[orderID] = withdrawalInstanceId
        if (!approval) continue
        approvalTasks.value[orderID] = approval.taskId
        approvalOverrides.value[orderID] = approval.override
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
  if (editing.value) return r.requiredQty === '' ? '' : String(Number(r.requiredQty ?? 0) - Number(r.orderedQty ?? 0))
  return r.availableQty ?? String(Number(r.requiredQty ?? 0) - Number(r.orderedQty ?? 0))
}

async function openCreate(preselect?: string[]) {
	editing.value = null
  draftSupplierActive.value = undefined
  selectedSupplierName.value = ''
  negotiatedDraftItems.value = null
	approvalEntry.value = Boolean(preselect?.length)
  form.supplierId = ''
  form.currency = 'CNY'
  form.expectedDate = ''
  form.payableDueDate = ''
  form.paymentTerms = ''
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
  const [reqs, ports, whs] = await Promise.all([
    get<{ requirements: Requirement[] }>('/requirements', { status: 'PENDING', page_size: 200 }),
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
		if (r.status !== 'WAITING_REQUOTE' && r.supplierId !== '0') form.supplierId = String(r.supplierId || '')
        form.currency = r.sourceCurrency || 'USD'
        priceOf[r.id] = r.sourceUnitPrice || '0'
      }
    })
    const requiredDates = pending.value.map((r) => r.requiredDate).filter(Boolean).sort()
    form.expectedDate = requiredDates[0] || ''
    const commercialTerms = pending.value
      .filter((r) => r.source === 'CUSTOMER_QUOTATION')
      .map((r) => `${r.productName}: ${r.sourcePaymentTerms || '—'} / ${r.sourceIncoterm || '—'} / ${r.sourceValidUntil || '—'}`)
    form.remark = commercialTerms.join('\n')
  }
  createOpen.value = true
}

function selectContractFile(file:any){contractFile.value=file.raw as File;contractFiles.value=[file]}
function openContract(row:Order){contractOrder.value=row;contractForm.contractNo=row.businessDocumentNo||row.poNo;contractForm.paymentTerms=row.paymentTerms||'';contractFile.value=null;contractFiles.value=[];contractOpen.value=true}
async function saveContract(){const row=contractOrder.value,file=contractFile.value;if(!row||!file){ElMessage.warning('请选择双方签署合同文件');return}if(!contractForm.contractNo.trim()||!contractForm.paymentTerms.trim()){ElMessage.warning('请填写采购合同号和付款条件');return}saving.value=true;try{const signed=await post<{fileKey:string;uploadUrl:string}>(`/purchase-orders/${row.id}/contract/presign`,{file_name:file.name});const putResult=await fetch(signed.uploadUrl,{method:'PUT',body:file});if(!putResult.ok)throw new Error('合同文件上传失败');await post(`/purchase-orders/${row.id}/contract`,{contract_no:contractForm.contractNo,payment_terms:contractForm.paymentTerms,file_key:signed.fileKey,file_name:file.name});ElMessage.success('签署合同已保存，等待财务审核');contractOpen.value=false;status.value='PENDING_CONTRACT';await router.replace({path:'/purchase-orders',query:{status:'PENDING_CONTRACT',order:row.id}});await reload()}finally{saving.value=false}}
async function loadDraftRequirements(items: OrderItem[]) {
  const pages = await Promise.all(['PENDING', 'PARTIALLY_ORDERED'].map(status =>
    get<{ requirements: Requirement[] }>('/requirements', { status, page_size: 200 }, quietErrors)
      .catch(() => ({ requirements: [] })),
  ))
  const requirements = new Map(pages.flatMap(page => page.requirements ?? []).map(row => [row.id, row]))
  await Promise.all(items.filter(item => !requirements.has(item.requirementId)).map(async item => {
    // The draft's saved lines must survive even if the requirements list omits them.
    const result = await get<{ requirement: Requirement }>(`/requirements/${item.requirementId}`, undefined, quietErrors)
      .catch(() => undefined)
    requirements.set(item.requirementId, result?.requirement ?? {
      id: item.requirementId, productName: item.productName, spec: item.spec, uomCode: item.uomCode,
      contractNo: item.contractNo, customerName: item.customerName, source: item.source,
      requiredQty: '', orderedQty: '', reservedQty: '', availableQty: '', requiredDate: '', status: '',
      quotationId: '', supplierId: '', supplierName: '', sourceCurrency: '', sourceUnitPrice: '',
      sourcePaymentTerms: '', sourceIncoterm: '', sourceValidUntil: '',
    })
  }))
  return [...requirements.values()]
}

async function openEdit(row: Order) {
 if(row.historical){historyId.value=row.id;historyOpen.value=true;return}
  const { order: current, items = [], fromExecutionNegotiation, sourceChangeReason, supplierActive } = await get<{
    order: Order; items?: OrderItem[]; fromExecutionNegotiation?: boolean; sourceChangeReason?: string; supplierActive?: boolean
  }>(`/purchase-orders/${row.id}`)
  if (!['DRAFT', 'REJECTED'].includes(current.status)) {
    ElMessage.warning(t('orders.notEditable'))
    await load()
    return
  }
  const [requirements, ports, whs] = await Promise.all([
    fromExecutionNegotiation ? Promise.resolve([]) : loadDraftRequirements(items),
    canReadPorts
      ? get<{ ports: typeof deliveryPorts.value }>('/ports', { page_size: 200, status: 'ACTIVE' })
      : Promise.resolve({ ports: [] }),
    canReadWarehouses
      ? get<{ warehouses: Warehouse[] }>('/warehouses')
      : Promise.resolve({ warehouses: [] }),
  ])
  editing.value = current
  draftSupplierActive.value = supplierActive
  selectedSupplierName.value = ''
  negotiatedDraftItems.value = fromExecutionNegotiation ? items : null
  approvalEntry.value = false
  form.supplierId = current.supplierId === '0' ? '' : current.supplierId
  form.currency = current.currency || 'CNY'
  form.expectedDate = current.expectedDate || ''
  form.payableDueDate = current.payableDueDate || ''
  form.paymentTerms = current.paymentTerms || ''
  form.remark = current.remark || ''
  form.fulfillmentMode = current.fulfillmentMode || 'DIRECT_SHIP'
  form.deliveryLocationType = current.deliveryLocationType || 'PORT'
  form.deliveryPortId = Number(current.deliveryPortId || 0)
  form.deliveryPortCode = current.deliveryPortCode || ''
  form.deliveryPortName = current.deliveryPortName || ''
  form.warehouseId = Number(current.warehouseId || 0)
  form.warehouseName = current.warehouseName || ''
  form.deliveryAddress = current.deliveryAddress || ''
  form.sourceChangeReason = sourceChangeReason ?? ''
  draftAdjustmentShown.value = Boolean(form.sourceChangeReason.trim())
  draftAdjustmentRequired.value = false
  editedDraftCell.value = ''
  switchSupplier.value = false
  Object.keys(qtyOf).forEach((key) => delete qtyOf[key])
  Object.keys(priceOf).forEach((key) => delete priceOf[key])
  pending.value = requirements
  deliveryPorts.value = ports.ports ?? []
  warehouses.value = whs.warehouses ?? []
  for (const item of items) {
    qtyOf[item.requirementId] = item.qty
    priceOf[item.requirementId] = item.unitPrice
  }
  createOpen.value = true
}

async function submitCreate() {
  const wasApprovalEntry = approvalEntry.value
  const supplierId = form.supplierId
  if (!supplierId || supplierId === '0') {
    ElMessage.warning(t('orders.supplierRequired'))
    return
  }
  const lines = editorRequirementIds.value
    .filter(id => Number(qtyOf[id] ?? 0) > 0)
    .map(id => ({
      requirement_id: Number(id),
      qty: qtyOf[id],
      unit_price: priceOf[id] || '0',
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
    ElMessage.warning(negotiatedDraftItems.value ? '请选择或输入收货港口' : '请选择收货港口')
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
    ElMessage.warning(negotiatedDraftItems.value ? '请填写调整原因'
      : switchSupplier.value ? '换供应商请填写调整原因' : '补购剩余数量请填写调整原因')
    return
  }
  saving.value = true
  try {
    const payload = {
      supplier_id: supplierId,
      currency: form.currency,
      expected_date: form.expectedDate,
      payable_due_date: form.payableDueDate,
      payment_terms: form.paymentTerms.trim(),
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
      try {
        res = await put<{ id?: string; poNo: string }>(`/purchase-orders/${editing.value.id}`, {
          ...payload,
          keep_current_supplier: supplierId === editing.value.supplierId,
        })
      } catch (error) {
        const code = (error as ApiFailure).code
        if (negotiatedDraftItems.value && (code === 'PO_SOURCE_CHANGE_REASON_REQUIRED' || code === 'PO_SUPPLIER_CHANGE_REASON_REQUIRED')) {
          draftAdjustmentRequired.value = true
        }
        throw error
      }
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
            status: draftStatus,
            keyword: pinnedSupplier.value?.id === supplierId ? pinnedSupplier.value.name : selectedSupplierName.value,
            page_size: 200,
          }, quietErrors),
        ))
        const existing = existingLists
          .flatMap((item) => item.orders ?? [])
          .find((order) => Number(order.sourceQuotationId) === quotationID && order.supplierId === supplierId)
        if (!existing) {
          ElMessage.error(apiFailure.message || t('common.requestFailed'))
          throw failure
        }
        const updated = await put<{ poNo: string }>(`/purchase-orders/${existing.id}`, {
          ...payload,
          payment_terms: payload.payment_terms || undefined,
          keep_current_supplier: true,
        })
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

function setNegotiatedDeliveryPort(value: number | string | undefined) {
  if (typeof value === 'number') {
    form.deliveryPortId = value
    selectDeliveryPort(value)
  } else {
    form.deliveryPortId = 0
    form.deliveryPortCode = ''
    form.deliveryPortName = value?.trim() ?? ''
  }
}

function portLabel(port: { unLocode?: string; nameZh?: string; nameEn?: string }) {
  return [port.unLocode, port.nameZh || port.nameEn].filter(Boolean).join(' · ')
}

function selectOrderWarehouse(id: number) {
  form.warehouseName = warehouses.value.find((item) => Number(item.id) === Number(id))?.name ?? ''
}

async function openDetail(row: Pick<Order, 'id'>) {
  const [d, f] = await Promise.all([
    get<{order: Order; items: OrderItem[]; receipts: Receipt[]}>(`/purchase-orders/${row.id}`),
    get<{files: OrderDraftFile[]}>(`/purchase-orders/${row.id}/draft-files`),
  ])
  detail.value = d.order
  detailItems.value = d.items ?? []
  detailReceipts.value = d.receipts ?? []
  detailDraftFiles.value=f.files??[]
  detailOpen.value = true
}
function formatDraftFileSize(value:string){const size=Number(value);return size>=1048576?`${(size/1048576).toFixed(1)} MB`:`${Math.max(1,Math.ceil(size/1024))} KB`}
async function downloadDraftFile(file:OrderDraftFile){if(!detail.value)return;const result=await download(`/purchase-orders/${detail.value.id}/draft-files/${file.id}/download`);saveBlob(result.blob,result.fileName||file.fileName)}
const uploadingDraftFiles = ref(false)
const canEditDraftFiles = computed(() => canWrite && ['DRAFT','REJECTED'].includes(detail.value?.status ?? ''))
async function uploadDraftFiles(event: Event) {
  const input = event.target as HTMLInputElement
  const files = [...(input.files ?? [])]
  input.value = ''
  const orderId = detail.value?.id
  if (!orderId || !canEditDraftFiles.value || uploadingDraftFiles.value) return
  uploadingDraftFiles.value = true
  try {
    for (const file of files) {
      if (file.size > 10*1024*1024) { ElMessage.warning(`${file.name} 超过 10 MB`); continue }
      const dataUrl = await new Promise<string>((resolve,reject)=>{
        const reader = new FileReader()
        reader.onload = () => resolve(String(reader.result))
        reader.onerror = reject
        reader.readAsDataURL(file)
      })
      const {file:uploaded} = await post<{file: OrderDraftFile}>(`/purchase-orders/${orderId}/draft-files`, {
        file_name:file.name,content_type:file.type,file_data:dataUrl.split(',')[1],
      })
      if (detail.value?.id === orderId && !detailDraftFiles.value.some(file=>file.id===uploaded.id)) {
        detailDraftFiles.value = [uploaded, ...detailDraftFiles.value]
      }
    }
  } finally { uploadingDraftFiles.value = false }
}
async function previewDraftFile(file:OrderDraftFile){
  if(!detail.value)return
  previewingDraftFileId.value=file.id
  try{
    const result=await download(`/purchase-orders/${detail.value.id}/draft-files/${file.id}/download`)
    draftFilePreviewSource.value=result.blob
    draftFilePreviewTitle.value=result.fileName||file.fileName
    draftFilePreviewType.value=file.contentType||result.blob.type
    draftFilePreviewOpen.value=true
  }finally{previewingDraftFileId.value=''}
}
async function deleteDraftFile(file:OrderDraftFile){if(!detail.value)return;await ElMessageBox.confirm(`删除资料“${file.fileName}”？`,'删除资料',{type:'warning'});await del(`/purchase-orders/${detail.value.id}/draft-files/${file.id}`);detailDraftFiles.value=detailDraftFiles.value.filter(item=>item.id!==file.id);ElMessage.success('采购资料已删除')}
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
  execution.followUps = data.followUps ?? []
  execution.confirmations = data.confirmations ?? []
  execution.milestones = data.milestones ?? []
  execution.exceptions = data.exceptions ?? []
  execution.reminders = data.reminders ?? []
  execution.inspections = data.inspections ?? []
}

async function openExecution(row: Order) {
  executing.value = row
  executionTab.value='overview'
  editingConfirmation.value=false
  followUpNote.value=''
  const detailData = await get<{ order: Order; items: OrderItem[]; receipts: Receipt[] }>(`/purchase-orders/${row.id}`)
  executionItems.value = detailData.items ?? []
  executionReceipts.value = detailData.receipts ?? []
  executing.value = detailData.order
  await loadExecution()
  if (!latestConfirmation.value) startConfirmationEdit()
  executionOpen.value = true
}

async function refreshAfterDelivery(){
  await load()
  if(executing.value){const d=await get<{order:Order;items:OrderItem[];receipts:Receipt[]}>(`/purchase-orders/${executing.value.id}`);executing.value=d.order;executionItems.value=d.items??[];executionReceipts.value=d.receipts??[]}
}

async function submitConfirmation() {
  if (!canEditConfirmation.value || saving.value) return
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
    editingConfirmation.value = false
    await load()
  } finally { saving.value = false }
}

function confirmationMoney(value:string){const n=Number(value);return Number.isFinite(n)?n.toLocaleString('en-US',{minimumFractionDigits:2,maximumFractionDigits:4}):'—'}

function formatFollowUpTime(value:string){if(!value)return '—';const date=new Date(value.replace(' ','T').replace(/([+-]\d{2})$/,'$1:00'));return Number.isNaN(date.getTime())?value:date.toLocaleString()}

async function saveFollowUp() {
  if(!executing.value || saving.value)return
  const note=followUpNote.value.trim()
  if(!note){ElMessage.warning(t('purchaseFulfillment.noteRequired'));return}
  saving.value=true
  try{await post(`/purchase-orders/${executing.value.id}/follow-ups`,{note});followUpNote.value='';await loadExecution();ElMessage.success(t('purchaseFulfillment.noteSaved'))}
  finally{saving.value=false}
}

function productionNodeLabel(node: string): string { return t(`orders.productionNodes.${node}`) }

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
async function withdrawApproval(row: Order) {
  const instanceId = withdrawalInstances.value[row.id]
  if (withdrawing.value || !canWithdrawApproval(row) || !instanceId) return
  withdrawing.value = true
  try {
    try {
      await ElMessageBox.confirm(
        t('orders.withdrawApprovalHint', { no: row.poNo }), t('orders.withdrawApproval'), {
          confirmButtonText: t('orders.withdrawApproval'),
          cancelButtonText: common('cancel'),
          type: 'warning',
        },
      )
    } catch {
      return
    }
    try {
      await post(`/purchase-orders/${row.id}/withdraw-approval`, { approval_instance_id: instanceId })
    } catch {
      // The API toast explains the failure. Refresh to drop stale actions.
      await load()
      return
    }
    ElMessage.success(t('orders.withdrawApprovalDone'))
    status.value = 'DRAFT'
    await reload()
  } finally {
    withdrawing.value = false
  }
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
    if(row.fulfillmentMode==='WAREHOUSE')void router.push('/warehouses/arrivals');else void openExecution(row)
    return
  }
  if (command === 'execution') {
    void openExecution(row)
    return
  }
  if (command === 'cancel') openCancel(row)
}

// 数据库沿用 ORDERED，页面根据合同资料是否上传、财务是否核验展示真实业务阶段。
function orderStatusLabel(row: Order): string {
  if (row.status === 'ORDERED' && Number(row.sourceBusinessId) > 0 && !row.signedContractUploadedAt)
    return t('orders.statuses.PENDING_CONTRACT')
  if (row.status === 'ORDERED' && Number(row.sourceBusinessId) > 0 && !row.contractVerifiedAt)
    return t('orders.statuses.PENDING_FINANCE')
  if (row.status === 'ORDERED') return t('orders.businessStatuses.ORDERED')
  return t(deliveryStatusKey(row))
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
  if (createOpen.value || cancelOpen.value) return
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
      if (route.query.fulfillment === '1') {
        const { order } = await get<{ order: Order }>(`/purchase-orders/${importedOrder}`)
        keyword.value = order.poNo
        status.value = order.status === 'RECEIVED' ? 'RECEIVED_OPEN' : order.status
        await load()
        await openExecution(order)
      } else {
        await openDetail({ id: importedOrder })
      }
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
  const qsStatus = listStatusFor(String(route.query.status ?? ''))
  const qsUnsent = String(route.query.unsent ?? '')
  if (qsUnsent === '1') {
    if (status.value !== 'UNSENT') {
      status.value = 'UNSENT'
      reload()
    }
  } else if (qsStatus && status.value !== qsStatus) {
    status.value = qsStatus
    reload()
  }
})
</script>

<style scoped>
.specification-remark {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
:global(.historical-order-detail .el-dialog__body){max-height:calc(90vh - 90px);overflow:auto}
.confirmation-section h3 { margin:0; }
.confirmation-section { border:1px solid var(--el-border-color-light); border-radius:8px; padding:14px; margin-bottom:18px; }
.confirmation-facts { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:12px 24px; margin:10px 0 16px; }
.confirmation-facts>div { display:flex; gap:14px; align-items:baseline; }
.confirmation-facts span { color:var(--el-text-color-secondary); flex-shrink:0; }
.confirmation-facts strong { font-weight:500; overflow-wrap:anywhere; }
.pending-date { color:var(--el-color-warning-dark-2); }
.confirmation-actions { margin-top:12px; }
.confirmation-history { margin-top:16px; }
.legacy-history { margin-top:20px; }

.follow-up-text{white-space:pre-wrap;overflow-wrap:anywhere}
:global(.fulfillment-dialog .el-dialog__body){max-height:calc(100vh - 170px);overflow-y:auto}

.requote-card{margin-bottom:16px;border-color:#cfeaf5}.requote-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:12px}.requote-head strong{color:#173a4d;font-size:17px}.requote-head p{margin:5px 0 0;color:#647789;font-size:13px}.requote-actions{display:flex;align-items:center;gap:10px}.batch-common{display:grid;grid-template-columns:220px minmax(280px,1fr) 220px;gap:14px;margin:16px 0 4px}.batch-table :deep(.el-select){width:100%}.contract-state{padding:10px 12px;border-radius:8px;background:#f2f8fb;color:#52697a}.quality-apply-head{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 16px}.quality-apply-head :deep(.el-input),.quality-apply-head :deep(.el-date-editor){width:100%}
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
.orders-table :deep(.el-table__header .el-table__cell) {
  padding-top: 13px;
  padding-bottom: 13px;
}
.orders-table :deep(.el-table__body .el-table__cell) {
  padding-top: 12px;
  padding-bottom: 12px;
}
.row-actions {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: 2px 12px;
}
.row-actions :deep(.el-button) {
  margin-left: 0;
}
.more-actions-trigger {
  font-weight: 500;
  min-width: 92px;
}
.drop-arrow {
  margin-left: 7px;
  font-size: 10px;
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
.supplier-row {
  display: flex;
  flex-wrap: wrap;
  width: 100%;
  align-items: center;
  gap: 6px;
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
:global(.purchase-draft-dialog) {
  --el-dialog-padding-primary: 18px;
  border-radius: 10px;
}
:global(.purchase-draft-dialog .el-dialog__body) {
  max-height: calc(90dvh - 108px);
  overflow-y: auto;
}
:global(.purchase-draft-dialog .el-dialog__footer) {
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.purchase-draft-form {
  display: grid;
  grid-template-columns: repeat(12, minmax(0, 1fr));
  gap: 10px 14px;
  margin-bottom: 0;
}
.purchase-draft-form :deep(.el-form-item) {
  min-width: 0;
  margin-bottom: 0;
}
.purchase-draft-form :deep(.el-form-item__label) {
  margin-bottom: 4px;
  line-height: 1.35;
  font-weight: 600;
}
.purchase-draft-form :deep(.el-date-editor.el-input),
.purchase-draft-form :deep(.el-select) {
  width: 100%;
}
.payment-terms-picker,
.payment-terms-picker :deep(.el-autocomplete) { width: 100%; }
.draft-location :deep(.el-radio-group) { gap: 4px 12px; }
.draft-location :deep(.el-radio) { margin-inline-end: 0; }
.purchase-draft-form .draft-supplier,
.purchase-draft-form .draft-note { grid-column: span 6; }
.purchase-draft-form .draft-note-full { grid-column: 1 / -1; }
.purchase-draft-form .draft-date,
.purchase-draft-form .draft-location { grid-column: span 3; }
.purchase-draft-form .draft-fulfillment { grid-column: span 5; }
.purchase-draft-form .draft-destination { grid-column: span 4; }
.purchase-draft-form .draft-warehouse { grid-column: span 7; }
.negotiated-draft-heading {
  margin-top: 14px;
  padding-top: 10px;
  border-top: 1px solid var(--el-border-color-lighter);
  color: #173d50;
}
.negotiated-draft-footer {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 16px;
}
.negotiated-draft-footer .total-row {
  margin-top: 0;
  text-align: left;
}
@media (max-width: 800px) {
  .purchase-draft-form .draft-fulfillment,
  .purchase-draft-form .draft-location { grid-column: span 6; }
  .purchase-draft-form .draft-destination { grid-column: 1 / -1; }
  .purchase-draft-form .draft-warehouse { grid-column: span 6; }
}
@media (max-width: 560px) {
  .purchase-draft-form .draft-supplier,
  .purchase-draft-form .draft-fulfillment,
  .purchase-draft-form .draft-location,
  .purchase-draft-form .draft-destination,
  .purchase-draft-form .draft-note { grid-column: 1 / -1; }
  .purchase-draft-form .draft-date { grid-column: span 6; }
}
.desc {
  margin-bottom: 14px;
}
.order-draft-files { margin:14px 0 10px; padding:10px 12px; border:1px solid #dce7ed; border-radius:8px; background:#fafcfd; }
.order-draft-files .side-title { margin:0 0 5px; font-size:14px; font-weight:600; color:#29495b; }
.draft-upload { display:inline-flex;padding:4px 8px;color:#167fa4;cursor:pointer; }
.draft-upload input { position:absolute;opacity:0;width:1px;height:1px; }
.draft-upload:focus-within { outline:2px solid #168cab; }
.order-draft-file { display:flex;align-items:center;justify-content:space-between;gap:14px;padding:7px 2px;border-top:1px solid #edf2f5; }
.order-draft-file>span:first-child { display:flex;flex-direction:column;gap:2px;min-width:0; }
.order-draft-file strong { overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:#344f5d;font-weight:500; }
.order-draft-file small { color:#7b8d97;font-size:12px; }
:global(.compact-draft-detail) {
  --el-dialog-padding-primary: 18px;
  border-radius: 10px;
}
:global(.compact-draft-detail .el-dialog__body) {
  max-height: calc(90dvh - 64px);
  overflow-y: auto;
}
.draft-detail-summary { padding-bottom: 10px; }
.draft-detail-heading {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}
.draft-detail-facts {
  display: grid;
  grid-template-columns: minmax(0, 1.25fr) repeat(4, minmax(0, 1fr));
  gap: 8px 16px;
  margin: 8px 0 0;
}
.draft-detail-facts dt {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.draft-detail-facts dd {
  margin: 2px 0 0;
  color: var(--el-text-color-primary);
  overflow-wrap: anywhere;
}
.draft-payment-terms {
  grid-column: 1 / -1;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.draft-detail-remark,
.order-draft-files.draft-detail-files {
  margin: 0;
  padding: 10px 0;
  border: 0;
  border-top: 1px solid var(--el-border-color-lighter);
  border-radius: 0;
  background: transparent;
}
.draft-detail-remark .side-title { margin: 0 0 4px; }
.draft-detail-remark p {
  margin: 0;
  line-height: 1.55;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.draft-detail-heading .side-title { margin: 0; }
.draft-detail-table-heading {
  margin-top: 0;
  padding-top: 10px;
  border-top: 1px solid var(--el-border-color-lighter);
  color: #173d50;
}
@media (max-width: 760px) {
  .draft-detail-facts { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
@media (max-width: 560px) {
  .draft-detail-facts { grid-template-columns: repeat(2, minmax(0, 1fr)); }
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
.quality-summary{margin-top:18px}.quality-summary-card{margin-bottom:12px}.quality-summary-head{display:flex;justify-content:space-between;align-items:center;margin-bottom:10px}.quality-files{display:flex;gap:10px;flex-wrap:wrap;margin:10px 0}.quality-handle{display:grid;grid-template-columns:150px 1fr auto;gap:10px;margin-top:12px}
@media (max-width: 760px) {
  .execution-section-head { align-items: flex-start; flex-direction: column; }
  .overall-progress { width: 100%; }
  .execution-form-grid { grid-template-columns: 1fr; }
  .span-all { grid-column: auto; }
}
</style>

<style scoped>
.confirmation-section{padding:14px;margin-bottom:0}.confirmation-section h3{font-size:16px;color:var(--el-text-color-primary)}.confirmation-facts{grid-template-columns:repeat(3,minmax(0,1fr));gap:10px 20px;margin:10px 0 12px;font-size:13px}.confirmation-facts>div{gap:10px}.execution-section-head{margin-bottom:10px}.confirmation-history{margin-top:10px}.confirmation-history :deep(.el-collapse-item__header){height:34px;font-size:12px}.confirmation-section :deep(.el-table__cell){padding:6px 0}.confirmation-section :deep(.cell){line-height:22px}:global(.fulfillment-dialog .el-dialog__body){padding-top:4px}
@media(max-width:750px){.confirmation-facts{grid-template-columns:repeat(2,minmax(0,1fr))}}
</style>
