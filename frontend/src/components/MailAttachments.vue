<!-- 一封邮件带的附件。
     抽出来是因为会话视图里的每一封也要显示它们，而这一块并不只是文件名列表：
     预览、下载、右键转 Excel 的入口都在这里。复制一份出去，日后修的就只会是
     其中一份。 -->
<template>
  <div v-if="files.length" class="files">
    <div
      v-for="a in files"
      :key="a.id"
      class="file"
      :class="{ dead: !a.downloadUrl }"
      :title="hint(a)"
      @contextmenu="emit('excelMenu', $event, a)"
      @mouseenter="emit('excelHover', $event, a)"
      @mouseleave="emit('excelLeave')"
    >
      <el-icon><Paperclip /></el-icon>
      <span class="fname ellipsis">{{ a.fileName }}</span>
      <span class="sub">{{ humanSize(Number(a.fileSize)) }}</span>
      <!-- 看和拿是两件事，所以是两个按钮。预览只对真能显示的东西出现；
           .pptx 或 .zip 的全部交互就是下载。 -->
      <el-tooltip
        v-if="a.previewUrl"
        :content="t('emails.previewFile')"
        placement="top"
        :show-after="0"
        :hide-after="0"
      >
        <button type="button" class="fbtn" @click="emit('preview', a)">
          <el-icon><View /></el-icon>
        </button>
      </el-tooltip>
      <el-tooltip
        v-if="a.downloadUrl"
        :content="t('emails.downloadFile', { f: a.fileName })"
        placement="top"
        :show-after="0"
        :hide-after="0"
      >
        <a class="fbtn" :href="a.downloadUrl" :download="a.fileName">
          <el-icon><Download /></el-icon>
        </a>
      </el-tooltip>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Download, Paperclip, View } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

export interface MailFile {
  id: string
  fileName: string
  fileSize: number | string
  contentType?: string
  downloadUrl?: string
  previewUrl?: string
  stored?: boolean
}

defineProps<{ files: MailFile[] }>()
const emit = defineEmits<{
  preview: [file: MailFile]
  excelMenu: [event: MouseEvent, file: MailFile]
  excelHover: [event: MouseEvent, file: MailFile]
  excelLeave: []
}>()
const { t } = useI18n()

function humanSize(bytes: number) {
  if (!bytes) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

// 三种说法，不能合成两种。
//
// 这两句原本是同一条消息，结果在文件其实好端端、只是某个过期服务没返回那个
// 字段的时候，告诉了人家他那份 8MB 的材料没有留底。对别人的数据这么笃定的
// 一句话，是要有依据才配说的。
function hint(a: MailFile) {
  if (a.downloadUrl) return t('emails.downloadFile', { f: a.fileName })
  return a.stored ? t('emails.fileUnavailable') : t('emails.fileGone')
}
</script>

<style scoped>
.files {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.file {
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: 100%;
  padding: 6px 10px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-lighter);
  font-size: 13px;
}
.file.dead {
  opacity: 0.6;
}
.fname {
  max-width: 260px;
}
.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sub {
  color: var(--el-text-color-secondary);
}
.fbtn {
  display: inline-flex;
  align-items: center;
  padding: 2px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--el-text-color-secondary);
  cursor: pointer;
}
.fbtn:hover {
  color: var(--el-color-primary);
  background: var(--el-fill-color);
}
</style>
