<!-- 一封信，自己一个窗口。
     双击列表里的一行就弹这个（照 Foxmail、Outlook、Apple Mail：桌面邮件
     客户端双击一封信都是开一个新窗口）。为什么值得有：邮件页是三栏，正文
     那一栏再宽也只有半个屏幕，而客户发来的报价单、带表格的装箱单、长长的
     往来记录，本来就该占满一块屏幕去看——甚至摆到第二块屏幕上，一边看信
     一边在主窗口里填单子。

     它是**读信的窗口**，不是第二个邮件页：回复、转发、归档、挪文件夹都还在
     主窗口的阅读区里。这里只有看和拿——正文、引用历史、附件。理由是这些动作
     都会改变列表的状态（已读、所在文件夹、勾选），两个窗口各改各的，谁也不
     知道对方改了什么；而"看"没有这个问题。

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
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { WarningFilled } from '@element-plus/icons-vue'
import { get, post } from '../api'
import MailBody from '../components/MailBody.vue'
import QuotedHistory from '../components/QuotedHistory.vue'
import MailAttachments, { type MailFile } from '../components/MailAttachments.vue'
import { mailDetailRows, replyToDiffers } from '../lib/mailDetails'
import { needsConversion } from '../lib/attachmentPreview'
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
const { t } = useI18n()

const mail = ref<WindowMail | null>(null)
const loading = ref(true)
const failed = ref(false)
const detailsOpen = ref(false)
const converting = ref('')

const detailRows = computed(() => mailDetailRows(mail.value, t))

onMounted(async () => {
  const id = String(route.params.id || '')
  try {
    const d = await get<{ mail: WindowMail }>(`/inbound-mails/${id}`)
    mail.value = d.mail
    // 任务栏和标签页上写这封信的主题。一个人开着三个这样的窗口时，三个
    // 一模一样的标题等于没有标题。
    document.title = d.mail.subject || t('emails.noSubject')
  } catch {
    // 信被删了、id 不是自己的、或者信箱这会儿是锁着的。分不出是哪一种，
    // 也不该猜——一句「打不开，回主窗口看看」比一个白屏诚实。
    failed.value = true
  } finally {
    loading.value = false
  }
})

// 预览在新标签页里开，不在这个窗口里弹对话框：这已经是一个弹出来的窗口了，
// 在它上面再叠一层对话框，人会分不清关掉的是哪个。办公文档要先转一趟，
// 转完拿到地址再开。
async function openPreview(a: MailFile) {
  if (!needsConversion(a)) {
    if (a.previewUrl) window.open(a.previewUrl, '_blank', 'noopener')
    return
  }
  if (converting.value || !mail.value) return
  converting.value = a.id
  try {
    const resp = await post<{ previewUrl?: string }>(
      `/inbound-mails/${mail.value.id}/attachments/${a.id}/preview`,
    )
    if (!resp?.previewUrl) return
    // 记在附件上：同一份文件第二次点是直接开的，连请求都不发。阅读区那边
    // 也是这么做的。
    a.previewUrl = resp.previewUrl
    window.open(resp.previewUrl, '_blank', 'noopener')
  } catch {
    // 转不了的理由后端已经用消息说了，拦截器会弹；这里不再叠一层。
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
.side-title {
  margin: 26px 0 10px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
</style>
