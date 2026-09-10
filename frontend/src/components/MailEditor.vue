<template>
  <div class="mail-editor">
    <div class="toolbar">
      <!-- Web-safe families only. Offering Roboto or Inter here would be a
           lie: the recipient's mail client has no way to load a webfont, so
           the text silently falls back and the sender never finds out. -->
      <el-select
        v-model="fontFamily"
        size="small"
        style="width: 132px"
        :placeholder="t('editor.font')"
        @change="applyFont"
      >
        <el-option
          v-for="f in FONTS"
          :key="f.stack"
          :label="f.label"
          :value="f.stack"
        >
          <span :style="{ fontFamily: f.stack }">{{ f.label }}</span>
        </el-option>
      </el-select>

      <el-select
        v-model="fontSize"
        size="small"
        style="width: 88px"
        :placeholder="t('editor.size')"
        @change="applySize"
      >
        <el-option v-for="s in SIZES" :key="s" :label="`${s} px`" :value="`${s}px`" />
      </el-select>

      <span class="sep" />

      <el-button-group>
        <el-button size="small" :title="t('editor.bold')" @click="cmd('bold')">
          <b>B</b>
        </el-button>
        <el-button size="small" :title="t('editor.italic')" @click="cmd('italic')">
          <i>I</i>
        </el-button>
        <el-button size="small" :title="t('editor.underline')" @click="cmd('underline')">
          <u>U</u>
        </el-button>
      </el-button-group>

      <el-color-picker
        v-model="color"
        size="small"
        :predefine="SWATCHES"
        @change="applyColor"
      />

      <span class="sep" />

      <el-button-group>
        <el-button size="small" :title="t('editor.bullets')" @click="cmd('insertUnorderedList')">
          ≔
        </el-button>
        <el-button size="small" :title="t('editor.numbers')" @click="cmd('insertOrderedList')">
          1.
        </el-button>
      </el-button-group>

      <el-button size="small" :title="t('editor.link')" @click="addLink">🔗</el-button>
      <el-button size="small" :title="t('editor.image')" @click="imagePickerOpen = true">
        🖼
      </el-button>
      <el-button size="small" :title="t('editor.clear')" @click="cmd('removeFormat')">
        {{ t('editor.clear') }}
      </el-button>

      <span class="grow" />
      <el-button size="small" link type="primary" @click="$emit('insert-variable')">
        {{ t('editor.variable') }}
      </el-button>
    </div>

    <div
      ref="area"
      class="canvas"
      contenteditable="true"
      spellcheck="true"
      :data-placeholder="placeholder"
      @input="emitChange"
      @blur="emitChange"
      @paste="onPaste"
      @drop="onDrop"
      @dragover.prevent
      @keyup="rememberCaret"
      @mouseup="rememberCaret"
    />

    <!-- Images are blocked by default in almost every mail client, so this is
         not a nag: a body that only works once the recipient clicks "show
         images" reads as broken to most of them. -->
    <div v-if="hasImages" class="note">{{ t('editor.imageWarning') }}</div>

    <el-dialog v-model="imagePickerOpen" :title="t('editor.insertImage')" width="560px" append-to-body>
      <el-upload
        :show-file-list="false"
        :before-upload="uploadImage"
        accept="image/png,image/jpeg,image/gif,image/webp"
        drag
      >
        <div class="drop">{{ t('editor.dropImage') }}</div>
      </el-upload>

      <!-- The other way in: an address. Most company logos are already on the
           company website, and asking somebody to download a file first only
           to upload it again is work for no reason. -->
      <div class="from-url">
        <div class="side-title">{{ t('editor.fromUrl') }}</div>
        <div class="url-row">
          <el-input
            v-model="imageUrlInput"
            placeholder="https://…"
            :disabled="importing"
            @keyup.enter="importFromUrl"
          />
          <el-button type="primary" :loading="importing" @click="importFromUrl">
            {{ t('editor.fetch') }}
          </el-button>
        </div>
        <!-- Said plainly, because it is the difference between a signature
             that still works next year and one that does not. -->
        <div class="url-note">{{ t('editor.fromUrlNote') }}</div>
      </div>

      <div v-if="images.length" class="library">
        <div class="side-title">{{ t('editor.library') }}</div>
        <div class="thumbs">
          <button
            v-for="img in images"
            :key="img.id"
            class="thumb"
            type="button"
            @click="insertImage(img)"
          >
            <img :src="imageUrl(img.token)" :alt="img.fileName" />
            <span class="thumb-name">{{ img.fileName }}</span>
          </button>
        </div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post, quietErrors } from '../api'
import { imageWidth, looksLikeTable, tableFromClipboard } from '../lib/pastedTable'

interface MailImage {
  id: string
  token: string
  fileName: string
  fileSize: string
  contentType: string
}

const props = defineProps<{ modelValue: string; placeholder?: string }>()
const emit = defineEmits<{
  'update:modelValue': [string]
  'insert-variable': []
  // Fired with the real dimensions once an inserted image has loaded. The
  // editor reports facts and holds no opinion — whether 1600px is "too big"
  // depends on whether this editor is writing a mail body or a signature,
  // and only the parent knows which it is.
  'image-inserted': [info: { width: number; height: number; bytes: number }]
}>()

const { t } = useI18n()

// Families every mail client can actually resolve. Each is a stack ending in
// a generic, because even these are not guaranteed on every platform.
const FONTS = [
  { label: 'Arial', stack: "Arial, Helvetica, sans-serif" },
  { label: 'Helvetica', stack: "Helvetica, Arial, sans-serif" },
  { label: 'Verdana', stack: "Verdana, Geneva, sans-serif" },
  { label: 'Tahoma', stack: "Tahoma, Verdana, sans-serif" },
  { label: 'Trebuchet', stack: "'Trebuchet MS', Tahoma, sans-serif" },
  { label: 'Georgia', stack: "Georgia, 'Times New Roman', serif" },
  { label: 'Times', stack: "'Times New Roman', Times, serif" },
  { label: 'Courier', stack: "'Courier New', Courier, monospace" },
]
const SIZES = [12, 13, 14, 16, 18, 20, 24, 28]
const SWATCHES = ['#000000', '#333333', '#c0392b', '#2980b9', '#27ae60', '#8e44ad']

const area = ref<HTMLDivElement>()
const fontFamily = ref('')
const fontSize = ref('')
const color = ref('')
const imagePickerOpen = ref(false)
const images = ref<MailImage[]>([])
const hasImages = ref(false)
const imageUrlInput = ref('')
const importing = ref(false)

onMounted(() => {
  // Inline styles rather than classes: Gmail strips <style> blocks outright,
  // so a class-based rule would arrive unstyled.
  document.execCommand('styleWithCSS', false, 'true')
  setHTML(props.modelValue)
  loadImages()
})

// Only push the prop in when it differs, or every keystroke would reset the
// caret to the start of the field.
watch(
  () => props.modelValue,
  (v) => {
    if (area.value && v !== area.value.innerHTML) setHTML(v)
  },
)

function setHTML(v: string) {
  if (!area.value) return
  // mail-sandbox-ok: 这里进来的永远是我们自己写的正文——写信框里敲的字，或者
  // 一封草稿的 body，而草稿存进库时过的是发信那份白名单（mailPolicy），它连
  // <style> 带里面的 CSS 一起丢掉。收到的信的 HTML 到不了这一行；它要么进
  // MailBody 的沙箱，要么先过 stripStylesheets。见 scripts/check-mail-sandbox.sh。
  area.value.innerHTML = v || ''
  hasImages.value = !!area.value.querySelector('img')
}

function emitChange() {
  if (!area.value) return
  rememberCaret()
  hasImages.value = !!area.value.querySelector('img')
  emit('update:modelValue', area.value.innerHTML)
}

// Where the caret was, kept because everything that inserts into this field
// is a control outside it — a toolbar button, a dialog, a variable chip on the
// parent page. Clicking any of them moves focus away, and an insert that does
// not put the selection back lands at the very start of the block.
let savedRange: Range | null = null

function rememberCaret() {
  const sel = window.getSelection()
  if (!sel || sel.rangeCount === 0) return
  const r = sel.getRangeAt(0)
  if (area.value?.contains(r.commonAncestorContainer)) savedRange = r.cloneRange()
}

function restoreCaret() {
  area.value?.focus()
  if (!savedRange) return
  const sel = window.getSelection()
  if (!sel) return
  sel.removeAllRanges()
  sel.addRange(savedRange)
}

// insertText is for the parent: the signature page keeps its variable chips
// outside the editor, so it needs a way in that does not depend on the field
// still holding focus.
function insertText(text: string) {
  restoreCaret()
  document.execCommand('insertText', false, text)
  emitChange()
}

defineExpose({ insertText })

function cmd(name: string) {
  area.value?.focus()
  document.execCommand(name, false)
  emitChange()
}

// Pasting from Word or a web page drags in classes, <style> blocks and
// layout the mail clients cannot render. Taking the plain text and letting
// the sender restyle it is the honest option; the server would strip most of
// it anyway, which would look like the editor losing their work.
function onPaste(e: ClipboardEvent) {
  const dt = e.clipboardData
  // 表格排在图片**之前**判断，这个顺序是 issue #363 的一半。
  //
  // 从 Excel 复制一块区域时，剪贴板里同时有图片和文字。图片先判断的话，
  // 一个表格会被当成截图上传，然后按缩略图宽度插进来——那就是「复制表格
  // 到邮件后显示过小」的由来，它其实根本没被当成表格。
  //
  // 重建用的是纯文本那一份，外来 HTML 一个字节都不进文档，见 lib/pastedTable。
  const clipHTML = dt?.getData('text/html') ?? ''
  const clipText = dt?.getData('text/plain') ?? ''
  if (looksLikeTable(clipHTML, clipText)) {
    e.preventDefault()
    rememberCaret()
    void pasteTable(clipHTML, clipText)
    return
  }
  // A pasted picture goes through the upload path, not into the text.
  //
  // Checked before the plain-text branch, because a screenshot on the
  // clipboard usually carries a text/plain flavour too (a file path, or
  // nothing) — taking that first silently swallowed the image and pasted an
  // empty string. This is the thing people expect from Gmail and the reason
  // "why can't I just paste it" kept coming up.
  const file = imageOnClipboard(dt)
  if (file) {
    e.preventDefault()
    rememberCaret()
    void uploadImage(file)
    return
  }
  e.preventDefault()
  const text = dt?.getData('text/plain') ?? ''
  document.execCommand('insertText', false, text)
  emitChange()
}

// 粘一个表格：把剪贴板那份 HTML 送去服务端净化，回来直接插。
//
// **净化在服务端**，理由见 services/mail/internal/app/pastedtable.go：那段
// HTML 是不可信输入，而净化器是最不能靠"看着对"的一类代码，这边跑在纯 node
// 的测试里连 DOMParser 都没有，写在这儿测不了。
//
// 网络出问题、或者服务端认不出表格（回空串），就退回纯文本重建那条老路——
// 那条路丢格式但一定能用，比"粘了没反应"强得多。
async function pasteTable(clipHTML: string, clipText: string) {
  let html = ''
  try {
    const d = await post<{ table?: string }>(
      '/email-html/clean-table',
      { html: clipHTML },
      quietErrors,
    )
    html = d.table ?? ''
  } catch {
    // 退回下面那条
  }
  if (!html) html = tableFromClipboard(clipHTML, clipText) ?? ''
  restoreCaret()
  if (html) {
    document.execCommand('insertHTML', false, html)
  } else {
    // 连表格都重建不出来：当普通文字粘。
    document.execCommand('insertText', false, clipText)
  }
  emitChange()
}

// The first image among the clipboard's items, or null.
//
// clipboardData.files is empty for a screenshot taken with the system
// shortcut on some platforms, so items has to be walked as well.
function imageOnClipboard(dt: DataTransfer | null): File | null {
  if (!dt) return null
  for (const f of Array.from(dt.files ?? [])) {
    if (f.type.startsWith('image/')) return f
  }
  for (const item of Array.from(dt.items ?? [])) {
    if (item.kind === 'file' && item.type.startsWith('image/')) {
      const f = item.getAsFile()
      if (f) return f
    }
  }
  return null
}

// Dropping a picture onto the body does the same thing as pasting one.
//
// The default would be to navigate the frame to the file, losing whatever was
// being written — which is the worst possible response to a dropped file.
function onDrop(e: DragEvent) {
  const file = imageOnClipboard(e.dataTransfer)
  if (!file) return
  e.preventDefault()
  rememberCaret()
  void uploadImage(file)
}

// execCommand has no reliable font-family or px font-size, so those wrap the
// selection by hand. Bold/italic/lists stay on execCommand, which every
// browser still implements consistently.
function wrapSelection(apply: (el: HTMLSpanElement) => void) {
  const sel = window.getSelection()
  if (!sel || sel.rangeCount === 0 || sel.isCollapsed) {
    ElMessage.info(t('editor.selectFirst'))
    return
  }
  const range = sel.getRangeAt(0)
  const span = document.createElement('span')
  apply(span)
  span.appendChild(range.extractContents())
  range.insertNode(span)
  sel.removeAllRanges()
  emitChange()
}

function applyFont(stack: string) {
  wrapSelection((el) => (el.style.fontFamily = stack))
}

function applySize(size: string) {
  wrapSelection((el) => (el.style.fontSize = size))
}

function applyColor(c: string | null) {
  if (!c) return
  wrapSelection((el) => (el.style.color = c))
}

function addLink() {
  const url = window.prompt(t('editor.linkPrompt'), 'https://')
  if (!url) return
  // Only schemes the server will keep. Refusing here as well means the
  // sender finds out now rather than watching the link vanish on save.
  if (!/^(https?:|mailto:)/i.test(url)) {
    ElMessage.error(t('editor.linkScheme'))
    return
  }
  area.value?.focus()
  document.execCommand('createLink', false, url)
  emitChange()
}

async function loadImages() {
  try {
    const d = await get<{ images: MailImage[] }>('/email-images')
    images.value = d.images ?? []
  } catch {
    // The picker simply shows nothing; composing still works.
  }
}

function imageUrl(token: string) {
  return `/api/public/mail-images/${token}`
}

// Uploads go browser-to-bucket through a presigned URL, so a logo never
// occupies the gateway. Returning false keeps el-upload from also posting it.
async function uploadImage(file: File) {
  try {
    const p = await post<{ fileKey: string; uploadUrl: string }>('/email-images/presign', {
      fileName: file.name,
    })
    const put = await fetch(p.uploadUrl, {
      method: 'PUT',
      body: file,
      headers: { 'Content-Type': file.type },
    })
    if (!put.ok) throw new Error(`upload failed: ${put.status}`)
    const reg = await post<{ image: MailImage }>('/email-images', {
      fileName: file.name,
      fileKey: p.fileKey,
    })
    images.value = [reg.image, ...images.value]
    insertImage(reg.image)
  } catch (e) {
    ElMessage.error(t('editor.uploadFailed'))
  }
  return false
}

// The server fetches the address and keeps a copy, so what gets inserted is
// our own URL either way. Hotlinking would work today and break the day the
// other site is redesigned — in every mail sent up to then, silently, because
// the composer would still show it correctly.
async function importFromUrl() {
  const raw = imageUrlInput.value.trim()
  if (!raw || importing.value) return
  importing.value = true
  try {
    const reg = await post<{ image: MailImage }>('/email-images/from-url', { url: raw })
    images.value = [reg.image, ...images.value]
    imageUrlInput.value = ''
    insertImage(reg.image)
  } catch {
    // The server says why — an unreachable host, a file that is not an image,
    // one over the cap — and the API layer has already shown that sentence.
  } finally {
    importing.value = false
  }
}

function insertImage(img: MailImage) {
  imagePickerOpen.value = false
  // The absolute URL matters: the recipient's mail client has no page to
  // resolve a relative path against.
  const src = `${window.location.origin}${imageUrl(img.token)}`
  // alt is not optional — with images blocked, it is what most recipients
  // see the first time they open the mail.
  const alt = img.fileName.replace(/\.[^.]+$/, '').replace(/"/g, '')

  // 宽度按图片自己的尺寸来，只在超过正文宽度时才收（见 lib/pastedTable 的
  // imageWidth）。
  //
  // **从前这里写死 width="160"。** 一张图不管多大都被插成 160px 的缩略图，
  // 而这个属性会跟着发出去——所以预览和收件人看到的都一样小。issue #363
  // 说的「复制表格过来显示过小」，一半是表格没被当成表格，另一半就是这里。
  //
  // 尺寸要先量到才能写，所以插入挪到 onload 里。量不到（图挂了）就不写
  // width，让浏览器用真实尺寸、max-width 兜上限——写一个猜的数更糟。
  //
  // 量的是**服务出来的那张图**，不是元数据：库里那行从来没记过尺寸，而文件
  // 本身是唯一不会说错的。
  const probe = new Image()
  const insert = (w: number) => {
    nextTick(() => {
      restoreCaret()
      const width = w > 0 ? ` width="${w}"` : ''
      document.execCommand(
        'insertHTML',
        false,
        `<img src="${src}" alt="${alt}"${width} style="max-width:100%;height:auto">`,
      )
      emitChange()
    })
  }
  probe.onload = () => {
    insert(imageWidth(probe.naturalWidth))
    emit('image-inserted', {
      width: probe.naturalWidth,
      height: probe.naturalHeight,
      bytes: Number(img.fileSize) || 0,
    })
  }
  // 图取不到也要把它插进去：地址是对的，可能只是这一刻网络不好，而使用者
  // 刚做的动作不该无声无息地什么都没发生。
  probe.onerror = () => insert(0)
  probe.src = src
}
</script>

<style scoped>
.mail-editor {
  width: 100%;
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  overflow: hidden;
}
.toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  padding: 6px 8px;
  background: var(--el-fill-color-lighter);
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.sep {
  width: 1px;
  height: 18px;
  background: var(--el-border-color);
}
.grow {
  flex: 1;
}
.canvas {
  min-height: 220px;
  max-height: 380px;
  overflow-y: auto;
  padding: 12px 14px;
  font-family: Arial, Helvetica, sans-serif;
  font-size: 14px;
  line-height: 1.6;
  outline: none;
}
.canvas:empty::before {
  content: attr(data-placeholder);
  color: var(--el-text-color-placeholder);
}
.canvas :deep(img) {
  max-width: 100%;
}
.note {
  padding: 6px 12px;
  font-size: 12px;
  color: var(--el-color-warning);
  background: var(--el-color-warning-light-9);
}
.drop {
  padding: 22px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.side-title {
  margin: 14px 0 8px;
  font-size: 13px;
  font-weight: 600;
}
.url-row {
  display: flex;
  gap: 8px;
}
.url-note {
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.thumbs {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.thumb {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  width: 96px;
  padding: 6px;
  background: none;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
  cursor: pointer;
}
.thumb:hover {
  border-color: var(--el-color-primary);
}
.thumb img {
  max-width: 100%;
  max-height: 48px;
  object-fit: contain;
}
.thumb-name {
  font-size: 11px;
  color: var(--el-text-color-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}
</style>
