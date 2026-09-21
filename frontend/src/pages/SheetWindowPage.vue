<!-- 一个表格附件，自己一个标签页。
     点附件上的「预览」开的就是这里。

     **不转 PDF。** 从前的预览是把 .xlsx 送到服务器上的 LibreOffice 转成 PDF
     再显示，那条路有三个毛病：等（一份大表几秒起步）、丑（宽表格被切成好几
     页、列对不上）、死（PDF 里的单元格选不中、搜不了、复制不出来）。而
     .xlsx 说到底是一个 zip 里的几份 XML，浏览器自己就读得动——ERP 里本来就
     有这个读法（导入询盘用的就是它）。

     所以这一页：拿原文件的字节，在浏览器里解出表格，画成 HTML。服务器不参与，
     什么都不落盘，转换出来的中间文件一个都不存在。 -->
<template>
  <div class="sheet-window">
    <header class="bar">
      <span class="fname" :title="fileName">{{ fileName }}</span>
      <span class="grow" />
      <!-- 多个工作表就给一排页签，和 Excel 底下那一排一个意思。 -->
      <span v-if="book && book.sheets.length > 1" class="tabs">
        <button
          v-for="s in book.sheets"
          :key="s.name"
          type="button"
          class="tab"
          :class="{ on: s.name === active }"
          @click="active = s.name"
        >
          {{ s.name }}
        </button>
      </span>
      <a v-if="downloadUrl" class="dl" :href="downloadUrl" :download="fileName">
        {{ t('emails.download') }}
      </a>
    </header>

    <div v-if="loading" v-loading="true" class="waiting" />

    <el-result
      v-else-if="failed"
      icon="warning"
      :title="t(`sheetWindow.${failureKey}`)"
      :sub-title="t(`sheetWindow.${failureKey}Hint`)"
    >
      <template #extra>
        <a v-if="downloadUrl" class="el-button el-button--primary" :href="downloadUrl" :download="fileName">
          {{ t('emails.download') }}
        </a>
      </template>
    </el-result>

    <template v-else-if="sheet">
      <div class="grid-wrap">
        <table class="grid">
          <thead>
            <tr>
              <th class="rownum" />
              <th v-for="(c, i) in sheet.columns" :key="i">{{ c }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, r) in sheet.rows" :key="r">
              <td class="rownum">{{ r + 2 }}</td>
              <td v-for="(cell, c) in row" :key="c" :class="{ num: isNumeric(cell) }">{{ cell }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <!-- 只有真被截断时才说。说的是「下载看全的」，因为这一页确实给不了。 -->
      <p v-if="sheet.totalRows > sheet.rows.length" class="note">
        {{ t('sheetWindow.truncated', { shown: sheet.rows.length, total: sheet.totalRows }) }}
      </p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { parseTableFile, type DirectWorkbook } from '../lib/attachmentExcel'

// 画多少。上限是浏览器的力气，不是格式的限制：2000 行 × 60 列已经是 12 万个
// 单元格，再多这一页自己会卡住，而那时人要的其实是下载原文件。
const MAX_ROWS = 2000
const MAX_COLUMNS = 60
const MAX_SHEETS = 50

interface Attachment {
  id: string
  fileName: string
  fileSize?: number | string
  contentType?: string
  downloadUrl?: string
}

// 超过这个大小就不在浏览器里解了。
//
// 上限是这一页的力气，不是格式的限制：一份 25 MB 的 xlsx 解开是几百兆的 XML，
// 拆完之前这个标签页就没气了——而卡死之前人连「太大了，下载吧」这句话都看不到。
// 和从前服务器那道转换上限取同一个数（25 MB）。
const MAX_BYTES = 25 << 20

const route = useRoute()
const { t } = useI18n()

const book = ref<DirectWorkbook | null>(null)
const active = ref('')
const fileName = ref('')
const downloadUrl = ref('')
const loading = ref(true)
const failed = ref(false)
const tooBig = ref(false)
// 这封信里根本没有这个附件。**和「读不出来」分开说**：从前两种都报「可能是
// 文件损坏」，而 2026-09-15 那次真相是页面找错了信——文件好好的，人却被告知
// 自己的文件坏了。一句这么肯定的话，得先配得上。
const notHere = ref(false)
const failureKey = computed(() => {
  if (tooBig.value) return 'tooBig'
  return notHere.value ? 'notHere' : 'failed'
})

const sheet = computed(() => book.value?.sheets.find((s) => s.name === active.value) ?? null)

// 这一页有两个来路，取到的都是同一样东西：文件名 + 下载地址。
//
//   /mail/<信>/sheet/<附件>       收到的附件：按信找
//   /attachment/sheet?key=&name=  写信窗口里还没发出去的附件：按 key 找
//
// 后者没有信可找——草稿的附件要到发送那一刻才登记。授权在服务端按 key 的
// 前缀判（见 mail/internal/app/draftattachment.go）。
async function loadDraftByKey(key: string, name: string) {
  const d = await post<{ files: { fileName: string; fileSize?: string; downloadUrl?: string }[] }>(
    '/email-attachments/preview',
    { files: [{ fileKey: key, fileName: name }] },
  )
  const f = d.files?.[0]
  if (!f) {
    notHere.value = true
    throw new Error('no such draft attachment')
  }
  return { fileName: f.fileName || name, fileSize: f.fileSize, downloadUrl: f.downloadUrl }
}

// 收到的附件：按「哪封信的哪个附件」找。
async function loadFromMail(mailId: string, attId: string) {
  const d = await get<{ mail: { subject?: string; attachments?: Attachment[] } }>(
    `/inbound-mails/${mailId}`,
  )
  const file = d.mail.attachments?.find((a) => String(a.id) === attId)
  if (!file) {
    notHere.value = true
    throw new Error('no such attachment on this mail')
  }
  return { fileName: file.fileName, fileSize: file.fileSize, downloadUrl: file.downloadUrl }
}

onMounted(async () => {
  const draftKey = String(route.query.key || '')
  try {
    // 两个来路，取到的是同一样东西；往下只有一条路。
    const f = draftKey
      ? await loadDraftByKey(draftKey, String(route.query.name || ''))
      : await loadFromMail(String(route.params.id || ''), String(route.params.att || ''))

    if (!f.downloadUrl) throw new Error('attachment has no download url')
    fileName.value = f.fileName
    downloadUrl.value = f.downloadUrl
    document.title = f.fileName
    if (Number(f.fileSize ?? 0) > MAX_BYTES) {
      tooBig.value = true
      failed.value = true
      return
    }

    // 原文件的字节，从签名地址直接取——和「下载」那颗按钮拿的是同一份东西。
    const resp = await fetch(f.downloadUrl)
    if (!resp.ok) throw new Error(`attachment fetch failed: ${resp.status}`)
    const parsed = await parseTableFile(f.fileName, await resp.arrayBuffer(), {
      maxRows: MAX_ROWS,
      maxColumns: MAX_COLUMNS,
      maxSheets: MAX_SHEETS,
    })
    book.value = parsed
    active.value = parsed.sheets[0]?.name ?? ''
  } catch {
    // 读不出来（信没了、附件没存、文件是坏的、或者是这个读法认不了的老 .xls）。
    // 「这封信里没有这个附件」那一种在上面单独标了 notHere——原因不一样，
    // 话就不该一样。这一页给不了内容，但下载那条路一直是通的，出口给在这儿。
    failed.value = true
  } finally {
    loading.value = false
  }
})

// 数字靠右。表格里一列数左对齐，位数就对不齐，扫一眼看不出谁大谁小。
function isNumeric(cell: string): boolean {
  return cell !== '' && /^-?[\d,]*\.?\d+%?$/.test(cell.trim())
}
</script>

<style scoped>
.sheet-window {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--el-bg-color);
}
.bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  background: var(--el-bg-color);
}
.fname {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.grow {
  flex: 1;
}
.tabs {
  display: flex;
  gap: 2px;
  overflow-x: auto;
  max-width: 50vw;
}
.tab {
  border: 0;
  background: transparent;
  padding: 3px 10px;
  border-radius: 999px;
  font: inherit;
  font-size: 13px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
  cursor: pointer;
}
.tab:hover {
  background: var(--el-fill-color);
}
.tab.on {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-weight: 600;
}
.dl {
  flex: none;
  font-size: 13px;
  color: var(--el-color-primary);
  text-decoration: none;
}
.dl:hover {
  text-decoration: underline;
}
.waiting {
  flex: 1;
}
/* 表格自己滚，表头钉在上面：一张装箱单往下翻二十行之后，没有表头的一行数字
   谁都读不懂。 */
.grid-wrap {
  flex: 1;
  overflow: auto;
}
.grid {
  border-collapse: collapse;
  font-size: 13px;
  white-space: nowrap;
}
.grid th,
.grid td {
  border: 1px solid var(--el-border-color-lighter);
  padding: 4px 10px;
  text-align: left;
  max-width: 420px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.grid thead th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--el-fill-color-light);
  font-weight: 600;
}
.grid td.num {
  text-align: right;
  font-variant-numeric: tabular-nums;
}
/* 行号那一列。Excel 里有，这儿也该有：客户说「第 14 行那个价格」时，
   两边说的得是同一个 14——所以从 2 开始数，第 1 行是表头。 */
.rownum {
  position: sticky;
  left: 0;
  z-index: 2;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-placeholder);
  text-align: right !important;
  font-variant-numeric: tabular-nums;
  user-select: none;
}
.grid thead .rownum {
  z-index: 3;
}
.note {
  margin: 0;
  padding: 8px 14px;
  border-top: 1px solid var(--el-border-color-lighter);
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
