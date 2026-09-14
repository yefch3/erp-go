<!-- 一个办公文档附件，在在线 Office 里打开，自己一个标签页。
     点附件上的「预览」，附件是 Word / Excel / PPT 的时候开的就是这里。

     页面本身很薄：向邮件服务要一份签过名的"打开这个文件"配置，把 OnlyOffice
     的编辑器脚本装进来，交给它画。文件不经过浏览器——Document Server 拿配置
     里的签名地址自己去取，画好了推过来。只读：能翻、能搜、能选、能复制、
     能下载、能打印，改不了。 -->
<template>
  <div class="office-window">
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
let editor: DocEditorHandle | null = null

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

onMounted(async () => {
  const mailId = String(route.params.id || '')
  const attId = String(route.params.att || '')
  try {
    const d = await get<{ docsUrl: string; configJson: string; token: string }>(
      `/inbound-mails/${mailId}/attachments/${attId}/office`,
      { lang: locale.value },
    )
    const cfg = JSON.parse(d.configJson) as Record<string, unknown> & { document?: { title?: string } }
    document.title = cfg.document?.title || t('emails.attachments')
    const api = await loadDocsApi(d.docsUrl)
    loading.value = false
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
      },
    })
  } catch {
    // 没配在线 Office、信没了、附件没存、或者脚本装不进来。这一页给不了内容，
    // 说一句话，附件在邮件页上照样能下载。
    failed.value = true
    loading.value = false
  }
})

onUnmounted(() => editor?.destroyEditor())
</script>

<style scoped>
.office-window {
  height: 100vh;
  display: flex;
  flex-direction: column;
}
.waiting {
  flex: 1;
}
.editor {
  flex: 1;
  min-height: 0;
}
</style>
