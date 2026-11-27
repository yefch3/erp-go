<template>
  <div class="home-page">
    <section class="home-head">
      <div class="head-copy">
        <h1>{{ t('todos.title') }}</h1>
        <div class="head-meta"><span>{{ auth.employeeName }}</span>
          <span>{{ auth.employeeDepartment || t('todos.departmentUnset') }}</span>
          <span class="meta-separator" aria-hidden="true" />
          <span>{{ today }}</span>
        </div>
      </div>
      <div class="head-actions">
        <span v-if="updatedAt" class="updated">{{ t('todos.updatedAt', { time: updatedAt }) }}</span>
        <el-button :loading="loading || countLoading || reminderCountLoading" @click="refreshAll">{{ t('common.refresh') }}</el-button>
      </div>
    </section>

    <section class="summary-grid" aria-label="home summary">
      <button class="summary-card" :class="{ active: activeTab === 'pending' }" type="button" @click="activate('pending')">
        <span>{{ t('todos.summaryPending') }}</span>
        <strong>{{ combinedPendingAvailable ? combinedPendingTotal : '—' }}</strong>
      </button>
      <button class="summary-card" :class="{ active: activeTab === 'reminders' && reminderTiming === 'UPCOMING' }" type="button" :disabled="!reminderSummaryAvailable" @click="activateReminderFilter('UPCOMING')">
        <span>{{ t('todos.summaryUpcoming') }}</span><strong>{{ reminderSummaryAvailable ? reminderSummary.upcoming : '—' }}</strong>
      </button>
      <button class="summary-card" :class="{ active: activeTab === 'reminders' && reminderTiming === 'OVERDUE', danger: reminderSummaryAvailable && reminderSummary.overdue > 0 }" type="button" :disabled="!reminderSummaryAvailable" @click="activateReminderFilter('OVERDUE')">
        <span>{{ t('todos.summaryOverdue') }}</span><strong>{{ reminderSummaryAvailable ? reminderSummary.overdue : '—' }}</strong>
      </button>
      <button class="summary-card" :class="{ active: activeTab === 'reminders' && reminderRead === 'UNREAD' }" type="button" :disabled="!reminderSummaryAvailable" @click="activateUnreadReminders">
        <span>{{ t('todos.summaryUnread') }}</span><strong>{{ reminderSummaryAvailable ? reminderSummary.unread : '—' }}</strong>
      </button>
    </section>

    <el-card class="work-card" shadow="never">
      <el-tabs v-model="activeTab" class="home-tabs" @tab-change="onTabChange">
        <el-tab-pane :label="t('todos.homeTabPending')" name="pending" />
        <el-tab-pane :label="t('todos.homeTabSubmitted')" name="submitted" />
        <el-tab-pane :label="t('todos.homeTabResponsible')" name="responsible" />
        <el-tab-pane :label="t('todos.homeTabReminders')" name="reminders" />
        <el-tab-pane :label="t('todos.homeTabHandled')" name="handled" />
      </el-tabs>

      <div v-if="hasApprovalSource" class="filters">
        <el-input v-model="keyword" clearable :placeholder="t('todos.searchPlaceholder')" @keyup.enter="query" @clear="query" />
        <el-select v-model="bizType" :placeholder="t('todos.allModules')" clearable @change="query">
          <el-option v-for="item in bizTypes" :key="item" :label="bizTypeLabel(item)" :value="item" />
        </el-select>
        <el-select v-if="activeTab !== 'pending'" v-model="statusFilter" :placeholder="t('todos.allStatuses')" clearable @change="query">
          <el-option v-for="item in statusOptions" :key="item" :label="activeTab === 'handled' ? taskStatusLabel(item) : instanceStatusLabel(item)" :value="item" />
        </el-select>
        <el-button type="primary" @click="query">{{ t('common.query') }}</el-button>
      </div>

      <div v-else-if="hasReminderSource" class="filters reminder-filters">
        <el-input v-model="keyword" clearable :placeholder="t('todos.reminderSearchPlaceholder')" @keyup.enter="query" @clear="query" />
        <el-select v-model="reminderSource" :placeholder="t('todos.allReminderSources')" clearable @change="query">
          <el-option v-for="item in reminderSourceOptions" :key="item" :label="reminderSourceLabel(item)" :value="item" />
        </el-select>
        <el-select v-model="reminderTiming" :placeholder="t('todos.allReminderTimings')" clearable @change="query">
          <el-option value="UPCOMING" :label="t('todos.timing.UPCOMING')" />
          <el-option value="OVERDUE" :label="t('todos.timing.OVERDUE')" />
          <el-option value="REMINDER" :label="t('todos.timing.REMINDER')" />
        </el-select>
        <el-select v-model="reminderRead" :placeholder="t('todos.allReadStates')" clearable @change="query">
          <el-option value="UNREAD" :label="t('todos.unread')" />
          <el-option value="READ" :label="t('todos.read')" />
        </el-select>
        <el-button type="primary" @click="query">{{ t('common.query') }}</el-button>
        <el-button v-if="reminderSummary.unread > 0" :loading="markingRead" @click="markAllRemindersRead">{{ t('todos.markAllRead') }}</el-button>
      </div>

      <el-alert v-if="sourceError" class="source-error" type="warning" :closable="false" show-icon :title="t('todos.sourceUnavailable')">
        <template #default><el-button link type="primary" @click="load">{{ t('todos.retry') }}</el-button></template>
      </el-alert>

      <el-alert v-if="hasReminderSource && reminderSourceError" class="source-error" type="warning" :closable="false" show-icon :title="t('todos.reminderSourceUnavailable')">
        <template #default><el-button link type="primary" @click="load">{{ t('todos.retry') }}</el-button></template>
      </el-alert>

      <template v-if="activeTab === 'pending'">
        <div v-loading="loading" class="pending-content">
          <div v-if="todos.length" class="todo-source-head approval-source-head"><div><h3>{{ t('todos.approvalTasks') }}</h3></div><span>{{ total }} {{ t('todos.items') }}</span></div>
        <el-table v-if="todos.length" :data="todos" class="home-table">
          <el-table-column :label="t('todos.priority')" width="145">
            <template #default="{ row }">
              <el-tag size="small" :type="priorityTagType(row.priority)" effect="light">{{ priorityLabel(row.priority) }}</el-tag>
              <div class="priority-reason">{{ priorityReason(row.remainingMinutes) }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('todos.workItem')" min-width="310">
            <template #default="{ row }">
              <div class="item-title">{{ bizTypeLabel(row.instance.bizType) }} · {{ row.instance.bizNo }}</div>
              <div class="item-meta">{{ row.task.nodeName }} · {{ row.instance.submitterName }}</div>
              <div class="item-summary">{{ summaryText(row.instance.bizSummary) }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('todos.submittedAt')" width="170"><template #default="{ row }">{{ formatTime(row.instance.submittedAt) }}</template></el-table-column>
          <el-table-column :label="t('common.status')" width="120"><template #default><el-tag size="small" type="warning" effect="plain">{{ t('todos.waitingForMe') }}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="150" fixed="right">
            <template #default="{ row }">
              <router-link v-if="approvalSourceLink(row.instance)" :to="approvalSourceLink(row.instance)!" class="doc-link">{{ t(row.instance.bizType==='SHIPPING_REQUOTE'?'shipping.todoViewOrder':'todos.goToSource') }}</router-link>
              <span v-else class="no-link">{{ t('todos.noSourceLink') }}</span>
            </template>
          </el-table-column>
        </el-table>
          <el-alert v-if="todoReceptionError" :title="t('todoReception.loadFailed')" type="warning" :closable="false" />
          <template v-if="showOperationalTodos && todoReception">
          <InquiryTodos :reception="todoReception" @count="inquiryPendingCount=$event" @state="inquiryState=$event" />
          <section v-if="visibleNegotiationTasks.length" class="todo-source-section">
            <div class="todo-source-head"><h3>实单议价与草稿退回</h3></div>
            <el-table :data="visibleNegotiationTasks" class="home-table" size="small">
              <el-table-column label="采购批次" prop="contractNo" min-width="130" />
              <el-table-column label="产品 / 工厂" min-width="200"><template #default="{row}"><strong>{{ row.productName }}</strong><div class="item-meta">{{ row.supplierName }}</div></template></el-table-column>
              <el-table-column label="议价备注" prop="instructions" min-width="170" show-overflow-tooltip />
              <el-table-column label="操作" width="128"><template #default="{row}">
                <el-button link type="primary" @click="openNegotiationTodo(row)">{{ row.orderStatus === 'REJECTED' ? '修改原草稿' : '处理议价' }}</el-button>
              </template></el-table-column>
            </el-table>
          </section>
          <el-alert v-if="executionTasksError" title="合同执行待办暂时无法加载，请刷新重试" type="warning" :closable="false" show-icon />
          <section v-if="showPurchaseFinanceSection && visiblePurchaseFinanceTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>采购合同财务核验</h3></div><el-tag v-if="!executionTasksError" type="warning" effect="plain">{{visiblePurchaseFinanceTasks.length}} 项</el-tag></div>
            <el-table :data="visiblePurchaseFinanceTasks" class="home-table purchase-finance-todos">
              <el-table-column label="采购单 / 供应商" width="260"><template #default="{row}"><div class="item-title">{{row.poNo}}</div><div class="item-meta">{{row.supplierName}}</div></template></el-table-column>
              <el-table-column label="采购金额" width="140" align="right" class-name="purchase-amount"><template #default="{row}">{{row.currency}} {{Number(row.totalAmount).toLocaleString('en-US',{minimumFractionDigits:2,maximumFractionDigits:2})}}</template></el-table-column>
              <el-table-column label="签署合同文件" min-width="260"><template #default="{row}"><router-link :to="{path:'/supplier-recon',query:{view:'contracts',order:row.id}}" class="purchase-attachment-link" title="进入核验页面查看合同附件"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true"><path d="M14 3H6a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8z"/><path d="M14 3v5h5M8 12h8M8 16h6"/></svg><span>{{row.signedContractName}}</span></router-link></template></el-table-column>
              <el-table-column label="状态" width="120" align="center"><template #default><el-tag type="warning">待财务核验</el-tag></template></el-table-column>
              <el-table-column label="操作" width="125" fixed="right" align="center"><template #default="{row}"><router-link :to="{path:'/supplier-recon',query:{view:'contracts',order:row.id}}" class="purchase-review-link">进入核验</router-link></template></el-table-column>
            </el-table>
          </section>
          <section v-if="showLogisticsFinanceSection && visibleLogisticsFinanceTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>物流合同财务核验</h3></div><el-tag v-if="!executionTasksError" type="warning" effect="plain">{{visibleLogisticsFinanceTasks.length}} 项</el-tag></div>
            <el-table :data="visibleLogisticsFinanceTasks" class="home-table">
              <el-table-column label="物流合同 / 外销合同" min-width="230"><template #default="{row}"><div class="item-title">{{row.businessDocumentNo||'物流合同号未填写'}} <el-tag v-if="row.businessDocumentNoGenerated" size="small" type="info">系统生成</el-tag></div><div class="item-meta">{{row.exportContractNo}}<template v-if="row.transportBatchNo"> · 第 {{row.transportBatchNo}} 批</template></div></template></el-table-column>
              <el-table-column prop="supplierName" label="货代" min-width="160" show-overflow-tooltip/>
              <el-table-column label="金额" width="140" align="right"><template #default="{row}">{{row.currency}} {{Number(row.totalAmount).toLocaleString('en-US',{minimumFractionDigits:2,maximumFractionDigits:2})}}</template></el-table-column>
              <el-table-column prop="signedContractName" label="签署合同文件" min-width="210" show-overflow-tooltip/>
              <el-table-column label="状态" width="120"><template #default><el-tag type="warning">待财务核验</el-tag></template></el-table-column>
              <el-table-column label="操作" width="125" fixed="right"><template #default="{row}"><router-link :to="{path:'/supplier-recon',query:{view:'contracts',order:row.id}}" class="purchase-review-link">查看并核验</router-link></template></el-table-column>
            </el-table>
          </section>
          <section v-if="visibleFinanceTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>合同执行条件确认</h3></div><el-tag type="warning" effect="plain">{{visibleFinanceTasks.length}} 项</el-tag></div>
            <el-table :data="visibleFinanceTasks" class="home-table">
              <el-table-column label="合同 / 客户" min-width="230"><template #default="{row}"><div class="item-title">{{row.contractNo}}</div><div class="item-meta">{{row.customerName}}</div></template></el-table-column>
              <el-table-column label="合同金额" width="160" align="right"><template #default="{row}">{{row.currency}} {{Number(row.totalAmount).toLocaleString('en-US',{minimumFractionDigits:2,maximumFractionDigits:2})}}</template></el-table-column>
              <el-table-column label="状态" width="130"><template #default><el-tag type="warning">待财务放行</el-tag></template></el-table-column>
              <el-table-column label="操作" width="160"><template #default="{row}"><router-link :to="{path:'/customer-recon',query:{keyword:row.contractNo,condition:row.id}}" class="doc-link">确认执行条件</router-link></template></el-table-column>
            </el-table>
          </section>
          <section v-if="visibleDeliveryTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>待确认工厂交货</h3></div><el-tag type="warning">{{deliveryTasks.length}} 项</el-tag></div>
            <el-table :data="visibleDeliveryTasks" class="home-table"><el-table-column prop="poNo" label="采购单号" min-width="180"/><el-table-column prop="supplierName" label="供应商" min-width="180"/><el-table-column prop="expectedDate" label="采购要求日期" width="150"/><el-table-column prop="progressText" label="交货进度" min-width="200"/><el-table-column label="状态" width="150"><template #default><el-tag type="success">整单质检合格</el-tag></template></el-table-column><el-table-column label="操作" width="150"><template #default="{row}"><router-link :to="{path:'/purchase-orders',query:{order:row.id,fulfillment:'1'}}" class="doc-link">确认工厂交货</router-link></template></el-table-column></el-table>
          </section>
          <section v-if="visibleQualityTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>{{ t('quality.todoTitle') }}</h3></div><el-tag type="warning" effect="plain">{{ qualityTasks.length }} {{ t('todos.items') }}</el-tag></div>
            <el-table :data="visibleQualityTasks" class="home-table sourcing-todo-table">
              <el-table-column :label="t('todos.priority')" width="145"><template #default><el-tag size="small" type="warning" effect="light">{{ t('todos.priorityLevels.HIGH') }}</el-tag><div class="priority-reason">{{ t('quality.pending') }}</div></template></el-table-column>
              <el-table-column :label="t('todos.workItem')" min-width="360"><template #default="{ row }"><div class="item-title">{{ row.taskNo }} · {{ row.poNo }}</div><div class="item-meta">{{ row.supplierName }} · 整单质检</div><div class="item-summary">{{ t('quality.expectedDate') }}：{{ row.expectedDate || '—' }}</div></template></el-table-column>
              <el-table-column :label="t('common.status')" width="120"><template #default><el-tag size="small" type="warning" effect="plain">{{ t('quality.pending') }}</el-tag></template></el-table-column>
              <el-table-column :label="t('common.actions')" width="150" fixed="right"><template #default="{ row }"><router-link :to="{ path:'/quality/tasks', query:{ task:String(row.id) } }" class="doc-link">{{ t('quality.todoAction') }}</router-link></template></el-table-column>
            </el-table>
          </section>
          <section v-if="visibleQualityProcurementTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>质检异常处理</h3></div><el-tag type="warning" effect="plain">{{qualityProcurementTasks.length}} 项</el-tag></div>
            <el-table :data="visibleQualityProcurementTasks" class="home-table sourcing-todo-table">
              <el-table-column :label="t('todos.priority')" width="145"><template #default><el-tag size="small" type="warning">高</el-tag><div class="priority-reason">禁止先发</div></template></el-table-column>
              <el-table-column :label="t('todos.workItem')" min-width="360"><template #default="{row}"><div class="item-title">{{row.taskNo}} · {{row.poNo}}</div><div class="item-meta">{{row.supplierName}}</div><div class="item-summary">{{qualityTodoSummary(row)}}</div></template></el-table-column>
              <el-table-column :label="t('common.actions')" width="150"><template #default="{row}"><router-link :to="{path:'/purchase-orders',query:{status:'ORDERED',order:String(row.poId)}}" class="doc-link">进入采购单</router-link></template></el-table-column>
            </el-table>
          </section>
          <section v-if="visibleExecutionProcurementTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>实单采购询价</h3></div><el-tag type="warning" effect="plain">{{ executionProcurementTasks.length }} 项</el-tag></div>
            <el-table :data="visibleExecutionProcurementTasks" class="home-table sourcing-todo-table">
              <el-table-column label="优先级" width="145"><template #default><el-tag size="small" type="warning" effect="light">高</el-tag><div class="priority-reason">需要重新询价</div></template></el-table-column>
              <el-table-column label="工作事项" min-width="360"><template #default="{row}"><div class="item-title">实单采购待重新询价 · {{ row.contractNo }}</div><div class="item-meta">{{ row.customerName || '—' }} · {{ row.productNames }}</div><div class="item-summary">共 {{ row.productCount }} 项产品，要求到货 {{ row.requiredDate || '待确认' }}</div></template></el-table-column>
              <el-table-column label="状态" width="120"><template #default><el-tag size="small" type="warning" effect="plain">待部门处理</el-tag></template></el-table-column>
              <el-table-column label="操作" width="150" fixed="right"><template #default><router-link to="/requirements" class="doc-link">进入实单询价</router-link></template></el-table-column>
            </el-table>
          </section>
          <section v-if="visibleExecutionShippingTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>实单物流询价</h3></div><el-tag type="warning" effect="plain">{{ executionShippingTasks.length }} 项</el-tag></div>
            <el-table :data="visibleExecutionShippingTasks" class="home-table sourcing-todo-table">
              <el-table-column label="优先级" width="145"><template #default="{row}"><el-tag size="small" type="warning" effect="light">高</el-tag><div class="priority-reason">{{ t(shippingTodoEntry(row)?.actionKey || 'shipping.todoViewOrder') }}</div></template></el-table-column>
              <el-table-column label="工作事项" min-width="360"><template #default="{row}"><div class="item-title">实单物流询价 · {{ row.contractNo }}</div><div class="item-meta">{{ row.customerName || '—' }} · 运输批次 #{{ row.batchNo }}</div><div class="item-summary">{{ row.portOfLoading || '待确认装货港' }} → {{ row.portOfDischarge || '待确认目的港' }}</div></template></el-table-column>
              <el-table-column label="状态" width="120"><template #default><el-tag size="small" type="warning" effect="plain">待部门处理</el-tag></template></el-table-column>
              <el-table-column label="操作" width="150" fixed="right"><template #default="{row}"><router-link v-if="shippingTodoEntry(row)" :to="shippingTodoEntry(row)!.to" class="doc-link">{{ t(shippingTodoEntry(row)!.actionKey) }}</router-link><span v-else>{{t('todos.noSourceLink')}}</span></template></el-table-column>
            </el-table>
          </section>
          <section v-if="visibleProcurementTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>{{ t('todos.procurementTasks') }}</h3></div><el-tag type="warning" effect="plain">{{ procurementTasks.length }} {{ t('todos.items') }}</el-tag></div>
            <el-table :data="visibleProcurementTasks" class="home-table sourcing-todo-table">
              <el-table-column :label="t('todos.priority')" width="145"><template #default><el-tag size="small" type="warning" effect="light">{{ t('todos.priorityLevels.HIGH') }}</el-tag><div class="priority-reason">{{ t('todos.procurementActionRequired') }}</div></template></el-table-column>
              <el-table-column :label="t('todos.workItem')" min-width="360"><template #default="{row}"><div class="item-title">{{ procurementReworkLabel(row.requestType) }} · {{ row.caseNo }}</div><div class="item-meta">{{ row.productName||t('todos.allProducts') }} · {{ row.supplierName||t('todos.newSupplier') }}</div><div class="item-summary">{{ row.reason }}</div></template></el-table-column>
              <el-table-column :label="t('todos.submittedAt')" width="170"><template #default="{row}">{{ formatTime(row.createdAt) }}</template></el-table-column>
              <el-table-column :label="t('common.status')" width="120"><template #default><el-tag size="small" type="warning" effect="plain">{{ t('todos.waitingForMe') }}</el-tag></template></el-table-column>
              <el-table-column :label="t('common.actions')" width="150" fixed="right"><template #default="{row}"><router-link :to="{path:`/procurement/sourcing/${row.caseId}`,query:{tab:'plans',rework:String(row.id)}}" class="doc-link">{{ t('todos.goToSource') }}</router-link></template></el-table-column>
            </el-table>
          </section>
          <section v-if="visibleShippingTasks.length" class="todo-source-section">
            <div class="todo-source-head"><div><h3>船运补充任务</h3></div><el-tag type="warning" effect="plain">{{ shippingTasks.length }} 项</el-tag></div>
            <el-table :data="visibleShippingTasks" class="home-table sourcing-todo-table"><el-table-column label="优先级" width="145"><template #default><el-tag size="small" type="warning">高</el-tag><div class="priority-reason">需要补充船运报价</div></template></el-table-column><el-table-column label="工作事项" min-width="360"><template #default="{row}"><div class="item-title">{{ shippingReworkLabel(row.requestType) }} · {{ row.caseNo }}</div><div class="item-meta">{{ row.productName||'全部货物' }} · {{ row.carrierForwarder||'新增船运公司' }}</div><div class="item-summary">{{ row.reason }}</div></template></el-table-column><el-table-column label="发布于" width="170"><template #default="{row}">{{ formatTime(row.createdAt) }}</template></el-table-column><el-table-column label="状态" width="120"><template #default><el-tag size="small" type="warning" effect="plain">待部门处理</el-tag></template></el-table-column><el-table-column label="操作" width="76" align="center" fixed="right"><template #default="{row}"><el-dropdown trigger="click" @command="handleShippingTaskAction($event,row)"><el-button text circle aria-label="操作"><span aria-hidden="true">•••</span></el-button><template #dropdown><el-dropdown-menu><el-dropdown-item command="open">进入船运询价</el-dropdown-item><el-dropdown-item command="resolve" divided>完成任务</el-dropdown-item></el-dropdown-menu></template></el-dropdown></template></el-table-column></el-table>
          </section>
          </template>
          <div v-if="showEmptyPending" class="pending-empty">
            <HomeEmpty :description="keyword.trim() || bizType ? t('todos.noMatchingTasks') : t('todos.empty')" />
            <el-button v-if="keyword.trim() || bizType" link type="primary" @click="clearPendingFilters">{{ t('todos.clearFilters') }}</el-button>
          </div>
        </div>
      </template>

      <template v-else-if="activeTab === 'handled'">
        <InquiryTodos mode="handled" />
        <el-table :data="todos" v-loading="loading" class="home-table">
          <el-table-column :label="t('todos.workItem')" min-width="330">
            <template #default="{ row }">
              <div class="item-title">{{ bizTypeLabel(row.instance.bizType) }} · {{ row.instance.bizNo }}</div>
              <div class="item-meta">{{ row.task.nodeName }} · {{ summaryText(row.instance.bizSummary) }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('todos.myDecision')" width="180">
            <template #default="{ row }"><el-tag size="small" :type="taskTagType(row.task.status)">{{ taskStatusLabel(row.task.status) }}</el-tag><span v-if="row.task.comment" class="comment">{{ row.task.comment }}</span></template>
          </el-table-column>
          <el-table-column :label="t('todos.actedAt')" width="170"><template #default="{ row }">{{ formatTime(row.task.actedAt) }}</template></el-table-column>
          <el-table-column :label="t('todos.docResult')" width="120"><template #default="{ row }"><el-tag size="small" effect="plain" :type="instanceTagType(row.instance.status)">{{ instanceStatusLabel(row.instance.status) }}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="100" fixed="right"><template #default="{ row }"><router-link v-if="approvalSourceLink(row.instance)" :to="approvalSourceLink(row.instance)!" class="doc-link">{{ t('common.view') }}</router-link></template></el-table-column>
          <template #empty><HomeEmpty :description="t('todos.emptyHandled')" /></template>
        </el-table>
      </template>

      <template v-else-if="activeTab === 'submitted'">
        <el-table :data="submitted" v-loading="loading" class="home-table">
          <el-table-column :label="t('todos.workItem')" min-width="360">
            <template #default="{ row }"><div class="item-title">{{ bizTypeLabel(row.bizType) }} · {{ row.bizNo }}</div><div class="item-summary">{{ summaryText(row.bizSummary) }}</div></template>
          </el-table-column>
          <el-table-column :label="t('todos.submittedAt')" width="170"><template #default="{ row }">{{ formatTime(row.submittedAt) }}</template></el-table-column>
          <el-table-column :label="t('common.status')" width="130"><template #default="{ row }"><el-tag size="small" effect="plain" :type="instanceTagType(row.status)">{{ instanceStatusLabel(row.status) }}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="100" fixed="right"><template #default="{ row }"><router-link v-if="approvalSourceLink(row)" :to="approvalSourceLink(row)!" class="doc-link">{{ t('common.view') }}</router-link></template></el-table-column>
          <template #empty><HomeEmpty :description="t('todos.emptySubmitted')" /></template>
        </el-table>
      </template>

      <template v-else-if="activeTab === 'reminders'">
        <el-table :data="reminders" v-loading="loading" class="home-table">
          <el-table-column :label="t('todos.priority')" width="125">
            <template #default="{ row }"><el-tag size="small" :type="homeReminderTagType(row.timing)" effect="light">{{ reminderTimingLabel(row.timing) }}</el-tag></template>
          </el-table-column>
          <el-table-column :label="t('todos.workItem')" min-width="360">
            <template #default="{ row }">
              <div class="item-title"><span v-if="row.unread" class="unread-dot" /><template v-if="row.scheduleNo">{{ row.bizNo }} · </template>{{ row.title }}</div>
              <div class="item-meta">{{ reminderSourceLabel(row.source) }}<template v-if="row.scheduleNo"> · {{ t('shipping.scheduleNo') }}：{{ row.scheduleNo }}</template><template v-else-if="row.bizNo"> · {{ row.bizNo }}</template></div>
              <div class="item-summary">{{ row.content }}</div>
            </template>
          </el-table-column>
          <el-table-column :label="t('todos.reminderDueAt')" width="145"><template #default="{ row }">{{ row.dueAt || '—' }}</template></el-table-column>
          <el-table-column :label="t('common.status')" width="100"><template #default="{ row }"><el-tag size="small" :type="row.unread ? 'primary' : 'info'" effect="plain">{{ row.unread ? t('todos.unread') : t('todos.read') }}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="170" fixed="right">
            <template #default="{ row }">
              <el-button v-if="row.source === 'SHIPPING_ACTION'" link type="primary" @click="resolveOperationalReminder(row)">填写结果</el-button>
              <template v-else>
                <el-button v-if="row.unread" link type="primary" @click="markReminderRead(row)">{{ t('todos.markRead') }}</el-button>
                <el-button link type="primary" @click="openReminder(row)">{{ t('common.view') }}</el-button>
              </template>
            </template>
          </el-table-column>
          <template #empty><HomeEmpty :description="t('todos.emptyReminders')" /></template>
        </el-table>
      </template>

      <HomeEmpty v-else :description="futureEmptyText" />

      <el-pagination v-if="hasDataSource && total > 0" class="pager" layout="total, prev, pager, next" :total="total" :page-size="pageSize" :current-page="page" @current-change="changePage" />
    </el-card>
    <el-dialog v-model="finalShippingResolveOpen" title="填写船运最终复询结果" width="720px"><el-alert type="warning" :closable="false" title="这里必须填写船运公司最终确认的运费、开船日、到港日和有效期。"/><el-form label-width="128px" style="margin-top:16px"><el-form-item label="结果说明" required><el-input v-model="finalShippingResolveForm.note"/></el-form-item><el-form-item label="币种 / 最终运费" required><el-input v-model="finalShippingResolveForm.currency" style="width:120px"/><el-input v-model="finalShippingResolveForm.freightAmount" inputmode="decimal" style="width:240px;margin-left:8px"/></el-form-item><el-form-item label="预计开船日" required><el-date-picker v-model="finalShippingResolveForm.estimatedDeparture" value-format="YYYY-MM-DD"/></el-form-item><el-form-item label="预计到港日" required><el-date-picker v-model="finalShippingResolveForm.estimatedArrival" value-format="YYYY-MM-DD"/></el-form-item><el-form-item label="有效期至" required><el-date-picker v-model="finalShippingResolveForm.validUntil" value-format="YYYY-MM-DD"/></el-form-item></el-form><template #footer><el-button @click="finalShippingResolveOpen=false">取消</el-button><el-button type="primary" :loading="finalShippingSaving" @click="submitFinalShippingResolve">保存最终复询</el-button></template></el-dialog>

  </div>
</template>

<script setup lang="ts">
import {shippingTodoEntry} from '../lib/shippingTodoEntry'
import {hasPassedDeliveryInspection,type DeliveryQualityTask} from '../lib/deliveryQuality'

import InquiryTodos from '../components/InquiryTodos.vue'
import { getTodoReception, receivesTodo, type TodoReception } from '../lib/todoReception'
import { computed, defineComponent, h, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElEmpty, ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { get, post, quietErrors } from '../api'
import { onLive } from '../live'
import { useAuthStore } from '../stores/auth'
import { approvalPriorityTagType, approvalSourceLink, approvalTimeAmount } from '../lib/homeApproval'
import {
  announceReminderChanged,
  homeReminderTagType,
  onReminderChanged,
  type HomeReminder,
  type HomeReminderSource,
  type HomeReminderSourceState,
  type HomeReminderSummary,
  type HomeReminderTiming,
} from '../lib/homeReminders'

interface Task { id: string; nodeSeq: number; nodeName: string; status: string; comment: string; actedAt: string }
interface Instance { id: string; bizType: string; bizId: string; bizNo: string; bizSummary: string; submitterName: string; status: string; currentSeq: number; submittedAt: string; finishedAt: string }
interface Todo { task: Task; instance: Instance; dueAt: string; priority: string; remainingMinutes: string | number }
interface ProcurementTaskCase { id:string; caseNo:string; customerName:string; title:string; updatedAt:string; openReworkCount:number|string; myOpenReworkCount:number|string }
interface ProcurementReworkTask { id:string; caseId:string; caseNo:string; requestType:string; productName:string; supplierName:string; reason:string; createdAt:string; assignedBuyerId:string|number; status:string }
interface ShippingReworkTask { id:string; caseId:string; caseNo:string; caseTitle:string; requestType:string; productName:string; carrierForwarder:string; reason:string; createdAt:string; assignedShippingId:string|number; status:string; finalRecheckTaskId?:string|number }
interface ExecutionRequirement { id:string; contractId:string; contractNo:string; customerName:string; productName:string; requiredDate:string; status:string }
interface FinanceExecutionTask {id:string;contractNo:string;customerName:string;currency:string;totalAmount:string}
interface PurchaseFinanceTask {id:string;poNo:string;supplierName:string;exportContractNo:string;currency:string;totalAmount:string;signedContractName:string}
interface LogisticsFinanceTask extends PurchaseFinanceTask {businessDocumentNo:string;sourceBusinessId:string;transportBatchNo:number;businessDocumentNoGenerated?:boolean}
interface ExecutionProcurementTask { key:string; contractNo:string; customerName:string; productNames:string; productCount:number; requiredDate:string }
interface ExecutionShippingTask { id:string; contractNo:string; customerName:string; batchNo:number; portOfLoading:string; portOfDischarge:string; status:string }
interface QualityTodoTask { id:string; taskNo:string; poNo:string; supplierName:string; batchNo:number; expectedDate:string; status:string }
interface QualityProcurementTodo extends QualityTodoTask {poId:string;lines:Array<{productName:string;requestedQty:string;qualifiedQty:string;uomCode:string;issueDescription:string}>}
type HomeTab = 'pending' | 'submitted' | 'responsible' | 'reminders' | 'handled'

const HomeEmpty = defineComponent({
  props: { description: { type: String, required: true } },
  setup(props) { return () => h(ElEmpty, { description: props.description, imageSize: 88 }) },
})

const { t, te, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
interface NegotiationTodo {
  id:string; batchKey:string; contractNo:string; productName:string; supplierName:string
  instructions:string; poId:string; orderStatus:string
}
const negotiationTasks = ref<NegotiationTodo[]>([])
const visibleNegotiationTasks = computed(() => {
  if (bizType.value && bizType.value !== 'PURCHASE_ORDER') return []
  const search = keyword.value.trim().toLocaleLowerCase()
  return negotiationTasks.value.filter(task => !search || [task.contractNo,task.productName,task.supplierName,task.instructions].some(value => value?.toLocaleLowerCase().includes(search)))
})
function openNegotiationTodo(task: NegotiationTodo) {
  if (task.orderStatus === 'REJECTED') return router.push({path:'/purchase-orders',query:{status:'REJECTED',order:task.poId}})
  return router.push({path:`/requirements/${encodeURIComponent(task.batchKey)}`,query:{step:'negotiation',assignment:task.id}})
}
async function loadNegotiationTasks() {
  if (!auth.can('procurement:requirement:read')) return
  try {
    const result = await post<{assignments?:NegotiationTodo[]}>('/execution-assignments',{action:'inbox'},quietErrors)
    negotiationTasks.value = result.assignments ?? []
  } catch {
    negotiationTasks.value = []
  }
}
const showPurchaseFinanceSection=computed(()=>auth.can('procurement:order:read')&&auth.can('procurement:recon:write')&&(!bizType.value||bizType.value==='PURCHASE_ORDER'))
const showLogisticsFinanceSection=computed(()=>auth.can('procurement:order:read')&&auth.can('procurement:recon:write')&&(!bizType.value||bizType.value==='SHIPPING_REQUOTE'))
const today = computed(() => new Intl.DateTimeFormat(
  locale.value === 'zh' ? 'zh-CN' : locale.value === 'es' ? 'es-ES' : 'en-US',
  { year: 'numeric', month: 'long', day: 'numeric', weekday: 'short' },
).format(new Date()))
const activeTab = ref<HomeTab>('pending')
const todos = ref<Todo[]>([])
const submitted = ref<Instance[]>([])
const reminders = ref<HomeReminder[]>([])
const reminderSummary = ref<HomeReminderSummary>({ upcoming: 0, overdue: 0, unread: 0 })
const total = ref(0)
const pendingTotal = ref(0)
const pendingCountAvailable = ref(true)
const inquiryPendingCount=ref(0)
const todoReception = ref<TodoReception | null>(null)
const todoReceptionError = ref(false)
const showOperationalTodos = computed(() => todoReception.value !== null && !todoReception.value.approvalsOnly)
const procurementTasks = ref<ProcurementReworkTask[]>([])
const procurementTasksAvailable = ref(false)
const shippingTasks = ref<ShippingReworkTask[]>([])
const shippingTasksAvailable = ref(false)
const executionProcurementTasks = ref<ExecutionProcurementTask[]>([])
const executionProcurementTasksAvailable = ref(false)
const executionShippingTasks = ref<ExecutionShippingTask[]>([])
const executionShippingTasksAvailable = ref(false)
const financeTasks=ref<FinanceExecutionTask[]>([])
const purchaseFinanceTasks=ref<PurchaseFinanceTask[]>([])
const logisticsFinanceTasks=ref<LogisticsFinanceTask[]>([])
const visibleLogisticsFinanceTasks=computed(()=>logisticsFinanceTasks.value.filter(row=>(!bizType.value||bizType.value==='SHIPPING_REQUOTE')&&(!keyword.value.trim()||[row.poNo,row.businessDocumentNo,row.exportContractNo,row.supplierName,row.signedContractName].some(value=>String(value||'').toLocaleLowerCase().includes(keyword.value.trim().toLocaleLowerCase())))))
const visiblePurchaseFinanceTasks=computed(()=>purchaseFinanceTasks.value.filter(row=>(!bizType.value||bizType.value==='PURCHASE_ORDER')&&(!keyword.value.trim()||[row.poNo,row.supplierName,row.exportContractNo,row.signedContractName].some(value=>String(value||'').toLocaleLowerCase().includes(keyword.value.trim().toLocaleLowerCase())))))
const executionTasksError=ref(false)
const visibleFinanceTasks=computed(()=>financeTasks.value.filter(row=>!keyword.value.trim()||[row.contractNo,row.customerName].some(value=>value.toLocaleLowerCase().includes(keyword.value.trim().toLocaleLowerCase()))))
type DeliveryTodo={progressText?:string;id:string;poNo:string;supplierName:string;expectedDate:string;fulfillmentMode:string;businessType:string;closedAt:string;sourceBusinessId:string;contractVerifiedAt:string}
const deliveryTasks=ref<DeliveryTodo[]>([])
const visibleDeliveryTasks=computed(()=>deliveryTasks.value.filter(row=>!keyword.value||[row.poNo,row.supplierName].some(text=>text.toLowerCase().includes(keyword.value.toLowerCase()))))
const qualityTasks = ref<QualityTodoTask[]>([])
const qualityTasksAvailable = ref(false)
const qualityProcurementTasks=ref<QualityProcurementTodo[]>([])
const qualityProcurementTasksAvailable=ref(false)
const finalShippingResolveOpen = ref(false)
const finalShippingSaving = ref(false)
const finalShippingResolveForm = reactive({id:'',note:'',currency:'USD',freightAmount:'',estimatedDeparture:'',estimatedArrival:'',validUntil:''})
const page = ref(1)
const pageSize = 100
const keyword = ref('')
const bizType = ref('')
const statusFilter = ref('')
const loading = ref(false)
const countLoading = ref(false)
const reminderCountLoading = ref(false)
const sourceError = ref(false)
const reminderSourceError = ref(false)
const reminderSummaryAvailable = ref(false)
const reminderSource = ref('')
const reminderTiming = ref('')
const reminderRead = ref('')
const markingRead = ref(false)
const updatedAt = ref('')
const operationalPendingTotal = computed(() =>
  inquiryPendingCount.value + negotiationTasks.value.length + deliveryTasks.value.length +
  purchaseFinanceTasks.value.length + logisticsFinanceTasks.value.length + financeTasks.value.length +
  (procurementTasksAvailable.value ? procurementTasks.value.length : 0) +
  (shippingTasksAvailable.value ? shippingTasks.value.length : 0) +
  (executionProcurementTasksAvailable.value ? executionProcurementTasks.value.length : 0) +
  (executionShippingTasksAvailable.value ? executionShippingTasks.value.length : 0) +
  (qualityTasksAvailable.value ? qualityTasks.value.length : 0) +
  (qualityProcurementTasksAvailable.value ? qualityProcurementTasks.value.length : 0),
)
const combinedPendingTotal = computed(() =>
  (pendingCountAvailable.value ? pendingTotal.value : 0) +
  (showOperationalTodos.value ? operationalPendingTotal.value : 0),
)
const combinedPendingAvailable = computed(() => pendingCountAvailable.value || (showOperationalTodos.value && (
  operationalPendingTotal.value > 0 || procurementTasksAvailable.value || shippingTasksAvailable.value ||
  executionProcurementTasksAvailable.value || executionShippingTasksAvailable.value ||
  qualityTasksAvailable.value || qualityProcurementTasksAvailable.value
)))
const visibleProcurementTasks = computed(() => {
  const query = keyword.value.trim().toLocaleLowerCase()
  return procurementTasks.value.filter(row => !query || [row.caseNo,row.productName,row.supplierName,row.reason].some(value => String(value||'').toLocaleLowerCase().includes(query)))
})
const visibleShippingTasks = computed(() => { const query=keyword.value.trim().toLocaleLowerCase();return shippingTasks.value.filter(row=>!query||[row.caseNo,row.caseTitle,row.productName,row.carrierForwarder,row.reason].some(value=>String(value||'').toLocaleLowerCase().includes(query))) })
const visibleExecutionProcurementTasks = computed(() => { const query=keyword.value.trim().toLocaleLowerCase();return executionProcurementTasks.value.filter(row=>!query||[row.contractNo,row.customerName,row.productNames].some(value=>String(value||'').toLocaleLowerCase().includes(query))) })
const visibleExecutionShippingTasks = computed(() => { const query=keyword.value.trim().toLocaleLowerCase();return executionShippingTasks.value.filter(row=>!query||[row.contractNo,row.customerName,row.portOfLoading,row.portOfDischarge].some(value=>String(value||'').toLocaleLowerCase().includes(query))) })
const visibleQualityTasks = computed(() => { const query=keyword.value.trim().toLocaleLowerCase();return qualityTasks.value.filter(row=>!query||[row.taskNo,row.poNo,row.supplierName].some(value=>String(value||'').toLocaleLowerCase().includes(query))) })
const visibleQualityProcurementTasks=computed(()=>{const query=keyword.value.trim().toLocaleLowerCase();return qualityProcurementTasks.value.filter(row=>!query||[row.taskNo,row.poNo,row.supplierName].some(v=>String(v||'').toLocaleLowerCase().includes(query)))})
function qualityTodoSummary(row:QualityProcurementTodo){return row.lines.filter(l=>Number(l.requestedQty)>Number(l.qualifiedQty)).map(l=>`${l.productName} 未合格 ${Number(l.requestedQty)-Number(l.qualifiedQty)} ${l.uomCode}${l.issueDescription?' · '+l.issueDescription:''}`).join('；')}

const bizTypes = ['CONTRACT', 'PURCHASE_ORDER', 'PURCHASE_ORDER_CHANGE', 'SHIPPING_REQUOTE', 'TRAVEL_REIMBURSEMENT', 'PAYMENT', 'LC_AMENDMENT', 'STOCK_ADJUST']
const hasApprovalSource = computed(() => ['pending', 'submitted', 'handled'].includes(activeTab.value))
const hasReminderSource = computed(() => activeTab.value === 'reminders')
const hasDataSource = computed(() => hasApprovalSource.value || hasReminderSource.value)
const reminderSourceOptions = computed<HomeReminderSource[]>(() => [
  ...(auth.can('export:receipt:read') ? ['RECEIVABLE' as const] : []),
  ...(auth.can('shipping:schedule:read') ? ['ARRIVAL' as const, 'BL' as const] : []),
  'SHIPPING_ACTION',
  ...(auth.can('masterdata:customer:read') ? ['CUSTOMER_DOCUMENT' as const] : []),
  ...(auth.can('masterdata:supplier:read') ? ['SUPPLIER_DOCUMENT' as const] : []),
])
const statusOptions = computed(() => activeTab.value === 'handled'
  ? ['APPROVED', 'REJECTED', 'RETURNED', 'CANCELLED', 'SKIPPED']
  : ['RUNNING', 'APPROVED', 'REJECTED', 'RETURNED', 'CANCELLED'])
const futureEmptyText = computed(() => t('todos.emptyResponsible'))

function activate(tab: HomeTab) {
  if (activeTab.value === tab) return
  activeTab.value = tab
  onTabChange(tab)
}

function activateReminderFilter(timing: HomeReminderTiming) {
  activeTab.value = 'reminders'
  page.value = 1
  keyword.value = ''
  reminderSource.value = ''
  reminderRead.value = ''
  reminderTiming.value = timing
  sourceError.value = false
  void load()
}

function activateUnreadReminders() {
  activeTab.value = 'reminders'
  page.value = 1
  keyword.value = ''
  reminderSource.value = ''
  reminderTiming.value = ''
  reminderRead.value = 'UNREAD'
  sourceError.value = false
  void load()
}

function onTabChange(tab: string | number) {
  activeTab.value = String(tab) as HomeTab
  page.value = 1
  keyword.value = ''
  bizType.value = ''
  statusFilter.value = ''
  reminderSource.value = ''
  reminderTiming.value = ''
  reminderRead.value = ''
  sourceError.value = false
  reminderSourceError.value = false
  void load()
}

const showEmptyPending = computed(() => {
  if (loading.value || !operationalReady.value || sourceError.value || todoReceptionError.value || todos.value.length) return false
  if (!showOperationalTodos.value) return true
  return inquiryState.value === 'ready' && !executionTasksError.value && !inquiryPendingCount.value &&
    ![visibleNegotiationTasks, visibleDeliveryTasks, visibleFinanceTasks, visibleQualityTasks,
      visibleQualityProcurementTasks, visibleExecutionProcurementTasks, visibleExecutionShippingTasks,
      visibleProcurementTasks, visibleShippingTasks].some(group => group.value.length) &&
    !(showPurchaseFinanceSection.value && visiblePurchaseFinanceTasks.value.length) &&
    !(showLogisticsFinanceSection.value && visibleLogisticsFinanceTasks.value.length)
})
const inquiryState = ref('loading')
const operationalReady = ref(false)
function clearPendingFilters() {
  keyword.value = ''
  bizType.value = ''
  query()
}
function query() { page.value = 1; void load() }
function changePage(next: number) { page.value = next; void load() }

async function loadPendingCount() {
  countLoading.value = true
  try {
    const data = await get<{ meta: { total: string } }>('/approvals/todos', { page: 1, page_size: 1 }, quietErrors)
    pendingTotal.value = Number(data.meta?.total ?? 0)
    pendingCountAvailable.value = true
  } catch {
    pendingCountAvailable.value = false
  } finally {
    countLoading.value = false
  }
}

async function loadProcurementTasks() { procurementTasks.value=[]; procurementTasksAvailable.value=true }

async function loadShippingTasks(){shippingTasks.value=[];shippingTasksAvailable.value=true}

async function loadQualityTasks() {
  if (!auth.can('quality:task:write')) {
    qualityTasks.value = []
    qualityTasksAvailable.value = false
    return
  }
  try {
    const data = await get<{ tasks: QualityTodoTask[] }>('/quality/tasks', { tab: 'PENDING', page: 1, page_size: 200 }, quietErrors)
    if (!receivesTodo(todoReception.value, 'QUALITY')) return
    qualityTasks.value = data.tasks ?? []
    qualityTasksAvailable.value = true
  } catch {
    qualityTasks.value = []
    qualityTasksAvailable.value = false
  }
}
async function loadDeliveryTasks(){
 if(!auth.can('procurement:order:write')||!auth.can('procurement:order:read')){deliveryTasks.value=[];return}
 try{
  const orders:DeliveryTodo[]=[]
  for(const status of ['ORDERED','PARTIALLY_RECEIVED']){let page=1,total=0;do{const d=await get<{orders:DeliveryTodo[];meta:{total:string}}>('/purchase-orders',{status,page,page_size:100},quietErrors);orders.push(...(d.orders||[]));total=Number(d.meta?.total||0);page++}while((page-1)*100<total)}
  const candidates=orders.filter(row=>row.fulfillmentMode!=='WAREHOUSE'&&row.businessType!=='LOGISTICS'&&!row.closedAt&&(!Number(row.sourceBusinessId)||row.contractVerifiedAt)),qualified:DeliveryTodo[]=[]
  for(let index=0;index<candidates.length;index+=8){const batch=await Promise.all(candidates.slice(index,index+8).map(async row=>{try{const d=await get<{tasks:DeliveryQualityTask[]}>(`/purchase-orders/${row.id}/quality-inspections`,{},quietErrors);if(!hasPassedDeliveryInspection(d.tasks||[]))return null;const detail=await get<{items:{qty:string;receivedQty:string;uomCode:string}[]}>(`/purchase-orders/${row.id}`,{},quietErrors);row.progressText=(detail.items??[]).map(item=>`已交 ${Number(item.receivedQty)} / ${Number(item.qty)} ${item.uomCode}，剩余 ${Math.max(0,Number(item.qty)-Number(item.receivedQty))} ${item.uomCode}`).join('；');return row}catch{return null}}));qualified.push(...batch.filter((row):row is DeliveryTodo=>row!==null))}
  deliveryTasks.value=qualified
 }catch{deliveryTasks.value=[]}
}
async function loadQualityProcurementTasks(){if(!auth.can('procurement:order:read')){qualityProcurementTasks.value=[];qualityProcurementTasksAvailable.value=false;return}try{const data=await get<{tasks:QualityProcurementTodo[]}>('/quality/procurement-todos',{},quietErrors);if(!receivesTodo(todoReception.value,'QUALITY'))return;qualityProcurementTasks.value=data.tasks??[];qualityProcurementTasksAvailable.value=true}catch{qualityProcurementTasks.value=[];qualityProcurementTasksAvailable.value=false}}

async function loadExecutionTasks() {
  purchaseFinanceTasks.value=[];logisticsFinanceTasks.value=[];financeTasks.value=[];executionProcurementTasks.value=[];executionShippingTasks.value=[];executionTasksError.value=false
  executionProcurementTasksAvailable.value=false;executionShippingTasksAvailable.value=false
  try {
    const data=await get<{finance:FinanceExecutionTask[];purchaseFinance:PurchaseFinanceTask[];logisticsFinance:LogisticsFinanceTask[];procurement:ExecutionRequirement[];shipping:ExecutionShippingTask[]}>('/home/execution-todos',{},quietErrors)
    purchaseFinanceTasks.value=data.purchaseFinance??[];logisticsFinanceTasks.value=data.logisticsFinance??[];financeTasks.value=data.finance??[]
    executionShippingTasks.value=receivesTodo(todoReception.value,'LOGISTICS_INQUIRY') ? data.shipping??[] : []
    const groups=new Map<string,ExecutionRequirement[]>()
    for(const row of receivesTodo(todoReception.value,'EXECUTION_PROCUREMENT') ? data.procurement??[] : []){const key=row.contractId||row.contractNo;groups.set(key,[...(groups.get(key)??[]),row])}
    executionProcurementTasks.value=[...groups.entries()].map(([key,rows])=>({key,contractNo:rows[0]?.contractNo??'',customerName:rows[0]?.customerName??'',productNames:[...new Set(rows.map(row=>row.productName).filter(Boolean))].join('、'),productCount:rows.length,requiredDate:[...rows.map(row=>row.requiredDate).filter(Boolean)].sort()[0]??''}))
    executionProcurementTasksAvailable.value=true;executionShippingTasksAvailable.value=true
  } catch {executionTasksError.value=true}
}
function shippingReworkLabel(value:string){return value==='ADD_CARRIER'?'增加船运公司':value==='REQUOTE'?'更新船运报价/船期':'重新议价'}
function handleShippingTaskAction(command:string,row:ShippingReworkTask){if(command==='open'){void router.push('/shipping/sourcing');return}if(command==='resolve')void resolveShippingTask(row)}
async function resolveShippingTask(row:ShippingReworkTask){if(Number(row.finalRecheckTaskId||0)>0){Object.assign(finalShippingResolveForm,{id:String(row.id),note:'',currency:'USD',freightAmount:'',estimatedDeparture:'',estimatedArrival:'',validUntil:''});finalShippingResolveOpen.value=true;return}const result=await ElMessageBox.prompt('请说明已完成的询价、议价或新增船运公司结果。','完成船运补充任务',{inputPlaceholder:'例如：已录入该船运公司的最新报价版本',inputValidator:(value:string)=>!!value.trim()||'请填写处理结果'}).catch(()=>null);if(!result)return;await post(`/shipping/sourcing-reworks/${row.id}/resolve`,{resolution_note:result.value});await refreshAll();ElMessage.success('船运补充任务已完成')}
async function submitFinalShippingResolve(){const f=finalShippingResolveForm;if(!f.note||!f.currency||Number(f.freightAmount)<=0||!f.estimatedDeparture||!f.estimatedArrival||!f.validUntil){ElMessage.warning('请完整填写最终运费、开船日、到港日和有效期');return}if(f.estimatedArrival<f.estimatedDeparture){ElMessage.warning('到港日不能早于开船日');return}finalShippingSaving.value=true;try{await post(`/shipping/sourcing-reworks/${f.id}/resolve`,{resolution_note:f.note,final_currency:f.currency,final_freight_amount:String(f.freightAmount),final_estimated_departure:f.estimatedDeparture,final_estimated_arrival:f.estimatedArrival,final_valid_until:f.validUntil});finalShippingResolveOpen.value=false;await refreshAll();ElMessage.success('船运最终复询结果已保存')}finally{finalShippingSaving.value=false}}

function procurementReworkLabel(value: string) {
  return value === 'ADD_SUPPLIER' ? t('todos.addSupplier') : value === 'REQUOTE' ? t('todos.requote') : t('todos.renegotiate')
}

function applyReminderResponse(data: { summary?: HomeReminderSummary; sources?: HomeReminderSourceState[] }) {
  reminderSummary.value = data.summary ?? { upcoming: 0, overdue: 0, unread: 0 }
  reminderSummaryAvailable.value = true
  reminderSourceError.value = (data.sources ?? []).some((source) => !source.available)
}

async function loadReminderSummary() {
  reminderCountLoading.value = true
  try {
    const data = await get<{ summary: HomeReminderSummary; sources: HomeReminderSourceState[] }>(
      '/home/reminders', { page: 1, page_size: 1 }, quietErrors,
    )
    applyReminderResponse(data)
  } catch {
    reminderSummaryAvailable.value = false
    reminderSourceError.value = true
  } finally {
    reminderCountLoading.value = false
  }
}

async function load() {
  if (hasReminderSource.value) {
    loading.value = true
    reminderSourceError.value = false
    try {
      const data = await get<{
        items: HomeReminder[]
        total: number
        summary: HomeReminderSummary
        sources: HomeReminderSourceState[]
      }>('/home/reminders', {
        page: page.value,
        page_size: pageSize,
        keyword: keyword.value,
        source: reminderSource.value,
        timing: reminderTiming.value,
        read: reminderRead.value,
      }, quietErrors)
      reminders.value = data.items ?? []
      todos.value = []
      submitted.value = []
      total.value = Number(data.total ?? 0)
      applyReminderResponse(data)
      updatedAt.value = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
    } catch {
      reminderSourceError.value = true
      reminders.value = []
      total.value = 0
    } finally {
      loading.value = false
    }
    return
  }
  if (!hasApprovalSource.value) {
    todos.value = []
    submitted.value = []
    reminders.value = []
    total.value = 0
    return
  }
  loading.value = true
  sourceError.value = false
  try {
    const requestedBizType = String(route.query.bizType ?? '')
    const requestedBizID = String(route.query.bizId ?? '')
    const targeted = Boolean(requestedBizType || requestedBizID)
    const params = {
      page: targeted ? 1 : page.value,
      page_size: targeted ? 200 : pageSize,
      biz_type: bizType.value || requestedBizType,
      keyword: keyword.value,
      status: activeTab.value === 'pending' ? '' : activeTab.value === 'handled' ? (statusFilter.value || 'HANDLED') : statusFilter.value,
    }
    if (activeTab.value === 'submitted') {
      const data = await get<{ instances: Instance[]; meta: { total: string } }>('/approvals/submitted', params, quietErrors)
      submitted.value = (data.instances ?? []).filter((row) => !requestedBizID || String(row.bizId) === requestedBizID)
      todos.value = []
      reminders.value = []
      total.value = targeted ? submitted.value.length : Number(data.meta?.total ?? 0)
    } else {
      const data = await get<{ todos: Todo[]; meta: { total: string } }>('/approvals/todos', params, quietErrors)
      todos.value = (data.todos ?? []).filter((row) => !requestedBizID || String(row.instance.bizId) === requestedBizID)
      submitted.value = []
      reminders.value = []
      total.value = targeted ? todos.value.length : Number(data.meta?.total ?? 0)
    }
    updatedAt.value = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  } catch {
    sourceError.value = true
    todos.value = []
    submitted.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

let operationalRefresh = 0
async function loadOperationalTodos() {
  operationalReady.value = false
  const request = ++operationalRefresh
  let reception: TodoReception
  try {
    reception = await getTodoReception('/me/todo-reception')
  } catch {
    if (request === operationalRefresh) {
      todoReception.value = null
      inquiryPendingCount.value = 0
      todoReceptionError.value = true
    }
    return
  }
  if (request !== operationalRefresh) return
  todoReceptionError.value = false
  if (JSON.stringify(reception) !== JSON.stringify(todoReception.value)) todoReception.value = reception
  if (reception.approvalsOnly) {
    operationalReady.value = true
    inquiryPendingCount.value = 0
    return
  }
  const quality = receivesTodo(reception, 'QUALITY')
  if (!quality) {
    qualityTasks.value = []
    qualityTasksAvailable.value = false
    qualityProcurementTasks.value = []
    qualityProcurementTasksAvailable.value = false
  }
  await Promise.all([
    loadProcurementTasks(), loadShippingTasks(), loadExecutionTasks(), loadNegotiationTasks(), loadDeliveryTasks(),
    ...(quality ? [loadQualityTasks(), loadQualityProcurementTasks()] : []),
  ])
  if (request === operationalRefresh) operationalReady.value = true
}

async function refreshAll() {
  await Promise.all([
    loadPendingCount(), loadOperationalTodos(), load(),
    ...(!hasReminderSource.value ? [loadReminderSummary()] : []),
  ])
}

async function markReminderRead(item: HomeReminder) {
  if (!item.unread) return
  await post('/home/reminders/read', { source: item.source, ids: [item.sourceId] })
  announceReminderChanged()
  await load()
}

async function markAllRemindersRead() {
  markingRead.value = true
  try {
    const data = await post<{ marked: number; sources: HomeReminderSourceState[] }>(
      '/home/reminders/read', { source: 'ALL', ids: [] }, quietErrors,
    )
    announceReminderChanged()
    if ((data.sources ?? []).length > 0) ElMessage.warning(t('todos.markReadPartial'))
    else ElMessage.success(t('todos.markedRead', { count: Number(data.marked ?? 0) }))
    await load()
  } finally {
    markingRead.value = false
  }
}

async function openReminder(item: HomeReminder) {
  if (item.unread) {
    try { await markReminderRead(item) } catch { /* 原业务入口仍应可打开 */ }
  }
  if (item.detailUrl) await router.push(item.detailUrl)
}

async function resolveOperationalReminder(item: HomeReminder) {
  const result = await ElMessageBox.prompt(
    '请填写你已完成的联系、核对或付款准备结果。处理结果会同步回正式船期。',
    item.title,
    { inputPlaceholder: '例如：已联系工厂，确认新的进仓安排', inputValidator: (value: string) => !!value.trim() || '请填写处理结果' },
  ).catch(() => null)
  if (!result) return
  await post(`/shipping/operational-alerts/${item.sourceId}/resolve`, { resolutionNote: result.value })
  announceReminderChanged()
  await load()
  ElMessage.success('处理结果已同步')
}

type SummaryValue = string | number | boolean | null | SummaryValue[] | { [key: string]: SummaryValue }

function parseSummary(raw: string): Record<string, SummaryValue> {
  if (!raw) return {}
  try { const parsed = JSON.parse(raw); return typeof parsed === 'object' && parsed !== null ? parsed : {} } catch { return {} }
}

function compactSummaryNumber(value: SummaryValue): string {
  const text = String(value ?? '')
  if (!/^-?\d+(?:\.\d+)?$/.test(text)) return text
  return text.replace(/(\.\d*?[1-9])0+$|\.0+$/, '$1')
}

function summaryValueText(key: string, value: SummaryValue): string {
  if (value === null) return ''
  if (key === 'expense_type' && ['TRAVEL', 'OFFICE_SUPPLIES', 'OTHER'].includes(String(value))) return t(`reimbursementExpense.${value}`)
  if (key === 'lines' && Array.isArray(value)) {
    return value.map((line) => {
      if (!line || Array.isArray(line) || typeof line !== 'object') return String(line ?? '')
      const product = String(line.product ?? line.name ?? '')
      const qty = compactSummaryNumber(line.qty ?? '')
      if (product && qty) return `${product} × ${qty}`
      return product || qty
    }).filter(Boolean).join('、')
  }
  if (Array.isArray(value)) return value.map((item) => summaryValueText('', item)).filter(Boolean).join('、')
  if (typeof value === 'object') {
    return Object.values(value).map((item) => summaryValueText('', item)).filter(Boolean).join(' / ')
  }
  return compactSummaryNumber(value)
}

function summaryText(raw: string): string {
  const summary = parseSummary(raw)
  const preferred = ['customer', 'supplier', 'forwarder', 'amount', 'laycan', 'etd', 'eta', 'carrier', 'payment_terms', 'expected_date', 'delivery_date', 'lines']
  const keys = [...preferred.filter((key) => key in summary), ...Object.keys(summary).filter((key) => !preferred.includes(key))]
  return keys
    .filter((key) => key !== 'approval_request_key' && key !== 'direct_finance_approval' && !(key === 'currency' && 'amount' in summary))
    .map((key) => {
      const value = key === 'amount' && summary.currency
        ? `${summaryValueText('currency', summary.currency)} ${summaryValueText(key, summary[key])}`
        : summaryValueText(key, summary[key])
      return value ? `${summaryLabel(key)}：${value}` : ''
    })
    .filter(Boolean)
    .join(' · ')
}
function summaryLabel(key: string): string { return labelOr(`todos.summaryKeys.${key}`, key) }
function taskStatusLabel(code: string): string { return code ? labelOr(`todos.task.${code}`, code) : '—' }
function priorityLabel(code: string): string { return labelOr(`todos.priorityLevels.${code}`, code) }
function instanceStatusLabel(code: string): string { return code ? labelOr(`todos.doc.${code}`, code) : '—' }
function bizTypeLabel(code: string): string { return code ? labelOr(`todos.biz.${code}`, code) : '—' }
function reminderSourceLabel(code?: string): string {
  if (!code) return '—'
  return labelOr(`todos.reminderSources.${code}`, code)
}
function reminderTimingLabel(code?: string): string {
  if (!code) return '—'
  return labelOr(`todos.timing.${code}`, code)
}
function labelOr(key: string, fallback: string): string { return te(key) ? t(key) : fallback }
function taskTagType(code: string): 'success' | 'danger' | 'warning' | 'info' { return ({ APPROVED: 'success', REJECTED: 'danger', RETURNED: 'warning' }[code] as 'success' | 'danger' | 'warning' | undefined) ?? 'info' }
function instanceTagType(code: string): 'success' | 'danger' | 'warning' | 'info' { return ({ APPROVED: 'success', REJECTED: 'danger', RETURNED: 'warning', RUNNING: 'warning' }[code] as 'success' | 'danger' | 'warning' | undefined) ?? 'info' }
function priorityTagType(code: string): 'danger' | 'warning' | 'info' { return approvalPriorityTagType(code) }
function priorityReason(rawMinutes: string | number): string {
  const amount = approvalTimeAmount(rawMinutes)
  const duration = t(`todos.duration${amount.unit[0].toUpperCase()}${amount.unit.slice(1)}`, { count: amount.count })
  return amount.overdue ? t('todos.overdueBy', { duration }) : t('todos.dueIn', { duration })
}
function formatTime(iso: string): string { return iso ? iso.replace('T', ' ').slice(0, 16) : '—' }

onMounted(refreshAll)
const stopListening = onLive((event) => {
  if (event.type === 'todo.changed' || event.type === 'doc.changed' || event.type === 'requirement.changed' || event.type === 'shipping.arrival_reminder') void refreshAll()
})
const stopReminderListening = onReminderChanged(() => { void refreshAll() })
onUnmounted(() => {
  stopListening()
  stopReminderListening()
})
</script>

<style scoped>
.home-page { max-width: 1500px; margin: 0 auto; color: #141817; }
.home-head { display:flex; align-items:center; justify-content:space-between; gap:12px; padding:12px 18px; border-radius:9px; color:white; background:linear-gradient(110deg,#153c58,#146a81); }
.head-copy,.head-meta,.head-actions { display:flex; align-items:center; gap:12px; flex-wrap:wrap; }
.home-head h1 { margin:0; font-size:21px; }
.head-meta,.updated { color:#dcecf4; font-size:12px; }
.meta-separator { width:3px; height:3px; background:currentColor; border-radius:50%; }
.summary-grid { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:1px; margin:10px 0; border:1px solid #dce6ec; background:#dce6ec; border-radius:8px; overflow:hidden; }
.summary-card { display:flex; align-items:center; justify-content:space-between; padding:10px 16px; border:0; background:white; color:#445b69; font-size:13px; }
.summary-card:not(:disabled) { cursor:pointer; }
.summary-card.active { background:#edf7fc; color:#165d82; }
.summary-card strong { font-size:21px; font-variant-numeric:tabular-nums; }
.summary-card.danger strong { color:#b63d44; }
.work-card { border-radius:8px; }
.work-card :deep(.el-card__body) { padding:10px 16px 16px; }
.todo-source-head { display:flex; align-items:center; justify-content:space-between; gap:8px; margin:12px 0 6px; color:#204257; }
.todo-source-head h3 { margin:0; font-size:15px; }
.approval-source-head span { font-size:12px; }
.pending-empty { text-align:center; }
.home-page :deep(.el-table) { --el-table-header-bg-color: #eef9fe; --el-table-header-text-color: #24323a; --el-table-row-hover-bg-color: #f0fbf6; }
.home-page :deep(.el-table th.el-table__cell) { border-bottom-color: #d9edf5; font-weight: 650; }
.home-tabs :deep(.el-tabs__header) { margin-bottom:8px; }
.filters { display:flex; flex-wrap:wrap; align-items:center; gap:8px; margin-bottom:10px; }
.filters .el-input { width:min(310px,100%); }
.filters .el-select { width:154px; }
.source-error { margin-bottom: 14px; }
.home-table { width: 100%; }
.home-page :deep(.purchase-finance-todos :is(th,td).el-table__cell > .cell){text-align:left!important}
.home-page :deep(.purchase-finance-todos :is(th,td).el-table__cell.is-right > .cell){text-align:right!important}
.home-page :deep(.purchase-finance-todos :is(th,td).el-table__cell.is-center > .cell){text-align:center!important}
.home-page :deep(.purchase-finance-todos td.el-table__cell){vertical-align:middle;padding-top:14px;padding-bottom:14px}
.home-page :deep(.purchase-finance-todos :is(th,td).el-table__cell > .cell){padding-left:14px;padding-right:14px}
.home-page :deep(.purchase-finance-todos :is(th,td).purchase-amount > .cell){padding-right:24px;white-space:nowrap;font-variant-numeric:tabular-nums}
.purchase-attachment-link{display:flex;align-items:flex-start;gap:7px;line-height:22px;color:var(--el-color-primary);text-decoration:none}
.purchase-attachment-link svg{flex-shrink:0;margin-top:2px}
.purchase-attachment-link span{min-width:0;white-space:normal;overflow-wrap:anywhere}
.purchase-attachment-link:hover span{text-decoration:underline}
.purchase-review-link{display:inline-flex;align-items:center;justify-content:center;white-space:nowrap;min-height:30px;padding:0 12px;border:1px solid var(--el-color-primary-light-5);border-radius:4px;background:var(--el-color-primary-light-9);color:var(--el-color-primary);font-size:13px;font-weight:500;text-decoration:none}
.purchase-review-link:hover{background:var(--el-color-primary);border-color:var(--el-color-primary);color:#fff}
.item-title { color: var(--el-text-color-primary); font-weight: 600; }
.unread-dot { display: inline-block; width: 7px; height: 7px; margin-right: 8px; border-radius: 50%; background: var(--el-color-primary); vertical-align: 1px; }
.item-meta, .item-summary { margin-top: 5px; color: var(--el-text-color-secondary); font-size: 13px; }
.item-summary { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.priority-reason { margin-top: 5px; color: var(--el-text-color-secondary); font-size: 12px; white-space: nowrap; }
.comment { margin-left: 8px; color: var(--el-text-color-secondary); }
.doc-link { color: var(--el-color-primary); text-decoration: none; }
.doc-link:hover { text-decoration: underline; }
.no-link { color: var(--el-text-color-placeholder); font-size: 13px; }
.pager { justify-content: flex-end; margin-top: 18px; }
@media (max-width: 680px) {
  .home-head { align-items:flex-start; flex-wrap:wrap; }
  .summary-grid { grid-template-columns:repeat(2,minmax(0,1fr)); }
}
</style>
