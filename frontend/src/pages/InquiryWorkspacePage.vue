<template>
 <main class="inquiry-workspace inquiry-workspace--operations" v-loading="busy">
  <WorkflowPageHeader v-if="!item" :title="title" :description="subtitle">
   <template v-if="view==='SALES'" #actions><div class="heading-actions"><el-button @click="router.push('/sales/settings/inquiry-templates')">{{t('inquiryWorkspace.templates')}}</el-button><el-button class="mail-import-action" @click="router.push('/emails')">{{t('inquiryWorkspace.fromMailbox')}}</el-button></div></template>
  </WorkflowPageHeader>
  <el-card v-if="!item" class="inquiry-list-panel" shadow="never">
   <div class="toolbar list-toolbar"><el-input v-model="keyword" :placeholder="t(department?'inquiryWorkspace.departmentSearch':'inquiryWorkspace.search')" clearable @change="loadList"/><el-select v-model="state" clearable :placeholder="t('inquiryWorkspace.allStatuses')" @change="loadList"><el-option v-for="o in states" :key="o.value" :value="o.value" :label="o.label"/></el-select><el-button @click="loadList">{{t('common.refresh')}}</el-button><el-button v-if="view==='SALES'&&canWrite" class="create-inquiry-action" type="primary" @click="openCreateDialog">{{t('inquiryWorkspace.uploadOrCreate')}}</el-button></div>
   <el-table class="inquiry-list-table" :data="items" stripe @row-dblclick="open"><el-table-column :label="t('inquiryWorkspace.number')" min-width="190"><template #default="{row}"><span class="inquiry-number">{{row.number}}</span></template></el-table-column><el-table-column prop="body.customer" :label="t('inquiryWorkspace.customer')" min-width="175" show-overflow-tooltip/><el-table-column prop="owner" :label="t('inquiryWorkspace.owner')" min-width="105"/><el-table-column :label="t('inquiryWorkspace.productCount')" min-width="95"><template #default="{row}">{{row.body.products?.length||0}}</template></el-table-column><el-table-column v-if="department" :label="t('inquiryWorkspace.totalQuantity')" min-width="120"><template #default="{row}">{{productTotal(row.body.products,'quantity','unit')}}</template></el-table-column><el-table-column v-if="view==='LOGISTICS'" :label="t('inquiryWorkspace.weightVolume')" min-width="180"><template #default="{row}">{{productTotal(row.body.products,'weight')}} / {{productTotal(row.body.products,'volume')}}</template></el-table-column><el-table-column v-if="department" prop="body.delivery" :label="t('inquiryWorkspace.delivery')" min-width="120"/><el-table-column v-if="view==='LOGISTICS'" prop="body.loadingPort" :label="t('inquiryWorkspace.loadingPort')" min-width="120"/><el-table-column v-if="view==='LOGISTICS'" prop="body.destinationPort" :label="t('inquiryWorkspace.destinationPort')" min-width="120"/><el-table-column v-if="view==='LOGISTICS'" prop="body.incoterm" :label="t('inquiryWorkspace.incoterm')" min-width="100"/><el-table-column v-if="department" :label="t(view==='PROCUREMENT'?'inquiryWorkspace.submittedFactoryQuotes':'inquiryWorkspace.submittedLogisticsQuotes')" min-width="135"><template #default="{row}">{{view==='PROCUREMENT'?row.procurementCount:row.logisticsCount}}</template></el-table-column><el-table-column :label="t('inquiryWorkspace.submittedAt')" min-width="160"><template #default="{row}">{{displayTime(row.submittedAt)}}</template></el-table-column><el-table-column :label="t('common.status')" min-width="145"><template #default="{row}"><el-tag effect="light" :type="row.state==='WITHDRAWN'?'info':row.state==='INQUIRING'?'success':'warning'">{{view==='QUOTATIONS'?offerStateLabel(row.id):stateLabel(row)}}</el-tag></template></el-table-column><el-table-column :label="t('common.actions')" min-width="150" align="center"><template #default="{row}"><el-button :link="!department" :plain="department" type="primary" @click="open(row)">{{listActionLabel(row)}}</el-button></template></el-table-column></el-table>
   <el-pagination class="list-pagination" v-model:current-page="page" v-model:page-size="size" :page-sizes="[20,50,100]" :total="total" layout="total, sizes, prev, pager, next" @change="loadList"/>
  </el-card>
  <template v-else>
   <section class="detail-hero">
     <div><span class="detail-kicker">{{t(item.number?'inquiryWorkspace.detail.inquiryNo':'inquiryWorkspace.detail.creating')}}</span><h3>{{item.number||t('inquiryWorkspace.detail.newInquiry')}}</h3><div v-if="item.body.title" class="detail-title">{{item.body.title}}</div><div class="detail-meta"><el-tag :type="item.state==='INQUIRING'?'success':item.state==='WITHDRAWN'?'info':'warning'">{{stateLabel(item)}}</el-tag><span>{{t('inquiryWorkspace.detail.owner',{name:item.owner})}}</span><span>{{t('inquiryWorkspace.detail.products',{count:item.body.products.length})}}</span><span v-if="view!=='SALES'">{{t('inquiryWorkspace.detail.quotes',{count:item.quotes?.length||0})}}</span></div></div>
     <div class="detail-actions"><el-button v-if="view==='SALES'&&item.canEdit&&item.state!=='INQUIRING'&&auth.can('sales:inquiry:submit')" type="primary" @click="submit">{{item.state==='WITHDRAWN'?t('inquiryWorkspace.detail.resubmit'):t('inquiryWorkspace.detail.submit')}}</el-button><el-button v-if="department&&canWrite&&!editor" type="primary" @click="editQuote()">{{t(view==='PROCUREMENT'?'inquiryWorkspace.detail.addFactoryQuote':'inquiryWorkspace.detail.addLogisticsQuote')}}</el-button><el-dropdown trigger="click" placement="bottom-end" popper-class="inquiry-action-menu" @visible-change="moreOpen=$event" @command="handleMore"><el-button class="more-actions" :class="{'is-open':moreOpen}">{{t('inquiryWorkspace.detail.more')}}<span class="more-arrow" aria-hidden="true">▼</span></el-button><template #dropdown><el-dropdown-menu><el-dropdown-item command="back"><el-icon><Back/></el-icon><span>{{t('inquiryWorkspace.detail.back')}}</span></el-dropdown-item><el-dropdown-item command="reload"><el-icon><Refresh/></el-icon><span>{{t('inquiryWorkspace.detail.reload')}}</span></el-dropdown-item><el-dropdown-item v-if="view==='SALES'&&item.canEdit&&item.state!=='INQUIRING'" class="menu-action-save" command="save"><el-icon><DocumentChecked/></el-icon><span>{{t('inquiryWorkspace.detail.saveDraft')}}</span></el-dropdown-item><el-dropdown-item v-if="view==='SALES'&&item.canEdit&&item.state==='INQUIRING'" class="menu-action-withdraw" divided :disabled="!item.canWithdraw" command="withdraw"><el-icon><RefreshLeft/></el-icon><span>{{t('inquiryWorkspace.detail.withdraw')}}</span></el-dropdown-item></el-dropdown-menu></template></el-dropdown></div>
    </section>
    <el-alert v-if="view==='SALES'&&(item.readOnlyReason||item.withdrawReason)" :title="item.readOnlyReason||item.withdrawReason" type="info" :closable="false" show-icon/>
    <el-tabs v-model="detailTab" class="detail-tabs">
     <el-tab-pane :label="t('inquiryWorkspace.detail.overview')" name="overview">
     <div class="overview-grid">
     <section class="business-card customer-card"><div class="section-title"><div><h3>{{t('inquiryWorkspace.detail.customerDelivery')}}</h3><p>{{t('inquiryWorkspace.detail.customerDeliveryHint')}}</p></div></div><el-form label-position="top" :disabled="!editable" class="inquiry-fields"><el-form-item :label="t('inquiryWorkspace.customer')"><el-autocomplete v-if="editable" v-model="item.body.customer" :fetch-suggestions="customerSuggestions" :placeholder="t('inquiryWorkspace.detail.customerSearch')" clearable @select="selectCustomer" @input="customerTyped"><template #default="{item:option}"><div class="option-title">{{option.name}}</div><span class="option-meta">{{option.code}}</span></template></el-autocomplete><el-input v-else v-model="item.body.customer" placeholder="—"/></el-form-item><el-form-item :label="t('inquiryWorkspace.detail.contact')"><el-autocomplete v-if="editable" v-model="item.body.contact" :fetch-suggestions="contactSuggestions" :placeholder="t('inquiryWorkspace.detail.contactSearch')" clearable @select="selectContact" @input="contactTyped"><template #default="{item:option}"><div class="option-title">{{option.name}}</div><span class="option-meta">{{contactOptionLabel(option)}}</span></template></el-autocomplete><el-input v-else v-model="item.body.contact" placeholder="—"/></el-form-item><el-form-item :label="t('inquiryWorkspace.delivery')"><el-input v-model="item.body.delivery" placeholder="—"/></el-form-item><el-form-item :label="t('inquiryWorkspace.loadingPort')"><el-input v-model="item.body.loadingPort" placeholder="—"/></el-form-item><el-form-item :label="t('inquiryWorkspace.destinationPort')"><el-input v-model="item.body.destinationPort" placeholder="—"/></el-form-item><el-form-item :label="t('inquiryWorkspace.detail.remark')" class="wide"><el-input v-model="item.body.remark" type="textarea" :rows="2" placeholder="—"/></el-form-item></el-form></section>
     </div></el-tab-pane>
     <el-tab-pane :label="t('inquiryWorkspace.detail.productRequirements',{count:item.body.products.length})" name="products"><section class="business-card product-card"><div class="section-title"><div><h3>{{t('inquiryWorkspace.detail.requirementsTitle')}}</h3><p>{{t('inquiryWorkspace.detail.requirementsHint')}}</p></div><div class="template-actions"><template v-if="view==='SALES'&&editable"><span class="template-label">{{t('inquiryWorkspace.templates')}}</span><el-select :model-value="item.body.template?.id" :placeholder="t('inquiryWorkspace.detail.selectTemplate')" @change="changeTemplate"><el-option v-for="template in selectableTemplates" :key="template.id" :label="`${templateName(template)} · v${template.version}${template.status==='ACTIVE'?'':t('inquiryWorkspace.detail.historical')}`" :value="template.id"/></el-select><el-button type="primary" plain @click="addProduct">{{t('inquiryWorkspace.detail.addProduct')}}</el-button></template><el-tag v-else-if="item.body.template" effect="plain">{{templateName(item.body.template)}} · v{{item.body.template.version}}</el-tag></div></div><el-collapse v-if="editor" v-model="requirementsOpen"><el-collapse-item :title="t('inquiryWorkspace.detail.viewRequirements')" name="products"><InquiryProducts :products="item.body.products" :fields="templateFields"/></el-collapse-item></el-collapse><InquiryProducts v-else :products="item.body.products" :fields="templateFields" :editable="!!editable" @remove="item.body.products.splice($event,1)"/></section></el-tab-pane>
     <el-tab-pane v-if="view!=='SALES'" :label="view==='QUOTATIONS'?t('inquiryWorkspace.quoteComparisonCalculation'):t('inquiryWorkspace.detail.submittedQuotes',{count:item.quotes?.length||0})" name="quotes">
     <section v-if="!editor&&view!=='QUOTATIONS'" class="business-card quotes-section">
      <div class="section-title">
       <div><h3>{{ quoteSectionTitle }}</h3><p>{{t('inquiryWorkspace.quotes.listHint')}}</p></div>
       <el-tag effect="plain">{{t('inquiryWorkspace.quotes.count',{count:item.quotes?.length||0})}}</el-tag>
      </div>
      <el-table :data="item.quotes||[]" :max-height="500" stripe :empty-text="t('inquiryWorkspace.quotes.empty')">
       <el-table-column prop="body.company" :label="t('inquiryWorkspace.quotes.quotingParty')" min-width="180" show-overflow-tooltip/>
       <el-table-column :label="t('inquiryWorkspace.quotes.type')" min-width="145"><template #default="{row:q}"><el-tag size="small" :type="q.kind==='PROCUREMENT'?'warning':'success'">{{q.kind==='PROCUREMENT'?quoteCategoryLabel(q.body.quoteCategory):t('inquiryWorkspace.quotes.forwarder')}}</el-tag></template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.quotes.content')" min-width="250"><template #default="{row:q}"><strong class="compact-quote">{{quoteSummary(q)}}</strong><small>{{quoteSubline(q)}}</small></template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.quotes.author')" min-width="180"><template #default="{row:q}">{{q.author||'—'}}<small>{{q.updatedBy&&q.updatedBy!==q.author?t('inquiryWorkspace.quotes.updatedBy',{name:q.updatedBy}):''}}</small></template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.submittedAt')" min-width="155"><template #default="{row:q}">{{displayTime(q.submittedAt)}}</template></el-table-column>
       <el-table-column :label="t('common.status')" width="100"><template #default="{row:q}"><el-tag size="small" effect="plain" :type="q.historical?'info':q.submittedAt?'success':'warning'">{{quoteStatus(q)}}</el-tag></template></el-table-column>
       <el-table-column :label="t('common.actions')" width="180" fixed="right"><template #default="{row:q}"><el-button v-if="q.canEdit" link type="primary" @click="editQuote(q)">{{t('inquiryWorkspace.quotes.edit')}}</el-button><el-button link type="primary" @click="preview=q">{{t('inquiryWorkspace.quotes.viewFull')}}</el-button></template></el-table-column>
      </el-table>
     </section>
   <section v-if="editor" class="quote-editor"><div class="quote-editor-heading"><div><h3>{{ quoteEditorTitle }}</h3><p>{{t(view==='LOGISTICS'?'inquiryWorkspace.quotes.shippingEditorHint':'inquiryWorkspace.quotes.editorHint')}}</p></div><el-tag effect="plain">{{view==='LOGISTICS'?t('inquiryWorkspace.quotes.salesOutputUsd'):t('inquiryWorkspace.quotes.originalQuote')}}</el-tag></div><el-form label-position="top" class="inquiry-fields"><template v-if="view==='PROCUREMENT'"><el-form-item :label="t('inquiryWorkspace.quotes.factorySupplier')"><el-autocomplete v-model="editor.body.company" :fetch-suggestions="companySuggestions" :placeholder="t('inquiryWorkspace.quotes.selectOrType')" :trigger-on-focus="true"/></el-form-item><el-form-item :label="t('inquiryWorkspace.quotes.quoteCategory')" required><el-select v-model="editor.body.quoteCategory" :placeholder="t('inquiryWorkspace.quotes.selectQuoteCategory')" @change="quoteCategoryChanged"><el-option v-for="category in quoteCategoryOptions" :key="category.value" :value="category.value" :label="category.label"/></el-select></el-form-item><el-form-item :label="t('inquiryWorkspace.quotes.currency')"><el-input v-model="editor.body.currency" maxlength="3" :disabled="!!procurementQuoteCurrency(editor.body.quoteCategory)"/></el-form-item><el-form-item :label="t('inquiryWorkspace.quotes.unifiedDelivery')"><el-date-picker v-model="editor.body.delivery" type="date" value-format="YYYY-MM-DD"/></el-form-item></template><template v-else><el-form-item :label="t('inquiryWorkspace.quotes.forwarderName')"><el-autocomplete v-model="editor.body.company" :fetch-suggestions="companySuggestions" :placeholder="t('inquiryWorkspace.quotes.shippingCompanyPlaceholder')" :trigger-on-focus="true" clearable/></el-form-item><el-form-item v-for="f in logisticsPrimaryFields" :key="f.key" :label="f.label"><el-input v-model="editor.body[f.key]"/></el-form-item><el-form-item :label="t('inquiryWorkspace.quotes.departure')"><el-date-picker v-model="editor.body.departure" value-format="YYYY-MM-DD"/></el-form-item><el-form-item :label="t('inquiryWorkspace.quotes.arrival')"><el-date-picker v-model="editor.body.arrival" value-format="YYYY-MM-DD"/></el-form-item></template></el-form>
    <el-collapse><el-collapse-item :title="t('inquiryWorkspace.quotes.moreInfo')" name="more"><el-form label-position="top" class="inquiry-fields"><template v-if="view==='LOGISTICS'"><el-form-item :label="t('inquiryWorkspace.quotes.route')"><el-input v-model="editor.body.route" :placeholder="t('inquiryWorkspace.quotes.routeHint')"/></el-form-item></template><el-form-item :label="t('inquiryWorkspace.quotes.validUntil')"><el-date-picker v-model="editor.body.validUntil" value-format="YYYY-MM-DD"/></el-form-item><el-form-item :label="t('inquiryWorkspace.quotes.paymentTerms')"><el-input v-model="editor.body.paymentTerms"/></el-form-item><el-form-item :label="t('inquiryWorkspace.quotes.attachments')" class="wide"><input type="file" @change="upload($event,true)"/><el-tag v-for="a in editor.body.attachments" :key="a.key" closable @close="editor!.body.attachments=editor!.body.attachments.filter(x=>x.key!==a.key)">{{a.name}}</el-tag></el-form-item><el-form-item :label="t('inquiryProducts.fields.remark')" class="wide"><el-input v-model="editor.body.remark" type="textarea"/></el-form-item></el-form></el-collapse-item></el-collapse>
     <template v-if="view==='PROCUREMENT'"><div class="toolbar"><el-input v-model="productSearch" :placeholder="t('inquiryWorkspace.quotes.searchProducts')" @input="productPage=1"/><el-select v-model="productFilter" @change="productPage=1"><el-option :label="t('inquiryWorkspace.quotes.allProducts')" value="all"/><el-option :label="t('inquiryWorkspace.quotes.unquotedProducts')" value="unquoted"/><el-option :label="t('inquiryWorkspace.quotes.quotedProducts')" value="quoted"/></el-select><span>{{t('inquiryWorkspace.quotes.quotedProgress',{quoted:editor.body.prices.length,total:item.body.products.length})}}</span></div><el-table :data="pagedProducts" row-key="id" :max-height="520"><el-table-column :label="t('inquiryProducts.select')" width="65"><template #default="{row}"><el-checkbox :model-value="!!priceFor(row.id)" @change="toggleProduct(row.id,!!$event)"/></template></el-table-column><el-table-column prop="product" :label="t('inquiryProducts.fields.product')" min-width="180"/><el-table-column prop="specification" :label="t('inquiryProducts.fields.specification')" min-width="190"/><el-table-column prop="quantity" :label="t('inquiryWorkspace.quotes.requiredQuantity')" width="100"/><el-table-column prop="unit" :label="t('inquiryProducts.fields.unit')" width="75"/><el-table-column v-if="editor.body.quoteCategory==='REPROCESSING_CNY'" :label="t('inquiryWorkspace.quotes.exFactoryUnitPrice')" min-width="130"><template #default="{row}"><el-input v-if="priceFor(row.id)" v-model="priceFor(row.id)!.factoryPrice"/></template></el-table-column><el-table-column :label="quotePriceLabel(editor.body.quoteCategory)" min-width="130"><template #default="{row}"><el-input v-if="priceFor(row.id)" v-model="priceFor(row.id)!.price"/></template></el-table-column><el-table-column :label="t('inquiryWorkspace.quotes.deliveryDate')" min-width="180"><template #default="{row}"><el-date-picker v-if="priceFor(row.id)" v-model="priceFor(row.id)!.delivery" value-format="YYYY-MM-DD" :placeholder="editor.body.delivery||t('inquiryWorkspace.quotes.deliveryDate')"/></template></el-table-column><el-table-column :label="t('inquiryProducts.fields.remark')" min-width="170"><template #default="{row}"><el-input v-if="priceFor(row.id)" v-model="priceFor(row.id)!.remark"/></template></el-table-column></el-table><p>{{quotePriceHint(editor.body.quoteCategory)}}</p><el-pagination v-model:current-page="productPage" v-model:page-size="productSize" :page-sizes="[20,50,100]" :total="filteredProducts.length" layout="total, sizes, prev, pager, next"/></template>
     <template v-else>
      <section v-if="nonUsdCurrencies.length" class="exchange-rate-panel">
       <div class="exchange-rate-copy"><span>{{t('inquiryWorkspace.quotes.exchangeRates')}}</span><strong>{{t('inquiryWorkspace.quotes.exchangeRatesHint')}}</strong></div>
       <label v-for="currency in nonUsdCurrencies" :key="currency" class="exchange-rate-field"><span>1 USD =</span><el-input v-model="editor.body.exchangeRates[currency]" inputmode="decimal" :placeholder="t('inquiryWorkspace.quotes.exchangeRatePlaceholder')"><template #append>{{currency}}</template></el-input></label>
      </section>
      <div class="logistics-section-title"><div><h4>{{t('inquiryWorkspace.quotes.applicableCargo')}}</h4><p>{{t('inquiryWorkspace.quotes.applicableCargoHint')}}</p></div></div>
      <el-table :data="item.body.products" class="freight-rate-table" table-layout="fixed">
       <el-table-column :label="t('inquiryProducts.select')" width="62" align="center"><template #default="{row}"><el-checkbox :model-value="!!freightRateFor(row.id)" @change="toggleFreightProduct(row,!!$event)"/></template></el-table-column>
       <el-table-column prop="product" :label="t('inquiryProducts.fields.product')" min-width="150" show-overflow-tooltip/>
       <el-table-column prop="quantity" :label="t('inquiryWorkspace.quotes.requiredQuantity')" min-width="90"/>
       <el-table-column :label="t('inquiryWorkspace.quotes.oceanUnitPrice')" min-width="135"><template #default="{row}"><template v-if="freightRateFor(row.id)"><el-input v-model="freightRateFor(row.id)!.price" inputmode="decimal"/><small class="usd-preview">{{usdPreview(freightRateFor(row.id)!.price,freightRateFor(row.id)!.currency)}}</small></template></template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.quotes.currency')" min-width="92"><template #default="{row}"><el-input v-if="freightRateFor(row.id)" v-model="freightRateFor(row.id)!.currency" maxlength="3"/></template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.quotes.pricingUnit')" min-width="100"><template #default="{row}"><el-input v-if="freightRateFor(row.id)" v-model="freightRateFor(row.id)!.unit" :placeholder="row.unit||'MT'"/></template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.quotes.lineTotal')" min-width="145" align="right"><template #default="{row}"><template v-if="freightRateFor(row.id)"><strong>{{freightRateFor(row.id)!.currency}} {{freightOriginalTotal(freightRateFor(row.id)!)||'—'}}</strong><small class="usd-preview">{{freightUsdTotalPreview(freightRateFor(row.id)!)}}</small></template></template></el-table-column>
      </el-table>
      <div v-if="editor.body.freightRates.length" class="charge-summary freight-summary"><span v-for="(v,k) in currentFreightOriginalTotals" :key="k">{{t('inquiryWorkspace.quotes.freightOriginalTotal',{currency:k,total:v})}}</span><strong>{{t('inquiryWorkspace.quotes.freightUsdTotal',{total:currentUsdFreightTotal})}}</strong></div>
      <div class="logistics-section-title"><div><h4>{{t('inquiryWorkspace.quotes.charges')}}</h4><p>{{t('inquiryWorkspace.quotes.chargesHint')}}</p></div><el-button @click="addCharge">{{t('inquiryWorkspace.quotes.addCharge')}}</el-button></div>
      <el-table :data="editor.body.charges" :max-height="420" table-layout="fixed">
       <el-table-column :label="t('inquiryWorkspace.quotes.chargeName')" min-width="130"><template #default="{row}"><el-input v-model="row.name"/></template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.quotes.applicableCargo')" min-width="150"><template #default="{row}"><el-select v-model="row.allocationType" @change="chargeAllocationChanged(row)"><el-option value="DIRECT" :label="t('inquiryWorkspace.quotes.directProduct')"/><el-option value="PER_TON" :label="t('inquiryWorkspace.quotes.perTon')"/><el-option value="FIXED" :label="t('inquiryWorkspace.quotes.fixedShipment')"/></el-select><el-select v-if="row.allocationType==='DIRECT'" v-model="row.productId" :placeholder="t('inquiryWorkspace.quotes.selectProduct')"><el-option v-for="p in item.body.products" :key="p.id" :value="p.id" :label="p.product"/></el-select></template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.quotes.amount')" min-width="125"><template #default="{row}"><el-input v-model="row.amount" inputmode="decimal"/><small class="usd-preview">{{chargeUsdPreview(row)}}</small></template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.quotes.currency')" min-width="86"><template #default="{row}"><el-input v-model="row.currency" maxlength="3"/></template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.quotes.pricingUnit')" min-width="92"><template #default="{row}"><el-input v-model="row.unit"/></template></el-table-column>
       <el-table-column :label="t('inquiryWorkspace.quotes.billingQuantity')" min-width="90"><template #default="{row}"><el-input v-model="row.quantity" inputmode="decimal"/></template></el-table-column>
       <el-table-column :label="t('inquiryProducts.fields.remark')" min-width="130"><template #default="{row}"><el-input v-model="row.remark"/></template></el-table-column>
       <el-table-column width="64"><template #default="{$index}"><el-button link type="danger" @click="editor!.body.charges.splice($index,1)">{{t('common.remove')}}</el-button></template></el-table-column>
      </el-table>
      <div v-if="Object.keys(chargeTotals(editor.body.charges)).length" class="charge-summary"><span v-for="(v,k) in chargeTotals(editor.body.charges)" :key="k">{{t('inquiryWorkspace.quotes.totalCharges',{currency:k,total:v})}}</span><strong>{{t('inquiryWorkspace.quotes.usdChargesTotal',{total:currentUsdChargeTotal})}}</strong></div>
     </template>
     <div class="toolbar"><el-button type="primary" @click="saveQuote(false)">{{t('common.save')}}</el-button><el-button v-if="!editor.submittedAt" type="success" @click="saveQuote(true)">{{t('inquiryWorkspace.quotes.submitQuote')}}</el-button><el-button @click="editor=null">{{t('inquiryWorkspace.quotes.backToInquiry')}}</el-button></div>
     </section>
     </el-tab-pane>
     <el-tab-pane v-if="view==='QUOTATIONS'" :label="t('inquiryWorkspace.detail.customerOffer')" name="offer"></el-tab-pane>
    </el-tabs>
    <CustomerOfferEditor v-if="view==='QUOTATIONS'" v-show="detailTab==='quotes'||detailTab==='offer'" :case-id="item.id" :mode="detailTab==='offer'?'offer':'compare'" :source-version="item.quotes.map(q=>`${q.id}:${q.version}`).join(',')" @navigate="detailTab=$event"/>
   </template>
   <el-dialog v-model="uploadOpen" :title="t('procurementIntakes.uploadTitle')" width="min(720px,94vw)" destroy-on-close>
    <el-alert :title="t('procurementIntakes.uploadHint')" type="info" :closable="false" show-icon/>
    <el-form label-position="top" class="upload-form">
     <el-form-item :label="t('procurementIntakes.format')"><div class="upload-template"><el-select v-model="uploadForm.templateId" filterable :placeholder="t('procurementIntakes.templatePlaceholder')"><el-option value="" :label="t('procurementIntakes.autoRecognize')"/><el-option v-for="template in activeTemplates" :key="template.id" :value="String(template.id)" :label="`${templateName(template)} · v${template.version}`"/></el-select><el-button :disabled="!uploadForm.templateId" @click="downloadSelectedTemplate">{{t('procurementIntakes.downloadSelectedTemplate')}}</el-button></div><small>{{t('procurementIntakes.autoFormatHint')}}</small></el-form-item>
     <el-form-item :label="t('procurementIntakes.inquiryTitle')"><el-input v-model="uploadForm.title" :placeholder="t('procurementIntakes.titleAuto')"/></el-form-item>
     <el-form-item :label="t('inquiryWorkspace.customer')" required><el-select v-model="uploadForm.customerId" filterable :placeholder="t('procurementIntakes.customerPlaceholder')" style="width:100%" @change="uploadCustomerChanged"><el-option v-for="option in customerOptions" :key="option.id" :value="option.id" :label="`${option.code} · ${option.name}`"/></el-select></el-form-item>
     <el-form-item :label="t('procurementIntakes.contact')" required><el-select v-model="uploadForm.contactId" filterable :disabled="!uploadForm.customerId" :placeholder="uploadForm.customerId?t('procurementIntakes.contactPlaceholder'):t('procurementIntakes.selectCustomerFirst')" style="width:100%"><el-option v-for="option in contactOptions" :key="option.id" :value="option.id" :label="`${option.name}${contactOptionLabel(option)?` · ${contactOptionLabel(option)}`:''}`"/></el-select></el-form-item>
     <el-form-item :label="t('procurementIntakes.contactEmail')"><el-input :model-value="selectedUploadContact?.email||''" readonly :placeholder="t('procurementIntakes.contactEmailAuto')"/></el-form-item>
     <el-form-item :label="t('procurementIntakes.standardFile')" required><input type="file" accept=".xlsx,.csv" @change="pickImportFile"/></el-form-item>
    </el-form>
    <template #footer><el-button @click="uploadOpen=false">{{t('common.cancel')}}</el-button><el-button @click="startManualInquiry">{{t('inquiryWorkspace.manualEntry')}}</el-button><el-button type="primary" :loading="busy" @click="importInquiry">{{t('procurementIntakes.uploadAndReview')}}</el-button></template>
   </el-dialog>
   <el-dialog :model-value="!!preview" :title="t('inquiryWorkspace.quotes.fullQuote')" width="92%" @close="preview=null"><template v-if="preview&&item"><div class="toolbar"><el-button v-for="a in preview.body.attachments" :key="a.key" link @click="download(a.key)">{{a.name}}</el-button></div><el-descriptions :column="3" border><el-descriptions-item v-if="preview.kind==='PROCUREMENT'" :label="t('inquiryWorkspace.quotes.quoteCategory')">{{quoteCategoryLabel(preview.body.quoteCategory)}}</el-descriptions-item><el-descriptions-item v-for="f in quoteDisplayFields" :key="f.key" :label="f.label">{{preview.body[f.key]||'—'}}</el-descriptions-item></el-descriptions><el-table v-if="preview.kind==='PROCUREMENT'" :data="preview.body.prices" :max-height="480"><el-table-column :label="t('inquiryProducts.fields.product')"><template #default="{row}">{{productFor(row.productId)?.product}}</template></el-table-column><el-table-column :label="t('inquiryProducts.fields.specification')"><template #default="{row}">{{productFor(row.productId)?.specification}}</template></el-table-column><el-table-column v-if="preview.body.quoteCategory==='REPROCESSING_CNY'" prop="factoryPrice" :label="t('inquiryWorkspace.quotes.exFactoryUnitPrice')"/><el-table-column prop="price" :label="quotePriceLabel(preview.body.quoteCategory)"/><el-table-column :label="t('inquiryWorkspace.quotes.deliveryDate')"><template #default="{row}">{{row.delivery||preview.body.delivery||'—'}}</template></el-table-column><el-table-column prop="remark" :label="t('inquiryProducts.fields.remark')"/></el-table><template v-else><p>{{t('inquiryWorkspace.quotes.applicableCargo')}}：{{preview.body.cargoIds?.length?preview.body.cargoIds.map(id=>productFor(id)?.product).join('、'):t('inquiryWorkspace.quotes.wholeShipment')}}</p><el-table :data="preview.body.charges" :max-height="480"><el-table-column v-for="f in chargeFields" :key="f.key" :prop="f.key" :label="f.label"/></el-table><p v-for="(v,k) in preview.body.totals" :key="k">{{t('inquiryWorkspace.quotes.totalCharges',{currency:k,total:v})}}</p></template></template></el-dialog>
 </main>
</template>
<script setup lang="ts">
import {computed,onMounted,onUnmounted,reactive,ref,watch} from 'vue'
import {useRoute,useRouter} from 'vue-router'
import {useI18n} from 'vue-i18n'
import {ElMessage,ElMessageBox} from 'element-plus'
import {Back,DocumentChecked,Refresh,RefreshLeft} from '@element-plus/icons-vue'
import {get,post} from '../api'
import {isAxiosError} from 'axios'
import InquiryProducts from '../components/InquiryProducts.vue'
import CustomerOfferEditor from '../components/CustomerOfferEditor.vue'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
import {onLive} from '../live'
import {useAuthStore} from '../stores/auth'
import type {InquiryTemplate} from '../lib/inquiryTemplates'
import {parseTableFile} from '../lib/attachmentExcel'
import {productsFromImportedSheet,selectImportTemplate} from '../lib/inquiryImport'
import {applyTemplateDefaults,blankBody,blankProduct,blankQuote,canonicalInquiryRouteID,pastePrices,chargeSubtotal,chargeTotals,productTotal,type Inquiry,type InquiryTemplateSnapshot,type Result,type Quote,type QuoteBody,type Product} from '../lib/inquiryWorkspace'
import {procurementQuoteCategories,procurementQuoteCategory,procurementQuoteCurrency} from '../lib/quoteClassification'
const props=defineProps<{view:'SALES'|'QUOTATIONS'|'PROCUREMENT'|'LOGISTICS'}>()
const {t,locale}=useI18n()
const view=computed(()=>props.view),route=useRoute(),router=useRouter(),auth=useAuthStore()
const title=computed(()=>view.value==='PROCUREMENT'?t('sourcing.title'):view.value==='LOGISTICS'?t('presalesShipping.workspaceTitle'):t(view.value==='SALES'?'inquiryWorkspace.inquiryTitle':'inquiryWorkspace.quotationTitle'))
const subtitle=computed(()=>view.value==='PROCUREMENT'?t('sourcing.subtitle'):view.value==='LOGISTICS'?t('presalesShipping.workspaceHint'):t(view.value==='SALES'?'inquiryWorkspace.inquirySubtitle':'inquiryWorkspace.quotationSubtitle'))
const department=computed(()=>view.value==='PROCUREMENT'||view.value==='LOGISTICS')
const canWrite=computed(()=>auth.can(view.value==='SALES'?'sales:inquiry:write':view.value==='PROCUREMENT'?'procurement:sourcing:write':'shipping:sourcing:write'))
const offerStates=ref<Record<string,{status:string;confirmedAt:string}>>({})
const item=ref<Inquiry|null>(null),items=ref<Inquiry[]>([]),total=ref(0),keyword=ref(''),state=ref(''),page=ref(1),size=ref(20),busy=ref(false),editor=ref<Quote|null>(null),preview=ref<Quote|null>(null),detailTab=ref('overview')
const refreshSuspended=ref(false),refreshErrorShown=ref(false)
const moreOpen=ref(false)
interface CustomerOption {value:string;id:string;code:string;name:string}
interface ContactOption {value:string;id:string;name:string;department:string;title:string;email:string;isPrimary:boolean}
const customerOptions=ref<CustomerOption[]>([]),contactOptions=ref<ContactOption[]>([])
const uploadOpen=ref(false)
const uploadForm=reactive<{templateId:string;title:string;customerId:string;contactId:string;file:File|null}>({templateId:'',title:'',customerId:'',contactId:'',file:null})
const selectedUploadContact=computed(()=>contactOptions.value.find(option=>option.id===uploadForm.contactId))
const templates=ref<InquiryTemplate[]>([])
const activeTemplates=computed(()=>templates.value.filter(template=>template.status==='ACTIVE'))
const templateFields=computed(()=>item.value?.body.template?.fields)
const selectableTemplates=computed(()=>{const rows=[...activeTemplates.value];const current=item.value?.body.template;if(current&&!rows.some(row=>row.id===current.id))rows.push({...current,description:'',status:'SUPERSEDED',isDefault:false,isSystem:false,fieldCount:current.fields.length,createdByName:'',updatedAt:''});return rows})
const editable=computed(()=>view.value==='SALES'&&item.value?.canEdit&&item.value.state!=='INQUIRING')
const states=computed(()=>department.value?[{value:'WAITING',label:t('inquiryWorkspace.statuses.waiting')},{value:'QUOTED',label:t('inquiryWorkspace.statuses.hasQuote')}]:view.value==='SALES'?[{value:'UNSUBMITTED',label:t('inquiryWorkspace.statuses.unsubmitted')},{value:'INQUIRING',label:t('inquiryWorkspace.statuses.inquiring')},{value:'WITHDRAWN',label:t('inquiryWorkspace.statuses.withdrawn')}]:[{value:'PENDING',label:t('inquiryWorkspace.statuses.waiting')},{value:'QUOTED',label:t('inquiryWorkspace.statuses.quoted')},{value:'CONFIRMED',label:t('inquiryWorkspace.statuses.confirmed')}])
const quoteSectionTitle=computed(()=>t(view.value==='QUOTATIONS'?'inquiryWorkspace.quotes.originalSupplierQuotes':view.value==='PROCUREMENT'?'inquiryWorkspace.quotes.factoryQuote':'inquiryWorkspace.quotes.shippingQuote'))
const quoteEditorTitle=computed(()=>t(view.value==='PROCUREMENT'?'inquiryWorkspace.quotes.factoryQuote':'inquiryWorkspace.quotes.shippingQuote'))
const quoteCategoryOptions=computed(()=>procurementQuoteCategories.map(category=>({...category,label:t(category.labelKey)})))
const submittedProcurementQuotes=computed(()=>(item.value?.quotes||[]).filter(q=>q.kind==='PROCUREMENT'&&!!q.submittedAt))
const submittedLogisticsQuotes=computed(()=>(item.value?.quotes||[]).filter(q=>q.kind==='LOGISTICS'&&!!q.submittedAt))
const submittedProcurementQuoteGroups=computed(()=>[
 ...quoteCategoryOptions.value.map(category=>({...category,quotes:submittedProcurementQuotes.value.filter(q=>q.body.quoteCategory===category.value)})),
 {value:'UNCLASSIFIED',label:t('inquiryWorkspace.quotes.categories.unclassified'),quotes:submittedProcurementQuotes.value.filter(q=>!procurementQuoteCategory(q.body.quoteCategory))},
])
const submittedQuotesCount=computed(()=>submittedProcurementQuotes.value.length+submittedLogisticsQuotes.value.length)
function submittedCategoryProductRows(category:string){return submittedProcurementQuotes.value.flatMap(quote=>{if(category==='UNCLASSIFIED'?(!!procurementQuoteCategory(quote.body.quoteCategory)):quote.body.quoteCategory!==category)return[];return (quote.body.prices||[]).map(price=>({quote,price,product:productFor(price.productId)}))})}
const productFields=computed(()=>[{key:'product',label:t('inquiryProducts.fields.product'),width:180},{key:'specification',label:t('inquiryProducts.fields.specification'),width:190},{key:'delivery',label:t('inquiryProducts.fields.delivery'),width:150},{key:'weight',label:t('inquiryProducts.fields.weight'),width:100},{key:'volume',label:t('inquiryProducts.fields.volume'),width:100},{key:'packaging',label:t('inquiryProducts.fields.packaging'),width:130},{key:'remark',label:t('inquiryProducts.fields.remark'),width:190}])
type QuoteTextKey={ [K in keyof QuoteBody]: QuoteBody[K] extends string ? K : never }[keyof QuoteBody]
interface QuoteField {key:QuoteTextKey;label:string}
const logisticsPrimaryFields=computed<QuoteField[]>(()=>[{key:'vessel',label:t('inquiryWorkspace.quotes.vessel')},{key:'voyage',label:t('inquiryWorkspace.quotes.voyage')},{key:'loadingPort',label:t('inquiryWorkspace.loadingPort')},{key:'destinationPort',label:t('inquiryWorkspace.destinationPort')},{key:'transitDays',label:t('inquiryWorkspace.quotes.transitDays')}])
const logisticsFields=computed<QuoteField[]>(()=>[...logisticsPrimaryFields.value,{key:'route',label:t('inquiryWorkspace.quotes.route')}])
const quoteDisplayFields=computed<QuoteField[]>(()=>[{key:'company',label:t('inquiryWorkspace.quotes.factoryOrForwarder')},{key:'currency',label:t('inquiryWorkspace.quotes.currency')},{key:'delivery',label:t('inquiryWorkspace.quotes.unifiedDelivery')},{key:'validUntil',label:t('inquiryWorkspace.quotes.validUntil')},{key:'paymentTerms',label:t('inquiryWorkspace.quotes.paymentTerms')},...logisticsFields.value,{key:'departure',label:t('inquiryWorkspace.quotes.departure')},{key:'arrival',label:t('inquiryWorkspace.quotes.arrival')},{key:'remark',label:t('inquiryProducts.fields.remark')}])
const chargeFields=computed(()=>[{key:'name',label:t('inquiryWorkspace.quotes.chargeName'),width:140},{key:'amount',label:t('inquiryWorkspace.quotes.amount'),width:110},{key:'remark',label:t('inquiryProducts.fields.remark'),width:170}])
const productSearch=ref(''),productFilter=ref('all'),productPage=ref(1),productSize=ref(20),requirementsOpen=ref<string[]>([])
const nonUsdCurrencies=computed(()=>{
 const body=editor.value?.body;if(!body||view.value!=='LOGISTICS')return[]
 return [...new Set([...body.freightRates.map(row=>row.currency),...body.charges.map(row=>row.currency)].map(value=>value.trim().toUpperCase()).filter(value=>/^[A-Z]{3}$/.test(value)&&value!=='USD'))].sort()
})
function usdAmount(amount:string,currency:string):number|null{if(!amount.trim())return null;const value=Number(amount),code=currency.trim().toUpperCase();if(!Number.isFinite(value)||value<0)return null;if(code==='USD')return value;const rate=Number(editor.value?.body.exchangeRates?.[code]);return Number.isFinite(rate)&&rate>0?value/rate:null}
function usdPreview(amount:string,currency:string){if(!amount.trim())return t('inquiryWorkspace.quotes.usdEquivalentEmpty');const converted=usdAmount(amount,currency);return converted===null?t('inquiryWorkspace.quotes.exchangeRatePending'):t('inquiryWorkspace.quotes.usdEquivalent',{amount:converted.toFixed(4)})}
function chargeUsdPreview(row:{amount:string;quantity:string;currency:string}){const subtotal=chargeSubtotal(row.amount,row.quantity);return subtotal?usdPreview(subtotal,row.currency):t('inquiryWorkspace.quotes.usdEquivalentEmpty')}
function freightOriginalTotal(rate:{productId:string;price:string}){const quantity=productFor(rate.productId)?.quantity||'';return chargeSubtotal(rate.price,quantity)}
function freightUsdTotalPreview(rate:{productId:string;price:string;currency:string}){const total=freightOriginalTotal(rate);if(!total)return t('inquiryWorkspace.quotes.usdEquivalentEmpty');const converted=usdAmount(total,rate.currency);return converted===null?t('inquiryWorkspace.quotes.exchangeRatePending'):t('inquiryWorkspace.quotes.usdEquivalent',{amount:converted.toFixed(2)})}
const currentFreightOriginalTotals=computed(()=>{const totals:Record<string,number>={};for(const rate of editor.value?.body.freightRates||[]){const raw=freightOriginalTotal(rate),currency=rate.currency.trim().toUpperCase();if(!raw)continue;const total=Number(raw);if(Number.isFinite(total)&&currency)totals[currency]=(totals[currency]||0)+total}return Object.fromEntries(Object.entries(totals).map(([currency,total])=>[currency,total.toFixed(2)]))})
const currentUsdFreightTotal=computed(()=>{let total=0;for(const rate of editor.value?.body.freightRates||[]){const original=freightOriginalTotal(rate);if(!original)return'—';const converted=usdAmount(original,rate.currency);if(converted===null)return t('inquiryWorkspace.quotes.exchangeRatePending');total+=converted}return total.toFixed(2)})
const currentUsdChargeTotal=computed(()=>{if(!editor.value)return'—';let total=0;for(const row of editor.value.body.charges){const subtotal=chargeSubtotal(row.amount,row.quantity),converted=usdAmount(subtotal,row.currency);if(converted===null)return t('inquiryWorkspace.quotes.exchangeRatePending');total+=converted}return total.toFixed(2)})
const filteredProducts=computed(()=>(item.value?.body.products||[]).filter(p=>(`${p.product} ${p.specification}`.toLowerCase().includes(productSearch.value.toLowerCase()))&&(productFilter.value==='all'||(productFilter.value==='quoted')===!!priceFor(p.id))))
const pagedProducts=computed(()=>filteredProducts.value.slice((productPage.value-1)*productSize.value,productPage.value*productSize.value))
function priceFor(id:string){return editor.value?.body.prices.find(p=>p.productId===id)}
function productFor(id:string){return item.value?.body.products.find(p=>p.id===id)}
function quoteTotals(q:Quote){const freight=q.body.freightTotalUsd?t('inquiryWorkspace.quotes.freightUsdTotal',{total:q.body.freightTotalUsd}):'',charges=(q.body.otherChargesTotalUsd||q.body.totalUsd)?t('inquiryWorkspace.quotes.usdChargesTotal',{total:q.body.otherChargesTotalUsd||q.body.totalUsd}):'';return[freight,charges].filter(Boolean).join(' · ')}
function quoteSummary(q:Quote){return q.kind==='PROCUREMENT'?t('inquiryWorkspace.quotes.productCoverage',{quoted:q.body.prices?.length||0,total:item.value?.body.products.length||0,currency:q.body.currency||'—'}):quoteTotals(q)||t('inquiryWorkspace.quotes.waiting')}
function quoteSubline(q:Quote){return q.kind==='PROCUREMENT'?t('inquiryWorkspace.quotes.deliverySummary',{delivery:q.body.delivery||t('inquiryWorkspace.quotes.byProduct')}):t('inquiryWorkspace.quotes.shippingSummary',{carrier:q.body.company||q.body.carrier||t('inquiryWorkspace.quotes.carrierMissing'),route:q.body.route||t('inquiryWorkspace.quotes.routeMissing')})}
function quoteStatus(q:Quote){return t(q.historical?'inquiryWorkspace.quotes.historical':q.submittedAt?'inquiryWorkspace.quotes.submitted':'inquiryWorkspace.quotes.saved')}
function quoteCategoryLabel(value:string){const category=procurementQuoteCategory(value);return category?t(category.labelKey):t('inquiryWorkspace.quotes.categories.unclassified')}
function quoteCategoryChanged(value:string){if(!editor.value)return;const currency=procurementQuoteCurrency(value);if(currency)editor.value.body.currency=currency}
function quotePriceLabel(value:string){return t(value==='REPROCESSING_CNY'?'inquiryWorkspace.quotes.processingFee':'inquiryWorkspace.quotes.quotedUnitPrice')}
function quotePriceHint(value:string){return t(value==='REPROCESSING_CNY'?'inquiryWorkspace.quotes.processingFeeHint':'inquiryWorkspace.quotes.factoryPriceHint')}
function toggleProduct(id:string,yes:boolean){if(!editor.value)return;if(yes&&!priceFor(id))editor.value.body.prices.push({productId:id,price:'',delivery:'',remark:''});else if(!yes)editor.value.body.prices=editor.value.body.prices.filter(p=>p.productId!==id)}
function paste(e:ClipboardEvent,id:string){const text=e.clipboardData?.getData('text')||'';if(editor.value&&/[\t\n]/.test(text)){e.preventDefault();editor.value.body.prices=pastePrices(text,filteredProducts.value,editor.value.body.prices,id)}}
function displayTime(value:string){if(!value)return '—';const date=new Date(value);return Number.isNaN(date.getTime())?value:date.toLocaleString(locale.value,{year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',hour12:false})}
function stateLabel(r:Inquiry){return department.value?((view.value==='PROCUREMENT'?r.procurementCount:r.logisticsCount)>0?t('inquiryWorkspace.statuses.hasQuote'):t('inquiryWorkspace.statuses.waiting')):({UNSUBMITTED:t('inquiryWorkspace.statuses.unsubmitted'),INQUIRING:t('inquiryWorkspace.statuses.inquiring'),WITHDRAWN:t('inquiryWorkspace.statuses.withdrawn')}[r.state]||r.state)}
const retryKeys=new Map<string,string>()
async function command(action:string,extra:object={}){const body={action,view:view.value,...extra};if(['list','get'].includes(action))return post<Result>('/inquiry-workspace',body);const fingerprint=JSON.stringify(body);let key=retryKeys.get(fingerprint);if(!key){key=crypto.randomUUID();retryKeys.set(fingerprint,key)}const r=await post<Result>('/inquiry-workspace',body,{headers:{'Idempotency-Key':key}});retryKeys.delete(fingerprint);return r}
function offerStateLabel(id:string){const s=offerStates.value[id]?.status;return s==='CONFIRMED'?t('inquiryWorkspace.statuses.confirmed'):s==='QUOTED'?t('inquiryWorkspace.statuses.quoted'):t('inquiryWorkspace.statuses.waiting')}
function listActionLabel(row:Inquiry){if(view.value==='PROCUREMENT')return t(row.procurementCount>0?'inquiryWorkspace.actions.factoryContinue':'inquiryWorkspace.actions.factoryNew');if(view.value==='LOGISTICS')return t(row.logisticsCount>0?'inquiryWorkspace.actions.logisticsContinue':'inquiryWorkspace.actions.logisticsNew');return t('inquiryWorkspace.actions.open')}
let listRequest=0
async function loadList(){
 const request=++listRequest
 if(view.value==='QUOTATIONS'&&auth.can('export:quotation:read'))offerStates.value=(await post<{summaries:Record<string,{status:string;confirmedAt:string}>}>('/customer-offer',{action:'summaries'})).summaries
 if(view.value==='QUOTATIONS'&&state.value){
  const matches:Inquiry[]=[];let sourcePage=1,sourceTotal=0
  do{const r=await command('list',{page:sourcePage,size:100,keyword:keyword.value,state:''});if(request!==listRequest)return;sourceTotal=r.total;matches.push(...r.items.filter(i=>(offerStates.value[i.id]?.status||'PENDING')===state.value));if(!r.items.length)break;sourcePage++}while((sourcePage-1)*100<sourceTotal)
  total.value=matches.length;page.value=Math.min(page.value,Math.max(1,Math.ceil(matches.length/size.value)));items.value=matches.slice((page.value-1)*size.value,page.value*size.value);return
 }
 const r=await command('list',{page:page.value,size:size.value,keyword:keyword.value,state:state.value});if(request!==listRequest)return;items.value=r.items;total.value=r.total
}

function showRefreshError(){if(refreshErrorShown.value)return;refreshErrorShown.value=true;ElMessage.error(t('inquiryWorkspace.messages.refreshFailed'))}
async function reload(){
 try{
  if(item.value?.id){const r=await command('get',{id:item.value.id});item.value=normalizeInquiry(r.item)}else await loadList()
  refreshSuspended.value=false;refreshErrorShown.value=false
 }catch(e){
  if(isAxiosError(e)&&[403,404].includes(e.response?.status||0)){item.value=null;preview.value=null;await router.replace({query:{}});await loadList();return}
  refreshSuspended.value=true;showRefreshError()
 }
}
 function initialDetailTab(){return view.value==='SALES'||view.value==='QUOTATIONS'?'overview':'products'}
function normalizeQuote(q:Quote){q.body.prices??=[];q.body.freightRates??=[];q.body.charges??=[];q.body.exchangeRates??={};q.body.totals??={};q.body.totalUsd??='';q.body.freightTotalUsd??='';q.body.otherChargesTotalUsd??=q.body.totalUsd||'';q.body.quoteCategory??='';q.body.incoterm??='';q.body.cargoIds??=[];q.body.attachments??=[];return q}
function normalizeInquiry(inquiry:Inquiry|undefined):Inquiry|null{if(!inquiry)return null;inquiry.body.title??='';inquiry.body.customerId??='';inquiry.body.contactId??='';inquiry.body.products??=[];inquiry.body.attachments??=[];inquiry.quotes??=[];inquiry.quotes.forEach(normalizeQuote);inquiry.body.products.forEach(product=>{product.customFields??={}});return inquiry}
async function open(r:Inquiry){editor.value=null;detailTab.value=initialDetailTab();const out=await command('get',{id:r.id});item.value=normalizeInquiry(out.item);refreshSuspended.value=false;refreshErrorShown.value=false;await router.replace({query:{...route.query,id:r.id}})}
async function back(){if(editor.value||editable.value){await ElMessageBox.confirm(t('inquiryWorkspace.messages.backConfirm'),t('inquiryWorkspace.messages.notice'))}item.value=null;editor.value=null;await router.replace({query:{}});await loadList()}
function templateSnapshot(template:InquiryTemplate):InquiryTemplateSnapshot{return{id:template.id,templateCode:template.templateCode,version:template.version,name:template.name,fields:(template.fields||[]).map(field=>({...field})).sort((a,b)=>a.sortOrder-b.sortOrder)}}
function templateName(template:{templateCode:string;name:string}){if(template.templateCode==='SYSTEM_DEFAULT')return t('inquiryWorkspace.systemTemplates.default');if(template.templateCode==='STEEL_DETAILED_DIMENSIONS')return t('inquiryWorkspace.systemTemplates.steelDetailed');return template.name}
async function loadTemplates(){if(view.value!=='SALES')return;const data=await get<{templates:InquiryTemplate[]}>('/inquiry-templates');templates.value=data.templates||[]}
function create(){const body=blankBody(),template=activeTemplates.value.find(row=>row.isDefault)||activeTemplates.value[0];if(template){body.template=templateSnapshot(template);body.products.forEach(product=>applyTemplateDefaults(product,body.template!.fields))}detailTab.value='overview';item.value={id:'',number:'',ownerId:'',owner:t('inquiryWorkspace.messages.currentSales'),state:'UNSUBMITTED',revision:0,submittedAt:'',body,quotes:[],procurementCount:0,logisticsCount:0,canEdit:true,sourceMailId:'',legacy:false}}
function changeTemplate(id:string){if(!item.value)return;const template=activeTemplates.value.find(row=>row.id===id);if(!template)return;item.value.body.template=templateSnapshot(template);item.value.body.products.forEach(product=>applyTemplateDefaults(product,item.value!.body.template!.fields))}
function addProduct(){if(!item.value)return;const product=blankProduct();applyTemplateDefaults(product,item.value.body.template?.fields||[]);item.value.body.products.push(product)}
async function save(notify=true){if(!item.value)return;busy.value=true;try{const r=await command('save',{id:item.value.id,revision:item.value.revision,body:item.value.body});item.value=normalizeInquiry(r.item);if(notify)ElMessage.success(t('inquiryWorkspace.messages.saved'))}finally{busy.value=false}}
async function submit(){
 if(busy.value)return
 try{
  await save(false)
  if(!item.value)return
  busy.value=true
  const r=await command('submit',{id:item.value.id,revision:item.value.revision})
  item.value=normalizeInquiry(r.item)
  ElMessage.success(t('inquiryWorkspace.messages.submitted'))
 }catch{
  // The API interceptor displays the server error. Saving alone is not submission.
 }finally{busy.value=false}
}
async function withdraw(){
 if(!item.value||busy.value)return
 try{
  await ElMessageBox.confirm(t('inquiryWorkspace.messages.withdrawConfirm'),t('inquiryWorkspace.detail.withdraw'),{type:'warning'})
  busy.value=true
  const r=await command('withdraw',{id:item.value.id,revision:item.value.revision})
  item.value=normalizeInquiry(r.item)
  ElMessage.success(t('inquiryWorkspace.messages.withdrawn'))
 }catch{
  // Cancellation needs no toast; request failures are shown by the API interceptor.
 }finally{busy.value=false}
}
async function handleMore(commandName:string){
 try{if(commandName==='back')await back();else if(commandName==='reload')await reload();else if(commandName==='save')await save();else if(commandName==='withdraw')await withdraw()}
 catch{ /* Dialog cancellation or an error already displayed by the API interceptor. */ }
}
async function loadCustomerOptions(){const data=await get<{customers:{id:string;code:string;name:string}[]}>('/sourcing-customer-options',{page_size:500});customerOptions.value=(data.customers||[]).map(option=>({...option,value:option.name}))}
async function customerSuggestions(query:string,done:(items:CustomerOption[])=>void){try{if(!customerOptions.value.length)await loadCustomerOptions();const needle=query.trim().toLowerCase();done(customerOptions.value.filter(option=>!needle||`${option.code} ${option.name}`.toLowerCase().includes(needle)).slice(0,50))}catch{done([])}}
async function loadContactOptions(customerId:string){if(!customerId){contactOptions.value=[];return}const data=await get<{contacts:Omit<ContactOption,'value'>[]}>(`/sourcing-customer-options/${customerId}/contacts`);contactOptions.value=(data.contacts||[]).map(option=>({...option,value:option.name}))}
async function contactSuggestions(query:string,done:(items:ContactOption[])=>void){try{if(item.value?.body.customerId&&!contactOptions.value.length)await loadContactOptions(item.value.body.customerId);const needle=query.trim().toLowerCase();done(contactOptions.value.filter(option=>!needle||`${option.name} ${option.department} ${option.title} ${option.email}`.toLowerCase().includes(needle)).slice(0,50))}catch{done([])}}
async function selectCustomer(option:CustomerOption){if(!item.value)return;item.value.body.customerId=option.id;item.value.body.customer=option.name;item.value.body.contactId='';contactOptions.value=[];await loadContactOptions(option.id);if(!item.value.body.contact){const primary=contactOptions.value.find(contact=>contact.isPrimary)||contactOptions.value[0];if(primary)selectContact(primary)}}
function customerTyped(){if(item.value)item.value.body.customerId='';contactOptions.value=[];if(item.value)item.value.body.contactId=''}
function selectContact(option:ContactOption){if(!item.value)return;item.value.body.contactId=option.id;item.value.body.contact=option.name}
function contactTyped(){if(item.value)item.value.body.contactId=''}
function contactOptionLabel(option:ContactOption){return [[option.department,option.title].filter(Boolean).join(' / '),option.email,option.isPrimary?t('inquiryWorkspace.messages.primaryContact'):''].filter(Boolean).join(' · ')}
async function openCreateDialog(){uploadForm.templateId='';uploadForm.title='';uploadForm.customerId='';uploadForm.contactId='';uploadForm.file=null;contactOptions.value=[];if(!templates.value.length)await loadTemplates();if(!customerOptions.value.length)await loadCustomerOptions();uploadOpen.value=true}
async function uploadCustomerChanged(customerId:string){uploadForm.contactId='';await loadContactOptions(customerId);const primary=contactOptions.value.find(contact=>contact.isPrimary)||contactOptions.value[0];if(primary)uploadForm.contactId=primary.id}
function pickImportFile(event:Event){const input=event.target as HTMLInputElement,file=input.files?.[0]||null;if(file&&!/\.(xlsx|csv)$/i.test(file.name)){ElMessage.error(t('inquiryWorkspace.messages.invalidImportFile'));input.value='';uploadForm.file=null;return}if(file&&file.size>8*1024*1024){ElMessage.error(t('inquiryWorkspace.messages.fileTooLarge'));input.value='';uploadForm.file=null;return}uploadForm.file=file}
function downloadSelectedTemplate(){if(uploadForm.templateId)window.open(`/api/inquiry-templates/${uploadForm.templateId}/download`,'_blank','noopener')}
function startManualInquiry(){uploadOpen.value=false;create()}
function fileTitle(name:string){return name.replace(/\.(xlsx|csv)$/i,'').trim()||t('inquiryWorkspace.messages.defaultImportTitle')}
async function importInquiry(){if(!uploadForm.customerId){ElMessage.error(t('inquiryWorkspace.messages.customerRequired'));return}if(!uploadForm.contactId){ElMessage.error(t('inquiryWorkspace.messages.contactRequired'));return}if(!uploadForm.file){ElMessage.error(t('inquiryWorkspace.messages.importFileRequired'));return}busy.value=true;try{const workbook=await parseTableFile(uploadForm.file.name,await uploadForm.file.arrayBuffer(),{maxRows:10001});const sheet=workbook.sheets[0];if(!sheet)throw new Error(t('inquiryWorkspace.messages.noReadableSheet'));const selected=selectImportTemplate(sheet.columns,activeTemplates.value,uploadForm.templateId);const customer=customerOptions.value.find(option=>option.id===uploadForm.customerId);const contact=contactOptions.value.find(option=>option.id===uploadForm.contactId);if(!customer||!contact)throw new Error(t('inquiryWorkspace.messages.customerChanged'));const body=blankBody();body.title=uploadForm.title.trim()||fileTitle(uploadForm.file.name);body.customerId=customer.id;body.customer=customer.name;body.contactId=contact.id;body.contact=contact.name;body.template=templateSnapshot(selected);body.products=productsFromImportedSheet(sheet,selected);body.attachments=[];item.value={id:'',number:'',ownerId:'',owner:t('inquiryWorkspace.messages.currentSales'),state:'UNSUBMITTED',revision:0,submittedAt:'',body,quotes:[],procurementCount:0,logisticsCount:0,canEdit:true,sourceMailId:'',legacy:false};await save(false);await storeAttachment(uploadForm.file,false);await save(false);uploadOpen.value=false;detailTab.value='products';ElMessage.success(t('inquiryWorkspace.messages.imported',{count:body.products.length}))}catch(error){ElMessage.error(error instanceof Error?error.message:t('inquiryWorkspace.messages.importFailed'))}finally{busy.value=false}}
function editQuote(q?:Quote){detailTab.value='quotes';editor.value=q?normalizeQuote(structuredClone(JSON.parse(JSON.stringify(q)))):{id:'',kind:view.value,version:0,body:blankQuote(),authorId:'',author:'',submittedAt:'',updatedBy:'',updatedAt:'',canEdit:true};productPage.value=1;productSearch.value='';productFilter.value='all';requirementsOpen.value=[];if(!q&&item.value&&editor.value){editor.value.body.loadingPort=item.value.body.loadingPort;editor.value.body.destinationPort=item.value.body.destinationPort;if(view.value==='LOGISTICS')editor.value.body.incoterm=item.value.body.incoterm||''}}
async function companySuggestions(query:string,done:(items:{value:string}[])=>void){try{const factory=view.value==='PROCUREMENT';const r=await get<{factories?:{nameZh?:string;name?:string}[];suppliers?:{name:string}[]}>(factory?'/factories':'/suppliers',{keyword:query,page:1,page_size:50});done((factory?r.factories||[]:r.suppliers||[]).map(x=>({value:('nameZh' in x?x.nameZh:x.name)||x.name||''})).filter(x=>x.value!==''))}catch{done([])}}
function freightRateFor(productId:string){return editor.value?.body.freightRates.find(rate=>rate.productId===productId)}
function toggleFreightProduct(product:Product,selected:boolean){if(!editor.value)return;if(selected){if(!freightRateFor(product.id))editor.value.body.freightRates.push({productId:product.id,price:'',currency:editor.value.body.currency||'USD',unit:product.unit||'MT',remark:''})}else editor.value.body.freightRates=editor.value.body.freightRates.filter(rate=>rate.productId!==product.id);editor.value.body.cargoIds=editor.value.body.freightRates.map(rate=>rate.productId)}
function chargeAllocationChanged(row:{allocationType?:string;productId?:string}){if(row.allocationType!=='DIRECT')row.productId=''}
function addCharge(){if(!editor.value)return;editor.value.body.charges.push({name:'',amount:'',currency:editor.value.body.currency||'USD',unit:'SHIPMENT',quantity:'1',subtotal:'',remark:'',allocationType:'FIXED',productId:''})}
async function saveQuote(submit:boolean){if(!item.value||!editor.value)return;if(view.value==='PROCUREMENT'){if(submit&&!procurementQuoteCategory(editor.value.body.quoteCategory)){ElMessage.warning(t('inquiryWorkspace.quotes.selectQuoteCategory'));return}quoteCategoryChanged(editor.value.body.quoteCategory)}if(view.value==='LOGISTICS'){editor.value.body.currency='USD';editor.value.body.freightRates.forEach(rate=>{rate.currency=rate.currency.trim().toUpperCase()});editor.value.body.cargoIds=editor.value.body.freightRates.map(rate=>rate.productId);editor.value.body.charges.forEach(charge=>{charge.currency=charge.currency.trim().toUpperCase();charge.quantity=charge.quantity||'1';charge.unit=charge.unit||'SHIPMENT';if(charge.allocationType!=='DIRECT')charge.productId=''});editor.value.body.exchangeRates=Object.fromEntries(Object.entries(editor.value.body.exchangeRates).map(([currency,rate])=>[currency.trim().toUpperCase(),rate.trim()]))}busy.value=true;try{const r=await command('quote',{id:item.value.id,revision:item.value.revision,quoteId:editor.value.id,quoteVersion:editor.value.version,submit,quote:editor.value.body});item.value=normalizeInquiry(r.item);editor.value=null;ElMessage.success(t(submit?'inquiryWorkspace.messages.quoteSubmitted':'inquiryWorkspace.messages.quoteSaved'))}finally{busy.value=false}}
async function download(key:string){if(!item.value)return;const r=await post<{url:string}>('/inquiry-workspace',{action:'download',view:view.value,id:item.value.id,fileKey:key});window.open(r.url,'_blank','noopener')}
async function storeAttachment(file:File,quote:boolean){if(!item.value)return;if(file.size>8*1024*1024)throw new Error(t('inquiryWorkspace.messages.fileTooLarge'));if(!item.value.id)await save(false);if(!item.value)return;const data=await new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result).split(',')[1]);reader.onerror=reject;reader.readAsDataURL(file)});const r=await command('upload',{id:item.value.id,revision:item.value.revision,fileName:file.name,fileData:data});if(!r.attachment)throw new Error(t('inquiryWorkspace.messages.attachmentMissing'));if(quote&&editor.value){editor.value.body.attachments??=[];editor.value.body.attachments.push(r.attachment)}else{item.value.body.attachments??=[];item.value.body.attachments.push(r.attachment)}}
async function upload(e:Event,quote:boolean){const input=e.target as HTMLInputElement,file=input.files?.[0];if(!file||!item.value)return;try{await storeAttachment(file,quote);ElMessage.success(t('inquiryWorkspace.messages.attachmentUploaded'))}catch(error){ElMessage.error(error instanceof Error?error.message:t('inquiryWorkspace.messages.attachmentFailed'))}finally{input.value=''}}
async function initial(){item.value=null;editor.value=null;detailTab.value=initialDetailTab();state.value='';page.value=1;refreshSuspended.value=false;refreshErrorShown.value=false;const routeID=String(route.params.id||route.query.id||''),id=canonicalInquiryRouteID(routeID);if(id){if(id!==routeID&&route.query.id!==undefined)await router.replace({query:{...route.query,id}});const r=await command('get',{id});item.value=normalizeInquiry(r.item)}else await loadList()}
const stopLive=onLive(e=>{if(e.type==='requirement.changed'&&!refreshSuspended.value&&!editor.value&&!editable.value&&!busy.value)void reload()})
let timer:ReturnType<typeof setInterval>|undefined
onMounted(()=>{void (async()=>{try{await loadTemplates();await initial()}catch{refreshSuspended.value=true;showRefreshError()}})();timer=setInterval(()=>{if(!refreshSuspended.value&&!editor.value&&!editable.value&&!busy.value)void reload()},5000)})
onUnmounted(()=>{stopLive();if(timer)clearInterval(timer)})
watch(view,()=>void initial())
</script>
<style scoped>
.inquiry-workspace{padding:20px;min-width:0}.toolbar{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-bottom:18px}.toolbar h2,.toolbar h3{margin:0 auto 0 0}.toolbar .el-input{width:280px}.toolbar .el-select{width:150px}.inquiry-fields{display:grid;grid-template-columns:repeat(3,minmax(180px,1fr));gap:0 20px}.wide{grid-column:1/-1}.quote-card{margin:12px 0}.quote-editor{margin-top:20px}.quote-editor-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:18px;padding:18px 20px;border:1px solid #d9e8e5;border-radius:12px;background:#f4faf8}.quote-editor-heading h3{margin:0;color:#173e53}.quote-editor-heading p{margin:6px 0 0;color:#637889}.field-help{display:inline-grid;place-items:center;width:17px;height:17px;margin-left:3px;border-radius:50%;background:#e5f2f5;color:#176b87;font-size:11px}.el-pagination{margin:20px 0}.el-table{margin-bottom:14px}.el-date-editor.el-input{max-width:100%}.el-checkbox-group{display:flex;flex-wrap:wrap;gap:8px}.quote-card p{color:var(--el-text-color-secondary)}@media(max-width:900px){.inquiry-fields{grid-template-columns:repeat(2,minmax(150px,1fr))}}

.inquiry-workspace { --ink:#17324d; --muted:#6b7c8f; --line:#dce6ef; --brand:#176b87; --surface:#fff; padding: 12px; color: var(--ink); }
.workspace-heading { display: flex; align-items: center; justify-content: space-between; gap: 24px; margin: 2px 0 24px; padding: 24px 26px; background: linear-gradient(120deg,#f3f9fb 0%,#eef6fb 52%,#f7fafc 100%); border: 1px solid #d8e8ef; border-radius: 14px; }
.workspace-heading h2 { margin: 0; font-size: 28px; letter-spacing: -.5px; color: #12334d; }
.workspace-heading p { margin: 9px 0 0; color: var(--muted); font-size: 14px; line-height: 1.6; }
.module-chip { margin-bottom: 10px; color: var(--brand); border-color: #9fc9d6; background: #f6fcfd; }
.heading-actions { display: flex; gap: 10px; flex-wrap: wrap; justify-content: flex-end; }
.list-toolbar { padding: 16px 18px; background: var(--surface); border: 1px solid var(--line); border-radius: 12px; box-shadow: 0 5px 18px rgba(31,65,91,.04); }
.list-toolbar > .el-input { width: min(390px, 100%); margin-right: auto; }
.inquiry-workspace--operations { --ink:#141817; --muted:#66727d; --line:#dfeaf0; --brand:#159fdc; }
.inquiry-workspace--operations .inquiry-list-panel { overflow:hidden; border-color:#dfeaf0; border-radius:14px; background:#fff; box-shadow:0 10px 28px rgb(20 24 23 / 5%); }
.inquiry-workspace--operations .list-toolbar { margin-bottom:14px; padding:0; border:0; border-radius:0; background:transparent; box-shadow:none; }
.inquiry-workspace--operations :deep(.inquiry-list-table) { margin-bottom:14px; border:0; border-radius:0; --el-table-header-bg-color:#eef9fe; --el-table-header-text-color:#24323a; --el-table-row-hover-bg-color:#f0fbf6; }
.inquiry-workspace--operations :deep(.inquiry-list-table th.el-table__cell) { height:48px; border-bottom-color:#d9edf5; font-weight:650; }
.inquiry-workspace--operations :deep(.inquiry-list-table td.el-table__cell) { padding:13px 0; border-bottom-color:#e7eff3; color:#141817; }
.inquiry-workspace--operations :deep(.inquiry-list-table .el-table__row--striped td.el-table__cell) { background:#fbfdfe; }
.inquiry-number { color:#159fdc; font-weight:600; }
.inquiry-workspace--operations :deep(.inquiry-list-table .el-button--primary.is-plain) { color:#159fdc; border-color:#9bdcff; background:#f4fbff; }
.inquiry-workspace--operations :deep(.inquiry-list-table .el-button--primary.is-plain:hover) { color:#141817; border-color:#4ac1ff; background:#4ac1ff; }
.inquiry-workspace--operations .list-pagination { justify-content:flex-end; margin:0; }
.inquiry-workspace :deep(.el-table) { border: 1px solid var(--line); border-radius: 12px; --el-table-header-bg-color: #f4f8fa; --el-table-header-text-color: #486174; --el-table-row-hover-bg-color: #f0f8fa; }
.inquiry-workspace :deep(.el-table th.el-table__cell) { height: 48px; font-weight: 600; }
.inquiry-workspace :deep(.el-table td.el-table__cell) { padding: 13px 0; color: #334155; }
.inquiry-workspace :deep(.el-button) { border-radius: 8px; }
.inquiry-workspace :deep(.el-button--primary:not(.is-plain):not(.is-link)) { --el-button-bg-color:#4ac1ff; --el-button-border-color:#4ac1ff; --el-button-text-color:#103a4e; --el-button-hover-bg-color:#34b5f6; --el-button-hover-border-color:#34b5f6; --el-button-hover-text-color:#102f3d; font-weight:600; }
.heading-actions :deep(.mail-import-action) { border-color:#9bdcff; background:#eefaff; color:#1479a6; font-weight:600; }
.heading-actions :deep(.mail-import-action:hover) { border-color:#4ac1ff; background:#ddf4ff; color:#0f6f9b; }
.inquiry-workspace :deep(.el-tag) { border-radius: 6px; }
.detail-hero { display:flex; align-items:center; justify-content:space-between; gap:24px; margin-bottom:16px; padding:20px 22px; color:#fff; background:linear-gradient(120deg,#143f5a,#176b87); border-radius:14px; box-shadow:0 10px 24px rgba(16,77,103,.14); }
.detail-kicker { display:block; margin-bottom:5px; color:#c8e7ef; font-size:12px; }
.detail-hero h3 { margin:0; font-size:21px; }
.detail-title { margin-top:5px; color:#eefbff; font-size:14px; }
.detail-meta { display:flex; align-items:center; gap:12px; margin-top:10px; color:#dceef3; font-size:13px; }
.detail-actions { display:flex; gap:10px; flex-wrap:wrap; justify-content:flex-end; }
.detail-actions :deep(.el-button:not(.el-button--primary)) { color:#17455f; border-color:#fff; background:#fff; }
.more-actions { min-width:126px; font-weight:700; box-shadow:0 4px 14px rgba(4,36,54,.18); }
.more-actions.is-open { color:#116f98; border-color:#a8ddf4; background:#f1faff; }
.more-arrow { margin-left:10px; color:#176b87; font-size:10px; line-height:1; transition:transform .18s ease; }
.more-actions.is-open .more-arrow { transform:rotate(180deg); }
:global(.inquiry-action-menu.el-popper) { box-sizing:border-box; overflow:hidden; width:160px; min-width:160px; padding:6px; border:1px solid #d9e8ef; border-radius:10px; box-shadow:0 12px 30px rgba(20,63,90,.16); }
:global(.inquiry-action-menu .el-dropdown-menu) { padding:0; }
:global(.inquiry-action-menu .el-dropdown-menu__item) { height:40px; gap:10px; margin:2px 0; padding:0 12px; color:#334b5c; border-radius:7px; font-size:14px; }
:global(.inquiry-action-menu .el-dropdown-menu__item .el-icon) { width:18px; margin:0; color:#718696; font-size:16px; }
:global(.inquiry-action-menu .el-dropdown-menu__item:not(.is-disabled):focus),
:global(.inquiry-action-menu .el-dropdown-menu__item:not(.is-disabled):hover) { color:#0e79a8; background:#edf8fd; }
:global(.inquiry-action-menu .el-dropdown-menu__item:not(.is-disabled):hover .el-icon) { color:#159fdc; }
:global(.inquiry-action-menu .el-dropdown-menu__item.el-dropdown-menu__item--divided) { margin-top:7px; }
:global(.inquiry-action-menu .el-dropdown-menu__item.el-dropdown-menu__item--divided::before) { left:-6px; right:-6px; height:1px; background:#e7eef2; }
:global(.inquiry-action-menu .menu-action-save) { color:#217652; }
:global(.inquiry-action-menu .menu-action-save .el-icon) { color:#2d966c; }
:global(.inquiry-action-menu .menu-action-save:not(.is-disabled):hover) { color:#176344; background:#edf8f2; }
:global(.inquiry-action-menu .menu-action-withdraw) { color:#bd542f; }
:global(.inquiry-action-menu .menu-action-withdraw .el-icon) { color:#d66a42; }
:global(.inquiry-action-menu .menu-action-withdraw:not(.is-disabled):hover) { color:#a84225; background:#fff2ed; }
.business-card { margin-bottom:16px; padding:20px 22px; background:var(--surface); border:1px solid var(--line); border-radius:14px; box-shadow:0 5px 18px rgba(31,65,91,.04); }
.section-title { display:flex; align-items:center; justify-content:space-between; gap:16px; margin-bottom:17px; }
.section-title h3 { margin:0; font-size:16px; color:var(--ink); }
.section-title p { margin:5px 0 0; color:var(--muted); font-size:13px; }
.template-actions { display:flex; align-items:center; gap:10px; flex-wrap:wrap; }
.template-actions .el-select { width:250px; }
.template-label { color:#4b6073; font-size:13px; font-weight:600; }
.inquiry-fields { padding: 0; border: 0; border-radius: 0; background: transparent; margin-bottom: 0; }
.inquiry-fields :deep(.el-form-item) { margin-bottom:17px; }
.inquiry-fields :deep(.el-form-item__label) { padding-bottom:7px; color:#4b6073; font-weight:600; }
.inquiry-fields :deep(.el-input__wrapper),.inquiry-fields :deep(.el-textarea__inner) { box-shadow:0 0 0 1px #cedbe5 inset; }
.inquiry-fields :deep(.el-autocomplete) { width:100%; }
.overview-grid { display:grid; grid-template-columns:minmax(0,1fr); gap:16px; align-items:start; }
.customer-card { min-width:0; }
.attachment-card { min-width:0; padding:18px; }
.attachment-heading { display:flex; align-items:flex-start; justify-content:space-between; gap:12px; margin-bottom:12px; }
.attachment-heading h3 { margin:0; font-size:16px; }
.attachment-heading p { margin:5px 0 0; color:var(--muted); font-size:12px; }
.attachment-empty { padding:13px 14px; color:#91a0ad; background:#f7fafb; border:1px dashed #d3e0e7; border-radius:8px; font-size:12px; text-align:center; }
.attachment-list { display:flex; gap:8px; flex-wrap:wrap; }
.attachment-list :deep(.el-button) { max-width:100%; overflow:hidden; text-overflow:ellipsis; }
.file-input { max-width:270px; color:var(--muted); font-size:13px; }
.file-input::file-selector-button { margin-right:10px; padding:8px 12px; color:var(--brand); background:#f1f9fb; border:1px solid #b8d7df; border-radius:7px; cursor:pointer; }
.product-card { overflow:hidden; }
.quotes-section { padding:22px; }
.original-category-panel { margin:0 0 22px; overflow:hidden; border:1px solid #d8e5eb; border-radius:12px; background:#fff; }
.original-category-panel--unclassified { border-color:#d8dee2; }
.original-category-heading { display:flex; align-items:center; justify-content:space-between; gap:20px; padding:16px 20px; border-bottom:1px solid #dce8ed; background:linear-gradient(135deg,#f8fbfc,#f1f8fa); }
.original-category-heading span { font-size:11px; font-weight:700; letter-spacing:.08em; color:#18809a; }
.original-category-heading h3 { margin:3px 0 4px; font-size:19px; color:#173f53; }
.original-category-heading p { margin:0; color:#637889; }
.original-category-heading>div:last-child { min-width:100px; text-align:right; }
.original-category-heading>div:last-child strong { display:block; font-size:24px; color:#126f87; }
.original-category-heading>div:last-child small { margin-top:2px; color:#718691; }
.original-category-panel :deep(th.el-table__cell) { background:#f7fafb; color:#365a6c; }
.original-category-panel :deep(td.el-table__cell) { padding:12px 0; }
.original-category-panel td small { display:block; margin-top:4px; color:#718691; }
.submitted-quote-name,.submitted-logistics-title { display:flex; align-items:center; justify-content:space-between; gap:10px; }
.submitted-logistics-title { margin:5px 0 12px; padding-top:14px; border-top:1px solid #e1eaee; color:#214a5e; }
.detail-tabs { min-width:0; }
.detail-tabs :deep(.el-tabs__header) { margin:0 0 16px; padding:0 18px; background:#fff; border:1px solid var(--line); border-radius:12px; box-shadow:0 5px 18px rgba(31,65,91,.04); }
.detail-tabs :deep(.el-tabs__nav-wrap::after) { display:none; }
.detail-tabs :deep(.el-tabs__item) { height:50px; color:#5a6e7f; font-weight:600; }
.detail-tabs :deep(.el-tabs__item.is-active) { color:var(--brand); }
.compact-quote { display:block; overflow:hidden; color:#173f59; text-overflow:ellipsis; white-space:nowrap; }
.compact-quote+small,.quotes-section td small { display:block; margin-top:3px; overflow:hidden; color:#81909c; font-size:12px; text-overflow:ellipsis; white-space:nowrap; }
.upload-form { margin-top:20px; }
.upload-form :deep(.el-form-item__content) { min-width:0; }
.upload-form small { display:block; width:100%; margin-top:6px; color:#7a8995; line-height:1.5; }
.upload-template { display:grid; grid-template-columns:minmax(0,1fr) auto; width:100%; gap:10px; }
.upload-template .el-select { flex:1; min-width:0; }
.upload-form input[type=file] { width:100%; padding:8px; color:#657786; background:#f7fafb; border:1px solid #d7e2e9; border-radius:8px; }
.upload-form input[type=file]::file-selector-button { margin-right:12px; padding:8px 13px; color:var(--brand); background:#fff; border:1px solid #b9d3dd; border-radius:7px; cursor:pointer; }
.option-title { color:#23455c; font-weight:600; }.option-meta { color:#8493a0; font-size:12px; }
.quote-editor { padding:22px; background:#fff; border:1px solid var(--line); border-radius:14px; }
@media (max-width:1100px) { .overview-grid { grid-template-columns:1fr; } }
@media (max-width: 760px) {
  .inquiry-workspace { padding: 0; }
  .workspace-heading { align-items: flex-start; flex-direction: column; gap: 16px; padding:20px; }
  .workspace-heading h2 { font-size: 23px; }
  .heading-actions { width:100%; justify-content:flex-start; }
  .list-toolbar { padding: 12px; gap: 10px; }
  .list-toolbar > .el-input { width: 100%; }
  .detail-hero { align-items:flex-start; flex-direction:column; }
  .detail-actions { justify-content:flex-start; }
  .business-card { padding:16px; }
  .attachment-heading { align-items:flex-start; flex-direction:column; }
  .section-title { align-items:flex-start; flex-direction:column; }
  .inquiry-fields { grid-template-columns: 1fr; }
  .detail-tabs :deep(.el-tabs__header) { padding:0 8px; overflow-x:auto; }
  .upload-template { align-items:stretch; flex-direction:column; }
  .el-pagination { overflow-x: auto; padding-bottom: 8px; }
}

.logistics-section-title{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin:22px 0 10px}.logistics-section-title h4{margin:0;color:#173e53}.logistics-section-title p{margin:5px 0 0;color:#637889;font-size:13px}.freight-rate-table{border:1px solid #dfe9ed;border-radius:10px;overflow:hidden}.freight-rate-table :deep(th.el-table__cell){background:#f5f9fa}.freight-rate-table :deep(.el-input__wrapper){padding:1px 8px}@media(max-width:900px){.logistics-section-title{align-items:stretch}.freight-rate-table :deep(.cell){padding:0 6px}.freight-rate-table :deep(th.el-table__cell){font-size:12px}}
.exchange-rate-panel{display:flex;align-items:center;gap:14px;flex-wrap:wrap;margin:18px 0 6px;padding:13px 15px;border:1px solid #cfe3ea;border-radius:10px;background:#f4fafc}.exchange-rate-copy{display:grid;gap:3px;margin-right:auto}.exchange-rate-copy span{font-weight:650;color:#17485d}.exchange-rate-copy strong{font-size:12px;font-weight:400;color:#637889}.exchange-rate-field{display:flex;align-items:center;gap:8px;color:#4c6875;font-size:13px;white-space:nowrap}.exchange-rate-field .el-input{width:170px}.usd-preview{display:block;margin-top:5px;color:#19718b;font-size:11px;line-height:1.2}.charge-summary{display:flex;align-items:center;justify-content:flex-end;gap:16px;flex-wrap:wrap;margin:8px 0 20px;padding:10px 14px;border-radius:8px;background:#f7fafb;color:#607987;font-size:12px}.charge-summary strong{color:#17485d;font-size:13px}@media(max-width:700px){.exchange-rate-panel{align-items:stretch}.exchange-rate-copy{width:100%}.exchange-rate-field{justify-content:space-between}.exchange-rate-field .el-input{width:min(210px,65vw)}}
</style>
