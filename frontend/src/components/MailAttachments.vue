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
      <!-- 带字、带底色，不是两个灰图标。原来那两个灰图标和文件名、大小混在
           一起，用的人说找不到；绿的是看、蓝的是拿，隔着半个屏幕也分得清。 -->
      <el-tooltip
        v-if="canPreview(a)"
        :content="t('emails.previewFile')"
        placement="top"
        :show-after="0"
        :hide-after="0"
      >
        <button
          type="button"
          class="fbtn preview"
          :disabled="converting === a.id"
          @click="emit('preview', a, mailId ?? '')"
        >
          <el-icon><Loading v-if="converting === a.id" /><View v-else /></el-icon>
          <span>{{ converting === a.id ? t('emails.converting') : t('emails.previewFile') }}</span>
        </button>
      </el-tooltip>
      <el-tooltip
        v-if="a.downloadUrl"
        :content="t('emails.downloadFile', { f: a.fileName })"
        placement="top"
        :show-after="0"
        :hide-after="0"
      >
        <a class="fbtn download" :href="a.downloadUrl" :download="a.fileName">
          <el-icon><Download /></el-icon>
          <span>{{ t('emails.download') }}</span>
        </a>
      </el-tooltip>
    </div>
  </div>
</template>

<script setup lang="ts">
import { Download, Loading, Paperclip, View } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { humanSize } from '../lib/humanSize'
import { canPreview } from '../lib/attachmentPreview'
import { attachmentHintKey } from '../lib/attachmentHint'

export interface MailFile {
  id: string
  fileName: string
  fileSize: number | string
  contentType?: string
  downloadUrl?: string
  previewUrl?: string
  // ""/"direct"/"convert"。见 lib/attachmentPreview。
  previewKind?: string
  stored?: boolean
}

defineProps<{
  files: MailFile[]
  // 正在转换的那个附件的 id。转换要往返服务器，按钮得说一声自己在忙，
  // 不然第一次点 Word 的人会以为没反应，然后连点。
  converting?: string
  // 这些附件属于哪封信。会话视图里一屏有好几封，各是各的号——用当前打开的
  // 那一封去请求，转出来的会是别人的附件。空表示这一组不支持转换预览
  // （我们自己发出去的那些）。
  mailId?: string
}>()
const emit = defineEmits<{
  preview: [file: MailFile, mailId: string]
  excelMenu: [event: MouseEvent, file: MailFile]
  excelHover: [event: MouseEvent, file: MailFile]
  excelLeave: []
}>()
const { t } = useI18n()

// 说哪一句由 lib/attachmentHint 判（那里写着四种说法各自的依据，并且单独
// 测过）；这里只负责把 key 翻成话。
function hint(a: MailFile) {
  return t(`emails.${attachmentHintKey(a)}`, { f: a.fileName })
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
  gap: 4px;
  padding: 3px 9px;
  border: 0;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  line-height: 1;
  color: #fff;
  cursor: pointer;
  text-decoration: none;
  transition: filter 0.15s ease, transform 0.15s ease;
}
.fbtn.preview {
  background: var(--el-color-success);
}
.fbtn.download {
  background: var(--el-color-primary);
}
.fbtn:hover {
  color: #fff;
  filter: brightness(1.08);
  transform: translateY(-1px);
}
.fbtn:active {
  transform: none;
}
.fbtn:disabled {
  cursor: progress;
  filter: none;
  opacity: 0.7;
  transform: none;
}
.fbtn:focus-visible {
  outline: 2px solid var(--el-color-primary-light-5);
  outline-offset: 2px;
}
</style>
