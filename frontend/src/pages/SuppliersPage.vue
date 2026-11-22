<template>
  <div class="supplier-page">
    <el-card class="list-card" shadow="never">
      <div class="supplier-toolbar">
        <div class="supplier-heading">
          <h1>{{ t('suppliers.suppliers') }}</h1><span>{{ total }}</span>
        </div>
        <div class="filters">
          <el-input v-model="keyword" size="small" clearable :aria-label="t('suppliers.search')" :placeholder="t('suppliers.searchShort')" @keyup.enter="resetLoad" />
          <el-button size="small" @click="resetLoad">{{ t('common.query') }}</el-button>
          <el-select v-model="countryCode" size="small" class="country-select" :placeholder="t('suppliers.allCountries')" filterable :aria-label="t('suppliers.countryGroups')" @change="resetLoad">
            <el-option :label="t('suppliers.allCountries') + '（' + countryTotal + '）'" value="" />
            <el-option v-for="group in countryChoiceList" :key="group.value" :label="countryLabel(group.code) + '（' + group.count + '）'" :value="group.value" />
          </el-select>
          <el-popover trigger="click" placement="bottom-start" :width="300">
            <template #reference>
              <el-button size="small">{{ t('suppliers.moreFilters') }}<span v-if="filterCount"> ({{ filterCount }})</span><span class="caret">▼</span></el-button>
            </template>
            <el-form label-position="top" size="small">
              <el-form-item :label="t('suppliers.businessType')">
                <el-select v-model="businessType" :teleported="false" clearable @change="resetLoad">
                  <el-option v-for="option in businessOptions" :key="option.code" :label="optionLabel(option.code)" :value="option.code" />
                </el-select>
              </el-form-item>
              <el-form-item :label="t('common.status')">
                <el-select v-model="status" :teleported="false" :placeholder="t('common.active')" @change="resetLoad">
                  <el-option :label="t('common.active')" value="" />
                  <el-option :label="t('common.inactive')" value="INACTIVE" />
                  <el-option :label="t('suppliers.allStatuses')" value="ALL" />
                </el-select>
              </el-form-item>
            </el-form>
          </el-popover>
        </div>
        <div class="filter-actions">
          <template v-if="auth.can('masterdata:supplier:write')">
            <el-button size="small" @click="importOpen = true">{{ t('suppliers.bulkImport') }}</el-button>
            <el-button size="small" type="primary" @click="dialogOpen = true">{{ t('suppliers.create') }}</el-button>
          </template>
          <MasterDataBulkDelete entity="SUPPLIER" @deleted="bulkDeleteSaved" />
        </div>
      </div>
      <div v-if="canManageOwners && selectedSuppliers.length" class="bulk-owner-bar">
        <span>已选择 <strong>{{ selectedSuppliers.length }}</strong> 条</span>
        <el-button size="small" type="primary" @click="openBulkOwners('ADD')">批量添加负责人</el-button>
        <el-button size="small" @click="openBulkOwners('REMOVE')">批量移除负责人</el-button>
        <el-button size="small" link @click="clearSelection">取消选择</el-button>
      </div>
      <el-table ref="supplierTable" size="small" stripe :data="rows" v-loading="loading" @selection-change="selectedSuppliers=$event">
        <el-table-column v-if="canManageOwners" type="selection" width="48" class-name="supplier-selection-column" label-class-name="supplier-selection-column" />
        <el-table-column :label="t('suppliers.name')" min-width="195">
          <template #header>
            <span class="supplier-name-heading">{{ t('suppliers.name') }}</span>
          </template>
          <template #default="{ row }">
            <button class="supplier-identity" type="button" :title="supplierNames(row)" @click="openDetail(row)">
              <strong>{{ displayName(row) }}</strong>
              <span>{{ row.code }}</span>
            </button>
          </template>
        </el-table-column>
        <el-table-column :label="t('suppliers.country')" width="122"><template #default="{row}">{{ row.countryCode ? countryName(row.countryCode, locale) : '—' }}</template></el-table-column>
        <el-table-column :label="t('suppliers.businessType')" min-width="175"><template #default="{row}"><el-tag v-for="v in row.businessTypes" :key="v" size="small" effect="plain">{{ optionLabel(v) }}</el-tag></template></el-table-column>
        <el-table-column :label="t('suppliers.owners')" min-width="240" prop="ownerNames" />
        <el-table-column :label="t('common.status')" width="74"><template #default="{row}"><el-tag :type="row.status==='ACTIVE'?'success':'info'" size="small">{{ row.status==='ACTIVE'?t('common.active'):t('common.inactive') }}</el-tag></template></el-table-column>
        <el-table-column :label="t('common.actions')" width="108" fixed="right"><template #default="{row}">
          <el-dropdown trigger="click" @command="(command:string)=>handleAction(command,row)">
            <el-button size="small" link type="primary">更多操作 <span class="caret">▼</span></el-button>
            <template #dropdown><el-dropdown-menu>
              <el-dropdown-item command="view">{{ t('suppliers.view') }}</el-dropdown-item>
              <el-dropdown-item v-if="canDelete" command="delete" divided class="danger-item">删除</el-dropdown-item>
            </el-dropdown-menu></template>
          </el-dropdown>
        </template></el-table-column>
      </el-table>
      <el-pagination class="pager" layout="total, sizes, prev, pager, next" :total="total" :page-size="pageSize" :page-sizes="[20,50,100]" :current-page="page" @size-change="changePageSize" @current-change="changePage" />
    </el-card>
    <CreateSupplierDialog v-if="auth.can('masterdata:supplier:write')" v-model:open="dialogOpen" :can-manage-owners="canManageOwners" :employee-id="String(auth.employeeId)" :business-options="businessOptions" :payment-options="paymentOptions" :role-label="optionLabel" @created="refreshAll" />
    <SupplierImportDialog v-model="importOpen" @imported="refreshAll" />
    <BulkOwnerDialog v-model:open="bulkOwnerOpen" entity-type="supplier" :action="bulkOwnerAction" :selected-ids="selectedSuppliers.map(row => row.id)" @saved="bulkOwnersSaved" />
  </div>
</template>

<script setup lang="ts">
import MasterDataBulkDelete from '../components/MasterDataBulkDelete.vue'
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { del, get } from '../api'
import CreateSupplierDialog from '../components/CreateSupplierDialog.vue'
import SupplierImportDialog from '../components/SupplierImportDialog.vue'
import BulkOwnerDialog from '../components/masterdata/BulkOwnerDialog.vue'
import { countryName } from '../lib/countries'
import { businessRoleLabel } from '../lib/masterDataDisplay'
import { masterDataListQuery, queryPage, queryText } from '../lib/masterDataListQuery'
import { countryChoices, totalInChoices, withSelectedChoice } from '../lib/supplierCountryFilter'
import { useAuthStore } from '../stores/auth'

interface Supplier { id:string;code:string;name:string;nameZh:string;nameEn:string;shortName:string;countryCode:string;businessTypes:string[];ownerNames:string;status:string }
interface CountryGroup { countryCode:string;count:string }
interface OptionItem { code:string;label:string }
const { t, locale } = useI18n(); const route=useRoute(); const router=useRouter(); const auth=useAuthStore()
const rows=ref<Supplier[]>([]), countries=ref<CountryGroup[]>([]), businessOptions=ref<OptionItem[]>([]), paymentOptions=ref<OptionItem[]>([])
const keyword=ref(queryText(route.query.keyword)), countryCode=ref(queryText(route.query.country)), businessType=ref(queryText(route.query.business_type)), status=ref(queryText(route.query.status)), loading=ref(false), dialogOpen=ref(false), importOpen=ref(false)
const canDelete=ref(false),canManageOwners=ref(false)
const page=ref(queryPage(route.query.page)), pageSize=ref(100), total=ref(0)
const supplierTable=ref<any>(),selectedSuppliers=ref<Supplier[]>([]),bulkOwnerOpen=ref(false),bulkOwnerAction=ref<'ADD'|'REMOVE'>('ADD')
const filterCount = computed(() => Number(Boolean(businessType.value)) + Number(Boolean(status.value)))
// 「国家/地区」下拉：没填国家的那一组叫「未分类」，选中后发给服务端的是专用取值而不是空串
// （空串是「全部国家」）。怎么整理选项、顶上那行算哪一种，规矩都在 lib/supplierCountryFilter。
const countryChoiceList=computed(()=>withSelectedChoice(countryChoices(countries.value),countryCode.value))
const countryTotal=computed(()=>totalInChoices(countryChoiceList.value))
function countryLabel(code:string){return code?countryName(code,locale.value):t('suppliers.unclassified')}
function fullName(row: Supplier) {
  return locale.value === 'zh' ? row.nameZh || row.nameEn || row.name : row.nameEn || row.nameZh || row.name
}
function displayName(row: Supplier) { return row.shortName?.trim() || fullName(row) }
function supplierNames(row: Supplier) {
  return [...new Set([fullName(row), row.nameZh, row.nameEn])].filter(Boolean).join(' · ')
}
function optionLabel(code:string){return businessRoleLabel(code,t,businessOptions.value.find(o=>o.code===code)?.label)}
async function syncQuery(){await router.replace({query:masterDataListQuery({keyword:keyword.value,country:countryCode.value,businessType:businessType.value,status:status.value,page:page.value})})}
async function load(){clearSelection();loading.value=true;try{await syncQuery();const d=await get<any>('/suppliers',{page:page.value,page_size:pageSize.value,keyword:keyword.value,country_code:countryCode.value,business_type:businessType.value,status:status.value});rows.value=d.suppliers||[];total.value=Number(d.meta?.total||0)}catch{rows.value=[];total.value=0}finally{loading.value=false}}
async function bulkDeleteSaved(){page.value=1;await Promise.all([load(),loadGroups()])}
async function loadGroups(){try{const d=await get<any>('/suppliers/countries');countries.value=(d.countries||[]) as CountryGroup[]}catch{countries.value=[]}}
function resetLoad(){page.value=1;load()}
function changePage(v:number){page.value=v;load()}
function changePageSize(v:number){pageSize.value=v;page.value=1;load()}
function clearSelection(){selectedSuppliers.value=[];supplierTable.value?.clearSelection()}
function openBulkOwners(action:'ADD'|'REMOVE'){bulkOwnerAction.value=action;bulkOwnerOpen.value=true}
async function bulkOwnersSaved(){clearSelection();await load()}
function openDetail(r:Supplier){router.push(`/basic/suppliers/${r.id}`)}
async function refreshAll(){await Promise.all([load(),loadGroups()])}
async function handleAction(command:string,row:Supplier){if(command==='view'){openDetail(row);return}if(command==='delete')await deleteSupplier(row)}
async function deleteSupplier(row:Supplier){try{await ElMessageBox.confirm('确定删除该供应商吗？','删除供应商',{confirmButtonText:'确定',cancelButtonText:'取消',type:'warning'});await del(`/suppliers/${row.id}`,{reason:'最高权限用户删除供应商'});ElMessage.success('供应商已删除');await refreshAll()}catch{/* 用户取消或接口错误时保持当前状态。 */}}
onMounted(async()=>{try{const [,business,payment,access]=await Promise.all([Promise.all([load(),loadGroups()]),get<any>('/options',{category:'SUPPLIER_BUSINESS_TYPE'}),get<any>('/options',{category:'PAYMENT_METHOD'}),get<any>('/suppliers/access')]);businessOptions.value=business.options||[];paymentOptions.value=payment.options||[];canDelete.value=Boolean(access.canDelete);canManageOwners.value=Boolean(access.canManageOwners)}catch{/* 各请求已由全局拦截器提示。 */}})
</script>

<style scoped>
.supplier-page { min-width: 0; }
.list-card :deep(.el-card__body) { padding: 12px 14px; }
.supplier-toolbar, .supplier-heading, .filters, .filter-actions, .bulk-owner-bar { display: flex; align-items: center; gap: 7px; }
.supplier-toolbar { flex-wrap: wrap; margin-bottom: 10px; }
.supplier-heading { flex: none; }
.supplier-heading h1 { margin: 0; font-size: 17px; color: #173b52; }
.supplier-heading span { color: var(--el-text-color-secondary); font-size: 12px; }
.filters { flex: 1 0 auto; flex-wrap: wrap; }
.filters .el-input { width: 138px; }
.filters .country-select { width: 158px; }
.filter-actions { flex-wrap: wrap; margin-inline-start: auto; }
.filter-actions .el-button + .el-button { margin-left: 0; }
.filter-actions :deep(.el-button) { min-height: 24px; height: auto; padding-inline: 8px; font-size: 12px; }
.bulk-owner-bar { flex-wrap: wrap; margin-bottom: 8px; padding: 6px 8px; background: #edf6fb; border-radius: 6px; }
.bulk-owner-bar span { margin-inline-end: auto; font-size: 13px; }
.supplier-name-heading { display: block; text-align: start; }
.supplier-identity {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  width: 100%;
  min-width: 0;
  gap: 1px;
  padding: 0;
  border: 0;
  background: transparent;
  text-align: start;
  cursor: pointer;
}
.supplier-identity strong, .supplier-identity span {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.supplier-identity strong { color: var(--el-color-primary); font-size: 13px; }
.supplier-identity span { color: var(--el-text-color-secondary); font-size: 11px; }
.list-card :deep(th.el-table__cell) { background: #f2f7fa; padding-block: 5px; }
.list-card :deep(.cell) { line-height: 1.45; }
.list-card :deep(.supplier-selection-column > .cell) { display: flex; justify-content: center; }
.list-card :deep(.el-tag) { margin: 2px 4px 2px 0; }
.pager { justify-content: flex-end; margin-top: 8px; }
.caret { font-size: 8px; margin-inline-start: 4px; }
.danger-item { color: var(--el-color-danger); }
@media (max-width: 540px) {
  .filters { flex-basis: 100%; }
  .filters .el-input { width: min(190px, 65%); }
  .filter-actions { margin-inline-start: 0; }
}
</style>
