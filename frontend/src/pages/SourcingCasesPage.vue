<template>
  <div class="page">
    <ProcurementNav />
    <div class="page-head">
      <div>
        <h1>{{ t('sourcing.title') }}</h1>
        <p>{{ t('sourcing.subtitle') }}</p>
      </div>
    </div>

    <div class="toolbar">
      <el-input v-model="keyword" :placeholder="t('sourcing.search')" clearable @keyup.enter="reload" />
      <el-select v-model="status" clearable :placeholder="t('common.status')" @change="reload">
        <el-option v-for="s in statuses" :key="s" :value="s" :label="t(`sourcing.statuses.${s}`)" />
      </el-select>
      <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe @row-click="openCase">
      <el-table-column prop="caseNo" :label="t('sourcing.caseNo')" width="180" />
      <el-table-column prop="title" :label="t('sourcing.inquiry')" min-width="240" />
      <el-table-column prop="customerName" :label="t('sourcing.customer')" min-width="180" />
      <el-table-column prop="ownerName" :label="t('sourcing.owner')" width="140" />
      <el-table-column :label="t('common.status')" width="160">
        <template #default="{ row }">
          <el-tag effect="plain">{{ t(`sourcing.statuses.${row.status}`) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('sourcing.createdAt')" width="170">
        <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
      </el-table-column>
      <template #empty>{{ t('sourcing.empty') }}</template>
    </el-table>

    <el-pagination
      class="pager" layout="total, prev, pager, next" :total="total"
      :page-size="pageSize" v-model:current-page="page" @current-change="load"
    />

    <el-dialog v-model="detailOpen" :title="detail?.caseNo || t('sourcing.title')" width="min(1200px, 94vw)">
      <el-descriptions v-if="detail" :column="3" border class="meta">
        <el-descriptions-item :label="t('sourcing.customer')">{{ detail.customerName || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('sourcing.contact')">{{ detail.contactName || detail.contactEmail || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('common.status')">{{ t(`sourcing.statuses.${detail.status}`) }}</el-descriptions-item>
      </el-descriptions>
      <el-table v-if="detail" :data="detail.lines" size="small" border>
        <el-table-column prop="lineNo" label="#" width="55" />
        <el-table-column :label="t('sourcing.product')" min-width="150">
          <template #default="{ row }">{{ row.extracted.product || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.standard')" min-width="190">
          <template #default="{ row }">{{ row.extracted.materialStandard || row.extracted.grade || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.size')" min-width="180">
          <template #default="{ row }">{{ sizeOf(row.extracted) }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.port')" min-width="130">
          <template #default="{ row }">{{ row.extracted.port || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.quantity')" width="140" align="right">
          <template #default="{ row }">{{ row.extracted.quantity }} {{ row.extracted.quantityUnit }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.decision')" width="120">
          <template #default="{ row }">{{ t(`sourcing.decisions.${row.decision}`) }}</template>
        </el-table-column>
        <el-table-column :label="t('sourcing.internalProduct')" min-width="150">
          <template #default="{ row }">{{ productLabel(row.productId, row.skuId) }}</template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="100" fixed="right">
          <template #default="{ row }"><el-button v-if="canWrite" link type="primary" @click.stop="openReview(row)">{{ t('sourcing.reviewLine') }}</el-button></template>
        </el-table-column>
      </el-table>
      <h3 class="section-title">{{ t('sourcing.factoryRfqs') }}</h3>
      <el-table :data="rfqs" size="small" border>
        <el-table-column prop="rfqNo" :label="t('sourcing.rfqNo')" width="170" />
        <el-table-column prop="supplierName" :label="t('sourcing.supplier')" min-width="180" />
        <el-table-column prop="currency" :label="t('sourcing.currency')" width="90" />
        <el-table-column prop="responseDueAt" :label="t('sourcing.responseDue')" width="130" />
        <el-table-column prop="lineCount" :label="t('sourcing.lineCount')" width="90" />
        <el-table-column :label="t('common.status')" width="130"><template #default="{ row }">{{ t(`sourcing.rfqStatuses.${row.status}`) }}</template></el-table-column>
        <el-table-column :label="t('common.actions')" width="310"><template #default="{ row }">
          <el-button link type="primary" @click.stop="downloadRFQ(row)">{{ t('sourcing.downloadRfq') }}</el-button>
          <el-button v-if="canSend" link type="primary" @click.stop="openSend(row)">{{ t('sourcing.sendRfq') }}</el-button>
          <el-button v-if="canPrice" link type="primary" @click.stop="openQuote(row)">{{ t('sourcing.enterQuote') }}</el-button>
          <el-button v-if="canPrice" link type="primary" @click.stop="openImport(row)">{{ t('sourcing.importQuote') }}</el-button>
        </template></el-table-column>
      </el-table>
      <h3 class="section-title">{{ t('sourcing.quoteComparison') }}</h3>
      <el-table :data="quoteLines" size="small" border>
        <el-table-column prop="supplierName" :label="t('sourcing.supplier')" min-width="150" />
        <el-table-column :label="t('sourcing.sourceLine')" width="100"><template #default="{ row }">{{ sourceLineNo(row.sourcingLineId) }}</template></el-table-column>
        <el-table-column prop="qty" :label="t('sourcing.quantity')" width="110" />
        <el-table-column :label="t('sourcing.unitPrice')" width="150"><template #default="{ row }">{{ row.currency }} {{ row.unitPrice }}</template></el-table-column>
        <el-table-column prop="amount" :label="t('sourcing.amount')" width="140" />
        <el-table-column prop="moq" :label="t('sourcing.moq')" width="120" />
        <el-table-column prop="leadTime" :label="t('sourcing.leadTime')" min-width="130" />
        <el-table-column prop="delivery" :label="t('sourcing.delivery')" min-width="150" />
        <el-table-column prop="paymentTerms" :label="t('sourcing.paymentTerms')" min-width="180" />
        <el-table-column prop="validUntil" :label="t('sourcing.validUntil')" width="130" />
      </el-table>
      <div class="section-heading">
        <h3 class="section-title">{{ t('sourcing.costScenarios') }}</h3>
        <el-button v-if="canPrice && quoteLines.length" type="primary" plain @click="openCostScenario">{{ t('sourcing.createCostScenario') }}</el-button>
      </div>
      <el-table :data="costScenarios" size="small" border @row-click="openCostDetail">
        <el-table-column prop="scenarioNo" :label="t('sourcing.scenarioNo')" width="180" />
        <el-table-column prop="currency" :label="t('sourcing.currency')" width="90" />
        <el-table-column prop="productTotal" :label="t('sourcing.productTotal')" width="130" align="right" />
        <el-table-column prop="chargeTotal" :label="t('sourcing.chargeTotal')" width="130" align="right" />
        <el-table-column prop="landedTotal" :label="t('sourcing.landedTotal')" width="130" align="right" />
        <el-table-column prop="marginTotal" :label="t('sourcing.marginTotal')" width="130" align="right" />
        <el-table-column prop="customerTotal" :label="t('sourcing.customerTotal')" width="140" align="right" />
        <el-table-column :label="t('common.status')" width="120"><template #default="{ row }"><el-tag effect="plain">{{ t(`sourcing.costStatuses.${row.status}`) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('common.actions')" width="220" fixed="right"><template #default="{ row }">
          <el-button v-if="canApprove && row.status === 'DRAFT'" link type="success" @click.stop="confirmCostScenario(row)">{{ t('sourcing.confirmCost') }}</el-button>
          <el-button v-if="canApprove && canCreateQuotation && row.status === 'CONFIRMED'" link type="primary" @click.stop="createCustomerQuotation(row)">{{ Number(row.customerQuotationId || 0) > 0 ? t('sourcing.openQuotation') : t('sourcing.createCustomerQuotation') }}</el-button>
        </template></el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="detailOpen = false">{{ t('common.close') }}</el-button>
        <el-button v-if="canWrite" type="primary" :disabled="!!detail?.lines.some(line => line.decision === 'PENDING') || !detail?.lines.some(line => line.decision === 'CONFIRMED')" @click="openRFQ">{{ t('sourcing.createFactoryRfq') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="reviewOpen" :title="t('sourcing.reviewLineTitle', { n: reviewing?.lineNo || '' })" width="860px" append-to-body>
      <el-alert type="info" :closable="false" show-icon class="review-hint">{{ t('sourcing.reviewHint') }}</el-alert>
      <el-form label-width="110px" class="review-form">
        <el-form-item :label="t('sourcing.internalProduct')" required>
          <el-select v-model="reviewForm.productId" filterable style="width:100%" @change="loadReviewSkus">
            <el-option-group v-if="candidateProducts.length" :label="t('sourcing.productCandidates')">
              <el-option v-for="p in candidateProducts" :key="`candidate-${p.id}`" :value="Number(p.id)" :label="`${p.code} · ${p.name}`" />
            </el-option-group>
            <el-option-group :label="t('sourcing.allProducts')">
              <el-option v-for="p in remainingProducts" :key="p.id" :value="Number(p.id)" :label="`${p.code} · ${p.name}`" />
            </el-option-group>
          </el-select>
        </el-form-item>
        <el-form-item :label="t('sourcing.sku')"><el-select v-model="reviewForm.skuId" clearable style="width:100%"><el-option v-for="sku in reviewSkus" :key="sku.id" :value="Number(sku.id)" :label="`${sku.code} · ${sku.spec || '—'}`" /></el-select></el-form-item>
        <div class="review-grid">
          <el-form-item v-for="field in reviewFields" :key="field.key" :label="t(`sourcing.reviewFields.${field.key}`)">
            <el-input v-model="reviewForm.extracted[field.key]" :type="field.long ? 'textarea' : 'text'" :rows="field.long ? 2 : undefined" />
          </el-form-item>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="reviewOpen=false">{{ t('common.cancel') }}</el-button>
        <el-button :loading="saving" @click="saveReview('SKIPPED')">{{ t('sourcing.skipLine') }}</el-button>
        <el-button type="warning" plain :loading="saving" @click="saveReview('NO_MATCH')">{{ t('sourcing.noMatch') }}</el-button>
        <el-button type="success" :loading="saving" @click="saveReview('CONFIRMED')">{{ t('sourcing.confirmLine') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="rfqOpen" :title="t('sourcing.createFactoryRfq')" width="560px" append-to-body>
      <el-form label-width="110px">
        <el-form-item :label="t('sourcing.supplier')" required><el-select v-model="rfqForm.supplierId" filterable style="width:100%"><el-option v-for="s in suppliers" :key="s.id" :value="Number(s.id)" :label="`${s.code} · ${s.name}`" /></el-select></el-form-item>
        <el-form-item :label="t('sourcing.contactEmail')"><el-input v-model="rfqForm.contactEmail" /></el-form-item>
        <el-form-item :label="t('sourcing.currency')"><el-input v-model="rfqForm.currency" maxlength="3" /></el-form-item>
        <el-form-item :label="t('sourcing.responseDue')"><el-date-picker v-model="rfqForm.responseDueAt" value-format="YYYY-MM-DD" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="rfqOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="createRFQ">{{ t('common.confirm') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="quoteOpen" :title="t('sourcing.enterQuote')" width="900px" append-to-body>
      <el-form inline>
        <el-form-item :label="t('sourcing.currency')"><el-input v-model="quoteForm.currency" style="width:90px" /></el-form-item>
        <el-form-item :label="t('sourcing.quotedAt')"><el-date-picker v-model="quoteForm.quotedAt" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item :label="t('sourcing.validUntil')"><el-date-picker v-model="quoteForm.validUntil" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item :label="t('sourcing.delivery')"><el-input v-model="quoteForm.delivery" /></el-form-item>
        <el-form-item :label="t('sourcing.paymentTerms')"><el-input v-model="quoteForm.paymentTerms" /></el-form-item>
      </el-form>
      <el-table :data="quoteRows" size="small" border>
        <el-table-column prop="product" :label="t('sourcing.product')" min-width="180" />
        <el-table-column prop="qty" :label="t('sourcing.quantity')" width="120" />
        <el-table-column :label="t('sourcing.unitPrice')" width="150"><template #default="{ row }"><el-input v-model="row.unitPrice" /></template></el-table-column>
        <el-table-column :label="t('sourcing.moq')" width="130"><template #default="{ row }"><el-input v-model="row.moq" /></template></el-table-column>
        <el-table-column :label="t('sourcing.leadTime')" min-width="160"><template #default="{ row }"><el-input v-model="row.leadTime" /></template></el-table-column>
      </el-table>
      <template #footer><el-button @click="quoteOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="saveQuote">{{ t('common.save') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="importOpen" :title="t('sourcing.importQuote')" width="650px" append-to-body>
      <el-alert type="info" :closable="false" show-icon class="review-hint">{{ t('sourcing.importHint') }}</el-alert>
      <el-form label-width="110px">
        <el-form-item :label="t('sourcing.quoteFile')" required><input type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" @change="selectQuoteFile" /></el-form-item>
        <el-form-item :label="t('sourcing.currency')"><el-input v-model="importForm.currency" maxlength="3" /></el-form-item>
        <el-form-item :label="t('sourcing.quotedAt')"><el-date-picker v-model="importForm.quotedAt" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item :label="t('sourcing.validUntil')"><el-date-picker v-model="importForm.validUntil" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item :label="t('sourcing.delivery')"><el-input v-model="importForm.delivery" /></el-form-item>
        <el-form-item :label="t('sourcing.paymentTerms')"><el-input v-model="importForm.paymentTerms" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="importOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="importQuote">{{ t('sourcing.importQuote') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="sendOpen" :title="t('sourcing.sendRfq')" width="650px" append-to-body>
      <el-alert type="info" :closable="false" show-icon class="review-hint">{{ t('sourcing.sharedSenderHint') }}</el-alert>
      <el-form label-width="110px">
        <el-form-item :label="t('sourcing.contactEmail')" required><el-input v-model="sendForm.recipientEmail" /></el-form-item>
        <el-form-item :label="t('sourcing.mailSubject')" required><el-input v-model="sendForm.subject" /></el-form-item>
        <el-form-item :label="t('sourcing.mailBody')" required><el-input v-model="sendForm.body" type="textarea" :rows="7" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="sendOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="sendRFQ">{{ t('sourcing.sendRfq') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="costOpen" :title="t('sourcing.createCostScenario')" width="min(1100px, 95vw)" append-to-body>
      <el-form inline>
        <el-form-item :label="t('sourcing.currency')"><el-input v-model="costForm.currency" maxlength="3" style="width:90px" /></el-form-item>
        <el-form-item :label="t('sourcing.allocationBasis')"><el-select v-model="costForm.allocationBasis" style="width:180px"><el-option value="TONS" :label="t('sourcing.allocationTons')" /><el-option value="PRODUCT_AMOUNT" :label="t('sourcing.allocationAmount')" /></el-select></el-form-item>
        <el-form-item :label="t('sourcing.marginType')"><el-select v-model="costForm.marginType" style="width:180px"><el-option value="PERCENT" :label="t('sourcing.marginPercent')" /><el-option value="FIXED_PER_TON" :label="t('sourcing.marginPerTon')" /></el-select></el-form-item>
        <el-form-item :label="t('sourcing.marginValue')"><el-input v-model="costForm.marginValue" style="width:120px" /></el-form-item>
      </el-form>
      <h4>{{ t('sourcing.selectQuotes') }}</h4>
      <el-table :data="costSelections" size="small" border>
        <el-table-column prop="lineNo" :label="t('sourcing.sourceLine')" width="100" />
        <el-table-column prop="product" :label="t('sourcing.product')" min-width="180" />
        <el-table-column :label="t('sourcing.selectedQuote')" min-width="430"><template #default="{ row }">
          <el-select v-model="row.quoteLineId" style="width:100%"><el-option v-for="quote in quotesForLine(row.sourcingLineId)" :key="quote.quoteLineId" :value="quote.quoteLineId" :label="`${quote.supplierName} · ${quote.currency} ${quote.unitPrice} × ${quote.qty}`" /></el-select>
        </template></el-table-column>
      </el-table>
      <div class="section-heading"><h4>{{ t('sourcing.charges') }}</h4><el-button @click="addCharge">{{ t('sourcing.addCharge') }}</el-button></div>
      <el-table :data="costCharges" size="small" border>
        <el-table-column :label="t('sourcing.chargeType')" width="190"><template #default="{ row }"><el-select v-model="row.chargeType"><el-option v-for="type in chargeTypes" :key="type" :value="type" :label="t(`sourcing.chargeTypes.${type}`)" /></el-select></template></el-table-column>
        <el-table-column :label="t('sourcing.chargeBasis')" width="170"><template #default="{ row }"><el-select v-model="row.basis"><el-option v-for="basis in chargeBases" :key="basis" :value="basis" :label="t(`sourcing.chargeBases.${basis}`)" /></el-select></template></el-table-column>
        <el-table-column :label="t('sourcing.amount')" width="130"><template #default="{ row }"><el-input v-model="row.amount" /></template></el-table-column>
        <el-table-column :label="t('sourcing.currency')" width="90"><template #default="{ row }"><el-input v-model="row.currency" maxlength="3" /></template></el-table-column>
        <el-table-column :label="t('sourcing.chargeSource')" min-width="180"><template #default="{ row }"><el-input v-model="row.source" /></template></el-table-column>
        <el-table-column :label="t('common.actions')" width="90"><template #default="{ $index }"><el-button link type="danger" @click="costCharges.splice($index, 1)">{{ t('common.delete') }}</el-button></template></el-table-column>
      </el-table>
      <template #footer><el-button @click="costOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="createCostScenario">{{ t('common.confirm') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="costDetailOpen" :title="costDetail?.scenarioNo || t('sourcing.costScenarios')" width="min(1100px, 95vw)" append-to-body>
      <el-descriptions v-if="costDetail" :column="4" border>
        <el-descriptions-item :label="t('sourcing.productTotal')">{{ costDetail.currency }} {{ costDetail.productTotal }}</el-descriptions-item>
        <el-descriptions-item :label="t('sourcing.chargeTotal')">{{ costDetail.currency }} {{ costDetail.chargeTotal }}</el-descriptions-item>
        <el-descriptions-item :label="t('sourcing.marginTotal')">{{ costDetail.currency }} {{ costDetail.marginTotal }}</el-descriptions-item>
        <el-descriptions-item :label="t('sourcing.customerTotal')">{{ costDetail.currency }} {{ costDetail.customerTotal }}</el-descriptions-item>
      </el-descriptions>
      <el-table :data="costDetail?.lines || []" size="small" border class="cost-lines">
        <el-table-column prop="productName" :label="t('sourcing.product')" min-width="150" />
        <el-table-column prop="supplierName" :label="t('sourcing.supplier')" min-width="150" />
        <el-table-column prop="qty" :label="t('sourcing.quantity')" width="100" align="right" />
        <el-table-column prop="productCost" :label="t('sourcing.productTotal')" width="130" align="right" />
        <el-table-column prop="allocatedCharge" :label="t('sourcing.allocatedCharge')" width="130" align="right" />
        <el-table-column prop="landedCost" :label="t('sourcing.landedUnit')" width="130" align="right" />
        <el-table-column prop="customerUnitPrice" :label="t('sourcing.customerUnit')" width="140" align="right" />
        <el-table-column prop="customerAmount" :label="t('sourcing.customerTotal')" width="140" align="right" />
      </el-table>
    </el-dialog>

    <el-dialog v-model="customerMapOpen" :title="t('sourcing.mapCustomer')" width="520px" append-to-body>
      <el-alert type="warning" :closable="false" show-icon class="review-hint">{{ t('sourcing.mapCustomerHint') }}</el-alert>
      <el-select v-model="mappedCustomerId" filterable style="width:100%"><el-option v-for="customer in customers" :key="customer.id" :value="customer.id" :label="`${customer.code} · ${customer.name}`" /></el-select>
      <template #footer><el-button @click="customerMapOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :disabled="!mappedCustomerId" @click="submitMappedCustomer">{{ t('common.confirm') }}</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { download, get, post, put, saveBlob } from '../api'
import { useAuthStore } from '../stores/auth'
import ProcurementNav from '../components/ProcurementNav.vue'

interface ExtractedLine {
  product: string; materialStandard: string; grade: string; thickness: string
  width: string; lengthOrForm: string; surfaceRequirement: string; coating: string; tolerance: string
  coilWeight: string; coilId: string; packaging: string; delivery: string; paymentTerms: string
  incoterm: string; port: string; quantityUnit: string; remarks: string; quantity: string
}
interface SourcingLine { id: string; lineNo: number; decision: string; productId: string; skuId: string; uomId: string; decidedByName: string; decidedAt: string; extracted: ExtractedLine }
interface SourcingCase {
  id: string; caseNo: string; title: string; customerId: string; customerName: string; contactName: string
  contactEmail: string; ownerName: string; status: string; createdAt: string; lines: SourcingLine[]
}
interface Supplier { id: string; code: string; name: string }
interface FactoryRFQ { id: string; rfqNo: string; supplierName: string; contactEmail: string; currency: string; responseDueAt: string; status: string; lineCount: number; sourcingLineIds: string[] }
interface QuoteComparison { quoteLineId: string; supplierName: string; sourcingLineId: string; qty: string; currency: string; unitPrice: string; amount: string; delivery: string; paymentTerms: string; moq: string; leadTime: string; validUntil: string }
interface CostScenarioLine { id: string; sourcingLineId: string; supplierName: string; productName: string; qty: string; productCost: string; allocatedCharge: string; landedCost: string; customerUnitPrice: string; customerAmount: string }
interface CostCharge { chargeType: string; basis: string; amount: string; currency: string; source: string }
interface CostScenario { id: string; scenarioNo: string; currency: string; status: string; productTotal: string; chargeTotal: string; landedTotal: string; marginTotal: string; customerTotal: string; customerQuotationId?: string; customerQuoteNo?: string; lines?: CostScenarioLine[] }
interface Product { id: string; code: string; name: string; nameEn?: string; brand?: string; description?: string; baseUomId: string }
interface Sku { id: string; code: string; spec: string; status: string }
interface Customer { id: string; code: string; name: string }

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = auth.can('procurement:sourcing:write')
const canSend = auth.can('procurement:sourcing:send')
const canPrice = auth.can('procurement:sourcing:price')
const canApprove = auth.can('procurement:sourcing:approve')
const canCreateQuotation = auth.can('export:quotation:write')
const statuses = ['REVIEWING', 'SOURCING', 'QUOTES_RECEIVED', 'COSTING', 'CUSTOMER_QUOTE_CREATED', 'CANCELLED']
const rows = ref<SourcingCase[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const keyword = ref('')
const status = ref('')
const loading = ref(false)
const detailOpen = ref(false)
const detail = ref<SourcingCase | null>(null)
const rfqs = ref<FactoryRFQ[]>([])
const quoteLines = ref<QuoteComparison[]>([])
const costScenarios = ref<CostScenario[]>([])
const costOpen = ref(false)
const costDetailOpen = ref(false)
const costDetail = ref<CostScenario | null>(null)
const customers = ref<Customer[]>([])
const customerMapOpen = ref(false)
const mappedCustomerId = ref('')
const pendingCostScenario = ref<CostScenario | null>(null)
const costForm = reactive({ currency: 'USD', allocationBasis: 'TONS', marginType: 'PERCENT', marginValue: '8' })
const costSelections = ref<{ sourcingLineId: string; lineNo: number; product: string; quoteLineId: string }[]>([])
const costCharges = reactive<CostCharge[]>([])
const chargeTypes = ['ORIGIN_TERMINAL', 'DESTINATION_TERMINAL', 'OCEAN_FREIGHT', 'INSURANCE', 'DOCUMENT', 'FINANCE', 'OTHER']
const chargeBases = ['PER_TON', 'PER_CONTAINER', 'PER_SHIPMENT', 'FIXED']
const suppliers = ref<Supplier[]>([])
const saving = ref(false)
const rfqOpen = ref(false)
const rfqForm = reactive({ supplierId: 0, contactEmail: '', currency: 'USD', responseDueAt: '' })
const quoteOpen = ref(false)
const quotingRFQ = ref<FactoryRFQ | null>(null)
const quoteForm = reactive({ currency: 'USD', quotedAt: '', validUntil: '', delivery: '', paymentTerms: '' })
const quoteRows = ref<{ sourcingLineId: string; product: string; qty: string; unitPrice: string; moq: string; leadTime: string }[]>([])
const importOpen = ref(false)
const importingRFQ = ref<FactoryRFQ | null>(null)
const quoteFile = ref<File | null>(null)
const importForm = reactive({ currency: 'USD', quotedAt: '', validUntil: '', delivery: '', paymentTerms: '' })
const sendOpen = ref(false)
const sendingRFQ = ref<FactoryRFQ | null>(null)
const sendForm = reactive({ recipientEmail: '', subject: '', body: '' })
const products = ref<Product[]>([])
const reviewSkus = ref<Sku[]>([])
const reviewOpen = ref(false)
const reviewing = ref<SourcingLine | null>(null)
const blankExtracted = (): ExtractedLine => ({ product: '', materialStandard: '', grade: '', thickness: '', width: '', lengthOrForm: '', surfaceRequirement: '', coating: '', tolerance: '', coilWeight: '', coilId: '', packaging: '', delivery: '', paymentTerms: '', incoterm: '', port: '', quantityUnit: '', remarks: '', quantity: '' })
const reviewForm = reactive<{ productId: number; skuId: number; extracted: ExtractedLine }>({ productId: 0, skuId: 0, extracted: blankExtracted() })
const reviewFields: { key: keyof ExtractedLine; long?: boolean }[] = [
  { key: 'product' }, { key: 'materialStandard' }, { key: 'grade' }, { key: 'thickness' }, { key: 'width' }, { key: 'lengthOrForm' },
  { key: 'surfaceRequirement' }, { key: 'coating' }, { key: 'tolerance' }, { key: 'coilWeight' }, { key: 'coilId' }, { key: 'packaging', long: true },
  { key: 'delivery' }, { key: 'paymentTerms', long: true }, { key: 'incoterm' }, { key: 'port' }, { key: 'quantityUnit' }, { key: 'remarks', long: true }, { key: 'quantity' },
]

function normalizedTokens(value: string) {
  return value.toLocaleLowerCase().split(/[^\p{L}\p{N}]+/u).filter(token => token.length >= 2)
}

function productCandidateScore(product: Product) {
  const source = [reviewForm.extracted.product, reviewForm.extracted.materialStandard, reviewForm.extracted.grade].join(' ').toLocaleLowerCase()
  const catalog = [product.code, product.name, product.nameEn, product.brand, product.description].filter(Boolean).join(' ').toLocaleLowerCase()
  if (!source.trim()) return 0
  let score = 0
  for (const token of new Set(normalizedTokens(source))) {
    if (catalog.includes(token)) score += token.length >= 4 ? 3 : 1
  }
  for (const token of new Set(normalizedTokens(catalog))) {
    if (source.includes(token)) score += token.length >= 4 ? 2 : 1
  }
  return score
}

const candidateProducts = computed(() => products.value
  .map(product => ({ product, score: productCandidateScore(product) }))
  .filter(item => item.score > 0)
  .sort((a, b) => b.score - a.score || a.product.code.localeCompare(b.product.code))
  .slice(0, 5)
  .map(item => item.product))
const remainingProducts = computed(() => {
  const suggested = new Set(candidateProducts.value.map(product => product.id))
  return products.value.filter(product => !suggested.has(product.id))
})

async function load() {
  loading.value = true
  try {
    const response = await get<{ sourcingCases: SourcingCase[]; meta: { total: number } }>('/sourcing-cases', {
      page: page.value, page_size: pageSize, keyword: keyword.value, status: status.value,
    })
    rows.value = response.sourcingCases ?? []
    total.value = Number(response.meta?.total ?? 0)
  } finally { loading.value = false }
}

function reload() { page.value = 1; load() }

async function openCase(row: Pick<SourcingCase, 'id'>) {
  if (!products.value.length) products.value = (await get<{ products: Product[] }>('/products', { page_size: 200, status: 'ACTIVE' })).products ?? []
  const response = await get<{ sourcingCase: SourcingCase }>(`/sourcing-cases/${row.id}`)
  detail.value = response.sourcingCase
  await loadSourcingCommercial(row.id)
  detailOpen.value = true
  router.replace({ query: { ...route.query, case: row.id } })
}

async function loadSourcingCommercial(caseID: string) {
  const [rfqResponse, quoteResponse, costResponse] = await Promise.all([
    get<{ factoryRfqs: FactoryRFQ[] }>(`/sourcing-cases/${caseID}/factory-rfqs`),
    get<{ lines: QuoteComparison[] }>(`/sourcing-cases/${caseID}/supplier-quotes`),
    get<{ costScenarios: CostScenario[] }>(`/sourcing-cases/${caseID}/cost-scenarios`),
  ])
  rfqs.value = rfqResponse.factoryRfqs ?? []
  quoteLines.value = quoteResponse.lines ?? []
  costScenarios.value = costResponse.costScenarios ?? []
}

function quotesForLine(lineID: string) { return quoteLines.value.filter(quote => String(quote.sourcingLineId) === String(lineID)) }

function openCostScenario() {
  if (!detail.value) return
  Object.assign(costForm, { currency: 'USD', allocationBasis: 'TONS', marginType: 'PERCENT', marginValue: '8' })
  costSelections.value = detail.value.lines.filter(line => line.decision === 'CONFIRMED').map(line => ({
    sourcingLineId: line.id, lineNo: line.lineNo, product: line.extracted.product,
    quoteLineId: quotesForLine(line.id)[0]?.quoteLineId || '',
  }))
  costCharges.splice(0)
  addCharge()
  costOpen.value = true
}

function addCharge() { costCharges.push({ chargeType: 'ORIGIN_TERMINAL', basis: 'PER_TON', amount: '', currency: costForm.currency, source: '' }) }

async function createCostScenario() {
  if (!detail.value || costSelections.value.some(line => !line.quoteLineId)) { ElMessage.warning(t('sourcing.quoteSelectionRequired')); return }
  if (costCharges.some(charge => charge.amount === '' || Number(charge.amount) < 0)) { ElMessage.warning(t('sourcing.chargeAmountRequired')); return }
  saving.value = true
  try {
    await post(`/sourcing-cases/${detail.value.id}/cost-scenarios`, {
      currency: costForm.currency, allocation_basis: costForm.allocationBasis,
      margin_type: costForm.marginType, margin_value: costForm.marginValue,
      selections: costSelections.value.map(line => ({ sourcing_line_id: Number(line.sourcingLineId), supplier_quote_line_id: Number(line.quoteLineId) })),
      charges: costCharges.map(charge => ({ charge_type: charge.chargeType, basis: charge.basis, amount: charge.amount, currency: charge.currency, source: charge.source })),
    })
    costOpen.value = false
    await loadSourcingCommercial(detail.value.id)
    await load()
    ElMessage.success(t('sourcing.costCreated'))
  } finally { saving.value = false }
}

async function openCostDetail(row: CostScenario) {
  const response = await get<{ costScenario: CostScenario }>(`/cost-scenarios/${row.id}`)
  costDetail.value = response.costScenario
  costDetailOpen.value = true
}

async function confirmCostScenario(row: CostScenario) {
  await post(`/cost-scenarios/${row.id}/confirm`)
  if (detail.value) await loadSourcingCommercial(detail.value.id)
  ElMessage.success(t('sourcing.costConfirmed'))
}

async function createCustomerQuotation(row: CostScenario) {
  if (Number(row.customerQuotationId || 0) > 0) { detailOpen.value = false; await router.push({ path: '/quotations', query: { quote: row.customerQuotationId } }); return }
  if (!Number(detail.value?.customerId || 0)) {
    customers.value = (await get<{ customers: Customer[] }>('/customers', { page_size: 200, status: 'ACTIVE' })).customers ?? []
    pendingCostScenario.value = row; mappedCustomerId.value = ''; customerMapOpen.value = true
    return
  }
  await performCreateCustomerQuotation(row, detail.value!.customerId)
}

async function submitMappedCustomer() {
  if (!pendingCostScenario.value || !mappedCustomerId.value) return
  customerMapOpen.value = false
  await performCreateCustomerQuotation(pendingCostScenario.value, mappedCustomerId.value)
}

async function performCreateCustomerQuotation(row: CostScenario, customerID: string) {
  saving.value = true
  try {
    const response = await post<{ quotationId: string; quoteNo: string }>(`/cost-scenarios/${row.id}/create-customer-quotation`, { customer_id: Number(customerID) })
    if (detail.value) await loadSourcingCommercial(detail.value.id)
    ElMessage.success(t('sourcing.customerQuotationCreated', { no: response.quoteNo }))
    detailOpen.value = false
    await router.push({ path: '/quotations', query: { quote: response.quotationId } })
  } finally { saving.value = false }
}

function sourceLineNo(id: string) { return detail.value?.lines.find(line => String(line.id) === String(id))?.lineNo ?? id }
function productLabel(productID: string, skuID: string) {
  const product = products.value.find(p => String(p.id) === String(productID))
  const sku = reviewSkus.value.find(s => String(s.id) === String(skuID))
  return product ? `${product.code}${sku ? ` / ${sku.code}` : ''}` : '—'
}

async function openReview(line: SourcingLine) {
  if (!products.value.length) products.value = (await get<{ products: Product[] }>('/products', { page_size: 200, status: 'ACTIVE' })).products ?? []
  reviewing.value = line; reviewForm.productId = Number(line.productId || 0); reviewForm.skuId = Number(line.skuId || 0); reviewForm.extracted = { ...blankExtracted(), ...line.extracted }
  await loadReviewSkus(true); reviewOpen.value = true
}

async function loadReviewSkus(preserveSelection = false) {
  const selectedSkuId = preserveSelection ? reviewForm.skuId : 0
  reviewForm.skuId = 0; reviewSkus.value = []
  if (reviewForm.productId) reviewSkus.value = (await get<{ skus: Sku[] }>(`/products/${reviewForm.productId}/skus`)).skus?.filter(s => s.status === 'ACTIVE') ?? []
  if (selectedSkuId && reviewSkus.value.some(sku => Number(sku.id) === selectedSkuId)) reviewForm.skuId = selectedSkuId
}

async function saveReview(decision: 'CONFIRMED' | 'NO_MATCH' | 'SKIPPED') {
  if (!detail.value || !reviewing.value) return
  if (decision === 'CONFIRMED' && !reviewForm.productId) { ElMessage.warning(t('sourcing.productRequired')); return }
  saving.value = true
  try {
    const response = await put<{ sourcingCase: SourcingCase }>(`/sourcing-cases/${detail.value.id}/lines/${reviewing.value.id}`, { extracted: reviewForm.extracted, product_id: reviewForm.productId, sku_id: reviewForm.skuId, decision })
    detail.value = response.sourcingCase; reviewOpen.value = false; ElMessage.success(t(`sourcing.reviewSaved.${decision}`))
  } finally { saving.value = false }
}

async function openRFQ() {
  if (!suppliers.value.length) suppliers.value = (await get<{ suppliers: Supplier[] }>('/suppliers', { page_size: 200, status: 'ACTIVE' })).suppliers ?? []
  rfqForm.supplierId = 0; rfqForm.contactEmail = ''; rfqForm.currency = 'USD'; rfqForm.responseDueAt = ''
  rfqOpen.value = true
}

async function createRFQ() {
  if (!detail.value || !rfqForm.supplierId) { ElMessage.warning(t('sourcing.supplierRequired')); return }
  saving.value = true
  try {
    await post(`/sourcing-cases/${detail.value.id}/factory-rfqs`, { supplier_id: rfqForm.supplierId, contact_email: rfqForm.contactEmail, currency: rfqForm.currency, response_due_at: rfqForm.responseDueAt, sourcing_line_ids: detail.value.lines.filter(l => l.decision === 'CONFIRMED').map(l => Number(l.id)) })
    rfqOpen.value = false; await loadSourcingCommercial(detail.value.id); ElMessage.success(t('sourcing.rfqCreated'))
  } finally { saving.value = false }
}

function openQuote(row: FactoryRFQ) {
  if (!detail.value) return
  quotingRFQ.value = row; quoteForm.currency = row.currency || 'USD'; quoteForm.quotedAt = ''; quoteForm.validUntil = ''; quoteForm.delivery = ''; quoteForm.paymentTerms = ''
  const included = new Set(row.sourcingLineIds.map(String))
  quoteRows.value = detail.value.lines.filter(line => included.has(String(line.id))).map(line => ({ sourcingLineId: line.id, product: line.extracted.product, qty: line.extracted.quantity, unitPrice: '', moq: '', leadTime: '' }))
  quoteOpen.value = true
}

async function saveQuote() {
  if (!quotingRFQ.value || quoteRows.value.some(row => Number(row.unitPrice) < 0 || row.unitPrice === '')) { ElMessage.warning(t('sourcing.priceRequired')); return }
  saving.value = true
  try {
    await post(`/factory-rfqs/${quotingRFQ.value.id}/supplier-quotes`, { quoted_at: quoteForm.quotedAt, valid_until: quoteForm.validUntil, currency: quoteForm.currency, payment_terms: quoteForm.paymentTerms, delivery: quoteForm.delivery, source: 'MANUAL', lines: quoteRows.value.map(row => ({ sourcing_line_id: Number(row.sourcingLineId), qty: row.qty, unit_price: row.unitPrice, moq: row.moq, lead_time: row.leadTime })) })
    quoteOpen.value = false; if (detail.value) await loadSourcingCommercial(detail.value.id); await load(); ElMessage.success(t('sourcing.quoteSaved'))
  } finally { saving.value = false }
}

async function downloadRFQ(row: FactoryRFQ) {
  const file = await download(`/factory-rfqs/${row.id}/workbook`)
  saveBlob(file.blob, file.fileName || `${row.rfqNo}-quote.xlsx`)
}

function openImport(row: FactoryRFQ) {
  importingRFQ.value = row; quoteFile.value = null
  Object.assign(importForm, { currency: row.currency || 'USD', quotedAt: '', validUntil: '', delivery: '', paymentTerms: '' })
  importOpen.value = true
}

function selectQuoteFile(event: Event) {
  quoteFile.value = (event.target as HTMLInputElement).files?.[0] ?? null
}

async function fileBase64(file: File) {
  const bytes = new Uint8Array(await file.arrayBuffer())
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return btoa(binary)
}

async function importQuote() {
  if (!importingRFQ.value || !quoteFile.value) { ElMessage.warning(t('sourcing.quoteFileRequired')); return }
  if (quoteFile.value.size > 2 * 1024 * 1024) { ElMessage.warning(t('sourcing.quoteFileTooLarge')); return }
  saving.value = true
  try {
    await post(`/factory-rfqs/${importingRFQ.value.id}/supplier-quotes/import`, { file_data: await fileBase64(quoteFile.value), quoted_at: importForm.quotedAt, valid_until: importForm.validUntil, currency: importForm.currency, payment_terms: importForm.paymentTerms, delivery: importForm.delivery })
    importOpen.value = false; if (detail.value) await loadSourcingCommercial(detail.value.id); await load(); ElMessage.success(t('sourcing.quoteImported'))
  } finally { saving.value = false }
}

function openSend(row: FactoryRFQ) {
  sendingRFQ.value = row
  Object.assign(sendForm, { recipientEmail: row.contactEmail || '', subject: `Request for quotation ${row.rfqNo}`, body: `Dear ${row.supplierName},\n\nPlease complete the attached quotation workbook for ${row.rfqNo} and return it without changing RFQ No, Line ID, Quantity or Unit.\n\nThank you.` })
  sendOpen.value = true
}

async function sendRFQ() {
  if (!sendingRFQ.value || !sendForm.recipientEmail || !sendForm.subject || !sendForm.body) { ElMessage.warning(t('sourcing.mailRequired')); return }
  saving.value = true
  try {
    await post(`/factory-rfqs/${sendingRFQ.value.id}/send`, { recipient_email: sendForm.recipientEmail, subject: sendForm.subject, body: sendForm.body })
    sendOpen.value = false; if (detail.value) await loadSourcingCommercial(detail.value.id); ElMessage.success(t('sourcing.rfqSent'))
  } finally { saving.value = false }
}

function sizeOf(line: ExtractedLine) {
  return [line.thickness, line.width, line.lengthOrForm].filter(Boolean).join(' × ') || '—'
}
function formatTime(value: string) { return value ? new Date(value).toLocaleString() : '—' }

onMounted(async () => {
  await load()
  const id = String(route.query.case || '')
  if (id) await openCase({ id })
})
</script>

<style scoped>
.page { padding: 24px; }
.page-head { display:flex; justify-content:space-between; margin-bottom:18px; }
h1 { margin:0; font-size:24px; } .page-head p { margin:6px 0 0; color:#6b7280; }
.toolbar { display:flex; gap:10px; margin-bottom:14px; }
.toolbar .el-input { width:300px; } .toolbar .el-select { width:190px; }
.pager { margin-top:16px; justify-content:flex-end; } .meta { margin-bottom:16px; }
.section-title { margin:20px 0 10px; font-size:16px; }
.section-heading { display:flex; align-items:center; justify-content:space-between; margin-top:18px; }
.section-heading .section-title, .section-heading h4 { margin:0 0 10px; }
.cost-lines { margin-top:16px; }
.review-hint { margin-bottom:16px; }
.review-grid { display:grid; grid-template-columns:1fr 1fr; gap:0 14px; }
.review-grid :deep(.el-form-item) { margin-bottom:14px; }
@media (max-width:700px) { .review-grid { grid-template-columns:1fr; } }
</style>
