<template>
 <div class="comparison-specification">
  <strong v-if="!hideName">{{productName||'—'}}</strong>
  <span ref="content" class="specification-preview">{{text||'—'}}</span>
  <el-button v-if="overflowing" link type="primary" class="specification-more" @click="open=true">{{t('inquiryWorkspace.offer.viewFullProductDescription')}}</el-button>
 </div>
 <el-dialog v-model="open" :title="t('inquiryWorkspace.offer.fullProductDescription')" width="min(720px,95vw)" append-to-body destroy-on-close>
  <div class="specification-full">{{text}}</div>
  <template #footer><el-button @click="open=false">{{t('inquiryWorkspace.offer.closeSpecification')}}</el-button><el-button type="primary" @click="copy">{{t('inquiryWorkspace.offer.copyProductDescription')}}</el-button></template>
 </el-dialog>
</template>
<script setup lang="ts">
import {ref,onMounted,onBeforeUnmount,watch,nextTick} from 'vue'
import {useI18n} from 'vue-i18n'
import {ElMessage} from 'element-plus'
const props=defineProps<{productName:string;text:string;hideName?:boolean}>()
const {t}=useI18n(),content=ref<HTMLElement|null>(null),overflowing=ref(false),open=ref(false)
let observer:ResizeObserver|undefined
function measure(){overflowing.value=!!content.value&&content.value.scrollHeight>content.value.clientHeight+1}
onMounted(()=>{observer=new ResizeObserver(measure);if(content.value)observer.observe(content.value);measure()})
watch(()=>props.text,async()=>{await nextTick();measure()})
onBeforeUnmount(()=>observer?.disconnect())
async function copy(){try{await navigator.clipboard.writeText(props.text);ElMessage.success(t('inquiryWorkspace.offer.productDescriptionCopied'))}catch{ElMessage.error(t('inquiryWorkspace.offer.productDescriptionCopyFailed'))}}
</script>
<style scoped>
.comparison-specification{display:grid;gap:4px;min-width:0;text-align:left;line-height:1.5}
.comparison-specification>strong{font-size:13px;color:#173f52;overflow-wrap:anywhere}
.specification-preview{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:3;overflow:hidden;font-size:12px;line-height:1.5;color:#496575;white-space:pre-wrap;overflow-wrap:anywhere}
.specification-more{justify-self:start;height:auto;font-size:11px;padding:0}
.specification-full{max-height:55vh;overflow-y:auto;white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.75;margin-top:12px;padding:12px;background:#f5f8fa;border:1px solid #e0e8ed;border-radius:4px;user-select:text}
</style>
