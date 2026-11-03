<template>
 <div class="clamped-text">
  <p ref="body" class="clamped-text__body" :class="{'is-clamped':!expanded}" :style="{'--clamped-lines':String(lines)}">{{text}}</p>
  <button v-if="expanded||overflowing" type="button" class="clamped-text__toggle" :aria-expanded="expanded" @click="expanded=!expanded">{{t(expanded?'common.collapse':'common.expandAll')}}</button>
 </div>
</template>
<script setup lang="ts">
import {nextTick,onBeforeUnmount,onMounted,ref,watch} from 'vue'
import {useI18n} from 'vue-i18n'
const props=withDefaults(defineProps<{text:string;lines?:number}>(),{lines:3})
const {t}=useI18n()
const body=ref<HTMLElement|null>(null),expanded=ref(false),overflowing=ref(false)
// 收起时内容比框高才算超出，才给「展开全部」。展开后框跟着内容长，量不出来，
// 所以展开期间不量，按钮留着当「收起」。
function measure(){const el=body.value;if(el&&!expanded.value)overflowing.value=el.scrollHeight>el.clientHeight+1}
// 所在页签没打开时框是 0×0，量出来永远「没超出」；切过去那一下尺寸变了，
// ResizeObserver 会再量一次。窗口变窄、换行变多也靠它。
let observer:ResizeObserver|undefined
onMounted(()=>{measure();if(body.value&&typeof ResizeObserver!=='undefined'){observer=new ResizeObserver(measure);observer.observe(body.value)}})
onBeforeUnmount(()=>observer?.disconnect())
// 收起状态下文字变长，框高不变，ResizeObserver 不会响，得自己量。
watch(()=>props.text,()=>void nextTick(measure))
watch(expanded,()=>void nextTick(measure))
</script>
<style scoped>
.clamped-text__body{margin:0;white-space:pre-wrap;overflow-wrap:anywhere}
.clamped-text__body.is-clamped{display:-webkit-box;-webkit-box-orient:vertical;-webkit-line-clamp:var(--clamped-lines);line-clamp:var(--clamped-lines);overflow:hidden}
.clamped-text__toggle{margin-top:4px;padding:0;color:#176b87;background:none;border:0;font:inherit;font-size:12px;cursor:pointer}
.clamped-text__toggle:hover,.clamped-text__toggle:focus-visible{color:#0e79a8;text-decoration:underline;outline:none}
</style>
