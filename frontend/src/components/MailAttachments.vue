<!-- 一封邮件带的附件。
     抽出来是因为会话视图里的每一封也要显示它们，而这一块并不只是文件名列表：
     预览、下载、右键转 Excel 的入口都在这里。复制一份出去，日后修的就只会是
     其中一份。 -->
<template>
  <section v-if="files.length" ref="root" class="attachments">
    <div class="attachment-header">
      <h4>{{ t('emails.attachments') }} <span>· {{ files.length }}</span></h4>
    <button
      v-if="downloadableOnly(files).length > 1"
      type="button"
      class="bundle"
      :disabled="bundling"
      @click="emit('downloadAll', mailId ?? '', files)"
    >
      <el-icon><Download /></el-icon>
      <span>{{ t(saveAsLabel, { n: downloadableOnly(files).length }) }}</span>
    </button>
    </div>
    <div ref="grid" class="files" :class="{ expanded }" :tabindex="expanded ? 0 : undefined" :aria-label="t('emails.attachments')">
    <div
      v-for="a in visibleFiles"
      :key="a.id"
      class="file"
      :class="{ dead: !a.downloadUrl }"
      :title="hint(a)"
      @contextmenu="emit('excelMenu', $event, a, mailId ?? '')"
      @mouseenter="emit('excelHover', $event, a, mailId ?? '')"
      @mouseleave="emit('excelLeave')"
    >
      <el-icon><Paperclip /></el-icon>
      <span class="file-info">
        <span class="fname ellipsis" :title="a.fileName">{{ a.fileName }}</span>
        <span class="sub">{{ humanSize(Number(a.fileSize)) }}</span>
      </span>
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

    </div>
    <button v-if="files.length > collapsedCount" type="button" class="expand-files" :aria-expanded="expanded" @click="toggleExpanded">
      {{ expanded ? t('emails.collapseAttachments') : t('emails.expandAttachments', { n: files.length - collapsedCount }) }}
    </button>
  </section>
</template>

<script setup lang="ts">
import { Download, Paperclip, View } from '@element-plus/icons-vue'
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { humanSize } from '../lib/humanSize'
import { canPreview } from '../lib/attachmentPreview'
import { canPickDirectory, downloadableOnly } from '../lib/saveAttachments'
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

const props = defineProps<{
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
  // 把这一封的附件一次拿走。组件不碰网络，取文件这件事留在页面那头
  // （两个页面各自有自己的保存方式）。
  //
  // 带上文件列表：现在取的是每个附件自己的下载地址，不再是「按信的编号请求
  // 服务端打包」，所以光有 mailId 不够。mailId 仍然带着，出错时要说清是哪
  // 一封。
  downloadAll: [mailId: string, files: MailFile[]]
}>()
const { t } = useI18n()
const root = ref<HTMLElement>()
const grid = ref<HTMLElement>()
const expanded = ref(false)
const columns = ref(2)
const collapsedCount = computed(() => columns.value * 2)
const visibleFiles = computed(() => expanded.value ? props.files : props.files.slice(0, collapsedCount.value))
let observer: ResizeObserver | undefined
onMounted(() => {
  observer = new ResizeObserver(([entry]) => { columns.value = entry.contentRect.width >= 680 ? 2 : 1 })
  if (root.value) observer.observe(root.value)
})
onBeforeUnmount(() => observer?.disconnect())
watch(() => [props.mailId, props.files.map(f => f.id).join(',')], () => {
  expanded.value = false
  if (grid.value) grid.value.scrollTop = 0
})
function toggleExpanded() {
  expanded.value = !expanded.value
  if (grid.value) grid.value.scrollTop = 0
}


// 按钮上写「另存为」还是「下载全部」——**说的是按下去会发生什么**。
//
// 能选文件夹的浏览器上，按下去先弹一个文件夹选择框，那就是另存为；不能选的
// 浏览器上，文件直接落进默认下载目录，那时写「另存为」是骗人。两种浏览器上
// 同一颗按钮写两种字，因为它们做的确实是两件事。
const saveAsLabel = canPickDirectory() ? 'emails.saveAllAs' : 'emails.downloadAll'

// 说哪一句由 lib/attachmentHint 判（那里写着四种说法各自的依据，并且单独
// 测过）；这里只负责把 key 翻成话。
function hint(a: MailFile) {
  return t(`emails.${attachmentHintKey(a)}`, { f: a.fileName })
}
</script>

<style scoped>
.attachments { container-type: inline-size; min-width: 0; }
.attachment-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
.attachment-header h4 { margin: 0; font-size: 14px; line-height: 1.5; }
.attachment-header h4 span { color: var(--el-text-color-secondary); font-weight: 400; }
.files { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.files.expanded { max-height: 260px; overflow-y: auto; overscroll-behavior: contain; padding-right: 4px; }
.file { display: flex; align-items: center; gap: 8px; min-width: 0; padding: 8px 10px; border: 1px solid var(--el-border-color-lighter); border-radius: 8px; background: var(--el-fill-color-lighter); font-size: 13px; }
.file > .el-icon { flex-shrink: 0; }
.file-info { display: flex; flex: 1; min-width: 0; flex-direction: column; gap: 3px; }
.expand-files { display: block; margin: 8px auto 0; padding: 6px 12px; border: 0; background: transparent; color: var(--el-color-primary); font-size: 12px; cursor: pointer; }
.expand-files:focus-visible { outline: 2px solid var(--el-color-primary); outline-offset: 2px; }
@container (max-width: 679px) { .files { grid-template-columns: minmax(0, 1fr); } }
.file.dead {
  opacity: 0.6;
}
.fname {
  display: block;
  font-size: 13px;
}
.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.fbtn {
  box-sizing: border-box;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 5px 8px;
  min-height: 26px;
  flex-shrink: 0;
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
