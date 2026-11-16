<template>
  <div ref="region" class="column-fill-region" tabindex="-1" @pointerdown="pointerDown" @pointermove="pointerMove" @pointerup="pointerUp" @pointercancel="cancelDrag" @keydown="onKeydown">
    <div v-if="editable && rows.length > 1" class="column-fill-actions">
      <span>{{ hint || labels.hint }}</span>
      <template v-if="selected.length > 1">
        <strong>{{ labels.selected(selected.length) }}</strong>
        <button type="button" @click="fillDown">{{ labels.fill }}</button>
      </template>
      <button v-if="undoBatch" type="button" @click="undo">{{ labels.undo }}</button>
    </div>
    <slot />
  </div>
</template>

<script setup lang="ts">
import {computed,nextTick,onBeforeUnmount,ref,watch} from 'vue'
import {ElMessage} from 'element-plus'
import {useI18n} from 'vue-i18n'

type Field={key:string;column:number}
type Entry={row:object;key:string;value:unknown}
const props=withDefaults(defineProps<{
  rows:object[]
  fields:Field[]
  editable?:boolean
  getCell?:(row:object,key:string)=>unknown
  setCell?:(row:object,key:string,value:unknown)=>void
  canFillRow?:(row:object)=>boolean
  hint?:string
}>(),{editable:true})

const {locale}=useI18n()
const labels=computed(()=>locale.value.startsWith('zh')?{
  hint:'拖动选中同列 · Ctrl+拖动追加 · Ctrl+Enter 填入当前格',selected:(count:number)=>`已选 ${count} 格`,fill:'向下复制首格',undo:'撤销填充',saved:(count:number)=>`已填充 ${count} 格，请保存后生效`,undone:'已撤销上次填充'
}:locale.value.startsWith('es')?{
  hint:'Arrastra para seleccionar una columna · Ctrl+arrastrar para añadir · Ctrl+Enter para rellenar',selected:(count:number)=>`${count} celdas seleccionadas`,fill:'Copiar primera celda hacia abajo',undo:'Deshacer relleno',saved:(count:number)=>`${count} celdas rellenadas; guarda para aplicar`,undone:'Relleno deshecho'
}:{
  hint:'Drag to select a column · Ctrl+drag to add · Ctrl+Enter to fill',selected:(count:number)=>`${count} cells selected`,fill:'Fill down from first cell',undo:'Undo fill',saved:(count:number)=>`Filled ${count} cells; save to apply`,undone:'Last fill undone'
})

const region=ref<HTMLElement|null>(null)
const selected=ref<number[]>([])
const fieldKey=ref('')
const activeIndex=ref(-1)
const undoBatch=ref<Entry[]|null>(null)
let drag:{start:number;key:string;initial:number[];add:boolean;moved:boolean}|null=null
let activeOriginal:unknown

function value(row:object,key:string){return props.getCell?props.getCell(row,key):(row as Record<string,unknown>)[key]}
function setValue(row:object,key:string,next:unknown){if(props.setCell)props.setCell(row,key,next);else (row as Record<string,unknown>)[key]=next}
function bodyRows(){return Array.from(region.value?.querySelectorAll('.el-table__body-wrapper tbody tr.el-table__row')||[])}
function locate(target:EventTarget|null){
  if(!(target instanceof Element)||!region.value)return null
  const td=target.closest('td') as HTMLTableCellElement|null
  const tr=td?.closest('tr.el-table__row')
  if(!td||!tr||!region.value.contains(td))return null
  const index=bodyRows().indexOf(tr)
  const field=props.fields.find(item=>item.column===td.cellIndex)
  return index>=0&&index<props.rows.length&&field&&(!props.canFillRow||props.canFillRow(props.rows[index]))?{index,key:field.key}:null
}
function syncHighlight(){
  for(const [index,tr] of bodyRows().entries()){
    for(const td of Array.from(tr.children) as HTMLTableCellElement[]){
      td.classList.toggle('column-fill-selected',props.editable&&selected.value.includes(index)&&props.fields.some(item=>item.column===td.cellIndex&&item.key===fieldKey.value))
    }
  }
}
function setSelection(indexes:number[],key:string){selected.value=indexes;fieldKey.value=key;syncHighlight()}
function pointerDown(event:PointerEvent){
  if(!props.editable||event.button!==0||event.shiftKey)return
  const cell=locate(event.target)
  if(!cell)return
  const add=event.ctrlKey||event.metaKey
  const initial=add&&fieldKey.value===cell.key?[...selected.value]:[]
  activeOriginal=value(props.rows[cell.index],cell.key)
  drag={start:cell.index,key:cell.key,initial,add,moved:false}
  setSelection(add?[...new Set([...initial,cell.index])]:[cell.index],cell.key)
  activeIndex.value=cell.index
}
function pointerMove(event:PointerEvent){
  if(!drag)return
  const cell=locate(document.elementFromPoint(event.clientX,event.clientY))
  if(!cell||cell.key!==drag.key)return
  if(cell.index!==drag.start)drag.moved=true
  const from=Math.min(drag.start,cell.index),to=Math.max(drag.start,cell.index)
  const range=Array.from({length:to-from+1},(_,offset)=>from+offset).filter(index=>!props.canFillRow||props.canFillRow(props.rows[index]))
  setSelection(drag.add?[...new Set([...drag.initial,...range])]:range,drag.key)
  if(drag.moved)event.preventDefault()
}
function pointerUp(event:PointerEvent){
  if(!drag)return
  pointerMove(event)
  const {start,key,initial,add,moved}=drag
  drag=null
  if(add&&!moved)setSelection(initial.includes(start)?initial.filter(index=>index!==start):[...initial,start],key)
  if(moved)region.value?.focus({preventScroll:true})
}
function cancelDrag(){drag=null}
function eligibleIndexes(){return selected.value.filter(index=>!!props.rows[index]&&(!props.canFillRow||props.canFillRow(props.rows[index])))}
function fill(source:unknown){
  if(!props.editable||selected.value.length<2||!fieldKey.value)return
  const rows=eligibleIndexes().map(index=>props.rows[index])
  if(rows.length<2)return
  undoBatch.value=rows.map(row=>({row,key:fieldKey.value,value:row===props.rows[activeIndex.value]?activeOriginal:value(row,fieldKey.value)}))
  for(const row of rows)setValue(row,fieldKey.value,source)
  if(props.rows[activeIndex.value])activeOriginal=value(props.rows[activeIndex.value],fieldKey.value)
  ElMessage.success(labels.value.saved(rows.length))
  void nextTick(syncHighlight)
}
function fillDown(){const top=Math.min(...eligibleIndexes());const row=props.rows[top];if(row)fill(value(row,fieldKey.value))}
function fillCurrent(){const row=props.rows[activeIndex.value];if(row)fill(value(row,fieldKey.value))}
function undo(){
  if(!undoBatch.value)return
  for(const entry of undoBatch.value)setValue(entry.row,entry.key,entry.value)
  if(props.rows[activeIndex.value])activeOriginal=value(props.rows[activeIndex.value],fieldKey.value)
  undoBatch.value=null
  ElMessage.success(labels.value.undone)
  void nextTick(syncHighlight)
}
function onKeydown(event:KeyboardEvent){
  if(!(event.ctrlKey||event.metaKey)||!props.editable)return
  const key=event.key.toLowerCase()
  if(key==='d'&&selected.value.length>1){event.preventDefault();event.stopPropagation();fillDown()}
  else if(key==='enter'&&selected.value.length>1){event.preventDefault();event.stopPropagation();fillCurrent()}
  else if(key==='z'&&undoBatch.value&&event.target===region.value){event.preventDefault();undo()}
}
watch(()=>props.rows,()=>{setSelection([],'');activeIndex.value=-1;undoBatch.value=null})
watch(()=>props.rows.length,()=>{setSelection([],'');activeIndex.value=-1;undoBatch.value=null})
watch(()=>props.editable,enabled=>{if(!enabled)setSelection([],'')})
onBeforeUnmount(()=>{for(const tr of bodyRows())for(const td of Array.from(tr.children))td.classList.remove('column-fill-selected')})
</script>

<style scoped>
.column-fill-region{min-width:0;outline:none}
.column-fill-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap;min-height:24px;margin:0 0 8px;color:#617487;font-size:12px}
.column-fill-actions strong{color:#176994}
.column-fill-actions button{padding:2px 8px;border:1px solid #b9def1;border-radius:4px;background:#edf8fe;color:#176994;font:inherit;cursor:pointer}
.column-fill-region :deep(td.column-fill-selected){background:#dff1fc!important;box-shadow:inset 0 0 0 2px #43a9df}
</style>
