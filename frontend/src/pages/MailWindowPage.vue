<!-- 一封信，自己一个窗口。
     双击列表里的一行就弹这个（照 Foxmail、Outlook、Apple Mail：桌面邮件
     客户端双击一封信都是开一个新窗口）。为什么值得有：邮件页是三栏，正文
     那一栏再宽也只有半个屏幕，而客户发来的报价单、带表格的装箱单、长长的
     往来记录，本来就该占满一块屏幕去看——甚至摆到第二块屏幕上，一边看信
     一边在主窗口里填单子。

     看信**和回信**。回复、回复全部、转发在这儿——客户的信摊在整块屏幕上，
     回信这件事本来就该在同一个窗口里完成，而不是"看完这一屏、回主窗口、
     再把信找出来"。

     挪文件夹、归档、删除仍然只在主窗口：那几件事会改列表的样子（这封信还在
     不在、在哪个文件夹、勾没勾上），而两个窗口各改各的，谁也不知道对方改了
     什么。回信不改列表，所以没有这个问题。

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
      <div v-if="canWrite" class="in-actions">
        <el-tooltip :content="t('emails.reply')" placement="bottom" :show-after="0" :hide-after="0">
          <button type="button" class="tb" :aria-label="t('emails.reply')" @click="reply">↩</button>
        </el-tooltip>
        <el-tooltip :content="t('emails.replyAll')" placement="bottom" :show-after="0" :hide-after="0">
          <button type="button" class="tb" :aria-label="t('emails.replyAll')" @click="replyAll">↩↩</button>
        </el-tooltip>
        <el-tooltip :content="t('emails.forward')" placement="bottom" :show-after="0" :hide-after="0">
          <button type="button" class="tb" :aria-label="t('emails.forward')" @click="forward">↪</button>
        </el-tooltip>
      </div>

      <el-divider />

      <!-- 正文一律在沙箱 frame 里渲染，纯文本也不例外——这条由
           scripts/check-mail-sandbox.sh 守着。 -->
      <MailBody v-if="mail.bodyHtml" :html="mail.bodyHtml" />
      <MailBody v-else :html="plainTextToHtml(mail.bodyText || '')" />
      <QuotedHistory v-if="mail.quotedHtml" :html="mail.quotedHtml" />

      <template v-if="mail.attachments?.length">
        <el-divider />
        <h4 class="side-title">{{ t('emails.attachments') }}</h4>
        <MailAttachments
          :files="mail.attachments"
          :converting="converting"
          :mail-id="String(mail.id)"
          @preview="openPreview"
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
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { WarningFilled } from '@element-plus/icons-vue'
import { get, post, quietErrors } from '../api'
import { useAuthStore } from '../stores/auth'
import { replyAllRecipients } from '../lib/replyAll'
import EmailComposer from '../components/EmailComposer.vue'
import MailBody from '../components/MailBody.vue'
import QuotedHistory from '../components/QuotedHistory.vue'
import MailAttachments, { type MailFile } from '../components/MailAttachments.vue'
import { mailDetailRows, replyToDiffers } from '../lib/mailDetails'
import { isSheetPreview, needsConversion } from '../lib/attachmentPreview'
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
const { t } = useI18n()
const auth = useAuthStore()

// 没有发信权限就不给这三颗按钮——和主窗口阅读区同一个判断。
const canWrite = computed(() => auth.can('mail:email:write'))

const mail = ref<WindowMail | null>(null)
const loading = ref(true)
const failed = ref(false)
const detailsOpen = ref(false)
const converting = ref('')
const composing = ref(false)
const composer = ref<{
  openReply: (m: WindowMail) => void
  openReplyAll: (m: WindowMail, who: { to: { name?: string; email: string }; cc: { name?: string; email: string }[] }) => void
  openForward: (m: WindowMail) => void
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

// 预览一律新开一个标签页，和邮件页那边同一条规矩（见 EmailsPage 的
// openPreview，那儿写着三条路各自的理由）。表格自己解自己画，不经过服务器；
// Word/PPT 还得先转一趟 PDF。
async function openPreview(a: MailFile) {
  if (!mail.value) return
  if (isSheetPreview(a)) {
    window.open(router.resolve({ path: `/mail/${mail.value.id}/sheet/${a.id}` }).href, `sheet-${a.id}`)?.focus()
    return
  }
  if (!needsConversion(a)) {
    if (a.previewUrl) window.open(a.previewUrl, '_blank', 'noopener')
    return
  }
  if (converting.value) return
  // 先占住标签页（此刻还在这次点击的手势里），转好了再把地址填进去。
  const tab = window.open('', `preview-${a.id}`)
  converting.value = a.id
  try {
    const resp = await post<{ previewUrl?: string }>(
      `/inbound-mails/${mail.value.id}/attachments/${a.id}/preview`,
    )
    if (!resp?.previewUrl) throw new Error('no url')
    // 记在附件上：同一份文件第二次点是直接开的，连请求都不发。阅读区那边
    // 也是这么做的。
    a.previewUrl = resp.previewUrl
    if (tab) tab.location.replace(resp.previewUrl)
    else window.open(resp.previewUrl, '_blank', 'noopener')
  } catch {
    // 转不了的理由后端已经用消息说了，拦截器会弹；这里不再叠一层。
    tab?.close()
  } finally {
    converting.value = ''
  }
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
  gap: 2px;
  margin-top: 14px;
}
.tb {
  display: grid;
  place-items: center;
  flex: none;
  min-width: 34px;
  height: 32px;
  padding: 0 6px;
  border: none;
  border-radius: 8px;
  background: none;
  color: var(--el-text-color-regular);
  /* 箭头是字形，不是图标字体：字号大一档才和一行 14px 的正文视觉上等重。 */
  font-size: 17px;
  line-height: 1;
  cursor: pointer;
  transition: background var(--mail-fast) var(--mail-ease),
    color var(--mail-fast) var(--mail-ease);
}
.tb:hover {
  background: var(--el-fill-color);
  color: var(--el-text-color-primary);
}
.tb:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: -2px;
}
.side-title {
  margin: 26px 0 10px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
</style>
