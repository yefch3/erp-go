<!-- 一个办公文档附件，在在线 Office 里打开，自己一个标签页。
     点附件上的「预览」，附件是 Word / Excel / PPT 的时候开的就是这里。

     页面本身很薄：向邮件服务要一份签过名的"打开这个文件"配置，把 OnlyOffice
     的编辑器脚本装进来，交给它画。

     **默认只读，编辑要明着点一下。** 不这么做的话，一次误触的键盘就能给附件
     添一个版本，而列表上从此挂着「已改」。点了「编辑」之后：改动存成新的一版，
     客户发来的原件永远不动（见迁移 00065 和 app/officeedit.go）。 -->
<template>
  <div class="office-window">
    <header v-if="!failed" class="bar">
      <span class="name ellipsis">{{ title }}</span>
      <!-- 打开的是第几版。0（原件）时不说话：那是常态，说了只是噪音。 -->
      <span v-if="version > 0" class="ver">{{ t('officeWindow.version', { n: version }) }}</span>
      <span class="grow" />
      <!-- 可编辑时这句话必须在：人得知道自己改的东西去了哪儿，以及原件还在。 -->
      <span v-if="editable" class="note">{{ t('officeWindow.editNote') }}</span>
      <el-button v-else-if="canEdit" size="small" type="primary" plain @click="startEditing">
        {{ t('officeWindow.edit') }}
      </el-button>
    </header>
    <div v-if="loading" v-loading="true" class="waiting" />
    <el-result
      v-else-if="failed"
      icon="warning"
      :title="t('officeWindow.failed')"
      :sub-title="t('officeWindow.failedHint')"
    />
    <!-- 编辑器会把这个占位替换成自己的 iframe。 -->
    <div id="office-editor" class="editor" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { get } from '../api'

// OnlyOffice 的脚本挂在 window 上的那个全局。只声明用到的那一点。
interface DocEditorHandle {
  destroyEditor: () => void
}
interface DocsApi {
  DocEditor: new (placeholder: string, config: Record<string, unknown>) => DocEditorHandle
}
declare global {
  interface Window {
    DocsAPI?: DocsApi
  }
}

const route = useRoute()
const { t, locale } = useI18n()

const loading = ref(true)
const failed = ref(false)
const title = ref('')
const editable = ref(false)
const version = ref(0)
// 服务端说不出"能不能编辑"，只能说"这一次是不是可编辑的"。所以按钮先给出来，
// 点了之后如果服务端回的 editable 还是 false（没配内网回程），按钮自己消失
// ——比一开始就藏起来诚实：藏起来的按钮解释不了自己为什么不在。
const canEdit = ref(true)
let editor: DocEditorHandle | null = null

// 这一次编辑有没有真的动过东西。只有动过才值得惊动主窗口重拉。
let touched = false

// 告诉主窗口「这封信的附件变了」，让它把「已改 · 第几版」那个标记刷出来。
// 用 BroadcastChannel 而不是 window.opener：刷新过的窗口 opener 是空的。
const bus = new BroadcastChannel('erp-mail')
function notifyChanged() {
  if (!touched) return
  touched = false
  bus.postMessage({ type: 'mail-changed', id: String(route.params.id || ''), gone: false })
}

// 把 api.js 装进页面。同一个页面只装一次；装完 window.DocsAPI 就有了。
function loadDocsApi(docsUrl: string): Promise<DocsApi> {
  if (window.DocsAPI) return Promise.resolve(window.DocsAPI)
  return new Promise((resolve, reject) => {
    const el = document.createElement('script')
    el.src = `${docsUrl}/web-apps/apps/api/documents/api.js`
    el.onload = () => (window.DocsAPI ? resolve(window.DocsAPI) : reject(new Error('DocsAPI missing')))
    el.onerror = () => reject(new Error('api.js failed to load'))
    document.head.appendChild(el)
  })
}

interface OfficeConfig {
  docsUrl: string
  configJson: string
  token: string
  editable?: boolean
  version?: number
}

async function open(edit: boolean) {
  const mailId = String(route.params.id || '')
  const attId = String(route.params.att || '')
  loading.value = true
  failed.value = false
  try {
    const d = await get<OfficeConfig>(`/inbound-mails/${mailId}/attachments/${attId}/office`, {
      lang: locale.value,
      ...(edit ? { edit: '1' } : {}),
    })
    const cfg = JSON.parse(d.configJson) as Record<string, unknown> & { document?: { title?: string } }
    title.value = cfg.document?.title || t('emails.attachments')
    document.title = title.value
    editable.value = Boolean(d.editable)
    version.value = Number(d.version ?? 0)
    // 要了编辑却没给：这套部署没配回程，按钮留着只会一直骗人。
    if (edit && !d.editable) canEdit.value = false
    const api = await loadDocsApi(d.docsUrl)
    loading.value = false
    // 换模式时先把上一个编辑器拆掉，否则占位已经被它换成 iframe，第二个
    // 编辑器找不到落脚的地方。
    editor?.destroyEditor()
    editor = new api.DocEditor('office-editor', {
      ...cfg,
      token: d.token,
      width: '100%',
      height: '100%',
      events: {
        // 服务器那边取不到文件、或者文件坏了，都从这儿来。
        onError: () => {
          failed.value = true
        },
        // 文档"脏了"和"存下了"走同一个事件：data 为 true 是有人动了东西，
        // 变回 false 是存完了。存完才通知主窗口——存之前通知，它刷出来的
        // 还是旧的版本号。
        onDocumentStateChange: (e: { data?: boolean }) => {
          if (e?.data) {
            touched = true
            return
          }
          notifyChanged()
        },
      },
    })
  } catch {
    // 没配在线 Office、信没了、附件没存、或者脚本装不进来。这一页给不了内容，
    // 说一句话，附件在邮件页上照样能下载。
    failed.value = true
    loading.value = false
  }
}

function startEditing() {
  void open(true)
}

// 人直接关窗口时，存那一下可能还在路上——OnlyOffice 是最后一个人离开之后
// 才回存的。这里补一次通知：主窗口多刷一遍，比漏掉那个标记好。
//
// **只在编辑过的时候补。** 只读地看一眼就关掉是最常见的动作，为它让主窗口
// 白跑一趟列表加一封信的详情，是给一个什么都没发生的操作加网络开销。
function onLeave() {
  if (!editable.value) return
  touched = true
  notifyChanged()
}

onMounted(() => {
  window.addEventListener('pagehide', onLeave)
  void open(route.query.edit === '1')
})
onUnmounted(() => {
  window.removeEventListener('pagehide', onLeave)
  editor?.destroyEditor()
  bus.close()
})
</script>

<style scoped>
.office-window {
  height: 100vh;
  display: flex;
  flex-direction: column;
}
.bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  background: var(--el-bg-color);
  font-size: 13px;
}
.name {
  font-weight: 600;
  max-width: 40%;
}
.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ver {
  color: var(--el-color-primary);
  font-variant-numeric: tabular-nums;
}
.grow {
  flex: 1;
}
.note {
  color: var(--el-text-color-secondary);
}
.waiting {
  flex: 1;
}
.editor {
  flex: 1;
  min-height: 0;
}
</style>
