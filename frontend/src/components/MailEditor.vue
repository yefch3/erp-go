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
import { get, post } from '../api'

interface MailImage {
  id: string
  token: string
  fileName: string
  fileSize: string
  contentType: string
}

const props = defineProps<{ modelValue: string; placeholder?: string }>()
const emit = defineEmits<{ 'update:modelValue': [string]; 'insert-variable': [] }>()

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
  // A pasted picture goes through the upload path, not into the text.
  //
  // Checked before the plain-text branch, because a screenshot on the
  // clipboard usually carries a text/plain flavour too (a file path, or
  // nothing) — taking that first silently swallowed the image and pasted an
  // empty string. This is the thing people expect from Gmail and the reason
  // "why can't I just paste it" kept coming up.
  const file = imageOnClipboard(e.clipboardData)
  if (file) {
    e.preventDefault()
    rememberCaret()
    void uploadImage(file)
    return
  }
  e.preventDefault()
  const text = e.clipboardData?.getData('text/plain') ?? ''
  document.execCommand('insertText', false, text)
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
  nextTick(() => {
    restoreCaret()
    document.execCommand(
      'insertHTML',
      false,
      `<img src="${src}" alt="${alt}" width="160" style="max-width:100%">`,
    )
    emitChange()
  })
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
