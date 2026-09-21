<!-- 一封信，自己一个窗口。
     双击列表里的一行就弹这个（照 Foxmail、Outlook、Apple Mail：桌面邮件
     客户端双击一封信都是开一个新窗口）。为什么值得有：邮件页是三栏，正文
     那一栏再宽也只有半个屏幕，而客户发来的报价单、带表格的装箱单、长长的
     往来记录，本来就该占满一块屏幕去看——甚至摆到第二块屏幕上，一边看信
     一边在主窗口里填单子。

     看信**和回信**。回复、回复全部、转发在这儿——客户的信摊在整块屏幕上，
     回信这件事本来就该在同一个窗口里完成，而不是"看完这一屏、回主窗口、
     再把信找出来"。

     删除、归档、挪文件夹、标未读、导出也在——和主窗口阅读区上那一排一样。
     这几件会改列表的样子（这封信还在不在、在哪个文件夹），而列表在主窗口里，
     所以做完要**告诉主窗口一声**：走 BroadcastChannel（同源的窗口都听得到，
     不依赖 window.opener——刷新过的窗口 opener 是空的），主窗口收到就把列表
     重新拉一遍。信离开了这一格（删了、挪了、归档了）的话这个窗口顺手关掉：
     它显示的东西已经不在那儿了。

     路由在 Shell 外面：这个窗口不该有顶栏和左侧菜单，它就是一封信。 -->
<template>
  <div class="mail-window">
    <div v-if="loading" v-loading="true" class="waiting" />

    <el-result
      v-else-if="failed"
      icon="warning"
      :title="t('mailWindow.gone')"
      :sub-title="t('mailWindow.goneHint')"
    />

    <template v-else-if="mail">
      <h1 class="in-subject">{{ mail.subject || t('emails.noSubject') }}</h1>

      <!-- 一行，和邮件页的阅读区一样：名字长一点就折成两行、把正文推下去的
           那个毛病在这里也一样难看。 -->
      <div class="in-from">
        <span class="avatar" :style="avatarStyle(mail.fromEmail)" aria-hidden="true">
          {{ initialOf(mail.fromName || mail.fromEmail) }}
        </span>
        <span class="in-name strong">{{ mail.fromName || mail.fromEmail }}</span>
        <span class="in-addr sub">&lt;{{ mail.fromEmail }}&gt;</span>
        <span class="in-to sub">{{ t('emails.inboundTo', { to: mail.toEmail }) }}</span>
        <button class="details-toggle" @click="detailsOpen = !detailsOpen">
          {{ detailsOpen ? t('emails.hideDetails') : t('emails.showDetails') }}
        </button>
        <span class="grow" />
        <span class="sub in-when" :title="zonedStamp(mail.sentAt || mail.receivedAt)">
          {{ shortTime(mail.sentAt || mail.receivedAt) }}
        </span>
      </div>

      <!-- 和阅读区里同一条提醒，同样不藏进详情：详情是折起来的，而这一条
           正是那种「不特意去看就不会知道」的事。 -->
      <div v-if="replyToDiffers(mail)" class="reply-mismatch">
        <el-icon><WarningFilled /></el-icon>
        <span>{{ t('emails.replyToMismatch', { addr: mail.replyTo }) }}</span>
      </div>

      <dl v-if="detailsOpen" class="mail-details">
        <template v-for="row in detailRows" :key="row.k">
          <dt>{{ row.k }}</dt>
          <dd>{{ row.v }}</dd>
        </template>
      </dl>

      <!-- 和主窗口阅读区上那排图标同一套：符号一样、次序一样、tooltip 一样，
           因为它们做的是同一件事。没有的那几颗（删除、归档、挪文件夹）见页头
           那段说明。 -->
      <div v-if="canWrite || deleteAction || menuAvailable" class="in-actions">
        <template v-if="canWrite">
          <el-tooltip :content="t('emails.reply')" placement="bottom" :show-after="0" :hide-after="0">
            <button type="button" class="tb" :aria-label="t('emails.reply')" @click="reply">
              <MailActionIcon kind="reply" />
            </button>
          </el-tooltip>
          <el-tooltip :content="t('emails.replyAll')" placement="bottom" :show-after="0" :hide-after="0">
            <button type="button" class="tb" :aria-label="t('emails.replyAll')" @click="replyAll">
              <MailActionIcon kind="reply-all" />
            </button>
          </el-tooltip>
          <el-tooltip :content="t('emails.forward')" placement="bottom" :show-after="0" :hide-after="0">
            <button type="button" class="tb" :aria-label="t('emails.forward')" @click="forward">
              <MailActionIcon kind="forward" />
            </button>
          </el-tooltip>
        </template>
        <!-- 删除在最右、隔一条线：前三颗是「继续这封信」，它是「结束这封信」。
             回收站里它是彻底删除，别处是挪进回收站——tooltip 说的就是按下去
             会发生什么，图标本身分不出这两者。 -->
        <span v-if="deleteAction" class="tb-sep" aria-hidden="true" />
        <el-tooltip
          v-if="deleteAction"
          :content="deleteAction.label"
          placement="bottom"
          :show-after="0"
          :hide-after="0"
        >
          <button
            type="button"
            class="tb tb-danger"
            :aria-label="deleteAction.label"
            :disabled="busy"
            @click="deleteAction.run()"
          ><el-icon><Delete /></el-icon></button>
        </el-tooltip>

        <span class="grow" />

        <!-- 「⋯」里是其余全部动作，分组和次序照主窗口。 -->
        <el-dropdown v-if="menuAvailable" trigger="click" placement="bottom-end" @command="onCommand">
          <button type="button" class="tb" :aria-label="t('emails.moreActions')">
            <el-icon><MoreFilled /></el-icon>
          </button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item v-if="canWrite" command="forwardAttachment" :disabled="!mail.hasRaw">
                {{ t('emails.forwardAsAttachment') }}
              </el-dropdown-item>
              <el-dropdown-item v-if="listed && view !== 'JUNK' && view !== 'TRASH'" command="unread" :divided="canWrite">
                {{ t('emails.markUnread') }}
              </el-dropdown-item>
              <el-dropdown-item v-if="view === 'JUNK'" command="notJunk" :divided="canWrite">
                {{ t('emails.notJunk') }}
              </el-dropdown-item>
              <el-dropdown-item v-if="view === 'ARCHIVE'" command="unarchive">
                {{ t('emails.unarchive') }}
              </el-dropdown-item>
              <el-dropdown-item v-else-if="listed && view !== 'JUNK' && view !== 'TRASH'" command="archive">
                {{ t('emails.archive') }}
              </el-dropdown-item>
              <el-dropdown-item v-if="view === 'TRASH'" command="restore" :divided="canWrite">
                {{ t('emails.restore') }}
              </el-dropdown-item>

              <template v-if="canMove">
                <el-dropdown-item disabled divided>{{ t('emails.moveTo') }}</el-dropdown-item>
                <el-dropdown-item v-if="view.startsWith('F:')" command="move:0">
                  {{ t('emails.moveToInbox') }}
                </el-dropdown-item>
                <el-dropdown-item
                  v-for="cf in folders"
                  :key="cf.id"
                  :command="`move:${cf.id}`"
                  :disabled="view === cf.viewKey || busy"
                >
                  {{ cf.name }}
                </el-dropdown-item>
                <el-dropdown-item v-if="!folders.length" disabled>
                  {{ t('emails.noFoldersYet') }}
                </el-dropdown-item>
              </template>

              <template v-if="canExport && mail.threadKey">
                <el-dropdown-item command="print" divided :disabled="exporting">
                  {{ t('emails.exportPrint') }}
                </el-dropdown-item>
                <el-dropdown-item command="save" :disabled="exporting">
                  {{ t('emails.exportSave') }}
                </el-dropdown-item>
              </template>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>

      <el-divider />

      <!-- 正文一律在沙箱 frame 里渲染，纯文本也不例外——这条由
           scripts/check-mail-sandbox.sh 守着。 -->
      <MailBody v-if="mail.bodyHtml" :html="mail.bodyHtml" />
      <MailBody v-else :html="plainTextToHtml(mail.bodyText || '')" />
      <QuotedHistory v-if="mail.quotedHtml" :html="mail.quotedHtml" />

      <template v-if="mail.attachments?.length">
        <el-divider />
        <div class="att-head">
          <h4 class="side-title">{{ t('emails.attachments') }}</h4>
        </div>
        <!-- 「下载全部」那颗在组件里，跟着这一封走。和主窗口同一条规矩。 -->
        <MailAttachments
          :files="mail.attachments"
          :mail-id="String(mail.id)"
          :bundling="bundling"
          @preview="openPreview"
          @download-all="downloadAll"
        />
      </template>
    </template>

    <!-- 写信框和主窗口用的是同一个组件，所以草稿、附件、签名、发件人选择
         全都一样——回信从哪个箱发出去，默认就是这封信落在的那个箱。 -->
    <EmailComposer
      ref="composer"
      v-model="composing"
      :mailboxes="boxes"
      :current-account="Number(mail?.accountId ?? 0)"
      @sent="onSent"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, MoreFilled, WarningFilled } from '@element-plus/icons-vue'
import { del, download, get, post, quietErrors, saveBlob } from '../api'
import { printDocument } from '../lib/printDocument'
import { useAuthStore } from '../stores/auth'
import { replyAllRecipients } from '../lib/replyAll'
import {
  DEFAULT_LIST_MODE,
  mergesThreads,
  normalizeListMode,
  type MailListMode,
} from '../lib/mailListMode'
import EmailComposer from '../components/EmailComposer.vue'
import MailActionIcon from '../components/MailActionIcon.vue'
import MailBody from '../components/MailBody.vue'
import QuotedHistory from '../components/QuotedHistory.vue'
import MailAttachments, { type MailFile } from '../components/MailAttachments.vue'
import { mailDetailRows, replyToDiffers } from '../lib/mailDetails'
import { isOfficePreview, isSheetPreview } from '../lib/attachmentPreview'
import { plainTextToHtml } from '../lib/linkifyText'
import { shortTime, zonedStamp } from '../lib/zonedtime'
import '../styles/mailbox.css'

// 只要读这一封需要的那些字段。**不是** EmailsPage 里那份 InboundMail 的副本：
// 那一份还带着列表、会话、已发送追踪要用的东西，这里一个都用不上。
interface WindowMail {
  id: string
  fromEmail: string
  fromName: string
  toEmail: string
  toAll?: string
  cc?: string
  replyTo?: string
  /** 拆好的人，「回复全部」要用。 */
  toParties?: { name?: string; email: string }[]
  ccParties?: { name?: string; email: string }[]
  /** 这封信落在哪个信箱：回信默认从同一个箱发出去。 */
  accountId?: number | string
  /** 左栏的哪一格：INBOX / ARCHIVE / JUNK / TRASH / F:…；已发送是空串。 */
  view?: string
  /** 原始 MIME 还在不在——「作为附件转发」要它。 */
  hasRaw?: boolean
  threadKey?: string
  subject: string
  sentAt: string
  receivedAt: string
  folder?: string
  rawSize?: number
  authSpf?: string
  authDkim?: string
  bodyHtml?: string
  bodyText?: string
  quotedHtml?: string
  attachments?: MailFile[]
}

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const auth = useAuthStore()

// 权限和主窗口阅读区同一套判断。
const canWrite = computed(() => auth.can('mail:email:write'))
const canExport = computed(() => auth.can('mail:email:export'))

// 这封信在左栏的哪一格。空串是已发送（或退信）：那一侧没有删除、归档这些。
const view = computed(() => mail.value?.view ?? '')
const listed = computed(() => view.value !== '')
// 「移动到」给不给：和主窗口那颗同一个条件。
const canMove = computed(
  () => listed.value && view.value !== 'TRASH' && view.value !== 'JUNK' && mail.value?.folder !== 'SENT',
)
// 删除那颗做哪件事。回收站里是彻底删除，别处是挪进回收站。
const deleteAction = computed(() => {
  if (!mail.value || !listed.value) return null
  if (view.value === 'TRASH') return { label: t('emails.purge'), run: purge }
  return { label: t('emails.toTrash'), run: () => mark({ deleted: true }, { gone: true }) }
})
// 「⋯」里到底有没有东西——一个点开是空的菜单比没有这颗按钮更糟。
const menuAvailable = computed(() => {
  const m = mail.value
  if (!m) return false
  return (
    canWrite.value
    || (listed.value && view.value !== 'JUNK' && view.value !== 'TRASH')
    || view.value === 'JUNK'
    || view.value === 'TRASH'
    || canMove.value
    || (canExport.value && !!m.threadKey)
  )
})

// 这个信箱的自建文件夹，「移动到」要列它们。
const folders = ref<{ id: number; name: string; viewKey: string }[]>([])
const busy = ref(false)
const exporting = ref(false)

// 告诉主窗口「这封信变了」。同源的页面都听得到；不用 window.opener——刷新过
// 的窗口 opener 是空的，而 BroadcastChannel 谁开的都一样。
const bus = new BroadcastChannel('erp-mail')
onUnmounted(() => bus.close())
function notify(gone: boolean) {
  bus.postMessage({ type: 'mail-changed', id: mail.value?.id ?? '', gone })
}
// 信离开了这一格，这个窗口就没有东西可显示了。自己关不掉（不是脚本开的、
// 或者浏览器不让）就留着，主窗口那边照样已经刷新了。
function leave() {
  window.close()
}

const mail = ref<WindowMail | null>(null)
const loading = ref(true)
const failed = ref(false)
const detailsOpen = ref(false)
const bundling = ref(false)
const composing = ref(false)
const composer = ref<{
  openReply: (m: WindowMail) => void
  openReplyAll: (m: WindowMail, who: { to: { name?: string; email: string }; cc: { name?: string; email: string }[] }) => void
  openForward: (m: WindowMail) => void
  openForwardAsAttachment: (m: WindowMail) => void
} | null>(null)
// 这个人名下的信箱：写信框拿它画发件人下拉，「回复全部」拿它认出"这次用来
// 回信的那个地址"（那个地址不进抄送）。
const boxes = ref<{ id: number; email: string }[]>([])

const detailRows = computed(() => mailDetailRows(mail.value, t))

onMounted(async () => {
  const id = String(route.params.id || '')
  try {
    const d = await get<{ mail: WindowMail }>(`/inbound-mails/${id}`)
    mail.value = d.mail
    // 任务栏和标签页上写这封信的主题。一个人开着三个这样的窗口时，三个
    // 一模一样的标题等于没有标题。
    document.title = d.mail.subject || t('emails.noSubject')
    // 信箱清单：写信框要它。悄悄失败——没有它写信框只是不显示发件人下拉，
    // 而这封信本身已经摆在屏幕上了，不该因为一条次要请求变成一页错误。
    void loadBoxes()
    void loadFolders(Number(d.mail.accountId ?? 0))
    // 这个人的列表是按会话合并的还是一封一行。这一页不画列表，但下面
    // 的归档/删除要照这一档来——合并档下删的是整条会话，单封档下只删
    // 这一封。**await 它**，和上面两条不同：拿不到就按合并档走，而那
    // 时人点删除会删掉他不想删的东西。
    await loadListMode()
  } catch {
    // 信被删了、id 不是自己的、或者信箱这会儿是锁着的。分不出是哪一种，
    // 也不该猜——一句「打不开，回主窗口看看」比一个白屏诚实。
    failed.value = true
  } finally {
    loading.value = false
  }
})

async function loadBoxes() {
  try {
    const d = await get<{ accounts?: { id: number | string; email?: string }[] }>('/my-mailboxes', undefined, quietErrors)
    boxes.value = (d.accounts ?? []).map((a) => ({ id: Number(a.id ?? 0), email: a.email ?? '' }))
  } catch {
    // 见调用处。
  }
}

async function loadFolders(accountId: number) {
  if (!accountId) return
  try {
    const d = await get<{ folders?: { id: number | string; name: string; viewKey?: string; role?: string }[] }>(
      '/mail-folders', { account_id: accountId }, quietErrors,
    )
    folders.value = (d.folders ?? [])
      .filter((f) => f.role === 'CUSTOM' && f.viewKey)
      .map((f) => ({ id: Number(f.id), name: f.name, viewKey: f.viewKey ?? '' }))
  } catch {
    // 没有这份清单只是「移动到」里少几项，信本身已经在屏幕上了。
  }
}

// 这个人的列表是按会话合并的还是一封一行（见 lib/mailListMode）。
//
// 这一页不画列表，要它只为一件事：下面那几个动作该作用在整条会话上还是
// 只作用在这一封。问不到就按合并档走——一直以来的样子。
const listMode = ref<MailListMode>(DEFAULT_LIST_MODE)
const mergeThreads = computed(() => mergesThreads(listMode.value))

async function loadListMode() {
  try {
    const d = await get<{ listMode?: string }>('/mail-list-mode', undefined, quietErrors)
    listMode.value = normalizeListMode(d.listMode)
  } catch {
    // 见上。
  }
}

// 标记类动作：已读/未读、归档、挪进回收站、还原、不是垃圾。作用范围跟着
// 这个人的列表档位走，和主窗口一样：合并档下整条会话一起，单封档下只这
// 一封。gone = 做完这封信就不在这一格了，窗口关掉。
async function mark(flags: Record<string, boolean>, opts: { gone: boolean }) {
  if (!mail.value || busy.value) return
  busy.value = true
  try {
    await post(`/inbound-mails/${mail.value.id}/mark`, { ...flags, wholeThread: mergeThreads.value })
    notify(opts.gone)
    if (opts.gone) leave()
  } catch {
    // 后端的原因拦截器已经弹了
  } finally {
    busy.value = false
  }
}

// 彻底删除，只在回收站里有，而且两边一起没：ERP 的副本和邮件服务器上的原件。
// 确认框说的就是这句，因为这是唯一一件事后没法检查的事。
async function purge() {
  if (!mail.value || busy.value) return
  try {
    await ElMessageBox.confirm(t('emails.purgeHint'), t('emails.purge'), {
      type: 'warning',
      confirmButtonText: t('emails.purge'),
    })
  } catch {
    return
  }
  busy.value = true
  try {
    await del(`/inbound-mails/${mail.value.id}?whole_thread=${mergeThreads.value}`)
    ElMessage.success(t('emails.purged'))
    notify(true)
    leave()
  } catch {
    // 同上
  } finally {
    busy.value = false
  }
}

async function moveTo(folderId: number) {
  if (!mail.value || busy.value) return
  busy.value = true
  try {
    await post(`/inbound-mails/${mail.value.id}/move`, { folderId: String(folderId) })
    ElMessage.success(t('emails.moved'))
    notify(true)
    leave()
  } catch {
    // 同上
  } finally {
    busy.value = false
  }
}

async function fetchTranscript() {
  const key = mail.value?.threadKey
  if (!key || exporting.value) return null
  exporting.value = true
  try {
    return await download('/mail-threads/export', {
      key,
      tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
      lang: locale.value,
    })
  } catch {
    return null
  } finally {
    exporting.value = false
  }
}

function onCommand(cmd: string) {
  if (cmd.startsWith('move:')) {
    void moveTo(Number(cmd.slice(5)))
    return
  }
  switch (cmd) {
    case 'forwardAttachment': return void forwardAsAttachment()
    case 'unread': return void mark({ read: false }, { gone: false })
    case 'notJunk': return void mark({ notJunk: true }, { gone: true })
    case 'archive': return void mark({ archived: true }, { gone: true })
    case 'unarchive': return void mark({ archived: false }, { gone: true })
    case 'restore': return void mark({ deleted: false }, { gone: true })
    case 'print': return void fetchTranscript().then(async (f) => { if (f) printDocument(await f.text()) })
    case 'save': return void fetchTranscript().then((f) => { if (f) saveBlob(f.blob, f.fileName) })
  }
}

function forwardAsAttachment() {
  if (!mail.value?.hasRaw) return
  const m = mail.value
  void openComposer(() => composer.value?.openForwardAsAttachment(m))
}

/** 这次回信从哪个地址发出去。写信框的发件人默认值同源，两边不会各说各的。 */
function replyingAddress(): string {
  const own = Number(mail.value?.accountId ?? 0)
  return boxes.value.find((b) => b.id === own)?.email ?? boxes.value[0]?.email ?? ''
}

async function openComposer(fill: () => void) {
  composing.value = true
  await nextTick()
  fill()
}

function reply() {
  if (!mail.value) return
  const m = mail.value
  void openComposer(() => composer.value?.openReply(m))
}

function replyAll() {
  if (!mail.value) return
  const m = mail.value
  // 收件人怎么算在 lib/replyAll（那儿写着为什么只去掉这次回信用的那一个
  // 地址，不是名下全部），和主窗口用的是同一份规则。
  void openComposer(() =>
    composer.value?.openReplyAll(m, replyAllRecipients({ ...m, self: replyingAddress() })),
  )
}

function forward() {
  if (!mail.value) return
  const m = mail.value
  void openComposer(() => composer.value?.openForward(m))
}

// 发出去了。窗口不自己关掉：人可能还要照着这封信再回一封、或者把附件下下来。
// 写信框自己已经弹过「已发送」，这里不再叠一句；主窗口的已发送列表会在它
// 下次拉的时候看到这封信。
function onSent() {}

/** 把这封信的附件打成一个压缩包下载。上限（40 MB）和超过时的话由后端说。 */
async function downloadAll() {
  const id = mail.value?.id
  if (!id || bundling.value) return
  bundling.value = true
  try {
    const file = await download(`/inbound-mails/${id}/attachments/download`)
    saveBlob(file.blob, file.fileName)
  } catch {
    // 具体原因（太大、原件读不到）后端已经用消息说了，拦截器会弹出来。
  } finally {
    bundling.value = false
  }
}

// 预览一律新开一个标签页，和邮件页那边同一条规矩（见 EmailsPage 的
// openPreview，那儿写着几条路各自的理由）。
function openPreview(a: MailFile) {
  if (!mail.value) return
  if (isOfficePreview(a)) {
    window.open(router.resolve({ path: `/mail/${mail.value.id}/office/${a.id}` }).href, `office-${a.id}`)?.focus()
    return
  }
  if (isSheetPreview(a)) {
    window.open(router.resolve({ path: `/mail/${mail.value.id}/sheet/${a.id}` }).href, `sheet-${a.id}`)?.focus()
    return
  }
  if (a.previewUrl) window.open(a.previewUrl, '_blank', 'noopener')
}

function avatarStyle(email: string) {
  let h = 0
  for (const ch of email || '') h = (h * 31 + ch.charCodeAt(0)) % 360
  return { background: `hsl(${h} 55% 42%)` }
}

function initialOf(name: string) {
  return (name || '?').trim().charAt(0).toUpperCase()
}
</script>

<style scoped>
/* 一封信占满窗口，两边留出读起来舒服的边。上限 960px：一行字太长，眼睛
   回到行首时会找错行——这是排版里最老的一条规矩，和屏幕多大无关。 */
.mail-window {
  max-width: 960px;
  margin: 0 auto;
  padding: 22px 26px 60px;
  min-height: 100vh;
  background: #ffffff;
}
.waiting {
  min-height: 240px;
}

/* 下面这些和邮件页阅读区里的同名规则一致：同一封信在两个地方长得一样，
   人才不会觉得自己打开的是另一个东西。 */
.in-subject {
  margin: 0 0 14px;
  font-size: 22px;
  font-weight: 400;
  line-height: 1.3;
}
.in-from {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.in-from .avatar {
  margin-right: 4px;
}
.in-name {
  flex: 0 1 auto;
  min-width: 3em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.in-addr,
.in-to {
  flex: 0 2 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.avatar {
  flex: none;
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 50%;
  color: #fff;
  font-size: 15px;
  font-weight: 500;
  user-select: none;
}
.in-when {
  white-space: nowrap;
}
.strong {
  font-weight: 500;
}
.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.grow {
  flex: 1;
}
.details-toggle {
  flex: none;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--el-color-primary);
  font-size: inherit;
  cursor: pointer;
}
.details-toggle:hover {
  text-decoration: underline;
}
.reply-mismatch {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 12px;
  padding: 8px 12px;
  border: 1px solid var(--el-color-warning-light-5);
  border-radius: 8px;
  background: var(--el-color-warning-light-9);
  color: var(--el-color-warning-dark-2);
  font-size: 13px;
}
.mail-details {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 6px 16px;
  margin: 12px 0 0;
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-lighter);
  font-size: 13px;
}
.mail-details dt {
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}
.mail-details dd {
  margin: 0;
  overflow-wrap: anywhere;
}
/* 那排动作按钮。和主窗口阅读区上的同名规则一致——同一个东西在两个地方
   长得一样，人才不会觉得自己打开的是另一个程序。 */
.in-actions {
  display: flex;
  align-items: center;
  flex-wrap: nowrap;
  gap: 6px;
  margin-top: 14px;
}
.tb {
  display: grid;
  place-items: center;
  flex: none;
  width: 36px;
  min-width: 36px;
  height: 34px;
  padding: 0;
  border: 1px solid #b8c9dc;
  border-radius: 8px;
  background: #ffffff;
  color: var(--mail-primary);
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
  box-shadow: 0 1px 2px rgb(35 75 128 / 0.05);
  transition: background var(--mail-fast) var(--mail-ease),
    border-color var(--mail-fast) var(--mail-ease),
    color var(--mail-fast) var(--mail-ease),
    box-shadow var(--mail-fast) var(--mail-ease),
    transform var(--mail-fast) var(--mail-ease);
}
.tb:hover {
  background: var(--mail-wash);
  border-color: var(--mail-soft);
  color: var(--mail-medium);
  box-shadow: 0 2px 6px rgb(35 75 128 / 0.12);
}
.tb:active {
  transform: translateY(1px);
  background: var(--mail-wash);
  box-shadow: inset 0 1px 2px rgb(35 75 128 / 0.16);
}
.tb:focus-visible {
  outline: 2px solid var(--mail-soft);
  outline-offset: 2px;
}
.tb:disabled {
  opacity: 0.5;
  cursor: default;
}
.tb-danger {
  border-color: var(--mail-danger-border);
  background: var(--mail-danger-bg);
  color: var(--mail-danger);
}
.tb-danger:hover {
  border-color: #dfa1a8;
  background: #fde8ea;
  color: #a92f38;
}
.tb-danger:active {
  background: #f9dadd;
  box-shadow: inset 0 1px 2px rgb(194 59 69 / 0.2);
}
.tb-sep {
  flex: none;
  width: 1px;
  height: 18px;
  margin: 0 4px;
  background: var(--mail-wash);
}
.side-title {
  margin: 26px 0 10px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
.att-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 26px 0 10px;
}
.att-head .side-title {
  margin: 0;
}
</style>
