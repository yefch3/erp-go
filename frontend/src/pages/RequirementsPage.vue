<template>
  <div class="requirements-page">
    <WorkflowPageHeader title="实单询价" description="财务放行后的采购任务，需要重新取得工厂报价并选定最终方案。" />

    <template v-if="!detailBatchKey">
    <el-card shadow="never">
      <div class="filters">
        <el-input
          v-model="keyword"
          :placeholder="t('requirements.searchPlaceholder')"
          clearable
          style="width: 260px"
          @keyup.enter="reload"
          @clear="reload"
        />
        <el-button @click="reload">{{ t('common.query') }}</el-button>
      </div>

      <el-table class="batch-hierarchy-table" :data="purchaseBatches" row-key="key" v-loading="loading">
        <el-table-column :label="t('requirements.purchaseBatch')" min-width="170">
          <template #default="{ row }">
            <div class="batch-no">{{ row.label }}</div>
            <div class="sub">{{ row.customerName || '—' }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('requirements.batchProducts')" min-width="230">
          <template #default="{ row }">
            <div class="batch-product-summary batch-product-summary--stacked">
              <span class="batch-product-summary__main"><span class="batch-product-summary__text">{{ batchProductSummary(row) }}</span><span v-if="row.productGroups.length > 1" class="batch-product-summary__count">+{{ row.productGroups.length - 1 }}</span></span>
              <small>{{ row.productGroups.length }} 种产品 · {{ row.lines.length }} 种规格</small>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="实单报价进度" min-width="160">
          <template #default="{ row }">
            <span>进入详情查看进度</span>
            <div class="sub">录入、比较并选定工厂报价</div>
            <el-tag class="batch-mobile-stage" type="warning" effect="plain" size="small">待实单询价</el-tag>
          </template>
        </el-table-column>
        <el-table-column class-name="batch-secondary-column" label-class-name="batch-secondary-column" :label="t('requirements.requiredDate')" width="130">
          <template #default="{ row }">{{ row.requiredDate || '—' }}</template>
        </el-table-column>
        <el-table-column class-name="batch-secondary-column" label-class-name="batch-secondary-column" :label="t('requirements.fromContract')" min-width="180">
          <template #default="{ row }">{{ row.sourceLabels || '—' }}</template>
        </el-table-column>
        <el-table-column class-name="batch-status-column" label-class-name="batch-status-column" :label="t('common.status')" min-width="105">
          <template #default><el-tag type="warning" effect="plain">待实单询价</el-tag></template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" min-width="125" align="center">
          <template #default="{ row }">
            <el-button type="primary" plain @click="openBatchReview(row)">进入详情</el-button>
          </template>
        </el-table-column>
        <template #empty>{{ t('requirements.empty') }}</template>
      </el-table>

      <el-pagination
        class="pager"
        layout="total, prev, pager, next"
        :total="purchaseBatches.length"
        :page-size="pageSize"
        :current-page="page"
        @current-change="(p: number) => { page = p; load() }"
      />
    </el-card>
    </template>

    <template v-else-if="activeBatch">
      <div class="detail-back"><el-button @click="backToBatchList">← 返回实单询价</el-button></div>
      <section class="inquiry-detail-hero">
        <div><span>采购批次</span><h2>{{ activeBatch.label }}</h2><p>{{ activeBatch.customerName || '未填写客户' }} · 来源合同 {{ activeBatch.sourceLabels || '—' }}</p></div>
        <el-tag type="warning" effect="light" size="large">{{ detailStageLabel }}</el-tag>
      </section>
      <el-card shadow="never" class="inquiry-detail-card">
        <div class="requote-summary">
          <div><span>客户</span><strong>{{ activeBatch.customerName || '—' }}</strong></div>
          <div><span>采购内容</span><strong>{{ activeBatch.productGroups.length }} 种产品 / {{ activeBatch.lines.length }} 种规格</strong></div>
          <div><span>要求到货</span><strong>{{ activeBatch.requiredDate || '—' }}</strong></div>
          <div><span>处理进度</span><strong>已报价 {{ quotedLineCount }}/{{ activeBatch.lines.length }} · 已选 {{ selectedCount }}/{{ activeBatch.lines.length }}</strong></div>
        </div>
        <el-tabs v-model="activeInquiryTab" class="inquiry-tabs">
          <el-tab-pane :label="`工厂报价 ${quotedLineCount}/${activeBatch.lines.length}`" name="quotes">
            <el-alert type="info" :closable="false" show-icon class="alert" title="售前报价只用于对照。本页录入合同执行阶段重新确认的工厂报价。" />
            <div v-if="canOrder" class="batch-quote-toolbar">
              <div><strong>工厂报价</strong><span>一次填写一家工厂对本批产品的报价，不供应的规格可留空</span></div>
              <el-button type="primary" @click="openBatchQuoteEditor">＋ 添加一份工厂报价</el-button>
            </div>
            <section v-for="group in activeBatch.productGroups" :key="group.key" class="detail-product-group">
              <button type="button" class="batch-product-heading" @click="toggleBatchProduct(activeBatch.key, group.key)">
                <el-icon :class="['batch-product-chevron', { 'is-expanded': isBatchProductExpanded(activeBatch.key, group.key) }]"><ArrowDown /></el-icon>
                <strong>{{ group.name }}</strong><span>{{ group.lines.length }} 种规格</span><span class="batch-product-total">合计 {{ requirementQuantitySummary(group.lines) }}</span>
              </button>
              <div v-show="isBatchProductExpanded(activeBatch.key, group.key)" class="detail-spec-list">
                <article v-for="line in group.lines" :key="line.id" class="requote-product">
                  <header class="requote-product-head"><div><strong>{{ line.spec || line.productCode || '未填写规格' }}</strong><span>{{ line.productCode || '未填写产品编码' }}</span></div><div class="requote-product-status"><span>{{ quotesFor(line.id).length ? `已录入 ${quotesFor(line.id).length} 家报价` : '待录入报价' }}</span><b>{{ formatQtyTwo(line.availableQty) }} {{ line.uomCode }}</b></div></header>
                  <div class="presale-reference"><span>售前参考</span><strong>{{ line.supplierName || '无售前供应商' }}</strong><span>{{ displaySourcePrice(line) }}</span><span>{{ line.sourcePaymentTerms || '未填写付款条件' }}</span></div>
                  <div class="quote-options">
                    <div v-for="quote in quotesFor(line.id)" :key="quote.id" :class="['quote-option', {selected:quote.selected}]">
                      <span class="quote-main"><strong>{{ quote.supplierName }}</strong><b>{{ quote.currency }} {{ formatMoney(quote.unitPrice) }}/{{ line.uomCode }}</b></span>
                      <div class="quote-meta"><span>交期 {{ quote.expectedDate || '—' }}</span><span>{{ quote.paymentTerms }}</span><span v-if="quote.validUntil">有效至 {{ quote.validUntil }}</span></div>
                      <div v-if="canOrder" class="quote-actions"><el-button link type="primary" @click="openQuoteEditor(line, quote)">编辑</el-button><el-button link type="danger" @click="removeQuote(line, quote)">删除</el-button></div>
                    </div>
                  </div>
                  <div v-if="!quotesFor(line.id).length" class="quote-empty"><span>暂无实单报价</span><small>使用上方按钮统一录入工厂报价</small></div>
                </article>
              </div>
            </section>
            <div class="tab-next"><el-button type="primary" plain @click="activeInquiryTab='selection'">下一步：选定报价与生成订单</el-button></div>
          </el-tab-pane>
          <el-tab-pane :label="`选定报价与生成订单 ${selectedCount}/${activeBatch.lines.length}`" name="selection">
            <el-alert type="info" :closable="false" show-icon class="alert" title="为每个规格选定一家工厂。系统会保存选择，并按供应商、币种、交货日期和付款条件拆分采购订单草稿。" />
            <section v-for="group in activeBatch.productGroups" :key="group.key" class="detail-product-group">
              <button type="button" class="batch-product-heading" @click="toggleBatchProduct(activeBatch.key, group.key)">
                <el-icon :class="['batch-product-chevron', { 'is-expanded': isBatchProductExpanded(activeBatch.key, group.key) }]"><ArrowDown /></el-icon>
                <strong>{{ group.name }}</strong><span>{{ selectedLinesIn(group) }}/{{ group.lines.length }} 个规格已选</span><span class="batch-product-total">合计 {{ requirementQuantitySummary(group.lines) }}</span>
              </button>
              <div v-show="isBatchProductExpanded(activeBatch.key, group.key)" class="detail-spec-list">
                <article v-for="line in group.lines" :key="line.id" class="requote-product">
                  <header class="requote-product-head"><div><strong>{{ line.spec || line.productCode || '未填写规格' }}</strong><span>售前参考：{{ line.supplierName || '无' }} · {{ displaySourcePrice(line) }}</span></div><b>{{ formatQtyTwo(line.availableQty) }} {{ line.uomCode }}</b></header>
                  <el-radio-group v-model="selectedQuote[line.id]" class="quote-options" :disabled="!canOrder" @change="(value:number|string|boolean|undefined) => persistQuoteSelection(line, Number(value))">
                    <div v-for="quote in quotesFor(line.id)" :key="quote.id" :class="['quote-option', {selected:Number(selectedQuote[line.id])===Number(quote.id)}]">
                      <el-radio :value="Number(quote.id)"><span class="quote-main"><strong>{{ quote.supplierName }}</strong><b>{{ quote.currency }} {{ formatMoney(quote.unitPrice) }}/{{ line.uomCode }}</b></span></el-radio>
                      <div class="quote-meta"><span>交期 {{ quote.expectedDate || '—' }}</span><span>{{ quote.paymentTerms }}</span><span v-if="quote.validUntil">有效至 {{ quote.validUntil }}</span><span v-if="quote.selectedByName">由 {{ quote.selectedByName }} 选定</span></div>
                    </div>
                  </el-radio-group>
                  <div v-if="!quotesFor(line.id).length" class="quote-empty"><span>尚无报价可选</span><small>请返回“工厂报价”录入</small></div>
                </article>
              </div>
            </section>
            <section v-if="selectedCount === activeBatch.lines.length" class="draft-preview">
              <div><strong>采购订单草稿预览</strong><span>预计生成 {{ draftOrderGroups.length }} 张草稿，提交审批请在采购订单草稿页面完成。</span></div>
              <div v-for="group in draftOrderGroups" :key="group.key" class="draft-preview-row"><strong>{{ group.supplierName }}</strong><span>{{ group.currency }} · {{ group.expectedDate || '未填写交期' }} · {{ group.lines.length }} 个规格</span></div>
              <el-button v-if="canOrder" type="primary" :loading="saving" @click="createDraftOrders">生成 {{ draftOrderGroups.length }} 张采购订单草稿</el-button>
            </section>
          </el-tab-pane>
        </el-tabs>
      </el-card>
    </template>
    <el-card v-else shadow="never" v-loading="loading"><el-empty description="未找到这条实单询价"><el-button @click="backToBatchList">返回列表</el-button></el-empty></el-card>

    <el-dialog v-model="batchQuoteOpen" title="添加一份工厂报价" width="780px" class="batch-quote-dialog" destroy-on-close>
      <div class="batch-dialog-intro"><strong>整份录入，减少重复操作</strong><span>公共条件只填一次，下面分别填写这家工厂愿意供应的产品单价。</span></div>
      <el-form label-position="top" class="batch-quote-form">
        <section class="batch-form-section">
          <div class="batch-section-title"><strong>报价基本信息</strong><span>同一家工厂的公共报价条件</span></div>
          <div class="batch-common-grid">
            <el-form-item label="工厂" required class="field-wide"><el-select v-model="batchQuoteForm.supplierId" filterable allow-create default-first-option clearable placeholder="输入新工厂名称或选择已有工厂" style="width:100%"><el-option v-for="supplier in suppliers" :key="supplier.id" :value="Number(supplier.id)" :label="`${supplier.code} · ${supplier.name}`" /></el-select></el-form-item>
            <el-form-item label="币种" required><el-select v-model="batchQuoteForm.currency" filterable allow-create default-first-option style="width:100%" @change="batchQuoteForm.currency=String(batchQuoteForm.currency).trim().toUpperCase()"><el-option v-for="currency in currencyOptions" :key="currency" :value="currency" /></el-select></el-form-item>
            <el-form-item label="预计交货日期"><el-date-picker v-model="batchQuoteForm.expectedDate" value-format="YYYY-MM-DD" clearable style="width:100%" /></el-form-item>
            <el-form-item label="报价有效期"><el-date-picker v-model="batchQuoteForm.validUntil" value-format="YYYY-MM-DD" clearable style="width:100%" /></el-form-item>
            <el-form-item label="付款条件" required class="field-wide"><el-select v-model="batchQuoteForm.paymentTerms" filterable allow-create default-first-option clearable placeholder="输入自定义条款或选择常用付款条件" style="width:100%"><el-option v-for="term in paymentTermOptions" :key="term" :label="term" :value="term" /></el-select></el-form-item>
          </div>
        </section>
        <section class="batch-form-section product-price-section">
          <div class="batch-section-title"><strong>产品报价</strong><span>不供应的产品可以留空</span></div>
          <div class="batch-price-list">
            <div v-for="line in activeBatch?.lines ?? []" :key="line.id" class="batch-price-line">
              <div><strong>{{ line.productName }}</strong><span>{{ line.spec || line.productCode || '未填写规格' }} · {{ trimQty(line.availableQty) }} {{ line.uomCode }}</span></div>
              <el-input v-model="batchQuotePrices[line.id]" placeholder="不报价可留空"><template #append>/ {{ line.uomCode }}</template></el-input>
            </div>
          </div>
        </section>
        <section class="batch-form-section">
          <div class="batch-section-title"><strong>备注</strong><span>可选</span></div>
          <el-form-item class="remark-field"><el-input v-model="batchQuoteForm.remark" type="textarea" :rows="2" placeholder="记录包装、交付或其他特殊条件" /></el-form-item>
        </section>
      </el-form>
      <template #footer><el-button @click="batchQuoteOpen=false">取消</el-button><el-button type="primary" :loading="savingBatchQuote" @click="saveBatchQuote">保存本次报价</el-button></template>
    </el-dialog>

    <el-dialog v-model="quoteOpen" :title="`编辑工厂报价 · ${quoteLine?.productName || ''}`" width="620px" destroy-on-close>
      <el-form label-width="110px">
        <el-form-item label="工厂" required><el-select v-model="quoteForm.supplierId" filterable allow-create default-first-option clearable placeholder="输入新工厂名称或选择已有工厂" style="width:100%"><el-option v-for="supplier in suppliers" :key="supplier.id" :value="Number(supplier.id)" :label="`${supplier.code} · ${supplier.name}`" /></el-select></el-form-item>
        <el-form-item label="报价" required><div class="price-row"><el-select v-model="quoteForm.currency" filterable allow-create default-first-option @change="quoteForm.currency=String(quoteForm.currency).trim().toUpperCase()"><el-option v-for="currency in currencyOptions" :key="currency" :value="currency" /></el-select><el-input v-model="quoteForm.unitPrice" placeholder="单价" /></div></el-form-item>
        <el-form-item label="预计交货日期"><el-date-picker v-model="quoteForm.expectedDate" value-format="YYYY-MM-DD" clearable /></el-form-item>
        <el-form-item label="付款条件" required><el-select v-model="quoteForm.paymentTerms" filterable allow-create default-first-option clearable placeholder="输入自定义条款或选择常用付款条件" style="width:100%"><el-option v-for="term in paymentTermOptions" :key="term" :label="term" :value="term" /></el-select></el-form-item>
        <el-form-item label="报价有效期"><el-date-picker v-model="quoteForm.validUntil" value-format="YYYY-MM-DD" clearable /></el-form-item>
        <el-form-item label="备注"><el-input v-model="quoteForm.remark" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="quoteOpen=false">取消</el-button><el-button type="primary" :loading="savingQuote" @click="saveQuote">保存报价</el-button></template>
    </el-dialog>

    <el-drawer v-model="detailOpen" :title="detail?.productName" size="620px">
      <el-descriptions :column="2" border size="small" class="desc">
        <el-descriptions-item :label="t('requirements.source')">
          <el-tag size="small" :type="detail?.source === 'MANUAL' ? 'info' : 'primary'" effect="plain">
            {{ detail?.source === 'MANUAL' ? t('requirements.manual') : t('requirements.fromContract') }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item :label="t('common.status')">
          {{ detail ? t(`requirements.statuses.${detail.status}`) : '' }}
        </el-descriptions-item>
        <el-descriptions-item v-if="detail?.source !== 'MANUAL'" :label="t('requirements.fromContract')">
          {{ detail?.contractNo }} · {{ detail?.customerName }}
          <template v-if="detail?.ownerName"> · {{ detail.ownerName }}</template>
        </el-descriptions-item>
        <el-descriptions-item :label="t('requirements.requiredDate')">
          {{ detail?.requiredDate || '—' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('requirements.qty')">
          {{ trimQty(detail?.requiredQty ?? '') }} {{ detail?.uomCode }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('requirements.progress')">
          {{ t('requirements.ordered', { n: trimQty(detail?.orderedQty ?? '0') }) }} ·
          {{ t('requirements.arrived', { n: trimQty(detail?.receivedQty ?? '0') }) }}
        </el-descriptions-item>
        <el-descriptions-item v-if="detail?.closedReason" :label="t('requirements.reason')" :span="2">
          {{ detail?.closedReason }}
        </el-descriptions-item>
      </el-descriptions>

      <!-- The question a buyer actually has in front of an outstanding line:
           is nobody buying this, or is it already on order and merely late? -->
      <div class="side-title">{{ t('requirements.coveringOrders') }}</div>
      <el-table :data="covering" size="small">
        <el-table-column :label="t('requirements.poNo')" min-width="150">
          <template #default="{ row }">
            <router-link :to="`/purchase-orders?keyword=${row.poNo}`" class="doc-link">
              {{ row.poNo }}
            </router-link>
            <div class="sub">{{ row.supplierName }}</div>
          </template>
        </el-table-column>
        <el-table-column :label="t('requirements.qty')" width="150" align="right">
          <template #default="{ row }">
            <span class="qty">{{ trimQty(row.qty) }}</span>
            <div v-if="Number(row.receivedQty) > 0" class="sub">
              {{ t('requirements.arrived', { n: trimQty(row.receivedQty) }) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('requirements.expected')" width="110">
          <template #default="{ row }">{{ row.expectedDate || '—' }}</template>
        </el-table-column>
        <el-table-column :label="t('common.status')" width="110">
          <template #default="{ row }">
            <el-tag size="small" effect="plain">{{ t(`orders.statuses.${row.status}`) }}</el-tag>
          </template>
        </el-table-column>
        <template #empty>{{ t('requirements.noOrders') }}</template>
      </el-table>
    </el-drawer>

    <el-dialog v-model="createOpen" :title="t('requirements.create')" width="520px">
      <el-alert type="info" :closable="false" show-icon class="alert">
        {{ t('requirements.createHint') }}
      </el-alert>
      <el-form label-width="90px">
        <el-form-item :label="t('requirements.product')" required>
          <el-select v-model="createForm.productId" filterable style="width: 100%">
            <el-option
              v-for="p in products"
              :key="p.id"
              :value="Number(p.id)"
              :label="`${p.code} · ${p.name}`"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('requirements.qty')" required>
          <el-input v-model="createForm.qty" style="width: 200px" />
        </el-form-item>
        <el-form-item :label="t('requirements.requiredDate')">
          <el-date-picker v-model="createForm.requiredDate" type="date" value-format="YYYY-MM-DD" style="width: 200px" />
        </el-form-item>
        <el-form-item :label="t('requirements.reason')">
          <el-input v-model="createForm.remark" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitCreate">{{ common('save') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="closeOpen" :title="t('requirements.close')" width="480px">
      <el-alert type="warning" :closable="false" show-icon class="alert">
        {{ t('requirements.closeWarning') }}
      </el-alert>
      <el-form label-width="90px" class="close-form">
        <el-form-item :label="t('requirements.product')">
          <div>
            <div>{{ closing?.productName }}</div>
            <div class="sub">
              {{ closing?.contractNo }} · {{ trimQty(closing?.requiredQty ?? '') }} {{ closing?.uomCode }}
            </div>
          </div>
        </el-form-item>
        <el-form-item :label="t('requirements.reason')" required>
          <el-input v-model="closeReason" type="textarea" :rows="3" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="closeOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="danger" :loading="saving" @click="confirmClose">
          {{ t('requirements.close') }}
        </el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="templateImportOpen" :title="t('requirements.importTemplate')" width="760px">
      <el-alert :title="t('requirements.templateHint')" type="info" :closable="false" show-icon class="alert" />
      <input type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" @change="selectTemplateFile" />
      <template v-if="templateGroups.length">
        <div class="side-title">{{ t('requirements.templateGroups', { n: templateGroups.length }) }}</div>
        <el-table :data="templateGroups" size="small" border>
          <el-table-column prop="supplierName" :label="t('orders.supplier')" min-width="180" />
          <el-table-column prop="currency" :label="t('requirements.currency')" width="90" />
          <el-table-column prop="expectedDate" :label="t('orders.expected')" width="120" />
          <el-table-column :label="t('requirements.templateLines')" width="100"><template #default="{ row }">{{ row.lines.length }}</template></el-table-column>
          <el-table-column prop="paymentTerms" :label="t('requirements.paymentTerms')" min-width="150" />
        </el-table>
      </template>
      <template #footer>
        <el-button @click="templateImportOpen = false">{{ common('cancel') }}</el-button>
        <el-button v-if="!templateGroups.length" type="primary" :disabled="!templateFile" :loading="saving" @click="previewTemplateImport">{{ t('requirements.previewTemplate') }}</el-button>
        <el-button v-else type="primary" :loading="saving" @click="confirmTemplateImport">{{ t('requirements.createTemplateOrders', { n: templateGroups.length }) }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { del, get, post, postDownload, saveBlob } from '../api'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'
import { purchaseBatchKey } from '../lib/requirements'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'

interface Requirement {
  id: string
  contractId: string
  contractNo: string
  versionNo: number
  customerName: string
  productCode: string
  productName: string
  spec: string
  uomCode: string
  requiredQty: string
  orderedQty: string
  reservedQty: string
  availableQty: string
  requiredDate: string
  source: string
  status: string
  ownerName: string
  closedReason: string
  receivedQty: string
  quotationId: string
  quotationNo: string
  supplierId: string
  supplierCode: string
  supplierName: string
  factoryId: string
  factoryName: string
  sourceCurrency: string
  sourceUnitPrice: string
  moq: string
  leadTime: string
  sourcePaymentTerms: string
  sourceIncoterm: string
  sourceValidUntil: string
}

interface CoveringOrder {
  poId: string
  poNo: string
  supplierName: string
  status: string
  expectedDate: string
  qty: string
  receivedQty: string
}

interface PurchaseBatch {
  key: string
  label: string
  customerName: string
  supplierNames: string
  supplierCount: number
  requiredDate: string
  sourceLabels: string
  statusLabels: string
  waitingRequote: boolean
  productGroups: PurchaseProductGroup[]
  lines: Requirement[]
}

interface PurchaseProductGroup {
  key: string
  name: string
  lines: Requirement[]
}

interface Supplier { id: string; code: string; name: string }
interface ExecutionQuote {
  id: string; requirementId: string; supplierId: string; supplierCode: string; supplierName: string
  currency: string; unitPrice: string; expectedDate: string; paymentTerms: string; validUntil: string
  remark: string; createdByName: string; createdAt: string; updatedAt: string
  selected: boolean; selectedById: string; selectedByName: string; selectedAt: string
}

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const canWrite = auth.can('procurement:requirement:write')
// Raising a requirement and committing money to a supplier are separate
// permissions, so the ordering actions are gated separately too.
const canOrder = auth.can('procurement:order:write')
const detailBatchKey = computed(() => String(route.params.batchKey ?? ''))
const activeInquiryTab = ref<'quotes'|'selection'>('quotes')

const rows = ref<Requirement[]>([])
// 同一客户报价是一批采购业务；批次内仍保留产品行，便于按供应商拆分审批。
const displayRows = computed(() => [...rows.value].sort((a, b) => {
  const batch = purchaseBatchKey(a).localeCompare(purchaseBatchKey(b))
  if (batch !== 0) return batch
  return `${a.supplierName}\u0000${a.productName}`.localeCompare(`${b.supplierName}\u0000${b.productName}`)
}))
// 主页面按客户报价/采购批次汇总，避免同一单据的每个产品重复占一行。
const purchaseBatches = computed<PurchaseBatch[]>(() => {
  const grouped = new Map<string, Requirement[]>()
  for (const row of displayRows.value) {
    const key = purchaseBatchKey(row)
    grouped.set(key, [...(grouped.get(key) ?? []), row])
  }
  return [...grouped.entries()].map(([key, lines]) => {
    const supplierNames = [...new Set(lines.map((line) => line.supplierName).filter(Boolean))]
    const productGroups = groupBatchProducts(lines)
    return {
      key,
      label: lines[0]?.contractNo || lines[0]?.quotationNo || t('requirements.manualBatch'),
      customerName: lines[0]?.customerName ?? '',
      supplierNames: supplierNames.join('、'),
      supplierCount: supplierNames.length,
      requiredDate: [...lines.map((line) => line.requiredDate).filter(Boolean)].sort()[0] ?? '',
      sourceLabels: [...new Set(lines.map((line) => line.source === 'MANUAL' ? t('requirements.manual') : line.contractNo).filter(Boolean))].join('、'),
      statusLabels: [...new Set(lines.map((line) => t(`requirements.statuses.${line.status}`)))].join('、'),
      waitingRequote: lines.some((line) => line.status === 'WAITING_REQUOTE'),
      productGroups,
      lines,
    }
  })
})
const expandedBatchProductKeys = ref<string[]>([])
function groupBatchProducts(lines: Requirement[]): PurchaseProductGroup[] {
  const groups = new Map<string, PurchaseProductGroup>()
  for (const line of lines) {
    const name = line.productName.trim() || '—'
    const key = name.toLocaleLowerCase()
    const group = groups.get(key)
    if (group) group.lines.push(line)
    else groups.set(key, { key, name, lines: [line] })
  }
  return [...groups.values()]
}
function batchProductSummary(batch: PurchaseBatch): string {
  const first = batch.productGroups[0]
  if (!first) return '—'
  const specificationCount = new Set(first.lines.map((line) => `${line.productCode}\u0000${line.spec}\u0000${line.uomCode}`)).size
  return `${first.name} · ${specificationCount} 种规格`
}
function batchProductStateKey(batchKey: string, productKey: string): string { return `${batchKey}:${productKey}` }
function isBatchProductExpanded(batchKey: string, productKey: string): boolean { return expandedBatchProductKeys.value.includes(batchProductStateKey(batchKey, productKey)) }
function toggleBatchProduct(batchKey: string, productKey: string) {
  const key = batchProductStateKey(batchKey, productKey)
  expandedBatchProductKeys.value = isBatchProductExpanded(batchKey, productKey) ? expandedBatchProductKeys.value.filter((value) => value !== key) : [...expandedBatchProductKeys.value, key]
}
function formatQtyTwo(value: string): string {
  const quantity = Number(value)
  return Number.isFinite(quantity) ? quantity.toFixed(2) : value || '—'
}
function requirementQuantitySummary(lines: Requirement[]): string {
  const totals = new Map<string, number>()
  const unstructured: string[] = []
  for (const line of lines) {
    const quantity = Number(line.availableQty)
    const unit = line.uomCode.trim()
    if (Number.isFinite(quantity)) totals.set(unit, (totals.get(unit) || 0) + quantity)
    else if (line.availableQty) unstructured.push(`${line.availableQty}${unit ? ` ${unit}` : ''}`)
  }
  return [...totals].map(([unit, total]) => `${total.toFixed(2)}${unit ? ` ${unit}` : ''}`).concat(unstructured).join('；') || '—'
}
const page = ref(1)
const pageSize = 20
// Outstanding work is what a buyer opens this page for; everything else is
// history they go looking for deliberately.
const keyword = ref('')
const loading = ref(false)
const saving = ref(false)
const closeOpen = ref(false)
const createOpen = ref(false)
const products = ref<{ id: string; code: string; name: string; uomId: string; uomCode: string }[]>([])
const createForm = reactive({ productId: 0, qty: '', requiredDate: '', remark: '' })
const closing = ref<Requirement | null>(null)
const closeReason = ref('')
const selected = ref<Requirement[]>([])
const activeBatch = ref<PurchaseBatch | null>(null)
const suppliers = ref<Supplier[]>([])
const currencyOptions = ['CNY','USD','EUR','GBP','HKD','JPY','AUD','CAD','AED','SAR']
const paymentTermOptions = ['T/T', '30%预付款，70%发货前付清', '信用证 L/C', '货到付款', '月结 30 天', '月结 60 天']
const executionQuotes = reactive<Record<string, ExecutionQuote[]>>({})
const selectedQuote = reactive<Record<string, number>>({})
const batchQuoteOpen = ref(false)
const savingBatchQuote = ref(false)
const batchQuoteForm = reactive<{supplierId:number|string|null;currency:string;expectedDate:string;paymentTerms:string;validUntil:string;remark:string}>({ supplierId: null, currency: 'CNY', expectedDate: '', paymentTerms: '', validUntil: '', remark: '' })
const batchQuotePrices = reactive<Record<string, string>>({})
const quoteOpen = ref(false)
const quoteLine = ref<Requirement | null>(null)
const quoteEditing = ref<ExecutionQuote | null>(null)
const savingQuote = ref(false)
const quoteForm = reactive<{supplierId:number|string|null;currency:string;unitPrice:string;expectedDate:string;paymentTerms:string;validUntil:string;remark:string}>({ supplierId: null, currency: 'CNY', unitPrice: '', expectedDate: '', paymentTerms: '', validUntil: '', remark: '' })
const selectedCount = computed(() => (activeBatch.value?.lines ?? []).filter((line) => Number(selectedQuote[line.id]) > 0).length)
const quotedLineCount = computed(() => (activeBatch.value?.lines ?? []).filter((line) => quotesFor(line.id).length > 0).length)
const detailStageLabel = computed(() => {
  const total = activeBatch.value?.lines.length ?? 0
  if (!total || quotedLineCount.value === 0) return '待实单询价'
  if (quotedLineCount.value < total) return '询价中'
  if (selectedCount.value < total) return '待选择最终报价'
  return '可生成采购草稿'
})
const draftOrderGroups = computed(() => {
  const grouped = new Map<string, {key:string;supplierName:string;currency:string;expectedDate:string;paymentTerms:string;lines:{line:Requirement;quote:ExecutionQuote}[]}>()
  for (const line of activeBatch.value?.lines ?? []) {
    const quote = quotesFor(line.id).find((item) => Number(item.id) === Number(selectedQuote[line.id]))
    if (!quote) continue
    const key = [quote.supplierId, quote.currency, quote.expectedDate, quote.paymentTerms].join('|')
    const group = grouped.get(key) ?? { key, supplierName:quote.supplierName, currency:quote.currency, expectedDate:quote.expectedDate, paymentTerms:quote.paymentTerms, lines:[] }
    group.lines.push({ line, quote })
    grouped.set(key, group)
  }
  return [...grouped.values()]
})
const detailOpen = ref(false)
const detail = ref<Requirement | null>(null)
const covering = ref<CoveringOrder[]>([])
interface TemplateLine { rowNo: number; requirementId: string; productName: string; qty: string; uomCode: string; unitPrice: string; moq: string }
interface TemplateGroup { importToken: string; supplierId: string; supplierCode: string; supplierName: string; currency: string; expectedDate: string; paymentTerms: string; lines: TemplateLine[] }
const templateImportOpen = ref(false)
const templateFile = ref<File | null>(null)
const templateGroups = ref<TemplateGroup[]>([])

const common = (k: string) => t(`common.${k}`)

async function load() {
  loading.value = true
  try {
    // 只买了一部分的也留在这页（A5）。
    //
    // 工厂这批只供得了 80 吨，剩下的 20 吨仍然是要买的活儿。从前这页只列
    // 「一点没买」的，一旦下了第一张单整批就从眼前消失了——等于告诉采购员
    // 这事办完了。剩下的得靠人记着，正是这类事情最容易掉的地方。
    const pending = await Promise.all([
      get<{ requirements: Requirement[] }>('/requirements', { page: page.value, page_size: pageSize, status: 'WAITING_REQUOTE', keyword: keyword.value }),
    ])
    const seen = new Set<string>()
    rows.value = pending.flatMap((result) => result.requirements ?? [])
      .filter((line) => Number(line.availableQty ?? 0) > 0)
      .filter((line) => {
        if (seen.has(line.id)) return false
        seen.add(line.id)
        return true
      })
    await hydrateActiveBatchFromRoute()
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

async function openBatchReview(batch: PurchaseBatch) {
  activeBatch.value = batch
  activeInquiryTab.value = 'quotes'
  expandedBatchProductKeys.value = batch.productGroups[0] ? [batchProductStateKey(batch.key, batch.productGroups[0].key)] : []
  await router.push({ path: `/requirements/${encodeURIComponent(batch.key)}` })
  await loadBatchQuotes(batch)
}

async function hydrateActiveBatchFromRoute() {
  if (!detailBatchKey.value) {
    activeBatch.value = null
    return
  }
  const batch = purchaseBatches.value.find((item) => item.key === detailBatchKey.value) ?? null
  activeBatch.value = batch
  if (!batch) return
  if (!expandedBatchProductKeys.value.length && batch.productGroups[0]) expandedBatchProductKeys.value = [batchProductStateKey(batch.key, batch.productGroups[0].key)]
  await loadBatchQuotes(batch)
}

async function loadBatchQuotes(batch: PurchaseBatch) {
  if (!suppliers.value.length) suppliers.value = (await get<{suppliers:Supplier[]}>('/suppliers', { page_size: 200, status: 'ACTIVE' })).suppliers ?? []
  await Promise.all(batch.lines.map(async (line) => {
    const result = await get<{quotes:ExecutionQuote[]}>(`/requirements/${line.id}/execution-quotes`)
    executionQuotes[line.id] = result.quotes ?? []
    const persisted = executionQuotes[line.id].find((quote) => quote.selected)
    if (persisted) selectedQuote[line.id] = Number(persisted.id)
    else delete selectedQuote[line.id]
  }))
}

function backToBatchList() { router.push('/requirements') }

function quotesFor(requirementId: string) { return executionQuotes[requirementId] ?? [] }
function selectedLinesIn(group: PurchaseProductGroup) { return group.lines.filter((line) => Number(selectedQuote[line.id]) > 0).length }
function formatMoney(value: string) { const amount = Number(value); return Number.isFinite(amount) ? amount.toFixed(2) : value || '—' }
function displaySourcePrice(line: Requirement) {
  return Number(line.sourceUnitPrice) > 0 ? `${line.sourceCurrency || ''} ${trimQty(line.sourceUnitPrice)}/${line.uomCode}` : '未带入售前价格'
}
function openBatchQuoteEditor() {
  Object.assign(batchQuoteForm, { supplierId:null,currency:'CNY',expectedDate:'',paymentTerms:'',validUntil:'',remark:'' })
  for (const key of Object.keys(batchQuotePrices)) delete batchQuotePrices[key]
  for (const line of activeBatch.value?.lines ?? []) batchQuotePrices[line.id] = ''
  batchQuoteOpen.value = true
}
async function resolveSupplier(value: number|string|null, currency: string, paymentTerms: string): Promise<number> {
  if (typeof value === 'number' && value > 0) return value
  const name = String(value ?? '').trim()
  if (!name) throw new Error('请输入工厂名称')
  const matched = suppliers.value.find((supplier) => [supplier.code, supplier.name].some((candidate) => candidate.trim().toLocaleLowerCase() === name.toLocaleLowerCase()))
  if (matched) return Number(matched.id)
  const result = await post<{supplier:Supplier}>('/requirements/execution-suppliers/resolve', { name, currency, payment_terms:paymentTerms })
  suppliers.value = [result.supplier, ...suppliers.value.filter((supplier) => supplier.id !== result.supplier.id)]
  return Number(result.supplier.id)
}
async function saveBatchQuote() {
  const pricedLines = (activeBatch.value?.lines ?? []).filter((line) => String(batchQuotePrices[line.id] ?? '').trim() !== '')
  if (!batchQuoteForm.supplierId || !batchQuoteForm.paymentTerms.trim() || !pricedLines.length) {
    ElMessage.warning('请选择工厂、填写付款条件，并至少填写一个产品单价')
    return
  }
  batchQuoteForm.currency = batchQuoteForm.currency.trim().toUpperCase()
  if (!/^[A-Z]{3}$/.test(batchQuoteForm.currency)) {
    ElMessage.warning('币种请输入 3 位代码，例如 CNY 或 USD')
    return
  }
  if (pricedLines.some((line) => !(Number(batchQuotePrices[line.id]) > 0))) {
    ElMessage.warning('已填写的产品单价必须大于 0')
    return
  }
  savingBatchQuote.value = true
  try {
    const supplierId = await resolveSupplier(batchQuoteForm.supplierId, batchQuoteForm.currency, batchQuoteForm.paymentTerms)
    batchQuoteForm.supplierId = supplierId
    const results = await Promise.all(pricedLines.map((line) => post<{quote:ExecutionQuote}>(`/requirements/${line.id}/execution-quotes`, {
      id:0,supplier_id:supplierId,currency:batchQuoteForm.currency,unit_price:batchQuotePrices[line.id],
      expected_date:batchQuoteForm.expectedDate,payment_terms:batchQuoteForm.paymentTerms,valid_until:batchQuoteForm.validUntil,remark:batchQuoteForm.remark,
    })))
    results.forEach((result, index) => {
      const line = pricedLines[index]!
      executionQuotes[line.id] = [result.quote, ...quotesFor(line.id).filter((item) => item.id !== result.quote.id)]
    })
    batchQuoteOpen.value = false
    ElMessage.success(`已保存这家工厂的 ${results.length} 项产品报价`)
  } finally { savingBatchQuote.value = false }
}
function openQuoteEditor(line: Requirement, quote: ExecutionQuote) {
  quoteLine.value = line
  quoteEditing.value = quote
  Object.assign(quoteForm, {
    supplierId:Number(quote.supplierId),currency:quote.currency,unitPrice:quote.unitPrice,expectedDate:quote.expectedDate,
    paymentTerms:quote.paymentTerms,validUntil:quote.validUntil,remark:quote.remark,
  })
  quoteOpen.value = true
}
async function saveQuote() {
  if (!quoteLine.value || !quoteForm.supplierId || !(Number(quoteForm.unitPrice)>0) || !quoteForm.paymentTerms.trim()) {
    ElMessage.warning('请填写工厂、有效单价和付款条件')
    return
  }
  quoteForm.currency = quoteForm.currency.trim().toUpperCase()
  if (!/^[A-Z]{3}$/.test(quoteForm.currency)) {
    ElMessage.warning('币种请输入 3 位代码，例如 CNY 或 USD')
    return
  }
  savingQuote.value = true
  try {
    const supplierId = await resolveSupplier(quoteForm.supplierId, quoteForm.currency, quoteForm.paymentTerms)
    quoteForm.supplierId = supplierId
    const result = await post<{quote:ExecutionQuote}>(`/requirements/${quoteLine.value.id}/execution-quotes`, {
      id:Number(quoteEditing.value?.id ?? 0),supplier_id:supplierId,currency:quoteForm.currency,unit_price:quoteForm.unitPrice,
      expected_date:quoteForm.expectedDate,payment_terms:quoteForm.paymentTerms,valid_until:quoteForm.validUntil,remark:quoteForm.remark,
    })
    const list = quotesFor(quoteLine.value.id).filter((item) => item.id !== result.quote.id)
    executionQuotes[quoteLine.value.id] = [result.quote, ...list]
    quoteOpen.value = false
    ElMessage.success('实单工厂报价已保存')
  } finally { savingQuote.value = false }
}

async function persistQuoteSelection(line: Requirement, quoteID: number) {
  if (!quoteID) return
  const previous = quotesFor(line.id).find((quote) => quote.selected)
  try {
    const result = await post<{quote:ExecutionQuote}>(`/requirements/${line.id}/execution-quotes/${quoteID}/select`, {})
    executionQuotes[line.id] = quotesFor(line.id).map((quote) => quote.id === result.quote.id ? result.quote : { ...quote, selected:false, selectedById:'0', selectedByName:'', selectedAt:'' })
    selectedQuote[line.id] = quoteID
    ElMessage.success(`已为“${line.productName}”保存最终报价`)
  } catch (error) {
    if (previous) selectedQuote[line.id] = Number(previous.id)
    else delete selectedQuote[line.id]
    throw error
  }
}
async function removeQuote(line: Requirement, quote: ExecutionQuote) {
  await ElMessageBox.confirm(`删除「${quote.supplierName}」的这条报价？`, '删除报价', { type:'warning' })
  await del(`/requirements/${line.id}/execution-quotes/${quote.id}`)
  executionQuotes[line.id] = quotesFor(line.id).filter((item) => item.id !== quote.id)
  if (Number(selectedQuote[line.id]) === Number(quote.id)) delete selectedQuote[line.id]
  ElMessage.success('报价已删除')
}

async function createDraftOrders() {
  const batch = activeBatch.value
  if (!batch || selectedCount.value !== batch.lines.length) { ElMessage.warning('请先为每项产品选择最终工厂报价'); return }
  const groups = draftOrderGroups.value
  saving.value = true
  try {
    for (const group of groups) {
      const quote = group.lines[0]!.quote
      await post('/purchase-orders', {
        supplier_id:Number(quote.supplierId),currency:quote.currency,expected_date:quote.expectedDate,payable_due_date:'',
        remark:quote.paymentTerms,fulfillment_mode:'DIRECT_SHIP',delivery_location_type:'CUSTOM',delivery_address:'按外销合同约定',
        source_change_reason:'实单重新询价后选定工厂',
        lines:group.lines.map(({line,quote}) => ({requirement_id:Number(line.id),qty:line.availableQty,unit_price:quote.unitPrice,execution_quote_id:Number(quote.id)})),
      })
    }
    ElMessage.success(`已生成 ${groups.length} 张采购订单草稿，请检查后提交审批`)
    await router.push({ path:'/purchase-orders', query:{ status:'DRAFT' } })
  } finally { saving.value = false }
}

// This is an exceptional requirement raised independently of a contract.
async function openCreate() {
  createForm.productId = 0
  createForm.qty = ''
  createForm.requiredDate = ''
  createForm.remark = ''
  if (!products.value.length) {
    products.value = (await get<{ products: typeof products.value }>('/products', { page_size: 200 })).products ?? []
  }
  createOpen.value = true
}

async function submitCreate() {
  const product = products.value.find((p) => Number(p.id) === createForm.productId)
  if (!product || !createForm.qty) {
    ElMessage.warning(t('requirements.createRequired'))
    return
  }
  saving.value = true
  try {
    await post('/requirements', {
      product_id: Number(product.id),
      sku_id: 0,
      product_code: product.code,
      product_name: product.name,
      uom_id: Number(product.uomId ?? 0),
      uom_code: product.uomCode ?? '',
      required_qty: createForm.qty,
      required_date: createForm.requiredDate,
      remark: createForm.remark,
    })
    ElMessage.success(t('requirements.created'))
    createOpen.value = false
    reload()
  } finally {
    saving.value = false
  }
}

function openClose(row: Requirement) {
  closing.value = row
  closeReason.value = ''
  closeOpen.value = true
}

async function confirmClose() {
  if (!closing.value) return
  if (!closeReason.value.trim()) {
    ElMessage.warning(t('requirements.reasonRequired'))
    return
  }
  saving.value = true
  try {
    await post(`/requirements/${closing.value.id}/cancel`, { reason: closeReason.value })
    ElMessage.success(t('requirements.closed'))
    closeOpen.value = false
    load()
  } finally {
    saving.value = false
  }
}

// Only lines with something still unbought can go onto an order.
function isOrderable(row: Requirement): boolean {
  if (row.status !== 'PENDING' && row.status !== 'PARTIALLY_ORDERED') return false
  return Number(row.availableQty ?? 0) > 0
}

function canReopen(row: Requirement): boolean {
  return (row.status === 'CANCELLED' || row.status === 'SUPERSEDED')
    && Number(row.orderedQty ?? 0) === 0
}

function onSelect(rows: Requirement[]) {
  selected.value = rows
}

async function exportTemplate() {
  saving.value = true
  try {
    const file = await postDownload('/requirements/purchase-template/export', { requirement_ids: selected.value.map((row) => Number(row.id)) })
    saveBlob(file.blob, file.fileName || 'purchase-import.xlsx')
  } finally { saving.value = false }
}

function openTemplateImport() {
  templateFile.value = null
  templateGroups.value = []
  templateImportOpen.value = true
}

function selectTemplateFile(event: Event) {
  templateFile.value = (event.target as HTMLInputElement).files?.[0] ?? null
  templateGroups.value = []
}

async function fileBase64(file: File) {
  const bytes = new Uint8Array(await file.arrayBuffer())
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return btoa(binary)
}

function base64Blob(value: string): Blob {
  const binary = atob(value)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i)
  return new Blob([bytes], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' })
}

async function previewTemplateImport() {
  if (!templateFile.value) return
  if (templateFile.value.size > 2 * 1024 * 1024) { ElMessage.warning(t('requirements.templateTooLarge')); return }
  saving.value = true
  try {
    const result = await post<{ groups: TemplateGroup[]; errorFileName?: string; errorFileData?: string }>('/purchase-orders/template-imports/preview', { file_data: await fileBase64(templateFile.value), source_file_name: templateFile.value.name })
    if (result.errorFileData) {
      saveBlob(base64Blob(result.errorFileData), result.errorFileName || 'purchase-import-errors.xlsx')
      ElMessage.warning(t('requirements.templateHasErrors'))
      return
    }
    templateGroups.value = result.groups ?? []
    if (!templateGroups.value.length) ElMessage.warning(t('requirements.templateNoGroups'))
  } finally { saving.value = false }
}

async function confirmTemplateImport() {
  saving.value = true
  try {
    const created: { id: string; poNo: string }[] = []
    for (const group of templateGroups.value) {
      const moq = group.lines.filter((line) => line.moq).map((line) => `${line.productName}: MOQ ${line.moq}`).join('; ')
      const remark = [t('requirements.templateImportRemark'), group.paymentTerms ? `${t('requirements.paymentTerms')}: ${group.paymentTerms}` : '', moq].filter(Boolean).join('; ')
      created.push(await post<{ id: string; poNo: string }>(`/purchase-orders/imports/${group.importToken}/confirm`, {
        supplier_id: Number(group.supplierId), currency: group.currency, expected_date: group.expectedDate, remark,
        lines: group.lines.map((line) => ({ row_no: line.rowNo, requirement_id: Number(line.requirementId), qty: line.qty, unit_price: line.unitPrice || '0' })),
      }))
    }
    ElMessage.success(t('requirements.templateOrdersCreated', { n: created.length }))
    templateImportOpen.value = false
    if (created.length === 1) router.push({ path: '/purchase-orders', query: { order: created[0].id } })
    else router.push('/purchase-orders')
  } finally { saving.value = false }
}

async function openDetail(row: Requirement) {
  detail.value = row
  covering.value = []
  detailOpen.value = true
  if (canOrder || auth.can('procurement:order:read')) {
    covering.value = (await get<{ orders: CoveringOrder[] }>(`/requirements/${row.id}/orders`)).orders ?? []
  }
}

async function reopen(row: Requirement) {
  await ElMessageBox.confirm(t('requirements.reopenWarning'), t('requirements.reopen'), {
    type: 'warning',
    confirmButtonText: t('requirements.reopen'),
    cancelButtonText: t('common.cancel'),
  })
  await post(`/requirements/${row.id}/reopen`, {})
  ElMessage.success(t('requirements.reopened'))
  load()
}

// Quantities arrive as exact decimals; "1500.0000 PCS" reads worse than
// "1500 PCS" and means the same thing.
function trimQty(v: string): string {
  if (!v.includes('.')) return v
  return v.replace(/0+$/, '').replace(/\.$/, '')
}

function isOverdue(row: Requirement): boolean {
  if (!row.requiredDate || row.status !== 'PENDING') return false
  return row.requiredDate < new Date().toISOString().slice(0, 10)
}

// Requirements move for reasons nobody on this page did: a contract change
// retires a line or an order is raised.
// A buyer working from a list that went stale minutes ago orders the wrong
// things, so the page re-reads rather than waiting for a manual refresh.
//
// The re-read goes through the normal API instead of trusting the event, so
// permissions are checked exactly as they are on first load.
const stopListening = onLive((event) => {
  if (event.type !== 'requirement.changed') return
  // Not while a dialog is open: swapping the numbers under somebody who is
  // halfway through filling in a form is worse than showing them stale ones.
  if (createOpen.value || closeOpen.value || detailOpen.value || detailBatchKey.value || templateImportOpen.value) return
  load()
})
onUnmounted(stopListening)

watch(() => route.params.batchKey, () => { hydrateActiveBatchFromRoute() })
onMounted(load)
</script>

<style scoped>
.requirements-page {
  --proc-blue: #4ac1ff;
  --proc-green: #1fbf6c;
  --proc-ink: #141817;
  --proc-canvas: #f5f7fb;
  --proc-surface: #fff;
  --el-color-primary: var(--proc-blue);
  --el-color-success: var(--proc-green);
  color: var(--proc-ink);
}
.head-note,
.sub {
  font-size: 12px;
  color: #66727d;
}
.requirements-page :deep(.el-card) {
  border-color: #dfeaf0;
  border-radius: 12px;
  background: var(--proc-surface);
  box-shadow: 0 10px 28px rgb(20 24 23 / 5%);
}
.requirements-page :deep(.el-table) {
  --el-table-header-bg-color: #eef9fe;
  --el-table-header-text-color: #24323a;
  --el-table-row-hover-bg-color: #f0fbf6;
}
.requirements-page :deep(.el-table th.el-table__cell) {
  border-bottom-color: #d9edf5;
  font-weight: 650;
}
.filters {
  display: flex;
  gap: 10px;
  margin-bottom: 14px;
}
.row-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  white-space: nowrap;
}
.row-actions :deep(.el-button) {
  margin-left: 0;
}
.prod {
  font-weight: 500;
}
.batch-no {
  color: #159fdc;
  font-weight: 600;
}
.batch-products {
  line-height: 1.5;
}
.requirements-page :deep(.batch-hierarchy-table .hidden-native-expander .cell) {
  display: none;
  padding: 0;
}
.requirements-page :deep(.batch-hierarchy-table .el-table__expanded-cell) {
  padding: 0 !important;
}
.batch-product-summary {
  display: flex;
  align-items: center;
  width: 100%;
  min-width: 0;
  padding: 7px 8px;
  color: #203747;
  background: transparent;
  border: 0;
  border-radius: 8px;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.batch-product-summary:hover,
.batch-product-summary:focus-visible {
  color: #0f719b;
  background: #edf8fd;
  outline: none;
}
.batch-product-summary--stacked { align-items: stretch; flex-direction: column; }
.batch-product-summary__main { display: flex; align-items: center; min-width: 0; }
.batch-product-summary__mobile-meta { display: none; margin-top: 3px; color: #718390; font-size: 11px; line-height: 1.35; }
.batch-mobile-source { display: none; }
.batch-product-summary__text,
.supplier-summary {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.batch-product-summary__count {
  flex: 0 0 auto;
  padding: 2px 7px;
  margin-left: 9px;
  color: #1479a6;
  background: #e7f6fc;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 650;
}
.batch-product-chevron {
  flex: 0 0 auto;
  margin-left: 9px;
  color: #648395;
  transition: transform .2s ease;
}
.batch-product-chevron.is-expanded { transform: rotate(180deg); }
.batch-product-hierarchy { padding: 10px 14px 12px; background: #f7fbfd; }
.batch-product-group { overflow: hidden; margin-bottom: 8px; background: #fff; border: 1px solid #dbe8ef; border-radius: 9px; }
.batch-product-group:last-child { margin-bottom: 0; }
.batch-product-heading {
  display: grid;
  grid-template-columns: 20px minmax(220px, 2fr) minmax(100px, .65fr) minmax(170px, 1fr);
  align-items: center;
  width: 100%;
  min-height: 46px;
  padding: 8px 14px;
  color: #243f52;
  background: #fff;
  border: 0;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.batch-product-heading:hover,
.batch-product-heading:focus-visible { background: #f1f8fb; outline: none; }
.batch-product-heading .batch-product-chevron { margin-left: 0; }
.batch-product-heading strong { overflow: hidden; color: #143d56; text-overflow: ellipsis; white-space: nowrap; }
.batch-product-heading > span { color: #6b7f8d; font-size: 13px; }
.batch-product-total { text-align: right; }
.batch-specifications { border-top: 1px solid #e2edf2; }
.batch-specification-header,
.batch-specification-row {
  display: grid;
  grid-template-columns: minmax(110px, .6fr) minmax(240px, 1.4fr) minmax(130px, .7fr) minmax(170px, 1fr) minmax(150px, .9fr);
  gap: 18px;
  align-items: center;
  padding: 0 34px;
}
.batch-specification-header { min-height: 34px; color: #7a8b98; background: #f8fbfc; font-size: 12px; font-weight: 650; }
.batch-specification-row { min-height: 58px; padding-top: 9px; padding-bottom: 9px; border-top: 1px solid #edf2f5; color: #426273; }
.batch-specification-header + .batch-specification-row { border-top: 0; }
.batch-specification-row > * { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.batch-specification-row strong { color: #173f56; text-align: right; }
.batch-specification-code { color: #607784; }
.batch-specification-size { color: #334b5c; }
.supplier-group {
  padding: 14px;
  margin-top: 14px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  background: #f5fbf8;
}
.supplier-group-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}
.detail-back { margin-bottom: 12px; }
.inquiry-detail-hero {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 20px 24px;
  margin-bottom: 14px;
  border: 1px solid #cfe4ee;
  border-radius: 14px;
  background: linear-gradient(110deg, #f0faff 0%, #f7fdf9 100%);
}
.inquiry-detail-hero span { color: #648091; font-size: 12px; }
.inquiry-detail-hero h2 { margin: 5px 0 3px; color: #113c55; font-size: 23px; }
.inquiry-detail-hero p { margin: 0; color: #607787; font-size: 13px; }
.inquiry-detail-card :deep(.el-card__body) { padding: 18px 20px 24px; }
.inquiry-tabs :deep(.el-tabs__header) { margin-bottom: 18px; }
.inquiry-tabs :deep(.el-tabs__item) { height: 46px; padding: 0 24px; color: #527084; font-weight: 650; }
.inquiry-tabs :deep(.el-tabs__item.is-active) { color: #0d779d; }
.detail-product-group { overflow: hidden; margin-top: 10px; border: 1px solid #dce8ee; border-radius: 10px; background: #fff; }
.detail-product-group .batch-product-heading { border: 0; }
.detail-spec-list { padding: 0 12px 12px; border-top: 1px solid #e7eff3; background: #f8fbfc; }
.detail-spec-list .requote-product { margin-top: 12px; }
.tab-next { display: flex; justify-content: flex-end; margin-top: 18px; }
.draft-preview { padding: 16px; margin-top: 18px; border: 1px solid #bfe1d4; border-radius: 10px; background: #f5fcf9; }
.draft-preview > div:first-child { display: flex; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.draft-preview > div:first-child strong { color: #174d45; }
.draft-preview > div:first-child span { color: #687f7b; font-size: 13px; }
.draft-preview-row { display: grid; grid-template-columns: minmax(160px, .7fr) minmax(260px, 1.3fr); gap: 16px; padding: 9px 12px; border-top: 1px solid #dfeee8; color: #506b66; }
.draft-preview-row strong { color: #204f48; }
.draft-preview > .el-button { display: block; margin: 16px 0 0 auto; }
.supplier-factories {
  margin-left: 10px;
}
.requote-summary { display:grid;grid-template-columns:repeat(4,1fr);gap:10px;margin-bottom:14px }
.requote-summary>div { padding:11px 13px;border:1px solid #d9edf5;border-radius:9px;background:#f7fcfe }
.requote-summary span { display:block;margin-bottom:4px;color:#66727d;font-size:12px }
.requote-summary strong { color:#173a4d;font-size:14px }
.batch-quote-toolbar { display:flex;align-items:center;justify-content:space-between;gap:16px;margin:14px 0;padding:12px 14px;border:1px solid #ccebf4;border-radius:10px;background:linear-gradient(90deg,#f4fbff,#f5fdf9) }
.batch-quote-toolbar strong { display:block;color:#173a4d;font-size:14px }
.batch-quote-toolbar span { display:block;margin-top:3px;color:#66727d;font-size:12px }
.batch-price-list { width:100%;overflow:hidden;border:1px solid #e1e8ed;border-radius:9px }
.batch-price-line { display:grid;grid-template-columns:minmax(220px,1fr) minmax(260px,1fr);align-items:center;gap:16px;padding:10px 12px;border-bottom:1px solid #edf1f3 }
.batch-price-line:last-child { border-bottom:0 }
.batch-price-line strong,.batch-price-line span { display:block }
.batch-price-line strong { color:#173a4d }
.batch-price-line span { margin-top:3px;color:#66727d;font-size:12px }
.requote-product { margin-top:12px;padding:14px 16px;border:1px solid #dfeaf0;border-radius:10px;background:#fff }
.requote-product-head { display:flex;align-items:center;justify-content:space-between;margin-bottom:10px }
.requote-product-head strong { display:block;color:#173a4d;font-size:16px }
.requote-product-head span { display:block;margin-top:3px;color:#66727d;font-size:12px }
.requote-product-status { display:flex;align-items:center;gap:12px }
.requote-product-status span { margin:0;padding:3px 8px;border-radius:999px;background:#effaf5;color:#16846f;font-size:12px }
.requote-product-status b { color:#087f6f;font-weight:650 }
.presale-reference { display:flex;gap:12px;align-items:center;padding:9px 11px;margin-bottom:10px;border-radius:7px;background:#f5f7fa;color:#66727d;font-size:12px }
.presale-reference>span:first-child { color:#8793a1;font-weight:600 }
.presale-reference strong { color:#344563 }
.quote-options { display:block;width:100% }
.quote-option { position:relative;padding:11px 92px 11px 12px;margin-bottom:8px;border:1px solid #e1e8ed;border-radius:8px;background:#fff }
.quote-option.selected { border-color:#4ac1ff;background:#f3fbff;box-shadow:0 0 0 1px rgb(74 193 255 / 12%) }
.quote-option :deep(.el-radio) { width:100%;height:auto;margin-right:0 }
.quote-option :deep(.el-radio__label) { flex:1 }
.quote-main { display:flex;justify-content:space-between;gap:14px;color:#24323a }
.quote-main b { color:#087f6f;font-variant-numeric:tabular-nums }
.quote-meta { display:flex;gap:16px;margin:6px 0 0 24px;color:#66727d;font-size:12px }
.quote-actions { position:absolute;right:12px;top:9px;display:flex;gap:5px }
.quote-empty { display:flex;align-items:center;gap:10px;margin-top:10px;padding:10px 12px;border:1px dashed #d7e4ea;border-radius:8px;background:#fafcfd;color:#60717c }
.quote-empty span { font-size:13px;font-weight:600 }
.quote-empty small { color:#8a969f;font-size:12px }
.price-row { display:grid;grid-template-columns:110px 1fr;gap:8px;width:100% }
.batch-dialog-intro { display:flex;align-items:center;gap:12px;margin:-4px 0 14px;padding:12px 14px;border-left:4px solid #4ac1ff;border-radius:8px;background:linear-gradient(90deg,#eefaff,#f3fcf7);color:#355466 }
.batch-dialog-intro strong { color:#173a4d;font-size:14px }
.batch-dialog-intro span { color:#687983;font-size:13px }
.batch-quote-form { display:grid;gap:12px }
.batch-form-section { padding:14px 16px;border:1px solid #deebf0;border-radius:10px;background:#fbfdfe }
.batch-section-title { display:flex;align-items:baseline;gap:10px;margin-bottom:12px }
.batch-section-title strong { color:#173a4d;font-size:15px }
.batch-section-title span { color:#81909a;font-size:12px }
.batch-common-grid { display:grid;grid-template-columns:1fr 1fr;gap:0 14px }
.batch-common-grid .field-wide { grid-column:1/-1 }
.batch-quote-form :deep(.el-form-item) { margin-bottom:12px }
.batch-quote-form :deep(.el-form-item__label) { height:auto;padding-bottom:5px;color:#536773;font-size:13px;line-height:20px }
.batch-quote-form .remark-field { margin-bottom:0 }
.product-price-section { padding-bottom:12px }
@media(max-width:1100px){.batch-product-heading{grid-template-columns:20px minmax(150px,1.4fr) 90px minmax(130px,1fr)}.batch-specification-header,.batch-specification-row{grid-template-columns:minmax(100px,.6fr) minmax(190px,1.2fr) minmax(120px,.7fr) minmax(150px,1fr) minmax(130px,.9fr);padding-left:26px;padding-right:26px;gap:12px}}
@media(max-width:900px){.requirements-page :deep(.batch-hierarchy-table .batch-secondary-column){display:none}.batch-product-summary__mobile-meta,.batch-mobile-source{display:block}.requirements-page :deep(.batch-hierarchy-table .el-table__body),.requirements-page :deep(.batch-hierarchy-table .el-table__header){width:100%!important}}
@media(min-width:801px){.batch-mobile-stage{display:none}}
@media(max-width:800px){.requirements-page :deep(.batch-hierarchy-table .batch-status-column){display:none}.batch-mobile-stage{display:inline-flex;margin-top:5px}}
@media(max-width:800px){.inquiry-detail-hero{align-items:flex-start;flex-direction:column}.draft-preview>div:first-child{flex-direction:column}.draft-preview-row{grid-template-columns:1fr}.requote-summary{grid-template-columns:repeat(2,1fr)}.batch-quote-toolbar,.batch-dialog-intro{align-items:flex-start;flex-direction:column}.batch-common-grid,.batch-price-line{grid-template-columns:1fr}.batch-common-grid .field-wide{grid-column:auto}.requote-product-status{align-items:flex-end;flex-direction:column;gap:4px}.quote-empty{align-items:flex-start;flex-direction:column;gap:3px}.presale-reference,.quote-main,.quote-meta{align-items:flex-start;flex-direction:column;gap:4px}.quote-option{padding-right:12px}.quote-actions{position:static;margin-left:24px}.batch-product-hierarchy{padding:8px}.batch-product-heading{grid-template-columns:20px minmax(120px,1fr) auto;gap:8px;padding-left:10px;padding-right:10px}.batch-product-total{grid-column:2/-1;margin-top:-4px;text-align:left}.batch-specification-header{display:none}.batch-specification-row{grid-template-columns:1fr 1fr;padding:10px 18px}.batch-specification-row strong{text-align:left}.batch-specification-row>span:nth-last-child(-n+2){font-size:12px}}
.qty {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}
.ver {
  margin-left: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.reason {
  margin-top: 2px;
  line-height: 1.4;
}
.overdue {
  color: var(--el-color-danger);
  font-weight: 600;
}
.doc-link {
  color: var(--el-color-primary);
  text-decoration: none;
}
.doc-link:hover {
  text-decoration: underline;
}
.alert {
  margin-bottom: 16px;
}
.close-form {
  margin-top: 4px;
}
.desc {
  margin-bottom: 16px;
}
.side-title {
  margin: 4px 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
.pager {
  margin-top: 14px;
  justify-content: flex-end;
}
</style>
