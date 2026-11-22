<template>
  <div class="quality-page">
    <WorkflowPageHeader :title="t('quality.title')" :description="t('quality.subtitle')">
      <template #actions><el-button v-if="canCreate" type="primary" @click="openCreate">{{ t('quality.create') }}</el-button></template>
    </WorkflowPageHeader>
    <el-card shadow="never">
      <el-radio-group v-model="tab" class="tabs" @change="reload">
        <el-radio-button value="PENDING">{{ t('quality.pending') }}</el-radio-button>
        <el-radio-button value="COMPLETED">{{ t('quality.completed') }}</el-radio-button>
      </el-radio-group>
      <div class="filters">
        <el-input v-model="keyword" clearable :placeholder="t('quality.search')" @keyup.enter="reload" @clear="reload" />
        <el-button @click="reload">{{ t('common.query') }}</el-button>
      </div>
      <el-table :data="rows" v-loading="loading">
        <el-table-column prop="taskNo" :label="t('quality.taskNo')" min-width="180"><template #default="{ row }"><b>{{ row.taskNo }}</b><div class="sub">{{ fmt(row.requestedAt) }}</div></template></el-table-column>
        <el-table-column prop="poNo" :label="t('quality.poNo')" min-width="160" />
        <el-table-column prop="supplierName" :label="t('quality.factory')" min-width="180" />
        <el-table-column prop="expectedDate" :label="t('quality.expectedDate')" width="150" />
        <el-table-column :label="t('common.status')" width="130"><template #default="{ row }"><el-tag :type="row.status === 'COMPLETED' ? 'success' : 'warning'" effect="plain">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('common.actions')" width="160" align="center"><template #default="{ row }">
          <el-dropdown trigger="click" @command="(command:string) => handleAction(command,row)">
            <el-button plain size="small">{{ t('quality.moreActions') }} ▼</el-button>
            <template #dropdown><el-dropdown-menu>
              <el-dropdown-item command="detail">{{ canWrite && row.status !== 'COMPLETED' ? t('quality.process') : t('common.detail') }}</el-dropdown-item>
              <el-dropdown-item v-if="canWrite && row.status !== 'COMPLETED'" command="edit">{{ t('quality.editBasics') }}</el-dropdown-item>
              <el-dropdown-item v-if="canDelete && row.deletable" command="delete" divided>{{ t('quality.deleteTask') }}</el-dropdown-item>
            </el-dropdown-menu></template>
          </el-dropdown>
        </template></el-table-column>
        <template #empty>{{ t('quality.empty') }}</template>
      </el-table>
      <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="100" :current-page="page" @current-change="(p:number) => { page = p; load() }" />
    </el-card>

    <el-dialog class="quality-dialog" v-model="createOpen" :title="t('quality.create')" width="min(960px, 94vw)" :close-on-click-modal="false">
      <el-form label-position="top" class="create-quality-form">
        <el-form-item :label="t('quality.poNo')" required>
          <el-input v-model="sourceKeyword" :placeholder="t('quality.sourceSearch')" clearable @keyup.enter="searchSources" @clear="searchSources"><template #append><el-button @click="searchSources">{{ t('common.query') }}</el-button></template></el-input>
        </el-form-item>
        <el-table :data="sourceOrders" v-loading="sourceLoading" highlight-current-row @row-click="selectSource" max-height="230">
          <el-table-column width="52"><template #default="{row}"><el-radio :model-value="selectedSource?.id" :value="row.id" :aria-label="row.poNo" @change="selectSource(row)"><span /></el-radio></template></el-table-column>
          <el-table-column prop="poNo" :label="t('quality.poNo')" />
          <el-table-column prop="supplierName" :label="t('quality.factory')" />
          <el-table-column width="150"><template #default="{row}"><el-button v-if="hasTask(row)" link type="primary" @click.stop="openExisting(row)">{{ t('quality.openExisting') }}</el-button></template></el-table-column>
          <template #empty>{{ t('quality.noSourceOrders') }}</template>
        </el-table>
        <el-pagination small layout="total, prev, pager, next" :total="sourceTotal" :page-size="100" v-model:current-page="sourcePage" @current-change="loadSources" />
        <div v-loading="sourceDetailLoading" class="source-detail">
          <template v-if="selectedSource">
            <p><b>{{ selectedSource.poNo }}</b> · {{ selectedSource.supplierName }}</p>
            <el-alert v-if="hasTask(selectedSource)" :title="t('quality.existingHint')" type="info" :closable="false" />
            <el-table :data="selectedSource.lines || []" max-height="230">
              <el-table-column prop="productName" :label="t('quality.product')" />
              <el-table-column prop="spec" :label="t('quality.sourceSpec')" />
              <el-table-column prop="qty" :label="t('quality.requestedQty')" width="120" />
              <el-table-column prop="uomCode" :label="t('quality.sourceUnit')" width="90" />
            </el-table>
            <div v-if="!hasTask(selectedSource)" class="create-fields">
              <el-form-item :label="t('quality.expectedDate')"><el-date-picker v-model="createForm.expectedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
              <el-form-item :label="t('quality.location')"><el-input v-model="createForm.location" /></el-form-item>
              <el-form-item :label="t('quality.contactName')"><el-input v-model="createForm.contactName" maxlength="100" /></el-form-item>
              <el-form-item :label="t('quality.contactPhone')"><el-input v-model="createForm.contactPhone" maxlength="64" /></el-form-item>
              <el-form-item class="full-width" :label="t('quality.requirements')"><el-input v-model="createForm.remark" type="textarea" :rows="3" /></el-form-item>
            </div>
          </template>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="createOpen=false">{{ t('common.cancel') }}</el-button>
        <el-button v-if="selectedSource && hasTask(selectedSource)" type="primary" @click="openExisting(selectedSource)">{{ t('quality.openExisting') }}</el-button>
        <el-button v-else type="primary" :loading="creating" :disabled="!selectedSource || sourceDetailLoading || !selectedSource.lines?.length" @click="createTask">{{ t('quality.create') }}</el-button>
      </template>
    </el-dialog>

    <el-dialog class="quality-dialog" v-model="editOpen" :title="t('quality.editBasics')" width="min(760px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!editing" :show-close="!editing">
      <el-form label-position="top" class="create-fields">
        <el-form-item :label="t('quality.poNo')"><el-input :model-value="editForm.poNo" readonly /></el-form-item>
        <el-form-item :label="t('quality.factory')"><el-input :model-value="editForm.supplierName" readonly /></el-form-item>
        <el-form-item :label="t('quality.expectedDate')"><el-date-picker v-model="editForm.expectedDate" type="date" value-format="YYYY-MM-DD" /></el-form-item>
        <el-form-item :label="t('quality.location')"><el-input v-model="editForm.location" /></el-form-item>
        <el-form-item :label="t('quality.contactName')"><el-input v-model="editForm.contactName" maxlength="100" /></el-form-item>
        <el-form-item :label="t('quality.contactPhone')"><el-input v-model="editForm.contactPhone" maxlength="64" /></el-form-item>
        <el-form-item class="full-width" :label="t('quality.requirements')"><el-input v-model="editForm.remark" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer><el-button :disabled="editing" @click="editOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="editing" @click="saveBasics">{{ t('common.save') }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="detailOpen" fullscreen :title="task?.taskNo || t('quality.title')" class="quality-dialog quality-detail-dialog">
      <template #header="{ titleId, titleClass }">
        <div class="inspection-heading">
          <h2 :id="titleId" :class="titleClass">{{ task?.taskNo || t('quality.title') }}</h2>
          <el-tag v-if="task" :type="inspectionStatusTone(task.status)" size="small" effect="light">
            {{ statusLabel(task.status) }}
          </el-tag>
        </div>
      </template>
      <template v-if="task">
        <dl class="inspection-facts">
          <div><dt>{{ t('quality.poNo') }}</dt><dd>{{ task.poNo }}</dd></div>
          <div><dt>{{ t('quality.factory') }}</dt><dd>{{ task.supplierName }}</dd></div>
          <div><dt>{{ t('quality.expectedDate') }}</dt><dd>{{ task.expectedDate || '—' }}</dd></div>
          <div><dt>{{ t('quality.location') }}</dt><dd>{{ task.inspectionLocation || '—' }}</dd></div>
          <div><dt>{{ t('quality.contactName') }}</dt><dd>{{ task.contactName || '—' }}<small v-if="task.contactPhone">{{ task.contactPhone }}</small></dd></div>
          <div><dt>{{ t('quality.requestedBy') }}</dt><dd>{{ task.requestedByName || '—' }}</dd></div>
        </dl>
        <section v-if="task.remark?.trim() || task.purchaseOrderRemark?.trim()" class="inspection-notes">
          <div v-if="task.remark?.trim()" class="inspection-request-remark">
            <h4>{{ t('quality.requirements') }}</h4>
            <p>{{ task.remark }}</p>
          </div>
          <PurchaseOrderRemark v-if="task.purchaseOrderRemark?.trim()" :remark="task.purchaseOrderRemark" />
        </section>
        <el-alert v-if="!canWrite" type="info" :closable="false" show-icon>{{ t('quality.readOnlyHint') }}</el-alert>

        <section class="panel quality-line-workspace">
          <div class="quality-line-toolbar">
            <div class="quality-line-title">
              <h3>{{ t(isRoundEntry ? 'quality.recordRound' : 'quality.products') }}</h3>
              <el-tag v-if="isRoundEntry && task.rounds.length" size="small" effect="plain">{{ t('quality.roundNo', { n: task.rounds.length + 1 }) }}</el-tag>
            </div>
            <el-input v-model="detailKeyword" size="small" clearable :aria-label="t('quality.lineSearch')" :placeholder="t('quality.lineSearch')" />
          </div>
          <el-form v-if="isRoundEntry" class="round-head" label-position="top" size="small" :disabled="saving" @submit.prevent>
            <el-form-item :label="t('quality.inspectedAt')"><el-date-picker v-model="round.inspectedAt" type="datetime" value-format="YYYY-MM-DDTHH:mm:ssZ" /></el-form-item>
            <el-form-item :label="t('quality.location')"><el-input v-model="round.location" /></el-form-item>
            <el-form-item :label="t('quality.roundRemark')"><el-input v-model="round.remark" :placeholder="t('quality.roundRemarkHint')" /></el-form-item>
          </el-form>
          <p v-if="isRoundEntry" class="round-entry-hint">{{ t('quality.roundEntryHint') }}</p>
          <el-table :key="isRoundEntry ? 'entry' : 'progress'" :data="visibleDetailLines" size="small" :class="{ 'inspection-entry-table': isRoundEntry }" row-key="id">
            <el-table-column :label="t('quality.product')" min-width="220">
              <template #default="{row}"><b>{{ row.productName }}</b><div class="sub">{{ row.spec || '—' }}</div></template>
            </el-table-column>
            <template v-if="isRoundEntry">
            <el-table-column :label="t('quality.thisBatchQty')" width="108" align="right"><template #default="{row}">{{ trim(row.unresolvedQty) }} {{ row.uomCode }}</template></el-table-column>
            <el-table-column :label="t('quality.result')" width="145"><template #default="{row}">
              <el-select v-model="roundOf[row.id].result" size="small" :disabled="saving" :aria-label="`${row.productName} ${t('quality.result')}`" @change="applyResult(row.id)">
                <el-option value="PASS" :label="resultLabel('PASS')" /><el-option value="PARTIAL" :label="resultLabel('PARTIAL')" /><el-option value="FAIL" :label="resultLabel('FAIL')" />
              </el-select>
            </template></el-table-column>
            <el-table-column :label="t('quality.inspectedQty')" width="132"><template #default="{row}"><el-input v-model="roundOf[row.id].inspectedQty" size="small" :disabled="saving" inputmode="decimal" :aria-label="`${row.productName} ${t('quality.inspectedQty')}`" @input="updateInspectedQty(row.id)" /></template></el-table-column>
            <el-table-column :label="t('quality.qualifiedQty')" width="122"><template #default="{row}"><el-input v-model="roundOf[row.id].qualifiedQty" size="small" :disabled="saving" inputmode="decimal" :aria-label="`${row.productName} ${t('quality.qualifiedQty')}`" @input="updateQualifiedQty(row.id)" /></template></el-table-column>
            <el-table-column :label="t('quality.unqualifiedQty')" width="140" align="right"><template #default="{row}">{{ roundOf[row.id].unqualifiedQty || '—' }}</template></el-table-column>
            <el-table-column :label="t('quality.issue')" width="115"><template #default="{row}">
              <el-popover v-if="roundOf[row.id].result !== 'PASS'" trigger="click" :width="360" placement="left" popper-class="quality-issue-popover">
                <el-form label-position="top" size="small">
                  <el-form-item :label="t('quality.issue')"><el-input v-model="roundOf[row.id].issueDescription" :disabled="saving" type="textarea" :rows="3" :placeholder="t('quality.issueHint')" /></el-form-item>
                  <el-form-item :label="t('quality.suggestion')"><el-input v-model="roundOf[row.id].handlingSuggestion" :disabled="saving" type="textarea" :rows="2" :placeholder="t('quality.suggestionHint')" /></el-form-item>
                </el-form>
                <template #reference><el-button link type="primary" size="small">{{ t('quality.issue') }}{{ roundOf[row.id].issueDescription || roundOf[row.id].handlingSuggestion ? ' •' : '' }}</el-button></template>
              </el-popover><span v-else class="sub">—</span>
            </template></el-table-column>
            </template>
            <template v-else>
              <el-table-column :label="t('quality.requestedQty')" width="134" align="right"><template #default="{ row }">{{ trim(row.requestedQty) }} {{ row.uomCode }}</template></el-table-column>
              <el-table-column :label="t('quality.qualifiedQty')" width="124" align="right"><template #default="{ row }">{{ trim(row.qualifiedQty) }} {{ row.uomCode }}</template></el-table-column>
              <el-table-column :label="t('quality.unresolvedQty')" width="134" align="right"><template #default="{ row }">{{ trim(row.unresolvedQty) }} {{ row.uomCode }}</template></el-table-column>
              <el-table-column :label="t('quality.result')" width="116" align="center">
                <template #default="{ row }"><el-tag size="small" effect="light" :type="inspectionResultTone(row.finalResult)">{{ resultLabel(row.finalResult) }}</el-tag></template>
              </el-table-column>
            </template>
            <template #empty>{{ t('quality.noMatchingLines') }}</template>
          </el-table>
          <div class="quality-line-footer">
            <el-pagination
              v-if="filteredDetailLines.length > 20 || detailPageSize !== 100"
              v-model:current-page="detailPage" v-model:page-size="detailPageSize" small
              :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" :total="filteredDetailLines.length"
            />
            <div v-if="isRoundEntry" class="round-submit">
              <span aria-live="polite">{{ t('quality.readyRoundLines', { n: readyLines.length }) }}</span>
              <span v-if="startedLines.length > readyLines.length" class="round-incomplete" aria-live="polite">{{ t('quality.incompleteRoundLines', { n: startedLines.length - readyLines.length }) }}</span>
              <el-button type="primary" :loading="saving" :disabled="!startedLines.length || uploading" @click="submitRound">{{ t('quality.saveRound') }}</el-button>
            </div>
          </div>
        </section>

        <section class="panel files-panel">
          <div class="section-title"><h3>{{ t('quality.attachments') }}</h3></div>
          <template v-if="canUpload">
            <div class="upload-row">
              <el-form-item :label="t('quality.category')"><el-select v-model="fileCategory" size="small" :disabled="uploading" :aria-label="t('quality.category')"><el-option v-for="c in categories" :key="c" :value="c" :label="t(`quality.categories.${c}`)" /></el-select></el-form-item>
              <input ref="fileInput" class="quality-file-input" type="file" multiple hidden aria-hidden="true" tabindex="-1" :aria-label="t('quality.chooseFiles')" :disabled="uploading" @change="pickFiles">
              <el-button size="small" :disabled="uploading" @click="fileInput?.click()">{{ t('quality.chooseFiles') }}</el-button>
              <el-button size="small" type="primary" :loading="uploading" :disabled="!picked.length || saving" @click="uploadFiles">{{ t('quality.upload') }}</el-button>
              <span class="upload-target">{{ t('quality.uploadRoundHint', { round: fileRoundLabel(activeRoundId) }) }}</span>
            </div>
            <div v-if="picked.length" class="staged-quality-files">
              <h4>{{ t('quality.pendingFiles') }}</h4>
              <el-table :data="picked" size="small" class="pending-files-table">
                <el-table-column prop="name" :label="t('quality.fileName')" min-width="280" show-overflow-tooltip/>
                <el-table-column :label="t('quality.fileSize')" width="110" align="right"><template #default="{ row }">{{ formatFileSize(row.size) }}</template></el-table-column>
                <el-table-column :label="t('common.actions')" width="95" align="center"><template #default="{ row }"><el-button link type="danger" :disabled="uploading" :aria-label="`${t('quality.removeFile')} ${row.name}`" @click="removePicked(row)">{{ t('quality.removeFile') }}</el-button></template></el-table-column>
              </el-table>
            </div>
          </template>
          <h4 v-if="picked.length && task.files?.length" class="quality-files-heading">{{ t('quality.uploadedFiles') }}</h4>
          <el-table v-if="task.files?.length" :data="task.files" size="small" class="uploaded-files-table">
            <el-table-column prop="fileName" :label="t('quality.fileName')" min-width="260" show-overflow-tooltip><template #default="{ row }"><a href="#" @click.prevent="openFile(row)">{{ row.fileName }}</a></template></el-table-column>
            <el-table-column :label="t('quality.fileSize')" width="100" align="right"><template #default="{ row }">{{ formatFileSize(Number(row.sizeBytes)) }}</template></el-table-column>
            <el-table-column :label="t('quality.category')" width="115"><template #default="{ row }">{{ row.category ? t(`quality.categories.${row.category}`) : '—' }}</template></el-table-column>
            <el-table-column :label="t('quality.fileRound')" width="150"><template #default="{ row }">{{ fileRoundLabel(row.roundId) }}</template></el-table-column>
            <el-table-column prop="uploadedByName" :label="t('quality.uploader')" width="125" />
            <el-table-column :label="t('quality.uploadedAt')" width="180"><template #default="{ row }">{{ fmt(row.uploadedAt) }}</template></el-table-column>
          </el-table>
          <p v-else-if="!picked.length" class="files-empty">{{ t('quality.noAttachments') }}</p>
        </section>

        <section class="panel history-panel">
          <div class="section-title"><h3>{{ t('quality.history') }}</h3></div>
          <el-empty v-if="!task.rounds.length" :description="t('quality.noHistory')" :image-size="72" />
          <template v-else>
            <details v-for="r in historyRounds" :key="r.id" class="inspection-history-round" :open="expandedHistory.has(String(r.id))" @toggle="toggleHistoryRound(r.id, $event)">
              <summary>
                <span class="history-toggle" aria-hidden="true"></span>
                <strong>{{ t('quality.roundNo', { n: r.roundNo }) }}</strong>
                <span>{{ r.inspectorName || '—' }}</span>
                <time :datetime="r.inspectedAt">{{ formatHistoryDate(r.inspectedAt) }}</time>
              </summary>
              <div v-if="expandedHistory.has(String(r.id))" class="inspection-history-body">
                <p v-if="r.remark">{{ r.remark }}</p>
                <el-table :data="r.lines" size="small" row-key="taskLineId">
                  <el-table-column :label="t('quality.product')" min-width="190">
                    <template #default="{ row }">
                      <b>{{ historyLine(row.taskLineId)?.productName || row.taskLineId }}</b>
                      <div v-if="historyLine(row.taskLineId)?.spec" class="sub">{{ historyLine(row.taskLineId)?.spec }}</div>
                    </template>
                  </el-table-column>
                  <el-table-column :label="t('quality.result')" width="116" align="center">
                    <template #default="{ row }"><el-tag size="small" effect="light" :type="inspectionResultTone(row.result)">{{ resultLabel(row.result) }}</el-tag></template>
                  </el-table-column>
                  <el-table-column :label="t('quality.inspectedQty')" width="124" align="right">
                    <template #default="{ row }">{{ historyQuantity(row.inspectedQty, row.taskLineId) }}</template>
                  </el-table-column>
                  <el-table-column :label="t('quality.qualifiedQty')" width="112" align="right">
                    <template #default="{ row }">{{ historyQuantity(row.qualifiedQty, row.taskLineId) }}</template>
                  </el-table-column>
                  <el-table-column :label="t('quality.historyUnqualifiedQty')" width="115" align="right">
                    <template #default="{ row }">{{ historyQuantity(row.unqualifiedQty, row.taskLineId) }}</template>
                  </el-table-column>
                  <el-table-column v-if="r.lines.some(line => line.issueDescription || line.handlingSuggestion)" :label="t('quality.issue')" min-width="175">
                    <template #default="{ row }">
                      <div v-if="row.issueDescription" class="history-note">{{ row.issueDescription }}</div>
                      <div v-if="row.handlingSuggestion" class="history-note sub">{{ t('quality.suggestion') }} · {{ row.handlingSuggestion }}</div>
                      <span v-if="!row.issueDescription && !row.handlingSuggestion">—</span>
                    </template>
                  </el-table-column>
                </el-table>
              </div>
            </details>
          </template>
        </section>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { get, post, patch, del } from '../api'
import { formatFileSize } from '../lib/pendingUploads'
import { qualityFileRound } from '../lib/qualityFiles'
import { qualityStatusKey } from '../lib/qualityStatus'
import { useAuthStore } from '../stores/auth'
import WorkflowPageHeader from '../components/WorkflowPageHeader.vue'
import PurchaseOrderRemark from '../components/PurchaseOrderRemark.vue'
import { relayFailureReason, saveViaRelay, uploadViaRelay } from '../lib/storageRelay'
type QLine={id:string;productName:string;spec:string;uomCode:string;requestedQty:string;qualifiedQty:string;unresolvedQty:string;finalResult:string;approvedReleaseQty:string}
type RoundLine={taskLineId:string;result:string;inspectedQty:string;qualifiedQty:string;unqualifiedQty:string;issueDescription:string;handlingSuggestion?:string}
type RoundEntry = {
  result: '' | 'PASS' | 'PARTIAL' | 'FAIL'
  initialInspectedQty: string
  inspectedQty: string
  qualifiedQty: string
  unqualifiedQty: string
  issueDescription: string
  handlingSuggestion: string
}
type QTask={id:string;deletable:boolean;taskNo:string;poNo:string;supplierName:string;batchNo:number;status:string;expectedDate:string;inspectionLocation:string;contactName:string;contactPhone:string;remark:string;purchaseOrderRemark?:string;requestedByName:string;requestedAt:string;lines:QLine[];rounds:{id:string;roundNo:number;inspectedAt:string;inspectorName:string;remark:string;lines:RoundLine[]}[];files:any[]}
const { t, locale }=useI18n(); const route=useRoute(); const auth=useAuthStore(); const canWrite=auth.can('quality:task:write'); const canUpload=auth.can('quality:file:upload')
type SourceOrder={id:string;poNo:string;supplierName:string;existingTaskId:string;lines?:{productName:string;spec:string;uomCode:string;qty:string}[]}
const canCreate=auth.can('quality:task:read') && auth.can('quality:task:write')
const createOpen=ref(false), creating=ref(false), sourceLoading=ref(false), sourceDetailLoading=ref(false)
const sourceKeyword=ref(''), sourcePage=ref(1), sourceTotal=ref(0), sourceOrders=ref<SourceOrder[]>([]), selectedSource=ref<SourceOrder|null>(null)
const createForm=reactive({expectedDate:'',location:'',contactName:'',contactPhone:'',remark:''})
let sourceRequest=0, detailRequest=0
const hasTask=(o:SourceOrder)=>!!o.existingTaskId && o.existingTaskId!=='0'
async function openCreate(){if(uploading.value){ElMessage.info(t('quality.uploadInProgress'));return}createOpen.value=true;selectedSource.value=null;sourceDetailLoading.value=false;++detailRequest;sourceKeyword.value='';sourcePage.value=1;Object.assign(createForm,{expectedDate:'',location:'',contactName:'',contactPhone:'',remark:''});await loadSources()}
async function searchSources(){sourcePage.value=1;await loadSources()}
async function loadSources(){const request=++sourceRequest;sourceLoading.value=true;try{const d=await get<{orders:SourceOrder[];meta:{total:number}}>('/quality/source-orders',{keyword:sourceKeyword.value,page:sourcePage.value,page_size: 100});if(request!==sourceRequest)return;sourceOrders.value=d.orders||[];sourceTotal.value=Number(d.meta?.total||0)}finally{if(request===sourceRequest)sourceLoading.value=false}}
async function selectSource(row:SourceOrder){const request=++detailRequest;Object.assign(createForm,{expectedDate:'',location:'',contactName:'',contactPhone:'',remark:''});selectedSource.value=null;sourceDetailLoading.value=true;try{const d=await get<{orders:SourceOrder[]}>(`/quality/source-orders/${row.id}`);if(request===detailRequest)selectedSource.value=d.orders[0]||null}finally{if(request===detailRequest)sourceDetailLoading.value=false}}
async function openExisting(row:SourceOrder){await openTask({id:row.existingTaskId});createOpen.value=false}
async function createTask(){if(!selectedSource.value||creating.value)return;creating.value=true;try{const d=await post<{task:QTask}>('/quality/tasks',{po_id:Number(selectedSource.value.id),expected_date:createForm.expectedDate||'',inspection_location:createForm.location,contact_name:createForm.contactName,contact_phone:createForm.contactPhone,remark:createForm.remark});createOpen.value=false;openDetail(d.task);tab.value='PENDING';page.value=1;await load();ElMessage.success(t('quality.created'))}finally{creating.value=false}}
const canDelete=ref(false), editOpen=ref(false), editing=ref(false)
const editForm=reactive({id:'',poNo:'',supplierName:'',expectedDate:'',location:'',contactName:'',contactPhone:'',remark:''})
async function handleAction(command:string,row:QTask){
  if(command==='detail'){await openTask(row);return}
  if(command==='edit'){
    const {task:q}=await get<{task:QTask}>(`/quality/tasks/${row.id}`)
    if(q.status==='COMPLETED'){await load();return}
    Object.assign(editForm,{id:q.id,poNo:q.poNo,supplierName:q.supplierName,expectedDate:q.expectedDate||'',location:q.inspectionLocation||'',contactName:q.contactName||'',contactPhone:q.contactPhone||'',remark:q.remark||''})
    editOpen.value=true;return
  }
  if(command==='delete'){
    try{await ElMessageBox.confirm(t('quality.deleteConfirm'),t('quality.deleteTask'),{type:'warning',confirmButtonText:t('quality.deleteTask'),cancelButtonText:t('common.cancel')})}catch{return}
    await del(`/quality/tasks/${row.id}`)
    if(task.value?.id===row.id){detailOpen.value=false;task.value=null}
    if(rows.value.length===1&&page.value>1)page.value--
    await load();ElMessage.success(t('quality.deleted'))
  }
}
async function saveBasics(){
  if(editing.value)return;editing.value=true
  try{
    const {task:q}=await patch<{task:QTask}>(`/quality/tasks/${editForm.id}/basics`,{expected_date:editForm.expectedDate||'',inspection_location:editForm.location,contact_name:editForm.contactName,contact_phone:editForm.contactPhone,remark:editForm.remark})
    if(task.value?.id===q.id) syncTaskDetail(q)
    editOpen.value=false;await load();ElMessage.success(t('quality.basicsSaved'))
  }finally{editing.value=false}
}
const tab=ref('PENDING'), keyword=ref(''), page=ref(1), total=ref(0), loading=ref(false), saving=ref(false), uploading=ref(false), detailOpen=ref(false); const rows=ref<QTask[]>([]), task=ref<QTask|null>(null)
const round=reactive({inspectedAt:'',location:'',remark:''}); const roundOf=reactive<Record<string,RoundEntry>>({}); const categories=['PHOTO','VIDEO','REPORT','THIRD_PARTY','OTHER']; const fileCategory=ref('PHOTO'); const picked=ref<File[]>([]); const fileInput=ref<HTMLInputElement|null>(null)
const unresolvedLines=computed(()=>task.value?.lines.filter(l=>Number(l.unresolvedQty)>0)||[])
const detailKeyword = ref('')
const detailPage = ref(1)
const detailPageSize = ref(100)
const isRoundEntry = computed(() => canWrite && Boolean(task.value) && task.value?.status !== 'COMPLETED' && unresolvedLines.value.length > 0)
const filteredDetailLines = computed(() => {
  const lines = isRoundEntry.value ? unresolvedLines.value : task.value?.lines ?? []
  const words = detailKeyword.value.toLowerCase().trim().split(/\s+/).filter(Boolean)
  return lines.filter(line => {
    const text = `${line.productName} ${line.spec ?? ''}`.toLowerCase()
    return words.every(word => text.includes(word))
  })
})
const visibleDetailLines = computed(() => {
  const offset = (detailPage.value - 1) * detailPageSize.value
  return filteredDetailLines.value.slice(offset, offset + detailPageSize.value)
})
const startedLines = computed(() => unresolvedLines.value.filter(line => hasStartedRoundEntry(roundOf[line.id])))
const readyLines = computed(() => startedLines.value.filter(isRoundLineValid))
const taskLinesById = computed(() => new Map(task.value?.lines.map(line => [String(line.id), line]) ?? []))
const historyRounds = computed(() => [...(task.value?.rounds ?? [])].reverse())
const expandedHistory = reactive(new Set<string>())
watch([isRoundEntry, detailKeyword, detailPageSize], () => { detailPage.value = 1 })
watch(filteredDetailLines, lines => {
  detailPage.value = Math.min(detailPage.value, Math.max(1, Math.ceil(lines.length / detailPageSize.value)))
})
const fmt=(v:string)=>v?new Date(v).toLocaleString():'—'; const trim=(v:string)=>v?.includes('.')?v.replace(/0+$/, '').replace(/\.$/, ''):v||'0'; const statusLabel=(s?:string)=>{const key=qualityStatusKey(s);return key?t(key):'—'}; const resultLabel=(s?:string)=>t(`quality.results.${s||'PENDING'}`)
const historyLine = (id: string) => taskLinesById.value.get(String(id))
function historyQuantity(value: string | undefined, id: string) {
  if (!value) return '—'
  const unit = historyLine(id)?.uomCode
  return unit ? `${trim(value)} ${unit}` : trim(value)
}
function formatHistoryDate(value: string) {
  return value ? new Date(value).toLocaleString(locale.value, { dateStyle: 'medium', timeStyle: 'short' }) : '—'
}

function toggleHistoryRound(id: string, event: Event) {
  const key = String(id)
  if ((event.currentTarget as HTMLDetailsElement).open) expandedHistory.add(key)
  else expandedHistory.delete(key)
}
function inspectionStatusTone(status: string) {
  if (status === 'COMPLETED') return 'success'
  if (status === 'IN_PROGRESS') return 'primary'
  return 'warning'
}

function inspectionResultTone(result: string) {
  if (result === 'PASS') return 'success'
  if (result === 'PARTIAL') return 'warning'
  if (result === 'FAIL') return 'danger'
  return 'info'
}
function resetRoundHeader() {
  round.inspectedAt = new Date().toISOString()
  round.location = task.value?.inspectionLocation ?? ''
  round.remark = ''
}

function hasStartedRoundEntry(entry?: RoundEntry) {
  return Boolean(entry && (entry.result || entry.qualifiedQty.trim() || entry.issueDescription.trim() ||
    entry.handlingSuggestion.trim() || entry.inspectedQty !== entry.initialInspectedQty))
}

function syncTaskDetail(nextTask: QTask, resetHistory = false) {
  if (resetHistory) expandedHistory.clear()
  const latestRound = nextTask.rounds.at(-1)?.id
  if (latestRound && (resetHistory || latestRound !== task.value?.rounds.at(-1)?.id)) expandedHistory.add(String(latestRound))
  task.value = nextTask
  const remaining = new Set(unresolvedLines.value.map(line => line.id))
  for (const id of Object.keys(roundOf)) {
    if (!remaining.has(id)) delete roundOf[id]
  }
  for (const line of unresolvedLines.value) {
    if (!hasStartedRoundEntry(roundOf[line.id])) {
      const quantity = trim(line.unresolvedQty)
      roundOf[line.id] = {
        result: '', initialInspectedQty: quantity, inspectedQty: quantity, qualifiedQty: '', unqualifiedQty: '',
        issueDescription: '', handlingSuggestion: '',
      }
    }
  }
}

function openDetail(nextTask: QTask) {
  for (const id of Object.keys(roundOf)) delete roundOf[id]
  syncTaskDetail(nextTask, true)
  detailKeyword.value = ''
  detailPage.value = 1
  resetRoundHeader()
  resetPicked()
  detailOpen.value = true
}
async function load(){loading.value=true;try{const d=await get<{tasks:QTask[];meta:{total:number}}>('/quality/tasks',{tab:tab.value,page:page.value,page_size: 100,keyword:keyword.value});rows.value=d.tasks||[];total.value=Number(d.meta?.total||0)}finally{loading.value=false}}
function reload(){page.value=1;void load()}
async function openTask(row:Pick<QTask,'id'>){if(uploading.value){ElMessage.info(t('quality.uploadInProgress'));return}if(saving.value)return;const d=await get<{task:QTask}>(`/quality/tasks/${row.id}`);openDetail(d.task)}
async function openRequestedTask(){const id=String(route.query.task||'');if(id)await openTask({id})}
async function refresh() {
  if (!task.value) return
  const id = task.value.id
  const { task: latest } = await get<{task: QTask}>(`/quality/tasks/${id}`)
  if (task.value?.id === id) syncTaskDetail(latest)
  await load()
}

// Inspection quantities are stored with four decimal places. Calculate in
// integer units so fractional results match the server's exact validation.
function quantityUnits(value: string) {
  const match = /^\+?(\d*)(?:\.(\d*))?$/.exec(value.trim())
  if (!match || !(match[1] || match[2])) return null
  const fraction = (match[2] ?? '').replace(/0+$/, '')
  if (fraction.length > 4) return null
  return BigInt((match[1] || '0') + fraction.padEnd(4, '0'))
}

function updateRejectedQuantity(entry: RoundEntry) {
  const inspected = quantityUnits(entry.inspectedQty)
  const qualified = quantityUnits(entry.qualifiedQty)
  if (inspected === null || qualified === null || qualified > inspected) {
    entry.unqualifiedQty = ''
    return
  }
  const units = String(inspected - qualified).padStart(5, '0')
  entry.unqualifiedQty = trim(`${units.slice(0, -4)}.${units.slice(-4)}`)
}

function applyResult(id: string) {
  const entry = roundOf[id]
  if (entry.result === 'PASS') entry.qualifiedQty = entry.inspectedQty
  else if (entry.result === 'FAIL') entry.qualifiedQty = '0'
  else {
    const qualified = quantityUnits(entry.qualifiedQty)
    const inspected = quantityUnits(entry.inspectedQty)
    if (qualified === null || inspected === null || qualified === BigInt(0) || qualified >= inspected) entry.qualifiedQty = ''
  }
  updateRejectedQuantity(entry)
}

function updateInspectedQty(id: string) {
  const entry = roundOf[id]
  if (entry.result === 'PASS') entry.qualifiedQty = entry.inspectedQty
  updateRejectedQuantity(entry)
}

function updateQualifiedQty(id: string) {
  const entry = roundOf[id]
  const inspected = quantityUnits(entry.inspectedQty)
  const qualified = quantityUnits(entry.qualifiedQty)
  if (inspected !== null && inspected > BigInt(0) && qualified !== null && qualified <= inspected) {
    entry.result = qualified === BigInt(0) ? 'FAIL' : qualified === inspected ? 'PASS' : 'PARTIAL'
  }
  updateRejectedQuantity(entry)
}

function isRoundLineValid(line: QLine) {
  const entry = roundOf[line.id]
  const inspected = quantityUnits(entry.inspectedQty)
  const remaining = quantityUnits(line.unresolvedQty)
  const qualified = quantityUnits(entry.qualifiedQty)
  if (inspected === null || remaining === null || qualified === null || inspected === BigInt(0) || inspected > remaining || qualified > inspected) return false
  const result = qualified === BigInt(0) ? 'FAIL' : qualified === inspected ? 'PASS' : 'PARTIAL'
  return entry.result === result
}

async function submitRound() {
  const current = task.value
  const started = startedLines.value
  if (!current || !isRoundEntry.value || saving.value || uploading.value || !started.length) return
  const invalid = started.find(line => !isRoundLineValid(line))
  if (invalid) {
    ElMessage.warning(t('quality.invalidRoundLine', { product: invalid.productName }))
    return
  }
  const selected = readyLines.value
  saving.value = true
  try {
    const {task: saved} = await post<{task: QTask}>(`/quality/tasks/${current.id}/rounds`, {
      inspected_at: round.inspectedAt || new Date().toISOString(),
      inspection_location: round.location || current.inspectionLocation,
      remark: round.remark,
      lines: selected.map(line => {
        const entry = roundOf[line.id]
        return {
          task_line_id: Number(line.id), result: entry.result, inspected_qty: entry.inspectedQty,
          qualified_qty: entry.qualifiedQty, unqualified_qty: entry.unqualifiedQty,
          issue_description: entry.issueDescription, handling_suggestion: entry.handlingSuggestion,
        }
      }),
    })
    if (task.value?.id === current.id) {
      for (const line of selected) delete roundOf[line.id]
      syncTaskDetail(saved)
      resetRoundHeader()
    }
    ElMessage.success(t('quality.roundSaved'))
    await load()
  } finally {
    saving.value = false
  }
}
const activeRoundId=computed(()=>String(task.value?.rounds.at(-1)?.id||'0'))
function fileRoundLabel(roundId?:string|number){const label=qualityFileRound(roundId,task.value?.rounds||[]);return t(label.key,label.params||{})}
function resetPicked(){picked.value=[];if(fileInput.value)fileInput.value.value=''}
function pickFiles(e:Event){const input=e.target as HTMLInputElement;const files=Array.from(input.files||[]);if(files.length)picked.value=files;input.value=''}
function removePicked(file:File){if(!uploading.value)picked.value=picked.value.filter(item=>item!==file)}
watch(detailOpen,open=>{if(!open&&!uploading.value)resetPicked()})
// 资料经网关中转进存储（lib/storageRelay.ts）——国内直连存储会连接超时。传成功一份就从待传
// 列表里拿掉一份：中途失败时再点只补传剩下的；失败原因连着文件名一起说，以前这里什么都不提示。
async function uploadFiles(){
 if(!task.value||!picked.value.length||uploading.value||saving.value)return
 const target=task.value,files=[...picked.value],category=fileCategory.value,latestRound=Number(target.rounds.at(-1)?.id||0)
 uploading.value=true
 try{
  let failed=false
  for(const file of files){
   try{
    const p=await post<{fileKey:string;uploadUrl:string}>(`/quality/tasks/${target.id}/files/presign`,{file_name:file.name})
    await uploadViaRelay(p.uploadUrl,file)
    await post(`/quality/tasks/${target.id}/files`,{file_key:p.fileKey,file_name:file.name,content_type:file.type,size_bytes:file.size,category,round_id:latestRound,supplemental:target.status==='COMPLETED'})
    picked.value=picked.value.filter(item=>item!==file)
   }catch(err){ElMessage.error({message:`${t('quality.uploadFailed')}：${file.name}（${relayFailureReason(err)||t('common.networkError')}）`,duration:8000});failed=true;break}
  }
  if(!failed)ElMessage.success(t('quality.uploaded'))
  await refresh()
 }finally{uploading.value=false}
}
async function openFile(row:{downloadUrl:string;fileName:string}){try{await saveViaRelay(row.downloadUrl,row.fileName)}catch(err){ElMessage.error(`${t('quality.openFailed')}：${relayFailureReason(err)||t('common.networkError')}`)}}
onMounted(async()=>{await load();await openRequestedTask();const access=await get<{canDelete:boolean}>('/quality/access');canDelete.value=access.canDelete})
watch(()=>route.query.task,()=>{void openRequestedTask()})
</script>

<style scoped>
.create-quality-form { max-height: 68vh; overflow: auto; padding-right: 8px; }
.create-fields { display: grid; grid-template-columns: 1fr 1fr; gap: 0 20px; margin-top: 18px; }
.create-fields .full-width { grid-column: 1 / -1; }
.source-detail { min-height: 30px; }
@media (max-width: 640px) { .create-fields { grid-template-columns: 1fr; } }

.tabs { margin-bottom: 14px; }
.filters { display: flex; gap: 10px; margin-bottom: 14px; }
.filters .el-input { max-width: 420px; }
.pager { justify-content: flex-end; margin-top: 14px; }
.sub { font-size: 12px; color: #627488; margin-top: 4px; }
.section-title { display: flex; justify-content: space-between; align-items: flex-start; gap: 12px; }
.section-title h3 { margin: 0; color: #163449; }
.section-title p { margin: 5px 0 0; color: #536780; }
.round-head { display: grid; grid-template-columns: minmax(220px, 1fr) minmax(160px, 1fr) minmax(220px, 1.4fr); gap: 10px; }
.quality-line-toolbar,
.quality-line-title,
.quality-line-footer,
.round-submit {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px 14px;
}
.quality-line-toolbar { justify-content: space-between; margin-bottom: 10px; }
.quality-line-toolbar h3 { margin: 0; color: var(--inspection-ink); font-size: 14px; }
.quality-line-toolbar .el-input { width: min(300px, 100%); }
.quality-line-footer { justify-content: space-between; margin-top: 10px; }
.quality-line-footer:empty { display: none; }
.round-submit { margin-inline-start: auto; }
.round-submit span, .round-entry-hint { color: var(--inspection-muted); font-size: 12px; }
.round-submit .round-incomplete { color: #8b4d19; }
.round-entry-hint { margin: 0 0 6px; }
.history-panel :deep(.el-empty) { padding: 14px 0; }

/* Compact typography is limited to the Quality page and its teleported dialogs. */
.quality-page, .quality-dialog { font-size: 13px; --el-font-size-base: 13px; --el-font-size-small: 12px; }
.quality-page :deep(.el-table), .quality-dialog :deep(.el-table),
.quality-dialog :deep(.el-form-item__label), .quality-dialog :deep(.el-input__inner),
.quality-dialog :deep(.el-textarea__inner), .quality-dialog :deep(.el-select__wrapper),
.quality-page :deep(.el-button), .quality-dialog :deep(.el-button) { font-size: 13px; }
.quality-page :deep(.procurement-page-header h1) { font-size: 20px; }
.quality-dialog :deep(.el-dialog__title) { font-size: 16px; }
.quality-dialog :deep(.el-table .cell) { line-height: 19px; padding: 0 8px; }
.quality-dialog :deep(.el-table__cell) { padding: 6px 0; }
.quality-dialog .sub { margin-top: 2px; font-size: 11px; }
.panel { padding-top: 12px; margin-top: 14px; border-top: 1px solid var(--inspection-divider); }
.section-title { margin-bottom: 9px; }
.section-title h3 { font-size: 14px; }
.section-title p { font-size: 12px; margin-top: 3px; }
:global(.quality-detail-dialog.el-dialog) {
  --inspection-gutter: max(24px, calc((100% - 1440px) / 2));
  --inspection-divider: #dce6ee;
  --inspection-ink: #14334a;
  --inspection-muted: #51667b;
  --el-color-primary: #176c91;
  --el-color-primary-light-3: #3786a8;
  --el-color-primary-dark-2: #105674;
  padding: 0;
}
:global(.quality-detail-dialog .el-dialog__header.show-close) {
  position: sticky;
  top: 0;
  z-index: 3;
  padding: 12px calc(var(--inspection-gutter) + 44px) 12px var(--inspection-gutter);
  border-bottom: 1px solid var(--inspection-divider);
  background: #edf5fa;
}
:global(.quality-detail-dialog .el-dialog__headerbtn) {
  top: 50%;
  right: max(4px, calc(var(--inspection-gutter) - 12px));
  transform: translateY(-50%);
}
:global(.quality-detail-dialog .el-dialog__body) {
  padding: 14px var(--inspection-gutter) 24px;
  color: var(--inspection-ink);
  font-size: 13px;
}
.inspection-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
}
.inspection-heading h2.el-dialog__title {
  margin: 0;
  color: var(--inspection-ink);
  font-size: 18px;
  font-weight: 650;
}
.inspection-facts {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px 20px;
  margin: 0;
  padding-bottom: 12px;
}
.inspection-facts dt {
  font-size: 12px;
  color: var(--inspection-muted);
}
.inspection-facts dd {
  margin: 3px 0 0;
  font-weight: 600;
  line-height: 1.35;
  overflow-wrap: anywhere;
}
.inspection-facts small {
  display: block;
  margin-top: 2px;
  color: var(--inspection-muted);
  font-weight: 400;
}
.inspection-notes {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 320px), 1fr));
  gap: 10px 24px;
  margin-bottom: 12px;
  padding-top: 10px;
  border-top: 1px solid var(--inspection-divider);
}
.inspection-notes :deep(.purchase-order-remark) {
  margin: 0;
  padding: 0;
  border: 0;
  background: transparent;
}
.inspection-request-remark h4,
.inspection-notes :deep(.purchase-order-remark h4) {
  margin: 0 0 3px;
  color: var(--inspection-ink);
  font-size: 13px;
}
.inspection-request-remark p {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  line-height: 1.55;
}
.inspection-notes :deep(.purchase-order-remark small) { display: none; }
:global(.quality-detail-dialog .el-table) {
  --el-table-header-bg-color: #eef4f8;
  --el-table-header-text-color: #314e65;
  --el-table-text-color: #284157;
  --el-table-border-color: var(--inspection-divider);
  --el-table-row-hover-bg-color: #f5f9fc;
  font-variant-numeric: tabular-nums;
}
:global(.quality-detail-dialog .el-table__cell) { padding: 6px 0; }
:global(.quality-detail-dialog .el-table .cell) { padding: 0 8px; font-size: 13px; line-height: 19px; }
.round-head :deep(.el-form-item) { margin-bottom: 8px; }
.round-head :deep(.el-date-editor) { width: 100%; }
.inspection-history-round {
  border-bottom: 1px solid var(--inspection-divider);
}
.inspection-history-round summary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
  padding: 9px 6px;
  color: var(--inspection-ink);
  cursor: pointer;
}
.inspection-history-round summary::-webkit-details-marker { display: none; }
.inspection-history-round summary:hover,
.inspection-history-round[open] summary { background: #f5f9fc; }
.inspection-history-round summary time {
  margin-inline-start: auto;
  color: var(--inspection-muted);
  font-size: 12px;
}
.history-toggle {
  width: 6px;
  height: 6px;
  margin-inline-end: 2px;
  border: solid var(--inspection-muted);
  border-width: 0 1.5px 1.5px 0;
  transform: rotate(-45deg);
}
.inspection-history-round[open] .history-toggle { transform: rotate(45deg); }
.inspection-history-body { padding: 0 0 8px; }
.inspection-history-body p {
  margin: 6px 8px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.history-note { white-space: pre-wrap; overflow-wrap: anywhere; }
@media (max-width: 760px) {
  :global(.quality-detail-dialog.el-dialog) { --inspection-gutter: 16px; }
  .inspection-facts, .round-head { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .round-head > :last-child { grid-column: 1 / -1; }
}
@media (max-width: 460px) {
  .round-head { grid-template-columns: minmax(0, 1fr); }
}
.inspection-entry-table :deep(.el-input__inner) { text-align: right; }
.inspection-entry-table :deep(.el-input__wrapper), .inspection-entry-table :deep(.el-select__wrapper) { min-height: 26px; }
.upload-row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; margin-bottom: 8px; }
.upload-row .el-form-item { margin: 0; }
.upload-row :deep(.el-select) { width: 155px; }
.upload-row :deep(.el-button) { margin-left: 0; }
.quality-file-input{position:absolute;width:1px;height:1px;overflow:hidden;opacity:0;pointer-events:none}
.upload-target, .files-empty { color: var(--inspection-muted); font-size: 12px; }
.upload-target { margin-inline-start: auto; }
.files-empty { margin: 8px 0 0; }
.staged-quality-files { margin: 10px 0; }
.staged-quality-files h4, .quality-files-heading { margin: 8px 0 5px; font-size: 13px; }

</style>

<style>
.quality-issue-popover { max-width: calc(100vw - 32px); font-size: 13px; }
.quality-issue-popover .el-form-item__label, .quality-issue-popover .el-textarea__inner { font-size: 13px; }
.quality-issue-popover .el-form-item:last-child { margin-bottom: 0; }
</style>
