<template>
  <div class="factory-page">
    <div class="page-head">
      <div><h2>{{ t('suppliers.factoryTitle') }}</h2><p>{{ t('suppliers.factorySubtitle') }}</p></div>
      <div v-if="auth.can('masterdata:factory:write')"><el-button @click="importOpen=true">{{ t('suppliers.bulkImport') }}</el-button><el-button type="primary" @click="openCreate">{{ t('suppliers.createFactory') }}</el-button></div>
    </div>
    <div class="mode-tabs"><button type="button" @click="router.push('/basic/suppliers')">{{ t('suppliers.suppliers') }}</button><button class="active">{{ t('suppliers.factories') }}</button></div>
    <div class="workspace">
      <el-card class="country-panel" shadow="never">
        <h3>{{ t('suppliers.countryGroups') }}</h3>
        <button :class="{active:countryCode===''}" @click="selectCountry('')"><span>{{ t('suppliers.allCountries') }}</span><b>{{ countryTotal }}</b></button>
        <button v-for="g in groups" :key="g.countryCode||'none'" :class="{active:countryCode===g.countryCode}" @click="selectCountry(g.countryCode)"><span>{{ g.countryCode?countryName(g.countryCode,locale):t('suppliers.unclassified') }}</span><b>{{ g.count }}</b></button>
      </el-card>
      <el-card shadow="never">
        <div class="filters">
          <el-input v-model="keyword" clearable :placeholder="t('suppliers.factorySearch')" @keyup.enter="resetLoad" />
          <el-input v-model="city" clearable :placeholder="t('suppliers.city')" @keyup.enter="resetLoad" />
          <el-input v-model="productCategory" clearable :placeholder="t('suppliers.productCategory')" @keyup.enter="resetLoad" />
          <el-select v-model="status" clearable :placeholder="t('common.status')" @change="resetLoad"><el-option v-for="s in statuses" :key="s" :label="statusLabel(s)" :value="s" /></el-select>
          <el-button type="primary" @click="resetLoad">{{ t('common.query') }}</el-button>
        </div>
        <el-table :data="rows" v-loading="loading">
          <el-table-column :label="t('suppliers.factoryCode')" width="130"><template #default="{row}"><el-button link type="primary" @click="detail(row)">{{ row.code }}</el-button></template></el-table-column>
          <el-table-column :label="t('suppliers.factoryName')" min-width="190"><template #default="{row}"><strong>{{ displayName(row) }}</strong></template></el-table-column>
          <el-table-column :label="t('suppliers.belongsSupplier')" min-width="180" prop="supplierName" />
          <el-table-column :label="t('suppliers.country')" width="130"><template #default="{row}">{{ row.countryCode?countryName(row.countryCode,locale):'—' }}</template></el-table-column>
          <el-table-column :label="t('suppliers.city')" width="120" prop="city" />
          <el-table-column :label="t('suppliers.owners')" min-width="150" prop="ownerNames" />
          <el-table-column :label="t('common.status')" width="110"><template #default="{row}"><el-tag :type="row.status==='COOPERATING'?'success':row.status==='SUSPENDED'?'warning':'info'">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="100"><template #default="{row}"><el-button link type="primary" @click="detail(row)">{{ t('suppliers.view') }}</el-button></template></el-table-column>
        </el-table>
        <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="pageSize" :current-page="page" @current-change="changePage" />
      </el-card>
    </div>
    <el-dialog v-model="dialogOpen" :title="t('suppliers.createFactory')" width="700px">
      <el-form :model="form" label-position="top" class="form-grid">
        <el-form-item :label="t('suppliers.belongsSupplier')" class="full" required><el-select v-model="form.supplierId" filterable><el-option v-for="s in supplierOptions" :key="s.id" :label="`${s.code} · ${s.nameZh||s.nameEn||s.name}`" :value="s.id" /></el-select></el-form-item>
        <el-alert class="full code-notice" :title="t('suppliers.factoryCodeNotice')" type="info" :closable="false" show-icon />
        <el-form-item :label="t('suppliers.factoryName')" required><el-input v-model="form.nameZh" /></el-form-item><el-form-item :label="t('suppliers.factoryNameEn')"><el-input v-model="form.nameEn" /></el-form-item>
        <el-form-item :label="t('suppliers.country')"><el-select v-model="form.countryCode" filterable clearable @change="syncCountry"><el-option v-for="c in countryOptions(locale)" :key="c.code" :label="c.name" :value="c.code" /></el-select></el-form-item><el-form-item :label="t('common.status')"><el-select v-model="form.status"><el-option v-for="s in statuses" :key="s" :label="statusLabel(s)" :value="s" /></el-select></el-form-item>
        <el-form-item :label="t('suppliers.city')"><el-select v-model="form.city" filterable allow-create default-first-option clearable :placeholder="t('suppliers.factoryCityPlaceholder')"><el-option v-for="value in cityOptions" :key="value" :label="value" :value="value" /></el-select></el-form-item>
        <el-form-item :label="t('suppliers.timezone')"><el-select v-model="form.timezone" filterable clearable :placeholder="t('suppliers.factoryTimezonePlaceholder')"><el-option v-for="z in timezoneOptions" :key="z" :label="z" :value="z" /></el-select></el-form-item>
        <el-form-item :label="t('suppliers.postalCode')"><el-input v-model="form.postalCode" /></el-form-item>
        <el-form-item :label="t('suppliers.factoryAddress')" class="full"><el-input v-model="form.address" /></el-form-item><el-form-item :label="t('suppliers.remark')" class="full"><el-input v-model="form.remark" type="textarea" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogOpen=false">{{ t('common.cancel') }}</el-button><el-button type="primary" :loading="saving" @click="save">{{ t('common.save') }}</el-button></template>
    </el-dialog>
    <SupplierFactoryImportDialog v-model="importOpen" kind="factory" @imported="refreshAll" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { get, post } from '../api'
import SupplierFactoryImportDialog from '../components/SupplierFactoryImportDialog.vue'
import { countryName, countryOptions } from '../lib/countries'
import { defaultPortTimezone, portCityOptions, portTimezoneOptions } from '../lib/portOptions'
import { masterDataListQuery, queryPage, queryText } from '../lib/masterDataListQuery'
import { useAuthStore } from '../stores/auth'

interface Factory { id:string;code:string;nameZh:string;nameEn:string;supplierName:string;countryCode:string;city:string;ownerNames:string;status:string }
interface Group { countryCode:string;count:string }
interface Supplier { id:string;code:string;name:string;nameZh:string;nameEn:string }
const { t, locale }=useI18n(), route=useRoute(), router=useRouter(), auth=useAuthStore()
const rows=ref<Factory[]>([]), groups=ref<Group[]>([]), supplierOptions=ref<Supplier[]>([])
const loading=ref(false), saving=ref(false), dialogOpen=ref(false), importOpen=ref(false)
const keyword=ref(queryText(route.query.keyword)), city=ref(queryText(route.query.city)), productCategory=ref(queryText(route.query.product_category)), countryCode=ref(queryText(route.query.country)), status=ref(queryText(route.query.status))
const page=ref(queryPage(route.query.page)), pageSize=20, total=ref(0)
const statuses=['PREPARING','COOPERATING','SUSPENDED','INACTIVE']
const empty={supplierId:'',code:'',nameZh:'',nameEn:'',shortName:'',countryCode:'',timezone:'',stateProvince:'',city:'',district:'',postalCode:'',address:'',status:'PREPARING',remark:''}
const form=reactive({...empty})
const countryTotal=computed(()=>groups.value.reduce((n,g)=>n+Number(g.count),0))
const cityOptions=computed(()=>portCityOptions(form.countryCode))
const timezoneOptions=computed(()=>portTimezoneOptions(form.countryCode))
function displayName(r:Factory){return locale.value==='zh'?r.nameZh||r.nameEn:r.nameEn||r.nameZh}
function statusLabel(s:string){return t(`suppliers.factoryStatus.${s}`)}
async function syncQuery(){await router.replace({query:masterDataListQuery({keyword:keyword.value,country:countryCode.value,city:city.value,productCategory:productCategory.value,status:status.value,page:page.value})})}
async function load(){loading.value=true;try{await syncQuery();const d=await get<any>('/factories',{page:page.value,page_size:pageSize,keyword:keyword.value,city:city.value,product_category:productCategory.value,country_code:countryCode.value,status:status.value});rows.value=d.factories||[];total.value=Number(d.meta?.total||0)}catch{rows.value=[];total.value=0}finally{loading.value=false}}
async function loadMeta(){try{const [g,s]=await Promise.all([get<any>('/factories/countries'),get<any>('/suppliers',{page:1,page_size:200,status:''})]);groups.value=g.countries||[];supplierOptions.value=s.suppliers||[]}catch{groups.value=[];supplierOptions.value=[]}}
async function refreshAll(){await Promise.all([load(),loadMeta()])}
function resetLoad(){page.value=1;load()}
function selectCountry(v:string){countryCode.value=v;resetLoad()}
function changePage(v:number){page.value=v;load()}
function detail(r:Factory){router.push(`/basic/suppliers/factories/${r.id}`)}
function openCreate(){Object.assign(form,empty);dialogOpen.value=true}
function syncCountry(){form.city='';form.timezone=defaultPortTimezone(form.countryCode)}
async function save(){if(!form.supplierId||!form.nameZh.trim()){ElMessage.warning(t('suppliers.factoryRequired'));return}saving.value=true;try{await post('/factories',{factory:{...form}});dialogOpen.value=false;ElMessage.success(t('suppliers.saved'));await refreshAll()}catch{/* 接口错误已显示统一提示，保留表单便于修正。 */}finally{saving.value=false}}
onMounted(()=>{void refreshAll()})
</script>

<style scoped>
.factory-page{--navy:#18324a;--teal:#147d7b}.page-head{display:flex;justify-content:space-between;align-items:center;margin-bottom:16px}.page-head h2{margin:0;color:var(--navy)}.page-head p{margin:6px 0 0;color:#778899}.mode-tabs{display:flex;gap:8px;margin-bottom:14px}.mode-tabs button{border:1px solid #d8e1e8;background:#fff;padding:9px 20px;border-radius:10px;color:#536575;cursor:pointer}.mode-tabs button.active{background:var(--navy);color:#fff}.workspace{display:grid;grid-template-columns:225px 1fr;gap:16px}.country-panel h3{margin:2px 0 12px}.country-panel button{width:100%;display:flex;justify-content:space-between;border:0;background:transparent;padding:10px 12px;border-radius:9px;cursor:pointer}.country-panel button.active{background:#e6f4f2;color:var(--teal)}.country-panel b{background:#edf1f4;border-radius:12px;padding:1px 9px}.filters{display:flex;flex-wrap:wrap;gap:10px;margin-bottom:16px}.filters .el-input{max-width:210px}.filters .el-select{width:160px}.pager{justify-content:flex-end;margin-top:18px}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 16px}.form-grid .full{grid-column:1/-1}.form-grid .code-notice{margin-bottom:18px}.form-grid :deep(.el-select){width:100%}@media(max-width:900px){.workspace{grid-template-columns:1fr}.country-panel{display:none}.form-grid{grid-template-columns:1fr}.form-grid .full{grid-column:auto}}
</style>
