<template>
  <div class="ports-page">
    <el-card class="port-list" shadow="never">
      <div class="port-toolbar">
        <div class="port-heading">
          <h1>{{ t('ports.portCategory') }}</h1>
          <span>{{ total }}</span>
        </div>
        <div class="filters">
          <el-input v-model="keyword" size="small" clearable :aria-label="t('ports.searchPlaceholder')" :placeholder="t('ports.searchShort')" :title="t('ports.searchPlaceholder')" @keyup.enter="reload" @clear="reload" />
          <el-button size="small" @click="reload">{{ t('common.query') }}</el-button>
          <el-select v-model="countryCode" size="small" class="country-select" filterable :placeholder="t('ports.allCountries')" :aria-label="t('ports.countryRegion')" @change="reload">
            <el-option :label="t('ports.allCountries') + '（' + countryTotal + '）'" value="" />
            <el-option v-for="item in countryCounts" :key="item.countryCode" :label="countryName(item.countryCode, locale) + '（' + item.portCount + '）'" :value="item.countryCode" />
          </el-select>
          <el-select v-model="status" size="small" class="status-select" :aria-label="t('common.status')" @change="reloadAll">
            <el-option :label="t('ports.activeOnly')" value="ACTIVE" />
            <el-option :label="t('ports.allStatuses')" value="ALL" />
            <el-option :label="t('ports.inactiveOnly')" value="INACTIVE" />
          </el-select>
        </div>
        <div class="filter-actions">
          <template v-if="canWrite">
            <el-button size="small" @click="importOpen = true">{{ t('ports.bulkImport') }}</el-button>
            <el-button size="small" type="primary" @click="openCreate">{{ t('ports.create') }}</el-button>
          </template>
          <MasterDataBulkDelete entity="PORT" @deleted="reloadAll" />
        </div>
      </div>
      <el-table :data="ports" v-loading="loading" size="small" stripe>
        <el-table-column prop="unlocode" label="UN/LOCODE" width="105" />
        <el-table-column :label="t('ports.portName')" min-width="220">
          <template #header><span class="port-name-heading">{{ t('ports.portName') }}</span></template>
          <template #default="{ row }">
            <div class="port-identity">
              <button v-if="canWrite" type="button" class="favorite-button" :class="{ active: row.isFavorite }" :aria-pressed="Boolean(row.isFavorite)" :aria-label="row.isFavorite ? t('ports.removeFavorite') : t('ports.addFavorite')" :title="row.isFavorite ? t('ports.removeFavorite') : t('ports.addFavorite')" @click.stop="toggleFavorite(row)">
                {{ row.isFavorite ? '★' : '☆' }}
              </button>
              <span v-else-if="row.isFavorite" class="favorite" :aria-label="t('ports.favorite')">★</span>
              <div class="port-names">
                <div class="port-name-line">
                  <strong>{{ displayPortName(row) }}</strong>
                  <el-tag size="small" effect="plain">{{ portTypeLabel(row.portType) }}</el-tag>
                </div>
                <span v-if="secondaryPortName(row)" class="secondary">{{ secondaryPortName(row) }}</span>
              </div>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="t('ports.country')" width="116">
          <template #default="{ row }">{{ countryName(row.countryCode, locale) }} · {{ row.countryCode }}</template>
        </el-table-column>
        <el-table-column :label="t('ports.location')" min-width="128">
          <template #default="{ row }">
            {{ row.city || '—' }}
            <div v-if="row.adminArea" class="secondary">{{ row.adminArea }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="timezone" :label="t('ports.timezone')" min-width="172" />
        <el-table-column :label="t('common.status')" width="70">
          <template #default="{ row }">
            <el-tag :type="row.status === 'ACTIVE' ? 'success' : 'info'" size="small">{{ row.status === 'ACTIVE' ? t('common.active') : t('common.inactive') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('common.actions')" width="111" fixed="right">
          <template #default="{ row }">
            <el-dropdown v-if="canWrite" trigger="click" @command="(command: string) => handleAction(command, row)">
              <el-button size="small" link type="primary">{{ t('ports.moreActions') }} <span class="caret" aria-hidden="true">▼</span></el-button>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item command="changes">{{ t('suppliers.changes') }}</el-dropdown-item>
                  <el-dropdown-item command="edit">{{ t('common.edit') }}</el-dropdown-item>
                  <el-dropdown-item command="status" divided>{{ row.status === 'ACTIVE' ? t('common.deactivate') : t('common.activate') }}</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <el-button v-else size="small" link type="primary" @click="openChanges(row)">{{ t('suppliers.changes') }}</el-button>
          </template>
        </el-table-column>
      </el-table>
      <el-pagination class="pager" layout="total, prev, pager, next" :total="total" :page-size="pageSize" :current-page="page" @current-change="changePage" />
    </el-card>

    <el-dialog v-model="dialogOpen" :title="form.id?t('ports.edit'):t('ports.create')" width="min(960px, 94vw)" top="5vh" append-to-body>
      <el-form class="port-form" label-position="top" size="small" :disabled="saving">
        <el-form-item :label="t('ports.countryRegion')" required><el-select v-model="form.countryCode" filterable style="width:100%" :disabled="Boolean(form.id)" @change="syncCountry"><el-option v-for="c in countries" :key="c.code" :value="c.code" :label="`${c.name} (${c.code})`"/></el-select></el-form-item>
        <el-form-item :label="t('ports.unlocode')" required><el-select v-model="form.unlocode" filterable allow-create default-first-option style="width:100%" :disabled="Boolean(form.id)" :placeholder="t('ports.unlocodePlaceholder')" @change="applyPortCode"><el-option v-for="port in codeOptions" :key="port.code" :value="port.code" :label="`${port.code} — ${port.nameZh} / ${port.nameEn}`"/></el-select><div class="hint">{{ form.id?t('ports.unlocodeImmutable'):t('ports.unlocodeHelp') }}</div></el-form-item>
        <el-form-item :label="t('ports.portType')" required><el-select v-model="form.portType" style="width:100%"><el-option v-for="type in portTypes" :key="type" :value="type" :label="portTypeLabel(type)"/></el-select></el-form-item>
        <el-form-item :label="t('ports.nameZh')"><el-input v-model="form.nameZh" :placeholder="t('ports.nameZhPlaceholder')" /></el-form-item>
        <el-form-item :label="t('ports.nameEn')"><el-input v-model="form.nameEn" :placeholder="t('ports.nameEnPlaceholder')" /><div class="hint">{{t('ports.nameHelp')}}</div></el-form-item>
        <el-form-item :label="t('ports.city')"><el-select v-model="form.city" filterable allow-create default-first-option clearable style="width:100%" :placeholder="t('ports.cityPlaceholder')"><el-option v-for="city in cityOptions" :key="city" :label="city" :value="city"/></el-select></el-form-item>
        <el-form-item :label="t('ports.adminArea')"><el-input v-model="form.adminArea" :placeholder="t('ports.adminAreaPlaceholder')" /></el-form-item>
        <el-form-item :label="t('ports.timezone')" required><el-select v-model="form.timezone" filterable style="width:100%" :placeholder="t('ports.timezonePlaceholder')"><el-option v-for="zone in timezoneOptions" :key="zone" :label="zone" :value="zone"/></el-select><div class="hint">{{ t('ports.timezoneHelp') }}</div></el-form-item>
        <el-form-item :label="t('ports.aliases')"><el-select v-model="form.aliases" multiple filterable allow-create default-first-option style="width:100%" :placeholder="t('ports.aliasesPlaceholder')" /></el-form-item>
        <el-form-item :label="t('ports.coordinates')" class="port-form-wide"><div class="coordinate-row"><el-input-number v-model="form.latitude" :min="-90" :max="90" :precision="6" :placeholder="t('ports.latitude')"/><el-input-number v-model="form.longitude" :min="-180" :max="180" :precision="6" :placeholder="t('ports.longitude')"/></div><div class="hint">{{t('ports.coordinatesHelp')}}</div></el-form-item>
        <el-form-item :label="t('ports.favorite')" class="port-form-wide"><el-switch v-model="form.isFavorite"/><span class="switch-help">{{t('ports.favoriteHelp')}}</span></el-form-item>
        <el-form-item :label="t('customers.remark')" class="port-form-wide"><el-input v-model="form.remark" type="textarea" :rows="3" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="dialogOpen=false">{{t('common.cancel')}}</el-button><el-button type="primary" :loading="saving" @click="save">{{t('common.save')}}</el-button></template>
    </el-dialog>
    <el-dialog v-model="changeOpen" :title="t('suppliers.changes')" width="min(760px, 94vw)">
      <el-timeline v-if="changes.length"><el-timeline-item v-for="item in changes" :key="item.id" :timestamp="new Date(item.createdAt).toLocaleString()"><strong>{{item.action}} · {{item.summary}}</strong><p>{{item.operatorName||'—'}}</p><el-collapse><el-collapse-item :title="t('departments.changeValues')"><div class="change-grid"><pre>{{formatChange(item.beforeJson)}}</pre><pre>{{formatChange(item.afterJson)}}</pre></div></el-collapse-item></el-collapse></el-timeline-item></el-timeline>
      <el-empty v-else :description="t('shipping.noChanges')" />
    </el-dialog>
    <ImportPortsDialog v-model="importOpen" @imported="reloadAll" />
  </div>
</template>

<script setup lang="ts">
import MasterDataBulkDelete from '../components/MasterDataBulkDelete.vue'
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
function handleAction(command: string, port: Port) {
  if (command === 'changes') void openChanges(port)
  if (!canWrite.value) return
  if (command === 'edit') openEdit(port)
  if (command === 'status') void toggleStatus(port)
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
.ports-page { min-width: 0; }
.port-list :deep(.el-card__body) { padding: 12px 14px; }
.port-toolbar, .port-heading, .filters, .filter-actions { display: flex; align-items: center; gap: 7px; }
.port-toolbar, .filters, .filter-actions { flex-wrap: wrap; }
.port-toolbar { margin-bottom: 10px; }
.port-heading { flex: none; }
.port-heading h1 { margin: 0; color: #173b52; font-size: 17px; }
.port-heading span, .secondary { color: var(--el-text-color-secondary); font-size: 12px; }
.filters { flex: 1 0 auto; }
.filters .el-input { width: 152px; }
.country-select { width: 142px; }
.status-select { width: 89px; }
.filter-actions { margin-inline-start: auto; }
.filter-actions .el-button + .el-button { margin-left: 0; }
.filter-actions :deep(.el-button) { min-height: 24px; height: auto; padding-inline: 8px; font-size: 12px; }
.port-list :deep(th.el-table__cell) { background: #f2f7fa; }
.port-name-heading { display: block; padding-inline-start: 22px; text-align: start; }
.port-identity { display: grid; grid-template-columns: 16px minmax(0, 1fr); gap: 6px; text-align: start; }
.port-names { grid-column: 2; min-width: 0; overflow-wrap: anywhere; }
.port-name-line { display: flex; align-items: center; flex-wrap: wrap; gap: 3px 6px; }
.port-name-line strong { font-size: 13px; }
.secondary { font-size: 11px; }
.favorite, .favorite-button { grid-column: 1; align-self: start; font-size: 15px; line-height: 20px; }
.favorite-button { padding: 0; border: 0; color: #61768a; background: transparent; cursor: pointer; }
.favorite, .favorite-button.active, .favorite-button:hover { color: #a66a05; }
.caret { margin-inline-start: 4px; font-size: 8px; }
.pager { justify-content: flex-end; margin-top: 8px; }
.port-form {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0 16px;
  max-height: 70dvh;
  overflow-y: auto;
  padding-inline-end: 6px;
}
.port-form :deep(.el-form-item) { min-width: 0; margin-bottom: 10px; }
.port-form :deep(.el-form-item__label) { margin-bottom: 3px; }
.port-form :deep(.el-select) { width: 100%; }
.port-form-wide { grid-column: 1 / -1; }
.hint, .switch-help { color: var(--el-text-color-secondary); font-size: 12px; line-height: 1.4; overflow-wrap: anywhere; }
.hint { margin-top: 4px; }
.switch-help { margin-inline-start: 8px; }
.coordinate-row { display: flex; flex-wrap: wrap; gap: 8px; width: 100%; }
.change-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.change-grid pre { margin: 0; padding: 9px; background: #f6f8fa; border-radius: 6px; white-space: pre-wrap; overflow-wrap: anywhere; }
@media (max-width: 760px) {
  .port-form { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 520px) {
  .filters { flex-basis: 100%; }
  .filter-actions { margin-inline-start: 0; }
  .port-form, .change-grid { grid-template-columns: minmax(0, 1fr); }
}
</style>
