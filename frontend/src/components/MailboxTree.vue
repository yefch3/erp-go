<!-- 左栏：每个信箱一棵可折叠的小树，文件夹长在它下面。

     照 Foxmail / Outlook 那个样子做的，而它之所以是那个样子，是因为**文件夹
     本来就属于某一个信箱**：「已发送」问的是"从这个地址发出去的"，「收件箱」
     问的是"寄到这个地址的"。从前左栏把文件夹摆在最上面、信箱摆在下面，等于
     让人先选动词再选主语——三个箱之后就答不上来"我现在看的已发送是谁的"。

     两个例外没有挂进树里，见 lib/mailFolders：待处理按人取、拒收名单按公司
     取，挂在某个箱底下就是在说一件不成立的事。 -->
<template>
  <nav class="rail-tree" :aria-label="t('mailGate.emailLabel')">
    <!-- 一个箱都没绑：树没有主语，文件夹平铺，人照样看得见这一页有什么。
         锁着时连这个也不画——那些路由全要解锁令牌，画出来点了也只是没反应。 -->
    <template v-if="!boxes.length && !locked">
      <button
        v-for="f in folders"
        :key="f.key"
        type="button"
        class="folder"
        :class="{ on: folder === f.key }"
        @click="emit('select', 0, f.key)"
      >
        <el-icon class="ficon"><component :is="f.icon" /></el-icon>
        <span class="fname">{{ t(`emails.folders.${f.key}`) }}</span>
        <!-- 这一支也要有数字。改版时漏掉过：从前那份平铺的清单每个人都看得到
             角标，而这条支路只画了图标和名字——一个箱都没绑、直接用通行证
             进来的人（MailboxGate 的 skipUnbound）于是看不见自己有几封草稿。 -->
        <span v-if="countOf(f.key) > 0" class="cnt">
          {{ countOf(f.key) > 99 ? '99+' : countOf(f.key) }}
        </span>
      </button>
    </template>

    <div v-for="b in boxes" :key="b.id" class="mbox-group">
      <!-- 一行三个动作：展开/收起、解绑、设为默认。所以是并排的按钮，不是
           一个按钮里套另一个——嵌套的可点击元素在 HTML 里是非法的，浏览器会
           把内层拆出去，而键盘和读屏软件对拆完的结果各有各的理解。 -->
      <div class="mbox-row" :class="{ on: modelValue === b.id }">
        <!-- **整行都能开合**，不是只有那颗三角。
             一颗 12px 的三角是个太小的靶子，而「点这一行」是人看到一棵树时
             的第一反应；把开合塞进箭头里，等于让人先瞄准再点。

             那切换信箱靠什么？靠点下面某个文件夹——那一下同时回答了「哪个箱」
             和「看什么」，比单点箱名多说了一半。右边那两颗（解绑、设为默认）
             是并排的兄弟按钮，不是套在里面的：嵌套的可点击元素在 HTML 里非法，
             浏览器会把内层拆出去，而键盘和读屏软件对拆完的结果各有各的理解。 -->
        <button
          class="mbox"
          type="button"
          :title="b.email"
          :aria-expanded="isOpen(b.id)"
          :aria-current="modelValue === b.id ? 'true' : undefined"
          @click="toggle(b.id)"
        >
          <el-icon class="caret" :class="{ open: isOpen(b.id) }"><CaretRight /></el-icon>
          <span
            class="mbox-dot"
            :class="{ bad: b.needsReauth, warn: !!b.lastError && !b.needsReauth, off: isLocked(b.id) }"
            :title="b.lastError || undefined"
          />
          <span class="mbox-name">{{ b.email }}</span>
          <!-- 未读角标。**当前这个箱不显示**：你正看着它，下面收件箱那一行
               已经把数字写出来了，再挂一个只是重复。它的用处是「另一个箱里
               有东西」——那才是需要一眼看见的。 -->
          <span v-if="b.unread > 0 && modelValue !== b.id" class="mbox-unread">
            {{ b.unread > 99 ? '99+' : b.unread }}
          </span>
          <!-- 已解绑：还在树里，因为历史邮件的入口就是这一行。标出来是为了
               人点进去看到「不能写信」时知道为什么。 -->
          <span v-if="b.unboundAt" class="mbox-tag mbox-off">{{ t('mailGate.unbound') }}</span>
          <span v-else-if="b.isDefault" class="mbox-tag">{{ t('mailGate.isDefault') }}</span>
        </button>
        <!-- 两个动作各占一格，**没有那个动作时留空格而不是不占位**。
             不留的话每行尾部宽度不一样（默认那行没有「设为默认」），名字那
             一列跟着长短不一，一排信箱看上去是歪的。

             空格用 span 不用透明按钮：opacity:0 的按钮照样可点，在触屏上就是
             一颗看不见的按钮盖在那儿。 -->
        <span class="mbox-actions">
          <!-- 解绑。**说清楚它不删邮件**——不说的话，一个只是想换邮箱的人会
               因为怕丢记录而不敢点，然后一直留着一个不用的箱。 -->
          <el-popconfirm
            v-if="!b.unboundAt"
            :title="t('mailGate.unbindConfirm', { email: b.email })"
            width="280"
            @confirm="unbind(b)"
          >
            <template #reference>
              <el-button
                link
                class="mbox-act"
                :aria-label="t('mailGate.unbind')"
                :title="t('mailGate.unbind')"
              >
                <el-icon><SwitchButton /></el-icon>
              </el-button>
            </template>
          </el-popconfirm>
          <span v-else class="mbox-act" />

          <!-- 设为默认只在非默认的那几行上出现，而且要点两次才生效：它改的是
               「以后写信从哪个地址发出去」，而客户看到的发件人跟着变。

               是 el-button 而不是一个透明的 span：span 用键盘 tab 不到。 -->
          <el-popconfirm
            v-if="!b.isDefault && !b.unboundAt"
            :title="t('mailGate.setDefault')"
            @confirm="setDefault(b)"
          >
            <template #reference>
              <el-button
                link
                class="mbox-act"
                :aria-label="t('mailGate.setDefault')"
                :title="t('mailGate.setDefault')"
              >
                <el-icon><Star /></el-icon>
              </el-button>
            </template>
          </el-popconfirm>
          <span v-else class="mbox-act" />
        </span>
      </div>

      <div v-if="isOpen(b.id)" class="mbox-folders">
        <!-- 还没登录这个箱：**不列那八个文件夹**。列了也点不进去，而一排
             点不动的文件夹看着就是坏了。给一行说清楚该做什么的。 -->
        <button
          v-if="isLocked(b.id)"
          type="button"
          class="folder sub locked"
          @click="emit('update:modelValue', b.id)"
        >
          <span class="ficon">🔒</span>
          <span class="fname">{{ t('mailGate.signInToRead') }}</span>
        </button>
        <button
          v-for="f in perMailbox"
          v-else
          :key="f.key"
          type="button"
          class="folder sub"
          :class="{ on: modelValue === b.id && folder === f.key }"
          @click="emit('select', b.id, f.key)"
        >
          <el-icon class="ficon"><component :is="f.icon" /></el-icon>
          <span class="fname">{{ t(`emails.folders.${f.key}`) }}</span>
          <!-- 数字只给当前这个箱：别的箱的分文件夹计数服务端没给，
               编一个出来比空着坏得多。 -->
          <span v-if="modelValue === b.id && countOf(f.key) > 0" class="cnt">
            {{ countOf(f.key) > 99 ? '99+' : countOf(f.key) }}
          </span>
        </button>
        <!-- 自建文件夹（Issue #362）：名字来自数据，不来自文案。它们真的建在
             邮件服务器上，Foxmail 里也看得到。 -->
        <template v-if="!isLocked(b.id)">
          <div
            v-for="cf in customFolders[b.id] ?? []"
            :key="cf.viewKey"
            class="folder sub custom"
            :class="{ on: modelValue === b.id && folder === cf.viewKey }"
          >
            <button type="button" class="custom-main" @click="emit('select', b.id, cf.viewKey)">
              <el-icon class="ficon"><Folder /></el-icon>
              <span class="fname">{{ cf.name }}</span>
            </button>
            <span class="custom-acts">
              <button type="button" class="custom-act" :title="t('mailGate.renameFolder')" @click.stop="emit('renameFolder', cf)">✎</button>
              <button type="button" class="custom-act" :title="t('mailGate.deleteFolder')" @click.stop="emit('deleteFolder', cf)">✕</button>
            </span>
          </div>
          <button type="button" class="folder sub new-folder" @click="emit('createFolder', b.id)">
            <span class="ficon">＋</span>
            <span class="fname">{{ t('mailGate.newFolder') }}</span>
          </button>
        </template>
      </div>
    </div>

    <el-button v-if="canAdd" link class="mbox-add" @click="adding = true">
      ＋ {{ t('mailGate.addMailbox') }}
    </el-button>

    <!-- 不跟信箱走的那一个（拒收名单）。**不给它一行标题**：那行字比它
         要解释的东西还长，而它到底为什么在树外面，说明留在 title 里——
         鼠标停上去看得到，平时不占地方。位置本身已经在说这件事了：它没有
         缩进到任何一个信箱底下，和上面隔着一条线。 -->
    <div v-if="boxes.length && shared.length && !locked" class="shared">
      <button
        v-for="f in shared"
        :key="f.key"
        type="button"
        class="folder"
        :class="{ on: folder === f.key }"
        :title="t('mailGate.sharedFoldersHint')"
        @click="emit('select', 0, f.key)"
      >
        <el-icon class="ficon"><component :is="f.icon" /></el-icon>
        <span class="fname">{{ t(`emails.folders.${f.key}`) }}</span>
        <span v-if="countOf(f.key) > 0" class="cnt">
          {{ countOf(f.key) > 99 ? '99+' : countOf(f.key) }}
        </span>
      </button>
    </div>

    <!-- append-to-body：这个弹窗开在左侧栏里，而 .rail 是 position: sticky，
         sticky 自成一个层叠上下文——弹窗生在里面就爬不到右边的邮件列表上面
         去，表现是「点了添加邮箱，弹窗被列表盖住只露出个标题」。

         同一个栏里的 MailSignatureDialog 和 MailTemplatesDialog 早就带着这个
         属性，注释里写的就是这件事。 -->
    <el-dialog
      v-model="adding"
      :title="t('mailGate.addMailboxTitle')"
      width="min(440px, 94vw)"
      append-to-body
    >
      <MailboxCredentialsForm
        :submit-label="t('mailGate.addMailbox')"
        @bound="onAdded"
      />
    </el-dialog>
  </nav>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { CaretRight, Star, SwitchButton, Folder } from '@element-plus/icons-vue'
import MailboxCredentialsForm from './MailboxCredentialsForm.vue'
import { adoptVerification, unlockedMailboxes, type VerifyResponse } from '../lib/mailUnlock'
import {
  expandedAfterSwitch,
  parseExpanded,
  splitFolders,
  toggleExpanded,
  type FolderDef,
} from '../lib/mailFolders'
import type { CustomFolder } from '../lib/mailFolders'

export interface Mailbox {
  id: number
  email: string
  isDefault: boolean
  lastError: string
  needsReauth: boolean
  /** 这个箱里有多少封没读。切换的理由就是它。 */
  unread: number
  /** 解绑时间，空表示还绑着。解绑的箱只能看历史，不能收发。 */
  unboundAt: string
}

const props = defineProps<{
  modelValue: number
  canAdd: boolean
  /** 现在停在哪个文件夹。 */
  folder: string
  /** 全部文件夹，带图标。拆成「跟信箱走的」和「不跟的」在 lib/mailFolders。 */
  folders: FolderDef[]
  /** 当前这个信箱各文件夹的数字。别的箱没有，所以只给当前那个用。 */
  counts: Record<string, number>
  /** 令牌变了的信号（解锁、退出、全部退出各拨一次）——localStorage 变化 Vue 看不见。 */
  tokensVersion: number
  /** 当前这个箱锁着没有。锁着时不画点不动的文件夹。 */
  locked: boolean
  /** 每个信箱的自建文件夹。由页面拉取，这里只画。 */
  customFolders: Record<number, CustomFolder[]>
}>()
const emit = defineEmits<{
  'update:modelValue': [number]
  /** 点了某个信箱下的某个文件夹。0 = 不跟信箱走的那两个。 */
  select: [number, string]
  /** 信箱清单变了（新绑了一个、换了默认），页面要跟着重新取列表。 */
  changed: [Mailbox[]]
  /** 刚收下一批新令牌。页面据此重算「哪些箱还开着」。 */
  added: []
  /** 自建文件夹的增删改：输入框和确认框都在页面那边，这里只发信号。 */
  createFolder: [accountId: number]
  renameFolder: [folder: CustomFolder]
  deleteFolder: [folder: CustomFolder]
}>()

const { t } = useI18n()
const boxes = ref<Mailbox[]>([])
const adding = ref(false)

const split = computed(() => splitFolders(props.folders))
const perMailbox = computed(() => split.value.perMailbox)
const shared = computed(() => split.value.shared)

function countOf(key: string): number {
  return Number(props.counts?.[key] ?? 0)
}

// ---------------------------------------------------------------- 开合

const EXPANDED_KEY = 'mail.expandedMailboxes'
const expanded = ref<number[]>(parseExpanded(localStorage.getItem(EXPANDED_KEY)))

function persist() {
  localStorage.setItem(EXPANDED_KEY, JSON.stringify(expanded.value))
}
function isOpen(id: number) {
  return expanded.value.includes(id)
}
function toggle(id: number) {
  expanded.value = toggleExpanded(expanded.value, id)
  persist()
}
// 切过去的那个一定展开：要读它，不能让人再点一下才看得见文件夹。
watch(
  () => props.modelValue,
  (id) => {
    if (!id) return
    const next = expandedAfterSwitch(expanded.value, id)
    if (next.length !== expanded.value.length) {
      expanded.value = next
      persist()
    }
  },
  { immediate: true },
)

// 哪些箱手上还有令牌。localStorage 变化 Vue 看不见，靠 tokensVersion 拨一下。
const openIds = computed(() => {
  void props.tokensVersion
  return new Set(unlockedMailboxes())
})
function isLocked(id: number) {
  return !openIds.value.has(id)
}

// ---------------------------------------------------------------- 数据

async function load() {
  const d = await get<{ accounts?: Mailbox[] }>('/my-mailboxes')
  // 服务端按「默认排最前」返回，这个顺序就是"用哪个箱发信"的答案，
  // 不要在前端再排一次。
  boxes.value = (d.accounts ?? []).map((a) => ({
    id: Number(a.id ?? 0),
    email: a.email ?? '',
    isDefault: !!a.isDefault,
    lastError: a.lastError ?? '',
    needsReauth: !!a.needsReauth,
    // protojson 把 int64 打成字符串，普通 JSON 打成数字。两条路都过一遍
    // Number——这个仓库为同一件事已经踩过一次（见 excelQuota.test.ts）。
    unread: Number(a.unread ?? 0),
    unboundAt: a.unboundAt ?? '',
  }))
  emit('changed', boxes.value)
}

onMounted(load)

async function unbind(b: Mailbox) {
  await post('/my-mailboxes/unbind', { accountId: b.id })
  ElMessage.success(t('mailGate.unbindDone', { email: b.email }))
  await load()
  // 解的是当前正看着的那个：切到剩下的第一个。留在原地的话，右边列的是一个
  // 不能写信的箱，而左边看不出为什么。
  if (b.id === props.modelValue) {
    const rest = boxes.value.filter((x) => !x.unboundAt)
    if (rest.length) emit('update:modelValue', rest[0].id)
  }
}

async function setDefault(b: Mailbox) {
  await post('/my-mailboxes/default', { accountId: b.id })
  ElMessage.success(t('mailGate.setDefaultDone', { email: b.email }))
  await load()
}

async function onAdded(d: VerifyResponse) {
  adding.value = false
  // **先收令牌，再切过去。** 漏了这一步的表现是：刚添加成功的那个箱，
  // 一点就弹回邮箱登录页——它确实绑上了，只是浏览器手上没有它那把令牌，
  // 而切换恰恰是拿令牌换的。
  const acct = adoptVerification(d)
  ElMessage.success(t('mailGate.addMailboxDone', { email: d.email }))
  await load()
  emit('added')
  // 绑完直接切过去：人刚填完一个地址，想看的就是它。
  if (acct) emit('update:modelValue', acct)
}

defineExpose({ reload: load })
</script>

<style scoped>
.rail-tree {
  display: block;
}

/* ------------------------------------------------------------- 信箱那一行 */

.mbox-group + .mbox-group {
  margin-top: 2px;
}
.mbox-row {
  display: flex;
  align-items: center;
  border-radius: var(--mail-pill, 6px);
}
.mbox-row:hover {
  background: var(--el-fill-color-light);
}
.mbox-row.on {
  background: var(--el-color-primary-light-9);
}
.mbox-row.on .mbox {
  color: var(--el-color-primary);
  font-weight: 600;
}
.mbox {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
  /* 左 6px：三角自己占 12px 宽，加起来和 .folder 的 12px 缩进对得上。 */
  padding: 6px 4px 6px 6px;
  border: none;
  border-radius: var(--mail-pill, 6px);
  background: transparent;
  font-size: 13px;
  color: var(--el-text-color-primary);
  cursor: pointer;
  text-align: left;
}
.mbox:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: -2px;
}
/* 三角转 90 度表示展开。transform 而不是换字符：换字符会让那一格的宽度跟着
   字形变，一排信箱的名字左边缘就对不齐了。 */
.caret {
  flex: none;
  width: 12px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  transition: transform var(--mail-fast, 0.15s) var(--mail-ease, ease);
}
.caret.open {
  transform: rotate(90deg);
}
.mbox-dot {
  flex: none;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--el-color-success);
}
/* 这个箱最近一次收发出过错。红点比一句横幅省地方，鼠标停上去看得到地址，
   点进去才是完整的错误——多信箱之后横幅说不清是哪个箱在报错。
   红 = 授权码被拒，得重新登录；黄 = 服务器暂时连不上，会自己重试。
   和横幅的规矩一致（lib/syncBanner）：不能横幅说"不用重登"、旁边却亮着红点。 */
.mbox-dot.bad {
  background: var(--el-color-danger);
}
.mbox-dot.warn {
  background: var(--el-color-warning);
}
/* 自建文件夹那一行：主体是按钮，右边两个小动作只在悬停时露出来。 */
.folder.custom {
  display: flex;
  align-items: center;
  padding: 0;
}
.custom-main {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  padding: 5px 8px 5px 22px;
  border: 0;
  background: none;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.custom-acts {
  display: none;
  gap: 2px;
  padding-right: 6px;
}
.folder.custom:hover .custom-acts,
.folder.custom.on .custom-acts {
  display: inline-flex;
}
.custom-act {
  border: 0;
  background: none;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  font-size: 12px;
  padding: 2px 4px;
  border-radius: 4px;
}
.custom-act:hover {
  color: var(--el-color-primary);
  background: var(--el-fill-color);
}
.new-folder {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
/* 还没登录这个箱：灰点。绿点说的是"在收信"，而没登录的箱确实没在收。 */
.mbox-dot.off {
  background: var(--el-text-color-placeholder);
}
.mbox-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mbox-tag {
  flex: none;
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
/* 已解绑：比「默认」那个标签更灰，因为它说的是「这一行不能做事了」。 */
.mbox-off {
  color: var(--el-text-color-placeholder);
}
/* 数字，不是一个红点：红点只说「有东西」，而这里要回答的是「值不值得现在
   切过去」——3 封和 40 封是两个决定。min-width 让一位数和两位数的行宽一样，
   否则一排信箱的右边缘会随未读数跳动。 */
.mbox-unread {
  flex: none;
  min-width: 18px;
  padding: 0 5px;
  border-radius: 9px;
  background: var(--el-color-danger);
  color: #fff;
  font-size: 11px;
  line-height: 16px;
  text-align: center;
  font-variant-numeric: tabular-nums;
}
/* 动作区宽度固定，**每行都一样**——里面永远是两格，没有那个动作时是个
   空的 span。不固定的话，默认那一行少一个「设为默认」，尾部就短一截，
   名字那一列跟着长短不一，一排信箱看上去是歪的。 */
.mbox-actions {
  display: flex;
  flex: none;
  align-items: center;
}
/* 淡出而不是消失：opacity 0 的元素照样占位、照样可点，在触屏上就是一颗
   看不见的按钮盖在那儿。所以未激活时是浅色而不是透明，指针设备上悬停才
   变深——键盘 tab 过去也一样看得见。

   宽高写死：图标本身的字形宽度各不相同（星星比电源符号窄），按内容撑的话
   两格宽度不等，一排下来还是歪的。 */
.mbox-act {
  flex: none;
  width: 24px;
  height: 24px;
  margin: 0;
  padding: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  color: var(--el-text-color-placeholder);
}
.mbox-row:hover .mbox-act,
.mbox-act:focus-visible {
  color: var(--el-color-primary);
}
/* el-button 之间默认有左外边距，两格会被推开而空的 span 不会——那正好又
   把对齐破坏掉。 */
.mbox-actions :deep(.el-button + .el-button) {
  margin-left: 0;
}

/* --------------------------------------------------------- 底下的文件夹 */

/* 这一段从 EmailsPage 搬过来的：文件夹现在由这个组件画，而 scoped 样式
   不作用到别的组件的元素上——留在原处的话，这里的文件夹会一行样式都没有。
   变量在 styles/mailbox.css 的 :root 上，跨组件照样读得到。 */
.folder {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  height: var(--mail-row-h);
  padding: 0 12px;
  margin-bottom: 2px;
  background: none;
  border: none;
  /* The capsule, cut flat against the rail's edge — the shape says "this
     column continues off-screen" rather than "here is a floating chip". */
  border-radius: var(--mail-pill);
  font-size: var(--mail-text);
  color: var(--el-text-color-regular);
  cursor: pointer;
  text-align: left;
  transition: background var(--mail-fast) var(--mail-ease);
}
.folder .fname {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ficon {
  flex: none;
  font-size: 16px;
  color: var(--el-text-color-secondary);
}
.folder.on .ficon {
  color: inherit;
}
.folder:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: -2px;
}
/* Grey under the cursor — a plain "you are pointing at this", distinct from
   the blue capsule that means "you are here". Two different statements should
   not be made in the same colour. */
.folder:hover {
  background: var(--mail-hover);
}
.folder.on {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  font-weight: 600;
}
/* The current folder keeps its own colour when pointed at: greying it would
   read as if the selection had been lost. */
.folder.on:hover {
  background: var(--el-color-primary-light-8);
}

.mbox-folders {
  /* 一条竖线代替一层缩进的空白：三个箱展开之后，没有这条线就看不出哪几行
     属于哪个箱——缩进 16px 在 178px 宽的栏里太细微了。 */
  margin: 2px 0 4px 13px;
  padding-left: 5px;
  border-left: 1px solid var(--el-border-color-lighter);
}
.folder.sub {
  height: 28px;
  font-size: 12.5px;
  padding: 0 8px;
}
.folder.sub .ficon {
  font-size: 14px;
}
.folder.locked {
  color: var(--el-text-color-secondary);
}
.cnt {
  flex: none;
  font-size: 11px;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
}
.folder.on .cnt {
  color: inherit;
}

.mbox-add {
  margin-top: 6px;
  margin-left: 12px;
  font-size: 12px;
}
/* 一条线代替那行标题：把「它不属于上面任何一个信箱」这件事说清楚，
   而不占一整行的高度。 */
.shared {
  margin-top: 10px;
  padding-top: 8px;
  border-top: 1px solid var(--el-border-color-lighter);
}
</style>
