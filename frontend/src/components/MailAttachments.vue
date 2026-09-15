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
      @contextmenu="emit('excelMenu', $event, a, mailId ?? '')"
      @mouseenter="emit('excelHover', $event, a, mailId ?? '')"
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
        v-if="canPreview(a, mailId ?? '')"
        :content="t('emails.previewFile')"
        placement="top"
        :show-after="0"
        :hide-after="0"
      >
        <button type="button" class="fbtn preview" @click="emit('preview', a, mailId ?? '')">
          <el-icon><View /></el-icon>
          <span>{{ t('emails.previewFile') }}</span>
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

    <!-- 这一封的附件打成一个 zip 拿走。**跟着这一封**，所以会话里每一封各有
         各的一颗：对方发来三个、我们回了两个，就是三个的那一颗和两个的那一颗。
         从前这颗只在阅读区最底下有一颗，打的是「打开的那封」的包，在会话里
         说不清它算谁的（2026-09-15 去掉了那一块）。

         两个以上才给：只有一个附件时它和旁边那颗「下载」是同一件事。
         mailId 为空时不给：打包按信取文件，没有信的编号就取不了——会话里
         「我发出」而本地又没留底的那几条就是这种。 -->
    <button
      v-if="canBundleAttachments(files.length, mailId ?? '')"
      type="button"
      class="bundle"
      :disabled="bundling"
      @click="emit('downloadAll', mailId ?? '')"
    >
      <el-icon><Download /></el-icon>
      <span>{{ t('emails.downloadAll', { n: files.length }) }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { Download, Paperclip, View } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { humanSize } from '../lib/humanSize'
import { canPreview } from '../lib/attachmentPreview'
import { canBundleAttachments } from '../lib/attachmentBundle'
import { attachmentHintKey } from '../lib/attachmentHint'

export interface MailFile {
  id: string
  fileName: string
  fileSize: number | string
  contentType?: string
  downloadUrl?: string
  previewUrl?: string
  // ""/"direct"/"convert"/"office"。见 lib/attachmentPreview。
  previewKind?: string
  stored?: boolean
  // 在浏览器里被改过几回。0 / 不给 = 没人改过。改过的话预览、下载、打包
  // 给的都是最新那一版（2026-09-15 起；之前下载给原件，页面上靠一个「已改」
  // 标记提醒，标记去掉了，行为也统一了）。
  revision?: number | string
}

defineProps<{
  files: MailFile[]
  // 这些附件属于哪封信。会话视图里一屏有好几封，各是各的号——用当前打开的
  // 那一封去请求，找到的会是别人的附件，或者干脆找不到。
  //
  // 「我发出」的那几条：附件在发件那张表里，编号和收件那套各走各的，所以
  // 这里给的是本地「已发送」里留着那一份的号（服务端的 local_mail_id）。
  // 空 = 没对上，那时预览和转 Excel 都不该出现，只能下载。
  mailId?: string
  // 这一封的包正在打。多封信各有各的按钮，所以转圈的是哪一颗由调用方说了算。
  bundling?: boolean
}>()
// 转 Excel 的两个事件都带 mailId：会话视图里一屏有好几封，各带各的附件，
// 页面那头必须知道点的是哪一封的——用「当前打开的那封」去请求，服务器在
// 那封里找不到这个附件，回的是「附件不存在或未保存」（2026-09-15 真发生过）。
const emit = defineEmits<{
  preview: [file: MailFile, mailId: string]
  excelMenu: [event: MouseEvent, file: MailFile, mailId: string]
  excelHover: [event: MouseEvent, file: MailFile, mailId: string]
  excelLeave: []
  // 把这一封的附件打成 zip。带上是哪一封——组件不碰网络，取文件这件事留在
  // 页面那头（两个页面各自有自己的保存方式）。
  downloadAll: [mailId: string]
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
/* 打包那一颗：描边不填色。它和每个文件旁边那颗蓝的「下载」是两件事，
   颜色上就该分得开——一屏里三颗蓝按钮，人会以为点哪个都一样。 */
.bundle {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 6px 11px;
  border: 1px dashed var(--el-border-color);
  border-radius: 999px;
  background: transparent;
  font: inherit;
  font-size: 12px;
  font-weight: 600;
  line-height: 1;
  color: var(--el-text-color-regular);
  cursor: pointer;
  transition: border-color 0.15s ease, color 0.15s ease;
}
.bundle:hover:not(:disabled) {
  border-color: var(--el-color-primary);
  color: var(--el-color-primary);
}
.bundle:disabled {
  cursor: progress;
  opacity: 0.6;
}
.bundle:focus-visible {
  outline: 2px solid var(--el-color-primary-light-5);
  outline-offset: 2px;
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
