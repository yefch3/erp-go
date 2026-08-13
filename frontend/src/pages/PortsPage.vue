<template>
  <div>
    <div class="page-head">
      <div><h2>{{ t('ports.title') }}</h2><p>{{ t('ports.subtitle') }}</p></div>
      <div v-if="canWrite && activeCategory==='PORT'"><el-button @click="importOpen=true">{{ t('ports.bulkImport') }}</el-button><el-button type="primary" @click="openCreate">{{ t('ports.create') }}</el-button></div>
    </div>

    <div class="mobile-category"><el-select v-model="activeCategory"><el-option :label="t('ports.portCategory')" value="PORT"/><el-option :label="t('ports.vesselCategory')" value="VESSEL"/></el-select></div>

    <div class="port-workspace" :class="{collapsed:categoryCollapsed}">
      <el-card class="category-panel" shadow="never">
        <div class="category-title"><span v-if="!categoryCollapsed">{{ t('ports.categories') }}</span><button type="button" @click="categoryCollapsed=!categoryCollapsed">{{categoryCollapsed?'›':'‹'}}</button></div>
        <button class="category-item" :class="{active:activeCategory==='PORT'}" type="button" @click="activeCategory='PORT'">{{ categoryCollapsed?t('ports.portShort'):t('ports.portCategory') }}</button>
        <button class="category-item" :class="{active:activeCategory==='VESSEL'}" type="button" @click="activeCategory='VESSEL'">{{ categoryCollapsed?t('ports.vesselShort'):t('ports.vesselCategory') }}</button>
        <template v-if="activeCategory==='PORT'">
          <div v-if="!categoryCollapsed" class="category-subtitle">{{ t('ports.countryRegion') }}</div>
          <el-select v-if="!categoryCollapsed" v-model="countryCode" filterable clearable :placeholder="t('ports.allCountries')" @change="reload">
            <el-option v-for="c in countries" :key="c.code" :value="c.code" :label="`${c.name} (${c.code})`" />
          </el-select>
          <button class="country-item" :class="{active:countryCode===''}" type="button" @click="selectCountry('')"><span>{{categoryCollapsed?t('ports.allShort'):t('ports.allCountries')}}</span><strong v-if="!categoryCollapsed">{{countryTotal}}</strong></button>
          <button v-for="item in countryCounts" :key="item.countryCode" class="country-item" :class="{active:countryCode===item.countryCode}" type="button" @click="selectCountry(item.countryCode)"><span>{{categoryCollapsed?item.countryCode:countryName(item.countryCode,locale)}}</span><strong v-if="!categoryCollapsed">{{item.portCount}}</strong></button>
          <div v-if="!categoryCollapsed" class="category-help">{{ t('ports.countryHelp') }}</div>
        </template>
      </el-card>

      <el-card v-if="activeCategory==='PORT'" class="port-list" shadow="never">
        <div class="filters">
          <el-input v-model="keyword" clearable :placeholder="t('ports.searchPlaceholder')" @keyup.enter="reload" @clear="reload" />
          <el-select v-model="status" @change="reloadAll"><el-option :label="t('ports.activeOnly')" value="ACTIVE"/><el-option :label="t('ports.allStatuses')" value="ALL"/><el-option :label="t('ports.inactiveOnly')" value="INACTIVE"/></el-select>
          <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
        </div>
        <el-table :data="ports" v-loading="loading">
          <el-table-column prop="unlocode" label="UN/LOCODE" width="130" />
          <el-table-column :label="t('ports.portName')" min-width="220"><template #default="{row}"><strong>{{ displayPortName(row) }}</strong><div class="secondary">{{ secondaryPortName(row) }}</div></template></el-table-column>
          <el-table-column :label="t('ports.country')" width="150"><template #default="{row}">{{ countryName(row.countryCode, locale) }} · {{row.countryCode}}</template></el-table-column>
          <el-table-column prop="city" :label="t('ports.city')" width="150" />
          <el-table-column prop="timezone" :label="t('ports.timezone')" width="180" />
          <el-table-column :label="t('common.status')" width="90"><template #default="{row}"><el-tag :type="row.status==='ACTIVE'?'success':'info'">{{row.status==='ACTIVE'?t('common.active'):t('common.inactive')}}</el-tag></template></el-table-column>
          <el-table-column v-if="canWrite" :label="t('common.actions')" width="150" fixed="right"><template #default="{row}"><el-button link type="primary" @click="openEdit(row)">{{t('common.edit')}}</el-button><el-button link :type="row.status==='ACTIVE'?'danger':'success'" @click="toggleStatus(row)">{{row.status==='ACTIVE'?t('common.deactivate'):t('common.activate')}}</el-button></template></el-table-column>
        </el-table>
        <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="pageSize" :current-page="page" @current-change="changePage" />
      </el-card>
      <el-card v-else class="vessel-placeholder" shadow="never"><el-empty :description="t('ports.vesselDeferred')"/><p>{{ t('ports.vesselBoundary') }}</p></el-card>
    </div>

    <el-dialog v-model="dialogOpen" :title="form.id?t('ports.edit'):t('ports.create')" width="640px">
      <el-form label-width="140px">
        <el-form-item :label="t('ports.countryRegion')" required><el-select v-model="form.countryCode" filterable style="width:100%" @change="syncCountry"><el-option v-for="c in countries" :key="c.code" :value="c.code" :label="`${c.name} (${c.code})`"/></el-select></el-form-item>
        <el-form-item :label="t('ports.unlocode')" required><el-select v-model="form.unlocode" filterable allow-create default-first-option style="width:100%" :placeholder="t('ports.unlocodePlaceholder')" @change="applyPortCode"><el-option v-for="port in codeOptions" :key="port.code" :value="port.code" :label="`${port.code} — ${port.nameZh} / ${port.nameEn}`"/></el-select><div class="hint">{{ t('ports.unlocodeHelp') }}</div></el-form-item>
        <el-form-item :label="t('ports.nameZh')" required><el-input v-model="form.nameZh" :placeholder="t('ports.nameZhPlaceholder')" /></el-form-item>
        <el-form-item :label="t('ports.nameEn')" required><el-input v-model="form.nameEn" :placeholder="t('ports.nameEnPlaceholder')" /></el-form-item>
        <el-form-item :label="t('ports.city')"><el-select v-model="form.city" filterable allow-create default-first-option clearable style="width:100%" :placeholder="t('ports.cityPlaceholder')"><el-option v-for="city in cityOptions" :key="city" :label="city" :value="city"/></el-select></el-form-item>
        <el-form-item :label="t('ports.timezone')" required><el-select v-model="form.timezone" filterable style="width:100%" :placeholder="t('ports.timezonePlaceholder')"><el-option v-for="zone in timezoneOptions" :key="zone" :label="zone" :value="zone"/></el-select><div class="hint">{{ t('ports.timezoneHelp') }}</div></el-form-item>
        <el-form-item :label="t('customers.remark')"><el-input v-model="form.remark" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogOpen=false">{{t('common.cancel')}}</el-button><el-button type="primary" :loading="saving" @click="save">{{t('common.save')}}</el-button></template>
    </el-dialog>
    <ImportPortsDialog v-model="importOpen" @imported="reloadAll" />
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post, put, quietErrors, type Envelope } from '../api'
import { countryName, countryOptions } from '../lib/countries'
import { commonPortOptions, defaultPortTimezone, portCityOptions, portTimezoneOptions } from '../lib/portOptions'
import { useAuthStore } from '../stores/auth'
import ImportPortsDialog from '../components/ImportPortsDialog.vue'

interface Port { id:number; unlocode:string; nameZh:string; nameEn:string; countryCode:string; city:string; timezone:string; aliases:string[]; status:string; remark:string; version:number }
const auth=useAuthStore(); const {locale,t}=useI18n(); const countries=computed(()=>countryOptions(String(locale.value))); const canWrite=computed(()=>auth.can('masterdata:port:write'))
const ports=ref<Port[]>([]), total=ref(0), loading=ref(false), saving=ref(false), dialogOpen=ref(false)
const countryCounts=ref<{countryCode:string;portCount:number}[]>([])
const countryTotal=computed(()=>countryCounts.value.reduce((sum,item)=>sum+Number(item.portCount),0))
const importOpen=ref(false)
const activeCategory=ref<'PORT'|'VESSEL'>('PORT')
const categoryCollapsed=ref(false)
const keyword=ref(''), countryCode=ref(''), status=ref('ACTIVE'), page=ref(1), pageSize=20
const empty=():Port=>({id:0,unlocode:'',nameZh:'',nameEn:'',countryCode:'',city:'',timezone:'',aliases:[],status:'ACTIVE',remark:'',version:0})
const form=reactive<Port>(empty())
const codeOptions=computed(()=>commonPortOptions(form.countryCode))
const cityOptions=computed(()=>portCityOptions(form.countryCode)); const timezoneOptions=computed(()=>portTimezoneOptions(form.countryCode))
async function load(){loading.value=true;try{const d=await get<{ports:Port[];meta:{total:number}}>('/ports',{keyword:keyword.value,country_code:countryCode.value,status:status.value,page:page.value,page_size:pageSize});ports.value=d.ports??[];total.value=Number(d.meta?.total??0)}finally{loading.value=false}}
async function loadCountries(){const data=await get<{countries:{countryCode:string;portCount:number}[]}>('/ports/countries',{status:status.value});countryCounts.value=data.countries??[]}
function reload(){page.value=1;load()} function changePage(p:number){page.value=p;load()}
function reloadAll(){reload();loadCountries()}
function selectCountry(code:string){countryCode.value=code;reload()}
function openCreate(){Object.assign(form,empty());dialogOpen.value=true} function openEdit(p:Port){Object.assign(form,p,{aliases:[...(p.aliases??[])]});dialogOpen.value=true}
function displayPortName(p:Port){return String(locale.value).startsWith('zh')?p.nameZh:p.nameEn}
function secondaryPortName(p:Port){return String(locale.value).startsWith('zh')?p.nameEn:p.nameZh}
function applyPortCode(value:string){form.unlocode=String(value).toUpperCase().replace(/[^A-Z0-9]/g,'').slice(0,5);if(form.unlocode.length>=2)form.countryCode=form.unlocode.slice(0,2);const known=commonPortOptions(form.countryCode).find(p=>p.code===form.unlocode);if(known){if(!form.nameZh)form.nameZh=known.nameZh;if(!form.nameEn)form.nameEn=known.nameEn;if(!form.city)form.city=known.city;if(!form.timezone)form.timezone=known.timezone}}
function syncCountry(){if(form.unlocode.length>=2)form.unlocode=form.countryCode+form.unlocode.slice(2);if(!form.timezone)form.timezone=defaultPortTimezone(form.countryCode)}
async function save(){
  if(!form.countryCode||form.unlocode.length!==5||!form.nameZh||!form.nameEn||!form.timezone){
    ElMessage.warning(t('ports.required'))
    return
  }
  saving.value=true
  try{
    // 新增前先做一次精确检查，让用户直接编辑已有港口；数据库唯一约束仍负责最终并发保护。
    if(!form.id){
      const existing=await get<{ports:Port[]}>('/ports',{keyword:form.unlocode,status:'ALL',page:1,page_size:20})
      const duplicate=(existing.ports??[]).find(p=>p.unlocode.toUpperCase()===form.unlocode.toUpperCase())
      if(duplicate){
        ElMessage.warning(t('ports.duplicate',{code:form.unlocode}))
        dialogOpen.value=false
        keyword.value=form.unlocode
        status.value='ALL'
        reload()
        return
      }
    }
    const body={port:{...form}}
    form.id?await put(`/ports/${form.id}`,body,quietErrors):await post('/ports',body,quietErrors)
    dialogOpen.value=false
    ElMessage.success(t('ports.saved'))
    reloadAll()
  }catch(error){
    const env=error as Envelope<unknown>
    ElMessage.error(env?.message||t('common.requestFailed'))
  }finally{
    saving.value=false
  }
}
async function toggleStatus(p:Port){
  const next=p.status==='ACTIVE'?'INACTIVE':'ACTIVE'
  try{
    await ElMessageBox.confirm(t('ports.statusConfirm',{action:next==='ACTIVE'?t('common.activate'):t('common.deactivate'),code:p.unlocode}),t('ports.confirmTitle'))
    await put(`/ports/${p.id}/status`,{status:next,version:p.version},quietErrors)
    ElMessage.success(t('ports.statusUpdated'))
    reloadAll()
  }catch(error){
    if(error==='cancel'||error==='close')return
    const env=error as Envelope<unknown>
    ElMessage.error(env?.message||t('common.requestFailed'))
    if(env?.code==='MD_PORT_VERSION_CONFLICT')reloadAll()
  }
}
load();loadCountries()
</script>

<style scoped>
.page-head{display:flex;align-items:flex-start;justify-content:space-between;margin-bottom:18px}.page-head h2{margin:0 0 5px}.page-head p,.secondary,.hint,.category-help,.vessel-placeholder p{color:#8b95a5}.page-head p{margin:0}.port-workspace{display:grid;grid-template-columns:230px minmax(0,1fr);gap:18px}.port-workspace.collapsed{grid-template-columns:72px minmax(0,1fr)}.category-panel{background:linear-gradient(180deg,#f3f8ff,#fff)}.category-title{font-weight:700;margin-bottom:10px;display:flex;align-items:center;justify-content:space-between}.category-title button{border:0;border-radius:6px;background:var(--el-fill-color);cursor:pointer;color:var(--el-text-color-secondary);font-size:20px}.category-item,.country-item{display:flex;width:100%;border:0;background:transparent;text-align:left;padding:10px 12px;border-radius:8px;font-size:15px;cursor:pointer;justify-content:space-between}.category-item.active,.country-item.active{background:#e8f2ff;color:#409eff;font-weight:700}.country-item{font-size:13px;margin-top:3px}.country-item strong{background:#eef1f5;border-radius:12px;min-width:30px;text-align:center}.category-subtitle{font-size:13px;font-weight:700;margin:18px 0 8px}.category-help{font-size:12px;line-height:1.7;margin-top:14px}.filters{display:flex;gap:12px;margin-bottom:16px}.filters .el-input{max-width:330px}.filters .el-select{width:140px}.secondary{font-size:12px;margin-top:3px}.pager{justify-content:flex-end;margin-top:16px}.hint{font-size:12px;margin-top:5px}.vessel-placeholder{text-align:center}.mobile-category{display:none;margin-bottom:12px}@media(max-width:850px){.port-workspace{grid-template-columns:1fr}.category-panel{display:none}.mobile-category{display:block}.filters{flex-wrap:wrap}}
</style>
