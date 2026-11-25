<template>
  <div class="requirements-page">
    <WorkflowPageHeader v-if="!detailBatchKey" title="实单询价" description="财务放行后的采购任务，需要重新取得工厂报价并选定最终方案。" />

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
        <el-select v-model="quoteStatusFilter" :aria-label="t('requirements.quoteStatus.label')" style="width: 150px" @change="reload">
          <el-option :label="t('requirements.quoteStatus.all')" value="ALL"/>
          <el-option v-for="status in quoteStatuses" :key="status" :label="t(`requirements.quoteStatus.${status}`)" :value="status"/>
        </el-select>
        <el-button @click="reload">{{ t('common.query') }}</el-button>
        <el-button v-if="requirementColumns.customized.value" link @click="requirementColumns.reset">{{ t('common.restoreColumnOrder') }}</el-button>
      </div>

      <el-table class="batch-hierarchy-table" :data="purchaseBatches" row-key="key" v-loading="loading" @row-dblclick="openBatchReview">
        <el-table-column v-for="column in requirementColumns.columns.value" :key="column.key" :min-width="column.minWidth" :width="column.width" :align="column.align" :class-name="column.className" :label-class-name="column.className" :show-overflow-tooltip="column.showOverflowTooltip">
          <template #header><ReorderableTableHeader :label="column.label" :hint="t('common.dragColumnHint')" :move-left-label="t('common.moveColumnLeft')" :move-right-label="t('common.moveColumnRight')" :can-move-left="requirementColumns.canMoveLeft(column.key)" :can-move-right="requirementColumns.canMoveRight(column.key)" @move-left="requirementColumns.moveBy(column.key,-1)" @move-right="requirementColumns.moveBy(column.key,1)" /></template>
          <template #default="{ row }">
            <template v-if="column.key==='batch'">
              <div class="batch-no">{{ row.label }}</div>
              <div v-if="row.additionalContractNos" class="sub">{{ t('requirements.fromContract') }} {{ row.additionalContractNos }}</div>
            </template>
            <span v-else-if="column.key==='customer'">{{ row.customerName || '—' }}</span>
            <div v-else-if="column.key==='products'" class="batch-product-summary batch-product-summary--stacked">
              <span class="batch-product-summary__main"><span class="batch-product-summary__text" :title="row.productGroups[0]?.name">{{ row.productGroups[0]?.name || '—' }}</span><span v-if="row.productGroups.length > 1" class="batch-product-summary__count">+{{ row.productGroups.length - 1 }}</span></span>
              <small>{{ t('requirements.batchDemand', { products: row.productGroups.length, specs: row.lines.length, quantity: row.availableQuantity }) }}</small>
            </div>
            <template v-else-if="column.key==='progress'">
              <el-tag v-if="row.quoteStatus" :type="quoteStatusTone(row.quoteStatus)" effect="light" size="small">{{ t(`requirements.quoteStatus.${row.quoteStatus}`) }}</el-tag>
              <span v-else>—</span>
            </template>
            <span v-else-if="column.key==='receivedDate'">{{ row.receivedDate || '—' }}</span>
            <span v-else-if="column.key==='requiredDate'">{{ row.requiredDate || '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" min-width="125" align="center" fixed="right">
          <template #default="{ row }">
            <el-button type="primary" link @click="openBatchReview(row)">{{ t('requirements.openInquiry') }}</el-button>
          </template>
        </el-table-column>
        <template #empty>{{ t('requirements.empty') }}</template>
      </el-table>

      <el-pagination
        class="pager"
        layout="total, prev, pager, next"
        :total="batchTotal"
        :page-size="pageSize"
        :current-page="page"
        @current-change="(p: number) => { page = p; load() }"
      />
    </el-card>
    </template>

    <template v-else-if="activeBatch">
      <section class="inquiry-detail-hero">
        <div class="inquiry-detail-title">
          <h2>{{ activeBatch.label }}</h2>
          <el-button size="small" @click="backToBatchList">← 返回实单询价</el-button>
        </div>
        <p v-if="activeBatch.additionalContractNos" class="inquiry-detail-source">
          {{ t('requirements.fromContract') }} {{ activeBatch.additionalContractNos }}
        </p>
        <dl class="inquiry-detail-summary">
          <div><dt>客户</dt><dd>{{ activeBatch.customerName || '—' }}</dd></div>
          <div><dt>采购内容</dt><dd>{{ activeBatch.productGroups.length }} 种产品 · {{ activeBatch.lines.length }} 种规格</dd></div>
          <div><dt>待采购量</dt><dd>{{ activeBatch.availableQuantity }}</dd></div>
          <div><dt>最早要求到货</dt><dd>{{ activeBatch.requiredDate || '—' }}</dd></div>
          <div><dt>报价进度</dt><dd title="按当前账号可见的工厂报价统计">{{ loading ? '—' : `${quotedLineCount} / ${activeBatch.lines.length} 个规格` }}</dd></div>
          <div><dt>待处理任务</dt><dd>{{ loading ? '—' : `${pendingTaskCount} 项` }}</dd></div>
        </dl>
      </section>

      <el-card shadow="never" class="inquiry-detail-card" :body-style="{ overflow: 'visible' }">
        <template v-if="batchQuoteOpen">
          <section v-for="note in sourceSalesRemarks(activeBatch)" :key="note.contractNo" class="source-sales-remark">
            <strong>{{ t('inquiryWorkspace.detail.salesRemark') }}<span v-if="note.contractNo && note.contractNo !== activeBatch.label"> · {{ note.contractNo }}</span></strong>
            <p>{{ note.text }}</p>
          </section>
          <h3 class="batch-entry-heading">添加工厂报价</h3>
          <el-form label-position="top" class="batch-quote-form" @submit.prevent>
            <div class="batch-common-grid">
              <el-form-item label="工厂" required>
                <el-select v-model="batchQuoteForm.supplierId" filterable allow-create default-first-option clearable placeholder="选择工厂或输入名称">
                  <el-option v-for="supplier in suppliers" :key="supplier.id" :value="Number(supplier.id)" :label="`${supplier.code} · ${supplier.name}`" />
                </el-select>
              </el-form-item>
              <el-form-item label="币种" required>
                <el-select v-model="batchQuoteForm.currency" filterable allow-create default-first-option @change="batchQuoteForm.currency=String(batchQuoteForm.currency).trim().toUpperCase()">
                  <el-option v-for="currency in currencyOptions" :key="currency" :value="currency" />
                </el-select>
              </el-form-item>
              <el-form-item label="预计交货日期">
                <el-date-picker v-model="batchQuoteForm.expectedDate" value-format="YYYY-MM-DD" clearable />
              </el-form-item>
              <el-form-item label="报价有效期">
                <el-date-picker v-model="batchQuoteForm.validUntil" value-format="YYYY-MM-DD" clearable />
              </el-form-item>
              <el-form-item label="付款条件" required class="batch-payment-field">
                <el-autocomplete v-model="batchQuoteForm.paymentTerms" :fetch-suggestions="suggestPaymentTerms" :debounce="0" clearable placeholder="选择常用条款或手填" />
              </el-form-item>
              <el-form-item label="整份报价备注" class="batch-remark-field">
                <el-input v-model="batchQuoteForm.remark" type="textarea" :rows="2" placeholder="记录包装、交付或其他特殊条件" />
              </el-form-item>
            </div>
            <div class="batch-entry-caption"><strong>产品报价</strong><span>不供应的规格可留空</span></div>
            <el-table :data="activeBatch.lines" row-key="id" border stripe size="small" class="execution-quote-grid">
              <el-table-column :label="t('inquiryWorkspace.list.productDetails')" min-width="270">
                <template #default="{row}">
                  <ComparisonSpecification :product-name="row.productName" :text="requirementProductDescription(row)" hide-name />
                </template>
              </el-table-column>
              <el-table-column label="待采购量" width="125" align="right">
                <template #default="{row}">{{ formatQtyTwo(row.availableQty) }} {{ row.uomCode }}</template>
              </el-table-column>
              <el-table-column label="报价单价" min-width="180">
                <template #default="{row}">
                  <el-input v-model="batchQuotePrices[row.id]" size="small" inputmode="decimal" :aria-label="`报价单价 ${row.productName}`" placeholder="填写单价">
                    <template #append>{{ batchQuoteForm.currency }}<template v-if="row.uomCode"> / {{ row.uomCode }}</template></template>
                  </el-input>
                </template>
              </el-table-column>
            </el-table>
            <footer class="batch-entry-actions">
              <el-button :disabled="savingBatchQuote" @click="batchQuoteOpen=false">返回工厂报价</el-button>
              <el-button type="primary" :loading="savingBatchQuote" @click="saveBatchQuote">保存报价</el-button>
            </footer>
          </el-form>
        </template>

        <div v-else class="inquiry-detail-tabs">
          <el-button v-if="canOrder && activeInquiryTab === 'quotes'" type="primary" class="execution-add-quote" @click="openBatchQuoteEditor">添加工厂报价</el-button>
          <el-tabs v-model="activeInquiryTab" class="inquiry-tabs" :class="{ 'has-quote-action': canOrder && activeInquiryTab === 'quotes' }" @tab-change="onInquiryTabChange">
            <el-tab-pane v-for="tab in inquirySteps" :key="tab" :name="tab" :label="inquiryTabLabels[tab]">
              <template v-if="activeInquiryTab === tab">
                <section v-for="note in sourceSalesRemarks(activeBatch)" :key="note.contractNo" class="source-sales-remark">
                  <strong>{{ t('inquiryWorkspace.detail.salesRemark') }}<span v-if="note.contractNo && note.contractNo !== activeBatch.label"> · {{ note.contractNo }}</span></strong>
                  <p>{{ note.text }}</p>
                </section>
                <div v-if="tab === 'quotes' && canAssign" class="quote-selection-toolbar">
                  <span aria-live="polite">已勾选 <strong>{{ pendingSelections.length }}</strong> 项</span>
                  <el-button
                    size="small" type="primary"
                    :disabled="!pendingSelections.length || dispatchingSelections"
                    @click="selectionReviewOpen = true"
                  >确认选定</el-button>
                </div>
                <el-table
                  v-if="tab === 'quotes'"
                  class="execution-quote-grid"
                  v-loading="loading"
                  :data="factoryQuoteRows"
                  :row-class-name="factoryQuoteRowClass"
                  :span-method="factoryQuoteCellSpan"
                  row-key="key"
                  size="small"
                  border
                >
                  <el-table-column v-if="canAssign" label="选厂" width="62" fixed align="center">
                    <template #default="{row}">
                      <span v-if="row.quote" :title="hasSelectableQuantity(row.line) ? undefined : '该规格没有可派发的剩余数量'">
                        <el-checkbox
                          :model-value="isDispatched(row.quote.id) || quoteSelections[row.line.id]?.quote.id === row.quote.id"
                          :disabled="!canSelectQuote(row.line, row.quote)"
                          :aria-label="`选择 ${row.quote.supplierName} · ${row.line.productName} ${row.line.spec} · ${row.quote.createdByName || '采购员'}`"
                          @change="toggleQuoteSelection(row.line, row.quote, $event === true)"
                        />
                      </span>
                    </template>
                  </el-table-column>
                  <el-table-column prop="line.productName" :label="t('inquiryWorkspace.list.productDetails')" min-width="250">
                    <template #default="{row}">
                      <ComparisonSpecification :product-name="row.line.productName" :text="requirementProductDescription(row.line)" hide-name />
                      <el-button v-if="row.line.presaleReferenceJson || row.line.supplierName || Number(row.line.sourceUnitPrice) > 0" link size="small" class="presale-reference-link" @click="presalePreview=row.line">查看售前参考</el-button>
                    </template>
                  </el-table-column>
                  <el-table-column prop="line.availableQty" label="待采购量" width="105" align="right">
                    <template #default="{row}">{{ formatQtyTwo(row.line.availableQty) }} {{ row.line.uomCode }}</template>
                  </el-table-column>
                  <el-table-column label="报价工厂" min-width="122">
                    <template #default="{row}">
                      <template v-if="row.quote">
                        {{ row.quote.supplierName }}<small v-if="row.quote.createdByName" class="assignment-author">{{ row.quote.createdByName }}</small>
                        <el-tag v-if="row.quote.selected" size="small" type="success" effect="light" class="execution-selected-tag">已选定</el-tag>
                      </template>
                      <el-tag v-else size="small" type="info" effect="plain">{{ canAssign ? '待报价' : '我未报价' }}</el-tag>
                    </template>
                  </el-table-column>
                  <el-table-column label="报价单价" width="139" align="right">
                    <template #default="{row}">
                      <span v-if="row.quote" class="execution-quote-price">
                        {{ row.quote.currency }} {{ formatMoney(row.quote.unitPrice) }}<small v-if="row.line.uomCode"> / {{ row.line.uomCode }}</small>
                      </span>
                      <span v-else>—</span>
                    </template>
                  </el-table-column>
                  <el-table-column label="预计交货日" width="115" align="center">
                    <template #default="{row}">{{ row.quote?.expectedDate || '—' }}</template>
                  </el-table-column>
                  <el-table-column label="操作" width="104" align="center">
                    <template #default="{row}">
                      <el-dropdown v-if="row.quote" trigger="click" @command="handleFactoryQuoteAction($event,row)">
                        <el-button link type="primary" size="small">更多操作 <span aria-hidden="true" class="execution-menu-arrow">▾</span></el-button>
                        <template #dropdown>
                          <el-dropdown-menu>
                            <el-dropdown-item command="view">查看</el-dropdown-item>
                            <el-dropdown-item v-if="canOrder && (canManageQuotes || auth.owns(row.quote.createdById))" command="edit">编辑</el-dropdown-item>
                            <el-dropdown-item v-if="canOrder && (canManageQuotes || auth.owns(row.quote.createdById))" command="delete">删除</el-dropdown-item>
                          </el-dropdown-menu>
                        </template>
                      </el-dropdown>
                      <span v-else>—</span>
                    </template>
                  </el-table-column>
                  <template #empty>暂无可选定的工厂报价</template>
                </el-table>
                <section v-else class="assignment-workspace" v-loading="loading">
                  <section v-if="negotiation && negotiationForm" class="negotiation-terms">
                    <header><strong>议价资料</strong></header>
                    <el-form class="assignment-fields" label-position="top" size="small" @submit.prevent>
                      <el-form-item label="交货日期" required>
                        <el-date-picker v-model="negotiationForm.date" :disabled="assignmentBusy" value-format="YYYY-MM-DD" />
                      </el-form-item>
                      <el-form-item label="付款条件" required>
                        <el-autocomplete v-model="negotiationForm.terms" :disabled="assignmentBusy" :fetch-suggestions="suggestPaymentTerms" :debounce="0" />
                      </el-form-item>
                      <el-form-item label="议价备注">
                        <el-input v-model="negotiationForm.remark" :disabled="assignmentBusy" type="textarea" :autosize="{minRows:1,maxRows:4}" />
                      </el-form-item>
                    </el-form>
                  </section>
                  <h3 v-if="negotiationRows.length" class="negotiation-detail-heading">议价明细</h3>
                  <el-table
                    v-if="negotiationRows.length" :data="negotiationRows" row-key="requirementId"
                    :row-class-name="negotiationRowClass"
                    @row-click="selectNegotiationRow"
                    size="small" class="execution-quote-grid negotiation-table"
                  >
                    <el-table-column label="产品规格" min-width="185">
                      <template #default="{row}">
                        <button type="button" class="negotiation-product" :disabled="assignmentBusy" :aria-pressed="row.task.id === activeNegotiationId" @click.stop="openNegotiation(row.task)">
                          {{ row.task.productName }}
                        </button>
                        <div v-if="row.task.specification" class="sub">{{ row.task.specification }}</div>
                      </template>
                    </el-table-column>
                    <el-table-column label="数量 / 单位" width="98" align="right">
                      <template #default="{row}">{{ formatQtyTwo(row.task.quantity) }} {{ row.task.uomCode }}</template>
                    </el-table-column>
                    <el-table-column label="工厂 / 负责人" min-width="116">
                      <template #default="{row}">
                        {{ row.task.supplierName }}<small class="assignment-author">{{ row.task.buyerName }}</small>
                        <div v-if="row.tasks.length > 1" @click.stop>
                          <el-dropdown trigger="click" @command="(id: string) => selectNegotiationTask(row.requirementId, id)">
                            <el-button link type="info" size="small" :disabled="assignmentBusy">切换任务（{{ row.tasks.length }}） <span aria-hidden="true">▾</span></el-button>
                            <template #dropdown>
                              <el-dropdown-menu>
                                <el-dropdown-item v-for="task in row.tasks" :key="task.id" :command="task.id">
                                  {{ task.supplierName }} · {{ task.buyerName }} · #{{ task.id }}
                                </el-dropdown-item>
                              </el-dropdown-menu>
                            </template>
                          </el-dropdown>
                        </div>
                      </template>
                    </el-table-column>
                    <el-table-column label="领导议价要求" min-width="136">
                      <template #default="{row}"><div class="negotiation-instructions">{{ row.task.instructions || '—' }}</div></template>
                    </el-table-column>
                    <el-table-column label="工厂原报价" width="108" align="right">
                      <template #default="{row}">{{ row.task.initialPrice ? `${row.task.currency} ${formatMoney(row.task.initialPrice)}` : '—' }}</template>
                    </el-table-column>
                    <el-table-column label="议价单价" width="140">
                      <template #default="{row}">
                        <el-input
                          v-if="negotiationEdits[row.task.id]" v-model="negotiationEdits[row.task.id].price"
                          :disabled="assignmentBusy" size="small" inputmode="decimal"
                          :aria-label="`议价单价 ${row.task.productName} ${row.task.specification}`"
                          @focus="openNegotiation(row.task)"
                          @blur="normalizeNegotiationPrice(row.task.id)"
                        ><template #prepend>{{ row.task.currency }}</template></el-input>
                        <span v-else>{{ row.task.negotiatedPrice ? `${row.task.currency} ${formatMoney(row.task.negotiatedPrice)}` : '—' }}</span>
                      </template>
                    </el-table-column>
                  </el-table>
                  <div v-else-if="!loading" class="assignment-empty">
                    <span>领导选定工厂并派发后，议价任务会显示在这里。</span>
                    <el-button v-if="canAssign" link type="primary" @click="replaceInquiryStep('quotes')">前往选厂</el-button>
                  </div>
                  <el-checkbox-group v-if="negotiation?.files?.length && negotiationForm" v-model="negotiationForm.fileIds" :disabled="assignmentBusy" class="negotiation-files" aria-label="随草稿带入的附件">
                    <div v-for="file in negotiation.files" :key="file.id" class="assignment-file-row">
                      <el-checkbox :value="file.id">{{ file.fileName }}</el-checkbox>
                      <el-button link type="primary" @click="downloadNegotiationFile(file)">下载</el-button>
                    </div>
                  </el-checkbox-group>
                  <footer v-if="activeNegotiationTask && (negotiation || activeOrderAction || canCancelActiveNegotiation)" class="negotiation-editor-actions">
                    <div class="negotiation-footer-buttons">
                      <el-button v-if="canCancelActiveNegotiation" link type="danger" :disabled="assignmentBusy" @click="cancelAssignment(activeNegotiationTask)">取消指派</el-button>
                      <el-button v-if="activeOrderAction" size="small" type="primary" @click="openAssignmentOrder(activeNegotiationTask)">{{ activeOrderAction }}</el-button>
                      <template v-if="negotiation">
                        <label class="file-pick"><input type="file" multiple :disabled="assignmentBusy" @change="uploadNegotiationFiles" />上传附件</label>
                        <el-button size="small" :disabled="assignmentBusy" @click="saveNegotiation(false)">保存议价</el-button>
                        <el-button size="small" type="primary" :loading="assignmentBusy" @click="saveNegotiation(true)">生成采购草稿</el-button>
                      </template>
                    </div>
                  </footer>
                </section>
              </template>
            </el-tab-pane>
          </el-tabs>
        </div>
      </el-card>
    </template>
    <el-card v-else shadow="never" v-loading="loading"><el-empty description="未找到这条实单询价"><el-button @click="backToBatchList">返回列表</el-button></el-empty></el-card>

    <el-dialog
      v-model="selectionReviewOpen" title="确认选厂并派发议价任务" width="min(980px, 94vw)"
      @open="checkAssignmentRecipients"
      :close-on-click-modal="false" :close-on-press-escape="!dispatchingSelections" :show-close="!dispatchingSelections"
    >
      <p class="selection-review-hint">确认后，由各报价的原录入人负责议价和生成草稿。</p>
      <el-alert v-if="recipientCheckFailed" :title="t('todoReception.recipientCheckFailed')" type="warning" :closable="false" />
      <el-table :data="pendingSelections" row-key="line.id" size="small" border max-height="440" class="execution-quote-grid">
        <el-table-column label="产品规格" min-width="190">
          <template #default="{row}">
            <ComparisonSpecification :product-name="row.line.productName" :text="requirementProductDescription(row.line)" hide-name />
            <small v-if="row.error" class="selection-error" role="alert">{{ row.error }}</small>
          </template>
        </el-table-column>
        <el-table-column prop="quote.supplierName" label="工厂" min-width="105" />
        <el-table-column label="负责采购员" min-width="135">
          <template #default="{ row }">
            {{ row.quote.createdByName }}
            <small v-if="approvalOnlyRecipients[row.quote.createdById]" class="selection-error">{{ t('todoReception.recipientApprovalsOnly') }}</small>
          </template>
        </el-table-column>
        <el-table-column label="单独要求（选填）" min-width="220">
          <template #default="{row}">
            <el-input
              v-model="row.instructions" size="small"
              :disabled="dispatchingSelections" :aria-label="`单独要求 ${row.line.productName} ${row.line.spec}`"
              placeholder="如目标价或交期要求"
            />
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button :disabled="dispatchingSelections" @click="selectionReviewOpen = false">返回勾选</el-button>
        <el-button type="primary" :loading="dispatchingSelections" :disabled="!pendingSelections.length || checkingRecipients" @click="dispatchSelections">
          确认派发 {{ pendingSelections.length }} 项
        </el-button>
      </template>
    </el-dialog>

    <el-dialog :model-value="!!presalePreview" title="售前参考" width="min(850px,94vw)" destroy-on-close @close="presalePreview=null">
      <template v-if="presalePreview">
        <ComparisonSpecification :product-name="presalePreview.productName" :text="requirementProductDescription(presalePreview)" hide-name />
        <PresaleReference v-if="presalePreview.presaleReferenceJson" :json="presalePreview.presaleReferenceJson" show-specification-remarks />
        <div v-else class="presale-reference">
          <span>售前供应商</span><strong>{{ presalePreview.supplierName || '无售前供应商' }}</strong>
          <span>{{ displaySourcePrice(presalePreview) }}</span><span>{{ presalePreview.sourcePaymentTerms || '未填写付款条件' }}</span>
        </div>
      </template>
      <template #footer><el-button @click="presalePreview=null">关闭</el-button></template>
    </el-dialog>

    <el-dialog :model-value="!!quotePreview" title="工厂报价详情" width="min(760px,94vw)" @close="quotePreview=null">
      <template v-if="quotePreview">
        <el-descriptions :column="2" border class="quote-preview-details">
          <el-descriptions-item :label="t('inquiryWorkspace.list.productDetails')" :span="2"><ComparisonSpecification :product-name="quotePreview.line.productName" :text="requirementProductDescription(quotePreview.line)" hide-name/></el-descriptions-item>
          <el-descriptions-item label="数量">{{ formatQtyTwo(quotePreview.line.availableQty) }} {{ quotePreview.line.uomCode }}</el-descriptions-item>
          <el-descriptions-item label="状态">{{ !quotePreview.quote ? '待报价' : quotePreview.quote.selected ? '已选定' : '已报价' }}</el-descriptions-item>
          <el-descriptions-item label="报价工厂">{{ quotePreview.quote?.supplierName || '待录入报价' }}</el-descriptions-item>
          <el-descriptions-item label="报价单价" :span="2">{{ quotePreview.quote ? `${quotePreview.quote.currency} ${formatMoney(quotePreview.quote.unitPrice)} / ${quotePreview.line.uomCode}` : '—' }}</el-descriptions-item>
          <el-descriptions-item label="交期">{{ quotePreview.quote?.expectedDate || '—' }}</el-descriptions-item>
          <el-descriptions-item label="有效期">{{ quotePreview.quote?.validUntil || '—' }}</el-descriptions-item>
          <el-descriptions-item label="付款条件" :span="2">{{ quotePreview.quote?.paymentTerms || '—' }}</el-descriptions-item>
          <el-descriptions-item label="备注" :span="2">{{ quotePreview.quote?.remark || '—' }}</el-descriptions-item>
          <el-descriptions-item label="选定人">{{ quotePreview.quote?.selectedByName || '—' }}</el-descriptions-item>
          <el-descriptions-item label="售前供应商">{{ quotePreview.line.supplierName || '—' }}</el-descriptions-item>
          <el-descriptions-item label="售前参考价">{{ displaySourcePrice(quotePreview.line) }}</el-descriptions-item>
          <el-descriptions-item label="售前付款条件" :span="2">{{ quotePreview.line.sourcePaymentTerms || '—' }}</el-descriptions-item>
        </el-descriptions>
      </template>
      <template #footer><el-button v-if="canOrder && quotePreview?.quote" type="danger" plain @click="deletePreviewQuote">删除报价</el-button><el-button @click="quotePreview=null">关闭</el-button></template>
    </el-dialog>

    <el-dialog v-model="quoteOpen" :title="`编辑工厂报价 · ${quoteLine?.productName || ''}`" width="620px" destroy-on-close>
      <el-form label-width="110px">
        <el-form-item label="工厂" required><el-select v-model="quoteForm.supplierId" filterable allow-create default-first-option clearable placeholder="输入新工厂名称或选择已有工厂" style="width:100%"><el-option v-for="supplier in suppliers" :key="supplier.id" :value="Number(supplier.id)" :label="`${supplier.code} · ${supplier.name}`" /></el-select></el-form-item>
        <el-form-item label="报价" required><div class="price-row"><el-select v-model="quoteForm.currency" filterable allow-create default-first-option @change="quoteForm.currency=String(quoteForm.currency).trim().toUpperCase()"><el-option v-for="currency in currencyOptions" :key="currency" :value="currency" /></el-select><el-input v-model="quoteForm.unitPrice" placeholder="单价" /></div></el-form-item>
        <el-form-item label="预计交货日期"><el-date-picker v-model="quoteForm.expectedDate" value-format="YYYY-MM-DD" clearable /></el-form-item>
        <el-form-item label="付款条件" required><el-autocomplete v-model="quoteForm.paymentTerms" :fetch-suggestions="suggestPaymentTerms" :debounce="0" clearable placeholder="输入自定义条款或选择常用付款条件" style="width:100%"/></el-form-item>
        <el-form-item label="报价有效期"><el-date-picker v-model="quoteForm.validUntil" value-format="YYYY-MM-DD" clearable /></el-form-item>
        <el-form-item label="补充说明">
          <div class="quote-supplement">
            <el-button link type="primary" :aria-expanded="quoteSupplementOpen" @click="quoteSupplementOpen = !quoteSupplementOpen">
              {{ quoteSupplementOpen ? '收起说明' : quoteForm.remark.trim() ? '查看或编辑已填说明' : '添加说明（选填）' }}
            </el-button>
            <el-input
              v-if="quoteSupplementOpen" v-model="quoteForm.remark" type="textarea" :rows="2"
              aria-label="工厂报价补充说明" placeholder="包装、发货或其他工厂条件"
            />
          </div>
        </el-form-item>
      </el-form>
      <template #footer><el-button @click="quoteOpen=false">取消</el-button><el-button type="primary" :loading="savingQuote" @click="saveQuote">保存报价</el-button></template>
    </el-dialog>

    <el-drawer v-model="detailOpen" :title="detail?.productName" size="620px">
      <el-descriptions :column="2" border size="small" class="desc">
        <el-descriptions-item v-if="detail" :label="t('inquiryWorkspace.list.productDetails')" :span="2"><ComparisonSpecification :product-name="detail.productName" :text="requirementProductDescription(detail)" hide-name/></el-descriptions-item>
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
import ComparisonSpecification from '../components/ComparisonSpecification.vue'
import {formatProductDescription} from '../lib/productDescription'

import {suggestPaymentTerms} from "../lib/paymentTermOptions"
import PresaleReference from '../components/PresaleReference.vue'
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { del, download, get, post, postDownload, quietErrors, saveBlob } from '../api'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'
import { purchaseBatchKey } from '../lib/requirements'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
import ReorderableTableHeader from '../components/ReorderableTableHeader.vue'
import { useTableColumnOrder, type TableColumnDefinition } from '../composables/useTableColumnOrder'

function requirementProductDescription(line:Requirement){
 return formatProductDescription(line.productName,[
  {key:'productCode',label:'产品编码',value:line.productCode},
  {key:'specification',label:t('inquiryProducts.fields.specification'),value:line.spec},
  {key:'contractItemRemark',label:'合同明细备注',value:line.contractItemRemark},
 ])
}
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
  createdAt?: string
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
  contractItemRemark?:string
  presaleReferenceJson?:string
  sourceSalesRemark: string
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
  receivedDate: string
  requiredDate: string
  quoteStatus?: ExecutionQuoteStatus
  additionalContractNos: string
  availableQuantity: string
  statusLabels: string
  waitingRequote: boolean
  productGroups: PurchaseProductGroup[]
  lines: Requirement[]
}

const quoteStatuses = ['MY_QUOTED', 'COLLEAGUE_QUOTED', 'WAITING'] as const
type ExecutionQuoteStatus = typeof quoteStatuses[number]
const quoteStatusTones = { MY_QUOTED: 'success', COLLEAGUE_QUOTED: 'info', WAITING: 'warning' } as const
function quoteStatusTone(status: ExecutionQuoteStatus) {
  return quoteStatusTones[status]
}

interface PurchaseProductGroup {
  key: string
  name: string
  lines: Requirement[]
}

interface Supplier { id: string; code: string; name: string }
interface ExecutionQuote {
  createdById: string
  id: string; requirementId: string; supplierId: string; supplierCode: string; supplierName: string
  currency: string; unitPrice: string; expectedDate: string; paymentTerms: string; validUntil: string
  remark: string; createdByName: string; createdAt: string; updatedAt: string
  selected: boolean; selectedById: string; selectedByName: string; selectedAt: string
  quoteCategory: string; incoterm: string; calculatedUnitPrice: string; calculationInput: string
  calculatedAt: string; calculatedById: string; calculatedByName: string
}

const { t } = useI18n()
const auth = useAuthStore()
const requirementColumnDefaults=computed<TableColumnDefinition[]>(()=>[
 {key:'batch',label:t('requirements.purchaseBatch'),minWidth:170},
 {key:'customer',label:t('requirements.customer'),minWidth:145,showOverflowTooltip:true},
 {key:'products',label:t('requirements.batchProducts'),minWidth:240},
 {key:'progress',label:t('requirements.quoteStatus.label'),minWidth:136},
 {key:'receivedDate',label:t('requirements.executionReceivedDate'),width:116,align:'center'},
 {key:'requiredDate',label:t('requirements.earliestArrival'),width:138},
])
const requirementColumns=useTableColumnOrder('purchase-requirement-list',requirementColumnDefaults)
const router = useRouter()
const route = useRoute()
const canWrite = auth.can('procurement:requirement:write')
// Raising a requirement and committing money to a supplier are separate
// permissions, so the ordering actions are gated separately too.
const canOrder = auth.can('procurement:order:write')
const canReadOrder = auth.can('procurement:order:read')
const canAssign = auth.can('procurement:execution-quote:select')
const canManageQuotes = auth.can('procurement:sourcing:approve')
const detailBatchKey = computed(() => String(route.params.batchKey ?? ''))
const inquirySteps = ['quotes', 'negotiation'] as const
const activeInquiryTab = ref<typeof inquirySteps[number]>('quotes')

const rows = ref<Requirement[]>([])
const batchTotal = ref(0)
const executionQuoteStatuses = ref<Partial<Record<string, ExecutionQuoteStatus>>>({})
const quoteStatusFilter = ref<ExecutionQuoteStatus | 'ALL'>('ALL')
const receivedDateFormat = new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit' })
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
    const key = detailBatchKey.value || purchaseBatchKey(row)
    grouped.set(key, [...(grouped.get(key) ?? []), row])
  }
  return [...grouped.entries()].map(([key, lines]) => {
    const supplierNames = [...new Set(lines.map((line) => line.supplierName).filter(Boolean))]
    const productGroups = groupBatchProducts(lines)
    const label = lines[0]?.contractNo || lines[0]?.quotationNo || t('requirements.manualBatch')
    const receivedAt = lines.reduce((first, line) => {
      const createdAt = Date.parse(line.createdAt ?? '')
      return Number.isFinite(createdAt) ? Math.min(first, createdAt) : first
    }, Infinity)
    return {
      key,
      label,
      customerName: lines[0]?.customerName?.trim() ?? '',
      supplierNames: supplierNames.join('、'),
      supplierCount: supplierNames.length,
      receivedDate: Number.isFinite(receivedAt) ? receivedDateFormat.format(receivedAt) : '',
      requiredDate: [...lines.map((line) => line.requiredDate).filter(Boolean)].sort()[0] ?? '',
      quoteStatus: executionQuoteStatuses.value[key],
      additionalContractNos: [...new Set(lines.map(line => line.contractNo).filter(no => no && no !== label))].join('、'),
      availableQuantity: requirementQuantitySummary(lines, ' + '),
      statusLabels: [...new Set(lines.map((line) => t(`requirements.statuses.${line.status}`)))].join('、'),
      waitingRequote: lines.some((line) => line.status === 'WAITING_REQUOTE'),
      productGroups,
      lines,
    }
  }).sort((a,b) => (a.requiredDate || '9999-12-31').localeCompare(b.requiredDate || '9999-12-31') || a.key.localeCompare(b.key))
})
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
function sourceSalesRemarks(batch: PurchaseBatch): {contractNo:string;text:string}[] {
  const notes = new Map<string,string>()
  for (const line of batch.lines) {
    const text = line.sourceSalesRemark?.trim()
    if (text) notes.set(line.contractNo || line.quotationNo || line.id, text)
  }
  return [...notes].map(([contractNo,text]) => ({contractNo,text}))
}
function formatQtyTwo(value: string): string {
  const quantity = Number(value)
  return Number.isFinite(quantity) ? quantity.toFixed(2) : value || '—'
}
function requirementQuantitySummary(lines: Requirement[], separator?: string): string {
  const totals = new Map<string, number>()
  const unstructured: string[] = []
  for (const line of lines) {
    const quantity = Number(line.availableQty)
    const unit = line.uomCode.trim()
    if (Number.isFinite(quantity)) totals.set(unit, (totals.get(unit) || 0) + quantity)
    else if (line.availableQty) unstructured.push(`${line.availableQty}${unit ? ` ${unit}` : ''}`)
  }
  return [...totals].map(([unit, total]) => `${separator ? trimQty(total.toFixed(2)) : total.toFixed(2)}${unit ? ` ${unit}` : ''}`).concat(unstructured).join(separator ?? '；') || '—'
}
const page = ref(1)
const pageSize = 100
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

const executionQuotes = reactive<Record<string, ExecutionQuote[]>>({})
const batchQuoteOpen = ref(false)
const savingBatchQuote = ref(false)
const batchQuoteForm = reactive({ supplierId:null as number|string|null, currency:'CNY', expectedDate:'', paymentTerms:'', validUntil:'', remark:'' })
const batchQuotePrices = reactive<Record<string, string>>({})
const quoteOpen = ref(false)
const quoteSupplementOpen = ref(false)
const presalePreview = ref<Requirement | null>(null)
const quotePreview = ref<{line:Requirement;quote:ExecutionQuote|null}|null>(null)
const quoteLine = ref<Requirement | null>(null)
const quoteEditing = ref<ExecutionQuote | null>(null)
const savingQuote = ref(false)
const quoteForm = reactive({ supplierId:null as number|string|null, currency:'CNY', unitPrice:'', expectedDate:'', paymentTerms:'', validUntil:'', remark:'' })

interface FactoryQuoteRow {
  key: string
  line: Requirement
  quote: ExecutionQuote | null
  specificationRows: number
}
// List each specification's quotes together, including specifications awaiting a quote.
const factoryQuoteRows = computed(() => (activeBatch.value?.lines ?? []).flatMap<FactoryQuoteRow>(line => {
  const quotes = quotesFor(line.id)
  if (!quotes.length) return [{key:`${line.id}:empty`,line,quote:null,specificationRows:1}]
  return quotes.map((quote, index) => ({
    key: `${line.id}:${quote.id}`,
    line,
    quote,
    specificationRows: index === 0 ? quotes.length : 0,
  }))
}))

function factoryQuoteRowClass({ row }: { row: FactoryQuoteRow }) {
  return row.specificationRows > 0 ? 'execution-spec-start' : ''
}
function factoryQuoteCellSpan({ row, column }: { row: FactoryQuoteRow; column: { property?: string } }) {
  if (column.property !== 'line.productName' && column.property !== 'line.availableQty') return
  return row.specificationRows > 0 ? [row.specificationRows, 1] : [0, 0]
}

const quotedLineCount = computed(() => (activeBatch.value?.lines ?? []).filter((line) => quotesFor(line.id).length > 0).length)
type AssignmentFile = { id: string; fileName: string; contentType: string }
interface ExecutionAssignment {
  id: string; requirementId: string; quoteId: string; buyerId: string; buyerName: string; supplierName: string
  productName: string; specification: string; currency: string; initialPrice: string; negotiatedPrice: string
  quantity: string; uomCode: string; instructions: string; expectedDate: string; paymentTerms: string
  remark: string; poId: string; orderStatus: string; files?: AssignmentFile[]
}
const assignments = ref<ExecutionAssignment[]>([])
const selectedNegotiationTasks = ref<Record<string, string>>({})
const pendingTaskCount = computed(() => assignments.value.filter(needsNegotiationAction).length)
const negotiationRows = computed(() => {
  const byRequirement = new Map<string, ExecutionAssignment[]>()
  for (const task of assignments.value) {
    const group = byRequirement.get(task.requirementId) ?? []
    group.push(task)
    byRequirement.set(task.requirementId, group)
  }
  return Array.from(byRequirement, ([requirementId, group]) => {
    const tasks = [...group].reverse()
    const task = tasks.find(item => item.id === selectedNegotiationTasks.value[requirementId])
      ?? tasks.find(needsNegotiationAction)
      ?? tasks[0]!
    return {requirementId, task, tasks}
  })
})
interface QuoteSelection {
  line: Requirement
  quote: ExecutionQuote
  instructions: string
  error: string
}
const quoteSelections = ref<Record<string, QuoteSelection>>({})
const pendingSelections = computed(() => Object.values(quoteSelections.value))
const selectionReviewOpen = ref(false)
const approvalOnlyRecipients = ref<Record<string, boolean>>({})
const recipientCheckFailed = ref(false)
const checkingRecipients = ref(false)
let recipientCheckRequest = 0

async function checkAssignmentRecipients() {
  const request = ++recipientCheckRequest
  const ids = [...new Set(pendingSelections.value.map(row => row.quote.createdById).filter(id => /^[1-9]\d*$/.test(id)))]
  approvalOnlyRecipients.value = {}
  recipientCheckFailed.value = false
  checkingRecipients.value = true
  try {
    const results: Record<string, boolean> = {}
    for (let index = 0; index < ids.length; index += 100) {
      const response = await post<{ approvalsOnly: Record<string, boolean> }>(
        '/execution-assignments/todo-reception', { employeeIds: ids.slice(index, index + 100) }, quietErrors,
      )
      Object.assign(results, response.approvalsOnly)
    }
    if (request === recipientCheckRequest) approvalOnlyRecipients.value = results
  } catch {
    if (request === recipientCheckRequest) recipientCheckFailed.value = true
  } finally {
    if (request === recipientCheckRequest) checkingRecipients.value = false
  }
}
const dispatchingSelections = ref(false)
const assignmentBusy = ref(false)
interface NegotiationEdit {
  price: string
  storedPrice: string
  date: string
  terms: string
  remark: string
  fileIds: string[]
}
const negotiationEdits = ref<Record<string, NegotiationEdit>>({})
const activeNegotiationId = ref('')
const activeNegotiationTask = computed(() => assignments.value.find(task => task.id === activeNegotiationId.value))
const negotiation = computed(() => {
  const task = activeNegotiationTask.value
  return task && canHandleNegotiation(task) ? task : undefined
})
const activeOrderAction = computed(() => {
  const task = activeNegotiationTask.value
  return task && hasOrder(task) && canReadOrder ? assignmentOrderAction(task) : ''
})
const canCancelActiveNegotiation = computed(() => canAssign && !!activeNegotiationTask.value && !hasOrder(activeNegotiationTask.value))
const negotiationForm = computed(() => negotiationEdits.value[activeNegotiationId.value])
function negotiationRowClass({row}: {row: {task: ExecutionAssignment}}) {
  return row.task.id === activeNegotiationId.value ? 'negotiation-current-row' : ''
}
const inquiryTabLabels = computed(() => ({
  quotes: `工厂报价与选定 ${quotedLineCount.value}/${activeBatch.value?.lines.length ?? 0}`,
  negotiation: `议价任务 (${assignments.value.length})`,
}))
const detailOpen = ref(false)
const detail = ref<Requirement | null>(null)
const covering = ref<CoveringOrder[]>([])
interface TemplateLine { rowNo: number; requirementId: string; productName: string; qty: string; uomCode: string; unitPrice: string; moq: string }
interface TemplateGroup { importToken: string; supplierId: string; supplierCode: string; supplierName: string; currency: string; expectedDate: string; paymentTerms: string; lines: TemplateLine[] }
const templateImportOpen = ref(false)
const templateFile = ref<File | null>(null)
const templateGroups = ref<TemplateGroup[]>([])

const common = (k: string) => t(`common.${k}`)

let requirementsRequest = 0
async function load() {
  const request = ++requirementsRequest
  const batchKey = detailBatchKey.value
  loading.value = true
  try {
    const data = await get<{
      requirements: Requirement[]
      executionQuoteStatuses?: Partial<Record<string, ExecutionQuoteStatus>>
      meta?: { total: string | number }
    }>('/requirements', batchKey
      ? {view:'execution_batches', batch_key:batchKey}
      : {view:'execution_batches', page:page.value, page_size:pageSize, keyword:keyword.value,
          quote_status:quoteStatusFilter.value === 'ALL' ? undefined : quoteStatusFilter.value})
    if (request !== requirementsRequest) return
    if (!batchKey) {
      batchTotal.value = Number(data.meta?.total ?? 0)
      const lastPage = Math.max(1, Math.ceil(batchTotal.value / pageSize))
      if (page.value > lastPage) {
        page.value = lastPage
        await load()
        return
      }
    }
    rows.value = (data.requirements ?? []).filter(line => batchKey || Number(line.availableQty ?? 0) > 0)
    executionQuoteStatuses.value = data.executionQuoteStatuses ?? {}
    await hydrateActiveBatchFromRoute()
  } catch {
    // The API shows the request error. Preserve the last successfully loaded view.
  } finally {
    if (request === requirementsRequest) loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

async function openBatchReview(batch: PurchaseBatch) {
  activeBatch.value = batch
  activeInquiryTab.value = 'quotes'
  await router.push({ path: `/requirements/${encodeURIComponent(batch.key)}` })
}

function routeInquiryStep(): 'quotes'|'negotiation' {
  return route.query.step === 'negotiation' || route.query.step === 'selection' ? 'negotiation' : 'quotes'
}

function replaceInquiryStep(step: 'quotes'|'negotiation') {
  activeInquiryTab.value = step
  const query = { ...route.query }
  if (step === 'negotiation') query.step = 'negotiation'
  else { delete query.step; delete query.assignment }
  return router.replace({ path: route.path, query })
}

function onInquiryTabChange(value: string | number) {
  const step = value === 'negotiation' ? 'negotiation' : 'quotes'
  if (routeInquiryStep() !== step) void replaceInquiryStep(step)
}

async function hydrateActiveBatchFromRoute() {
  if (!detailBatchKey.value) {
    activeBatch.value = null
    return
  }
  const batch = purchaseBatches.value.find((item) => item.key === detailBatchKey.value) ?? null
  activeBatch.value = batch
  if (!batch) return
  activeInquiryTab.value = routeInquiryStep()
  await Promise.all([loadBatchQuotes(batch), loadAssignments(batch)])
  focusLinkedNegotiation()
}

async function loadBatchQuotes(batch: PurchaseBatch) {
  if (canOrder && !suppliers.value.length) suppliers.value = (await get<{suppliers:Supplier[]}>('/suppliers', { page_size: 200, status: 'ACTIVE' })).suppliers ?? []
  await Promise.all(batch.lines.map(async (line) => {
    const result = await get<{quotes:ExecutionQuote[]}>(`/requirements/${line.id}/execution-quotes`)
    executionQuotes[line.id] = result.quotes ?? []
  }))
}

function backToBatchList() { router.push('/requirements') }

function quotesFor(requirementId: string) { return executionQuotes[requirementId] ?? [] }
function formatMoney(value: string) { const amount = Number(value); return Number.isFinite(amount) ? amount.toFixed(2) : value || '—' }
function displaySourcePrice(line: Requirement) {
  return Number(line.sourceUnitPrice) > 0 ? `${line.sourceCurrency || ''} ${trimQty(line.sourceUnitPrice)}/${line.uomCode}` : '未带入售前价格'
}
function openBatchQuoteEditor() {
  if (!canOrder || savingBatchQuote.value) return
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
  if (!canOrder || savingBatchQuote.value) return
  const pricedLines = (activeBatch.value?.lines ?? []).filter((line) => String(batchQuotePrices[line.id] ?? '').trim() !== '')
  if (!batchQuoteForm.supplierId || !batchQuoteForm.paymentTerms.trim() || !pricedLines.length) {
    ElMessage.warning('请选择工厂，填写付款条件，并至少填写一个产品单价')
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
async function handleFactoryQuoteAction(command: string, { line, quote }: FactoryQuoteRow) {
  if (!quote) return
  if (command === 'view') {
    quotePreview.value = { line, quote }
  } else if (canOrder && command === 'edit') {
    openQuoteEditor(line, quote)
  } else if (canOrder && command === 'delete') {
    await removeQuote(line, quote)
  }
}
function openQuoteEditor(line: Requirement, quote: ExecutionQuote) {
  quoteSupplementOpen.value = false
  quoteLine.value = line
  quoteEditing.value = quote
  Object.assign(quoteForm, {
    supplierId:Number(quote.supplierId),currency:quote.currency,unitPrice:quote.unitPrice,expectedDate:quote.expectedDate,
    paymentTerms:quote.paymentTerms,validUntil:quote.validUntil,remark:quote.remark ?? '',
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

function hasOrder(task: ExecutionAssignment) {
  return Number(task.poId) > 0
}
function needsNegotiationAction(task: ExecutionAssignment) {
  return !hasOrder(task) || task.orderStatus === 'DRAFT' || task.orderStatus === 'REJECTED'
}
function canHandleNegotiation(task: ExecutionAssignment) {
  return !hasOrder(task) && canOrder && (canManageQuotes || auth.owns(task.buyerId))
}
function formatNegotiationPrice(value: string) {
  const price = value.trim()
  return /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$/.test(price) ? formatMoney(price) : price
}
function normalizeNegotiationPrice(taskId: string) {
  const form = negotiationEdits.value[taskId]
  if (form) form.price = formatNegotiationPrice(form.price)
}
function assignmentOrderAction(task: ExecutionAssignment) {
  if (task.orderStatus === 'REJECTED') return '打开原草稿'
  return task.orderStatus === 'DRAFT' ? '打开草稿' : '查看采购单'
}
function isDispatched(quoteId: string) {
  return assignments.value.some(task => task.quoteId === quoteId && task.orderStatus !== 'CANCELLED')
}
function hasOpenAssignment(requirementId: string) {
  return assignments.value.some(task => task.requirementId === requirementId && !hasOrder(task))
}
function hasSelectableQuantity(line: Requirement) {
  return Number(line.availableQty) > 0
}
function canSelectQuote(line: Requirement, quote: ExecutionQuote) {
  return canAssign && !dispatchingSelections.value && hasSelectableQuantity(line)
    && !isDispatched(quote.id) && !hasOpenAssignment(line.id)
}
async function loadAssignments(batch: PurchaseBatch) {
  const pages = await Promise.all(batch.lines.map(line => post<{assignments?: ExecutionAssignment[]}>('/execution-assignments', {action:'list', requirement_id:line.id})))
  if (detailBatchKey.value !== batch.key) return
  assignments.value = pages.flatMap(page => page.assignments ?? [])
  const editable = assignments.value.filter(canHandleNegotiation)
  for (const task of editable) {
    const storedPrice = task.negotiatedPrice || task.initialPrice || ''
    negotiationEdits.value[task.id] ??= {
      price: formatNegotiationPrice(storedPrice),
      storedPrice,
      date: task.expectedDate || '',
      terms: task.paymentTerms || '',
      remark: task.remark || '',
      fileIds: (task.files ?? []).map(file => file.id),
    }
  }
  const editableIds = new Set(editable.map(task => task.id))
  for (const id of Object.keys(negotiationEdits.value)) {
    if (!editableIds.has(id)) delete negotiationEdits.value[id]
  }
  if (!activeNegotiationTask.value) {
    const first = negotiationRows.value.find(row => canHandleNegotiation(row.task))?.task ?? editable[0] ?? negotiationRows.value[0]?.task
    activeNegotiationId.value = first?.id ?? ''
    if (first) selectedNegotiationTasks.value[first.requirementId] = first.id
  }
}
function toggleQuoteSelection(line: Requirement, quote: ExecutionQuote, checked: boolean) {
  if (!canSelectQuote(line, quote)) return
  if (checked) {
    quoteSelections.value[line.id] = {line, quote, instructions: '', error: ''}
  } else if (quoteSelections.value[line.id]?.quote.id === quote.id) {
    delete quoteSelections.value[line.id]
  }
}
async function dispatchSelections() {
  const batch = activeBatch.value
  const selections = pendingSelections.value
  if (!canAssign || !batch || !selections.length || dispatchingSelections.value) return
  dispatchingSelections.value = true
  const sent = new Set<string>()
  try {
    for (const selection of selections) {
      selection.error = ''
      try {
        await post('/execution-assignments', {
          action: 'assign',
          requirement_id: selection.line.id,
          quote_id: selection.quote.id,
          instructions: selection.instructions.trim(),
        }, quietErrors)
        sent.add(selection.line.id)
      } catch (error) {
        selection.error = typeof error === 'object' && error && 'message' in error && typeof error.message === 'string'
          ? error.message
          : '派发未确认，请重试'
      }
    }
    if (activeBatch.value !== batch) return
    try {
      await loadAssignments(batch)
      // A response can be lost after the task was created. Confirm against the refreshed list.
      for (const selection of selections) {
        if (isDispatched(selection.quote.id)) sent.add(selection.line.id)
      }
    } catch {
      ElMessage.warning('议价任务未能刷新，可重新打开详情查看派发结果')
    }
    if (activeBatch.value !== batch) return
    for (const lineId of sent) delete quoteSelections.value[lineId]
    if (sent.size === selections.length) {
      selectionReviewOpen.value = false
      ElMessage.success(`已派发 ${sent.size} 项议价任务`)
      await replaceInquiryStep('negotiation')
    } else {
      ElMessage.warning(`已派发 ${sent.size} 项，另有 ${selections.length - sent.size} 项未确认，请查看行内提示`)
    }
  } finally {
    dispatchingSelections.value = false
  }
}
async function cancelAssignment(task: ExecutionAssignment) {
  try { await ElMessageBox.confirm('取消后可为这个规格重新选厂。', '取消议价任务', {type:'warning'}) } catch { return }
  await post('/execution-assignments', {action:'cancel', requirement_id:task.requirementId, id:task.id})
  if (activeBatch.value) await loadAssignments(activeBatch.value)
}
function openNegotiation(task: ExecutionAssignment) {
  if (assignmentBusy.value) return
  activeNegotiationId.value = task.id
  selectedNegotiationTasks.value[task.requirementId] = task.id
}
function selectNegotiationRow(row: {task: ExecutionAssignment}) {
  openNegotiation(row.task)
}
function selectNegotiationTask(requirementId: string, id: string) {
  if (assignmentBusy.value) return
  const task = assignments.value.find(item => item.id === id && item.requirementId === requirementId)
  if (task) openNegotiation(task)
}
function focusLinkedNegotiation() {
  if (routeInquiryStep() !== 'negotiation') return
  const target = assignments.value.find(task => task.id === String(route.query.assignment ?? ''))
  if (target) openNegotiation(target)
}
function openAssignmentOrder(task: ExecutionAssignment) {
  return router.push({path:'/purchase-orders', query:{order:task.poId, status:task.orderStatus || 'DRAFT'}})
}
async function saveNegotiation(createDraft: boolean) {
  const task = negotiation.value
  const form = negotiationForm.value
  if (!task || !form || assignmentBusy.value) return
  const displayedPrice = formatNegotiationPrice(form.price)
  const unitPrice = displayedPrice === formatNegotiationPrice(form.storedPrice) ? form.storedPrice : displayedPrice
  assignmentBusy.value = true
  try {
    const target = {id:task.id, requirement_id:task.requirementId}
    await post('/execution-assignments', {...target, action:'negotiate', negotiated_price:unitPrice,
      expected_date:form.date, payment_terms:form.terms, remark:form.remark})
    form.storedPrice = unitPrice
    form.price = displayedPrice
    if (createDraft) {
      const result = await post<{orderId:string}>('/execution-assignments', {...target, action:'draft', file_ids:form.fileIds})
      ElMessage.success('采购草稿已生成')
      await router.push({path:'/purchase-orders',query:{status:'DRAFT',order:result.orderId}})
    } else {
      if (activeBatch.value) await loadAssignments(activeBatch.value)
      ElMessage.success('已保存议价结果')
    }
  } finally { assignmentBusy.value = false }
}
async function uploadNegotiationFiles(event: Event) {
  const input=event.target as HTMLInputElement
  const files = [...(input.files ?? [])]
  input.value = ''
  const task = negotiation.value
  const form = negotiationForm.value
  if (!task || !form || !files.length || assignmentBusy.value) return
  assignmentBusy.value = true
  try {
    for (const file of files) {
      if (file.size > 10*1024*1024) { ElMessage.warning(`${file.name} 超过 10 MB`); continue }
      const {id} = await post<{id:string}>('/execution-assignments', { action:'upload', requirement_id:task.requirementId, id:task.id,
        file_name:file.name, content_type:file.type, file_data:await fileBase64(file) })
      form.fileIds.push(id)
    }
  } finally {
    assignmentBusy.value = false
    if (activeBatch.value) await loadAssignments(activeBatch.value)
  }
}
async function downloadNegotiationFile(file: AssignmentFile) {
  const task = negotiation.value
  if (!task) return
  const data = await post<{fileData:string;fileName:string;contentType:string}>('/execution-assignments',
    {action:'download',requirement_id:task.requirementId,id:task.id,file_id:file.id})
  const bytes = Uint8Array.from(atob(data.fileData), character=>character.charCodeAt(0))
  saveBlob(new Blob([bytes],{type:data.contentType}),data.fileName)
}

async function deletePreviewQuote() {
  const preview = quotePreview.value
  if (!preview?.quote) return
  await removeQuote(preview.line, preview.quote)
  if (!quotesFor(preview.line.id).some(quote => quote.id === preview.quote?.id)) quotePreview.value = null
}
async function removeQuote(line: Requirement, quote: ExecutionQuote) {
  await ElMessageBox.confirm(`删除「${quote.supplierName}」的这条报价？`, '删除报价', { type:'warning' })
  await del(`/requirements/${line.id}/execution-quotes/${quote.id}`)
  executionQuotes[line.id] = quotesFor(line.id).filter((item) => item.id !== quote.id)
  ElMessage.success('报价已删除')
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

watch(detailBatchKey, () => {
  batchQuoteOpen.value = false
  presalePreview.value = null
  assignments.value = []
  selectedNegotiationTasks.value = {}
  negotiationEdits.value = {}
  activeNegotiationId.value = ''
  quoteSelections.value = {}
  selectionReviewOpen.value = false
  void load()
})
watch(() => route.query.step, () => {
  activeInquiryTab.value = routeInquiryStep()
  if (activeInquiryTab.value === 'negotiation') batchQuoteOpen.value = false
})
watch(() => route.query.assignment, focusLinkedNegotiation)
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
.requirements-page :deep(.batch-hierarchy-table .el-table__row) { cursor: pointer; }
.inquiry-detail-hero {
  display: grid;
  gap: 8px;
  padding: 13px 16px;
  border: 1px solid #143f5a;
  border-radius: 10px;
  background: linear-gradient(120deg, #143f5a, #176b87);
  color: #fff;
}
.inquiry-detail-title { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 12px; }
.inquiry-detail-title h2 { margin: 0; font-size: 22px; line-height: 1.25; overflow-wrap: anywhere; }
.inquiry-detail-title .el-button { margin-left: auto; }
.inquiry-detail-source { margin: 0; color: #d2e8f1; font-size: 12px; overflow-wrap: anywhere; }
.inquiry-detail-summary { display: flex; align-items: baseline; flex-wrap: wrap; gap: 6px 24px; margin: 0; }
.inquiry-detail-summary div { display: flex; align-items: baseline; flex-wrap: wrap; gap: 3px 7px; min-width: 0; }
.inquiry-detail-summary dt { color: #d2e8f1; font-size: 12px; }
.inquiry-detail-summary dd { margin: 0; font-size: 13px; font-weight: 600; overflow-wrap: anywhere; }
.inquiry-detail-card { overflow: visible; }
.inquiry-detail-card :deep(.el-card__body) { padding: 0 12px 12px; }
.inquiry-detail-tabs { position: relative; }
.execution-add-quote { position: absolute; top: 5px; right: 0; z-index: 1; }
.inquiry-tabs.has-quote-action :deep(.el-tabs__header) { padding-right: 130px; }
.inquiry-tabs { min-width: 0; }
.inquiry-tabs :deep(.el-tabs__header) { margin: 0; border-bottom: 1px solid #e1e8ed; }
.inquiry-tabs :deep(.el-tabs__nav-wrap::after) { display: none; }
.inquiry-tabs :deep(.el-tabs__item) { height: 42px; padding: 0 15px; color: #527084; font-weight: 650; }
.inquiry-tabs :deep(.el-tabs__item.is-active) { color: #0d779d; }
@media (max-width: 430px) {
  .execution-add-quote { position: static; margin-top: 6px; }
  .inquiry-tabs.has-quote-action :deep(.el-tabs__header) { padding-right: 0; }
}
.pricing-category { margin: 9px 0; overflow: hidden; border: 1px solid #c9dce8; border-radius: 10px; background: #fff; }
.pricing-category__head { display: flex; align-items: center; justify-content: space-between; min-height: 44px; padding: 7px 14px; background: #edf6fa; border-bottom: 1px solid #d7e6ee; cursor: pointer; }
.pricing-category__head:hover { background: #e7f3f8; }
.pricing-category__head.is-empty { min-height: 38px; border-bottom:0; background:#f7fafb; cursor:default; }
.pricing-category__head.is-empty:hover { background:#f7fafb; }
.pricing-category__head>div { display: flex; align-items: baseline; gap: 11px; }
.pricing-category__head strong { color: #23485d; font-size: 15px; font-weight:600; }
.pricing-category__head span { color: #6a8290; font-size: 13px; }
.pricing-category__head .pricing-category__eyebrow { color: #4b7b92; font-size: 11px; font-weight: 500; letter-spacing: .03em; }
.trade-term-chevron { flex:0 0 auto; color:#5f8193; transition:transform .18s ease; }
.trade-term-chevron.is-expanded { transform:rotate(180deg); }
.category-calculation { display: grid; grid-template-columns: minmax(340px,1.15fr) minmax(500px,2fr); gap: 14px; padding: 10px 14px; border-bottom: 1px solid #e0eaf0; background: #f9fcfd; }
.category-formula { display: flex; flex-direction: column; justify-content: center; gap: 5px; min-width: 0; }
.category-formula>span { color: #27789c; font-size: 12px; font-weight: 500; }
.category-formula>strong { color: #294b5e; font-size: 14px; font-weight:600; line-height: 1.45; }
.category-formula>small { color: #718591; line-height: 1.45; }
.category-inputs { display: grid; grid-template-columns: repeat(3,minmax(145px,1fr)); gap: 8px; align-items: end; }
.category-inputs label { display: flex; flex-direction: column; gap: 5px; color: #496879; font-size: 12px; font-weight: 600; }
.category-empty { padding: 16px 18px; color: #8a9aa4; text-align: center; background: #fff; }
.quote-calculation { margin: 0; padding: 0; border: 0; background: transparent; }
.calculation-result { display: flex; align-items: center; justify-content: flex-end; gap: 7px; margin: 0; color: #607b8a; font-size: 11px; white-space: nowrap; }
.calculation-result strong { color: #17617a; font-size: 13px; font-weight:600; }
.execution-files { margin: 7px 0; padding: 8px 10px; border: 1px solid #d7e5ed; border-radius: 7px; background: #fbfdfe; }
.execution-files>header { display:flex; align-items:center; justify-content:space-between; gap:16px; }
.execution-files>header>div:first-child { display:flex; flex-direction:column; gap:2px; min-width:0; }
.execution-files>header strong { color:#294a58; font-size:13px; font-weight:600; }
.execution-files>header span { color:#708591; font-size:11px; }
.execution-file-toolbar { display:flex; align-items:center; gap:9px; white-space:nowrap; }
.execution-file-toolbar small { color:#708591; font-size:11px; }
.file-pick { padding:5px 10px; border:1px solid #75b9d8; border-radius:6px; color:#0879aa; cursor:pointer; white-space:nowrap; background:#fff; font-size:12px; line-height:1.2; }
.file-pick input { display:none; }
.supplier-factories {
  margin-left: 10px;
}
.presale-reference { display:flex;gap:12px;align-items:center;padding:9px 11px;margin-bottom:10px;border-radius:7px;background:#f5f7fa;color:#66727d;font-size:12px }
.presale-reference>span:first-child { color:#8793a1;font-weight:600 }
.presale-reference strong { color:#344563 }
.price-row { display:grid;grid-template-columns:110px 1fr;gap:8px;width:100% }
.batch-entry-heading { margin: 0; padding: 12px 0 8px; color: #173a4d; font-size: 15px; }
.batch-common-grid {
  display: grid;
  grid-template-columns: minmax(200px, 1.4fr) minmax(90px, .55fr) repeat(2, minmax(130px, 1fr));
  align-items: start;
  gap: 8px 12px;
  padding-bottom: 9px;
}
.batch-common-grid :is(.batch-payment-field, .batch-remark-field) { grid-column: span 2; }
.batch-quote-form :deep(.el-form-item) { min-width: 0; margin-bottom: 0; }
.batch-quote-form :deep(.el-form-item__label) { height: auto; padding-bottom: 4px; color: #536773; font-size: 13px; line-height: 1.4; }
.batch-quote-form :deep(.el-select),
.batch-quote-form :deep(.el-date-editor),
.batch-quote-form :deep(.el-autocomplete) { width: 100%; }
.batch-entry-caption { display: flex; align-items: baseline; flex-wrap: wrap; gap: 6px 10px; padding: 7px 0; color: #173a4d; font-size: 13px; }
.batch-entry-caption span { color: #607787; font-size: 12px; }
.batch-entry-actions { position: sticky; bottom: 0; z-index: 6; display: flex; justify-content: space-between; gap: 8px; padding: 8px 0; border-top: 1px solid #dce5ed; background: #fff; }
@media (max-width: 830px) {
  .batch-common-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .batch-common-grid :is(.batch-payment-field, .batch-remark-field) { grid-column: 1 / -1; }
}
@media (max-width: 480px) {
  .batch-common-grid { grid-template-columns: 1fr; }
}
@media(max-width:1100px){.category-calculation{grid-template-columns:1fr}.category-inputs{grid-template-columns:repeat(2,minmax(150px,1fr))}.batch-product-heading{grid-template-columns:20px minmax(150px,1.4fr) 90px minmax(130px,1fr)}.batch-specification-header,.batch-specification-row{grid-template-columns:minmax(100px,.6fr) minmax(190px,1.2fr) minmax(120px,.7fr) minmax(150px,1fr) minmax(130px,.9fr);padding-left:26px;padding-right:26px;gap:12px}}
@media(max-width:900px){.requirements-page :deep(.batch-hierarchy-table .batch-secondary-column){display:none}.batch-product-summary__mobile-meta,.batch-mobile-source{display:block}.requirements-page :deep(.batch-hierarchy-table .el-table__body),.requirements-page :deep(.batch-hierarchy-table .el-table__header){width:100%!important}}
@media(min-width:801px){.batch-mobile-stage{display:none}}
@media(max-width:800px){.requirements-page :deep(.batch-hierarchy-table .batch-status-column){display:none}.batch-mobile-stage{display:inline-flex;margin-top:5px}}
@media(max-width:800px){.pricing-category__head{align-items:flex-start;flex-direction:column;gap:8px}.pricing-category__head>div{align-items:flex-start;flex-direction:column;gap:4px}.category-calculation{grid-template-columns:1fr;padding:12px}.category-inputs{grid-template-columns:1fr}.execution-files>header{align-items:flex-start;flex-direction:column}.calculation-result{justify-content:flex-start}.batch-product-hierarchy{padding:8px}.batch-product-heading{grid-template-columns:20px minmax(120px,1fr) auto;gap:8px;padding-left:10px;padding-right:10px}.batch-product-total{grid-column:2/-1;margin-top:-4px;text-align:left}.batch-specification-header{display:none}.batch-specification-row{grid-template-columns:1fr 1fr;padding:10px 18px}.batch-specification-row strong{text-align:left}.batch-specification-row>span:nth-last-child(-n+2){font-size:12px}}
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
.source-sales-remark { padding: 7px 10px; border-bottom: 1px solid #dce5ed; background: #f2f8fb; color: #17485d; font-size: 12px; }
.source-sales-remark strong span { font-weight: 400; }
.source-sales-remark p { margin: 2px 0 0; white-space: pre-wrap; overflow-wrap: anywhere; color: #334e5c; }
.execution-quote-grid { width: 100%; --el-table-border-color: #dce5ed; --el-table-header-bg-color: #f4f7fb; }
.execution-quote-grid :deep(th.el-table__cell) { padding: 7px 0; color: #294b60; font-size: 12px; }
.execution-quote-grid :deep(td.el-table__cell) { padding: 5px 0; font-size: 13px; vertical-align: middle; }
.execution-quote-grid :deep(.cell) { padding: 0 8px; line-height: 19px; word-break: normal; overflow-wrap: anywhere; }
.execution-quote-grid :deep(.execution-spec-start:not(:first-child) td) { border-top: 1px solid #bfd5e1; }
 .execution-selected-tag { margin-left: 4px; }
.assignment-author { display:block;color:#607787; }
.assignment-workspace { padding-top:4px; }
.negotiation-instructions { white-space: pre-wrap; overflow-wrap: anywhere; }
.negotiation-table :deep(.negotiation-current-row td) { background: #f0f8fc; }
.negotiation-table :deep(td.el-table__cell) { cursor: pointer; }
.negotiation-product { padding: 0; border: 0; background: none; color: #284e60; font: inherit; font-weight: 600; cursor: pointer; }
.negotiation-product:hover { text-decoration: underline; }
.negotiation-terms header { padding: 8px 10px 0; color: #284e60; font-size: 13px; }
.negotiation-detail-heading { margin: 8px 0 0; padding: 7px 10px; color: #284e60; border-top: 1px solid #e1e8ed; font-size: 14px; }
.negotiation-table :deep(.el-input-group__prepend) { padding: 0 6px; }
.negotiation-files { padding: 4px 10px; }
.negotiation-editor-actions { display: flex; justify-content: flex-end; padding: 10px; border-top: 1px solid #e1e8ed; }
.negotiation-footer-buttons { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
.negotiation-footer-buttons .el-button { margin-left: 0; }
.negotiation-footer-buttons .el-button--primary { --el-button-bg-color: #176f85; --el-button-border-color: #176f85; --el-button-hover-bg-color: #218399; --el-button-hover-border-color: #218399; }
.assignment-empty { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 14px; padding: 12px; color: #526a7b; font-size: 13px; }
.assignment-empty :deep(.el-button) { padding: 0; min-height: 0; }
.quote-selection-toolbar { display: flex; align-items: center; gap: 12px; padding: 7px 10px; background: #f4f8fb; color: #345367; font-size: 13px; }
.selection-review-hint { margin: 0 0 10px; color: #526a7b; }
.quote-supplement { display: grid; gap: 6px; width: 100%; }
.quote-supplement .el-button { justify-self: start; }
.selection-error { display: block; margin-top: 4px; color: var(--el-color-danger-dark-2); }
.assignment-fields { display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px;padding: 8px 12px; }
.assignment-fields :deep(.el-date-editor),.assignment-fields :deep(.el-autocomplete) { width:100%; }
.assignment-fields :deep(.el-form-item) { margin-bottom: 0; }
@media (max-width: 650px) { .assignment-fields { grid-template-columns: 1fr; } }
.assignment-file-row { display:flex;justify-content:space-between;gap:12px; }
.execution-quote-price { font-variant-numeric: tabular-nums; }
.execution-quote-price small { color: #607787; white-space: nowrap; }
.execution-menu-arrow { margin-left: 3px; }
.presale-reference-link { display: flex; min-height: 0; width: max-content; padding: 0; color: #557b90; font-size: 11px; }
.execution-quote-grid :deep(.el-checkbox) { height:22px; }
.quote-preview-details :deep(.el-descriptions__content) { white-space:pre-wrap; overflow-wrap:anywhere; }
.factory-quote-cards .quote-option { padding-right:170px; }
@media(max-width:800px) { .factory-quote-cards .quote-option { padding-right:12px; } }
</style>
