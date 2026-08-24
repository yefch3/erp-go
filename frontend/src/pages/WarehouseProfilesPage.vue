<template>
  <div class="page">
    <header class="hero">
      <div><p class="eyebrow">WAREHOUSE DIRECTORY</p><h1>仓库档案</h1><p>维护公司自有仓、港口仓库和第三方仓库，以及联系人与库存核算方式。</p></div>
      <div><el-button @click="router.push('/warehouses')">返回工作台</el-button><el-button type="primary" @click="openCreate">新建仓库</el-button></div>
    </header>
    <el-card shadow="never" v-loading="loading">
      <div class="filters"><el-input v-model="keyword" clearable placeholder="搜索编码、名称或城市"/><el-select v-model="profileFilter"><el-option label="全部类型" value=""/><el-option v-for="item in profileOptions" :key="item.value" :label="item.label" :value="item.value"/></el-select></div>
      <el-table :data="filteredWarehouses">
        <el-table-column label="仓库"><template #default="{row}"><strong>{{ row.name }}</strong><small>{{ row.code }}</small></template></el-table-column>
        <el-table-column label="类型"><template #default="{row}">{{ profileText(row.profileType) }}</template></el-table-column>
        <el-table-column label="地点"><template #default="{row}">{{ [row.city,row.countryCode].filter(Boolean).join(' · ') || '—' }}</template></el-table-column>
        <el-table-column prop="timezone" label="时区"/>
        <el-table-column label="核算方式"><template #default="{row}">{{ accountingText(row.accountingMode) }}</template></el-table-column>
        <el-table-column label="状态" width="90"><template #default="{row}"><el-tag :type="row.status==='ACTIVE'?'success':'info'">{{ row.status==='ACTIVE'?'启用':'停用' }}</el-tag></template></el-table-column>
        <el-table-column label="操作" width="100"><template #default="{row}"><el-dropdown trigger="click" @command="handleAction($event,row)"><el-button link type="primary">操作⌄</el-button><template #dropdown><el-dropdown-menu><el-dropdown-item command="edit">编辑</el-dropdown-item><el-dropdown-item command="toggle" divided>{{ row.status==='ACTIVE'?'停用':'启用' }}</el-dropdown-item></el-dropdown-menu></template></el-dropdown></template></el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑仓库' : '新建仓库'" width="760px" destroy-on-close>
      <el-form label-width="110px" class="form-grid">
        <el-form-item label="仓库类型" required><el-select v-model="form.profileType"><el-option v-for="item in profileOptions" :key="item.value" :label="item.label" :value="item.value"/></el-select></el-form-item>
        <el-form-item label="仓库编号"><el-input v-if="editingId" v-model="form.code" disabled/><el-alert v-else title="保存后自动生成" type="info" :closable="false" show-icon/></el-form-item>
        <el-form-item label="仓库名称" required><el-input v-model="form.name"/></el-form-item>
        <el-form-item label="国家/地区"><el-select v-model="form.countryCode" filterable clearable @change="syncCountry"><el-option v-for="country in countries" :key="country.code" :label="`${country.name}（${country.code}）`" :value="country.code"/></el-select></el-form-item>
        <el-form-item label="城市"><el-input v-model="form.city"/></el-form-item>
        <el-form-item label="IANA 时区"><el-select v-model="form.timezone" filterable allow-create><el-option v-for="tz in timezoneOptions" :key="tz" :label="tz" :value="tz"/></el-select></el-form-item>
        <el-form-item label="核算方式"><el-select v-model="form.accountingMode"><el-option label="仅记录单据" value="DOCUMENT_ONLY"/><el-option label="同步库存" value="SYNC_INVENTORY"/></el-select></el-form-item>
        <el-form-item label="详细地址" class="full"><el-input v-model="form.address"/></el-form-item>
      </el-form>
      <div class="contact-head"><strong>联系人与内部负责人</strong><div><el-button link type="primary" @click="addOwner">添加内部负责人</el-button><el-button link type="primary" @click="addContact">添加外部联系人</el-button></div></div>
      <div v-for="(contact,index) in form.contacts" :key="index" class="contact-row">
        <el-select v-model="contact.contactType" @change="contactTypeChanged(contact)"><el-option label="内部负责人" value="OWNER"/><el-option label="外部联系人" value="CONTACT"/></el-select>
        <el-select v-if="contact.contactType==='OWNER'" v-model="contact.employeeId" filterable placeholder="选择在职员工" @change="employeeChanged(contact)"><el-option v-for="employee in employees" :key="employee.id" :label="`${employee.code} · ${employee.name}`" :value="employee.id"/></el-select><el-input v-else v-model="contact.name" placeholder="姓名"/>
        <el-input v-model="contact.phone" placeholder="电话" :disabled="contact.contactType==='OWNER'"/><el-input v-model="contact.email" placeholder="邮箱" :disabled="contact.contactType==='OWNER'"/>
        <el-checkbox v-model="contact.isPrimary">主要</el-checkbox><el-button link type="danger" @click="form.contacts.splice(index,1)">删除</el-button>
      </div>
      <template #footer><el-button @click="dialogVisible=false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { get, post, put } from '../api'
import { countryOptions } from '../lib/countries'
import { defaultPortTimezone, portTimezoneOptions } from '../lib/portOptions'

interface Contact { contactType:string; employeeId:string; name:string; phone:string; email:string; isPrimary:boolean; status:string }
interface Warehouse { id:string; code:string; name:string; profileType:string; address:string; countryCode:string; city:string; timezone:string; accountingMode:string; status:string; contacts?:Contact[] }
interface Employee { id:string; code:string; name:string; phone:string; email:string }
const {locale}=useI18n(), router=useRouter(), loading=ref(false), saving=ref(false), dialogVisible=ref(false), editingId=ref(''), keyword=ref(''), profileFilter=ref(''), warehouses=ref<Warehouse[]>([]), employees=ref<Employee[]>([])
const profileOptions=[{label:'公司自有仓',value:'OWN'},{label:'港口仓库',value:'PORT'},{label:'第三方仓库',value:'THIRD_PARTY'}]
const blank=()=>({code:'',name:'',profileType:'OWN',address:'',countryCode:'CN',city:'',timezone:'Asia/Shanghai',accountingMode:'SYNC_INVENTORY',status:'ACTIVE',changeReason:'',contacts:[] as Contact[]})
const form=reactive(blank())
const countries=computed(()=>countryOptions(locale.value))
const timezoneOptions=computed(()=>portTimezoneOptions(form.countryCode))
const filteredWarehouses=computed(()=>warehouses.value.filter(x=>(!profileFilter.value||x.profileType===profileFilter.value)&&(!keyword.value||[x.code,x.name,x.city].join(' ').toLowerCase().includes(keyword.value.toLowerCase()))))
function profileText(v:string){return profileOptions.find(x=>x.value===v)?.label??v}
function accountingText(v:string){return v==='DOCUMENT_ONLY'?'仅记录单据':'同步库存'}
function reset(value=blank()){Object.assign(form,value)}
function openCreate(){editingId.value='';reset();dialogVisible.value=true}
function contactPayload(contact:Contact){return {contactType:contact.contactType,employeeId:contact.employeeId||'0',name:contact.name,phone:contact.phone,email:contact.email,isPrimary:contact.isPrimary,status:contact.status||'ACTIVE'}}
function warehousePayload(source:ReturnType<typeof blank>|Warehouse,status=source.status){return {code:source.code,name:source.name,profileType:source.profileType,address:source.address,countryCode:source.countryCode,city:source.city,timezone:source.timezone,accountingMode:source.accountingMode,status,changeReason:'',contacts:(source.contacts??[]).map(contactPayload)}}
function openEdit(row:Warehouse){editingId.value=row.id;reset(warehousePayload(row));dialogVisible.value=true}
function addOwner(){form.contacts.push({contactType:'OWNER',employeeId:'',name:'',phone:'',email:'',isPrimary:!form.contacts.some(x=>x.contactType==='OWNER'&&x.isPrimary),status:'ACTIVE'})}
function addContact(){form.contacts.push({contactType:'CONTACT',employeeId:'0',name:'',phone:'',email:'',isPrimary:false,status:'ACTIVE'})}
function employeeChanged(contact:Contact){const employee=employees.value.find(x=>String(x.id)===String(contact.employeeId));if(!employee)return;contact.name=employee.name;contact.phone=employee.phone||'';contact.email=employee.email||''}
function contactTypeChanged(contact:Contact){contact.employeeId=contact.contactType==='OWNER'?'':'0';contact.name='';contact.phone='';contact.email=''}
function syncCountry(){form.city='';form.timezone=defaultPortTimezone(form.countryCode)||'UTC'}
async function load(){loading.value=true;try{const data=await get<{warehouses:Warehouse[]}>('/warehouses',{include_inactive:true});warehouses.value=data.warehouses??[]}finally{loading.value=false}}
async function loadEmployees(){try{const data=await get<{employees:Employee[]}>('/employees',{page:1,page_size:200,employment_status:'ACTIVE'});employees.value=data.employees??[]}catch{employees.value=[]}}
function handleAction(command:string,row:Warehouse){if(command==='edit'){openEdit(row);return}if(command==='toggle')void toggleStatus(row)}
async function toggleStatus(row:Warehouse){const next=row.status==='ACTIVE'?'INACTIVE':'ACTIVE';await ElMessageBox.confirm(`确定${next==='ACTIVE'?'启用':'停用'}仓库“${row.name}”吗？`,'状态确认',{type:next==='ACTIVE'?'success':'warning'});await put(`/warehouses/${row.id}`,{warehouse:warehousePayload(row,next)});ElMessage.success(`仓库已${next==='ACTIVE'?'启用':'停用'}`);await load()}
async function save(){if(!form.name.trim()){ElMessage.warning('请填写仓库名称');return}const invalidOwner=form.contacts.find(x=>x.contactType==='OWNER'&&!x.employeeId);if(invalidOwner){ElMessage.warning('请选择内部负责人');return}const invalidContact=form.contacts.find(x=>x.contactType==='CONTACT'&&!x.name.trim());if(invalidContact){ElMessage.warning('请填写外部联系人姓名');return}saving.value=true;try{const body={warehouse:warehousePayload(form)};if(editingId.value)await put(`/warehouses/${editingId.value}`,body);else await post('/warehouses',body);ElMessage.success('仓库档案已保存');dialogVisible.value=false;await load()}finally{saving.value=false}}
onMounted(()=>{void load();void loadEmployees()})
</script>

<style scoped>
.page{padding:28px;max-width:1500px;margin:auto}.hero{display:flex;justify-content:space-between;align-items:center;margin-bottom:20px}.hero h1{margin:3px 0;font-size:30px}.hero p{margin:4px 0;color:#748196}.eyebrow{font-size:12px!important;letter-spacing:2px;color:#087f78!important;font-weight:700}.filters{display:flex;gap:12px;margin-bottom:16px}.filters .el-input{max-width:360px}.filters .el-select{width:180px}small{display:block;color:#8a96a6;margin-top:4px}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:0 14px}.form-grid .full{grid-column:1/-1}.form-grid :deep(.el-select){width:100%}.contact-head{display:flex;justify-content:space-between;border-top:1px solid #e5eaf0;padding:15px 0 10px}.contact-row{display:grid;grid-template-columns:130px 1fr 1fr 1.2fr auto auto;gap:8px;align-items:center;margin-bottom:8px}.contact-row :deep(.el-select){width:100%}@media(max-width:800px){.hero{align-items:flex-start;flex-direction:column;gap:12px}.form-grid{grid-template-columns:1fr}.form-grid .full{grid-column:auto}.contact-row{grid-template-columns:1fr 1fr}.page{padding:16px}}
</style>
