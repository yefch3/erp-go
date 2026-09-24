<template>
  <div class="supplier-page">
    <div class="country-strip">
      <div><span>{{ t('suppliers.countryGroups') }}</span><strong>{{ countryCode ? countryName(countryCode, locale) : t('suppliers.allCountries') }} · {{ countryCode ? Number(countries.find(g => g.countryCode === countryCode)?.count || 0) : countryTotal }}</strong></div>
      <el-select v-model="countryCode" filterable :aria-label="t('suppliers.countryGroups')" @change="resetLoad">
        <el-option :label="`${t('suppliers.allCountries')}（${countryTotal}）`" value="" />
        <el-option v-for="g in countries" :key="g.countryCode||'none'" :label="`${g.countryCode ? countryName(g.countryCode, locale) : t('suppliers.unclassified')}（${Number(g.count)}）`" :value="g.countryCode" />
      </el-select>
    </div>
    <div class="workspace">
      <el-card class="list-card" shadow="never">
        <div class="filters">
          <el-input v-model="keyword" clearable :placeholder="t('suppliers.search')" @keyup.enter="load" />
          <el-select v-model="businessType" clearable :placeholder="t('suppliers.businessType')" @change="resetLoad"><el-option v-for="o in businessOptions" :key="o.code" :label="optionLabel(o.code)" :value="o.code" /></el-select>
          <el-select v-model="status" @change="resetLoad"><el-option :label="t('common.active')" value=""/><el-option :label="t('common.inactive')" value="INACTIVE"/><el-option :label="t('suppliers.allStatuses')" value="ALL"/></el-select>
          <el-button type="primary" @click="resetLoad">{{ t('common.query') }}</el-button>
          <div v-if="auth.can('masterdata:supplier:write')" class="filter-actions"><el-button @click="importOpen=true">{{ t('suppliers.bulkImport') }}</el-button><el-button type="primary" @click="openCreate">{{ t('suppliers.create') }}</el-button></div>
        </div>
        <div v-if="canManageOwners" class="bulk-owner-bar">
          <el-button @click="toggleSelectPage">{{ allPageSelected ? '取消全选' : '全选本页' }}</el-button>
          <span>已选择 <strong>{{ selectedSuppliers.length }}</strong> 条</span>
          <el-button type="primary" :disabled="!selectedSuppliers.length" @click="openBulkOwners('ADD')">批量添加负责人</el-button>
          <el-button :disabled="!selectedSuppliers.length" @click="openBulkOwners('REMOVE')">批量移除负责人</el-button>
          <el-button v-if="selectedSuppliers.length" link @click="clearSelection">取消选择</el-button>
        </div>
        <el-table ref="supplierTable" :data="rows" v-loading="loading" @selection-change="selectedSuppliers=$event">
          <el-table-column v-if="canManageOwners" type="selection" width="48" />
          <el-table-column :label="t('suppliers.code')" width="110"><template #default="{row}"><el-button link type="primary" @click="openDetail(row)">{{ row.code }}</el-button></template></el-table-column>
          <el-table-column :label="t('suppliers.name')" min-width="220"><template #default="{row}"><div class="name-cell"><strong>{{ displayName(row) }}</strong><small>{{ secondaryName(row) }}</small></div></template></el-table-column>
          <el-table-column :label="t('suppliers.country')" min-width="120"><template #default="{row}">{{ row.countryCode ? countryName(row.countryCode, locale) : '—' }}</template></el-table-column>
          <el-table-column :label="t('suppliers.businessType')" min-width="140"><template #default="{row}"><el-tag v-for="v in row.businessTypes" :key="v" size="small" effect="plain">{{ optionLabel(v) }}</el-tag></template></el-table-column>
          <el-table-column :label="t('suppliers.owners')" min-width="145" prop="ownerNames" />
          <el-table-column :label="t('common.status')" width="78"><template #default="{row}"><el-tag :type="row.status==='ACTIVE'?'success':'info'" size="small">{{ row.status==='ACTIVE'?t('common.active'):t('common.inactive') }}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="120" fixed="right"><template #default="{row}">
            <el-dropdown trigger="click" @command="(command:string)=>handleAction(command,row)">
              <el-button>更多操作 <span class="caret">▼</span></el-button>
              <template #dropdown><el-dropdown-menu>
                <el-dropdown-item command="view">{{ t('suppliers.view') }}</el-dropdown-item>
                <el-dropdown-item v-if="canDelete" command="delete" divided class="danger-item">删除</el-dropdown-item>
              </el-dropdown-menu></template>
            </el-dropdown>
          </template></el-table-column>
        </el-table>
        <el-pagination class="pager" layout="total, sizes, prev, pager, next" :total="total" :page-size="pageSize" :page-sizes="[20,50,100]" :current-page="page" @size-change="changePageSize" @current-change="changePage" />
      </el-card>
    </div>

    <el-dialog v-model="dialogOpen" :title="t('suppliers.create')" width="680px">
      <el-form :model="form" label-width="130px" class="form-grid">
        <el-alert class="full code-notice" :title="t('suppliers.codeAutoNotice')" type="info" :closable="false" show-icon />
        <el-form-item :label="t('suppliers.nameZh')"><el-input v-model="form.nameZh" /></el-form-item>
        <el-form-item :label="t('suppliers.nameEn')"><el-input v-model="form.nameEn" /></el-form-item>
        <el-form-item :label="t('suppliers.country')"><el-select v-model="form.countryCode" filterable clearable><el-option v-for="c in countryOptions(locale)" :key="c.code" :label="c.name" :value="c.code" /></el-select></el-form-item>
        <el-form-item :label="t('suppliers.currency')"><el-select v-model="form.currency"><el-option v-for="c in ['USD','EUR','CNY','GBP']" :key="c" :label="c" :value="c" /></el-select></el-form-item>
        <el-form-item :label="t('suppliers.businessType')" class="full"><el-checkbox-group v-model="form.businessTypes"><el-checkbox v-for="o in businessOptions" :key="o.code" :value="o.code">{{ optionLabel(o.code) }}</el-checkbox></el-checkbox-group></el-form-item>
        <el-form-item :label="t('suppliers.taxId')"><el-input v-model="form.taxId" /></el-form-item>
        <el-form-item :label="t('suppliers.paymentTerm')"><el-select v-model="form.paymentTerm" filterable allow-create default-first-option clearable :placeholder="t('suppliers.paymentTermPlaceholder')"><el-option v-for="o in paymentOptions" :key="o.code" :label="o.label" :value="o.code" /></el-select></el-form-item>
        <el-form-item :label="t('suppliers.registeredAddress')" class="full"><el-input v-model="form.registeredAddress" /></el-form-item>
        <el-form-item :label="t('suppliers.address')" class="full"><el-input v-model="form.address" type="textarea" /></el-form-item>
        <el-form-item :label="t('suppliers.remark')" class="full"><el-input v-model="form.remark" type="textarea" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button></template>
    </el-dialog>
    <SupplierImportDialog v-model="importOpen" @imported="refreshAll" />
    <BulkOwnerDialog v-model:open="bulkOwnerOpen" entity-type="supplier" :action="bulkOwnerAction" :selected-ids="selectedSuppliers.map(row=>row.id)" @saved="bulkOwnersSaved" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { del, get, post } from '../api'
import SupplierImportDialog from '../components/SupplierImportDialog.vue'
import BulkOwnerDialog from '../components/masterdata/BulkOwnerDialog.vue'
import { countryName, countryOptions } from '../lib/countries'
import { businessRoleLabel } from '../lib/masterDataDisplay'
import { masterDataListQuery, queryPage, queryText } from '../lib/masterDataListQuery'
import { confirmPossibleDuplicates } from '../lib/masterDataDuplicates'
import { useAuthStore } from '../stores/auth'

interface Supplier { id:string;code:string;name:string;nameZh:string;nameEn:string;shortName:string;countryCode:string;businessTypes:string[];ownerNames:string;status:string }
interface CountryGroup { countryCode:string;count:string }
interface OptionItem { code:string;label:string }
const { t, locale } = useI18n(); const route=useRoute(); const router=useRouter(); const auth=useAuthStore()
const rows=ref<Supplier[]>([]), countries=ref<CountryGroup[]>([]), businessOptions=ref<OptionItem[]>([]), paymentOptions=ref<OptionItem[]>([])
const keyword=ref(queryText(route.query.keyword)), countryCode=ref(queryText(route.query.country)), businessType=ref(queryText(route.query.business_type)), status=ref(queryText(route.query.status)), loading=ref(false), saving=ref(false), dialogOpen=ref(false), importOpen=ref(false)
const canDelete=ref(false),canManageOwners=ref(false)
const page=ref(queryPage(route.query.page)), pageSize=ref(20), total=ref(0)
const supplierTable=ref<any>(),selectedSuppliers=ref<Supplier[]>([]),bulkOwnerOpen=ref(false),bulkOwnerAction=ref<'ADD'|'REMOVE'>('ADD')
const allPageSelected=computed(()=>rows.value.length>0&&selectedSuppliers.value.length===rows.value.length)
const empty={code:'',name:'',nameZh:'',nameEn:'',shortName:'',country:'',countryCode:'',currency:'USD',businessTypes:['GENERAL'],taxId:'',paymentTerm:'',registeredAddress:'',address:'',remark:''}
const form=reactive({...empty})
const countryTotal=computed(()=>countries.value.reduce((n,g)=>n+Number(g.count),0))
function displayName(r:Supplier){return locale.value==='zh'?r.nameZh||r.nameEn||r.name:r.nameEn||r.nameZh||r.name}
function secondaryName(r:Supplier){const v=locale.value==='zh'?r.nameEn:r.nameZh;return v&&v!==displayName(r)?v:''}
function optionLabel(code:string){return businessRoleLabel(code,t,businessOptions.value.find(o=>o.code===code)?.label)}
async function syncQuery(){await router.replace({query:masterDataListQuery({keyword:keyword.value,country:countryCode.value,businessType:businessType.value,status:status.value,page:page.value})})}
async function load(){clearSelection();loading.value=true;try{await syncQuery();const d=await get<any>('/suppliers',{page:page.value,page_size:pageSize.value,keyword:keyword.value,country_code:countryCode.value,business_type:businessType.value,status:status.value});rows.value=d.suppliers||[];total.value=Number(d.meta?.total||0)}catch{rows.value=[];total.value=0}finally{loading.value=false}}
async function loadGroups(){try{const d=await get<any>('/suppliers/countries');countries.value=(d.countries||[]) as CountryGroup[]}catch{countries.value=[]}}
function resetLoad(){page.value=1;load()}
function changePage(v:number){page.value=v;load()}
function changePageSize(v:number){pageSize.value=v;page.value=1;load()}
function clearSelection(){selectedSuppliers.value=[];supplierTable.value?.clearSelection()}
function toggleSelectPage(){if(allPageSelected.value)clearSelection();else supplierTable.value?.toggleAllSelection()}
function openBulkOwners(action:'ADD'|'REMOVE'){bulkOwnerAction.value=action;bulkOwnerOpen.value=true}
async function bulkOwnersSaved(){clearSelection();await load()}
function openCreate(){Object.assign(form,empty,{businessTypes:['GENERAL']});dialogOpen.value=true}
function openDetail(r:Supplier){router.push(`/basic/suppliers/${r.id}`)}
async function refreshAll(){await Promise.all([load(),loadGroups()])}
async function handleAction(command:string,row:Supplier){if(command==='view'){openDetail(row);return}if(command==='delete')await deleteSupplier(row)}
async function deleteSupplier(row:Supplier){try{await ElMessageBox.confirm('确定删除该供应商吗？','删除供应商',{confirmButtonText:'确定',cancelButtonText:'取消',type:'warning'});await del(`/suppliers/${row.id}`,{reason:'最高权限用户删除供应商'});ElMessage.success('供应商已删除');await refreshAll()}catch{/* 用户取消或接口错误时保持当前状态。 */}}
async function save(){if(!form.nameZh.trim()&&!form.nameEn.trim()){ElMessage.warning(t('suppliers.nameRequired'));return}saving.value=true;try{const duplicates=await get<any>('/suppliers/duplicates',{name:form.nameZh||form.nameEn,tax_id:form.taxId});await confirmPossibleDuplicates(duplicates.candidates||[],t);await post('/suppliers',{...form,name:form.nameZh||form.nameEn,country:form.countryCode});dialogOpen.value=false;ElMessage.success(t('suppliers.saved'));await refreshAll()}catch{/* 取消重复确认或接口错误时保留表单，便于用户复核。 */}finally{saving.value=false}}
onMounted(async()=>{try{const [,business,payment,access]=await Promise.all([Promise.all([load(),loadGroups()]),get<any>('/options',{category:'SUPPLIER_BUSINESS_TYPE'}),get<any>('/options',{category:'PAYMENT_METHOD'}),get<any>('/suppliers/access')]);businessOptions.value=business.options||[];paymentOptions.value=payment.options||[];canDelete.value=Boolean(access.canDelete);canManageOwners.value=Boolean(access.canManageOwners)}catch{/* 各请求已由全局拦截器提示。 */}})
</script>

<style scoped>
.supplier-page{--navy:#18324a;--teal:#147d7b}.page-head{display:flex;justify-content:space-between;align-items:center;margin-bottom:16px}.page-head h2{margin:0;color:var(--navy)}.page-head p{margin:6px 0 0;color:#778899}.workspace{display:grid;grid-template-columns:225px 1fr;gap:16px}.country-panel h3{margin:2px 0 12px;color:var(--navy)}.country-panel button{width:100%;display:flex;justify-content:space-between;border:0;background:transparent;padding:10px 12px;border-radius:9px;color:#526372;cursor:pointer}.country-panel button.active{background:#e6f4f2;color:var(--teal)}.country-panel b{background:#edf1f4;border-radius:12px;padding:1px 9px}.filters{display:flex;gap:10px;margin-bottom:16px}.filters .el-input{max-width:280px}.filters .el-select{width:165px}.bulk-owner-bar{display:flex;align-items:center;gap:10px;margin:0 0 12px;padding:10px 12px;border:1px solid var(--el-color-primary-light-7);border-radius:8px;background:var(--el-color-primary-light-9)}.bulk-owner-bar span{margin-right:auto;color:var(--el-text-color-regular)}.name-cell{display:flex;flex-direction:column}.name-cell small{color:#8b99a5}.el-tag+.el-tag{margin-left:5px}.pager{justify-content:flex-end;margin-top:18px}.form-grid{display:grid;grid-template-columns:1fr 1fr;column-gap:18px}.form-grid .full{grid-column:1/-1}.form-grid .code-notice{margin-bottom:18px}.form-grid :deep(.el-select){width:100%}.caret{font-size:10px;margin-left:5px}.danger-item{color:#f56c6c}@media(max-width:900px){.workspace{grid-template-columns:1fr}.country-panel{display:none}.filters,.bulk-owner-bar{flex-wrap:wrap}.bulk-owner-bar span{width:100%;margin-right:0}.form-grid{grid-template-columns:1fr}.form-grid .full{grid-column:auto}}
.workspace{display:block}.country-strip{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:9px 12px;margin-bottom:10px;border:1px solid var(--el-border-color-lighter);border-radius:10px;background:var(--el-bg-color)}.country-strip>div{display:flex;flex-direction:column;gap:2px}.country-strip span{font-size:11px;color:var(--el-text-color-secondary)}.country-strip strong{font-size:14px}.country-strip .el-select{width:min(320px,48%)}.list-card :deep(.el-card__body){padding:12px 14px}.filters{align-items:center;flex-wrap:wrap;gap:8px;margin-bottom:10px}.filters .el-input{width:min(280px,25%)}.filters .el-select{width:145px}.filter-actions{display:flex;gap:8px;margin-left:auto}.filter-actions .el-button+.el-button{margin-left:0}.bulk-owner-bar{gap:8px;padding:7px 10px;margin-bottom:10px}.list-card :deep(th.el-table__cell){height:36px;padding:4px 0;font-size:12px}.list-card :deep(td.el-table__cell){padding:6px 0;font-size:12px}.list-card :deep(.el-table__cell .cell){line-height:1.35}.name-cell strong{font-size:13px}.name-cell small{font-size:11px}.list-card :deep(.el-tag){font-size:11px}.pager{margin-top:8px}
@media(max-width:850px){.filters .el-input{width:100%;max-width:none}.filters .el-select{flex:1;min-width:135px}.filter-actions{margin-left:0}}
@media(max-width:480px){.country-strip{align-items:stretch;flex-direction:column}.country-strip .el-select{width:100%}.filter-actions{width:100%}.filter-actions .el-button{flex:1}}
</style>
