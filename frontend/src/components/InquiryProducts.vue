<template>
  <div class="product-toolbar">
    <el-input v-model="search" :placeholder="t('inquiryProducts.search')" clearable @input="page=1" />
    <div class="product-summary"><span><b>{{ products.length }}</b> {{ t('inquiryProducts.items') }}</span><span>{{ t('inquiryProducts.totalDemand') }} <b>{{ productTotal(products,'quantity','unit') }}</b></span><span>{{ t('inquiryProducts.totalWeight') }} {{ productTotal(products,'weight') }}</span><span>{{ t('inquiryProducts.totalVolume') }} {{ productTotal(products,'volume') }}</span></div>
  </div>
  <div ref="gridElement" class="product-grid">
    <table :style="{width:`max(100%, ${tableWidth}px)`}">
      <colgroup>
        <col v-if="cargoIds" style="width:56px" />
        <col v-for="field in tableFields" :key="field.fieldKey" :style="{width:`${columnWidths[field.fieldKey]}px`}" />
        <col v-if="editable" style="width:68px" />
      </colgroup>
      <thead>
        <tr>
          <th v-if="cargoIds" scope="col">{{t('inquiryProducts.select')}}</th>
          <th v-for="field in tableFields" :key="field.fieldKey" scope="col" :class="{'product-sticky':field.fieldKey==='product'}" :style="field.fieldKey==='product'?{left:cargoIds?'56px':'0'}:undefined">{{fieldLabel(field)}}<span v-if="field.isRequired" class="required-mark"> *</span></th>
          <th v-if="editable" scope="col">{{t('common.actions')}}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in visibleRows" :key="row.id">
          <td v-if="cargoIds"><el-checkbox :model-value="cargoIds.includes(row.id)" @change="choose(row.id,!!$event)" /></td>
          <td v-for="field in tableFields" :key="field.fieldKey" :class="{'product-sticky':field.fieldKey==='product'}" :style="field.fieldKey==='product'?{left:cargoIds?'56px':'0'}:undefined">
            <div class="product-cell">
              <template v-if="editable&&editingCell===cellKey(row,field.fieldKey)">
                <el-date-picker v-if="field.dataType==='DATE'" :ref="setEditInput" :model-value="value(row,field.fieldKey)" value-format="YYYY-MM-DD" type="date" @update:model-value="setValue(row,field.fieldKey,String($event||''))" @change="stopEditing" @blur="stopEditing"/>
                <el-input v-else :ref="setEditInput" class="inline-editor" :model-value="value(row,field.fieldKey)" :type="field.dataType==='NUMBER'?'number':multilineField(field)?'textarea':'text'" :autosize="multilineField(field)?field.fieldKey==='remarks'?{minRows:1}:{minRows:1,maxRows:8}:undefined" :aria-label="fieldLabel(field)" @update:model-value="setValue(row,field.fieldKey,String($event??''))" @blur="stopEditing" @keydown.esc="stopEditing"/>
              </template>
              <button v-else-if="editable" type="button" class="cell-preview" :class="{'cell-preview--empty':!value(row,field.fieldKey),'cell-preview--clamped':field.fieldKey==='remarks'&&!isTextExpanded(row,field.fieldKey)}" :aria-label="`${fieldLabel(field)}：${value(row,field.fieldKey)||'—'}`" @click="startEditing(row,field.fieldKey)">{{value(row,field.fieldKey)||'—'}}</button>
              <span v-else class="cell-value" :class="{'cell-value--clamped':field.fieldKey==='remarks'&&!isTextExpanded(row,field.fieldKey)}">{{value(row,field.fieldKey)||'—'}}</span>
              <button v-if="field.fieldKey==='remarks'&&canExpandText(row,field.fieldKey)&&editingCell!==cellKey(row,field.fieldKey)" type="button" class="cell-expand" @click="toggleText(row,field.fieldKey)">{{isTextExpanded(row,field.fieldKey)?t('inquiryProducts.collapseText'):t('inquiryProducts.expandText')}}</button>
            </div>
          </td>
          <td v-if="editable"><el-button link type="danger" @click="$emit('remove',products.indexOf(row))">{{t('common.remove')}}</el-button></td>
        </tr>
        <tr v-if="!visibleRows.length"><td class="product-grid-empty" :colspan="tableFields.length+(cargoIds?1:0)+(editable?1:0)">{{t('inquiryProducts.empty')}}</td></tr>
      </tbody>
    </table>
  </div>
  <div class="table-footer"><el-pagination v-model:current-page="page" v-model:page-size="size" :page-sizes="[20,50,100]" :total="filtered.length" layout="total, sizes, prev, pager, next"/></div>
</template>
<script setup lang="ts">
import {computed,nextTick,onBeforeUnmount,onMounted,ref,watch} from 'vue'
import {useI18n} from 'vue-i18n'
import type {TemplateField} from '../lib/inquiryTemplates'
import {productTemplateValue,setProductTemplateValue,productTotal,type Product} from '../lib/inquiryWorkspace'
import {inquiryProductFields,planInquiryColumns} from '../lib/inquiryProductLayout'
const props=defineProps<{products:Product[];editable?:boolean;cargoIds?:string[];fields?:TemplateField[]}>()
const emit=defineEmits<{remove:[index:number];'update:cargoIds':[ids:string[]]}>()
const {t,te}=useI18n()
const search=ref(''),page=ref(1),size=ref(20)
const gridElement=ref<HTMLElement|null>(null),gridWidth=ref(1000)
let gridObserver:ResizeObserver|undefined
onMounted(()=>{
 if(!gridElement.value)return
 gridWidth.value=gridElement.value.clientWidth
 gridObserver=new ResizeObserver(entries=>{gridWidth.value=entries[0]?.contentRect.width||gridWidth.value})
 gridObserver.observe(gridElement.value)
})
onBeforeUnmount(()=>gridObserver?.disconnect())
const editingCell=ref(''),expandedTextCells=ref<string[]>([])
const editInput=ref<{focus:()=>void}|null>(null)
function setEditInput(instance:unknown){editInput.value=instance as {focus:()=>void}|null}
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
const allFields=computed(()=>inquiryProductFields(props.products,props.fields?.length?props.fields:fallbackFields,!!props.editable))
const columnPlan=computed(()=>planInquiryColumns(allFields.value,props.products,Object.fromEntries(allFields.value.map(field=>[field.fieldKey,fieldLabel(field)])),gridWidth.value,(props.cargoIds?56:0)+(props.editable?68:0)))
const tableFields=computed(()=>columnPlan.value.columns)
const columnWidths=computed(()=>columnPlan.value.widths)
const tableWidth=computed(()=>columnPlan.value.totalWidth)
const filtered=computed(()=>{const needle=search.value.trim().toLowerCase();return props.products.filter(p=>!needle||allFields.value.some(f=>productTemplateValue(p,f.fieldKey).toLowerCase().includes(needle)))})
const visibleRows=computed(()=>filtered.value.slice((page.value-1)*size.value,page.value*size.value))
watch(()=>props.editable,()=>{editingCell.value=''})
function choose(id:string,yes:boolean){const ids=props.cargoIds||[];emit('update:cargoIds',yes?[...new Set([...ids,id])]:ids.filter(x=>x!==id))}
function value(product:Product,key:string){return productTemplateValue(product,key)}
function setValue(product:Product,key:string,value:string){setProductTemplateValue(product,key,value)}
function multilineField(field:TemplateField){
 return field.dataType==='TEXT'&&['product','material_standard','specification','remarks'].includes(field.fieldKey)
}
function cellKey(product:Product,key:string){return `${product.id}:${key}`}
function startEditing(product:Product,key:string){
 editingCell.value=cellKey(product,key)
 void nextTick(()=>editInput.value?.focus())
}
function stopEditing(){editingCell.value=''}
function isTextExpanded(product:Product,key:string){return expandedTextCells.value.includes(cellKey(product,key))}
function toggleText(product:Product,key:string){
 const cell=cellKey(product,key)
 expandedTextCells.value=isTextExpanded(product,key)?expandedTextCells.value.filter(item=>item!==cell):[...expandedTextCells.value,cell]
}
function canExpandText(product:Product,key:string){
 const text=value(product,key)
 const visualLength=Array.from(text).reduce((length,char)=>length+(/[^\x00-\x7f]/.test(char)?2:1),0)
 return visualLength>280||text.split('\n').length>8
}
function fieldLabel(field:TemplateField){const key=`inquiryProducts.fields.${field.fieldKey}`;return te(key)?t(key):field.displayName}
</script>
<style scoped>
.product-grid{width:100%;overflow-x:auto;border:1px solid var(--el-border-color-lighter);border-radius:5px}
.product-grid table{width:100%;table-layout:fixed;border-collapse:collapse;font-size:13px}
.product-grid th,.product-grid td{max-width:360px;padding:7px 10px;border-right:1px solid var(--el-border-color-lighter);border-bottom:1px solid var(--el-border-color-lighter);white-space:pre-wrap;word-break:break-word;text-align:left;vertical-align:top}
.product-grid th{position:sticky;top:0;z-index:1;background:var(--el-fill-color-light);font-weight:600}
.product-grid .product-sticky{position:sticky;z-index:2;background:#fff}
.product-grid th.product-sticky{z-index:3;background:var(--el-fill-color-light)}
.product-grid-empty{text-align:center!important;color:#8a99a4}
.product-cell{min-width:0;color:#263f53;font-size:13px;line-height:20px}
.cell-preview,.cell-value{display:block;width:100%;min-height:24px;white-space:pre-wrap;overflow-wrap:anywhere;text-align:left;line-height:20px}
.cell-preview{padding:2px 0;border:0;background:transparent;color:inherit;font:inherit;cursor:text}
.cell-preview:hover{background:#f0f8fc}
.cell-preview:focus-visible,.cell-expand:focus-visible{outline:2px solid #55aee3;outline-offset:2px}
.cell-preview--empty{color:#9aaab5}
.cell-preview--clamped,.cell-value--clamped{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:8;overflow:hidden}
.cell-expand{display:block;margin-top:2px;padding:0;border:0;background:transparent;color:#1684c4;font-size:12px;line-height:18px;cursor:pointer}
.product-cell--numeric .cell-preview,.product-cell--numeric .cell-value{text-align:right}
.inline-editor{width:100%}
.inline-editor :deep(.el-textarea__inner),.inline-editor :deep(.el-input__wrapper){padding:3px 6px;border-radius:4px;box-shadow:0 0 0 1px #64b4e4 inset;background:#fff;color:#263f53;font-size:13px;line-height:20px}
.inline-editor :deep(.el-textarea__inner){resize:none;overflow-wrap:anywhere}
.product-toolbar{display:flex;gap:10px 18px;align-items:center;flex-wrap:wrap;margin:4px 0 14px;padding:12px 14px;background:#f5f9fb;border:1px solid #e0e9ef;border-radius:10px}.product-toolbar .el-input{width:250px}.product-summary{display:flex;gap:8px 18px;align-items:center;flex:1;flex-wrap:wrap;color:#617487;font-size:13px}.product-summary b{color:#173f59}.required-mark{color:#e45b65}.cell-value{color:#263f53}:deep(.el-table){--el-table-header-bg-color:#f4f8fa;--el-table-row-hover-bg-color:#f0f8fa}:deep(.el-table th.el-table__cell){color:#486174;font-weight:600}:deep(.el-input__wrapper){border-radius:6px;box-shadow:0 0 0 1px #d2dee7 inset}
.product-toolbar{margin:0 0 14px;padding:0;border:0;border-radius:0;background:transparent}
.product-grid .product-cell,.product-grid .cell-value{color:var(--el-text-color-regular)}
.table-footer{display:flex;justify-content:flex-end;margin-top:14px}
.el-pagination{margin:0;max-width:100%;overflow-x:auto}
@media(max-width:760px){.product-toolbar{align-items:flex-start}.product-toolbar .el-input{width:100%}.table-footer{justify-content:flex-start}}
</style>
