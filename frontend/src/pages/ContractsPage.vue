<template>
  <div class="contracts-page">
    <WorkflowPageHeader :title="t('contracts.title')" :description="t('contracts.listSubtitle')">
      <template #actions><el-button v-if="canWrite" type="primary" @click="openDirect">{{t('contracts.enterExecuting')}}</el-button></template>
    </WorkflowPageHeader>

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
          <el-option v-for="s in STATUSES" :key="s" :value="s" :label="contractStatusLabel(s)" />
        </el-select>
        <el-select v-model="ownerFilter" clearable filterable :placeholder="t('contracts.responsibleSales')" style="width: 220px" @change="reload"><el-option v-for="e in filterOwners" :key="e.id" :value="e.id" :label="e.name"/></el-select>
        <el-button @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table :data="contracts" v-loading="loading">
        <el-table-column prop="contractNo" :label="t('contracts.systemContractNo')" min-width="175"><template #default="{row}"><el-button link type="primary" @click="openDetail(row.id)">{{row.contractNo}}</el-button></template></el-table-column>
        <el-table-column :label="t('contracts.externalContractNo')" min-width="140"><template #default="{row}">{{row.externalContractNo||'—'}}</template></el-table-column>
        <el-table-column prop="customerName" :label="t('contracts.customer')" min-width="150" />
        <el-table-column :label="t('contracts.amount')" width="140" align="right">
          <template #default="{ row }">{{ row.totalAmount }} {{ row.currency }}</template>
        </el-table-column>
        <el-table-column :label="t('contracts.owner')" width="90">
          <template #default="{ row }">{{ row.salesEmployee || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="statusType(row.status)">{{ contractStatusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('contracts.updatedAt')" width="170"><template #default="{row}">{{row.updatedAt?new Date(row.updatedAt).toLocaleString():'—'}}</template></el-table-column>
        <el-table-column :label="t('common.actions')" width="130" fixed="right"><template #default="{row}"><el-button link type="primary" @click="openDetail(row.id)">{{t(['DRAFT','PENDING_APPROVAL','PENDING_SIGN'].includes(row.status)?'contracts.continue':'contracts.view')}}</el-button></template></el-table-column>
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

    <el-dialog v-model="supplementOpen" title="补充合同信息" width="min(520px,94vw)"><el-form label-position="top"><el-form-item label="原合同号"><el-input v-model="supplementForm.externalContractNo"/></el-form-item><el-form-item label="应收日期"><el-date-picker v-model="supplementForm.due" type="date" value-format="YYYY-MM-DD"/></el-form-item></el-form><template #footer><el-button @click="supplementOpen=false">取消</el-button><el-button type="primary" @click="saveSupplement">保存</el-button></template></el-dialog>
    <!-- A signed contract that existed before it reached this ERP. -->
    <el-dialog v-model="directOpen" :title="t('contracts.createDirect')" width="min(1180px,94vw)" top="4vh">
      <el-alert :title="t('contracts.directHint')" type="info" :closable="false" show-icon class="alert" />
      <el-form label-width="120px" class="head-form">
        <el-form-item :label="t('contracts.customer')" required>
          <el-select
            v-model="directForm.customerId"
            filterable
            clearable
            remote
            :remote-method="searchCustomers"
            style="width: 320px"
            :placeholder="t('contracts.pickCustomer')"
          >
            <el-option
              v-for="c in customers"
              :key="c.id"
              :value="Number(c.id)"
              :label="`${c.code} · ${c.name}`"
            />
          </el-select>
          <el-select v-model="directForm.currency" style="width: 110px; margin-left: 12px">
            <el-option v-for="c in CURRENCIES" :key="c" :value="c" :label="c" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('contracts.responsibleSales')" required>
          <el-select v-model="directForm.salesEmployeeId" :disabled="!canPickContractOwner" filterable style="width: 320px">
            <el-option v-for="e in contractOwners" :key="e.id" :value="Number(e.id)" :label="e.name" />
          </el-select>
          <span class="hint">{{ t('contracts.ownerHint') }}</span>
        </el-form-item>
        <el-form-item :label="t('contracts.originalBuyer')">
          <el-select v-model="directForm.procurementEmployeeId" filterable style="width: 320px">
            <el-option v-for="e in contractOwners" :key="e.id" :value="Number(e.id)" :label="e.name" />
          </el-select>
          <span class="hint">{{ t('contracts.originalBuyerHint') }}</span>
        </el-form-item>
        <el-form-item :label="t('contracts.originalSupplier')">
          <el-select v-model="directForm.supplierId" filterable style="width: 420px">
            <el-option v-for="s in directSuppliers" :key="s.id" :value="Number(s.id)" :label="`${s.code} · ${s.nameZh || s.nameEn || s.name}`" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('contracts.externalContractNo')">
          <el-input v-model="directForm.externalContractNo" clearable style="width: 320px" :placeholder="t('contracts.externalContractNoHint')" />
        </el-form-item>
        <el-form-item :label="t('contracts.contractDates')" required>
          <el-date-picker v-model="directForm.signedDate" type="date" value-format="YYYY-MM-DD" :placeholder="t('contracts.signedDate')" style="width: 190px" />
          <el-date-picker v-model="directForm.effectiveDate" type="date" value-format="YYYY-MM-DD" :placeholder="t('contracts.effectiveDate')" style="width: 190px; margin-left: 12px" />
          <el-date-picker v-model="directForm.deliveryDate" type="date" value-format="YYYY-MM-DD" :placeholder="t('contracts.deliveryDate')" style="width: 190px; margin-left: 12px" />
          <el-date-picker v-model="directForm.receivableDueDate" type="date" value-format="YYYY-MM-DD" clearable :placeholder="t('contracts.receivableDue')" style="width: 190px; margin-left: 12px" />
        </el-form-item>
        <el-form-item :label="t('contracts.commercialTerms')">
          <el-select v-model="directForm.incoterm" filterable allow-create default-first-option style="width: 150px" :placeholder="t('contracts.incoterm')">
            <el-option v-for="i in INCOTERMS" :key="i" :value="i" :label="i" />
          </el-select>
          <el-select v-model="directForm.paymentMethod" filterable allow-create default-first-option clearable style="width: 240px; margin-left: 12px" :placeholder="t('contracts.payment')">
            <el-option v-for="o in paymentOptions" :key="o.code" :value="o.code" :label="o.label" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('contracts.ports')">
          <el-select
            v-model="directForm.portOfLoading"
            filterable allow-create default-first-option clearable remote
            :remote-method="searchContractPorts"
            style="width: 260px"
            :placeholder="t('contracts.pickOrEnterPol')"
          >
            <el-option v-for="p in contractPorts" :key="`pol-${p.id}`" :value="portContractValue(p)" :label="portOptionLabel(p)" />
          </el-select>
          <el-select
            v-model="directForm.portOfDischarge"
            filterable allow-create default-first-option clearable remote
            :remote-method="searchContractPorts"
            style="width: 260px; margin-left: 12px"
            :placeholder="t('contracts.pickOrEnterPod')"
          >
            <el-option v-for="p in contractPorts" :key="`pod-${p.id}`" :value="portContractValue(p)" :label="portOptionLabel(p)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('contracts.terms')">
          <el-input v-model="directForm.terms" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('contracts.executionState')">
          <el-radio-group v-model="directForm.partiallyExecuted">
            <el-radio-button :value="false">{{ t('contracts.notStarted') }}</el-radio-button>
            <el-radio-button :value="true">{{ t('contracts.partiallyExecuted') }}</el-radio-button>
          </el-radio-group>
        </el-form-item>
      </el-form>

      <div class="side-title">
        {{ t('contracts.lines') }}
        <span class="hint">{{ t('contracts.linesHint') }}</span>
        <el-button link type="primary" @click="addDirectLine">{{ t('contracts.addLine') }}</el-button>
      </div>
      <el-table :data="directForm.items" size="small" border max-height="440">
        <el-table-column :label="t('contracts.product')" min-width="210">
          <template #default="{ row }">
            <el-select
              v-model="row.productId"
              filterable
              allow-create
              default-first-option
              clearable
              remote
              :remote-method="searchProducts"
              style="width: 100%"
              :placeholder="t('contracts.pickOrEnterProduct')"
              @change="syncDirectProductUnit(row)"
            >
              <el-option
                v-for="p in products"
                :key="p.id"
                :value="Number(p.id)"
                :label="`${p.code} · ${p.name}`"
              />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column :label="t('contracts.uom')" width="105">
          <template #default="{ row }">
            <el-input
              v-model="row.uomCode"
              size="small"
              :disabled="isCatalogDirectProduct(row)"
              :placeholder="t('contracts.uomHint')"
            />
          </template>
        </el-table-column>
        <el-table-column :label="t('contracts.spec')" min-width="150">
          <template #default="{ row }"><el-input v-model="row.spec" size="small" /></template>
        </el-table-column>
        <el-table-column :label="t('contracts.qty')" width="120">
          <template #default="{ row }"><el-input v-model="row.qty" size="small" /></template>
        </el-table-column>
        <el-table-column :label="t('contracts.unitPrice')" width="120">
          <template #default="{ row }"><el-input v-model="row.unitPrice" size="small" /></template>
        </el-table-column>
        <el-table-column :label="t('contracts.amount')" width="120" align="right">
          <template #default="{ row }">
            <span class="num">{{ lineAmount(row) }}</span>
          </template>
        </el-table-column>
        <template v-if="directForm.partiallyExecuted"><el-table-column label="已落实采购数量" width="125"><template #default="{row}"><el-input v-model="row.openingProcuredQty" size="small"/></template></el-table-column>
          <el-table-column :label="t('contracts.openingArrived')" width="125">
            <template #default="{ row }"><el-input v-model="row.openingArrivedQty" size="small" /></template>
          </el-table-column>
          <el-table-column :label="t('contracts.openingShipped')" width="125">
            <template #default="{ row }"><el-input v-model="row.openingShippedQty" size="small" /></template>
          </el-table-column>
        </template>
        <el-table-column width="60">
          <template #default="{ $index }">
            <el-button link type="danger" @click="directForm.items.splice($index, 1)">
              {{ common('delete') }}
            </el-button>
          </template>
        </el-table-column>
        <template #empty>{{ t('contracts.noLines') }}</template>
      </el-table>
      <div class="total-row">
        {{ t('contracts.total') }}<span class="num money">{{ directTotal }} {{ directForm.currency }}</span>
      </div>
      <el-form label-width="120px" class="head-form direct-tail">
        <el-form-item v-if="directForm.partiallyExecuted" :label="t('contracts.openingReceived')">
          <el-input v-model="directForm.openingReceivedAmount" inputmode="decimal" style="width: 240px">
            <template #prepend>{{ directForm.currency }}</template>
          </el-input>
        </el-form-item>
        <el-form-item :label="t('contracts.contractFile')">
          <input ref="directFileInput" type="file" accept="application/pdf,.pdf" @change="pickDirectFile" />
          <span class="hint">保存前上传已签署文件</span>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="directOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="createDirect">{{ t('contracts.saveAndExecute') }}</el-button>
      </template>
    </el-dialog>

    <!-- Fast correction for a signed contract imported from outside ERP.
         Financial lines and execution openings intentionally stay read-only. -->
    <el-dialog v-model="existingEditOpen" :title="t('contracts.editExisting')" width="min(820px,94vw)">
      <el-alert :title="t('contracts.editExistingHint')" type="info" :closable="false" show-icon class="alert" />
      <el-form label-width="125px">
        <el-form-item :label="t('contracts.externalContractNo')">
          <el-input v-model="existingEditForm.externalContractNo" clearable :placeholder="t('contracts.externalContractNoHint')" />
        </el-form-item>
        <el-form-item :label="t('contracts.contractDates')" required>
          <el-date-picker v-model="existingEditForm.signedDate" type="date" value-format="YYYY-MM-DD" :placeholder="t('contracts.signedDate')" style="width: 200px" />
          <el-date-picker v-model="existingEditForm.effectiveDate" type="date" value-format="YYYY-MM-DD" :placeholder="t('contracts.effectiveDate')" style="width: 200px; margin-left: 12px" />
          <el-date-picker v-model="existingEditForm.deliveryDate" type="date" value-format="YYYY-MM-DD" clearable :placeholder="t('contracts.deliveryDate')" style="width: 200px; margin-left: 12px" />
        </el-form-item>
        <el-form-item :label="t('contracts.receivableDue')">
          <el-date-picker v-model="existingEditForm.receivableDueDate" type="date" value-format="YYYY-MM-DD" clearable style="width: 200px" />
        </el-form-item>
        <el-form-item :label="t('contracts.commercialTerms')">
          <el-select v-model="existingEditForm.incoterm" filterable allow-create default-first-option style="width: 180px">
            <el-option v-for="i in INCOTERMS" :key="i" :value="i" :label="i" />
          </el-select>
          <el-select v-model="existingEditForm.paymentMethod" filterable allow-create default-first-option clearable style="width: 260px; margin-left: 12px">
            <el-option v-for="o in paymentOptions" :key="o.code" :value="o.code" :label="o.label" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('contracts.ports')">
          <el-select v-model="existingEditForm.portOfLoading" filterable allow-create default-first-option clearable remote :remote-method="searchContractPorts" style="width: 290px" :placeholder="t('contracts.pickOrEnterPol')">
            <el-option v-for="p in contractPorts" :key="`edit-pol-${p.id}`" :value="portContractValue(p)" :label="portOptionLabel(p)" />
          </el-select>
          <el-select v-model="existingEditForm.portOfDischarge" filterable allow-create default-first-option clearable remote :remote-method="searchContractPorts" style="width: 290px; margin-left: 12px" :placeholder="t('contracts.pickOrEnterPod')">
            <el-option v-for="p in contractPorts" :key="`edit-pod-${p.id}`" :value="portContractValue(p)" :label="portOptionLabel(p)" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('contracts.terms')">
          <el-input v-model="existingEditForm.terms" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="existingEditOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveExistingEdit">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>



    <!-- Detail: terms, lines, version history -->
    <el-drawer v-model="detailOpen" size="min(1040px,100vw)" :title="detail?.contract.contractNo ?? ''">
      <div v-if="detail" v-loading="loadingDetail" class="contract-detail">
        <div class="detail-head">
          <div>
            <el-tag :type="statusType(detail.contract.status)">
              {{ contractStatusLabel(detail.contract.status) }}
            </el-tag>
            <span class="version-chip">
              v{{ detail.version.versionNo }} · {{ t(`contracts.versionStatuses.${detail.version.status}`) }}
            </span>
            <span v-if="isInForce" class="in-force">{{ t('contracts.inForce') }}</span>
          </div>
          <div class="detail-actions">
            <template v-if="myConfirmationTask"><el-button type="success" @click="confirmContract('APPROVE')">同意</el-button><el-button type="warning" @click="confirmContract('RETURN')">退回修改</el-button></template>
            <el-button v-if="canTransfer" size="small" plain @click="openTransfer">
              {{ t('ownership.transfer') }}
            </el-button>
            <template v-if="canWrite && isMine">
            <template v-if="['EXECUTING','EFFECTIVE'].includes(detail.contract.status)"><el-button @click="detail.contract.entrySource==='EXISTING_CONTRACT'?openExistingEdit(detail.contract.id):openSupplement()">补充信息</el-button></template>
            <el-button v-if="editable" size="small" @click="openTerms">{{ t('contracts.editDraft') }}</el-button>
            <el-button v-if="editable" size="small" type="primary" @click="submit(detail.contract)">
              {{ '提交上级确认' }}
            </el-button>
            <el-tooltip
              v-if="detail.contract.status === 'PENDING_SIGN'"
              :disabled="hasSignedCopy"
              :content="t('contracts.signNeedsCopy')"
              placement="bottom"
            >
              <span>
                <el-button size="small" type="success" :disabled="!hasSignedCopy" @click="sign(detail.contract)">
                  开始执行
                </el-button>
              </span>
            </el-tooltip>

            </template>
            <span v-else-if="canWrite" class="not-mine">{{ t('contracts.notOwner') }}</span>
          </div>
        </div>

        <el-descriptions :column="2" border size="small" class="desc">
          <el-descriptions-item label="客户联系人">{{acceptedOffer?.contact||'—'}}</el-descriptions-item>
          <el-descriptions-item label="原合同号">{{detail.contract.externalContractNo||'—'}}</el-descriptions-item>
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
          <el-descriptions-item :label="t('contracts.receivableDue')">
            <span :class="{ missing: !detail.contract.receivableDueDate }">
              {{ detail.contract.receivableDueDate || t('contracts.notSet') }}
            </span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('contracts.deliveryDate')">
            <span :class="{ missing: !detail.version.deliveryDate }">{{ detail.version.deliveryDate || t('contracts.notSet') }}</span>
          </el-descriptions-item>
          <el-descriptions-item :label="t('contracts.fromQuote')">{{ detail.contract.quoteNo || '—' }}</el-descriptions-item>
          <el-descriptions-item :label="t('contracts.owner')">{{ detail.contract.salesEmployee || '—' }}</el-descriptions-item>
          <el-descriptions-item v-if="detail.contract.entrySource === 'EXISTING_CONTRACT'" :label="t('contracts.externalContractNo')">
            {{ detail.contract.externalContractNo || '—' }}
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.contract.entrySource === 'EXISTING_CONTRACT'" :label="t('contracts.contractSource')">
            <el-tag size="small" type="warning" effect="plain">{{ t('contracts.existingContract') }}</el-tag>
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.contract.signatureSource" :label="t('contracts.signedVia')">
            <el-tag size="small" :type="detail.contract.signatureSource === 'PLATFORM' ? 'success' : 'warning'" effect="plain">
              {{ t(`contracts.fileSources.${detail.contract.signatureSource}`) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.contract.status === 'EXECUTING'" :label="t('contracts.conditionConfirmation')">
            <el-tag :type="detail.contract.conditionConfirmedAt ? 'success' : 'warning'" effect="light">
              {{ detail.contract.conditionConfirmedAt ? t('contracts.conditionReady') : t('contracts.conditionWaiting') }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item :label="t('contracts.terms')" :span="2">
            <div class="terms">{{ detail.version.terms || '—' }}</div>
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.version.changeReason" :label="t('contracts.changeReason')" :span="2">
            {{ detail.version.changeReason }}
          </el-descriptions-item>
        </el-descriptions>

        <details class="detail-section" open>
          <summary class="detail-section-summary">
            <span class="section-summary-title">{{ t('contracts.items') }}</span>
            <span class="section-summary-meta">{{ detail.items.length }} 项 · {{ detail.version.totalAmount }} {{ detail.version.currency }}</span>
          </summary>
        <div class="detail-section-body">
        <el-table max-height="440" :data="detail.items" size="small">
          <el-table-column prop="lineNo" label="#" width="45" />
          <el-table-column :label="t('contracts.product')" min-width="200">
            <template #default="{ row }">
              <div class="product-name">{{ row.productName }}</div>
              <div class="product-spec">{{ row.productCode }}<template v-if="row.spec"> · {{ row.spec }}</template></div>
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
          <template v-if="detail.contract.entrySource === 'EXISTING_CONTRACT'">
            <el-table-column :label="t('contracts.takeoverProgress')" min-width="360">
              <template #default="{ row }">
                <div class="sub">
                  {{ t('contracts.completed') }}：
                  {{ t('contracts.openingProcured') }} {{ trimZeros(row.openingProcuredQty) }} /
                  {{ t('contracts.openingArrived') }} {{ trimZeros(row.openingArrivedQty) }} /
                  {{ t('contracts.openingShipped') }} {{ trimZeros(row.openingShippedQty) }} {{ row.uomCode }}
                </div>
                <div>
                  {{ t('contracts.remaining') }}：
                  {{ t('contracts.pendingPurchase') }} {{ remainingQty(row.qty, row.openingProcuredQty) }} /
                  {{ t('contracts.pendingArrival') }} {{ remainingQty(row.openingProcuredQty, row.openingArrivedQty) }} /
                  {{ t('contracts.pendingShipment') }} {{ remainingQty(row.qty, row.openingShippedQty) }} {{ row.uomCode }}
                </div>
              </template>
            </el-table-column>
          </template>
        </el-table>
        <div class="totals">
          <span class="total-label">{{ t('contracts.total') }}</span>
          <strong class="total-value">{{ detail.version.totalAmount }} {{ detail.version.currency }}</strong>
          <span class="sub">≈ {{ detail.version.baseAmount }} {{ detail.version.fx.baseCurrency }}</span>
        </div>
        <div v-if="detail.contract.entrySource === 'EXISTING_CONTRACT'" class="totals opening-money">
          <span>{{ t('contracts.openingReceived') }}</span>
          <strong>{{ moneyValue(detail.contract.openingReceivedAmount) }} {{ detail.version.currency }}</strong>
          <span>{{ t('contracts.remainingReceivable') }}</span>
          <strong>{{ remainingMoney(detail.version.totalAmount, detail.contract.openingReceivedAmount) }} {{ detail.version.currency }}</strong>
        </div>
        <el-alert
          v-if="detail.contract.entrySource === 'EXISTING_CONTRACT' && detail.contract.filePending"
          :title="t('contracts.filePendingWarning')" type="warning" :closable="false" show-icon class="alert"
        />
        <div class="snapshot">
          {{ t('contracts.fxSnapshot') }}:
          1 {{ detail.version.fx.baseCurrency }} = {{ detail.version.fx.rate }} {{ detail.version.currency }}
          · {{ detail.version.fx.source }} · {{ formatTime(detail.version.fx.rateAt) }}
          <!-- A directly written contract took today's rate; only one that
               grew out of a quotation inherited it. Saying "inherited from
               the quotation" on a contract that never had one is the kind of
               small lie that makes people stop trusting the rest. -->
          <span class="hint">
            创建合同后保留当时的有效汇率
          </span>
        </div>
        </div>
        </details>

        <details v-if="acceptedOffer?.transports.length" class="detail-section" open>
          <summary class="detail-section-summary">
            <span class="section-summary-title">客户运输方案</span>
            <span class="section-summary-meta">{{ acceptedOffer.transports.length }} 个方案 · 查看客户选择及对应货物</span>
          </summary>
          <div class="detail-section-body">
          <el-table :data="acceptedOffer.transports" max-height="300" stripe>
            <el-table-column prop="title" label="方案" min-width="120"/>
            <el-table-column label="对客费用" min-width="150"><template #default="{row}">{{row.currency}} {{row.price||'—'}}</template></el-table-column>
            <el-table-column label="客户选择" width="110"><template #default="{row}"><el-tag :type="row.accepted?'success':'info'" effect="light">{{row.accepted?'已接受':'未选候选'}}</el-tag></template></el-table-column>
            <el-table-column label="对应产品数量" min-width="220"><template #default="{row}"><span class="cargo-summary">{{allocatedProducts(row.quantities)||'—'}}</span></template></el-table-column>
          </el-table>
          </div>
        </details>

        <details class="detail-section">
          <summary class="detail-section-summary">
            <span class="section-summary-title">{{ t('contracts.files') }}</span>
            <span class="section-summary-meta">{{ files.length }} 个文件 · 保存合同拟稿、签署件及补充资料</span>
          </summary>
          <div class="detail-section-body">
        <div v-if="canWrite && isMine && detail.contract.status!=='COMPLETED'" class="upload-bar">
          <el-select v-model="uploadKind" style="width: 150px">
            <el-option v-for="k in FILE_KINDS" :key="k" :value="k" :label="t(`contracts.fileKinds.${k}`)" />
          </el-select>
          <input ref="fileInput" type="file" hidden @change="upload" />
          <el-button :loading="uploading" @click="fileInput?.click()">{{ t('contracts.upload') }}</el-button>
          <span class="hint">签署件归属当前合同；开始执行后保留签署记录</span>
        </div>
        <el-table :data="files" size="small" max-height="320" stripe>
          <el-table-column :label="t('contracts.fileKind')" width="170">
            <template #default="{ row }">
              <el-tag size="small" :type="row.kind === 'SIGNED' ? 'success' : 'info'" effect="plain">
                {{ t(`contracts.fileKinds.${row.kind}`) }}
              </el-tag>
              <!-- A scan somebody uploaded and an envelope the platform sent
                   back are not equal evidence, so they never look equal. -->
              <el-tag
                v-if="row.kind === 'SIGNED'"
                size="small"
                class="src-tag"
                :type="row.source === 'PLATFORM' ? 'success' : 'warning'"
                effect="dark"
              >
                {{ t(`contracts.fileSources.${row.source || 'MANUAL'}`) }}
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
          <el-table-column v-if="canWrite && isMine && detail.contract.status!=='COMPLETED'" width="70">
            <template #default="{ row }">
              <el-button v-if="row.kind!=='SIGNED'||!detail.contract.currentVersionId||detail.contract.currentVersionId==='0'" link type="danger" @click="removeFile(row)">{{ t('common.delete') }}</el-button>
            </template>
          </el-table-column>
          <template #empty>{{ t('contracts.noFiles') }}</template>
        </el-table>
          </div>
        </details>

        <template v-if="transfers.length">
          <details class="detail-section">
            <summary class="detail-section-summary">
              <span class="section-summary-title">{{ t('ownership.history') }}</span>
              <span class="section-summary-meta">{{ transfers.length }} 条 · {{ t('ownership.historyHint') }}</span>
            </summary>
            <div class="detail-section-body">
          <el-table :data="transfers" size="small">
            <el-table-column :label="t('ownership.at')" width="130">
              <template #default="{ row }">{{ formatTime(row.transferredAt) }}</template>
            </el-table-column>
            <el-table-column :label="t('ownership.from')" width="100">
              <template #default="{ row }">{{ row.fromEmployee }}</template>
            </el-table-column>
            <el-table-column :label="t('ownership.to')" width="100">
              <template #default="{ row }">{{ row.toEmployee }}</template>
            </el-table-column>
            <el-table-column :label="t('ownership.by')" width="110">
              <template #default="{ row }">{{ row.transferredByName }}</template>
            </el-table-column>
            <el-table-column :label="t('ownership.reason')" min-width="180">
              <template #default="{ row }"><span class="sub">{{ row.reason || '—' }}</span></template>
            </el-table-column>
          </el-table>
            </div>
          </details>
        </template>

        <!-- What has physically left the warehouse. Kept beside the contract
             rather than inside it: an approved version is frozen because it
             records what was agreed, and shipping keeps moving afterwards. -->
        <template v-if="detail.shipments?.length">
          <details class="detail-section">
            <summary class="detail-section-summary">
              <span class="section-summary-title">{{ t('contracts.shipping') }}</span>
              <span class="section-summary-meta">{{ detail.shipments.length }} 项 · 查看各产品已发货和待发货数量</span>
            </summary>
            <div class="detail-section-body">
          <el-alert v-if="overShipped" type="warning" :closable="false" show-icon class="alert">
            {{ t('contracts.overShipped') }}
          </el-alert>
          <el-table :data="detail.shipments" size="small" max-height="360">
            <el-table-column :label="t('contracts.product')" min-width="200">
              <template #default="{ row }">{{ row.productName }}</template>
            </el-table-column>
            <el-table-column :label="t('contracts.sold')" width="120" align="right">
              <template #default="{ row }"><span class="num">{{ trimQty(row.qty) }}</span></template>
            </el-table-column>
            <el-table-column :label="t('contracts.shipped')" width="120" align="right">
              <template #default="{ row }">
                <span class="num" :class="{ dim: Number(row.shippedQty) === 0 }">
                  {{ trimQty(row.shippedQty) }}
                </span>
              </template>
            </el-table-column>
            <el-table-column :label="t('contracts.toShip')" width="140" align="right">
              <template #default="{ row }">
                <span class="num" :class="remainClass(row.remainingQty)">
                  {{ trimQty(row.remainingQty) }}
                </span>
                <div v-if="Number(row.remainingQty) < 0" class="sub over">
                  {{ t('contracts.overShippedBy', { n: trimQty(String(-Number(row.remainingQty))) }) }}
                </div>
              </template>
            </el-table-column>
          </el-table>
            </div>
          </details>
        </template>

        <!-- Which boat the goods are on. "Where is my customer's order" is the
             most-asked question on an export desk, and the answer lives on a
             different document, so it is fetched and shown here rather than
             leaving sales to ring the forwarder. -->
        <template v-if="vessels.length">
          <details class="detail-section">
            <summary class="detail-section-summary">
              <span class="section-summary-title">{{ t('contracts.vessels') }}</span>
              <span class="section-summary-meta">{{ vessels.length }} 条船期</span>
            </summary>
            <div class="detail-section-body">
          <el-table :data="vessels" size="small">
            <el-table-column :label="t('contracts.vessel')" min-width="190">
              <template #default="{ row }">
                <div>{{ row.vesselName || '—' }}</div>
                <div class="sub">{{ row.shipmentNo }}{{ row.voyageNo ? ` · ${row.voyageNo}` : '' }}</div>
              </template>
            </el-table-column>
            <el-table-column :label="t('contracts.blNo')" min-width="150">
              <template #default="{ row }">
                <div>{{ row.blNo || '—' }}</div>
                <div class="sub">{{ row.portOfDischarge }}</div>
              </template>
            </el-table-column>
            <el-table-column :label="t('contracts.schedule')" width="180">
              <template #default="{ row }">{{ row.etd || '—' }} → {{ row.eta || '—' }}</template>
            </el-table-column>
            <el-table-column :label="t('contracts.shipped')" width="110" align="right">
              <template #default="{ row }"><span class="num">{{ trimQty(row.qty) }}</span></template>
            </el-table-column>
            <el-table-column :label="t('common.status')" width="100">
              <template #default="{ row }">
                <el-tag size="small" effect="plain" :type="row.status === 'ARRIVED' ? 'success' : 'warning'">
                  {{ t(`shipments.statuses.${row.status}`) }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
            </div>
          </details>
        </template>

        <!-- Collection. The salesperson's question is not "did finance file
             it" but "has my customer paid", so the answer belongs on the
             contract, not only in the finance queue. -->
        <template v-if="receiptProgress && auth.can('export:receipt:read')">
          <details class="detail-section">
            <summary class="detail-section-summary">
              <span class="section-summary-title">{{ t('contracts.receipts') }}</span>
              <span class="section-summary-meta">已收 {{ receiptProgress.receivedAmount }} / 合同 {{ receiptProgress.totalAmount }} {{ receiptProgress.currency }}</span>
            </summary>
            <div class="detail-section-body">
          <div class="recv">
            <span>{{ t('contracts.contracted') }} <b class="num">{{ receiptProgress.totalAmount }}</b></span>
            <span>{{ t('contracts.received') }} <b class="num">{{ receiptProgress.receivedAmount }}</b></span>
            <span>
              {{ t('contracts.stillOwed') }}
              <b class="num" :class="Number(receiptProgress.openAmount) > 0 ? 'over' : ''">
                {{ receiptProgress.openAmount }}
              </b>
            </span>
            <span class="sub">{{ receiptProgress.currency }}</span>
          </div>
          <el-table v-if="receipts.length" :data="receipts" size="small">
            <el-table-column :label="t('contracts.paidOn')" width="110">
              <template #default="{ row }">{{ row.valueDate }}</template>
            </el-table-column>
            <el-table-column :label="t('contracts.bankRef')" min-width="150">
              <template #default="{ row }">
                <div>{{ row.bankRef }}</div>
                <div class="sub">{{ row.counterparty }}</div>
              </template>
            </el-table-column>
            <el-table-column :label="t('contracts.paidAmount')" width="120" align="right">
              <template #default="{ row }">
                <span class="num" :class="Number(row.amount) < 0 ? 'over' : ''">{{ row.amount }}</span>
                <div v-if="Number(row.feeAmount) !== 0" class="sub">
                  {{ t('contracts.plusFee', { n: row.feeAmount }) }}
                </div>
              </template>
            </el-table-column>
            <el-table-column :label="t('contracts.paidBy')" width="90">
              <template #default="{ row }"><span class="sub">{{ row.allocatedByName }}</span></template>
            </el-table-column>
          </el-table>
            </div>
          </details>
        </template>

        <details v-if="canSeeApproval && approvals.length" class="detail-section approval-panel">
          <summary class="detail-section-summary">
            <span class="section-summary-title">审批进度</span>
            <span class="section-summary-meta">{{ approvals.length }} 轮 · 上级确认及退回修改意见</span>
          </summary>
          <div class="detail-section-body approval-body">
          <div class="approval-history">
            <div v-for="(inst,i) in approvals" :key="inst.instance.id" class="approval-round">
              <div class="round-head">
                <div class="round-title">
                  <strong>{{ t('contracts.round', {n:i+1}) }}</strong>
                  <el-tag size="small" :type="instanceTagType(inst.instance.status)" effect="light">{{ t(`todos.doc.${inst.instance.status}`) }}</el-tag>
                </div>
                <div class="round-submission">
                  <span>提交人 {{inst.instance.submitterName}}</span>
                  <time>{{formatTime(inst.instance.submittedAt)}}</time>
                </div>
              </div>
              <div v-for="task in inst.tasks" :key="task.id" class="approval-entry" :class="'status-'+task.status.toLowerCase()">
                <span class="approval-marker">{{task.status==='APPROVED'?'✓':task.status==='RETURNED'||task.status==='REJECTED'?'!':'·'}}</span>
                <div class="approval-content">
                  <div class="approval-entry-head">
                    <strong>{{task.nodeName}}</strong>
                    <el-tag size="small" :type="task.status==='APPROVED'?'success':task.status==='RETURNED'||task.status==='REJECTED'?'danger':'info'" effect="light">{{t(`todos.task.${task.status}`)}}</el-tag>
                    <time>{{task.actedAt?formatTime(task.actedAt):'待处理'}}</time>
                  </div>
                  <div class="approval-person">审批人：{{employeeName(task.assigneeId)}}</div>
                  <div v-if="task.comment" class="approval-comment"><span>审批意见</span>{{task.comment}}</div>
                </div>
              </div>
            </div>
          </div>
          </div>
        </details>

        <details class="detail-section">
          <summary class="detail-section-summary">
            <span class="section-summary-title">{{ t('contracts.versions') }}</span>
            <span class="section-summary-meta">{{ detail.versions.length }} 个版本 · 查看已保存的历史合同记录</span>
          </summary>
          <div class="detail-section-body">
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
        </details>
      </div>
    </el-drawer>

    <!-- Edit the working draft: terms and lines -->
    <el-dialog v-model="termsOpen" :title="t('contracts.editDraft')" width="min(860px,94vw)">
      <el-form label-width="110px">
        <div class="grid">
          <el-form-item label="原合同号"><el-input v-model="termsForm.externalContractNo"/></el-form-item>
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
          <el-form-item :label="t('contracts.receivableDue')">
            <el-date-picker v-model="termsForm.receivableDueDate" type="date" value-format="YYYY-MM-DD"
                            clearable :placeholder="t('contracts.receivableDueHint')" style="width: 100%" />
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
      <el-table :data="termsForm.items" size="small" max-height="440">
        <el-table-column :label="t('contracts.product')" min-width="220">
          <template #default="{ row }">
            <el-autocomplete
              v-model="row.productName"
              :fetch-suggestions="suggestContractProducts"
              placeholder="输入产品名称，或搜索已有产品"
              style="width:100%"
              @input="row.productId='0'"
              @select="selectContractProduct(row,$event)"
            />
          </template>
        </el-table-column>
        <el-table-column :label="t('contracts.spec')" width="150">
          <template #default="{ row }"><el-input v-model="row.spec" /></template>
        </el-table-column>
        <el-table-column :label="t('contracts.qty')" width="110">
          <template #default="{ row }"><el-input v-model="row.qty" /></template>
        </el-table-column>
        <el-table-column label="单位" width="100"><template #default="{row}"><el-input v-model="row.uomCode"/></template></el-table-column>
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
    <el-dialog v-model="changeOpen" :title="t('contracts.change')" width="min(820px,94vw)">
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
            <el-select
              v-model="row.productId"
              filterable
              clearable
              style="width: 100%"
              :placeholder="t('contracts.pickProduct')"
            >
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

    <!-- Hand the deal over. The dialog names both documents on purpose: the
         quotation moves with the contract, and the person doing it should
         see that before they confirm rather than discover it afterwards. -->
    <el-dialog v-model="transferOpen" :title="t('ownership.transfer')" width="520">
      <el-alert type="warning" :closable="false" show-icon class="transfer-note">
        {{ t('ownership.warning') }}
      </el-alert>
      <el-form label-width="110px" class="transfer-form">
        <el-form-item :label="t('ownership.moving')">
          <div>
            <div>{{ detail?.contract.contractNo }}</div>
            <div v-if="detail?.contract.quoteNo" class="sub">{{ detail.contract.quoteNo }}</div>
          </div>
        </el-form-item>
        <el-form-item :label="t('ownership.currentOwner')">
          {{ detail?.contract.salesEmployee || '—' }}
        </el-form-item>
        <el-form-item :label="t('ownership.to')">
          <el-select v-model="transferForm.toEmployeeId" filterable style="width: 100%">
            <el-option
              v-for="e in transferTargets"
              :key="e.id"
              :value="e.id"
              :label="e.name"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('ownership.reason')">
          <el-input v-model="transferForm.reason" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="transferOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="saveTransfer">{{ t('ownership.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import {contractItemPayload} from '../lib/contractItem'
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { del, get, post, put } from '../api'
import { newIdempotencySession, withIdempotency } from '../lib/idempotency'

// 防重键：合同重复一张不是删一行的事——它会往下游派生采购需求。
const createIdem = newIdempotencySession()
import { CURRENCIES } from '../constants'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'

interface Fx { rate: string; rateAt: string; source: string; baseCurrency: string }
interface Contract {
 updatedAt:string
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
  salesEmployeeId: string
  signatureSource: string
  signedAt: string
  effectiveAt: string
  conditionConfirmedAt: string
  conditionConfirmationNote: string
  conditionConfirmedByName: string
  receivableDueDate: string
  externalContractNo: string
  entrySource: string
  openingReceivedAmount: string
  filePending: boolean
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
  openingProcuredQty: string
  openingArrivedQty: string
  openingShippedQty: string
}
interface ReceiptProgress {
  currency: string
  totalAmount: string
  receivedAmount: string
  openAmount: string
}
interface ContractReceipt {
  allocationId: string
  bankRef: string
  valueDate: string
  counterparty: string
  amount: string
  feeAmount: string
  allocatedByName: string
}
interface Vessel {
  shipmentNo: string
  vesselName: string
  voyageNo: string
  blNo: string
  portOfDischarge: string
  etd: string
  eta: string
  status: string
  qty: string
}
interface Shipment {
  productId: string
  productName: string
  uomCode: string
  qty: string
  shippedQty: string
  remainingQty: string
}
interface Detail {
 acceptedOfferJson?:string
  contract: Contract
  version: Version
  items: Item[]
  versions: VersionRow[]
  shipments: Shipment[]
}
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
  source: string
}
interface Transfer {
  id: string
  bizType: string
  bizNo: string
  fromEmployee: string
  toEmployee: string
  reason: string
  transferredByName: string
  transferredAt: string
}
interface Quote { id: string; quoteNo: string; customerName: string; currency: string; totalAmount: string }
interface Product { id: string; code: string; name: string; uomCode?: string }
interface ContractPort { id: string; unlocode: string; nameZh: string; nameEn: string; countryCode: string }
interface OptionItem { code: string; label: string }
interface ChangeLine { productName?:string;uomCode?:string; productId: string; spec: string; qty: string; unitPrice: string }

const STATUSES = ['PENDING_APPROVAL', 'PENDING_SIGN', 'EXECUTING', 'COMPLETED']
const ownerFilter=ref('')
const filterOwners=ref<{id:string;name:string}[]>([])
function contractStatusLabel(s:string){return t(`contracts.statuses.${s}`)}
const INCOTERMS = ['FOB', 'CIF', 'CFR', 'EXW', 'DDP']
// DRAFT is what we sent out, SIGNED is what came back with a signature on it.
const FILE_KINDS = ['DRAFT', 'SIGNED', 'OTHER']

const { t, locale } = useI18n()
// 直接下单那张明细表上的「删除」用的就是它，而这一行一直没定义——那颗按钮
// 在浏览器里会当场炸掉。类型检查一开就抓住了，说明那个弹窗少有人点到底。
const common = (k: string) => t(`common.${k}`)
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = auth.can('export:contract:write')

const contracts = ref<Contract[]>([])
const products = ref<Product[]>([])
const paymentOptions = ref<OptionItem[]>([])
const detail = ref<Detail | null>(null)

// Shipping more than the contract now says was sold means the contract was
// reduced after goods had already left. No rule gets that right on its own,
// so it is shown rather than clamped to zero and forgotten.
const overShipped = computed(() =>
  (detail.value?.shipments ?? []).some((s) => Number(s.remainingQty) < 0),
)

function remainClass(v: string): string {
  const n = Number(v)
  if (n < 0) return 'over'
  if (n === 0) return 'done'
  return ''
}

// Quantities arrive as exact decimals; "1500.0000 PCS" reads worse than
// "1500 PCS" and means the same thing.
function trimQty(v: string): string {
  if (!v) return '0'
  if (!v.includes('.')) return v
  return v.replace(/0+$/, '').replace(/\.$/, '')
}
const total = ref(0)
const page = ref(1)
const pageSize = 10
const keyword = ref('')
const status = ref('')
const loading = ref(false)
const loadingDetail = ref(false)
const saving = ref(false)
const detailOpen = ref(false)
const directOpen = ref(false)
const existingEditOpen = ref(false)
const customers = ref<{ id: string; code: string; name: string }[]>([])
const contractPorts = ref<ContractPort[]>([])
const canReadPorts = auth.can('masterdata:port:read')

// A number means a selected Product Master row; a string means the exact
// product wording typed from the paper contract.  Keeping those types
// distinct also lets a legitimate product name such as "304" stay manual.
interface DirectLine {
  productId?: number | string
  catalogProductId?: number
  uomCode: string
  spec: string
  qty: string
  unitPrice: string
  openingProcuredQty: string
  openingArrivedQty: string
  openingShippedQty: string
}
const directForm = reactive({
  customerId: undefined as number | undefined,
  salesEmployeeId: undefined as number | undefined,
  procurementEmployeeId: undefined as number | undefined,
  supplierId: undefined as number | undefined,
  externalContractNo: '',
  currency: 'USD',
  signedDate: '',
  effectiveDate: '',
  deliveryDate: '',
  // 应收到期日：这份合同的钱什么时候该收回来。**员工填**，可留空。
  // 不再从客户主数据的账期推——同一个客户这一单谈 60 天、下一单要求预付，
  // 都是常事。
  receivableDueDate: '',
  incoterm: 'FOB',
  paymentMethod: '',
  portOfLoading: '',
  portOfDischarge: '',
  terms: '',
  partiallyExecuted: false,
  openingReceivedAmount: '0',
  filePending: false,
  items: [] as DirectLine[],
})
const existingEditForm = reactive({
  id: '', externalContractNo: '', signedDate: '', effectiveDate: '', deliveryDate: '', receivableDueDate: '',
  buyerName: '', buyerAddress: '', sellerName: '', sellerAddress: '',
  incoterm: '', paymentMethod: '', portOfLoading: '', portOfDischarge: '', terms: '',
})
const contractOwners = ref<{ id: string; name: string; status: string }[]>([])
const directSuppliers = ref<{ id: string; code: string; name: string; nameZh: string; nameEn: string }[]>([])
const canPickContractOwner = auth.can('iam:employee:read')
const directFileInput = ref<HTMLInputElement | null>(null)
const directFile = ref<File | null>(null)

// Recomputed as the user types. The server prices it again and is the
// authority; this is so nobody signs off a total they have not seen.
const directTotal = computed(() =>
  directForm.items.reduce((sum, r) => sum + Number(lineAmount(r)), 0).toFixed(2),
)

function addDirectLine() {
	directForm.items.push({
		productId: undefined, catalogProductId: undefined, uomCode: '', spec: '', qty: '', unitPrice: '',
    openingProcuredQty: '0', openingArrivedQty: '0', openingShippedQty: '0',
  })
}

async function openDirect() {
  Object.assign(directForm, {
    customerId: undefined, salesEmployeeId: Number(auth.employeeId), externalContractNo: '',
    procurementEmployeeId: undefined, supplierId: undefined,
    currency: 'USD', signedDate: '', effectiveDate: '', deliveryDate: '', receivableDueDate: '', incoterm: 'FOB',
    paymentMethod: '', portOfLoading: '', portOfDischarge: '', terms: '', partiallyExecuted: false,
    openingReceivedAmount: '0', filePending: false, items: [],
  })
  directFile.value = null
  if (directFileInput.value) directFileInput.value.value = ''
  addDirectLine()
  await Promise.all([searchCustomers(''), searchProducts(''), searchContractPorts(''), loadContractOwners(), loadDirectSuppliers()])
  directOpen.value = true
}

function canCorrectExisting(row: Contract): boolean {
  return canWrite && row.entrySource === 'EXISTING_CONTRACT' && row.status !== 'CANCELLED'
}

function businessDate(value: string): string {
  return value ? value.slice(0, 10) : ''
}

async function openExistingEdit(id: string) {
  const data = await get<Detail>(`/contracts/${id}`)
  const c = data.contract
  const v = data.version
  Object.assign(existingEditForm, {
    id: c.id, externalContractNo: c.externalContractNo || '',
    signedDate: businessDate(c.signedAt), effectiveDate: businessDate(c.effectiveAt),
    deliveryDate: v.deliveryDate || '', receivableDueDate: c.receivableDueDate || '',
    buyerName: v.buyerName, buyerAddress: v.buyerAddress, sellerName: v.sellerName, sellerAddress: v.sellerAddress,
    incoterm: v.incoterm, paymentMethod: v.paymentMethod, portOfLoading: v.portOfLoading,
    portOfDischarge: v.portOfDischarge, terms: v.terms,
  })
  await searchContractPorts('')
  existingEditOpen.value = true
}

async function saveExistingEdit() {
  if (!existingEditForm.signedDate || !existingEditForm.effectiveDate) {
    ElMessage.warning(t('contracts.contractDatesRequired'))
    return
  }
  saving.value = true
  try {
    await put(`/contracts/${existingEditForm.id}`, {
      externalContractNo: existingEditForm.externalContractNo.trim(),
      signedDate: existingEditForm.signedDate,
      effectiveDate: existingEditForm.effectiveDate,
      terms: {
        buyerName: existingEditForm.buyerName, buyerAddress: existingEditForm.buyerAddress,
        sellerName: existingEditForm.sellerName, sellerAddress: existingEditForm.sellerAddress,
        incoterm: existingEditForm.incoterm, paymentMethod: existingEditForm.paymentMethod,
        portOfLoading: existingEditForm.portOfLoading, portOfDischarge: existingEditForm.portOfDischarge,
        deliveryDate: existingEditForm.deliveryDate, receivableDueDate: existingEditForm.receivableDueDate,
        terms: existingEditForm.terms,
      },
    })
    ElMessage.success(t('contracts.existingUpdated'))
    existingEditOpen.value = false
    await load()
    if (detailOpen.value && detail.value?.contract.id === existingEditForm.id) await openDetail(existingEditForm.id)
  } finally {
    saving.value = false
  }
}

async function searchCustomers(keyword: string) {
  customers.value = (await get<{ customers: typeof customers.value }>('/customers', { keyword, page_size: 50 })).customers ?? []
}

async function searchProducts(keyword: string) {
  if (!auth.can('product:product:read')) {
    products.value = []
    return
  }
  products.value = (await get<{ products: Product[] }>('/products', { keyword, page_size: 50, status: 'ACTIVE' })).products ?? []
}

async function searchContractPorts(keyword: string) {
  if (!canReadPorts) {
    contractPorts.value = []
    return
  }
  contractPorts.value = (await get<{ ports: ContractPort[] }>('/ports', {
    keyword, page: 1, page_size: 100, status: 'ACTIVE',
  })).ports ?? []
}

function portContractValue(port: ContractPort): string {
  return (locale.value === 'zh' ? port.nameZh || port.nameEn : port.nameEn || port.nameZh) || port.unlocode
}

function portOptionLabel(port: ContractPort): string {
  return [port.unlocode, port.nameZh || port.nameEn, port.countryCode].filter(Boolean).join(' · ')
}

function catalogDirectProduct(row: DirectLine): Product | undefined {
  if (!row.catalogProductId) return undefined
  return products.value.find((p) => Number(p.id) === row.catalogProductId)
}

function isCatalogDirectProduct(row: DirectLine): boolean {
  return Boolean(row.catalogProductId)
}

function syncDirectProductUnit(row: DirectLine) {
  const product = typeof row.productId === 'number'
    ? products.value.find((p) => Number(p.id) === row.productId)
    : undefined
  row.catalogProductId = product ? Number(product.id) : undefined
  row.uomCode = product?.uomCode ?? ''
}

async function loadContractOwners() {
  const mine = { id: String(auth.employeeId), name: auth.employeeName, status: 'ACTIVE' }
  if (!canPickContractOwner) {
    contractOwners.value = [mine]
    return
  }
  const data = await get<{ employees: { id: string; name: string; status: string }[] }>('/employees', { page_size: 200, status: 'ACTIVE' })
  contractOwners.value = data.employees?.length ? data.employees : [mine]
}

async function loadDirectSuppliers() {
  const data = await get<{ suppliers: typeof directSuppliers.value }>('/suppliers', { page_size: 200, status: 'ACTIVE' })
  directSuppliers.value = data.suppliers ?? []
}

function pickDirectFile(event: Event) {
  const input = event.target as HTMLInputElement
  directFile.value = input.files?.[0] ?? null
  if (directFile.value) directForm.filePending = false
}

async function uploadExistingContractFile(contractId: string, versionId: string, file: File) {
  const signed = await post<{ fileKey: string; uploadUrl: string }>(`/contracts/${contractId}/files/presign`, {
    fileName: file.name, contentType: file.type || 'application/pdf',
  })
  const putResult = await fetch(signed.uploadUrl, { method: 'PUT', body: file })
  if (!putResult.ok) throw new Error(`upload failed: ${putResult.status}`)
  await post(`/contracts/${contractId}/files`, {
    fileKey: signed.fileKey, fileName: file.name, contentType: file.type || 'application/pdf',
    sizeBytes: String(file.size), kind: 'SIGNED', contractVersionId: versionId,
  })
}

async function createDirect() {
  if (!directForm.customerId) {
    ElMessage.warning(t('contracts.customerRequired'))
    return
  }
  if (!directForm.salesEmployeeId) {
    ElMessage.warning(t('contracts.ownerRequired'))
    return
  }

  if (!directForm.signedDate || !directForm.effectiveDate) {
    ElMessage.warning(t('contracts.contractDatesRequired'))
    return
  }
  if (!directFile.value) {
    ElMessage.warning(t('contracts.fileOrLaterRequired'))
    return
  }
  const items = directForm.items.filter((r) => r.productId || r.qty || r.unitPrice)
  if (!items.length || items.some((r) => !String(r.productId ?? '').trim() || Number(r.qty) <= 0 || Number(r.unitPrice) < 0)) {
    ElMessage.warning(t('contracts.linesRequired'))
    return
  }
  if (items.some((r) => !r.uomCode.trim())) {
    ElMessage.warning(t('contracts.uomRequired'))
    return
  }
  for (const row of items) {
    const total = Number(row.qty)
    const openings = directForm.partiallyExecuted
      ? [row.openingProcuredQty,row.openingArrivedQty, row.openingShippedQty].map(Number)
      : [0, 0, 0]
    if (openings.some((value) => !Number.isFinite(value) || value < 0 || value > total)) {
      ElMessage.warning(t('contracts.openingQtyInvalid'))
      return
    }
  }
  const openingReceived = directForm.partiallyExecuted ? Number(directForm.openingReceivedAmount || 0) : 0
  if (!Number.isFinite(openingReceived) || openingReceived < 0 || openingReceived > Number(directTotal.value)) {
    ElMessage.warning(t('contracts.openingReceivedInvalid'))
    return
  }
  try {
    await ElMessageBox.confirm(t('contracts.executeConfirm'), t('contracts.createDirect'), { type: 'warning' })
  } catch {
    return
  }
  saving.value = true
  try {
    const sourceFile=directFile.value!
    const signed=await post<{fileKey:string;uploadUrl:string}>('/contracts/existing/files/presign',{fileName:sourceFile.name,contentType:sourceFile.type||'application/pdf'})
    const uploaded=await fetch(signed.uploadUrl,{method:'PUT',headers:{'Content-Type':sourceFile.type||'application/pdf'},body:sourceFile})
    if(!uploaded.ok)throw new Error('签署文件上传失败，请重试')
    const data = await post<Detail>('/contracts/existing', {
      signedFileKey:signed.fileKey,signedFileName:sourceFile.name,
      customerId: directForm.customerId!,
      salesEmployeeId: directForm.salesEmployeeId,
      procurementEmployeeId: directForm.procurementEmployeeId,
      supplierId: directForm.supplierId,
      externalContractNo: directForm.externalContractNo.trim(),
      currency: directForm.currency,
      signedDate: directForm.signedDate,
      effectiveDate: directForm.effectiveDate,
      openingReceivedAmount: String(openingReceived),
      terms: {
        incoterm: directForm.incoterm,
        paymentMethod: directForm.paymentMethod,
        portOfLoading: directForm.portOfLoading,
        portOfDischarge: directForm.portOfDischarge,
        deliveryDate: directForm.deliveryDate,
        receivableDueDate: directForm.receivableDueDate,
        terms: directForm.terms,
      },
      items: items.map((r) => {
        const catalog = catalogDirectProduct(r)
        return {
          productId: r.catalogProductId ? String(r.catalogProductId) : '0',
          productName: catalog ? '' : String(r.productId).trim(),
          uomCode: r.uomCode.trim().toUpperCase(),
          spec: r.spec,
          qty: r.qty,
          unitPrice: r.unitPrice || '0',
          openingProcuredQty: directForm.partiallyExecuted ? r.openingProcuredQty || '0' : '0',
          openingArrivedQty: directForm.partiallyExecuted ? r.openingArrivedQty || '0' : '0',
          openingShippedQty: directForm.partiallyExecuted ? r.openingShippedQty || '0' : '0',
        }
      }),
    }, withIdempotency(createIdem))
    createIdem.reset()
    ElMessage.success(t('contracts.existingImported'))
    directOpen.value = false
    load()
    openDetail(data.contract.id)
  } finally {
    saving.value = false
  }
}
const acceptedOffer=computed(()=>{try{return JSON.parse(detail.value?.acceptedOfferJson||'null') as {contact:string;transports:{quoteId:string;title:string;currency:string;price:string;accepted:boolean;quantities:Record<string,string>}[];lines:{id:string;product:string;unit:string}[]}|null}catch{return null}})
function allocatedProducts(quantities:Record<string,string>){return Object.entries(quantities||{}).map(([id,qty])=>{const line=acceptedOffer.value?.lines.find(l=>l.id===id);return `${line?.product||id}: ${qty} ${line?.unit||''}`}).join('；')}
const myConfirmationTask=ref(''),supplementOpen=ref(false),supplementForm=reactive({externalContractNo:'',due:''})
async function loadMyConfirmation(id:string){
 myConfirmationTask.value='';if(!auth.can('approval:task:act'))return
 let page=1,total=0;do{const data=await get<{todos:{task:{id:string};instance:{bizId:string}}[];meta:{total:string}}>('/approvals/todos',{biz_type:'CONTRACT',page,page_size:100});total=Number(data.meta.total);myConfirmationTask.value=data.todos.find(t=>t.instance.bizId===id)?.task.id||'';page++}while(!myConfirmationTask.value&&(page-1)*100<total)
}
async function confirmContract(action:'APPROVE'|'RETURN'){
 if(!detail.value||!myConfirmationTask.value)return
 let comment='';if(action==='RETURN'){const r=await ElMessageBox.prompt('请说明需要修改的内容','退回修改',{inputValidator:v=>!!v?.trim()||'请填写退回原因'});comment=r.value}else{await ElMessageBox.confirm('同意这份合同，进入双方签署阶段？','上级确认')}
 await post(`/approvals/tasks/${myConfirmationTask.value}/act`,{action,comment});myConfirmationTask.value='';ElMessage.success(action==='RETURN'?'已退回负责销售':'已同意，合同状态正在更新');const id=detail.value.contract.id;for(let i=0;i<8;i++){await openDetail(id);if(detail.value?.contract.status!=='PENDING_APPROVAL')break;await new Promise(r=>setTimeout(r,500))}await load()
}
function openSupplement(){if(!detail.value)return;supplementForm.externalContractNo=detail.value.contract.externalContractNo||'';supplementForm.due=detail.value.contract.receivableDueDate||'';supplementOpen.value=true}
async function saveSupplement(){if(!detail.value)return;await put(`/contracts/${detail.value.contract.id}`,{externalContractNo:supplementForm.externalContractNo,terms:{receivableDueDate:supplementForm.due}});supplementOpen.value=false;await openDetail(detail.value.contract.id);await load()}
const termsOpen = ref(false)
const changeOpen = ref(false)

const termsForm = reactive({
  buyerName: '', buyerAddress: '', sellerName: '', sellerAddress: '',
  incoterm: 'FOB', portOfLoading: '', portOfDischarge: '', paymentMethod: '',
  externalContractNo:'', deliveryDate: '', receivableDueDate: '', terms: '', items: [] as ChangeLine[],
})
const changeForm = reactive({ reason: '', deliveryDate: '', items: [] as ChangeLine[] })
const approvals = ref<ApprovalRound[]>([])
const employees = ref<Record<string, string>>({})
const canSeeApproval = auth.can('export:contract:read')
const canTransfer = auth.can('export:ownership:transfer')
// Being able to open a contract and being able to change it are different
// questions: approvers and supervisors read documents they do not own.
const isMine = computed(() => auth.owns(detail.value?.contract.salesEmployeeId ?? ''))
const transfers = ref<Transfer[]>([])
const vessels = ref<Vessel[]>([])
const receipts = ref<ContractReceipt[]>([])
const receiptProgress = ref<ReceiptProgress | null>(null)
const transferOpen = ref(false)
const transferForm = reactive({ toEmployeeId: '', reason: '' })
const staff = ref<{ id: string; name: string; status: string }[]>([])
// Never offer the person who already holds it, and never offer somebody who
// has left: both are refused by the service, and an option that always
// errors is worse than no option.
const transferTargets = computed(() =>
  staff.value.filter(
    (e) => e.status === 'ACTIVE' && e.id !== detail.value?.contract.salesEmployeeId,
  ),
)
const files = ref<ContractFile[]>([])
// Signing by hand asserts the customer agreed; this is the thing being
// asserted. Scoped to the version on screen, because a scan of v1 is not
// evidence for the v2 that replaced it.
const hasSignedCopy = computed(() =>
  files.value.some((f) => f.kind === 'SIGNED' && f.versionNo === detail.value?.version.versionNo),
)
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
    const data = await get<{ contracts: Contract[]; owners: {id:string;name:string}[]; meta: { total: string } }>('/contracts', {
      page: page.value, page_size: pageSize, keyword: keyword.value, status: status.value, sales_employee_id: ownerFilter.value,
    })
    contracts.value = data.contracts ?? []
    filterOwners.value = data.owners ?? []
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
    await Promise.all([loadMyConfirmation(id), loadFiles(id), loadApprovals(id), loadTransfers(id), loadVessels(id), loadReceipts(id)])
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

async function suggestContractProducts(query:string,done:(rows:(Product & {value:string})[])=>void){
  try{await searchProducts(query);done(products.value.map(p=>({...p,value:p.name})))}catch{done([])}
}
function selectContractProduct(row:ChangeLine,product:Product){
  row.productId=String(product.id);row.productName=product.name
  if(product.uomCode)row.uomCode=product.uomCode
}
function openTerms() {
  const v = detail.value!.version
  Object.assign(termsForm, {
    externalContractNo:detail.value!.contract.externalContractNo||'',
    buyerName: v.buyerName, buyerAddress: v.buyerAddress,
    sellerName: v.sellerName, sellerAddress: v.sellerAddress,
    incoterm: v.incoterm, portOfLoading: v.portOfLoading, portOfDischarge: v.portOfDischarge,
    paymentMethod: v.paymentMethod, deliveryDate: v.deliveryDate, terms: v.terms,
    // 到期日在合同主表上，不在版本上——版本审批后冻结，而到期日要能改。
    receivableDueDate: detail.value!.contract.receivableDueDate || '',
    items: toLines(detail.value!.items),
  })
  termsOpen.value = true
}

async function saveTerms() {
  if (termsForm.items.length === 0) {
    ElMessage.warning(t('contracts.itemsRequired'))
    return
  }
  const { items, externalContractNo, ...terms } = termsForm
  saving.value = true
  try {
    await put(`/contracts/${detail.value!.contract.id}`, { terms, items: items.map(contractItemPayload), externalContractNo })
    ElMessage.success(t('contracts.updated'))
    termsOpen.value = false
    await openDetail(detail.value!.contract.id)
    load()
  } catch {
    // The API interceptor displays the error; retain the form for retry.
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
  const result = await get<{ rounds: ApprovalRound[] }>(`/contracts/${contractId}/approvals`)
  approvals.value = (result.rounds ?? []).sort((a,b)=>Number(a.instance.id)-Number(b.instance.id))
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

// ---------------------------------------------------------------- ownership

// Reading shipments is its own permission. Somebody who lacks it should
// still get the rest of the drawer rather than an empty one, so a refusal
// here is swallowed and the section simply does not appear.
// Reading collection is gated on the contract permission, but the finance
// data can still be absent for a contract with no version; a failure here
// hides the section rather than breaking the drawer.
async function loadReceipts(contractId: string) {
if(!auth.can('export:receipt:read')){receiptProgress.value=null;receipts.value=[];return}
  try {
    const d = await get<{ progress: ReceiptProgress; receipts: ContractReceipt[] }>(
      `/contracts/${contractId}/receipts`,
    )
    receiptProgress.value = d.progress?.currency ? d.progress : null
    receipts.value = d.receipts ?? []
  } catch {
    receiptProgress.value = null
    receipts.value = []
  }
}

async function loadVessels(contractId: string) {
  try {
    vessels.value =
      (await get<{ vessels: Vessel[] }>(`/contracts/${contractId}/vessels`)).vessels ?? []
  } catch {
    vessels.value = []
  }
}

async function loadTransfers(contractId: string) {
  transfers.value =
    (await get<{ transfers: Transfer[] }>('/ownership/transfers', {
      biz_type: 'CONTRACT',
      biz_id: contractId,
    })).transfers ?? []
}

async function openTransfer() {
  transferForm.toEmployeeId = ''
  transferForm.reason = ''
  if (!staff.value.length) {
    staff.value =
      (await get<{ employees: { id: string; name: string; status: string }[] }>('/employees', {
        page_size: 200,
      })).employees ?? []
  }
  transferOpen.value = true
}

async function saveTransfer() {
  if (!detail.value) return
  saving.value = true
  try {
    const resp = await post<{ transfers: Transfer[] }>('/ownership/transfer', {
      biz_type: 'CONTRACT',
      biz_id: detail.value.contract.id,
      to_employee_id: transferForm.toEmployeeId,
      reason: transferForm.reason,
    })
    ElMessage.success(t('ownership.done', { count: resp.transfers?.length ?? 0 }))
    transferOpen.value = false
    // The contract may now be outside the caller's own scope, so re-reading
    // it can legitimately fail; the list is what has to stay truthful.
    await load()
    await openDetail(detail.value.contract.id)
  } finally {
    saving.value = false
  }
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
    productName:i.productName,uomCode:i.uomCode,productId: i.productId, spec: i.spec, qty: trimZeros(i.qty), unitPrice: i.unitPrice,
  }))
}

// Preview only. The server recomputes every amount and its numbers are the
// ones stored; this just keeps the editor oriented while typing.
// Used by both the change form and the direct-creation form: neither cares
// about anything but the two numbers.
function lineAmount(row: { qty: string; unitPrice: string }): string {
  const qty = Number(row.qty)
  const price = Number(row.unitPrice)
  if (!Number.isFinite(qty) || !Number.isFinite(price)) return '—'
  return (Math.round(qty * price * 100) / 100).toFixed(2)
}

function remainingQty(total: string, completed: string): string {
  const left = Math.max(0, Number(total || 0) - Number(completed || 0))
  return trimZeros(left.toFixed(4))
}

function moneyValue(value: string): string {
  const number = Number(value || 0)
  return Number.isFinite(number) ? number.toFixed(2) : '0.00'
}

function remainingMoney(total: string, received: string): string {
  return Math.max(0, Number(total || 0) - Number(received || 0)).toFixed(2)
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
      items: changeForm.items.map(contractItemPayload),
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
 try {
  await ElMessageBox.confirm('确认双方已签署且签署合同已上传，开始执行后进入财务入账。采购和物流仍须等待财务确认执行条件。','开始执行',{confirmButtonText:'开始执行',cancelButtonText:'返回检查'})
  await post(`/contracts/${row.id}/sign`, {})
  ElMessage.success('合同已开始执行')
  if (detailOpen.value) await openDetail(row.id)
  await load()
 } catch { /* Keep the contract open for correction or retry. */ }
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
  await loadContractOwners()
  load()
  // Approval reads the contract's product snapshot; catalog access is optional.
  if (auth.can('product:product:read')) {
    products.value = (await get<{ products: Product[] }>('/products', { page_size: 200 })).products ?? []
  }
  paymentOptions.value = (await get<{ options: OptionItem[] }>('/options', { category: 'PAYMENT_METHOD' })).options ?? []
  // Names for the approval timeline. Skipped when the user cannot read the
  // directory: the timeline then shows ids, which is worse than names but
  // better than hiding who a contract is waiting on.
  if (auth.can('iam:employee:read')) {
    const staff = await get<{ employees: { id: string; name: string }[] }>('/employees', { page_size: 200 })
    employees.value = Object.fromEntries((staff.employees ?? []).map((e) => [e.id, e.name]))
  }
})

// An approver acted on something this person submitted. Only doc.changed is
// watched: todo.changed means their own approval queue moved, which this page
// says nothing about, and reloading on it would be noise.
//
// The re-read goes through the normal API rather than trusting the event's
// contents, so the data scope is applied exactly as it is everywhere else.
const stopListening = onLive((event) => {
  if (event.type !== 'doc.changed') return
  load()
  // Leave the drawer alone while a form is open over it: swapping the values
  // underneath somebody who is halfway through editing is worse than making
  // them close the dialog to see the update.
  const editing = termsOpen.value || changeOpen.value || transferOpen.value
  const current = detail.value?.contract.id
  if (detailOpen.value && current && !editing && event.subject === `CONTRACT:${current}`) {
    openDetail(current)
  }
})
onUnmounted(stopListening)
</script>

<style scoped>
.contract-detail{
  --detail-title:#173f54;
  --detail-text:#2d4554;
  --detail-muted:#718391;
  color:var(--detail-text);
  font-size:14px;
  line-height:1.55;
}
.contract-detail :deep(.el-descriptions__label.el-descriptions__cell){
  width:132px;
  background:#f5f8fa;
  color:#526876;
  font-size:13px;
  font-weight:500;
}
.contract-detail :deep(.el-descriptions__content.el-descriptions__cell){
  color:var(--detail-text);
  font-size:14px;
  font-weight:400;
}
.contract-detail :deep(.el-table th.el-table__cell){
  background:#f6f9fb;
  color:#506775;
  font-size:13px;
  font-weight:600;
}
.contract-detail :deep(.el-table td.el-table__cell){
  color:#334e5e;
  font-size:14px;
}
.contract-detail :deep(.el-divider__text){
  color:var(--detail-title);
  font-size:16px;
  font-weight:600;
}
.product-name{color:#203f51;font-size:14px;font-weight:500}
.product-spec{
  margin-top:4px;
  max-width:520px;
  color:var(--detail-muted);
  font-size:13px;
  font-weight:400;
  line-height:1.6;
  display:-webkit-box;
  -webkit-line-clamp:3;
  -webkit-box-orient:vertical;
  overflow:hidden;
}
.detail-section{margin:14px 0;border:1px solid #dce7ee;border-radius:10px;background:#fff;overflow:hidden}
.detail-section-summary{display:flex;align-items:center;gap:12px;min-height:48px;padding:0 16px;cursor:pointer;list-style:none;background:#f7fafb;transition:background-color .18s ease}
.detail-section-summary::-webkit-details-marker{display:none}
.detail-section-summary::before{content:'›';flex:0 0 auto;color:#6d8593;font-size:20px;line-height:1;transform:rotate(0);transition:transform .18s ease}
.detail-section[open]>.detail-section-summary::before{transform:rotate(90deg)}
.detail-section-summary:hover{background:#f1f7f9}
.section-summary-title{color:var(--detail-title);font-size:15px;font-weight:600}
.section-summary-meta{margin-left:auto;color:var(--detail-muted);font-size:12px;font-weight:400;text-align:right}
.detail-section-body{padding:14px 16px 16px;border-top:1px solid #e8eef2}
.approval-panel{margin-top:14px}
.approval-body{padding-top:6px;padding-bottom:8px}
.approval-history{max-height:440px;overflow:auto}
.approval-panel .approval-round{margin:0;padding:0 0 4px}
.approval-panel .approval-round+.approval-round{margin-top:10px;padding-top:10px;border-top:1px solid #e3eaf0}
.round-head{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:6px 0 8px;border-bottom:1px solid #e8eef2}
.round-title,.round-submission{display:flex;align-items:center;gap:10px}
.round-title strong{color:#24485b;font-size:14px;font-weight:600}
.round-submission{color:#718391;font-size:12px}
.round-submission time{font-variant-numeric:tabular-nums}
.approval-entry{display:grid;grid-template-columns:24px minmax(0,1fr);gap:10px;padding:10px 0 4px}
.approval-marker{display:grid;place-items:center;width:22px;height:22px;border-radius:50%;background:#eaf4f7;color:#17728a;font-size:13px;font-weight:600}
.status-approved .approval-marker{background:#edf8e9;color:#4e982f}
.status-returned .approval-marker,.status-rejected .approval-marker{background:#fff0ed;color:#d75a48}
.approval-entry-head{display:flex;align-items:center;gap:10px;flex-wrap:wrap;color:#34556a;font-size:13px}
.approval-entry-head strong{font-size:14px;font-weight:600}
.approval-entry-head time{margin-left:auto;color:#718391;font-size:12px;font-variant-numeric:tabular-nums}
.approval-person{margin-top:3px;color:#718391;font-size:12px}
.approval-comment{display:grid;grid-template-columns:60px minmax(0,1fr);gap:8px;margin:7px 0 0;padding:8px 10px;background:#fff7f3;color:#7e4331;border-radius:6px;white-space:pre-wrap;overflow-wrap:anywhere}
.approval-comment span{color:#a56a58;font-size:12px}
.cargo-summary{display:block;max-height:76px;overflow:auto;line-height:1.6}
.detail-head{padding:16px;border:1px solid #dce7ee;border-radius:10px;background:#f3f8fb}
@media(max-width:600px){.detail-section-summary{align-items:flex-start;flex-wrap:wrap;gap:6px;padding:10px 12px}.section-summary-meta{width:100%;margin-left:30px;text-align:left}.detail-section-body{padding:10px 12px}.round-head,.round-submission{align-items:flex-start;flex-direction:column}.round-head{gap:6px}.approval-entry-head time{width:100%;margin-left:0}.approval-comment{grid-template-columns:1fr}}
.recv {
  display: flex;
  gap: 22px;
  align-items: baseline;
  margin-bottom: 10px;
  font-size: 13px;
}
.not-mine {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.src-tag {
  margin-left: 6px;
}
.contracts-page :deep(.el-card) { border-color:#dceaf0; border-radius:12px; box-shadow:0 10px 28px rgb(20 24 23 / 5%); }
.contracts-page :deep(.el-table) { --el-table-header-bg-color:#eef9fe; --el-table-header-text-color:#24323a; --el-table-row-hover-bg-color:#f0fbf6; }
.contracts-page :deep(.el-table th.el-table__cell) { height:48px; border-bottom-color:#d9edf5; font-weight:650; }
.contracts-page :deep(.el-table td.el-table__cell) { padding:13px 0; border-bottom-color:#e7eff3; color:#141817; }
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
  color: var(--detail-muted, var(--el-text-color-secondary));
  font-size: 12px;
  font-weight: 400;
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
.total-label{color:#5d7280;font-size:13px;font-weight:500}
.totals .total-value{color:#153f55;font-size:20px;font-weight:600;letter-spacing:.1px}
.snapshot {
  margin-top: 12px;
  padding: 8px 12px;
  border-radius: 6px;
  background: var(--el-fill-color-light);
  color:#536b79;
  font-size:12px;
  font-weight:400;
}
.items-foot {
  margin-top: 10px;
}
.num {
  font-variant-numeric: tabular-nums;
}
.num.dim {
  color: var(--el-text-color-placeholder);
}
.num.done {
  color: var(--el-color-success);
}
.num.over,
.sub.over {
  color: var(--el-color-danger);
  font-weight: 600;
}
.grow {
  flex: 1;
}
.head-form {
  margin-bottom: 4px;
}
.side-title {
  display: flex;
  align-items: baseline;
  gap: 10px;
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
  font-weight: 600;
}
</style>
