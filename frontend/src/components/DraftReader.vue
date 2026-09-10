<template>
  <div v-if="draft" class="reader">
    <h3 class="subject">{{ draft.subject || t('emails.noSubject') }}</h3>

    <!-- 头部和收到的信是同一个形状（头像 + 两行），但问的是相反的问题：
         收到的信说「谁写的、写给谁」，写了一半的信只有一半答案——写给谁是
         知道的，从哪个箱发出去要到发送那一刻才定。所以这里只写收件人。 -->
    <div class="head">
      <div class="avatar">{{ initial }}</div>
      <div class="head-text">
        <div class="line1">
          <span class="to">{{ t('emails.draftTo') }}</span>
          <span v-if="to.length" class="people">{{ to.join('，') }}</span>
          <!-- 说出来，不留白。一封没有收件人的草稿是很正常的东西（先写完
               正文再想发给谁），而一片空白看着像没加载出来。 -->
          <span v-else class="dim italic">{{ t('emails.draftNoRecipient') }}</span>
          <span class="dim" :title="stampFull">{{ stamp }}</span>
        </div>
        <div v-if="cc.length || bcc.length" class="line2">
          <span v-if="cc.length" class="dim">{{ t('emails.ccLabel') }}：{{ cc.join('，') }}</span>
          <span v-if="bcc.length" class="dim">{{ t('emails.bccLabel') }}：{{ bcc.join('，') }}</span>
        </div>
      </div>
    </div>

    <!-- 正文和收到的信走同一条路：HTML 关进沙箱 iframe（MailBody），纯文本
         原样排。草稿的 HTML 在存的时候就净化过了（SaveDraft），但仍然进
         iframe——净化和隔离各挡一层，少一层就是把「存的时候净化对了」当成
         前提，而那正是最不该当成前提的事。 -->
    <MailBody v-if="draft.bodyFormat === 'HTML' && draft.body" class="body" :html="draft.body" />
    <pre v-else-if="draft.body" class="body text">{{ draft.body }}</pre>
    <p v-else class="body empty">{{ t('emails.draftNoBody') }}</p>

    <div v-if="draft.attachments?.length" class="files">
      <div class="side-title">{{ t('reader.attachments', { n: draft.attachments.length }) }}</div>
      <div class="chips">
        <el-tag v-for="a in draft.attachments" :key="a.id" type="info" effect="plain">
          {{ a.fileName }}
        </el-tag>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { shortTime, zonedStamp } from '../lib/zonedtime'
import MailBody from './MailBody.vue'

interface Person {
  email: string
  name?: string
}

/** 展开来看的一封草稿。GetDraft 给的那一份，只列这里用得到的字段。 */
export interface DraftDetail {
  id: string
  subject?: string
  body?: string
  bodyFormat?: string
  updatedAt?: string
  recipients?: Person[]
  cc?: Person[]
  bcc?: Person[]
  attachments?: { id: string; fileName: string }[]
}

const props = defineProps<{ draft: DraftDetail | null }>()
const { t } = useI18n()

function names(list?: Person[]): string[] {
  return (list ?? []).filter((p) => p && (p.name || p.email)).map((p) => p.name || p.email)
}

const to = computed(() => names(props.draft?.recipients))
const cc = computed(() => names(props.draft?.cc))
const bcc = computed(() => names(props.draft?.bcc))

// 保存的时刻。草稿没有「发出」也没有「收到」，它唯一的时间就是上次存下来
// 的那一下——而那正是人在列表里找它时记得的东西。
const stamp = computed(() => shortTime(props.draft?.updatedAt || ''))
const stampFull = computed(() => zonedStamp(props.draft?.updatedAt || ''))

// 头像上那个字取第一个收件人。一个收件人都没有时给个铅笔——空圆圈看着像
// 没加载完，而铅笔正好是「这封还在写」。
const initial = computed(() => {
  const first = to.value[0] || ''
  const ch = [...first.trim()].find((c) => /[\p{L}\p{N}]/u.test(c))
  return ch ? ch.toUpperCase() : '✎'
})
</script>

<style scoped>
/* 和 MailReader 同一套尺寸：两个阅读区在同一栏里轮流出现，字号或间距不一样
   的话，点一下草稿再点一下收件箱，整栏会跳一下。 */
.reader {
  padding: 2px 2px 20px;
}
.subject {
  margin: 0 0 16px;
  font-size: 19px;
  font-weight: 500;
  line-height: 1.4;
}
.head {
  display: flex;
  gap: 12px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.avatar {
  flex: none;
  width: 38px;
  height: 38px;
  border-radius: 50%;
  background: var(--el-color-primary-light-7);
  color: var(--el-color-primary);
  font-weight: 600;
  display: flex;
  align-items: center;
  justify-content: center;
}
.head-text {
  min-width: 0;
  flex: 1;
}
.line1 {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 6px;
}
.line2 {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 5px;
}
.to {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.people {
  font-weight: 500;
}
.dim {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
/* 斜体而不是红色：还没写收件人是这封信的进度，不是一个要人去修的错。 */
.italic {
  font-style: italic;
}
.body {
  margin: 18px 0 0;
  font-size: 14px;
  line-height: 1.65;
}
.body.text {
  white-space: pre-wrap;
  font-family: inherit;
}
.body.empty {
  color: var(--el-text-color-placeholder);
  font-style: italic;
}
.files {
  margin-top: 20px;
  padding-top: 14px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.side-title {
  margin: 0 0 8px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-regular);
}
</style>
