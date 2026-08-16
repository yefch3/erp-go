<template>
  <div class="supplier-page">
    <div class="page-head">
      <div><h2>{{ t('suppliers.title') }}</h2><p>{{ t('suppliers.subtitle') }}</p></div>
      <div v-if="auth.can('masterdata:supplier:write')" class="head-actions"><el-button @click="importOpen=true">{{ t('suppliers.bulkImport') }}</el-button><el-button type="primary" @click="openCreate">{{ t('suppliers.create') }}</el-button></div>
    </div>
    <div class="mode-tabs">
      <button class="active" type="button">{{ t('suppliers.suppliers') }}</button>
      <button type="button" @click="router.push('/basic/suppliers/factories')">{{ t('suppliers.factories') }}</button>
    </div>
    <div class="workspace">
      <el-card class="country-panel" shadow="never">
        <h3>{{ t('suppliers.countryGroups') }}</h3>
        <button :class="{active:countryCode===''}" @click="chooseCountry('')"><span>{{ t('suppliers.allCountries') }}</span><b>{{ countryTotal }}</b></button>
        <button v-for="g in countries" :key="g.countryCode||'none'" :class="{active:countryCode===g.countryCode}" @click="chooseCountry(g.countryCode)">
          <span>{{ g.countryCode ? countryName(g.countryCode, locale) : t('suppliers.unclassified') }}</span><b>{{ g.count }}</b>
        </button>
      </el-card>
      <el-card class="list-card" shadow="never">
        <div class="filters">
          <el-input v-model="keyword" clearable :placeholder="t('suppliers.search')" @keyup.enter="load" />
          <el-select v-model="businessType" clearable :placeholder="t('suppliers.businessType')" @change="resetLoad"><el-option v-for="o in businessOptions" :key="o.code" :label="optionLabel(o.code)" :value="o.code" /></el-select>
          <el-select v-model="status" @change="resetLoad"><el-option :label="t('common.active')" value=""/><el-option :label="t('common.inactive')" value="INACTIVE"/><el-option :label="t('suppliers.allStatuses')" value="ALL"/></el-select>
          <el-button type="primary" @click="resetLoad">{{ t('common.query') }}</el-button>
        </div>
        <el-table :data="rows" v-loading="loading">
          <el-table-column :label="t('suppliers.code')" width="135"><template #default="{row}"><el-button link type="primary" @click="openDetail(row)">{{ row.code }}</el-button></template></el-table-column>
          <el-table-column :label="t('suppliers.name')" min-width="210"><template #default="{row}"><div class="name-cell"><strong>{{ displayName(row) }}</strong><small>{{ secondaryName(row) }}</small></div></template></el-table-column>
          <el-table-column :label="t('suppliers.country')" width="150"><template #default="{row}">{{ row.countryCode ? countryName(row.countryCode, locale) : '—' }}</template></el-table-column>
          <el-table-column :label="t('suppliers.businessType')" min-width="180"><template #default="{row}"><el-tag v-for="v in row.businessTypes" :key="v" size="small" effect="plain">{{ optionLabel(v) }}</el-tag></template></el-table-column>
          <el-table-column :label="t('suppliers.owners')" min-width="150" prop="ownerNames" />
          <el-table-column :label="t('suppliers.factoryCount')" width="90" prop="factoryCount" />
          <el-table-column :label="t('common.status')" width="90"><template #default="{row}"><el-tag :type="row.status==='ACTIVE'?'success':'info'">{{ row.status==='ACTIVE'?t('common.active'):t('common.inactive') }}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="165" fixed="right"><template #default="{row}"><el-button link type="primary" @click="openDetail(row)">{{ t('suppliers.view') }}</el-button><el-button v-if="auth.can('masterdata:supplier:write')" link :type="row.status==='ACTIVE'?'danger':'success'" @click="toggleStatus(row)">{{ row.status==='ACTIVE'?t('common.deactivate'):t('common.activate') }}</el-button></template></el-table-column>
        </el-table>
        <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="pageSize" :current-page="page" @current-change="changePage" />
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
    <SupplierFactoryImportDialog v-model="importOpen" kind="supplier" @imported="refreshAll" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { del, get, post } from '../api'
import SupplierFactoryImportDialog from '../components/SupplierFactoryImportDialog.vue'
import { countryName, countryOptions } from '../lib/countries'
import { businessRoleLabel } from '../lib/masterDataDisplay'
import { masterDataListQuery, queryPage, queryText } from '../lib/masterDataListQuery'
import { useAuthStore } from '../stores/auth'

interface Supplier { id:string;code:string;name:string;nameZh:string;nameEn:string;shortName:string;countryCode:string;businessTypes:string[];ownerNames:string;factoryCount:string;status:string }
interface CountryGroup { countryCode:string;count:string }
interface OptionItem { code:string;label:string }
const { t, locale } = useI18n(); const route=useRoute(); const router=useRouter(); const auth=useAuthStore()
const rows=ref<Supplier[]>([]), countries=ref<CountryGroup[]>([]), businessOptions=ref<OptionItem[]>([]), paymentOptions=ref<OptionItem[]>([])
const keyword=ref(queryText(route.query.keyword)), countryCode=ref(queryText(route.query.country)), businessType=ref(queryText(route.query.business_type)), status=ref(queryText(route.query.status)), loading=ref(false), saving=ref(false), dialogOpen=ref(false), importOpen=ref(false)
const page=ref(queryPage(route.query.page)), pageSize=20, total=ref(0)
const empty={code:'',name:'',nameZh:'',nameEn:'',shortName:'',country:'',countryCode:'',currency:'USD',businessTypes:['GENERAL'],taxId:'',paymentTerm:'',registeredAddress:'',address:'',remark:''}
const form=reactive({...empty})
const countryTotal=computed(()=>countries.value.reduce((n,g)=>n+Number(g.count),0))
function displayName(r:Supplier){return locale.value==='zh'?r.nameZh||r.nameEn||r.name:r.nameEn||r.nameZh||r.name}
function secondaryName(r:Supplier){const v=locale.value==='zh'?r.nameEn:r.nameZh;return v&&v!==displayName(r)?v:''}
function optionLabel(code:string){return businessRoleLabel(code,t,businessOptions.value.find(o=>o.code===code)?.label)}
async function syncQuery(){await router.replace({query:masterDataListQuery({keyword:keyword.value,country:countryCode.value,businessType:businessType.value,status:status.value,page:page.value})})}
async function load(){loading.value=true;try{await syncQuery();const d=await get<any>('/suppliers',{page:page.value,page_size:pageSize,keyword:keyword.value,country_code:countryCode.value,business_type:businessType.value,status:status.value});rows.value=d.suppliers||[];total.value=Number(d.meta?.total||0)}catch{rows.value=[];total.value=0}finally{loading.value=false}}
async function loadGroups(){try{const d=await get<any>('/suppliers/countries');countries.value=(d.countries||[]) as CountryGroup[]}catch{countries.value=[]}}
function resetLoad(){page.value=1;load()}
function chooseCountry(v:string){countryCode.value=v;resetLoad()}
function changePage(v:number){page.value=v;load()}
function openCreate(){Object.assign(form,empty,{businessTypes:['GENERAL']});dialogOpen.value=true}
function openDetail(r:Supplier){router.push(`/basic/suppliers/${r.id}`)}
async function refreshAll(){await Promise.all([load(),loadGroups()])}
async function toggleStatus(row:Supplier){const deactivating=row.status==='ACTIVE';const message=deactivating?t('suppliers.deactivateCascadeConfirm',{name:displayName(row),count:Number(row.factoryCount||0)}):t('suppliers.activateConfirm',{name:displayName(row)});try{await ElMessageBox.confirm(message,t('suppliers.confirmTitle'),{confirmButtonText:deactivating?t('suppliers.deactivateAndPause'):t('common.activate'),cancelButtonText:t('common.cancel'),type:deactivating?'warning':'info'});if(deactivating)await del(`/suppliers/${row.id}`);else await post(`/suppliers/${row.id}/activate`);ElMessage.success(deactivating?t('suppliers.deactivatedWithFactories'):t('suppliers.activatedFactoriesRemainPaused'));await refreshAll()}catch{/* 取消确认或接口错误已由全局拦截器处理。 */}}
async function save(){if(!form.nameZh.trim()&&!form.nameEn.trim()){ElMessage.warning(t('suppliers.nameRequired'));return}saving.value=true;try{await post('/suppliers',{...form,name:form.nameZh||form.nameEn,country:form.countryCode});dialogOpen.value=false;ElMessage.success(t('suppliers.saved'));await refreshAll()}catch{/* 接口错误已显示统一提示，保留表单便于修正。 */}finally{saving.value=false}}
onMounted(async()=>{try{const [,business,payment]=await Promise.all([Promise.all([load(),loadGroups()]),get<any>('/options',{category:'SUPPLIER_BUSINESS_TYPE'}),get<any>('/options',{category:'PAYMENT_METHOD'})]);businessOptions.value=business.options||[];paymentOptions.value=payment.options||[]}catch{/* 各请求已由全局拦截器提示。 */}})
</script>

<style scoped>
.supplier-page{--navy:#18324a;--teal:#147d7b}.page-head{display:flex;justify-content:space-between;align-items:center;margin-bottom:16px}.page-head h2{margin:0;color:var(--navy)}.page-head p{margin:6px 0 0;color:#778899}.mode-tabs{display:flex;gap:8px;margin-bottom:14px}.mode-tabs button{border:1px solid #d8e1e8;background:#fff;padding:9px 20px;border-radius:10px;color:#536575;cursor:pointer}.mode-tabs button.active{background:var(--navy);color:#fff;border-color:var(--navy)}.workspace{display:grid;grid-template-columns:225px 1fr;gap:16px}.country-panel h3{margin:2px 0 12px;color:var(--navy)}.country-panel button{width:100%;display:flex;justify-content:space-between;border:0;background:transparent;padding:10px 12px;border-radius:9px;color:#526372;cursor:pointer}.country-panel button.active{background:#e6f4f2;color:var(--teal)}.country-panel b{background:#edf1f4;border-radius:12px;padding:1px 9px}.filters{display:flex;gap:10px;margin-bottom:16px}.filters .el-input{max-width:280px}.filters .el-select{width:165px}.name-cell{display:flex;flex-direction:column}.name-cell small{color:#8b99a5}.el-tag+.el-tag{margin-left:5px}.pager{justify-content:flex-end;margin-top:18px}.form-grid{display:grid;grid-template-columns:1fr 1fr;column-gap:18px}.form-grid .full{grid-column:1/-1}.form-grid .code-notice{margin-bottom:18px}.form-grid :deep(.el-select){width:100%}@media(max-width:900px){.workspace{grid-template-columns:1fr}.country-panel{display:none}.filters{flex-wrap:wrap}.form-grid{grid-template-columns:1fr}.form-grid .full{grid-column:auto}}
</style>
