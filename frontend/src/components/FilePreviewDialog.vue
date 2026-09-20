<template>
  <el-dialog
    :model-value="modelValue"
    :title="title || '资料预览'"
    width="min(1040px, 88vw)"
    class="file-preview-dialog"
    destroy-on-close
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div v-if="objectUrl && previewKind === 'image'" class="file-preview file-preview--image">
      <img :src="objectUrl" :alt="title" />
    </div>
    <iframe
      v-else-if="objectUrl && previewKind === 'frame'"
      class="file-preview file-preview--frame"
      :src="objectUrl"
      :title="title"
    />
    <el-empty v-else description="该文件格式暂不支持在线预览，可下载后查看">
      <el-button v-if="source" type="primary" plain @click="downloadSource">下载资料</el-button>
    </el-empty>
    <template #footer>
      <el-button v-if="source" @click="downloadSource">下载</el-button>
      <el-button type="primary" @click="emit('update:modelValue', false)">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'

const props = defineProps<{
  modelValue: boolean
  source: Blob | File | null
  title: string
  contentType?: string
}>()
const emit = defineEmits<{ (event: 'update:modelValue', value: boolean): void }>()

const objectUrl = ref('')

function releaseObjectUrl() {
  if (!objectUrl.value) return
  URL.revokeObjectURL(objectUrl.value)
  objectUrl.value = ''
}

watch(() => props.source, (source) => {
  releaseObjectUrl()
  if (source) objectUrl.value = URL.createObjectURL(source)
}, { immediate: true })

onBeforeUnmount(releaseObjectUrl)

const normalizedType = computed(() => (props.contentType || props.source?.type || '').toLowerCase())
const extension = computed(() => props.title.split('.').pop()?.toLowerCase() || '')
const previewKind = computed<'image' | 'frame' | 'unsupported'>(() => {
  if (normalizedType.value.startsWith('image/') || ['png', 'jpg', 'jpeg', 'gif', 'webp', 'bmp', 'svg'].includes(extension.value)) return 'image'
  if (normalizedType.value === 'application/pdf' || extension.value === 'pdf') return 'frame'
  if (normalizedType.value.startsWith('text/') || ['txt', 'csv', 'json', 'xml', 'md'].includes(extension.value)) return 'frame'
  return 'unsupported'
})

function downloadSource() {
  if (!objectUrl.value) return
  const anchor = document.createElement('a')
  anchor.href = objectUrl.value
  anchor.download = props.title || '采购资料'
  anchor.click()
}
</script>

<style scoped>
.file-preview { width: 100%; border: 1px solid #dce7ed; border-radius: 8px; background: #f7fafb; }
.file-preview--frame { height: min(70vh, 760px); }
.file-preview--image { display: flex; align-items: center; justify-content: center; min-height: 320px; max-height: 70vh; overflow: auto; }
.file-preview--image img { display: block; max-width: 100%; max-height: 68vh; object-fit: contain; }
</style>
