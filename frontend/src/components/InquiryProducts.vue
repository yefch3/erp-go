<template>
  <div class="product-toolbar">
    <el-input v-model="search" placeholder="搜索任一产品字段" clearable @input="page=1" />
    <div class="product-summary"><span><b>{{ products.length }}</b> 项产品</span><span>总需求 <b>{{ productTotal(products,'quantity','unit') }}</b></span><span>总重量 {{ productTotal(products,'weight') }}</span><span>总体积 {{ productTotal(products,'volume') }}</span></div>
    <el-button-group v-if="!editable" class="view-switch"><el-button :type="mode==='compact'?'primary':'default'" @click="mode='compact'">精简视图</el-button><el-button :type="mode==='all'?'primary':'default'" @click="mode='all'">全部字段</el-button></el-button-group>
  </div>
  <el-table :data="visible" row-key="id" :max-height="520" empty-text="暂无产品">
    <el-table-column v-if="mode==='compact'" type="expand" width="46"><template #default="{row}"><div class="product-detail-grid"><div v-for="field in detailFields" :key="field.fieldKey" class="detail-field"><span>{{field.displayName}}</span><strong>{{value(row,field.fieldKey)||'—'}}</strong></div></div></template></el-table-column>
    <el-table-column type="index" label="#" width="52" :index="indexNumber" />
    <el-table-column v-if="cargoIds" label="选择" width="65"><template #default="{row}"><el-checkbox :model-value="cargoIds.includes(row.id)" @change="choose(row.id,!!$event)" /></template></el-table-column>
    <el-table-column v-for="field in tableFields" :key="field.fieldKey" :min-width="fieldWidth(field)" show-overflow-tooltip>
      <template #header><span>{{field.displayName}}</span><span v-if="field.isRequired" class="required-mark"> *</span></template>
      <template #default="{row}"><el-date-picker v-if="editable&&field.dataType==='DATE'" :model-value="value(row,field.fieldKey)" value-format="YYYY-MM-DD" type="date" @update:model-value="setValue(row,field.fieldKey,String($event||''))"/><el-input v-else-if="editable" :model-value="value(row,field.fieldKey)" :type="field.dataType==='NUMBER'?'number':'text'" @update:model-value="setValue(row,field.fieldKey,String($event??''))"/><span v-else class="cell-value">{{value(row,field.fieldKey)||'—'}}</span></template>
    </el-table-column>
    <el-table-column v-if="editable" label="操作" width="75" fixed="right"><template #default="{row}"><el-button link type="danger" @click="$emit('remove',products.indexOf(row))">移除</el-button></template></el-table-column>
  </el-table>
  <div class="table-footer"><span v-if="mode==='compact'" class="expand-tip">点击每行左侧箭头查看该产品的全部字段</span><el-pagination v-model:current-page="page" v-model:page-size="size" :page-sizes="[20,50,100]" :total="filtered.length" layout="total, sizes, prev, pager, next"/></div>
</template>
<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import type {TemplateField} from '../lib/inquiryTemplates'
import {productTemplateValue,setProductTemplateValue,productTotal,type Product} from '../lib/inquiryWorkspace'
const props=defineProps<{products:Product[];editable?:boolean;cargoIds?:string[];fields?:TemplateField[]}>()
const emit=defineEmits<{remove:[index:number];'update:cargoIds':[ids:string[]]}>()
const search=ref(''),page=ref(1),size=ref(20),mode=ref<'compact'|'all'>(props.editable?'all':'compact')
const fallbackFields:TemplateField[]=[
 {fieldKey:'product',displayName:'产品',sortOrder:1,isRequired:true,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:true},
 {fieldKey:'specification',displayName:'规格',sortOrder:2,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:false},
 {fieldKey:'quantity',displayName:'数量',sortOrder:3,isRequired:true,defaultValue:'',dataType:'NUMBER',isCustom:false,isCore:true},
 {fieldKey:'quantity_unit',displayName:'单位',sortOrder:4,isRequired:true,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:true},
 {fieldKey:'delivery',displayName:'交货要求',sortOrder:5,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:false},
 {fieldKey:'weight',displayName:'重量',sortOrder:6,isRequired:false,defaultValue:'',dataType:'NUMBER',isCustom:false,isCore:false},
 {fieldKey:'volume',displayName:'体积',sortOrder:7,isRequired:false,defaultValue:'',dataType:'NUMBER',isCustom:false,isCore:false},
 {fieldKey:'packaging',displayName:'包装方式',sortOrder:8,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:false},
 {fieldKey:'package_quantity',displayName:'包装数量',sortOrder:9,isRequired:false,defaultValue:'',dataType:'NUMBER',isCustom:false,isCore:false},
 {fieldKey:'remarks',displayName:'备注',sortOrder:10,isRequired:false,defaultValue:'',dataType:'TEXT',isCustom:false,isCore:false},
]
const allFields=computed(()=>(props.fields?.length?props.fields:fallbackFields).slice().sort((a,b)=>a.sortOrder-b.sortOrder).filter(field=>!['unit_price','total_price'].includes(field.fieldKey)))
const compactKeys=['product','specification','quantity','quantity_unit','delivery','packaging','remarks']
const compactFields=computed(()=>{const selected=compactKeys.map(key=>allFields.value.find(field=>field.fieldKey===key)).filter((field):field is TemplateField=>!!field);for(const field of allFields.value){if(selected.length>=7)break;if(!selected.some(row=>row.fieldKey===field.fieldKey))selected.push(field)}return selected})
const tableFields=computed(()=>mode.value==='all'||props.editable?allFields.value:compactFields.value)
const detailFields=computed(()=>allFields.value)
const filtered=computed(()=>{const needle=search.value.trim().toLowerCase();return props.products.filter(p=>!needle||allFields.value.some(f=>productTemplateValue(p,f.fieldKey).toLowerCase().includes(needle)))})
const visible=computed(()=>filtered.value.slice((page.value-1)*size.value,page.value*size.value))
watch(()=>props.editable,editable=>{mode.value=editable?'all':'compact'})
function choose(id:string,yes:boolean){const ids=props.cargoIds||[];emit('update:cargoIds',yes?[...new Set([...ids,id])]:ids.filter(x=>x!==id))}
function value(product:Product,key:string){return productTemplateValue(product,key)}
function setValue(product:Product,key:string,value:string){setProductTemplateValue(product,key,value)}
function fieldWidth(field:TemplateField){return field.dataType==='DATE'?170:Math.max(110,Math.min(240,field.displayName.length*18+54))}
function indexNumber(index:number){return(page.value-1)*size.value+index+1}
</script>
<style scoped>
.product-toolbar{display:flex;gap:10px 18px;align-items:center;flex-wrap:wrap;margin:4px 0 14px;padding:12px 14px;background:#f5f9fb;border:1px solid #e0e9ef;border-radius:10px}.product-toolbar .el-input{width:250px}.product-summary{display:flex;gap:8px 18px;align-items:center;flex:1;flex-wrap:wrap;color:#617487;font-size:13px}.product-summary b{color:#173f59}.view-switch{margin-left:auto}.required-mark{color:#e45b65}.cell-value{color:#263f53}:deep(.el-table){--el-table-header-bg-color:#f4f8fa;--el-table-row-hover-bg-color:#f0f8fa}:deep(.el-table th.el-table__cell){color:#486174;font-weight:600}:deep(.el-input__wrapper){border-radius:6px;box-shadow:0 0 0 1px #d2dee7 inset}.product-detail-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:14px 22px;padding:16px 22px;background:#f8fbfc;border-left:3px solid #62a9bb}.detail-field{min-width:0}.detail-field span{display:block;margin-bottom:4px;color:#7a8b9a;font-size:12px}.detail-field strong{display:block;overflow:hidden;color:#29475d;font-size:13px;text-overflow:ellipsis;white-space:nowrap}.table-footer{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-top:14px}.expand-tip{color:#7a8b9a;font-size:12px}.el-pagination{margin:0 0 0 auto}@media(max-width:760px){.product-toolbar{align-items:flex-start}.product-toolbar .el-input{width:100%}.view-switch{margin-left:0}.table-footer{align-items:flex-start;flex-direction:column}.el-pagination{margin:0;overflow-x:auto;max-width:100%}}
</style>
