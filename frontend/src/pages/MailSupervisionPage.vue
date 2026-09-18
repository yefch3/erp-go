<!-- 员工邮箱监管（老板端）。

     左边一棵「国家 → 员工 → 收件箱 / 已发送」的树，右边是那个人的信。

     **国家不是员工的属性**，是他负责的客户的：张三接了巴西的单子，他就在
     巴西下面，不必有人去改一次员工资料。所以一个人负责几个国家就在几个
     国家下各出现一次——那是实情，不是要修的毛病。一个客户都没分到的人在
     「未分配国家」那一档，他照样有信箱，照样可能是要看的那个。

     这一页是**只读**的：不标已读、不回信、不挪信。看一眼别人的收件箱不该
     在员工自己的列表里留下任何痕迹——痕迹该留在监管日志里，那是服务端每
     读一次就写一行的东西（写不进去就不给读）。 -->
<template>
  <div class="sv">
    <!-- ---------------------------------------------------------- 左：树 -->
    <aside class="rail">
      <div class="rail-head">{{ t('supervision.title') }}</div>
      <p class="rail-note">{{ t('supervision.note') }}</p>

      <el-skeleton v-if="loadingTree" :rows="6" animated />
      <el-empty v-else-if="!countries.length" :description="t('supervision.emptyTree')" />

      <template v-for="c in countries" v-else :key="c.countryCode || '_'">
        <button type="button" class="row country" @click="toggleCountry(c.countryCode)">
          <el-icon class="caret" :class="{ open: isOpen(c.countryCode) }"><CaretRight /></el-icon>
          <span class="name">{{ countryLabel(c.countryCode) }}</span>
          <span class="cnt">{{ c.employees.length }}</span>
        </button>

        <template v-if="isOpen(c.countryCode)">
          <template v-for="e in c.employees" :key="c.countryCode + ':' + e.employeeId">
            <button
              type="button"
              class="row person"
              :class="{ on: e.employeeId === employeeId }"
              @click="pickEmployee(e)"
            >
              <el-icon class="ficon"><User /></el-icon>
              <span class="name">{{ e.name || t('supervision.unnamed', { id: e.employeeId }) }}</span>
              <span v-if="e.unread > 0" class="cnt unread">{{ e.unread > 99 ? '99+' : e.unread }}</span>
            </button>
            <!-- 收件箱 / 已发送。只在选中这个人的时候摊开：一屏里同时摊开
                 三十个人的两个文件夹，等于没有层级。 -->
            <template v-if="e.employeeId === employeeId">
              <button
                v-for="f in FOLDERS"
                :key="f"
                type="button"
                class="row folder"
                :class="{ on: folder === f }"
                @click="pickFolder(f)"
              >
                <span class="name">{{ t(`supervision.folders.${f}`) }}</span>
              </button>
            </template>
          </template>
        </template>
      </template>
    </aside>

    <!-- -------------------------------------------------------- 中：列表 -->
    <section class="list">
      <div v-if="employeeId" class="list-head">
        <span class="who">{{ currentName }}</span>
        <span class="sub">{{ t(`supervision.folders.${folder}`) }}</span>
        <span class="grow" />
        <el-input
          v-model="keyword"
          size="small"
          clearable
          style="width: 190px"
          :placeholder="t('supervision.search')"
          @keyup.enter="reload"
          @clear="reload"
        />
      </div>
      <el-empty v-if="!employeeId" :description="t('supervision.pickSomebody')" />
      <template v-else>
        <el-skeleton v-if="loadingList" :rows="5" animated />
        <el-empty v-else-if="!mails.length" :description="t('supervision.emptyFolder')" />
        <ul v-else class="mails">
          <li
            v-for="m in mails"
            :key="m.id"
            class="mail"
            :class="{ on: m.id === openedId, unread: !m.isRead }"
            @click="openMail(m.id)"
          >
            <div class="line">
              <span class="party">{{ partyOf(m) }}</span>
              <span class="when">{{ shortTime(m) }}</span>
            </div>
            <div class="subject">{{ m.subject || t('supervision.noSubject') }}</div>
            <div class="snippet">{{ m.snippet }}</div>
          </li>
        </ul>
        <div v-if="nextCursor" class="more">
          <el-button link type="primary" :loading="loadingMore" @click="loadMore">
            {{ t('supervision.more') }}
          </el-button>
        </div>
      </template>
    </section>

    <!-- -------------------------------------------------------- 右：读信 -->
    <section class="reader">
      <el-empty v-if="!opened" :description="t('supervision.pickMail')" />
      <template v-else>
        <h2 class="subject">{{ opened.subject || t('supervision.noSubject') }}</h2>
        <dl class="meta">
          <div><dt>{{ t('supervision.from') }}</dt><dd>{{ opened.fromName ? opened.fromName + ' ' : '' }}&lt;{{ opened.fromEmail }}&gt;</dd></div>
          <div><dt>{{ t('supervision.to') }}</dt><dd>{{ opened.toAll || opened.toEmail }}</dd></div>
          <div v-if="opened.cc"><dt>{{ t('supervision.cc') }}</dt><dd>{{ opened.cc }}</dd></div>
          <div><dt>{{ t('supervision.at') }}</dt><dd>{{ longTime(opened) }}</dd></div>
          <div>
            <dt>{{ t('supervision.state') }}</dt>
            <dd>{{ opened.isRead ? t('supervision.read') : t('supervision.unread') }}</dd>
          </div>
        </dl>

        <!-- 附件。下载地址是服务端签好的对象存储直链，和员工自己看到的是
             同一份文件。这里**只给下载**，不给在线编辑——编辑会改动员工的
             附件，而这一页是只读的。 -->
        <div v-if="opened.attachments?.length" class="atts">
          <span class="atts-head">{{ t('supervision.attachments') }}</span>
          <a
            v-for="a in opened.attachments"
            :key="a.id"
            class="att"
            :href="a.downloadUrl"
            target="_blank"
            rel="noopener"
          >📎 {{ a.fileName }} <span class="size">{{ humanSize(Number(a.fileSize || 0)) }}</span></a>
        </div>

        <MailBody v-if="opened.bodyHtml" :html="opened.bodyHtml" />
        <pre v-else class="plain">{{ opened.bodyText }}</pre>

        <!-- 整条会话。一封回信单独看是半句话。 -->
        <div v-if="thread.length > 1" class="thread">
          <div class="thread-head">{{ t('supervision.thread', { n: thread.length }) }}</div>
          <details v-for="it in thread" :key="it.direction + it.id" class="turn">
            <summary>
              <span class="dir">{{ it.direction === 'OUT' ? t('supervision.out') : t('supervision.in') }}</span>
              {{ it.fromName || it.fromEmail }} · {{ it.at?.slice(0, 16).replace('T', ' ') }}
            </summary>
            <MailBody v-if="it.bodyFormat === 'HTML'" :html="it.body" />
            <pre v-else class="plain">{{ it.body }}</pre>
          </details>
        </div>
      </template>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { CaretRight, User } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { get } from '../api'
import MailBody from '../components/MailBody.vue'
import { countryName } from '../lib/countries'
import { humanSize } from '../lib/humanSize'

const FOLDERS = ['inbox', 'sent'] as const
type Folder = (typeof FOLDERS)[number]

interface SupervisedEmployee {
  employeeId: string
  name: string
  code: string
  mailboxes: number
  unread: number
  customers: number
}
interface SupervisedCountry {
  countryCode: string
  employees: SupervisedEmployee[]
}
interface SupervisedMail {
  id: string
  fromEmail: string
  fromName: string
  toEmail: string
  toAll: string
  cc: string
  subject: string
  snippet: string
  isRead: boolean
  receivedAt: string
  sentAt: string
  bodyHtml: string
  bodyText: string
  attachments?: { id: string; fileName: string; fileSize: string; downloadUrl: string }[]
}
interface ThreadTurn {
  direction: string
  id: string
  fromEmail: string
  fromName: string
  body: string
  bodyFormat: string
  at: string
}

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()

const countries = ref<SupervisedCountry[]>([])
const loadingTree = ref(true)
const openCountries = ref<string[]>([])
const employeeId = ref('')
const folder = ref<Folder>('inbox')
const keyword = ref('')

const mails = ref<SupervisedMail[]>([])
const nextCursor = ref('')
const loadingList = ref(false)
const loadingMore = ref(false)

const openedId = ref('')
const opened = ref<SupervisedMail | null>(null)
const thread = ref<ThreadTurn[]>([])

const currentName = computed(() => {
  for (const c of countries.value) {
    for (const e of c.employees) {
      if (e.employeeId === employeeId.value) return e.name || e.employeeId
    }
  }
  return employeeId.value
})

function countryLabel(code: string): string {
  return code ? countryName(code, locale.value) : t('supervision.noCountry')
}
function isOpen(code: string) {
  return openCountries.value.includes(code)
}
function toggleCountry(code: string) {
  openCountries.value = isOpen(code)
    ? openCountries.value.filter((c) => c !== code)
    : [...openCountries.value, code]
}

// 地址栏记着「看谁、看哪个文件夹、开着哪封」：刷新回到原处，也能把一个
// 位置发给别人。监管本来就是要留痕的事，能被引用是好事。
function pushState(next: Record<string, string>) {
  void router.replace({ query: { ...route.query, ...next } })
}

function partyOf(m: SupervisedMail): string {
  // 收件箱看发信的是谁，已发送看发给了谁。同一列两种含义，写反了整页就读不通。
  if (folder.value === 'sent') return m.toAll || m.toEmail || '—'
  return m.fromName || m.fromEmail || '—'
}
function stamp(m: SupervisedMail): string {
  return m.receivedAt || m.sentAt || ''
}
function shortTime(m: SupervisedMail): string {
  return stamp(m).slice(0, 16).replace('T', ' ')
}
function longTime(m: SupervisedMail): string {
  return stamp(m).slice(0, 19).replace('T', ' ')
}

async function loadTree() {
  loadingTree.value = true
  try {
    const d = await get<{ countries: SupervisedCountry[] }>('/mail-supervision/tree')
    countries.value = d.countries ?? []
    // 第一次进来把有人的国家都摊开：一棵全收起来的树看不出有什么。
    if (!openCountries.value.length) {
      openCountries.value = countries.value.map((c) => c.countryCode)
    }
  } finally {
    loadingTree.value = false
  }
}

function pickEmployee(e: SupervisedEmployee) {
  if (employeeId.value === e.employeeId) return
  employeeId.value = e.employeeId
  openedId.value = ''
  opened.value = null
  pushState({ employee: e.employeeId, folder: folder.value, mail: '' })
  void reload()
}
function pickFolder(f: Folder) {
  if (folder.value === f) return
  folder.value = f
  openedId.value = ''
  opened.value = null
  pushState({ folder: f, mail: '' })
  void reload()
}

async function reload() {
  if (!employeeId.value) return
  loadingList.value = true
  nextCursor.value = ''
  try {
    const d = await fetchPage('')
    mails.value = d.mails
    nextCursor.value = d.nextCursor
  } finally {
    loadingList.value = false
  }
}
async function loadMore() {
  if (!nextCursor.value) return
  loadingMore.value = true
  try {
    const d = await fetchPage(nextCursor.value)
    mails.value = [...mails.value, ...d.mails]
    nextCursor.value = d.nextCursor
  } finally {
    loadingMore.value = false
  }
}
async function fetchPage(cursor: string) {
  const d = await get<{ mails: SupervisedMail[]; nextCursor: string }>('/mail-supervision/mails', {
    employee_id: employeeId.value,
    folder: folder.value,
    keyword: keyword.value.trim(),
    cursor,
    page_size: 25,
  })
  return { mails: d.mails ?? [], nextCursor: d.nextCursor ?? '' }
}

async function openMail(id: string) {
  openedId.value = id
  pushState({ mail: id })
  const d = await get<{ mail: SupervisedMail; thread: ThreadTurn[] }>(
    `/mail-supervision/mails/${id}`,
    { employee_id: employeeId.value },
  )
  opened.value = d.mail
  thread.value = d.thread ?? []
}

onMounted(async () => {
  await loadTree()
  const q = route.query
  if (typeof q.employee === 'string' && q.employee) employeeId.value = q.employee
  if (q.folder === 'sent') folder.value = 'sent'
  if (employeeId.value) {
    await reload()
    if (typeof q.mail === 'string' && q.mail) await openMail(q.mail)
  }
})

// 换了人或文件夹之后，正开着的那封信不属于这里了。
watch([employeeId, folder], () => {
  thread.value = []
})
</script>

<style scoped>
.sv {
  display: grid;
  grid-template-columns: 250px 340px 1fr;
  gap: 12px;
  height: calc(100vh - 120px);
}
.rail,
.list,
.reader {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  padding: 10px;
  overflow: auto;
}
.rail-head {
  font-weight: 600;
  margin-bottom: 2px;
}
.rail-note {
  margin: 0 0 8px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.6;
}
.row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  box-sizing: border-box;
  padding: 6px 8px;
  margin-bottom: 2px;
  border: none;
  background: none;
  border-radius: 6px;
  font: inherit;
  color: var(--el-text-color-regular);
  text-align: left;
  cursor: pointer;
}
.row:hover {
  background: var(--el-fill-color-light);
}
.row.on {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-weight: 600;
}
/* 层级靠缩进和图标颜色分，和邮件左栏那棵树一个路子。 */
.country .name {
  font-weight: 600;
}
.person {
  padding-left: 22px;
}
.person .ficon {
  color: var(--el-color-primary);
}
.folder {
  padding-left: 46px;
  font-size: 12.5px;
  color: var(--el-text-color-secondary);
}
.caret {
  flex: none;
  width: 12px;
  transition: transform 0.15s ease;
}
.caret.open {
  transform: rotate(90deg);
}
.name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.cnt {
  flex: none;
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
.cnt.unread {
  color: var(--el-color-primary);
  font-weight: 600;
}
.list-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  margin-bottom: 6px;
}
.who {
  font-weight: 600;
}
.sub,
.snippet {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.grow {
  flex: 1;
}
.mails {
  margin: 0;
  padding: 0;
  list-style: none;
}
.mail {
  padding: 8px;
  border-radius: 6px;
  cursor: pointer;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.mail:hover {
  background: var(--el-fill-color-light);
}
.mail.on {
  background: var(--el-color-primary-light-9);
}
.mail.unread .subject {
  font-weight: 700;
}
.line {
  display: flex;
  gap: 8px;
  font-size: 12px;
}
.party {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 600;
}
.when {
  flex: none;
  color: var(--el-text-color-secondary);
}
.subject,
.snippet {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.more {
  text-align: center;
  padding: 8px 0;
}
.reader .subject {
  margin: 0 0 10px;
  font-size: 18px;
}
.meta {
  margin: 0 0 10px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.meta div {
  display: flex;
  gap: 8px;
}
.meta dt {
  flex: none;
  width: 56px;
}
.meta dd {
  margin: 0;
  min-width: 0;
  word-break: break-all;
}
.atts {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  padding: 8px 0;
  border-top: 1px solid var(--el-border-color-lighter);
  border-bottom: 1px solid var(--el-border-color-lighter);
  margin-bottom: 10px;
  font-size: 12px;
}
.atts-head {
  color: var(--el-text-color-secondary);
}
.size {
  color: var(--el-text-color-secondary);
}
.plain {
  white-space: pre-wrap;
  word-break: break-word;
  font: inherit;
  margin: 0;
}
.thread {
  margin-top: 14px;
  border-top: 1px solid var(--el-border-color-lighter);
  padding-top: 10px;
}
.thread-head {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
}
.turn summary {
  cursor: pointer;
  font-size: 12px;
  padding: 4px 0;
}
.dir {
  display: inline-block;
  margin-right: 6px;
  padding: 0 6px;
  border-radius: 4px;
  background: var(--el-fill-color);
}
</style>
