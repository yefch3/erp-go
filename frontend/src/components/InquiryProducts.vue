<template>
  <div class="product-toolbar">
    <el-input v-model="search" :placeholder="t('inquiryProducts.search')" clearable @input="page=1" />
    <div class="product-summary"><span><b>{{ products.length }}</b> {{ t('inquiryProducts.items') }}</span><span>{{ t('inquiryProducts.totalDemand') }} <b>{{ productTotal(products,'quantity','unit') }}</b></span><span>{{ t('inquiryProducts.totalWeight') }} {{ productTotal(products,'weight') }}</span><span>{{ t('inquiryProducts.totalVolume') }} {{ productTotal(products,'volume') }}</span></div>
    <el-button-group v-if="!editable" class="view-switch"><el-button :type="mode==='compact'?'primary':'default'" @click="mode='compact'">{{ t('inquiryProducts.compact') }}</el-button><el-button :type="mode==='all'?'primary':'default'" @click="mode='all'">{{ t('inquiryProducts.allFields') }}</el-button></el-button-group>
  </div>
  <div v-if="!editable&&mode==='compact'" class="product-groups">
    <section v-for="group in visibleGroups" :key="group.key" class="product-group">
      <button type="button" class="product-group-heading" :aria-expanded="isExpanded(group.key)" @click="toggleGroup(group.key)">
        <el-icon class="group-chevron" :class="{'is-expanded':isExpanded(group.key)}"><ArrowDown/></el-icon>
        <strong>{{group.name}}</strong>
        <span>{{t('inquiryWorkspace.list.specificationCount',{count:group.specifications.length})}}</span>
        <b>{{t('inquiryWorkspace.list.totalQuantity')}} {{groupedQuantity(group.products)}}</b>
      </button>
      <div v-show="isExpanded(group.key)" class="specification-list">
        <div class="specification-grid specification-header" :class="{'has-selection':!!cargoIds}">
          <span v-if="cargoIds">{{t('inquiryProducts.select')}}</span>
          <span>{{t('inquiryWorkspace.list.materialStandard')}}</span>
          <span>{{t('inquiryWorkspace.list.sizeSpecification')}}</span>
          <span>{{t('inquiryWorkspace.list.quantityUnit')}}</span>
        </div>
        <article v-for="specification in group.specifications" :key="specification.key" class="specification-grid specification-row" :class="{'has-selection':!!cargoIds}">
          <el-checkbox v-if="cargoIds" :model-value="specificationSelected(specification.products)" @change="chooseProducts(specification.products,!!$event)"/>
          <span class="specification-material" :title="materialSummary(specification.representative)">{{materialSummary(specification.representative)||'—'}}</span>
          <span class="specification-size" :title="dimensionSummary(specification.representative)||specification.representative.specification">{{dimensionSummary(specification.representative)||specification.representative.specification||'—'}}</span>
          <strong class="specification-quantity">{{groupedQuantity(specification.products)}}</strong>
          <div v-if="specificationAuxiliaryFields(specification.representative,allFields).length" class="specification-auxiliary">
            <span v-for="field in specificationAuxiliaryFields(specification.representative,allFields)" :key="field.key"><b>{{listFieldLabel(field.label)}}</b>{{field.value}}</span>
          </div>
        </article>
      </div>
    </section>
    <div v-if="!visibleGroups.length" class="product-empty">{{t('inquiryProducts.empty')}}</div>
  </div>
  <el-table v-else :data="visibleRows" row-key="id" :max-height="520" :empty-text="t('inquiryProducts.empty')">
    <el-table-column type="index" label="#" width="52" :index="indexNumber" fixed="left" />
    <el-table-column v-if="cargoIds" :label="t('inquiryProducts.select')" width="65"><template #default="{row}"><el-checkbox :model-value="cargoIds.includes(row.id)" @change="choose(row.id,!!$event)" /></template></el-table-column>
    <el-table-column v-for="field in tableFields" :key="field.fieldKey" :min-width="fieldWidth(field)" :width="field.fieldKey==='product'?productColumnWidth:undefined" :fixed="field.fieldKey==='product'?'left':undefined" show-overflow-tooltip>
      <template #header><span>{{fieldLabel(field)}}</span><span v-if="field.isRequired" class="required-mark"> *</span></template>
      <template #default="{row}"><el-input v-if="editable&&field.fieldKey==='product'" class="product-name-input" :model-value="value(row,field.fieldKey)" type="textarea" :autosize="{minRows:1}" :aria-label="fieldLabel(field)" @update:model-value="setValue(row,field.fieldKey,String($event??''))"/><el-date-picker v-else-if="editable&&field.dataType==='DATE'" :model-value="value(row,field.fieldKey)" value-format="YYYY-MM-DD" type="date" @update:model-value="setValue(row,field.fieldKey,String($event||''))"/><el-input v-else-if="editable" :model-value="value(row,field.fieldKey)" :type="field.dataType==='NUMBER'?'number':'text'" @update:model-value="setValue(row,field.fieldKey,String($event??''))"/><span v-else class="cell-value" :class="{'product-name-value':field.fieldKey==='product'}">{{value(row,field.fieldKey)||'—'}}</span></template>
    </el-table-column>
    <el-table-column v-if="editable" :label="t('common.actions')" width="75" fixed="right"><template #default="{row}"><el-button link type="danger" @click="$emit('remove',products.indexOf(row))">{{ t('common.remove') }}</el-button></template></el-table-column>
  </el-table>
  <div class="table-footer"><span v-if="mode==='compact'" class="expand-tip">{{ t('inquiryProducts.expandTip') }}</span><el-pagination v-model:current-page="page" v-model:page-size="size" :page-sizes="[20,50,100]" :total="paginationTotal" layout="total, sizes, prev, pager, next"/></div>
</template>
<script setup lang="ts">
import {computed,ref,watch} from 'vue'
import {useI18n} from 'vue-i18n'
import {ArrowDown} from '@element-plus/icons-vue'
import type {TemplateField} from '../lib/inquiryTemplates'
import {productTemplateValue,setProductTemplateValue,productTotal,type Product} from '../lib/inquiryWorkspace'
import {dimensionSummary,groupedQuantity,groupInquiryProducts,materialSummary,specificationAuxiliaryFields} from '../lib/inquiryList'
const props=defineProps<{products:Product[];editable?:boolean;cargoIds?:string[];fields?:TemplateField[]}>()
const emit=defineEmits<{remove:[index:number];'update:cargoIds':[ids:string[]]}>()
const {t,te}=useI18n()
const search=ref(''),page=ref(1),size=ref(20),mode=ref<'compact'|'all'>(props.editable?'all':'compact'),expandedGroups=ref<string[]>([])
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
const tableFields=computed(()=>allFields.value)
// Use all products so filtering and pagination do not change the column width.
// Include input padding; very long names wrap once the column reaches its cap.
const productColumnWidth=computed(()=>{
 let width=180
 for(const product of props.products){
  for(const line of productTemplateValue(product,'product').split('\n')){
   const textWidth=Array.from(line).reduce((total,char)=>total+(/[^\x00-\x7f]/.test(char)?16:9),0)
   width=Math.max(width,textWidth+60)
  }
 }
 return Math.min(420,width)
})
const filtered=computed(()=>{const needle=search.value.trim().toLowerCase();return props.products.filter(p=>!needle||allFields.value.some(f=>productTemplateValue(p,f.fieldKey).toLowerCase().includes(needle)))})
const filteredGroups=computed(()=>groupInquiryProducts(filtered.value))
const visibleGroups=computed(()=>filteredGroups.value.slice((page.value-1)*size.value,page.value*size.value))
const visibleRows=computed(()=>filtered.value.slice((page.value-1)*size.value,page.value*size.value))
const paginationTotal=computed(()=>!props.editable&&mode.value==='compact'?filteredGroups.value.length:filtered.value.length)
watch(()=>props.editable,editable=>{mode.value=editable?'all':'compact'})
function choose(id:string,yes:boolean){const ids=props.cargoIds||[];emit('update:cargoIds',yes?[...new Set([...ids,id])]:ids.filter(x=>x!==id))}
function chooseProducts(products:Product[],yes:boolean){let ids=[...(props.cargoIds||[])];for(const product of products)ids=yes?[...new Set([...ids,product.id])]:ids.filter(id=>id!==product.id);emit('update:cargoIds',ids)}
function specificationSelected(products:Product[]){return products.every(product=>(props.cargoIds||[]).includes(product.id))}
function isExpanded(key:string){return expandedGroups.value.includes(key)}
function toggleGroup(key:string){expandedGroups.value=isExpanded(key)?expandedGroups.value.filter(value=>value!==key):[...expandedGroups.value,key]}
function value(product:Product,key:string){return productTemplateValue(product,key)}
function setValue(product:Product,key:string,value:string){setProductTemplateValue(product,key,value)}
function fieldWidth(field:TemplateField){return field.dataType==='DATE'?170:Math.max(110,Math.min(240,field.displayName.length*18+54))}
function fieldLabel(field:TemplateField){const key=`inquiryProducts.fields.${field.fieldKey}`;return te(key)?t(key):field.displayName}
function listFieldLabel(label:string){const key=`inquiryProducts.fields.${label}`;return te(key)?t(key):label}
function indexNumber(index:number){return(page.value-1)*size.value+index+1}
</script>
<style scoped>
.product-name-input{width:100%}
.product-name-input :deep(.el-textarea__inner){padding:6px 11px;line-height:22px;font-size:14px;resize:none;border-radius:6px;overflow-wrap:anywhere;box-shadow:0 0 0 1px #d2dee7 inset}
.product-name-value{display:block;white-space:pre-wrap;overflow-wrap:anywhere;line-height:22px}
.product-toolbar{display:flex;gap:10px 18px;align-items:center;flex-wrap:wrap;margin:4px 0 14px;padding:12px 14px;background:#f5f9fb;border:1px solid #e0e9ef;border-radius:10px}.product-toolbar .el-input{width:250px}.product-summary{display:flex;gap:8px 18px;align-items:center;flex:1;flex-wrap:wrap;color:#617487;font-size:13px}.product-summary b{color:#173f59}.view-switch{margin-left:auto}.required-mark{color:#e45b65}.cell-value{color:#263f53}:deep(.el-table){--el-table-header-bg-color:#f4f8fa;--el-table-row-hover-bg-color:#f0f8fa}:deep(.el-table th.el-table__cell){color:#486174;font-weight:600}:deep(.el-input__wrapper){border-radius:6px;box-shadow:0 0 0 1px #d2dee7 inset}
.product-groups{overflow:hidden;border:1px solid #dbe7ed;border-radius:10px;background:#f7fafb}.product-group{background:#fff;border-bottom:1px solid #dbe7ed}.product-group:last-child{border-bottom:0}.product-group-heading{display:grid;grid-template-columns:20px minmax(180px,1.6fr) minmax(100px,.65fr) minmax(150px,.85fr);align-items:center;gap:12px;width:100%;min-height:50px;padding:8px 16px;color:#526b7a;background:#fff;border:0;font:inherit;text-align:left;cursor:pointer;transition:background-color .18s ease}.product-group-heading:hover,.product-group-heading:focus-visible{background:#f1f8fb;outline:none}.product-group-heading strong{overflow:hidden;color:#163f57;text-overflow:ellipsis;white-space:nowrap}.product-group-heading span{font-size:13px}.product-group-heading b{color:#17475e;text-align:right;white-space:nowrap}.group-chevron{color:#688596;transition:transform .2s ease}.group-chevron.is-expanded{transform:rotate(180deg)}.specification-list{border-top:1px solid #e0ebf0}.specification-grid{display:grid;grid-template-columns:minmax(160px,.85fr) minmax(230px,1.25fr) minmax(130px,.65fr);align-items:center;gap:0 20px;padding:0 36px}.specification-grid.has-selection{grid-template-columns:42px minmax(150px,.85fr) minmax(220px,1.25fr) minmax(125px,.65fr)}.specification-header{min-height:34px;color:#7a8d99;background:#f7fafb;font-size:12px;font-weight:650}.specification-row{min-height:52px;padding-top:8px;padding-bottom:8px;border-top:1px solid #edf2f4}.specification-header+.specification-row{border-top:0}.specification-material,.specification-size{overflow:hidden;color:#344f5f;text-overflow:ellipsis;white-space:nowrap}.specification-quantity{color:#173f56;text-align:right;white-space:nowrap}.specification-auxiliary{display:flex;grid-column:1/-1;gap:4px 16px;flex-wrap:wrap;margin-top:6px;color:#718390;font-size:12px}.specification-auxiliary span{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.specification-auxiliary b{margin-right:5px;color:#536d7d;font-weight:600}.product-empty{padding:22px;color:#8a99a4;text-align:center}.table-footer{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-top:14px}.expand-tip{color:#7a8b9a;font-size:12px}.el-pagination{margin:0 0 0 auto}@media(max-width:760px){.product-toolbar{align-items:flex-start}.product-toolbar .el-input{width:100%}.view-switch{margin-left:0}.product-group-heading{grid-template-columns:20px minmax(120px,1fr) auto;padding-left:10px;padding-right:10px}.product-group-heading b{grid-column:2/-1;text-align:left}.specification-grid,.specification-grid.has-selection{grid-template-columns:minmax(110px,.85fr) minmax(150px,1.15fr) auto;gap:0 10px;min-width:540px;padding-left:14px;padding-right:14px}.specification-grid.has-selection{grid-template-columns:36px minmax(105px,.8fr) minmax(145px,1.1fr) auto}.specification-list{overflow-x:auto}.table-footer{align-items:flex-start;flex-direction:column}.el-pagination{margin:0;overflow-x:auto;max-width:100%}}
</style>
