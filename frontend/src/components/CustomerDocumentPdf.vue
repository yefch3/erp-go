<template>
  <div v-loading="loading" class="pdf-viewer">
    <div class="pdf-toolbar"><el-button :disabled="pageNumber<=1||loading" @click="show(pageNumber-1)">上一页</el-button><span>{{ pageNumber }} / {{ pages }}</span><el-button :disabled="pageNumber>=pages||loading" @click="show(pageNumber+1)">下一页</el-button></div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
    <div class="pdf-paper"><canvas ref="canvas" aria-label="PDF 资料页面" /></div>
  </div>
</template>
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import type { PDFDocumentProxy, PDFDocumentLoadingTask, RenderTask } from 'pdfjs-dist'
import workerURL from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
const props=defineProps<{source:Blob}>()
const canvas=ref<HTMLCanvasElement|null>(null),loading=ref(true),error=ref(''),pageNumber=ref(1),pages=ref(0)
let document:PDFDocumentProxy|undefined,task:PDFDocumentLoadingTask|undefined,render:RenderTask|undefined,disposed=false
async function show(number:number){
 if(!document||!canvas.value||disposed)return
 loading.value=true;error.value=''
 try{
  const page=await document.getPage(number);if(disposed||!canvas.value)return
  const viewport=page.getViewport({scale:1.5});canvas.value.width=viewport.width;canvas.value.height=viewport.height
  render=page.render({canvas:canvas.value,viewport});await render.promise;pageNumber.value=number
 }catch(e){if(!disposed)error.value='PDF 页面无法显示，请下载文件查看'}finally{loading.value=false}
}
onMounted(async()=>{
 try{const pdf=await import('pdfjs-dist');pdf.GlobalWorkerOptions.workerSrc=workerURL
  const data=new Uint8Array(await props.source.arrayBuffer());if(disposed)return
  task=pdf.getDocument({data,isEvalSupported:false});document=await task.promise;if(disposed){await document.destroy();return};pages.value=document.numPages;await show(1)
 }catch(e){if(!disposed)error.value='PDF 文件无法预览，请下载后查看'}finally{loading.value=false}
})
onBeforeUnmount(()=>{disposed=true;render?.cancel();void task?.destroy()})
</script>
<style scoped>
.pdf-toolbar{display:flex;justify-content:center;align-items:center;gap:20px;margin-bottom:12px}.pdf-paper{overflow:auto;max-height:65vh;background:#e2e8f0;text-align:center;padding:12px}.pdf-paper canvas{max-width:100%;height:auto;background:white}.pdf-viewer{min-height:240px}
</style>
