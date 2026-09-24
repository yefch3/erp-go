<template>
  <div>
    <div class="country-strip">
      <div><span>{{ t('ports.countryRegion') }}</span><strong>{{ countryCode ? countryName(countryCode, locale) : t('ports.allCountries') }} · {{ countryCode ? Number(countryCounts.find(item => item.countryCode === countryCode)?.portCount || 0) : countryTotal }}</strong></div>
      <el-select v-model="countryCode" filterable :aria-label="t('ports.countryRegion')" @change="reload">
        <el-option :label="`${t('ports.allCountries')}（${countryTotal}）`" value="" />
        <el-option v-for="item in countryCounts" :key="item.countryCode" :label="`${countryName(item.countryCode,locale)}（${item.portCount}）`" :value="item.countryCode" />
      </el-select>
    </div>
    <div class="port-workspace">
      <el-card class="port-list" shadow="never">
        <div class="filters">
          <el-input v-model="keyword" clearable :placeholder="t('ports.searchPlaceholder')" @keyup.enter="reload" @clear="reload" />
          <el-select v-model="status" @change="reloadAll"><el-option :label="t('ports.activeOnly')" value="ACTIVE"/><el-option :label="t('ports.allStatuses')" value="ALL"/><el-option :label="t('ports.inactiveOnly')" value="INACTIVE"/></el-select>
          <el-button type="primary" @click="reload">{{ t('common.query') }}</el-button>
          <div v-if="canWrite" class="filter-actions"><el-button @click="importOpen=true">{{ t('ports.bulkImport') }}</el-button><el-button type="primary" @click="openCreate">{{ t('ports.create') }}</el-button></div>
        </div>
        <el-table :data="ports" v-loading="loading">
          <el-table-column prop="unlocode" label="UN/LOCODE" width="105" />
          <el-table-column :label="t('ports.portName')" min-width="210"><template #default="{row}"><div class="port-name"><button v-if="canWrite" type="button" class="favorite-button" :class="{active:row.isFavorite}" :title="row.isFavorite?t('ports.removeFavorite'):t('ports.addFavorite')" @click.stop="toggleFavorite(row)">{{row.isFavorite?'★':'☆'}}</button><span v-else-if="row.isFavorite" class="favorite">★</span><strong>{{ displayPortName(row) }}</strong><el-tag size="small" effect="plain">{{ portTypeLabel(row.portType) }}</el-tag></div><div class="secondary">{{ secondaryPortName(row) }}</div></template></el-table-column>
          <el-table-column :label="t('ports.country')" min-width="115"><template #default="{row}">{{ countryName(row.countryCode, locale) }} · {{row.countryCode}}</template></el-table-column>
          <el-table-column :label="t('ports.location')" min-width="130"><template #default="{row}">{{row.city||'—'}}<div v-if="row.adminArea" class="secondary">{{row.adminArea}}</div></template></el-table-column>
          <el-table-column prop="timezone" :label="t('ports.timezone')" min-width="130" />
          <el-table-column :label="t('common.status')" width="78"><template #default="{row}"><el-tag :type="row.status==='ACTIVE'?'success':'info'" size="small">{{row.status==='ACTIVE'?t('common.active'):t('common.inactive')}}</el-tag></template></el-table-column>
          <el-table-column :label="t('common.actions')" width="182" fixed="right"><template #default="{row}"><el-button link type="primary" @click="openChanges(row)">{{t('suppliers.changes')}}</el-button><el-button v-if="canWrite" link type="primary" @click="openEdit(row)">{{t('common.edit')}}</el-button><el-button v-if="canWrite" link :type="row.status==='ACTIVE'?'danger':'success'" @click="toggleStatus(row)">{{row.status==='ACTIVE'?t('common.deactivate'):t('common.activate')}}</el-button></template></el-table-column>
        </el-table>
        <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="pageSize" :current-page="page" @current-change="changePage" />
      </el-card>
    </div>

    <el-dialog v-model="dialogOpen" :title="form.id?t('ports.edit'):t('ports.create')" width="640px">
      <el-form label-width="140px">
        <el-form-item :label="t('ports.countryRegion')" required><el-select v-model="form.countryCode" filterable style="width:100%" :disabled="Boolean(form.id)" @change="syncCountry"><el-option v-for="c in countries" :key="c.code" :value="c.code" :label="`${c.name} (${c.code})`"/></el-select></el-form-item>
        <el-form-item :label="t('ports.unlocode')" required><el-select v-model="form.unlocode" filterable allow-create default-first-option style="width:100%" :disabled="Boolean(form.id)" :placeholder="t('ports.unlocodePlaceholder')" @change="applyPortCode"><el-option v-for="port in codeOptions" :key="port.code" :value="port.code" :label="`${port.code} — ${port.nameZh} / ${port.nameEn}`"/></el-select><div class="hint">{{ form.id?t('ports.unlocodeImmutable'):t('ports.unlocodeHelp') }}</div></el-form-item>
        <el-form-item :label="t('ports.portType')" required><el-select v-model="form.portType" style="width:100%"><el-option v-for="type in portTypes" :key="type" :value="type" :label="portTypeLabel(type)"/></el-select></el-form-item>
        <el-form-item :label="t('ports.nameZh')"><el-input v-model="form.nameZh" :placeholder="t('ports.nameZhPlaceholder')" /></el-form-item>
        <el-form-item :label="t('ports.nameEn')"><el-input v-model="form.nameEn" :placeholder="t('ports.nameEnPlaceholder')" /><div class="hint">{{t('ports.nameHelp')}}</div></el-form-item>
        <el-form-item :label="t('ports.city')"><el-select v-model="form.city" filterable allow-create default-first-option clearable style="width:100%" :placeholder="t('ports.cityPlaceholder')"><el-option v-for="city in cityOptions" :key="city" :label="city" :value="city"/></el-select></el-form-item>
        <el-form-item :label="t('ports.adminArea')"><el-input v-model="form.adminArea" :placeholder="t('ports.adminAreaPlaceholder')" /></el-form-item>
        <el-form-item :label="t('ports.timezone')" required><el-select v-model="form.timezone" filterable style="width:100%" :placeholder="t('ports.timezonePlaceholder')"><el-option v-for="zone in timezoneOptions" :key="zone" :label="zone" :value="zone"/></el-select><div class="hint">{{ t('ports.timezoneHelp') }}</div></el-form-item>
        <el-form-item :label="t('ports.aliases')"><el-select v-model="form.aliases" multiple filterable allow-create default-first-option style="width:100%" :placeholder="t('ports.aliasesPlaceholder')" /></el-form-item>
        <el-form-item :label="t('ports.coordinates')"><div class="coordinate-row"><el-input-number v-model="form.latitude" :min="-90" :max="90" :precision="6" :placeholder="t('ports.latitude')"/><el-input-number v-model="form.longitude" :min="-180" :max="180" :precision="6" :placeholder="t('ports.longitude')"/></div><div class="hint">{{t('ports.coordinatesHelp')}}</div></el-form-item>
        <el-form-item :label="t('ports.favorite')"><el-switch v-model="form.isFavorite"/><span class="switch-help">{{t('ports.favoriteHelp')}}</span></el-form-item>
        <el-form-item :label="t('customers.remark')"><el-input v-model="form.remark" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogOpen=false">{{t('common.cancel')}}</el-button><el-button type="primary" :loading="saving" @click="save">{{t('common.save')}}</el-button></template>
    </el-dialog>
    <el-dialog v-model="changeOpen" :title="t('suppliers.changes')" width="760px">
      <el-timeline v-if="changes.length"><el-timeline-item v-for="item in changes" :key="item.id" :timestamp="new Date(item.createdAt).toLocaleString()"><strong>{{item.action}} · {{item.summary}}</strong><p>{{item.operatorName||'—'}}</p><el-collapse><el-collapse-item :title="t('departments.changeValues')"><div class="change-grid"><pre>{{formatChange(item.beforeJson)}}</pre><pre>{{formatChange(item.afterJson)}}</pre></div></el-collapse-item></el-collapse></el-timeline-item></el-timeline>
      <el-empty v-else :description="t('shipping.noChanges')" />
    </el-dialog>
    <ImportPortsDialog v-model="importOpen" @imported="reloadAll" />
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post, put, quietErrors, type Envelope } from '../api'
import { countryName, countryOptions } from '../lib/countries'
import { commonPortOptions, defaultPortTimezone, portCityOptions, portTimezoneOptions } from '../lib/portOptions'
import { useAuthStore } from '../stores/auth'
import ImportPortsDialog from '../components/ImportPortsDialog.vue'
import { confirmDeactivation, promptActivationReason } from '../lib/masterDataLifecycle'

interface Port { id:number; unlocode:string; nameZh:string; nameEn:string; countryCode:string; city:string; timezone:string; aliases:string[]; status:string; remark:string; version:number; portType:string; adminArea:string; latitude:number|null; longitude:number|null; hasCoordinates:boolean; isFavorite:boolean }
const auth=useAuthStore(); const {locale,t}=useI18n(); const countries=computed(()=>countryOptions(String(locale.value))); const canWrite=computed(()=>auth.can('masterdata:port:write'))
const ports=ref<Port[]>([]), total=ref(0), loading=ref(false), saving=ref(false), dialogOpen=ref(false)
const changeOpen=ref(false), changes=ref<any[]>([])
const countryCounts=ref<{countryCode:string;portCount:number}[]>([])
const countryTotal=computed(()=>countryCounts.value.reduce((sum,item)=>sum+Number(item.portCount),0))
const importOpen=ref(false)
const keyword=ref(''), countryCode=ref(''), status=ref('ACTIVE'), page=ref(1), pageSize=100
const empty=():Port=>({id:0,unlocode:'',nameZh:'',nameEn:'',countryCode:'',city:'',timezone:'',aliases:[],status:'ACTIVE',remark:'',version:0,portType:'SEAPORT',adminArea:'',latitude:null,longitude:null,hasCoordinates:false,isFavorite:false})
const portTypes=['SEAPORT','RIVER_PORT','DRY_PORT','AIRPORT','OTHER']
const form=reactive<Port>(empty())
const codeOptions=computed(()=>commonPortOptions(form.countryCode))
const cityOptions=computed(()=>portCityOptions(form.countryCode)); const timezoneOptions=computed(()=>portTimezoneOptions(form.countryCode))
async function load(){loading.value=true;try{const d=await get<{ports:Port[];meta:{total:number}}>('/ports',{keyword:keyword.value,country_code:countryCode.value,status:status.value,page:page.value,page_size:pageSize});ports.value=d.ports??[];total.value=Number(d.meta?.total??0)}finally{loading.value=false}}
async function loadCountries(){const data=await get<{countries:{countryCode:string;portCount:number}[]}>('/ports/countries',{status:status.value});countryCounts.value=data.countries??[]}
function reload(){page.value=1;load()} function changePage(p:number){page.value=p;load()}
function reloadAll(){reload();loadCountries()}
function openCreate(){Object.assign(form,empty());dialogOpen.value=true} function openEdit(p:Port){Object.assign(form,p,{aliases:[...(p.aliases??[])],latitude:p.hasCoordinates?p.latitude:null,longitude:p.hasCoordinates?p.longitude:null});dialogOpen.value=true}
function displayPortName(p:Port){return (String(locale.value).startsWith('zh')?p.nameZh:p.nameEn)||p.nameZh||p.nameEn||p.unlocode}
function secondaryPortName(p:Port){const secondary=String(locale.value).startsWith('zh')?p.nameEn:p.nameZh;return secondary&&secondary!==displayPortName(p)?secondary:''}
function portTypeLabel(type:string){return t(`ports.types.${type||'SEAPORT'}`)}
function applyPortCode(value:string){form.unlocode=String(value).toUpperCase().replace(/[^A-Z0-9]/g,'').slice(0,5);if(form.unlocode.length>=2)form.countryCode=form.unlocode.slice(0,2);const known=commonPortOptions(form.countryCode).find(p=>p.code===form.unlocode);if(known){if(!form.nameZh)form.nameZh=known.nameZh;if(!form.nameEn)form.nameEn=known.nameEn;if(!form.city)form.city=known.city;if(!form.timezone)form.timezone=known.timezone}}
function syncCountry(){if(form.unlocode.length>=2)form.unlocode=form.countryCode+form.unlocode.slice(2);if(!form.timezone)form.timezone=defaultPortTimezone(form.countryCode)}
async function save(){
  if(!form.countryCode||form.unlocode.length!==5||(!form.nameZh&&!form.nameEn)||!form.timezone||!form.portType){
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
    const hasCoordinates=form.latitude!==null&&form.longitude!==null
    const body={port:{...form,latitude:form.latitude??0,longitude:form.longitude??0,hasCoordinates}}
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
    const reason=next==='INACTIVE'?await confirmDeactivation(`/ports/${p.id}/deactivation-impact`,p.unlocode,t):await promptActivationReason(p.unlocode,t)
    await put(`/ports/${p.id}/status`,{status:next,version:p.version,reason},quietErrors)
    ElMessage.success(t('ports.statusUpdated'))
    reloadAll()
  }catch(error){
    if(error==='cancel'||error==='close')return
    const env=error as Envelope<unknown>
    ElMessage.error(env?.message||t('common.requestFailed'))
    if(env?.code==='MD_PORT_VERSION_CONFLICT')reloadAll()
  }
}
async function openChanges(p:Port){changes.value=(await get<any>(`/ports/${p.id}/changes`)).changes??[];changeOpen.value=true}
function formatChange(value:string){try{return JSON.stringify(JSON.parse(value||'{}'),null,2)}catch{return value||'—'}}
async function toggleFavorite(p:Port){
  try{
    await put(`/ports/${p.id}`,{port:{...p,isFavorite:!p.isFavorite,latitude:p.latitude??0,longitude:p.longitude??0}},quietErrors)
    ElMessage.success(t(p.isFavorite?'ports.favoriteRemoved':'ports.favoriteAdded'))
    reloadAll()
  }catch(error){
    const env=error as Envelope<unknown>
    ElMessage.error(env?.message||t('common.requestFailed'))
  }
}
load();loadCountries()
</script>

<style scoped>
.change-grid{display:grid;grid-template-columns:1fr 1fr;gap:12px}.change-grid pre{margin:0;padding:12px;background:#f6f8fa;border-radius:8px;white-space:pre-wrap;overflow:auto}
.page-head{display:flex;align-items:flex-start;justify-content:space-between;margin-bottom:18px}.page-head h2{margin:0 0 5px}.page-head p,.secondary,.hint,.category-help,.vessel-placeholder p{color:#8b95a5}.page-head p{margin:0}.port-workspace{display:grid;grid-template-columns:230px minmax(0,1fr);gap:18px}.port-workspace.collapsed{grid-template-columns:72px minmax(0,1fr)}.category-panel{background:linear-gradient(180deg,#f3f8ff,#fff)}.category-title{font-weight:700;margin-bottom:10px;display:flex;align-items:center;justify-content:space-between}.category-title button{border:0;border-radius:6px;background:var(--el-fill-color);cursor:pointer;color:var(--el-text-color-secondary);font-size:20px}.category-item,.country-item{display:flex;width:100%;border:0;background:transparent;text-align:left;padding:10px 12px;border-radius:8px;font-size:15px;cursor:pointer;justify-content:space-between}.category-item.active,.country-item.active{background:#e8f2ff;color:#409eff;font-weight:700}.country-item{font-size:13px;margin-top:3px}.country-item strong{background:#eef1f5;border-radius:12px;min-width:30px;text-align:center}.category-subtitle{font-size:13px;font-weight:700;margin:18px 0 8px}.category-help{font-size:12px;line-height:1.7;margin-top:14px}.filters{display:flex;gap:12px;margin-bottom:16px}.filters .el-input{max-width:330px}.filters .el-select{width:140px}.secondary{font-size:12px;margin-top:3px}.pager{justify-content:flex-end;margin-top:16px}.hint{font-size:12px;margin-top:5px}.vessel-placeholder{text-align:center}.mobile-category{display:none;margin-bottom:12px}.port-name{display:flex;align-items:center;gap:7px}.favorite,.favorite-button.active{color:#f5a623}.favorite{font-size:17px}.favorite-button{border:0;background:transparent;padding:0;cursor:pointer;color:#a8b0bc;font-size:19px;line-height:1}.favorite-button:hover{color:#f5a623}.coordinate-row{display:flex;gap:12px}.switch-help{margin-left:10px;color:#8b95a5;font-size:12px}@media(max-width:850px){.port-workspace{grid-template-columns:1fr}.category-panel{display:none}.mobile-category{display:block}.filters{flex-wrap:wrap}.coordinate-row{flex-wrap:wrap}}
.port-workspace{display:block}.country-strip{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:9px 12px;margin-bottom:10px;border:1px solid var(--el-border-color-lighter);border-radius:10px;background:var(--el-bg-color)}.country-strip>div{display:flex;flex-direction:column;gap:2px}.country-strip span{font-size:11px;color:var(--el-text-color-secondary)}.country-strip strong{font-size:14px}.country-strip .el-select{width:min(320px,48%)}.port-list :deep(.el-card__body){padding:12px 14px}.filters{align-items:center;flex-wrap:wrap;gap:8px;margin-bottom:10px}.filters .el-input{width:min(300px,28%)}.filters .el-select{width:140px}.filter-actions{display:flex;gap:8px;margin-left:auto}.filter-actions .el-button+.el-button{margin-left:0}.port-list :deep(th.el-table__cell){height:36px;padding:4px 0;font-size:12px}.port-list :deep(td.el-table__cell){padding:6px 0;font-size:12px}.port-list :deep(.el-table__cell .cell){line-height:1.35;padding:0 7px}.port-name{gap:4px;font-size:12px}.port-name strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.secondary{font-size:11px;margin-top:1px}.favorite-button{font-size:16px}.pager{margin-top:8px}
@media(max-width:850px){.filters .el-input{width:100%;max-width:none}.filter-actions{margin-left:0}}
@media(max-width:480px){.country-strip{align-items:stretch;flex-direction:column}.country-strip .el-select{width:100%}.filter-actions{width:100%}.filter-actions .el-button{flex:1}}
</style>
