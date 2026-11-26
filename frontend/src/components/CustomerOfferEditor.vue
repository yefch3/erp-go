<template>
 <section v-loading="busy" class="quote-workspace" :class="{'compare-mode':isCompare,'customer-mode':!isCompare}">
  <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon/>
  <template v-if="offer">
   <template v-if="isCompare">
    <div class="section-tools"><el-input v-model="search" :placeholder="t('inquiryWorkspace.offer.searchPlaceholder')" clearable/><div class="category-visibility-tools"><el-checkbox v-model="showEmptyCategories">{{t('inquiryWorkspace.offer.showEmptyCategories')}}</el-checkbox><span>{{t('inquiryWorkspace.offer.toolHint')}}</span></div></div>
    <el-empty v-if="!visibleGroups.length" :description="t('inquiryWorkspace.offer.noSupplierCategories')" :image-size="70"/>
    <section v-for="group in visibleGroups" :key="group.value" class="workspace-card category-panel" :class="'category-'+group.value.toLowerCase()">
     <div class="card-heading compact-category-heading compact-category-header">
      <div class="compact-category-main">
      <div class="compact-category-identity"><h3><el-popover trigger="click" placement="bottom-start" :width="560" :popper-style="{maxWidth:'calc(100vw - 32px)'}"><template #reference><el-button link class="category-formula-name" :title="t('inquiryWorkspace.offer.categoryFormulaHint')" :aria-label="`${group.label} · ${t('inquiryWorkspace.offer.viewFormula')}`">{{group.label}}</el-button></template><div class="compact-formula-content"><strong>{{t(`inquiryWorkspace.offer.${formulaKey(group.value)}`)}}</strong><p>{{t(group.value==='DIRECT_CFR_USD'?'inquiryWorkspace.offer.directCalculationHint':'inquiryWorkspace.offer.categoryAutoCalculationHint')}}</p></div></el-popover></h3></div>
      <div class="manual-inputs">
       <label v-if="group.currency==='CNY'">{{t('inquiryWorkspace.offer.exchangeRate')}}<el-input v-model="categoryCalculation(group.value).quoteFx" size="small" :aria-label="t('inquiryWorkspace.offer.exchangeRate')" :disabled="!editable" inputmode="decimal" :placeholder="t('inquiryWorkspace.offer.parameterExample',{value:'8.5'})" :title="t('inquiryWorkspace.offer.exchangeRatePlaceholder')"/></label>
       <label v-if="needsInlandFreight(group.value)">{{t('inquiryWorkspace.offer.compactInlandFreight')}}<el-input v-model="categoryCalculation(group.value).inlandFreight" size="small" :aria-label="t('inquiryWorkspace.offer.productInlandFreightUnitPrice')" :disabled="!editable" inputmode="decimal" :placeholder="t('inquiryWorkspace.offer.parameterExample',{value:'0'})" :title="t('inquiryWorkspace.offer.cnyPerUnit')"/></label>
       <label v-if="needsPortCharge(group.value)">{{t('inquiryWorkspace.offer.compactPortCharge')}}<el-input v-model="categoryCalculation(group.value).portCharge" size="small" :aria-label="t('inquiryWorkspace.offer.portChargeUnitPrice')" :disabled="!editable" inputmode="decimal" :placeholder="t('inquiryWorkspace.offer.parameterExample',{value:'0'})" :title="t('inquiryWorkspace.offer.cnyPerUnit')"/></label>
       <label v-if="group.value==='REPROCESSING_CNY'">{{t('inquiryWorkspace.offer.compactLoss')}}<el-input v-model="categoryCalculation(group.value).loss" size="small" :aria-label="t('inquiryWorkspace.offer.lossUnitPrice')" :disabled="!editable" inputmode="decimal" :placeholder="t('inquiryWorkspace.offer.parameterExample',{value:'0'})" :title="t('inquiryWorkspace.offer.cnyPerUnit')"/></label>
       <label>{{t('inquiryWorkspace.offer.annualInterestRate')}}<el-input v-model="categoryCalculation(group.value).interestRate" class="interest-rate-input" size="small" :aria-label="t('inquiryWorkspace.offer.annualInterestRate')" :disabled="!editable" inputmode="decimal" :placeholder="t('inquiryWorkspace.offer.parameterExample',{value:'6'})" :title="t('inquiryWorkspace.offer.interestRatePlaceholder')"><template #append>%</template></el-input></label>
       <label>{{t('inquiryWorkspace.offer.interestDays')}}<el-input v-model="categoryCalculation(group.value).interestDays" class="interest-days-input" size="small" :aria-label="t('inquiryWorkspace.offer.interestDays')" :disabled="!editable" inputmode="numeric" :placeholder="t('inquiryWorkspace.offer.parameterExample',{value:'60'})" :title="t('inquiryWorkspace.offer.interestDaysPlaceholder')"><template #append>{{t('inquiryWorkspace.offer.daysUnit')}}</template></el-input></label>
      </div>
      </div>
      <div class="group-count compact-quote-count" :title="`${t('inquiryWorkspace.offer.productQuoteCounts',{products:productGroupsFor(group.value).length,quotes:rowsFor(group.value).length})} · ${t('inquiryWorkspace.offer.selectedChoiceCounts',{products:selectedProductCount(group.value),quotes:selectedCount(group.value)})}`">{{t('inquiryWorkspace.offer.compactQuoteCounts',{products:productGroupsFor(group.value).length,quotes:rowsFor(group.value).length,selected:selectedCount(group.value)})}}</div>
      <span v-if="categoryCalculationStatus(group.value)" class="automatic-calculation-status" role="status" aria-live="polite" :class="{'pending':categoryCalculationIssue(group.value)||automaticError}">{{categoryCalculationStatus(group.value)}}</span>
     </div>

     <el-empty v-if="!productGroupsFor(group.value).length" :description="t(search?'inquiryWorkspace.offer.noSearchMatch':'inquiryWorkspace.offer.categoryNoQuotes')" :image-size="40"/>
     <table v-else class="comparison-product-table description-comparison-table" :aria-label="`${group.label} · ${t('inquiryWorkspace.offer.productDescription')}`">
      <colgroup><col :style="{width:group.value==='DIRECT_CFR_USD'?'45%':'33%'}"/><col :style="{width:group.value==='DIRECT_CFR_USD'?'10%':'9%'}"/><col :style="{width:group.value==='DIRECT_CFR_USD'?'28%':'21%'}"/><col v-if="group.value!=='DIRECT_CFR_USD'" style="width:23%"/><col :style="{width:group.value==='DIRECT_CFR_USD'?'17%':'14%'}"/></colgroup>
      <thead><tr class="comparison-column-headings"><th scope="col">{{t('inquiryWorkspace.offer.productDescription')}}</th><th scope="col" class="quantity-cell">{{t('inquiryWorkspace.offer.quantity')}}</th><th scope="col">{{t('inquiryWorkspace.offer.supplierQuote')}}</th><th v-if="group.value!=='DIRECT_CFR_USD'" scope="col">{{t('inquiryWorkspace.offer.productLogisticsUsd')}}</th><th scope="col" class="cfr-cell">{{t('inquiryWorkspace.offer.cfrUnitPrice')}}</th></tr></thead>
      <tbody><tr v-for="row in comparisonSpecificationRows(productGroupsFor(group.value))" :key="row.key" :class="{'selected-row':selectedRows(row).length}">
       <td><ComparisonSpecification :product-name="row.product.product" :text="comparisonSpecificationSentence(row.product)" hide-name/></td>
       <td class="quantity-cell"><span class="number-cell">{{formatQuantity(row.product.quantity)}} {{row.product.unit}}</span></td>
       <td>
        <el-select :model-value="specificationQuoteId(row)" filterable clearable :reserve-keyword="false" :disabled="!editable"
         :placeholder="t('inquiryWorkspace.offer.selectSupplierQuotes')"
         :aria-label="t('inquiryWorkspace.offer.specificationSupplierAria',{product:row.product.product,specification:specification(row.product)})"
         style="width:100%" @change="selectSpecificationQuote(row,$event)">
         <el-option v-for="candidate in supplierOptionsFor(row)" :key="candidate.quote.id" :value="candidate.quote.id"
          :label="supplierChoiceLabel(candidate)" class="supplier-choice-option">
          <div class="supplier-choice-line">
           <span>{{supplierChoicePrice(candidate)}}</span>
           <strong>{{supplierChoiceName(candidate)}}</strong>
           <el-button link type="primary" size="small" @click.stop="preview=candidate.quote">{{t('inquiryWorkspace.offer.view')}}</el-button>
          </div>
          <small v-if="candidate.price.remark?.trim()" class="supplier-choice-remark" :title="candidate.price.remark.trim()">
           <strong>{{t('inquiryWorkspace.quotes.specificationRemark')}}</strong> {{candidate.price.remark.trim()}}
          </small>
          <small v-if="candidate.quote.body.remark?.trim()" class="supplier-choice-remark" :title="candidate.quote.body.remark.trim()">
           <strong>{{t('inquiryWorkspace.quotes.overallRemark')}}</strong> {{candidate.quote.body.remark.trim()}}
          </small>
          <small>{{t('inquiryWorkspace.offer.deliveryDate')}}: {{candidate.price.delivery||candidate.quote.body.delivery||'—'}}<template v-if="candidate.category==='REPROCESSING_CNY'"> · {{t('inquiryWorkspace.offer.exFactoryUnitPrice')}}: {{formatCfrUnitPrice(candidate.price.factoryPrice)||'—'}}</template></small>
         </el-option>
        </el-select>
        <small v-if="specificationQuoteIds(row).length>1" class="pending supplier-reselect">{{t('inquiryWorkspace.offer.reselectSingleSupplier')}}</small>
       </td>
       <td v-if="group.value!=='DIRECT_CFR_USD'&&row.key===row.productGroup.specs[0].key" :rowspan="row.productGroup.specs.length">
        <div class="description-logistics">
         <el-select :model-value="productFreightId(row.productGroup)" :disabled="!editable"
          :placeholder="t(hasProductFreight(row.productGroup)?'inquiryWorkspace.offer.productFreightMixed':'inquiryWorkspace.offer.logisticsSelectPlaceholder')"
          :aria-label="t('inquiryWorkspace.offer.productLogisticsAria',{product:row.productGroup.productName})"
          clearable class="description-logistics-select" size="small" @change="selectProductFreight(row.productGroup,$event)">
          <template #label="{value,label}">
           <span class="logistics-selection-label" :title="label">
            <strong>{{freightChoice(row.productGroup,value).price}}</strong>
            <span>{{freightChoice(row.productGroup,value).name}}</span>
           </span>
          </template>
          <el-option v-for="quote in logisticsQuotes" :key="quote.id" :value="quote.id" :disabled="!productFreightAvailable(row.productGroup,quote)"
           :label="productFreightChoiceLabel(row.productGroup,quote)" :title="productFreightLabel(row.productGroup,quote)" class="product-freight-option freight-choice-option">
           <div class="freight-choice-line">
            <span>{{productFreightPriceLabel(groupProducts(row.productGroup),quote)}}</span>
            <strong>{{productFreightName(quote)}}</strong>
            <el-button link type="primary" size="small" @click.stop="preview=quote">{{t('inquiryWorkspace.offer.viewFreightDetails')}}</el-button>
           </div>
           <small>{{quote.body.route||t('inquiryWorkspace.offer.routeMissing')}}<template v-if="quote.body.departure||quote.body.arrival"> · {{quote.body.departure||'—'}} → {{quote.body.arrival||'—'}}</template></small>
          </el-option>
         </el-select>
        </div>
       </td>
       <td class="cfr-cell"><strong v-if="specificationQuoteIds(row).length===1&&selectedRows(row).length" class="single-cfr">{{candidateCfrText(selectedRows(row)[0])}}</strong><span v-else-if="specificationQuoteIds(row).length>1" class="pending">{{t('inquiryWorkspace.offer.reselectSingleSupplier')}}</span><span v-else>—</span></td>
      </tr></tbody>
     </table>
    </section>
    <details :key="props.caseId" class="workspace-card logistics-panel">
     <summary class="logistics-reference-summary"><strong>{{t('inquiryWorkspace.offer.transportCosts')}}</strong><span class="logistics-reference-actions"><span>{{t('inquiryWorkspace.offer.logisticsAvailable',{count:logisticsQuotes.length})}}</span><span class="logistics-reference-expand">{{t('inquiryWorkspace.offer.expandLogisticsReference')}}</span><span class="logistics-reference-collapse">{{t('inquiryWorkspace.offer.collapseLogisticsReference')}}</span><i class="logistics-reference-chevron" aria-hidden="true"/></span></summary>
     <el-table :data="logisticsQuotes" class="quote-table" :empty-text="t('inquiryWorkspace.offer.noLogisticsQuote')">
      <el-table-column :label="t('inquiryWorkspace.offer.logisticsCompany')" min-width="230"><template #default="{row}"><strong>{{row.body.company||row.body.carrier||t('inquiryWorkspace.offer.logisticsMissing')}}</strong><small>{{routeOf(row)}}</small></template></el-table-column>
      <el-table-column :label="t('inquiryWorkspace.offer.departureArrival')" min-width="160"><template #default="{row}">{{row.body.departure||'—'}}<small>{{row.body.arrival||'—'}}</small></template></el-table-column>
      <el-table-column :label="t('inquiryWorkspace.offer.validUntil')" width="135"><template #default="{row}">{{row.body.validUntil||'—'}}</template></el-table-column>
      <el-table-column :label="t('inquiryWorkspace.offer.details')" width="90"><template #default="{row}"><el-button link type="primary" @click="preview=row">{{t('inquiryWorkspace.offer.view')}}</el-button></template></el-table-column>
     </el-table>
    </details>
   </template>
   <template v-else>
    <el-alert v-if="sourceChanges.selectionChanged&&customerRows.length" :title="t('inquiryWorkspace.offer.selectionPendingAlert')" type="warning" :closable="false" show-icon/>
    <el-alert v-if="sourceChanges.sources.length" :title="t('inquiryWorkspace.offer.sourceChangedAlert')" type="warning" :closable="false" show-icon>
     <ul><li v-for="change in sourceChanges.sources" :key="change.id">{{sourceChangeDescription(change)}}</li></ul>
    </el-alert>
    <el-alert v-if="offer.status==='CONFIRMED'" :title="t('inquiryWorkspace.offer.confirmedSnapshotHint')" type="info" :closable="false" show-icon/>
    <details class="workspace-card customer-information quote-terms"><summary>{{t('inquiryWorkspace.offer.quoteInfo')}} <span v-if="offer.body.selectionSavedAt" class="saved-at">{{t('inquiryWorkspace.offer.savedAt',{time:displayTime(offer.body.selectionSavedAt)})}}</span></summary>
     <el-form class="customer-fields" label-position="top" size="small" :disabled="!editable">
      <el-form-item :label="t('inquiryWorkspace.offer.quoteValidity')">
       <el-date-picker v-model="offer.body.validUntil" value-format="YYYY-MM-DD"/>
      </el-form-item>
      <el-form-item :label="t('inquiryWorkspace.offer.quoteDelivery')">
       <el-date-picker v-model="offer.body.delivery" value-format="YYYY-MM-DD"/>
      </el-form-item>
      <el-form-item :label="t('inquiryWorkspace.offer.paymentTerms')">
       <el-autocomplete v-model="offer.body.payment" :fetch-suggestions="suggestPaymentTerms" :debounce="0" clearable :placeholder="t('suppliers.paymentTermPlaceholder')"/>
      </el-form-item>
      <el-form-item :label="t('inquiryWorkspace.offer.quoteRemark')">
       <el-input v-model="offer.body.remark" type="textarea" :autosize="{minRows:1,maxRows:5}"/>
      </el-form-item>
     </el-form>
    </details>
    <section v-if="offer.status==='CONFIRMED'" class="workspace-card"><div class="card-heading"><h3>{{t('inquiryWorkspace.offer.confirmedDetails')}}</h3></div><el-table :data="offer.body.lines" class="quote-table"><el-table-column prop="product" :label="t('inquiryWorkspace.offer.product')"/><el-table-column :label="t('inquiryWorkspace.offer.quantity')"><template #default="{row}">{{formatCfrUnitPrice(row.quantity)||'—'}}</template></el-table-column><el-table-column prop="unit" :label="t('inquiryWorkspace.offer.unit')"/><el-table-column :label="t('inquiryWorkspace.offer.customerUnitPrice')"><template #default="{row}">{{offer.body.currency}} {{formatCfrUnitPrice(row.unitPrice)||'—'}}</template></el-table-column><el-table-column :label="t('inquiryWorkspace.offer.amount')"><template #default="{row}">{{formatCfrUnitPrice(row.amount)||'—'}}</template></el-table-column></el-table></section>
    <el-empty v-if="!customerRows.length&&offer.status!=='CONFIRMED'" :description="t('inquiryWorkspace.offer.noCustomerPlan')"><el-button type="primary" @click="emit('navigate','quotes')">{{t('inquiryWorkspace.offer.goCompare')}}</el-button></el-empty>
    <section v-for="group in customerGroups" :key="group.value" class="workspace-card category-panel negotiation-panel">
     <header><strong>{{group.label}}</strong><span>{{t('inquiryWorkspace.offer.candidateSuppliers',{count:customerSupplierCount(group.rows)})}}</span></header>
     <div class="negotiation-scroll">
      <table class="negotiation-grid">
       <thead><tr>
        <th scope="col">
          <el-checkbox class="customer-plan-all" :model-value="group.allSelected" :indeterminate="group.partiallySelected"
            :disabled="!editable" :aria-label="t('inquiryWorkspace.offer.selectAllCustomerPlans',{group:group.label})"
            @change="chooseAllAccepted(group.rows,!!$event)">{{t('inquiryWorkspace.offer.customerSelectedPlan')}}</el-checkbox>
        </th>
        <th>{{t('inquiryWorkspace.offer.productDescription')}}</th>
        <th>{{t('inquiryWorkspace.offer.qtyUnit')}}</th>
        <th>{{t('inquiryWorkspace.offer.selectedSupplier')}}</th>
        <th>{{t('inquiryWorkspace.offer.calculatedCustomerPrice')}}</th>
        <th>{{t('inquiryWorkspace.offer.customerQuotePrice')}}</th>
       </tr></thead>
       <tbody><tr v-for="row in group.rows" :key="selectionKey({productId:row.product.id,category:row.category,quoteId:row.quote.id})">
        <td :data-label="t('inquiryWorkspace.offer.customerSelectedPlan')"><el-checkbox :aria-label="t('inquiryWorkspace.offer.customerSelectedPlan')+' · '+row.product.product" :model-value="customerPlanAccepted(row)" :disabled="!editable" @change="chooseAccepted(row,!!$event)">{{t(customerPlanAccepted(row)?'inquiryWorkspace.offer.planSelected':'inquiryWorkspace.offer.planPending')}}</el-checkbox></td>
        <td :data-label="t('inquiryWorkspace.offer.productDescription')"><ComparisonSpecification :product-name="row.product.product" :text="comparisonSpecificationSentence(row.product)" hide-name/></td>
        <td :data-label="t('inquiryWorkspace.offer.qtyUnit')">{{formatQuantity(row.product.quantity)}} {{row.product.unit}}</td>
        <td :data-label="t('inquiryWorkspace.offer.selectedSupplier')">
         <strong>{{row.quote.body.company||'—'}}</strong>
         <el-popover v-if="row.price.remark?.trim()||row.quote.body.remark?.trim()" trigger="click"
          placement="right-start" :width="360" :popper-style="{maxWidth:'calc(100vw - 24px)'}">
          <template #reference>
           <el-button link type="primary" size="small" class="customer-factory-remark-trigger">{{t('inquiryWorkspace.quotes.viewFactoryRemarks')}}</el-button>
          </template>
          <dl class="customer-factory-remark-details">
           <template v-if="row.price.remark?.trim()">
            <dt>{{t('inquiryWorkspace.quotes.specificationRemark')}}</dt>
            <dd>{{row.price.remark.trim()}}</dd>
           </template>
           <template v-if="row.quote.body.remark?.trim()">
            <dt>{{t('inquiryWorkspace.quotes.overallRemark')}}</dt>
            <dd>{{row.quote.body.remark.trim()}}</dd>
           </template>
          </dl>
         </el-popover>
        </td>
        <td :data-label="t('inquiryWorkspace.offer.calculatedCustomerPrice')">USD {{formatCfrUnitPrice(initialCalculatedPrice(row))||'—'}} / {{row.product.unit}}</td>
        <td :data-label="t('inquiryWorkspace.offer.customerQuotePrice')"><el-input v-model="negotiationFor(row).proposedPrice" size="small" :disabled="!editable" inputmode="decimal" :aria-label="t('inquiryWorkspace.offer.customerQuotePrice')+' · '+row.product.product" :title="t('inquiryWorkspace.offer.customerQuotePricePlaceholder')" placeholder="—" @blur="normalizeProposedPrice(row)"><template #prepend>USD</template></el-input></td>
       </tr></tbody>
      </table>
     </div>
    </section>
    <section class="document-panel"><div><h3>{{t('inquiryWorkspace.offer.customerQuotationDocument')}}</h3><p>{{t('inquiryWorkspace.offer.documentHint')}}</p></div><div class="document-actions"><label>{{t('inquiryWorkspace.offer.documentLanguage')}}<el-select v-model="offer.body.documentLanguage" :disabled="!editable"><el-option v-for="option in documentLanguages" :key="option.value" :value="option.value" :label="option.label"/></el-select></label><el-tooltip :content="documentTooltip"><span><el-button :disabled="!canGenerateDocument" :loading="busy" type="primary" @click="generateDocument">{{t('inquiryWorkspace.offer.generateDocument')}}</el-button></span></el-tooltip></div></section>
   </template>
   <footer class="workspace-footer"><div><strong>{{t(isCompare?'inquiryWorkspace.offer.compareSummary':'inquiryWorkspace.offer.customerSummary',isCompare?{products:selections.length,logistics:selectedFreightQuoteIds.length}:customerSelectionCounts)}}</strong><small>{{t(dirty?'inquiryWorkspace.offer.unsaved':isCompare?'inquiryWorkspace.offer.compareFooterHint':'inquiryWorkspace.offer.offerFooterHint')}}</small></div><div class="footer-actions"><el-button @click="refreshSources" :disabled="busy">{{t('inquiryWorkspace.offer.refreshSources')}}</el-button><el-button v-if="!isCompare" @click="emit('navigate','quotes')">{{t('inquiryWorkspace.offer.backToCompare')}}</el-button><el-button v-if="editable" @click="persist(false)">{{t(isCompare?'inquiryWorkspace.offer.saveSelection':'inquiryWorkspace.offer.saveQuoteInfo')}}</el-button><el-button v-if="isCompare&&editable" type="primary" :disabled="!selections.length" @click="persist(true)">{{t(customerRows.length?'inquiryWorkspace.offer.updateCustomerSelection':'inquiryWorkspace.offer.addToCustomerQuote')}}</el-button><el-button v-if="!isCompare&&editable" type="success" :disabled="busy||(offer.body.categoryWorkflow?!customerRows.length:!offer.body.lines.length)" @click="confirmCustomerContract">{{t('contractRepair.confirm')}}</el-button><el-button v-if="offer.contractId&&offer.contractId!=='0'" type="primary" @click="router.push({path:'/contracts',query:{id:offer.contractId}})">{{t('inquiryWorkspace.offer.openContract')}}</el-button></div></footer>
  </template>
  <el-dialog :model-value="!!preview" :title="t(preview?.kind==='LOGISTICS'?'inquiryWorkspace.offer.logisticsOriginalQuote':'inquiryWorkspace.offer.supplierOriginalQuote')" width="min(1000px,95vw)" @close="preview=null">
   <template v-if="preview">
    <el-descriptions :column="3" border>
     <el-descriptions-item :label="t('inquiryWorkspace.offer.quotingParty')">{{preview.body.company||'—'}}</el-descriptions-item>
     <el-descriptions-item :label="t('inquiryWorkspace.offer.currency')">{{preview.body.currency||'—'}}</el-descriptions-item>
     <el-descriptions-item :label="t('inquiryWorkspace.offer.version')">v{{preview.version}}</el-descriptions-item>
     <el-descriptions-item :label="t('inquiryWorkspace.offer.paymentMethod')">{{preview.body.paymentTerms||'—'}}</el-descriptions-item>
     <el-descriptions-item :label="t('inquiryWorkspace.offer.validUntil')">{{preview.body.validUntil||'—'}}</el-descriptions-item>
     <el-descriptions-item :label="t('inquiryWorkspace.offer.submittedBy')">{{preview.author||'—'}}</el-descriptions-item>
     <el-descriptions-item :label="t(preview.kind==='PROCUREMENT'?'inquiryWorkspace.quotes.overallRemark':'inquiryWorkspace.offer.remark')" :span="3"><span class="supplier-original-remark">{{preview.body.remark||'—'}}</span></el-descriptions-item>
    </el-descriptions>
    <el-table v-if="preview.kind==='PROCUREMENT'" :data="preview.body.prices" max-height="420">
     <el-table-column :label="t('inquiryWorkspace.offer.productDescription')" min-width="280"><template #default="{row}"><ComparisonSpecification :product-name="productName(row.productId)" :text="sourceProductDescription(row.productId)" hide-name/></template></el-table-column>
     <el-table-column :label="t('inquiryWorkspace.offer.originalPrice')"><template #default="{row}">{{preview.body.currency}} {{priceOf(row)}}</template></el-table-column>
     <el-table-column prop="delivery" :label="t('inquiryWorkspace.offer.delivery')"/>
     <el-table-column :label="t('inquiryWorkspace.quotes.specificationRemark')" min-width="160"><template #default="{row}"><span class="supplier-original-remark">{{row.remark}}</span></template></el-table-column>
    </el-table>
    <template v-else>
     <div class="detail-section-title">{{t('inquiryWorkspace.quotes.productOceanFreight')}}</div>
     <el-table :data="preview.body.freightRates||[]" max-height="300">
      <el-table-column :label="t('inquiryWorkspace.offer.productDescription')" min-width="280"><template #default="{row}"><ComparisonSpecification :product-name="productName(row.productId)" :text="sourceProductDescription(row.productId)" hide-name/></template></el-table-column>
      <el-table-column :label="t('inquiryWorkspace.offer.originalPrice')" min-width="180"><template #default="{row}">{{row.currency}} {{formatCfrUnitPrice(row.price)||'—'}} / {{row.unit}}</template></el-table-column>
      <el-table-column :label="t('inquiryWorkspace.offer.usdUnitPrice')" min-width="180"><template #default="{row}">USD {{formatCfrUnitPrice(row.usdPrice||row.price)||'—'}} / {{row.unit}}</template></el-table-column>
      <el-table-column prop="remark" :label="t('inquiryWorkspace.offer.remark')"/>
     </el-table>
     <template v-if="preview.body.charges?.length">
      <div class="detail-section-title">{{t('inquiryWorkspace.quotes.charges')}}</div>
      <el-table :data="preview.body.charges" max-height="300">
       <el-table-column prop="name" :label="t('inquiryWorkspace.offer.chargeName')"/>
       <el-table-column :label="t('inquiryWorkspace.offer.unitPriceAndUnit')"><template #default="{row}">{{row.currency}} {{formatCfrUnitPrice(row.amount)||'—'}} / {{row.unit}}</template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.offer.billingQty')"><template #default="{row}">{{formatCfrUnitPrice(row.quantity)||'—'}}</template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.offer.subtotal')"><template #default="{row}">{{formatCfrUnitPrice(row.subtotal)||'—'}}</template></el-table-column>
       <el-table-column prop="remark" :label="t('inquiryWorkspace.offer.remark')"/>
      </el-table>
     </template>
    </template>
   </template>
  </el-dialog>
 </section>
</template>
<script setup lang="ts">
import {computed,ref,watch,onBeforeUnmount} from 'vue'
import {useRouter} from 'vue-router'
import {useI18n} from 'vue-i18n'
import {ElMessage,ElMessageBox} from 'element-plus'
import {suggestPaymentTerms} from '../lib/paymentTermOptions'
import {post,quietErrors,saveBlob} from '../api'
import {productTemplateValue,type Quote,type Product,type Price} from '../lib/inquiryWorkspace'
import type {Offer,CategorySelection,OfferSelectionSnapshot,CategoryCalculation,OfferNegotiation} from '../lib/customerOffer'
import {formatCfrUnitPrice,offerSpecification as specification} from '../lib/customerOffer'
import {freightProductKey,freightCovers,commonProductFreight,applyProductFreight,productFreightPriceLabel,productFreightPriceSummary} from '../lib/productFreight'
import {latestCalculation} from '../lib/latestCalculation'
import {automaticOfferInputKey,offerCalculationIssue,readyOfferCalculationBody} from '../lib/automaticOfferCalculation'
import ComparisonSpecification from './ComparisonSpecification.vue'
import {comparisonProductDescription,comparisonSpecificationRows,replaceSpecificationSelection,hasMultipleSpecificationSuppliers,comparisonProductFields,type Candidate,type ProductCandidateGroup,type ComparisonSpecificationRow} from '../lib/comparisonSpecifications'
import {procurementQuoteCategories} from '../lib/quoteClassification'
import {offerSourceChanges} from '../lib/offerSourceChanges'
const props=defineProps<{caseId:string;sourceVersion?:string;mode:'compare'|'offer'}>()
const emit=defineEmits<{navigate:[tab:string]}>()
const router=useRouter(),{t,locale}=useI18n(),offer=ref<Offer|null>(null),busy=ref(false),error=ref(''),search=ref(''),preview=ref<Quote|null>(null),savedBody=ref('')
const showEmptyCategories=ref(false)
const automaticPending=ref(false),automaticError=ref('')
const productFreightChoices=ref<Record<string,{id:string;version?:number}>>({})
const isCompare=computed(()=>props.mode==='compare'),editable=computed(()=>!!offer.value?.canEdit)
const dirty=computed(()=>!!offer.value&&JSON.stringify(offer.value.body)!==savedBody.value)
const selections=computed(()=>offer.value?.body.categorySelections||[])
const groups=computed(()=>procurementQuoteCategories.map(g=>({...g,label:t(g.labelKey)})))
const procurementQuotes=computed(()=>offer.value?.source.quotes.filter(q=>q.kind==='PROCUREMENT'&&!q.historical&&q.submittedAt)||[])
const logisticsQuotes=computed(()=>offer.value?.source.quotes.filter(q=>q.kind==='LOGISTICS'&&!q.historical&&q.submittedAt)||[])
const documentLanguages=computed(()=>['ZH','EN','ES'].map(value=>({value,label:t(`inquiryWorkspace.offer.documentLanguages.${value}`)})))
const visibleGroups=computed(()=>groups.value.filter(group=>showEmptyCategories.value||rowsFor(group.value,false).length>0))
function rowsFor(category:string,filterSearch=true):Candidate[]{return procurementQuotes.value.filter(q=>q.body.quoteCategory===category).flatMap(quote=>(quote.body.prices||[]).flatMap(price=>{const product=offer.value?.source.body.products.find(p=>p.id===price.productId);return product?[{product,quote,price,category}]:[]})).filter(row=>!filterSearch||!search.value||`${row.product.product} ${specification(row.product)} ${row.quote.body.company}`.toLowerCase().includes(search.value.toLowerCase()))}
function productGroupsFor(category:string):ProductCandidateGroup[]{const grouped=new Map<string,ProductCandidateGroup>();for(const row of rowsFor(category)){const productKey=row.product.product.trim().toLocaleLowerCase()||row.product.id;let productGroup=grouped.get(productKey);if(!productGroup){productGroup={key:`${category}:${productKey}`,productName:row.product.product||'—',category,specs:[],rows:[]};grouped.set(productKey,productGroup)}productGroup.rows.push(row);let specGroup=productGroup.specs.find(item=>item.product.id===row.product.id);if(!specGroup){specGroup={key:`${productGroup.key}:${row.product.id}`,product:row.product,rows:[]};productGroup.specs.push(specGroup)}specGroup.rows.push(row)}return[...grouped.values()]}
function isSelected(row:Candidate){return selections.value.some(s=>s.productId===row.product.id&&s.quoteId===row.quote.id&&s.category===row.category)}
function selectionFor(row:Candidate){return selections.value.find(s=>s.productId===row.product.id&&s.quoteId===row.quote.id&&s.category===row.category)}
function sameProductSpecs(product:Product){return offer.value?.source.body.products.filter(p=>freightProductKey(p)===freightProductKey(product))||[]}
function groupProducts(group:ProductCandidateGroup){return sameProductSpecs(group.specs[0].product)}
function productFreightAvailable(group:ProductCandidateGroup,quote:Quote){return groupProducts(group).every(p=>freightCovers(quote,p.id))}
function productFreightId(group:ProductCandidateGroup){
 const products=groupProducts(group),key=freightProductKey(products[0]),choice=productFreightChoices.value[key]
 if(choice!==undefined)return logisticsQuotes.value.some(q=>q.id===choice.id&&q.version===choice.version&&productFreightAvailable(group,q))?choice.id:undefined
 return commonProductFreight(products,selections.value,logisticsQuotes.value)
}
function hasProductFreight(group:ProductCandidateGroup){const ids=new Set(groupProducts(group).map(p=>p.id));return selections.value.some(s=>ids.has(s.productId)&&s.category!=='DIRECT_CFR_USD'&&s.freightQuoteId)}
function productFreightName(quote:Quote){return quote.body.company||quote.body.carrier||t('inquiryWorkspace.offer.logisticsMissing')}
function productFreightChoiceLabel(group:ProductCandidateGroup,quote:Quote){const price=productFreightPriceLabel(groupProducts(group),quote);return price?price+' · '+productFreightName(quote):productFreightName(quote)}
function freightChoice(group:ProductCandidateGroup,id:string){const quote=logisticsQuotes.value.find(q=>q.id===id);return{name:quote?productFreightName(quote):'',price:productFreightPriceLabel(groupProducts(group),quote)}}
function productFreightLabel(group:ProductCandidateGroup,quote:Quote){
 const products=groupProducts(group),covered=products.filter(p=>freightCovers(quote,p.id)).length
 const summary=productFreightPriceSummary(products,quote)
 const coverage=covered<products.length?t('inquiryWorkspace.offer.productFreightCoverage',{covered,total:products.length}):''
 return [summary,productFreightName(quote),quote.body.route||quote.id,coverage].filter(Boolean).join(' · ')
}
function selectProductFreight(group:ProductCandidateGroup,id?:string){
 const products=groupProducts(group),quote=logisticsQuotes.value.find(q=>q.id===id)
 if(!applyProductFreight(products,selections.value,quote))return
 productFreightChoices.value[freightProductKey(products[0])]={id:quote?.id||'',version:quote?.version}
}
function sourceProductDescription(id:string){const product=offer.value?.source.body.products.find(p=>p.id===id);return product?comparisonSpecificationSentence(product):productName(id)}
function comparisonSpecificationSentence(product:Product){
 const keys:Record<string,string>={specification:'specification',delivery:'deliveryDate',weight:'productWeight',volume:'productVolume',packaging:'productPackaging',package_quantity:'productPackageQuantity',remarks:'remark'}
 const fields=comparisonProductFields(product,offer.value?.source.body.template?.fields||[],key=>keys[key]?t('inquiryWorkspace.offer.'+keys[key]):key)
 return comparisonProductDescription(product,fields)
}
function supplierOptionsFor(row:ComparisonSpecificationRow){return rowsFor(row.category,false).filter(candidate=>candidate.product.id===row.product.id)}
function specificationQuoteIds(row:ComparisonSpecificationRow){return selections.value.filter(s=>s.productId===row.product.id&&s.category===row.category).map(s=>s.quoteId)}
function specificationQuoteId(row:ComparisonSpecificationRow){const ids=specificationQuoteIds(row);return ids.length===1?ids[0]:undefined}
function supplierChoicePrice(row:Candidate){return `${priceOf(row.price)} / ${row.product.unit}`}
function supplierChoiceName(row:Candidate){return row.quote.body.company||t('inquiryWorkspace.offer.supplierMissing')}
function supplierChoiceLabel(row:Candidate){return `${supplierChoicePrice(row)} · ${supplierChoiceName(row)}`}
function selectSpecificationQuote(row:ComparisonSpecificationRow,quoteId:string|undefined){
 if(!offer.value)return
 const products=sameProductSpecs(row.product),key=freightProductKey(row.product),choice=productFreightChoices.value[key]
 const id=choice?(logisticsQuotes.value.some(q=>q.id===choice.id&&q.version===choice.version)?choice.id:undefined):commonProductFreight(products,selections.value,logisticsQuotes.value)
 const previous=selections.value,next=replaceSpecificationSelection(previous,row.product.id,row.category,quoteId,supplierOptionsFor(row).map(candidate=>candidate.quote.id))
 const quote=logisticsQuotes.value.find(q=>q.id===id)
 if(quote&&products.every(p=>freightCovers(quote,p.id))){for(const selection of next){if(selection.productId===row.product.id&&selection.category===row.category&&!previous.includes(selection))applyProductFreight(products,[selection],quote)}}
 offer.value.body.categorySelections=next
 if(id!==undefined&&!choice)productFreightChoices.value[key]={id,version:quote?.version}
}
function selectedRows(group:{rows:Candidate[]}){return group.rows.filter(isSelected)}
function specFields(product:Product){const hidden=new Set(['product','quantity','quantity_unit']);const templateFields=(offer.value?.source.body.template?.fields||[]).slice().sort((a,b)=>a.sortOrder-b.sortOrder).flatMap(field=>{if(hidden.has(field.fieldKey))return[];const value=productTemplateValue(product,field.fieldKey).trim();return value?[{key:field.fieldKey,label:field.displayName,value}]:[]});if(templateFields.length)return templateFields;const value=specification(product).trim();return value?[{key:'specification',label:t('inquiryWorkspace.offer.specification'),value}]:[]}
function specificationSentence(product:Product){const fields=specFields(product);return fields.length?fields.map(field=>`${field.label}: ${field.value}`).join(' · '):'—'}
function formatQuantity(value:string){const parsed=Number(value);return Number.isFinite(parsed)?parsed.toFixed(2):value}
function selectedCount(category:string){return selections.value.filter(s=>s.category===category).length}
function selectedProductCount(category:string){return new Set(rowsFor(category).filter(isSelected).map(row=>freightProductKey(row.product))).size}
function snapshotRow(s:OfferSelectionSnapshot):Candidate{return{product:s.product,quote:s.quote,category:s.category,price:s.quote.body.prices?.find(p=>p.productId===s.productId)||{productId:s.productId,price:'',delivery:'',remark:''}}}
const customerRows=computed(()=>offer.value?.body.customerSelections?.map(snapshotRow)||[])
const acceptedCustomerPlans=computed(()=>new Set(offer.value?.body.negotiations?.filter(n=>n.status==='AGREED').map(selectionKey)))
function customerPlanAccepted(row:Candidate){
 return acceptedCustomerPlans.value.has(selectionKey({productId:row.product.id,category:row.category,quoteId:row.quote.id}))
}
const customerSelectionCounts=computed(()=>{
 const rows=customerRows.value
 const selected=rows.filter(customerPlanAccepted)
 return {count:new Set(selected.map(r=>r.product.id)).size,total:new Set(rows.map(r=>r.product.id)).size}
})
const customerGroups=computed(()=>groups.value.flatMap(group=>{
 const rows=customerRows.value.filter(row=>row.category===group.value)
 if (!rows.length) return []
 const total=new Set(rows.map(row=>row.product.id)).size
 const selected=new Set(rows.filter(customerPlanAccepted).map(row=>row.product.id)).size
 return [{...group,rows,allSelected:selected===total,partiallySelected:selected>0&&selected<total}]
}))
function customerSupplierCount(rows:Candidate[]){return new Set(rows.map(row=>row.quote.id)).size}
const hasCustomer=computed(()=>Number(offer.value?.body.customerId||0)>0)
const canGenerateDocument=computed(()=>hasCustomer.value&&customerRows.value.length>0&&customerRows.value.every(row=>{const value=effectiveCustomerPrice(row);return value!==''&&Number.isFinite(Number(value))&&Number(value)>=0}))
const documentTooltip=computed(()=>{if(!hasCustomer.value)return t('inquiryWorkspace.messages.customerRequired');return canGenerateDocument.value?t('inquiryWorkspace.offer.documentReady'):t(customerRows.value.length?'inquiryWorkspace.offer.documentPricesRequired':'inquiryWorkspace.offer.documentSelectionRequired')})
function categoryCalculation(category:string):CategoryCalculation{if(!offer.value)throw new Error('offer unavailable');return offer.value.body.categoryCalculations![category]}
function formulaKey(category:string){return({FOB_USD:'fobUsdCfrFormula',FOB_CNY:'fobCnyCfrFormula',ALL_IN_PORT_CNY:'allInPortCfrFormula',EX_FACTORY_CNY:'exFactoryCfrFormula',REPROCESSING_CNY:'reprocessingCfrFormula',DIRECT_CFR_USD:'directCfrFormula'} as Record<string,string>)[category]}
function needsPortCharge(category:string){return['ALL_IN_PORT_CNY','EX_FACTORY_CNY','REPROCESSING_CNY'].includes(category)}
function needsInlandFreight(category:string){return['EX_FACTORY_CNY','REPROCESSING_CNY'].includes(category)}
function supplierPriceLabel(category:string){return({FOB_USD:'inquiryWorkspace.offer.supplierFobUnitPrice',FOB_CNY:'inquiryWorkspace.offer.supplierFobUnitPrice',ALL_IN_PORT_CNY:'inquiryWorkspace.offer.allInPortUnitPrice',EX_FACTORY_CNY:'inquiryWorkspace.offer.exFactoryUnitPrice',REPROCESSING_CNY:'inquiryWorkspace.offer.processingFeeUnitPrice',DIRECT_CFR_USD:'inquiryWorkspace.offer.factoryCfrUnitPrice'} as Record<string,string>)[category]}
function supplierPriceUnitLabel(category:string,currency:string){return category==='REPROCESSING_CNY'?t('inquiryWorkspace.offer.cnyPerUnit'):`${currency} / ${t('inquiryWorkspace.offer.pricingUnit')}`}
function negotiationFor(row:Candidate):OfferNegotiation{if(!offer.value)throw new Error('offer unavailable');const found=offer.value.body.negotiations!.find(item=>item.productId===row.product.id&&item.category===row.category&&item.quoteId===row.quote.id);if(found)return found;const created={productId:row.product.id,category:row.category,quoteId:row.quote.id,initialPrice:'',customerCounterPrice:'',proposedPrice:'',status:'DRAFT',note:''};offer.value.body.negotiations!.push(created);return created}
function customerSelectionFor(row:Candidate){return offer.value?.body.customerSelections?.find(item=>item.productId===row.product.id&&item.category===row.category&&item.quoteId===row.quote.id)}
function initialCalculatedPrice(row:Candidate){return negotiationFor(row).initialPrice.trim()||customerSelectionFor(row)?.cfrUnitPrice?.trim()||''}
function effectiveCustomerPrice(row:Candidate){return negotiationFor(row).proposedPrice.trim()||initialCalculatedPrice(row)}
function normalizeProposedPrice(row:Candidate){const negotiation=negotiationFor(row),raw=negotiation.proposedPrice.trim();if(!raw)return;if(/^\d+(\.\d{1,4})?$/.test(raw))negotiation.proposedPrice=raw}
function selectionKey(s:CategorySelection){return `${s.productId}:${s.category}:${s.quoteId}`}
const selectedFreightQuoteIds=computed(()=>[...new Set(selections.value.filter(item=>!!item.freightQuoteVersion).map(item=>item.freightQuoteId).filter((id):id is string=>!!id))])
const sourceChanges=computed(()=>offer.value?offerSourceChanges(offer.value.body,offer.value.source.quotes):{selectionChanged:false,sources:[]})
function sourceChangeDescription(change:ReturnType<typeof offerSourceChanges>['sources'][number]){
 const products=change.productIds.map(id=>{const p=offer.value?.body.customerSelections?.find(s=>s.productId===id)?.product;return p?`${p.product} · ${specificationSentence(p)}`:id}).join('；')
 const reason=t(change.removed?'inquiryWorkspace.offer.sourceRemoved':change.oldVersion!==change.newVersion?'inquiryWorkspace.offer.sourceVersionChanged':'inquiryWorkspace.offer.sourceContentChanged',{oldVersion:change.oldVersion,newVersion:change.newVersion})
 return `${change.company}${products?` · ${products}`:''}：${reason}`
}
function priceOf(p:Price){return formatCfrUnitPrice(p.price||p.factoryPrice||p.fobPrice)||'—'}
function routeOf(q:Quote){return[q.body.route,q.body.carrier&&q.body.carrier!==q.body.company?q.body.carrier:'',[q.body.loadingPort,q.body.destinationPort].filter(Boolean).join(' → ')].filter(Boolean).join(' · ')||t('inquiryWorkspace.offer.routeMissing')}
function productName(id:string){return customerRows.value.find(r=>r.product.id===id)?.product.product||offer.value?.source.body.products.find(p=>p.id===id)?.product||id}
function cargoDescription(q:Quote){return q.body.cargoIds?.length?q.body.cargoIds.map(productName).join(t('inquiryWorkspace.offer.listSeparator')):t('inquiryWorkspace.offer.wholeShipment')}
function displayTime(value:string){if(!value)return'—';const d=new Date(value);return Number.isNaN(d.getTime())?value:d.toLocaleString(locale.value,{year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false})}
function prepare(next:Offer){next.body.categorySelections??=[];next.body.transports??=[];next.body.customerSelections??=[];next.body.customerLogistics??=[];next.body.categoryCalculations??={};next.body.negotiations??=[];next.body.documentLanguage??='ZH';for(const group of groups.value){const calculation=next.body.categoryCalculations[group.value]??={quoteFx:'',quoteFxConfirmed:false,portCharge:'',inlandFreight:'',loss:'',interestRate:'',interestDays:'',note:''};calculation.quoteFx??='';calculation.portCharge??='';calculation.inlandFreight??='';calculation.loss??='';calculation.interestRate??='';calculation.interestDays??=''}for(const selection of next.body.customerSelections){const negotiation=next.body.negotiations.find(item=>selectionKey(item)===selectionKey(selection));if(!negotiation)next.body.negotiations.push({productId:selection.productId,category:selection.category,quoteId:selection.quoteId,initialPrice:selection.cfrUnitPrice||'',customerCounterPrice:'',proposedPrice:'',status:'DRAFT',note:''});else if(!negotiation.initialPrice)negotiation.initialPrice=selection.cfrUnitPrice||''}return next}
let sequence=0
async function refreshSources(){if(busy.value)return;const request=++sequence;busy.value=true;error.value='';try{const next=prepare(await post<Offer>('/customer-offer',{action:'get',caseId:props.caseId},quietErrors));if(request!==sequence)return;if(offer.value&&dirty.value){if(next.revision!==offer.value.revision){error.value=t('inquiryWorkspace.offer.concurrentUpdate');return}offer.value.source=next.source;offer.value.canEdit=next.canEdit}else{savedBody.value=JSON.stringify(next.body);offer.value=next}}catch(e){error.value=(e as Error).message||t('inquiryWorkspace.offer.loadFailed')}finally{if(request===sequence)busy.value=false}}
function chooseAccepted(row:Candidate,yes:boolean){
 if(!offer.value)return
 if(yes)for(const n of offer.value.body.negotiations||[])if(n.productId===row.product.id&&n.status==='AGREED')n.status='QUOTED'
 negotiationFor(row).status=yes?'AGREED':'QUOTED'
}
function chooseAllAccepted(rows:Candidate[],yes:boolean){
 if(!editable.value)return
 if(!yes){
  rows.filter(customerPlanAccepted).forEach(row=>chooseAccepted(row,false))
  return
 }
 const choices=new Map<string,Candidate>()
 for(const row of rows){
  if(!choices.has(row.product.id)||customerPlanAccepted(row))choices.set(row.product.id,row)
 }
 for(const row of choices.values()){
  if(!customerPlanAccepted(row))chooseAccepted(row,true)
 }
}
async function confirmCustomerContract(){
 if(!offer.value||busy.value)return
 try{await ElMessageBox.confirm(t('contractRepair.confirmHint'),t('contractRepair.confirmTitle'))}catch{return}
 busy.value=true;error.value=''
 try{
  if(dirty.value){offer.value=prepare(await post<Offer>('/customer-offer',{action:'save',caseId:props.caseId,revision:offer.value.revision,body:offer.value.body},quietErrors));savedBody.value=JSON.stringify(offer.value.body)}
  offer.value=prepare(await post<Offer>('/customer-offer',{action:'confirm',caseId:props.caseId,revision:offer.value.revision},quietErrors))
  savedBody.value=JSON.stringify(offer.value.body)
  await router.push({path:'/contracts',query:{id:offer.value.contractId}})
 }catch(e){error.value=(e as Error).message||t('contractRepair.failed')}finally{busy.value=false}
}
function calculationIssueText(issue:string){
 const field=({quoteFx:'exchangeRate',portCharge:'portChargeUnitPrice',inlandFreight:'productInlandFreightUnitPrice',loss:'lossUnitPrice',interestRate:'annualInterestRate',interestDays:'interestDays'} as Record<string,string>)[issue]
 return field?t('inquiryWorkspace.offer.autoParameterRequired',{field:t('inquiryWorkspace.offer.'+field)}):t('inquiryWorkspace.offer.'+({supplier:'autoSupplierRequired',supplierPrice:'autoSupplierPriceInvalid',factoryPrice:'autoFactoryPriceRequired',freight:'autoFreightRequired'} as Record<string,string>)[issue])
}
function categoryCalculationIssue(category:string){return selections.value.filter(s=>s.category===category).map(s=>offerCalculationIssue(s,offer.value!.body,offer.value!.source)).find(Boolean)}
function categoryCalculationStatus(category:string){
 if(!selectedCount(category))return''
 const issue=categoryCalculationIssue(category)
 if(issue)return calculationIssueText(issue)
 if(automaticPending.value)return t('inquiryWorkspace.offer.autoCalculating')
 return automaticError.value||''
}
function candidateCfrText(row:Candidate,unitPriceOnly=false){
 const selection=selectionFor(row);if(!selection||!offer.value)return'—'
 const issue=offerCalculationIssue(selection,offer.value.body,offer.value.source)
 if(issue)return calculationIssueText(issue)
 if(automaticPending.value)return t('inquiryWorkspace.offer.autoCalculating')
 return selection.cfrUnitPrice?(unitPriceOnly?formatCfrUnitPrice(selection.cfrUnitPrice):`USD ${formatCfrUnitPrice(selection.cfrUnitPrice)} / ${row.product.unit}`):t('inquiryWorkspace.offer.autoCalculationFailed')
}
const automaticTask=latestCalculation<Offer,Offer['body']['categorySelections']>({
 pending:value=>{automaticPending.value=value},
 async run(input){
  const body=readyOfferCalculationBody(input.body,input.source)
  if(!body.categorySelections.length)return []
  const result=await post<Offer>('/customer-offer',{action:'calculate_all',caseId:input.source.id,revision:input.revision,body},quietErrors)
  return result.body.categorySelections
 },
 apply(results){
  for(const result of results){const selection=selections.value.find(s=>selectionKey(s)===selectionKey(result));if(selection){selection.supplierFobUnitPrice=result.supplierFobUnitPrice;selection.productFreightUnitPrice=result.productFreightUnitPrice;selection.cfrUnitPrice=result.cfrUnitPrice}}
  automaticError.value=''
 },
 fail(reason){automaticError.value=(reason as Error).message||t('inquiryWorkspace.offer.autoCalculationFailed')},
})
function queueAutomaticCalculation(){
 automaticTask.cancel();automaticError.value=''
 if(!offer.value||!isCompare.value||!editable.value)return
 error.value=''
 for(const selection of selections.value){selection.supplierFobUnitPrice='';selection.productFreightUnitPrice='';selection.cfrUnitPrice=''}
 automaticTask.schedule(JSON.parse(JSON.stringify(offer.value)) as Offer)
}
let automaticActive=true
async function ensureAutomaticCalculation(){
 if(!isCompare.value)return true
 const caseId=props.caseId
 if(automaticError.value)queueAutomaticCalculation()
 const ok=await automaticTask.flush()
 if(!automaticActive||caseId!==props.caseId||!offer.value)return false
 if(!ok){error.value=automaticError.value||t('inquiryWorkspace.offer.autoCalculationFailed');return false}
 const issue=selections.value.map(s=>offerCalculationIssue(s,offer.value!.body,offer.value!.source)).find(Boolean)
 if(issue){error.value=calculationIssueText(issue);return false}
 if(selections.value.some(s=>!s.cfrUnitPrice)){error.value=t('inquiryWorkspace.offer.autoCalculationFailed');return false}
 error.value='';return true
}
watch(()=>offer.value&&isCompare.value&&editable.value?props.caseId+automaticOfferInputKey(offer.value):'',queueAutomaticCalculation,{flush:'sync'})
onBeforeUnmount(()=>{automaticActive=false;automaticTask.dispose()})

function base64Workbook(value:string){const binary=atob(value),bytes=new Uint8Array(binary.length);for(let i=0;i<binary.length;i++)bytes[i]=binary.charCodeAt(i);return new Blob([bytes],{type:'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'})}
async function generateDocument(){if(!offer.value||busy.value||!canGenerateDocument.value)return;busy.value=true;error.value='';try{if(dirty.value){const body={...offer.value.body,categoryWorkflow:true};offer.value=prepare(await post<Offer>('/customer-offer',{action:'save',caseId:props.caseId,revision:offer.value.revision,body},quietErrors));savedBody.value=JSON.stringify(offer.value.body)}const file=await post<{fileName:string;fileData:string}>('/customer-offer',{action:'xlsx',caseId:props.caseId},quietErrors);saveBlob(base64Workbook(file.fileData),file.fileName);ElMessage.success(t('inquiryWorkspace.offer.documentGenerated'))}catch(e){error.value=(e as Error).message||t('inquiryWorkspace.offer.documentGenerateFailed');ElMessage.error(error.value)}finally{busy.value=false}}
async function persist(stage:boolean){if(!offer.value||busy.value)return;if(hasMultipleSpecificationSuppliers(selections.value)){error.value=t('inquiryWorkspace.offer.reselectSingleSupplier');return;}if(stage&&customerRows.value.length){try{await ElMessageBox.confirm(t('inquiryWorkspace.offer.updateSelectionConfirm'),t('inquiryWorkspace.offer.updateSelectionTitle'),{confirmButtonText:t('inquiryWorkspace.offer.updateSelection'),cancelButtonText:t('inquiryWorkspace.offer.returnToReview'),type:'info'})}catch{return}}
 if(!await ensureAutomaticCalculation()||busy.value||!offer.value)return
 busy.value=true;error.value='';try{const body={...offer.value.body,categoryWorkflow:true,pricingSnapshot:'',lines:offer.value.body.lines.map(l=>({...l,calculatedPrice:'',unitPrice:'',amount:''}))};offer.value=prepare(await post<Offer>('/customer-offer',{action:stage?'stage_selections':'save',caseId:props.caseId,revision:offer.value.revision,body},quietErrors));savedBody.value=JSON.stringify(offer.value.body);ElMessage.success(t(stage?'inquiryWorkspace.offer.staged':'inquiryWorkspace.offer.saved'));if(stage)emit('navigate','offer')}catch(e){error.value=(e as Error).message||t('inquiryWorkspace.offer.saveFailed');ElMessage.error(error.value)}finally{busy.value=false}}
watch(()=>props.caseId,()=>{sequence++;showEmptyCategories.value=false;automaticTask.cancel();automaticError.value='';productFreightChoices.value={};offer.value=null;savedBody.value='';busy.value=false;void refreshSources()},{immediate:true})
watch(()=>props.sourceVersion,()=>{void refreshSources()})
</script>
<style scoped>
.quote-workspace{--ink:#234455;--muted:#70828d;--line:#dfe8ed;--accent:#166d82;color:var(--ink);font-size:14px;padding-bottom:96px}.workspace-heading{display:flex;justify-content:space-between;align-items:center;gap:24px;padding:26px 28px;background:linear-gradient(115deg,#eff7fa,#f6faf9);border:1px solid #d7e7ed;border-radius:14px}.eyebrow{font-size:11px;letter-spacing:.07em;font-weight:650;color:#518297}.workspace-heading h2{font-size:24px;margin:6px 0 10px;font-weight:650}.workspace-heading p,.card-heading p{margin:0;color:var(--muted);line-height:1.65;font-size:13px}.workspace-metrics{display:flex;align-items:center;gap:12px;white-space:nowrap}.workspace-metrics strong{font-size:30px;color:var(--accent)}.workspace-metrics span{color:var(--muted)}.flow-strip{display:flex;gap:20px;align-items:center;padding:18px 8px;color:#80939d;font-size:13px}.flow-strip .active{color:var(--accent);font-weight:650}.flow-arrow{color:#a8bac3}.workspace-card{margin-bottom:20px;background:#fff;border:1px solid var(--line);border-radius:12px;overflow:hidden}.card-heading{display:flex;justify-content:space-between;align-items:center;gap:20px;padding:19px 22px;border-bottom:1px solid var(--line);background:#fafcfd}.card-heading h3{font-size:18px;margin:4px 0 6px;line-height:1.4;font-weight:650}.category-panel{border-top:3px solid #3a839b}.category-fob_cny{border-top-color:#5c8e9b}.category-all_in_port_cny{border-top-color:#5b9688}.category-ex_factory_cny{border-top-color:#a59571}.category-reprocessing_cny{border-top-color:#8581a4}.group-count{display:flex;align-items:center;gap:8px;white-space:nowrap;color:var(--muted);font-size:12px}.group-count strong{font-size:22px;color:var(--ink)}.preparation-fields{padding:18px 22px;display:flex;align-items:center;gap:26px;flex-wrap:wrap}.preparation-fields>label:not(.el-checkbox){display:grid;gap:8px;font-size:13px;min-width:250px}.quiet-note{color:var(--muted);font-size:13px}.section-tools{display:flex;align-items:center;justify-content:space-between;gap:16px;margin:20px 0 16px}.section-tools .el-input{max-width:330px}.section-tools>span{font-size:12px;color:var(--muted)}.quote-table{--el-table-header-bg-color:#f6f9fb;--el-table-header-text-color:#527082;--el-table-border-color:#e6edf1;--el-table-row-hover-bg-color:#f4f9fb;color:var(--ink)}.quote-table :deep(th.el-table__cell){font-size:13px;font-weight:600;height:46px}.quote-table :deep(td.el-table__cell){padding:17px 0;font-size:14px}.quote-table :deep(.selected-row){--el-table-tr-bg-color:#f0f8fa}.quote-table strong{font-weight:600}.quote-workspace small{display:block;font-size:12px;line-height:1.6;margin-top:5px;color:var(--muted)}.spec{display:-webkit-box!important;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden;max-width:100%}.money{font-variant-numeric:tabular-nums;color:#28596c}.pending{font-size:12px;color:#a18450}.logistics-panel{border-top:3px solid #59988c}.charge-details{padding:8px 28px 20px;background:#f8fbfa}.charge-details p{color:var(--muted)}.customer-fields{padding:22px;display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:0 20px}.customer-fields :deep(.el-date-editor){width:100%}.saved-at{font-size:12px;color:var(--muted)}.document-panel{display:flex;align-items:center;justify-content:space-between;gap:20px;background:#f4f7fa;border:1px dashed #ccd9e1;border-radius:12px;padding:20px 24px;margin-top:20px}.document-panel h3{margin:0 0 8px;font-size:17px}.document-panel p{color:var(--muted);font-size:13px;margin:0;line-height:1.6}.workspace-footer{display:flex;align-items:center;justify-content:space-between;gap:18px;padding:18px 22px;margin-top:22px;border:1px solid #d8e4ea;border-radius:12px;background:rgba(255,255,255,.97);position:sticky;bottom:10px;z-index:6;box-shadow:0 6px 22px #26475612}.workspace-footer strong{font-size:14px;font-weight:600}.footer-actions{display:flex;gap:10px;flex-wrap:wrap}.footer-actions .el-button{margin:0}.quote-workspace :deep(.el-button--primary:not(.is-link):not(.is-disabled)){background:#176f85;border-color:#176f85;color:#fff}.quote-workspace :deep(.el-button.is-link){color:#24778e}.quote-workspace>.el-alert{margin-bottom:18px}.quote-workspace :deep(.el-descriptions){margin-bottom:16px}@media(max-width:1100px){.customer-fields{grid-template-columns:repeat(2,minmax(0,1fr))}.workspace-footer{flex-wrap:wrap}.workspace-heading{align-items:flex-start}.workspace-metrics{flex-wrap:wrap}}@media(max-width:700px){.workspace-heading,.card-heading,.document-panel{flex-direction:column;align-items:flex-start;padding:18px}.workspace-heading h2{font-size:21px}.section-tools>span{display:none}.customer-fields{grid-template-columns:1fr}.flow-strip{gap:8px;font-size:12px}.workspace-footer{position:static}.preparation-fields{gap:15px}}
.category-calculation{padding:16px 22px;border-bottom:1px solid var(--line);background:#f8fbfc}.calculation-title{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-bottom:14px}.calculation-title strong{font-size:14px;color:#284e60}.calculation-title small{margin-top:3px}.calculation-fields{display:grid;grid-template-columns:repeat(4,minmax(170px,1fr));gap:14px}.calculation-fields label{display:grid;gap:7px;color:#536f7d;font-size:12px}.calculation-fields .calculation-note{grid-column:span 2}.calculation-fields :deep(.el-select){width:100%}.negotiation-table,.comparison-table{width:100%}.negotiation-table :deep(.el-table__inner-wrapper){min-width:0}.negotiation-table :deep(.el-input-group__prepend){padding:0 7px;color:#55717e;background:#f5f8fa}.negotiation-table :deep(.el-input__wrapper),.negotiation-table :deep(.el-select__wrapper){box-shadow:0 0 0 1px #d8e3e8 inset}.negotiation-table :deep(.el-input__wrapper:focus-within),.negotiation-table :deep(.el-select__wrapper.is-focused){box-shadow:0 0 0 1px #4c91a3 inset}.customer-information .customer-fields{grid-template-columns:repeat(4,minmax(180px,1fr))}@media(max-width:1050px){.calculation-fields{grid-template-columns:repeat(2,minmax(170px,1fr))}}@media(max-width:700px){.calculation-fields{grid-template-columns:1fr}.calculation-fields .calculation-note{grid-column:auto}.calculation-title{align-items:flex-start;flex-direction:column}.customer-information .customer-fields{grid-template-columns:1fr}}
.fixed-cfr-calculation{display:flex;align-items:center;justify-content:space-between;gap:18px}.fixed-formula{display:grid;grid-template-columns:auto minmax(0,1fr);gap:3px 14px;align-items:baseline}.fixed-formula span{font-size:11px;font-weight:650;color:#4d8293;letter-spacing:.04em}.fixed-formula strong{font-size:15px;color:#244d60}.fixed-formula small{grid-column:2;margin:0;color:#70838e}@media(max-width:700px){.fixed-cfr-calculation{align-items:stretch;flex-direction:column}.fixed-formula{grid-template-columns:1fr}.fixed-formula small{grid-column:auto}}
@media(max-width:700px){.quote-workspace{padding-bottom:10px}}
.quote-workspace.compare-mode{padding:20px 20px 96px;background:#f3f6f8;border:1px solid #e5ecef;border-radius:16px}.compare-mode .section-tools{margin:0 0 22px;padding:12px 16px;background:#fff;border:1px solid #e2eaee;border-radius:12px;box-shadow:0 2px 10px #274a5a08}.compare-mode .section-tools>span{padding:6px 10px;background:#f4f8fa;border-radius:999px;color:#667e8a}.compare-mode .workspace-card{margin-bottom:22px;border:1px solid #dfe8ec;border-top-width:4px;border-radius:14px;box-shadow:0 5px 16px #24485808}.compare-mode .card-heading{padding:17px 20px;background:linear-gradient(135deg,#f8fbfc,#eef7fa)}.compare-mode .card-heading h3{font-size:19px;letter-spacing:.01em}.compact-category-heading{min-height:92px}.compact-category-heading .eyebrow{font-size:10px}.compact-category-heading p{font-size:12px;line-height:1.45}.category-title{display:flex;align-items:center;gap:9px}.category-title h3{margin:2px 0 4px}.compare-mode .category-fob_cny .card-heading{background:linear-gradient(135deg,#f8fbfb,#eef6f5)}.compare-mode .category-all_in_port_cny .card-heading{background:linear-gradient(135deg,#f7fbf9,#eef8f4)}.compare-mode .category-ex_factory_cny .card-heading{background:linear-gradient(135deg,#fbfaf7,#f6f2e9)}.compare-mode .category-reprocessing_cny .card-heading{background:linear-gradient(135deg,#faf9fc,#f3f1f8)}.compare-mode .group-count{align-items:flex-end;flex-direction:column;gap:6px;padding:8px 10px;background:#ffffffcc;border:1px solid #dde8ec;border-radius:10px}.compare-mode .category-calculation{display:grid;grid-template-columns:150px minmax(0,1fr);align-items:start;gap:16px;margin:14px 16px 16px;padding:16px;border:1px solid #dbe7eb;border-radius:11px;background:linear-gradient(135deg,#fbfcfd,#f5f9fa)}.compare-mode .calculation-title{align-items:flex-start;flex-direction:column;margin:2px 0 0;gap:8px}.compare-mode .calculation-title small{line-height:1.35}.compare-mode .calculation-fields{grid-template-columns:minmax(260px,2fr) repeat(2,minmax(170px,1fr));gap:11px}.compare-mode .calculation-fields .formula-expression{grid-column:auto}.compare-mode .quote-table{width:calc(100% - 32px);margin:0 16px 16px;border:1px solid #dfe9ed;border-radius:11px;overflow:hidden}.compare-mode .quote-table :deep(th.el-table__cell){height:44px;background:#f3f7f8}.compare-mode .quote-table :deep(td.el-table__cell){padding:14px 0}.compare-mode .quote-table :deep(.el-table__empty-block){min-height:58px}.compare-mode .logistics-panel{margin-top:30px;border-top-color:#488d80}.compare-mode .logistics-panel .card-heading{background:linear-gradient(135deg,#f5faf8,#edf7f4)}.compare-mode .logistics-panel .quote-table{margin-top:13px}.compare-mode .workspace-footer{margin-top:28px}.document-actions{display:flex;align-items:flex-end;gap:12px}.document-actions label{display:grid;gap:6px;min-width:180px;color:var(--muted);font-size:12px}.document-actions :deep(.el-select){width:100%}
@media(max-width:1250px){.compare-mode .category-calculation{grid-template-columns:1fr}.compare-mode .calculation-title{flex-direction:row;align-items:center;justify-content:space-between}.compare-mode .calculation-fields{grid-template-columns:repeat(3,minmax(150px,1fr))}}
@media(max-width:1050px){.comparison-table :deep(th.el-table__cell),.negotiation-table :deep(th.el-table__cell){font-size:12px}.comparison-table :deep(td.el-table__cell),.negotiation-table :deep(td.el-table__cell){font-size:13px;padding:12px 0}.comparison-table :deep(.cell),.negotiation-table :deep(.cell){padding:0 8px}.negotiation-table :deep(.el-input__wrapper){padding-left:7px;padding-right:7px}}
@media(max-width:700px){.quote-workspace.compare-mode{padding:14px 12px 18px}.compare-mode .section-tools{padding:12px;margin-bottom:20px}.compare-mode .workspace-card{margin-bottom:18px}.compare-mode .card-heading{padding:13px 14px}.compare-mode .category-calculation{margin:10px 10px 13px;padding:13px}.compare-mode .calculation-fields{grid-template-columns:1fr}.compare-mode .calculation-fields .formula-expression,.compare-mode .calculation-fields .calculation-note{grid-column:auto}.compare-mode .quote-table{width:calc(100% - 16px);margin:0 8px 10px}.compare-mode .logistics-panel{margin-top:24px}.document-actions{width:100%;align-items:stretch;flex-direction:column}.document-actions label{width:100%}}
.calculation-fields{grid-template-columns:repeat(2,minmax(220px,1fr))}.formula-summary{grid-column:1/-1;display:grid;grid-template-columns:auto minmax(0,1fr);gap:5px 12px;padding:10px 12px;border:1px solid #dce8ec;border-radius:8px;background:#fff;color:#536f7d;font-size:12px}.formula-summary strong{color:#294f60;font-weight:600}.formula-summary small{grid-column:2;margin:0;color:#728690}.compare-mode .calculation-fields{grid-template-columns:repeat(2,minmax(220px,1fr))}
.compare-mode .fixed-cfr-calculation{display:flex;grid-template-columns:none;align-items:center;justify-content:space-between;gap:18px}.compare-mode .fixed-cfr-calculation .fixed-formula{flex:1;min-width:0}.compare-mode .fixed-cfr-calculation>.el-button{flex:0 0 138px;margin:0}
@media(max-width:700px){.compare-mode .calculation-fields{grid-template-columns:1fr}.formula-summary{grid-column:auto;grid-template-columns:1fr}.formula-summary small{grid-column:auto}}
@media(max-width:700px){.compare-mode .fixed-cfr-calculation{align-items:stretch;flex-direction:column}.compare-mode .fixed-cfr-calculation>.el-button{flex-basis:auto;width:100%}}
.number-cell{display:inline-block;font-size:14px;font-weight:600;line-height:1.45;font-variant-numeric:tabular-nums;color:#28596c;white-space:nowrap}.column-heading{display:flex;flex-direction:column;gap:2px;font-size:13px;font-weight:600;line-height:1.25}.column-heading small{font-size:11px!important;font-weight:500;line-height:1.2!important;margin:0!important;color:#78909c!important}
.detail-section-title{margin:18px 0 8px;font-size:14px;font-weight:650;color:var(--ink)}
.full-spec{white-space:normal;overflow-wrap:anywhere;line-height:1.45!important}.product-summary{padding:3px 0}.product-summary>strong{font-size:16px;color:#173f52}.product-summary>small{margin-top:4px!important}.specification-groups{display:grid;gap:12px;padding:14px 16px 16px;background:#eef3f5}.specification-card{overflow:hidden;border:1px solid #d9e5e9;border-radius:11px;background:#fff;box-shadow:0 2px 7px #294c5908}.specification-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;padding:13px 16px;border-bottom:1px solid #e1eaed;background:#f9fbfc}.specification-index{display:block;margin-bottom:8px;color:#287489;font-size:11px;font-weight:700;letter-spacing:.04em}.specification-fields{display:flex;flex-wrap:wrap;gap:7px 9px}.specification-fields>span{display:flex;align-items:baseline;gap:5px;padding:4px 8px;border:1px solid #dce7eb;border-radius:6px;background:#fff;line-height:1.4}.specification-fields b{color:#718590;font-size:11px;font-weight:600}.specification-fields em{color:#294c5c;font-size:12px;font-style:normal;font-weight:600}.specification-meta{display:grid;justify-items:end;gap:2px;min-width:135px;white-space:nowrap}.specification-meta>span,.specification-meta>small{color:#718590;font-size:11px}.specification-meta>strong{color:#1f5367;font-size:15px;font-variant-numeric:tabular-nums}.selected-cfr-list{display:flex;flex-wrap:wrap;gap:6px}.cfr-chip{display:grid;grid-template-columns:minmax(100px,1fr) auto;align-items:center;gap:8px;padding:5px 8px;border:1px solid #cfe1e6;border-radius:7px;background:#f5fafb;font-size:11px}.cfr-chip>span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:#607985}.cfr-chip strong{white-space:nowrap;color:#176d82;font-variant-numeric:tabular-nums}.supplier-options{padding:10px 12px 12px;background:#f6f9fa}.supplier-option-table{border:1px solid #dfe9ed;border-radius:9px;overflow:hidden}.supplier-option-table :deep(td.el-table__cell){padding:12px 0}.product-group-table :deep(.el-table__expanded-cell){padding:0!important}.product-group-table :deep(.el-table__expand-icon){color:#24778e}.product-group-table :deep(.selected-row>td.el-table__cell){background:#f0f8fa}.product-group-table :deep(.el-table__row>td.el-table__cell){border-bottom-color:#e2ebee}@media(max-width:800px){.specification-heading{flex-direction:column}.specification-meta{justify-items:start}.cfr-chip{grid-template-columns:1fr}.cfr-chip strong{white-space:normal}}
.fixed-category-calculation{display:grid!important;grid-template-columns:minmax(340px,1.5fr) minmax(260px,1fr) auto!important;align-items:end!important;gap:16px!important}.manual-inputs{display:grid;grid-template-columns:repeat(3,minmax(120px,1fr));gap:10px}.manual-inputs label{display:grid;gap:6px;color:#536f7d;font-size:12px}.fixed-category-calculation>.el-button{margin:0;min-width:116px;width:138px;justify-self:end}@media(max-width:1100px){.fixed-category-calculation{grid-template-columns:1fr!important}.manual-inputs{grid-template-columns:repeat(3,minmax(120px,1fr))}.fixed-category-calculation>.el-button{width:140px}}@media(max-width:700px){.manual-inputs{grid-template-columns:1fr}.fixed-category-calculation>.el-button{width:100%}}
.compare-mode .compact-category-heading{min-height:auto;padding:10px 14px}.compare-mode .compact-category-heading h3{font-size:16px;margin:0}.compare-mode .compact-category-heading p{font-size:11px;line-height:1.35}.compare-mode .compact-category-heading .eyebrow{font-size:9px}.compare-mode .group-count{align-items:center;flex-direction:row;gap:7px;padding:0;background:transparent;border:0;border-radius:0}.compare-mode .fixed-category-calculation{grid-template-columns:minmax(360px,1.6fr) minmax(300px,1fr) auto!important;gap:10px!important;margin:8px 12px 10px;padding:10px 12px;border-radius:8px}.compare-mode .fixed-formula{gap:2px 9px}.compare-mode .fixed-formula strong{font-size:14px;line-height:1.35}.compare-mode .fixed-formula small{font-size:11px;line-height:1.35}.compare-mode .manual-inputs{gap:7px}.compare-mode .manual-inputs label{gap:4px;font-size:11px}.compare-mode .manual-inputs :deep(.el-input__wrapper){min-height:30px}.compare-mode .fixed-category-calculation>.el-button{height:32px}.compare-mode .product-group-table{width:calc(100% - 24px);margin:0 12px 12px}.compare-mode .product-group-table :deep(th.el-table__cell){height:36px}.compare-mode .product-group-table :deep(td.el-table__cell){padding:9px 0}.product-summary{padding:0}.product-summary>strong{font-size:14px}.specification-groups{gap:8px;padding:8px 10px 10px}.specification-card{border-radius:8px;box-shadow:none}.specification-heading{align-items:center;gap:12px;padding:7px 10px}.specification-line{display:flex;align-items:baseline;gap:9px;min-width:0}.specification-line>strong{flex:0 0 auto;color:#287489;font-size:10px}.specification-line>span{color:#526c78;font-size:11px;line-height:1.5}.specification-meta{display:flex;align-items:center;gap:8px;min-width:auto}.specification-meta>strong{font-size:13px}.specification-meta>span{color:#718590;font-size:10px}.supplier-options{padding:6px 8px 8px}.supplier-option-table{border-radius:7px}.supplier-option-table :deep(th.el-table__cell){height:36px}.supplier-option-table :deep(td.el-table__cell){padding:8px 0}.cfr-chip{padding:3px 6px;border-radius:5px}.selected-cfr-list{gap:4px}@media(max-width:1100px){.compare-mode .fixed-category-calculation{grid-template-columns:1fr!important}.specification-heading{align-items:flex-start;flex-direction:column}.specification-meta{justify-items:start}}@media(max-width:700px){.compare-mode .group-count{align-items:flex-start;flex-direction:column}.specification-line{align-items:flex-start;flex-direction:column;gap:3px}}
.compare-mode .fixed-category-calculation{grid-template-columns:minmax(0,1fr) auto!important;align-items:center!important;column-gap:14px!important;row-gap:8px!important}.compare-mode .fixed-category-calculation .fixed-formula{grid-column:1;grid-row:1}.compare-mode .fixed-category-calculation .manual-inputs{grid-column:1/-1;grid-row:2;grid-template-columns:repeat(auto-fit,minmax(145px,180px));justify-content:start;width:100%}.compare-mode .fixed-category-calculation>.el-button{grid-column:2;grid-row:1;align-self:center}.compare-mode .fixed-formula strong{white-space:normal}.cfr-chip{grid-template-columns:minmax(150px,1fr) auto}.cfr-chip>span{max-width:none}@media(max-width:700px){.compare-mode .fixed-category-calculation{grid-template-columns:1fr!important}.compare-mode .fixed-category-calculation .fixed-formula,.compare-mode .fixed-category-calculation .manual-inputs,.compare-mode .fixed-category-calculation>.el-button{grid-column:1;grid-row:auto}.compare-mode .fixed-category-calculation>.el-button{justify-self:stretch;width:100%}}
.customer-product-table :deep(.el-table__expanded-cell){padding:0!important}.customer-product-table :deep(.el-table__expand-icon){color:#24778e}.customer-product-table :deep(.el-table__row>td.el-table__cell){border-bottom-color:#e2ebee}.customer-specification-groups{display:grid;gap:10px;padding:10px 12px 12px;background:#eef3f5}.customer-specification-card{overflow:hidden;border:1px solid #d9e5e9;border-radius:9px;background:#fff}.customer-specification-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;padding:10px 13px;border-bottom:1px solid #e1eaed;background:#f9fbfc}.customer-specification-heading>div:first-child{display:flex;align-items:baseline;gap:10px;min-width:0}.customer-specification-heading>div:first-child>strong{flex:0 0 auto;color:#287489;font-size:11px}.customer-specification-heading>div:first-child>span{color:#526c78;font-size:12px;line-height:1.5}.customer-specification-meta{display:flex;align-items:center;gap:10px;white-space:nowrap}.customer-specification-meta>strong{color:#1f5367;font-size:14px;font-variant-numeric:tabular-nums}.customer-specification-meta>span{color:#718590;font-size:11px}.customer-supplier-table :deep(th.el-table__cell){height:38px;background:#f6f9fa}.customer-supplier-table :deep(td.el-table__cell){padding:10px 0}.customer-supplier-table :deep(.el-input__inner){font-variant-numeric:tabular-nums}@media(max-width:800px){.customer-specification-heading{flex-direction:column}.customer-specification-heading>div:first-child{align-items:flex-start;flex-direction:column;gap:3px}.customer-specification-meta{white-space:normal}}
.customer-category-panel{border-top-width:3px}.compact-customer-heading{min-height:0;padding:12px 18px;background:linear-gradient(135deg,#fbfcfd,#f4f9fa)}.compact-customer-heading>div{width:100%}.compact-customer-heading .eyebrow{font-size:10px}.compact-customer-heading p{font-size:12px;line-height:1.4}.customer-category-title{display:flex;align-items:center;gap:10px;margin:2px 0 4px}.customer-category-title h3{margin:0;font-size:17px}.customer-product-table :deep(th.el-table__cell){height:40px;background:#f5f8fa}.customer-product-table :deep(td.el-table__cell){padding:11px 0}.customer-product-table :deep(.el-table__expanded-cell){background:#f4f7f8}.customer-specification-groups{gap:0;padding:8px 14px 10px;background:#f1f5f6}.customer-specification-card{border:0;border-radius:0;box-shadow:none}.customer-specification-card+.customer-specification-card{margin-top:8px;border-top:1px solid #d9e4e8}.customer-specification-heading{align-items:center;padding:8px 12px;border:0;background:#f8fafb}.customer-specification-heading>div:first-child>span{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:2;overflow:hidden}.customer-specification-meta{margin-left:auto}.customer-specification-meta>strong{font-size:13px}.customer-supplier-table{border-top:1px solid #e3eaed}.customer-supplier-table :deep(th.el-table__cell){height:36px;background:#fff}.customer-supplier-table :deep(td.el-table__cell){padding:9px 0}.customer-price{display:inline-flex;align-items:baseline;gap:4px;white-space:nowrap}.customer-price span{color:var(--muted);font-size:11px;font-weight:500}.customer-price-input{width:min(100%,300px)}.customer-price-input :deep(.el-input-group__prepend){padding:0 10px}.customer-price-input :deep(.el-input__wrapper){min-height:34px}@media(max-width:800px){.compact-customer-heading{padding:11px 14px}.customer-specification-groups{padding:7px 9px 9px}.customer-specification-heading{align-items:flex-start}.customer-specification-meta{margin-left:0}.customer-price-input{width:100%}}
.customer-category-panel .compact-customer-heading{padding:9px 15px}.customer-category-panel .compact-customer-heading p{font-size:11px}.customer-category-panel .customer-category-title{margin:1px 0 2px}.customer-category-panel .customer-category-title h3{font-size:16px}.customer-product-table :deep(th.el-table__cell){height:34px}.customer-product-table :deep(td.el-table__cell){padding:7px 0}.customer-product-table .product-summary>strong{font-size:13px}.customer-specification-groups{padding:5px 10px 7px}.customer-specification-heading{padding:6px 10px}.customer-specification-heading>div:first-child>span{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:11px}.customer-specification-meta>strong{font-size:12px}.customer-supplier-table :deep(th.el-table__cell){height:32px;font-size:12px}.customer-supplier-table :deep(td.el-table__cell){padding:6px 0;font-size:13px}.customer-supplier-table :deep(td.el-table__cell strong){font-size:13px}.customer-price-input{width:min(100%,270px)}.customer-price-input :deep(.el-input__wrapper){min-height:30px}.customer-price-input :deep(.el-input-group__prepend){padding:0 8px}.customer-supplier-table :deep(.el-button.is-link){font-size:13px}@media(max-width:800px){.customer-category-panel .compact-customer-heading{padding:8px 12px}.customer-specification-heading>div:first-child>span{white-space:normal}.customer-price-input{width:100%}}
.customer-specification-info{display:block!important;flex:1;min-width:0}.customer-specification-label{display:flex;align-items:center;gap:8px;margin-bottom:3px}.customer-specification-label strong{color:#287489;font-size:10px}.customer-specification-label span{padding-left:8px;border-left:1px solid #cbd9de;color:#1f5367;font-size:11px;font-weight:650;font-variant-numeric:tabular-nums}.customer-specification-fields{display:block;overflow:hidden;color:#526c78;font-size:11px;line-height:1.45;text-overflow:ellipsis;white-space:nowrap}.customer-specification-fields>span{display:inline}.customer-specification-fields>span+span::before{content:' · ';color:#a3b2b9}.customer-specification-fields b{margin-right:3px;color:#7a8d96;font-size:10px;font-weight:500}.customer-specification-fields em{color:#385867;font-style:normal;font-weight:600}.customer-specification-meta{flex:0 0 auto;margin-left:16px}.customer-specification-meta>span{font-size:10px;white-space:nowrap}@media(max-width:800px){.customer-specification-info{width:100%}.customer-specification-fields{white-space:normal}.customer-specification-meta{margin-left:0}}
.customer-product-quantity{color:#1f5367;font-size:13px;font-variant-numeric:tabular-nums;white-space:nowrap}.customer-specification-heading{align-items:flex-start;padding:9px 12px}.customer-specification-label{margin-bottom:5px}.customer-specification-label strong{font-size:11px}.customer-specification-fields{overflow:visible;font-size:12px;line-height:1.65;text-overflow:clip;white-space:normal}.customer-specification-fields b{font-size:11px}.customer-specification-fields em{font-size:12px}.customer-specification-meta>span{font-size:11px}
.compare-mode .compact-category-heading{padding:9px 15px}.compare-mode .compact-category-heading h3{font-size:16px}.compare-mode .compact-category-heading p{font-size:11px}.compare-mode .fixed-category-calculation{margin:7px 10px 9px;padding:9px 11px}.compare-mode .fixed-category-calculation .manual-inputs{grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:8px}.compare-mode .manual-inputs :deep(.el-input__wrapper){min-height:32px}.compare-mode .product-group-table{width:calc(100% - 20px);margin:0 10px 10px}.compare-mode .product-group-table :deep(th.el-table__cell){height:34px}.compare-mode .product-group-table :deep(td.el-table__cell){padding:7px 0}.compare-mode .specification-groups{gap:7px;padding:6px 9px 8px;background:#f1f5f6}.compare-mode .specification-card{border:0;border-radius:0;background:#fff}.compare-mode .specification-card+.specification-card{border-top:1px solid #d9e4e8}.compare-mode .specification-heading{align-items:flex-start;padding:9px 12px;border-bottom:0;background:#f8fafb}.compare-mode .specification-line{align-items:flex-start}.compare-mode .specification-line>strong{padding-top:2px;font-size:11px}.compare-mode .specification-line>span{font-size:12px;line-height:1.65}.compare-mode .specification-meta>strong{font-size:13px}.compare-mode .specification-meta>span{font-size:11px}.compare-mode .supplier-options{padding:0;background:#fff}.compare-mode .supplier-option-table{border:0;border-top:1px solid #e3eaed;border-radius:0}.compare-mode .supplier-option-table :deep(th.el-table__cell){height:34px;background:#fff}.compare-mode .supplier-option-table :deep(td.el-table__cell){padding:7px 0}.compare-mode .selected-cfr-list{justify-content:center}.compare-mode .cfr-chip{min-width:240px;text-align:left}@media(max-width:900px){.compare-mode .fixed-category-calculation .manual-inputs{grid-template-columns:repeat(2,minmax(150px,1fr))}.compare-mode .specification-line{flex-direction:column;gap:3px}}@media(max-width:600px){.compare-mode .fixed-category-calculation .manual-inputs{grid-template-columns:1fr}}
.customer-category-panel{margin-bottom:12px}.customer-category-panel .compact-customer-heading{padding:9px 14px}.customer-category-panel .compact-customer-heading p{margin-top:3px;line-height:1.3}.customer-category-title{gap:6px;margin:2px 0}.trade-term-tag{--el-tag-text-color:#174f64;--el-tag-border-color:#afd3df;--el-tag-bg-color:#f2fafc;font-weight:700}.trade-currency-tag{--el-tag-text-color:#52707d;--el-tag-border-color:#d4e2e7;--el-tag-bg-color:#fff}.customer-candidate-count{margin-left:5px;color:#5f7884;font-size:11px}.customer-quote-comparison{overflow:hidden;border-top:1px solid #dce7eb;background:#fff}.customer-quote-grid{display:grid;grid-template-columns:minmax(0,28fr) minmax(100px,15fr) minmax(0,57fr);align-items:center}.customer-quote-grid--header{min-height:34px;padding:0 16px;background:#f5f8fa;color:#365c6e;font-size:12px;font-weight:650}.customer-quote-grid--header span:nth-child(2){text-align:center}.customer-quote-grid--row{min-height:74px;padding:8px 16px}.customer-quote-product+.customer-quote-product{border-top:1px solid #e0e8eb}.customer-product-identity{display:flex;align-items:center;gap:9px;min-width:0;padding-right:18px}.customer-product-identity>div{display:grid;min-width:0;gap:3px}.customer-product-identity strong{overflow:hidden;color:#153f52;font-size:13px;text-overflow:ellipsis;white-space:nowrap}.customer-product-identity small{overflow:hidden;color:#758892;font-size:10px;font-weight:400;line-height:1.35;text-overflow:ellipsis;white-space:nowrap}.customer-product-toggle{display:grid;flex:0 0 24px;width:24px;height:24px;padding:0;border:0;border-radius:5px;background:transparent;color:#397a91;cursor:pointer;place-items:center}.customer-product-toggle:hover{background:#edf6f8}.customer-product-toggle .el-icon{font-size:12px;transition:transform .18s ease}.customer-product-toggle .el-icon.is-expanded{transform:rotate(180deg)}.customer-product-quantity{justify-self:center;color:#184f65;font-size:12px;font-variant-numeric:tabular-nums;white-space:nowrap}.supplier-quote-list{display:grid;gap:6px;min-width:0;padding-left:12px}.supplier-quote-row{display:grid;grid-template-columns:minmax(0,1fr) minmax(135px,auto);align-items:center;gap:12px;min-height:38px;padding:7px 12px;border:1px solid #cfe1e6;border-radius:7px;background:#f5fafb}.supplier-quote-name{overflow:hidden;color:#254f61;font-size:11px;font-weight:650;text-overflow:ellipsis;white-space:nowrap}.supplier-quote-price{justify-self:end;color:#096e82;font-size:12px;font-variant-numeric:tabular-nums;white-space:nowrap}.supplier-quote-expand{justify-self:start;margin-top:3px;padding:3px 2px;border:0;background:transparent;color:#2385a2;font-size:10px;cursor:pointer}.supplier-quote-expand:hover{text-decoration:underline}.supplier-quote-expand span{margin-left:3px;font-size:8px}.customer-quote-product>.customer-specification-groups{border-top:1px solid #e0e8eb}.customer-product-table :deep(th.el-table__cell){height:34px}.customer-product-table :deep(td.el-table__cell){padding:7px 0}.selected-cfr-list{display:grid;gap:4px;width:100%}.cfr-chip{display:grid;grid-template-columns:minmax(135px,.8fr) minmax(180px,1.25fr) auto;gap:10px;width:100%;padding:5px 8px;text-align:left}.cfr-chip__supplier{color:#284d5e!important;font-weight:600}.cfr-chip__spec{color:#6d808a!important}.cfr-chip strong{justify-self:end}.customer-product-table :deep(.cell){padding-left:10px;padding-right:10px}@media(max-width:1100px){.customer-quote-grid--row{padding-left:11px;padding-right:11px}.cfr-chip{grid-template-columns:minmax(120px,.8fr) minmax(150px,1fr) auto}}@media(max-width:760px){.customer-quote-grid--header{display:none}.customer-quote-grid--row{grid-template-columns:1fr;gap:8px}.customer-product-quantity{justify-self:start;margin-left:33px}.supplier-quote-list{padding-left:33px}.cfr-chip{grid-template-columns:1fr auto}.cfr-chip__spec{grid-column:1/-1;grid-row:2}.customer-category-panel .compact-customer-heading p{display:none}}@media(max-width:520px){.supplier-quote-row{grid-template-columns:minmax(0,1fr) auto;gap:8px;padding:6px 9px}.supplier-quote-list{padding-left:0}}
.customer-quote-grid{grid-template-columns:minmax(0,32fr) minmax(95px,16fr) minmax(0,52fr)}
.customer-quote-grid--row{align-items:start;min-height:0;padding:7px 14px}
.customer-product-identity{align-items:flex-start;padding-top:5px;padding-right:14px}
.customer-product-quantity{padding-top:5px}
.supplier-quote-list{gap:0;padding-left:0;overflow:hidden;border:1px solid #d8e5e9;border-radius:6px;background:#fff}
.supplier-quote-row{min-height:32px;padding:5px 10px;border:0;border-radius:0;background:#fff}
.supplier-quote-row+.supplier-quote-row{border-top:1px solid #e4ecef}
.supplier-quote-expand{margin:0;padding:5px 10px;border-top:1px solid #e4ecef;background:#fff}
@media(max-width:760px){.customer-product-identity,.customer-product-quantity{padding-top:0}.supplier-quote-list{margin-left:33px;padding-left:0}.supplier-quote-expand{width:100%;text-align:left}}
@media(max-width:520px){.supplier-quote-list{margin-left:0}}
/* Keep the selected prices readable without making each quote a nested card. */
.compare-mode .compact-category-heading{gap:12px}
.compare-mode .group-count{min-width:0;flex-wrap:wrap;justify-content:flex-end;white-space:normal}
.compare-mode .fixed-category-calculation .manual-inputs{grid-template-columns:repeat(auto-fit,minmax(150px,190px));justify-content:start;gap:10px}
.compare-mode .fixed-category-calculation .manual-inputs label{min-width:0}
.compare-mode .fixed-category-calculation .fixed-formula{min-width:0}
.compare-mode .fixed-category-calculation .fixed-formula strong{overflow-wrap:anywhere}
.compare-mode .product-group-table .product-summary{text-align:center}
.compare-mode .product-group-table .selected-cfr-list{display:grid;gap:0;min-width:0;width:100%}
.compare-mode .product-group-table .cfr-chip{display:grid;grid-template-columns:minmax(0,1fr) max-content;align-items:start;gap:6px 12px;min-width:0;width:100%;padding:5px 0;border:0;border-radius:0;background:transparent;text-align:left}
.compare-mode .product-group-table .cfr-chip+.cfr-chip{border-top:1px solid #e2ebee}
.compare-mode .product-group-table .cfr-chip>span{min-width:0;max-width:none;overflow:visible;text-overflow:clip;white-space:normal;overflow-wrap:anywhere}
.compare-mode .product-group-table .cfr-chip strong{justify-self:end;white-space:nowrap;text-align:right}
@media(max-width:700px){
 .compare-mode .group-count{justify-content:flex-start}
 .compare-mode .fixed-category-calculation .manual-inputs{grid-template-columns:repeat(auto-fit,minmax(140px,1fr))}
}
.product-freight-details{margin-top:8px;text-align:left}
.product-freight-details>summary{display:flex;align-items:baseline;flex-wrap:wrap;gap:4px 8px;cursor:pointer;font-size:11px;line-height:1.6;list-style:none;color:#607985}
.product-freight-details>summary::-webkit-details-marker{display:none}
.product-freight-details>summary:focus-visible{outline:2px solid #2385a2;outline-offset:3px;border-radius:3px}
.product-freight-details>summary>strong{color:#176d82;font-variant-numeric:tabular-nums}
.freight-expand-label,.freight-collapse-label{color:#2385a2;text-decoration:underline;text-underline-offset:2px}
.product-freight-details .freight-collapse-label{display:none}
.product-freight-details[open] .freight-expand-label{display:none}
.product-freight-details[open] .freight-collapse-label{display:inline}
.product-freight-prices{display:grid;gap:5px;margin-top:8px;max-height:220px;overflow-y:auto;padding-right:4px;text-align:left}
.product-freight-prices>div{display:flex;align-items:baseline;justify-content:space-between;gap:8px;font-size:11px;line-height:1.5}
.product-freight-prices span{min-width:0;overflow-wrap:anywhere;color:#607985}
.product-freight-prices strong{flex-shrink:0;color:#176d82;font-variant-numeric:tabular-nums;white-space:nowrap}
.product-freight-option{height:auto;min-height:34px;max-width:650px;white-space:normal;line-height:1.5;padding-top:7px;padding-bottom:7px;overflow-wrap:anywhere}
.automatic-calculation-status{align-self:center;color:#176d82;font-size:12px;line-height:1.6;max-width:220px}
.automatic-calculation-status.pending{color:#a18450}
.category-visibility-tools{display:flex;align-items:center;justify-content:flex-end;flex-wrap:wrap;gap:8px 18px}
.category-visibility-tools>span{font-size:12px;color:var(--muted)}
.category-visibility-tools .el-checkbox{flex-shrink:0;margin-right:0}
@media(max-width:700px){.compare-mode .section-tools{flex-wrap:wrap;gap:8px}.category-visibility-tools{justify-content:flex-start}.category-visibility-tools>span{display:none}}
.compare-mode .compact-category-header{display:flex;align-items:center;justify-content:flex-start;flex-wrap:wrap;gap:8px 6px;padding:10px 14px}
.compact-category-identity{display:flex;align-items:center;gap:6px;flex-shrink:0}
.compare-mode .compact-category-header h3{margin:0;font-size:14px;line-height:24px}
.compare-mode .compact-category-header .manual-inputs{display:contents}
.compare-mode .compact-category-header .manual-inputs>label{display:flex;flex-shrink:0;align-items:center;gap:4px;margin:0;font-size:11px;white-space:nowrap;color:#536f7d}
.compare-mode .compact-category-header .manual-inputs .el-input{width:80px;--el-input-height:24px;font-size:12px}
.compare-mode .compact-category-header .manual-inputs :deep(.el-input__wrapper){min-height:24px;padding:0 6px}
.compare-mode .compact-category-header .manual-inputs :deep(.el-input-group__append){padding:0 5px;font-size:11px}
.compare-mode .compact-category-header .category-formula-name{height:24px;font-size:14px;font-weight:700;color:#173f52;text-decoration:underline dotted;text-underline-offset:4px}
.compare-mode .compact-category-header .group-count{margin-left:auto;gap:6px;font-size:11px;flex-wrap:wrap;white-space:normal}
.compare-mode .compact-category-header .group-count :deep(.el-tag){font-size:11px}
.compare-mode .compact-category-header .automatic-calculation-status{max-width:none;font-size:11px}
.compact-formula-content{font-size:12px;line-height:1.7;color:#365c6e}
.compact-formula-content>strong{display:block;font-size:13px}
.compact-formula-content>p{margin:6px 0 0;color:#70838e}
@media(max-width:700px){.compare-mode .compact-category-header{gap:8px;padding:10px}.compare-mode .compact-category-header .group-count{margin-left:0}}
.compare-mode .compact-category-header .compact-quote-count{flex-shrink:0;white-space:nowrap;color:#70828d;font-size:10px}
.compare-mode .compact-category-header .manual-inputs .interest-rate-input{width:80px}
.compare-mode .compact-category-header .manual-inputs .interest-days-input{width:88px}
.compact-category-main{display:flex;align-items:center;flex-wrap:wrap;gap:6px 5px;max-width:100%;min-width:0}
/* Module seams use borders instead of nested cards and whitespace. */
.quote-workspace:is(.compare-mode,.customer-mode){padding:0;background:#fff;border:1px solid #dce6ef;border-top:0;border-radius:0}
.quote-workspace.compare-mode .section-tools{margin:0;padding:6px 12px;min-height:42px;box-sizing:border-box;gap:8px;border:0;border-bottom:1px solid var(--line);border-radius:0;box-shadow:none}
.quote-workspace.compare-mode .section-tools :deep(.el-input__wrapper){min-height:30px}
.quote-workspace.compare-mode .section-tools :deep(.el-checkbox__label){font-size:12px}
.quote-workspace:is(.compare-mode,.customer-mode) .workspace-card{margin:0;border:0;border-radius:0;box-shadow:none}
.quote-workspace:is(.compare-mode,.customer-mode) .workspace-card+.workspace-card{border-top:1px solid #cbdce5}
.quote-workspace.compare-mode .compact-category-header{border-radius:0}
.quote-workspace.compare-mode .quote-table{width:100%;margin:0;border:0;border-radius:0;box-shadow:none}
.quote-workspace.compare-mode .logistics-panel{margin-top:0}
.quote-workspace.compare-mode .logistics-panel .card-heading{padding:10px 12px}
.quote-workspace.compare-mode .logistics-panel .card-heading h3{font-size:14px}
.quote-workspace:is(.compare-mode,.customer-mode) .workspace-footer{position:sticky;bottom:0;margin:0;padding:8px 12px;gap:8px 12px;border:0;border-top:1px solid #cbdce5;border-radius:0;background:#fff;box-shadow:none}
.quote-workspace:is(.compare-mode,.customer-mode) .workspace-footer strong{font-size:12px}
.quote-workspace:is(.compare-mode,.customer-mode) .workspace-footer small{margin-top:2px;font-size:11px}
.quote-workspace:is(.compare-mode,.customer-mode) .workspace-footer .el-button{height:30px;min-height:30px;padding:0 10px;font-size:12px}
.logistics-reference-summary{display:flex;align-items:center;justify-content:space-between;gap:12px;min-height:36px;box-sizing:border-box;padding:7px 12px;cursor:pointer;list-style:none;background:#f2f8f6;color:#315d69;font-size:12px;line-height:20px}
.logistics-reference-summary::-webkit-details-marker{display:none}
.logistics-reference-summary:focus-visible{outline:2px solid #2385a2;outline-offset:-2px}
.logistics-reference-summary>strong{font-size:13px;color:#173f52}
.logistics-reference-actions{display:flex;align-items:center;gap:12px;white-space:nowrap}
.logistics-reference-actions>span:first-child{color:#65857a;font-size:11px}
.logistics-reference-expand,.logistics-reference-collapse{color:#24778e}
.logistics-reference-collapse{display:none}
.logistics-panel[open] .logistics-reference-collapse{display:inline}
.logistics-panel[open] .logistics-reference-expand{display:none}
.logistics-panel[open] .logistics-reference-summary{border-bottom:1px solid var(--line)}
.logistics-reference-chevron{width:6px;height:6px;border-right:1px solid currentColor;border-bottom:1px solid currentColor;transform:rotate(45deg);margin-right:3px;transition:transform .15s}
.logistics-panel[open] .logistics-reference-chevron{transform:rotate(225deg)}
.supplier-reselect{display:block;font-size:11px;margin-top:4px}.single-cfr{font-size:12px;color:#176d82;font-variant-numeric:tabular-nums;white-space:nowrap}
.supplier-original-remark{white-space:pre-wrap;overflow-wrap:anywhere}
.supplier-choice-option{height:auto;min-height:52px;max-width:640px;padding-top:7px;padding-bottom:7px;line-height:1.5;white-space:normal}
.supplier-choice-line{display:flex;align-items:center;gap:12px;padding-right:18px}
.supplier-choice-line>strong{flex:1;min-width:0;overflow-wrap:anywhere}
.supplier-choice-line>span{color:#176d82;white-space:nowrap;font-variant-numeric:tabular-nums}
.supplier-choice-option>small{display:block;color:#70828d;font-size:11px;margin-top:3px}
.supplier-choice-option>.supplier-choice-remark{overflow:hidden;color:#496575;text-overflow:ellipsis;white-space:nowrap}
.supplier-choice-remark strong{margin-right:4px;font-weight:600}
.specification-comparison-table :deep(td.el-table__cell){vertical-align:middle;padding:9px 0}
.specification-comparison-table :deep(.cell){padding:0 9px}
.specification-comparison-table :deep(td.el-table__cell[rowspan]){border-bottom:1px solid #dce6ef}

/* Each product owns its sticky heading and all visible specification rows. */
.quote-workspace.compare-mode .category-panel{overflow:visible}
.comparison-product-table{width:100%;table-layout:fixed;border-collapse:separate;border-spacing:0;font-size:12px;color:#315767}
/* Shell content has 20px scroll padding (16px on narrow screens). */
.comparison-product-table thead{position:sticky;top:-20px;z-index:5;background:#f2f8fa;box-shadow:0 1px 0 #cadde5}
.comparison-product-table th,.comparison-product-table td{padding:9px 12px;border-bottom:1px solid #dce6ef;text-align:left;vertical-align:middle}
.comparison-column-headings th{height:30px;padding:4px 12px;font-size:11px;color:#607985;font-weight:600;background:#f6f9fa}
.comparison-product-table tbody tr{background:#fff}
.comparison-product-table tbody tr.selected-row{background:#f3fafb}
.comparison-product-table tbody tr:hover{background:#edf6f8}
.comparison-product-table .quantity-cell,.comparison-product-table .cfr-cell{text-align:right}
.comparison-product-table .number-cell{white-space:nowrap;font-variant-numeric:tabular-nums;font-weight:600}
.comparison-product-table .single-cfr{font-size:12px}
.comparison-product-table td :deep(.el-select__wrapper){min-height:30px;font-size:12px}
@media(max-width:1000px){.comparison-product-table thead{top:-16px}}
@media(max-width:700px){.comparison-product-table th,.comparison-product-table td{padding-left:7px;padding-right:7px}.comparison-product-table .number-cell,.comparison-product-table .single-cfr{white-space:normal;font-size:11px}}

.description-logistics{display:grid;gap:5px;min-width:0}
.description-logistics-select{width:100%;min-width:0}
.description-comparison-table .single-cfr{white-space:normal}

.logistics-selection-label{display:flex;align-items:center;gap:6px;min-width:0;max-width:100%;width:100%}
.logistics-selection-label>span{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.logistics-selection-label>strong{flex-shrink:0;font-size:11px;color:#176d82;font-variant-numeric:tabular-nums}
.freight-choice-option{min-height:52px;max-width:680px}
.freight-choice-line{display:flex;align-items:center;gap:12px;padding-right:18px}
.freight-choice-line>strong{flex:1;min-width:0;overflow-wrap:anywhere}
.freight-choice-line>span{white-space:nowrap;color:#176d82;font-variant-numeric:tabular-nums}
.freight-choice-option>small{display:block;color:#70828d;font-size:11px;margin-top:3px}
.quote-terms summary{cursor:pointer;padding:8px 14px;font-weight:600}.quote-terms .saved-at{margin-left:12px;font-weight:400}
.negotiation-panel header{display:flex;align-items:center;gap:14px;padding:7px 12px;background:#eef6f8}.negotiation-panel header span{font-size:12px;color:#536f7d}
.negotiation-scroll{overflow-x:auto;container-type:inline-size}.negotiation-grid{width:100%;min-width:760px;table-layout:fixed;border-collapse:collapse;font-size:13px}
.negotiation-grid th,.negotiation-grid td{padding:6px 9px;text-align:left;border-bottom:1px solid var(--line);overflow-wrap:anywhere}
.negotiation-grid th{background:#f4f8fa;color:#365c6e}.negotiation-grid th:first-child{width:9em}.negotiation-grid th:nth-child(3){width:9%}.negotiation-grid th:nth-child(4){width:15%}.negotiation-grid th:nth-child(5){width:14%}.negotiation-grid th:last-child{width:17%}
.negotiation-grid :deep(.el-checkbox__label){padding-left:5px;font-size:12px}
.customer-plan-all :deep(.el-checkbox__label){color:inherit;font-weight:600}
.negotiation-grid :deep(.el-input-group__prepend){padding:0 6px}
.customer-factory-remark-trigger{display:flex;height:auto;margin-top:4px;padding:0;line-height:1.4}
.customer-factory-remark-details{max-height:50vh;overflow:auto;margin:0;line-height:1.5}
.customer-factory-remark-details dt{font-weight:600}
.customer-factory-remark-details dd{margin:3px 0 0;white-space:pre-wrap;overflow-wrap:anywhere}
.customer-factory-remark-details dd+dt{margin-top:10px}
@container(max-width:759px){
 .negotiation-grid{min-width:0;display:block}.negotiation-grid thead{position:absolute;width:1px;height:1px;clip-path:inset(50%);overflow:hidden}.negotiation-grid tbody{display:block}
 .negotiation-grid tr{display:grid;grid-template-columns:1fr 1fr;align-items:start;padding:6px 8px;border-bottom:1px solid var(--line)}
 .negotiation-grid td{display:block;border:0;padding:3px 5px}.negotiation-grid td:first-child,.negotiation-grid td:nth-child(2){grid-column:1/-1}
 .negotiation-grid td:last-child{text-align:left}.negotiation-grid td:not(:nth-child(2))::before{content:attr(data-label);display:block;color:#536f7d;font-size:11px}
}
.customer-mode .document-panel{margin:0;padding:8px 12px;border:0;border-top:1px solid var(--line);border-radius:0;box-shadow:none}
.customer-mode .quote-terms{container-type:inline-size}
.customer-mode .document-panel h3{margin:0 0 3px;color:#284e60;font-size:14px;font-weight:600;line-height:1.45}
.customer-mode .document-panel p{color:#566d7a;font-size:12px;line-height:1.5}
.customer-mode .document-actions{gap:8px}
.customer-mode .document-actions label{min-width:128px;gap:3px;font-size:12px;line-height:1.4}
.customer-mode .document-actions :deep(.el-select__wrapper){min-height:30px;font-size:13px}
.customer-mode .document-actions :deep(.el-button){height:30px;padding:0 12px;font-size:13px;font-weight:500}
.customer-mode .customer-fields{grid-template-columns:9rem 9rem minmax(0,1fr);align-items:start;padding:4px 12px 10px;gap:8px 12px}
.customer-mode .customer-fields :deep(.el-form-item__label){margin-bottom:3px;font-size:12px}
.customer-mode .customer-fields :deep(.el-autocomplete){width:100%}
.customer-mode .customer-fields > :last-child{grid-column:1/-1}
@container(max-width:600px){.customer-mode .customer-fields{grid-template-columns:repeat(2,minmax(0,1fr))}.customer-mode .customer-fields > :nth-child(3){grid-column:1/-1}}
@container(max-width:400px){.customer-mode .customer-fields{grid-template-columns:minmax(0,1fr)}}
.customer-mode .customer-fields :deep(.el-form-item){margin-bottom:0;min-width:0}
.customer-mode .customer-fields :deep(.el-date-editor){width:100%;max-width:100%}
.customer-mode .workspace-footer{flex-wrap:wrap}
</style>
