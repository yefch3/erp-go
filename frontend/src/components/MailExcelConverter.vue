<!-- 「生成 Excel」这一整条：选中正文或附件后冒出来的那颗按钮、选模板、预览、
     转入待复核询盘，外加后台任务的轮询。

     从 EmailsPage 里抽出来，是因为独立邮件窗口也要它（2026-09-24）。这一条
     有八百来行，复制一份就是两份要一起修的东西；抽成组件，两个页面只负责告诉
     它「选中了什么、属于哪封信」。

     页面怎么用：拿到 ref，信箱能用了之后调一次 start()；把 MailBody /
     QuotedHistory 的 selection-context、
     selection-clear，和 MailAttachments 的 excel-menu / excel-hover /
     excel-leave 接到 openText / openAttachmentMenu / hoverAttachment /
     scheduleHide / close 上。 -->
<template>
  <!-- A floating entry at the selection, not a replacement for the browser's
       context menu: right-click stays native, because a page cannot add an
       entry to the browser's own menu — it can only replace it, and
       replacing it costs the reader copy, look-up and translate. -->
  <div
    v-if="excelMenu.open"
    class="excel-context"
    :style="{ left: excelMenu.x + 'px', top: excelMenu.y + 'px' }"
    role="menu"
    @mouseenter="cancelExcelMenuHide"
    @mouseleave="scheduleExcelMenuHide"
  >
    <button
      type="button"
      role="menuitem"
      :disabled="(!excelAvailable && !excelMenuDirectFile) || !!excelMenu.disabledReason"
      :aria-describedby="((!excelAvailable && !excelMenuDirectFile) || excelMenu.disabledReason) ? 'excel-unavailable-reason' : undefined"
      @click="convertExcelSelection"
    >
      {{ t('emails.convertToExcel') }}
    </button>
    <p v-if="(!excelAvailable && !excelMenuDirectFile) || excelMenu.disabledReason" id="excel-unavailable-reason" class="excel-context-reason">
      {{ excelMenu.disabledReason ? t(excelMenu.disabledReason) : t('emails.excelUnavailable') }}
    </p>
  </div>

  <el-dialog
    v-model="excelTemplateOpen"
    :title="t('emails.selectExcelTemplate')"
    width="min(520px, 92vw)"
    append-to-body
  >
    <el-form label-position="top" v-loading="excelTemplatesBusy">
      <el-form-item :label="t('emails.excelTemplate')" required>
        <el-select
          v-model="selectedInquiryTemplateId"
          :placeholder="t('emails.excelTemplatePlaceholder')"
          style="width: 100%"
        >
          <el-option
            v-for="template in excelTemplates"
            :key="template.id"
            :value="template.id"
            :label="`${template.name} (v${template.version})`"
          >
            <span>{{ template.name }} (v{{ template.version }})</span>
            <el-tag v-if="template.isDefault" size="small" type="success" effect="plain" style="margin-left: 8px">
              {{ t('emails.defaultTemplate') }}
            </el-tag>
          </el-option>
        </el-select>
      </el-form-item>
      <p v-if="selectedExcelTemplate?.description" class="sub">{{ selectedExcelTemplate.description }}</p>
    </el-form>
    <template #footer>
      <el-button @click="excelTemplateOpen = false">{{ t('common.cancel') }}</el-button>
      <el-button
        type="primary"
        :disabled="!selectedInquiryTemplateId || excelTemplatesBusy"
        @click="confirmExcelTemplate"
      >
        {{ t('emails.generateExcel') }}
      </el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="excelOpen"
    :title="excelResult?.fileName || t('emails.excelPreview')"
    width="min(1100px, 94vw)"
    top="4vh"
    append-to-body
    :close-on-click-modal="false"
    :close-on-press-escape="false"
  >
    <div v-loading="excelBusy" class="excel-preview">
      <el-empty v-if="!excelBusy && !excelResult" :description="t('emails.excelWaiting')" />
      <template v-else-if="excelResult">
        <div class="excel-model">
          <template v-if="excelResult.model">{{ t('emails.generatedBy', { model: excelResult.model }) }}</template>
          <template v-else>{{ t('emails.excelDirectNote') }}</template>
          <span v-if="excelResult.model && selectedExcelTemplate"> · {{ t('emails.excelTemplateUsed', { name: selectedExcelTemplate.name, version: selectedExcelTemplate.version }) }}</span>
        </div>
        <el-alert
          v-if="excelResult.previewError"
          type="warning"
          :closable="false"
          show-icon
          :title="excelResult.previewError"
        />
        <el-tabs v-model="excelSheet">
          <el-tab-pane
            v-for="sheet in excelResult.sheets"
            :key="sheet.name"
            :label="sheet.name"
            :name="sheet.name"
          >
            <p v-if="sheet.summary" class="sub">{{ sheet.summary }}</p>
            <p v-if="Number(sheet.totalRows) > sheet.rows.length" class="sub">
              {{ t('emails.excelPreviewRows', { shown: sheet.rows.length, total: sheet.totalRows }) }}
            </p>
            <div class="excel-grid">
              <table>
                <thead><tr><th v-for="c in sheet.columns" :key="c">{{ c }}</th></tr></thead>
                <tbody>
                  <tr v-for="(row, ri) in sheet.rows" :key="ri">
                    <td v-for="(cell, ci) in row.cells" :key="ci">{{ cell }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </el-tab-pane>
        </el-tabs>
      </template>
    </div>
    <template #footer>
      <el-button @click="excelOpen = false">{{ t('emails.close') }}</el-button>
      <el-button
        v-if="excelResult && auth.can('sales:inquiry:write')"
        @click="openSourcingTransfer"
      >
        {{ t('emails.createSourcingCase') }}
      </el-button>
      <el-button v-if="excelResult && excelAvailable" :loading="excelBusy" @click="regenerateExcel">
        {{ t('emails.switchTemplateAndRegenerate') }}
      </el-button>
      <el-button v-if="excelResult" type="primary" @click="downloadExcel">
        {{ t('emails.downloadExcel') }}
      </el-button>
    </template>
  </el-dialog>

  <MailSourcingTransferDialog
    v-model="sourcingOpen"
    :result="excelResult"
    :source-mail-id="convertedExcelSource?.mailId ?? ''"
    @transferred="onTransferred"
  />
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, mailExcelRequest, post } from '../api'
import MailSourcingTransferDialog from './MailSourcingTransferDialog.vue'
import type { MailFile } from './MailAttachments.vue'
import { isDirectTableFile, parseTableFile } from '../lib/attachmentExcel'
import { attachmentExcelSource } from '../lib/attachmentExcelSource'
import {
  base64ToBytes,
  excelPollAfterFailure,
  hydrateExcelResult,
  serverMessageOf,
  type ExcelResult,
} from '../lib/excelJobResult'
import type { InquiryTemplate } from '../lib/inquiryTemplates'
import { useAuthStore } from '../stores/auth'

const props = withDefaults(defineProps<{
  // 附件按「信的号 + 附件号」找回它自己。只用来判断是不是能直接读的表格
  // （.xlsx/.csv），没配模型时照样能预览；找不到就当作不能直接读。
  findAttachment?: (mailId: string, attachmentId: string) => MailFile | undefined
  // 信箱锁着的时候轮询别再问：解锁由全局处理。
  locked?: boolean
  // 进行中的任务号存在 sessionStorage 里，刷新后接着等。**两个页面各用各的键**：
  // window.open 开出来的窗口会把打开它的那页的 sessionStorage 复制一份，同一
  // 个键的话，主窗口正在等的那个任务会在新窗口里也弹一遍。
  jobStorageKey?: string
}>(), {
  findAttachment: undefined,
  locked: false,
  jobStorageKey: 'mailExcelJobId',
})

const emit = defineEmits<{
  // 转入询盘成功。去哪儿由页面决定：主窗口直接跳过去，独立窗口另开一页，
  // 免得把正在看的这封信换掉。
  transferred: [caseId: string]
}>()

const { t, locale } = useI18n()
const auth = useAuthStore()

type ExcelSource =
  | { kind: 'text'; mailId: string; text: string }
  | { kind: 'attachment'; mailId: string; attachmentId: string }

interface ExcelJob {
  id: string
  status: 'PENDING' | 'PROCESSING' | 'COMPLETED' | 'FAILED'
  result?: ExcelResult
  errorCode?: string
  errorMessage?: string
  inquiryTemplateId?: string
  inquiryTemplateCode?: string
  inquiryTemplateVersion?: number
}

const excelMenu = reactive({
  open: false, x: 0, y: 0, source: null as ExcelSource | null, disabledReason: '',
})
const excelOpen = ref(false)
const excelTemplateOpen = ref(false)
const excelTemplatesBusy = ref(false)
const excelTemplates = ref<InquiryTemplate[]>([])
const selectedInquiryTemplateId = ref('')
const pendingExcelSource = ref<ExcelSource | null>(null)
const selectedExcelTemplate = computed(() =>
  excelTemplates.value.find((template) => template.id === selectedInquiryTemplateId.value) ?? null,
)
const excelBusy = ref(false)
const excelResult = ref<ExcelResult | null>(null)
const excelJobId = ref('')
const excelSheet = ref('')
const excelAvailable = ref(false)
const sourcingOpen = ref(false)
const convertedExcelSource = ref<ExcelSource | null>(null)
// Results live in memory: asking for the same attachment or text again opens
// the stored workbook instead of spending another model call. 重新生成 is the
// explicit way to pay for a fresh read.
const excelResultCache = new Map<string, ExcelResult>()
// 计时器句柄写死成 number，不写 ReturnType<typeof setTimeout>：测试要用
// node:zlib，于是 node 的类型进了全局，而 Node 的 setTimeout 返回的是
// Timeout 对象、浏览器的返回 number。这三处跑在浏览器里，number 才是它
// 们真正的样子——推导反而会挑错那一版。
let excelPollTimer: number | null = null
// 连着几次没问到结果。新任务开始归零，问到一次归零。
let excelPollFailures = 0
let excelHoverTimer: number | null = null
let excelHideTimer: number | null = null

function excelCacheKey(source: ExcelSource, templateId = selectedInquiryTemplateId.value): string {
  const sourceKey = source.kind === 'attachment'
    ? `attachment:${source.mailId}:${source.attachmentId}`
    : `text:${source.mailId}:${source.text}`
  return `${sourceKey}:template:${templateId || 'direct'}`
}

function positionExcelMenu(x: number, y: number, source: ExcelSource, disabledReason = '') {
  cancelExcelMenuHide()
  // x is the anchor's centre (the bubble is centred via CSS), so the clamp
  // keeps half a bubble's width inside each edge.
  excelMenu.x = Math.max(110, Math.min(x, window.innerWidth - 110))
  excelMenu.y = Math.max(8, Math.min(y, window.innerHeight - 54))
  excelMenu.source = source
  excelMenu.disabledReason = disabledReason
  excelMenu.open = true
}

// When the menu's source is a spreadsheet we can read directly, the entry
// stays available even with no model configured.
const excelMenuDirectFile = computed(() => (excelMenu.source ? directTableAttachment(excelMenu.source) : null))

// Hover opens the same bubble right-click opens; the brief delay keeps a
// mouse crossing the attachments row from flashing it on every card.
// mailId 是附件自己所属的那封信（组件传来的），不是当前打开的那封——见
// lib/attachmentExcelSource 里那段为什么。
function hoverAttachment(event: MouseEvent, file: MailFile, mailId: string) {
  const source = attachmentExcelSource(mailId, file.id)
  if (!source) return
  const current = excelMenu.source
  if (excelMenu.open && current?.kind === 'attachment' && current.attachmentId === file.id) return
  if (excelHoverTimer) window.clearTimeout(excelHoverTimer)
  const card = event.currentTarget as HTMLElement
  excelHoverTimer = window.setTimeout(() => {
    const rect = card.getBoundingClientRect()
    positionExcelMenu(rect.left + rect.width / 2, rect.bottom + 6, source,
      file.stored ? '' : 'emails.attachmentNotStored')
  }, 250)
}

function scheduleExcelMenuHide() {
  if (excelHoverTimer) {
    window.clearTimeout(excelHoverTimer)
    excelHoverTimer = null
  }
  if (excelHideTimer) window.clearTimeout(excelHideTimer)
  excelHideTimer = window.setTimeout(closeExcelMenu, 300)
}

function cancelExcelMenuHide() {
  if (excelHoverTimer) {
    window.clearTimeout(excelHoverTimer)
    excelHoverTimer = null
  }
  if (excelHideTimer) {
    window.clearTimeout(excelHideTimer)
    excelHideTimer = null
  }
}

// 正文（包括折起来的引用）里选中了文字，或者点了一张正文内嵌的图。
function openText(
  event: { text?: string; attachmentId?: string; x: number; y: number },
  mailId: string,
) {
  if (!mailId) return
  if (event.attachmentId) {
    positionExcelMenu(event.x, event.y, {
      kind: 'attachment', mailId, attachmentId: event.attachmentId,
    })
    return
  }
  const text = event.text?.trim() ?? ''
  if (!text) return
  positionExcelMenu(event.x, event.y, { kind: 'text', mailId, text })
}

function openAttachmentMenu(event: MouseEvent, file: MailFile, mailId: string) {
  const source = attachmentExcelSource(mailId, file.id)
  if (!source) return
  event.preventDefault()
  positionExcelMenu(event.clientX, event.clientY, source,
    file.stored ? '' : 'emails.attachmentNotStored')
}

function closeExcelMenu() {
  excelMenu.open = false
}

async function convertExcelSelection() {
  const source = excelMenu.source
  closeExcelMenu()
  if (!source || excelBusy.value || excelMenu.disabledReason) return
  // 有模型时先选模板，即使附件本身是 xlsx 也要按所选模板标准化。没有模型
  // 时仍保留原表直接预览，避免破坏既有的基础查看能力。
  const directFile = directTableAttachment(source)
  if (!excelAvailable.value) {
    if (directFile && source.kind === 'attachment' && (await openAttachmentDirect(source, directFile))) return
    ElMessage.error(t('emails.excelUnavailable'))
    return
  }
  await openExcelTemplatePicker(source)
}

async function loadExcelTemplates() {
  excelTemplatesBusy.value = true
  try {
    const data = await get<{ templates: InquiryTemplate[] }>('/mail-inquiry-templates', undefined, mailExcelRequest)
    excelTemplates.value = (data.templates ?? []).filter((template) => template.status === 'ACTIVE')
    const selectedStillExists = excelTemplates.value.some((template) => template.id === selectedInquiryTemplateId.value)
    if (!selectedStillExists) {
      selectedInquiryTemplateId.value = excelTemplates.value.find((template) => template.isDefault)?.id
        ?? excelTemplates.value[0]?.id
        ?? ''
    }
  } finally {
    excelTemplatesBusy.value = false
  }
}

async function openExcelTemplatePicker(source: ExcelSource) {
  pendingExcelSource.value = source
  excelTemplateOpen.value = true
  try {
    await loadExcelTemplates()
  } catch {
    excelTemplateOpen.value = false
  }
}

async function confirmExcelTemplate() {
  const source = pendingExcelSource.value
  const templateId = selectedInquiryTemplateId.value
  if (!source || !templateId || excelTemplatesBusy.value) return
  excelTemplateOpen.value = false
  const cached = excelResultCache.get(excelCacheKey(source, templateId))
  if (cached) {
    convertedExcelSource.value = source
    excelResult.value = cached
    excelSheet.value = cached.sheets[0]?.name ?? ''
    excelOpen.value = true
    return
  }
  await startExcelConversion(source, templateId)
}

function directTableAttachment(source: ExcelSource): MailFile | null {
  if (source.kind !== 'attachment') return null
  const file = props.findAttachment?.(source.mailId, source.attachmentId)
  if (!file?.downloadUrl || !isDirectTableFile(file.fileName, file.contentType)) return null
  return file
}

// Reads the spreadsheet straight from its signed storage URL — the same
// bytes the download button hands out — and presents the parsed preview.
// Returns false when anything about the read fails, so the caller can fall
// back to the model.
async function openAttachmentDirect(source: ExcelSource, file: MailFile): Promise<boolean> {
  if (!source.mailId || !file.downloadUrl) return false
  excelResult.value = null
  convertedExcelSource.value = source
  excelSheet.value = ''
  excelOpen.value = true
  excelBusy.value = true
  try {
    const response = await fetch(file.downloadUrl)
    if (!response.ok) throw new Error(`attachment fetch failed: ${response.status}`)
    const data = await response.arrayBuffer()
    const parsed = await parseTableFile(file.fileName, data)
    const result: ExcelResult = {
      fileName: file.fileName,
      fileData: bytesToBase64(new Uint8Array(data)),
      sheets: parsed.sheets.map((sheet) => ({
        name: sheet.name,
        summary: '',
        columns: sheet.columns,
        rows: sheet.rows.map((cells) => ({ cells })),
        totalRows: String(sheet.totalRows),
      })),
      model: '',
    }
    excelResultCache.set(excelCacheKey(source, 'direct'), result)
    excelResult.value = result
    excelSheet.value = result.sheets[0]?.name ?? ''
    return true
  } catch {
    excelResult.value = null
    excelOpen.value = false
    return false
  } finally {
    excelBusy.value = false
  }
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(binary)
}

async function startExcelConversion(source: ExcelSource, templateId = selectedInquiryTemplateId.value) {
  excelResult.value = null
  convertedExcelSource.value = source
  excelSheet.value = ''
  excelOpen.value = true
  excelBusy.value = true
  try {
    const body = source.kind === 'text'
      ? { selectedText: source.text, locale: locale.value, inquiryTemplateId: templateId }
      : { attachmentId: source.attachmentId, locale: locale.value, inquiryTemplateId: templateId }
    const response = await post<{ job: ExcelJob }>(
      `/inbound-mails/${source.mailId}/excel`, body, mailExcelRequest,
    )
    excelJobId.value = response.job.id
    sessionStorage.setItem(props.jobStorageKey, response.job.id)
    ElMessage.info(t('emails.excelQueued'))
    scheduleExcelJobPoll(300)
  } catch {
    excelOpen.value = false
    excelBusy.value = false
  }
}

async function regenerateExcel() {
  const source = convertedExcelSource.value
  if (!source || excelBusy.value || !excelAvailable.value) return
  excelOpen.value = false
  await openExcelTemplatePicker(source)
}

function resumeExcelJob() {
  const id = sessionStorage.getItem(props.jobStorageKey) ?? ''
  if (!id || excelJobId.value === id) return
  excelJobId.value = id
  excelPollFailures = 0
  excelResult.value = null
  excelBusy.value = true
  excelOpen.value = true
  scheduleExcelJobPoll(0)
}

function scheduleExcelJobPoll(delay = 1500) {
  if (excelPollTimer) window.clearTimeout(excelPollTimer)
  excelPollTimer = window.setTimeout(() => void refreshJob(), delay)
}

// 也给页面用：服务端推送「任务变了」时（mail.excel_job.changed）不必等下一
// 轮轮询。subject 是 "EXCEL_JOB:<id>"，不是这一个的就不管。
async function refreshJob(subject = '') {
  const idFromEvent = subject.startsWith('EXCEL_JOB:') ? subject.slice('EXCEL_JOB:'.length) : ''
  const id = excelJobId.value
  if (!id || (idFromEvent && idFromEvent !== id)) return
  try {
    // quiet：问不到的那几次自己处理（见下面的 catch），不让每一次都弹红字。
    const response = await get<{ job: ExcelJob }>(`/inbound-excel-jobs/${id}`, undefined, {
      ...mailExcelRequest,
      quiet: true,
    })
    // The poll timer and the SSE hint race to fetch the same job; only the
    // first response back may announce the terminal state, the later one
    // finds the id already settled and stays silent.
    if (excelJobId.value !== id) return
    excelPollFailures = 0
    const job = response.job
    if (job.status === 'PENDING' || job.status === 'PROCESSING') {
      scheduleExcelJobPoll()
      return
    }
    excelBusy.value = false
    excelJobId.value = ''
    sessionStorage.removeItem(props.jobStorageKey)
    if (job.status === 'FAILED' || !job.result) {
      excelOpen.value = false
      ElMessage.error(job.errorMessage || t('emails.excelFailed'))
      return
    }
    const delivered: ExcelResult = {
      ...job.result,
      inquiryTemplateId: job.inquiryTemplateId,
      inquiryTemplateCode: job.inquiryTemplateCode,
      inquiryTemplateVersion: job.inquiryTemplateVersion,
    }
    // 行数据不在响应里，在文件里：从 file_data 解出来。解不出来不是网络
    // 问题，不能落到下面那个「继续轮询」的 catch 里——那会一直轮下去。
    // 这时表格空着、说一句为什么，下载照常能用。
    const result = await hydrateExcelResult(delivered).catch(
      (): ExcelResult => ({ ...delivered, previewError: t('emails.excelPreviewUnreadable') }),
    )
    if (job.inquiryTemplateId) selectedInquiryTemplateId.value = job.inquiryTemplateId
    excelResult.value = result
    if (convertedExcelSource.value) {
      excelResultCache.set(excelCacheKey(convertedExcelSource.value, job.inquiryTemplateId), result)
    }
    excelSheet.value = result.sheets[0]?.name ?? ''
    excelOpen.value = true
    ElMessage.success(t('emails.excelReady'))
  } catch (err: unknown) {
    // 网络抖一下、网关重启一下、对象存储暂时取不到文件——3 秒后再问，中间
    // 不弹字。但只问约一分钟（excelPollAfterFailure）：改之前这里是无限问
    // 下去、每 3 秒弹一次红字，直到对象存储恢复。到点停下、关弹窗，把服务
    // 端那句话弹一次（比如「暂时取不到，请稍后重试或重新转换」）。
    // 邮箱解锁过期由全局处理，这里只需要不再问。
    if (props.locked) return
    excelPollFailures += 1
    if (excelPollAfterFailure(excelPollFailures) === 'retry') {
      scheduleExcelJobPoll(3000)
      return
    }
    excelBusy.value = false
    excelJobId.value = ''
    sessionStorage.removeItem(props.jobStorageKey)
    excelOpen.value = false
    ElMessage.error(serverMessageOf(err) ?? t('emails.excelFailed'))
  }
}

// 先问客户，再转。询盘最后要变成报价和合同，那两步都要一个真客户；这里不
// 问，就会在生成报价那一步才卡住——错得更晚，也更难查。
function openSourcingTransfer() {
  const result = excelResult.value
  const sheet = result?.sheets[0]
  if (!result || !convertedExcelSource.value || !sheet?.rows.length) return
  if (Number(sheet.totalRows) > sheet.rows.length) {
    ElMessage.warning(t('emails.sourcingPreviewIncomplete'))
    return
  }
  sourcingOpen.value = true
}

// 转入成功：预览也收起，去哪儿由页面定。
function onTransferred(caseId: string) {
  excelOpen.value = false
  emit('transferred', caseId)
}

function downloadExcel() {
  const result = excelResult.value
  if (!result) return
  const url = URL.createObjectURL(new Blob([base64ToBytes(result.fileData)], {
    type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  }))
  const link = document.createElement('a')
  link.href = url
  link.download = result.fileName
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

// 这个部署有没有接模型适配器。**只问一次**：它是部署配置，不会在一个人
// 开着页面的时候变。
async function loadExcelCapability() {
  try {
    const d = await get<{ excelAvailable: boolean }>('/my-mail-account')
    excelAvailable.value = d.excelAvailable === true
  } catch {
    excelAvailable.value = false
  }
}

// 由页面在「信箱能用了」之后叫：问一次这个部署有没有模型、接着等刷新前没等完的
// 任务。不放进 onMounted，是因为收件箱页面可能先锁着（公司邮箱要解锁）——那时
// 问就是白问、按钮一直灰着；接着等的任务也会一碰到锁就停下、解锁后再也不问。
// 原来这两件事就在收件箱的 init() 里，解锁之后才做，这里保持那个时机。
function start() {
  void loadExcelCapability()
  resumeExcelJob()
}

onMounted(() => {
  window.addEventListener('click', closeExcelMenu)
  window.addEventListener('blur', closeExcelMenu)
  // Capture phase, because the reading pane scrolls in an inner container and
  // scroll events do not bubble. A fixed-position bubble that stays put while
  // its selection scrolls away is pointing at nothing.
  window.addEventListener('scroll', closeExcelMenu, true)
})

onUnmounted(() => {
  if (excelPollTimer) window.clearTimeout(excelPollTimer)
  if (excelHoverTimer) window.clearTimeout(excelHoverTimer)
  if (excelHideTimer) window.clearTimeout(excelHideTimer)
  window.removeEventListener('click', closeExcelMenu)
  window.removeEventListener('blur', closeExcelMenu)
  window.removeEventListener('scroll', closeExcelMenu, true)
})

defineExpose({
  start,
  openText,
  openAttachmentMenu,
  hoverAttachment,
  scheduleHide: scheduleExcelMenuHide,
  close: closeExcelMenu,
  refreshJob,
})
</script>

<style scoped>
.excel-context {
  position: fixed;
  z-index: 4000;
  transform: translateX(-50%);
  min-width: 190px;
  max-width: 320px;
  padding: 5px;
  border: 1px solid var(--el-border-color-light);
  border-radius: 7px;
  background: var(--el-bg-color-overlay);
  box-shadow: var(--el-box-shadow-light);
}
.excel-context button {
  width: 100%;
  padding: 8px 12px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--el-text-color-primary);
  text-align: left;
  cursor: pointer;
}
.excel-context button:hover,
.excel-context button:focus-visible {
  background: var(--el-fill-color-light);
  color: var(--el-color-primary);
  outline: none;
}
.excel-context button:disabled {
  color: var(--el-text-color-disabled);
  cursor: not-allowed;
}
.excel-context button:disabled:hover,
.excel-context button:disabled:focus-visible {
  background: transparent;
  color: var(--el-text-color-disabled);
}
.excel-context-reason {
  max-width: 240px;
  margin: 3px 8px 6px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.45;
}
.excel-preview {
  min-height: 180px;
}
.excel-model {
  margin-bottom: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.sub {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.excel-grid {
  max-height: 58vh;
  overflow: auto;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 5px;
}
.excel-grid table {
  width: max-content;
  min-width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.excel-grid th,
.excel-grid td {
  max-width: 360px;
  padding: 7px 10px;
  border-right: 1px solid var(--el-border-color-lighter);
  border-bottom: 1px solid var(--el-border-color-lighter);
  white-space: pre-wrap;
  word-break: break-word;
  text-align: left;
  vertical-align: top;
}
.excel-grid th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--el-fill-color-light);
  font-weight: 600;
}
</style>
