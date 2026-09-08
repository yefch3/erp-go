<template>
 <div class="fx-page">
  <header><div><h1>汇率</h1><p>查看系统参考汇率，确认本公司报价使用的有效汇率。</p></div><div><el-button :loading="loading" @click="load">刷新</el-button><el-button v-if="auth.can('fx:rate:write')" type="primary" @click="edit()">确认有效汇率</el-button></div></header>
  <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
  <el-table :data="rows" v-loading="loading" max-height="620" stripe empty-text="暂无汇率，可手工录入并确认有效值">
   <el-table-column label="币种对" min-width="145"><template #default="{row}">1 {{row.baseCurrency}} → {{row.quoteCurrency}}</template></el-table-column>
   <el-table-column label="系统参考值" min-width="140"><template #default="{row}">{{row.systemRate||'暂无参考值'}}</template></el-table-column>
   <el-table-column label="有效汇率" min-width="140"><template #default="{row}"><strong v-if="row.rate">{{row.rate}}</strong><el-tag v-else type="warning">尚未确认</el-tag></template></el-table-column>
   <el-table-column label="系统更新时间" min-width="180"><template #default="{row}">{{date(row.systemUpdatedAt)}}</template></el-table-column>
   <el-table-column prop="confirmedBy" label="确认人" min-width="100" />
   <el-table-column label="确认时间" min-width="180"><template #default="{row}">{{date(row.confirmedAt)}}</template></el-table-column>
   <el-table-column prop="remark" label="备注" min-width="180" show-overflow-tooltip />
   <el-table-column v-if="auth.can('fx:rate:write')" label="操作" width="120" fixed="right"><template #default="{row}"><el-button link type="primary" @click="edit(row)">{{row.rate?'修改有效值':'确认有效值'}}</el-button></template></el-table-column>
  </el-table>
  <p class="footnote">系统参考值刷新后，有效汇率保持不变。已确认报价和合同保留当时的汇率。</p>
  <el-dialog v-model="open" title="确认有效汇率" width="min(520px, 94vw)" :close-on-click-modal="false">
   <el-form label-position="top" @submit.prevent="save">
    <div class="pair"><el-form-item label="基础币种"><el-input v-model="form.base" maxlength="3" placeholder="USD" /></el-form-item><el-form-item label="目标币种"><el-input v-model="form.quote" maxlength="3" placeholder="CNY" /></el-form-item></div>
    <el-form-item :label="`1 ${form.base.toUpperCase()} 可兑换的 ${form.quote.toUpperCase()} 数量`"><el-input v-model="form.rate" inputmode="decimal" placeholder="例如 7.20" /></el-form-item>
    <el-form-item label="备注"><el-input v-model="form.remark" type="textarea" maxlength="2000" :rows="3" /></el-form-item>
   </el-form>
   <template #footer><el-button @click="open=false">取消</el-button><el-button type="primary" :loading="saving" @click="save">确认并生效</el-button></template>
  </el-dialog>
 </div>
</template>
<script setup lang="ts">
import {onMounted,reactive,ref} from 'vue'
import {ElMessage} from 'element-plus'
import {get,post} from '../api'
import {useAuthStore} from '../stores/auth'
interface Rate {baseCurrency:string;quoteCurrency:string;rate:string;confirmedBy:string;confirmedAt:string;remark:string;systemRate:string;systemUpdatedAt:string}
const auth=useAuthStore(),rows=ref<Rate[]>([]),loading=ref(false),saving=ref(false),open=ref(false),error=ref('')
const form=reactive({base:'USD',quote:'CNY',rate:'',remark:''})
const date=(s:string)=>s?new Date(s).toLocaleString():'—'
async function load(){loading.value=true;error.value='';try{rows.value=(await get<{rates:Rate[]}>('/fx/effective')).rates||[]}catch{error.value='汇率加载失败，请重试'}finally{loading.value=false}}
function edit(r?:Rate){Object.assign(form,{base:r?.baseCurrency||'USD',quote:r?.quoteCurrency||'CNY',rate:r?.rate||r?.systemRate||'',remark:r?.remark||''});open.value=true}
async function save(){if(saving.value)return;const base=form.base.trim().toUpperCase(),quote=form.quote.trim().toUpperCase();if(!/^[A-Z]{3}$/.test(base)||!/^[A-Z]{3}$/.test(quote)||base===quote||!/^\d{1,16}(\.\d{1,8})?$/.test(form.rate.trim())||Number(form.rate)<=0){ElMessage.warning('请填写不同币种和正数汇率，最多八位小数');return}saving.value=true;try{await post('/fx/effective',{base_currency:base,quote_currency:quote,rate:form.rate.trim(),remark:form.remark});open.value=false;ElMessage.success('有效汇率已确认');await load()}catch{/* API shows the server error; keep the form for correction. */}finally{saving.value=false}}
onMounted(load)
</script>
<style scoped>
.fx-page{padding:24px;max-width:1600px;margin:auto}header{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:24px}h1{margin:0;color:#173b52}header p,.footnote{color:#63778a}.pair{display:grid;grid-template-columns:1fr 1fr;gap:16px}strong{color:#146d83} .el-alert{margin-bottom:16px}@media(max-width:640px){.fx-page{padding:12px}header{align-items:flex-start;flex-direction:column}}
</style>
