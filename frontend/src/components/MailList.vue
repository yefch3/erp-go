<template>
  <!-- A list, not a table. The columns of a table promise that each cell is a
       separate fact worth comparing down the page; a mail row is one sentence
       — who, about what, when — and reading it as a sentence is what lets the
       eye take forty of them in one pass. -->
  <div class="mail-list-wrap">
    <!-- 排序栏。不是表头——上面说了这不是表格——只是一行「按什么排」的开关，
         照着 263 网页邮箱表头上那几个箭头做的：点一列按它排，再点一下反过来。
         没给 sortFields 的地方（搜索结果）不出现，因为服务端在那儿不接排序。 -->
    <div
      v-if="sortFields && sortFields.length"
      class="sort-bar"
      role="toolbar"
      :aria-label="t('emails.sortBar.label')"
    >
      <span class="sort-label">{{ t('emails.sortBar.label') }}</span>
      <button
        v-for="f in sortFields"
        :key="f"
        type="button"
        class="sort-key"
        :class="{ on: sort?.by === f }"
        :aria-pressed="sort?.by === f"
        :aria-label="sortAria(f)"
        :title="t('emails.sortBar.hint')"
        @click="emit('sort', f)"
      >
        {{ t(`emails.sortBar.${f}`) }}<span v-if="sort?.by === f" class="dir" aria-hidden="true">{{ sort?.dir === 'asc' ? '↑' : '↓' }}</span>
      </button>
    </div>
  <ul v-loading="loading" class="mail-list" role="list">
    <!-- 拖一行到左栏的文件夹上就是「挪进去」。
         整行可拖，不是只有某个把手：一列邮件里每一行本身就是那封信，拖它
         是最直白的说法。勾选框和星标各自 @click.stop，所以拖动不会误触。 -->
    <li
      v-for="m in mails"
      :key="m.id"
      class="row"
      :class="{ unread: !m.isRead, picked: isPicked(m), dragging: isDragging(m) }"
      :draggable="canDrag"
      @dragstart="onDragStart(m, $event)"
      @dragend="onDragEnd"
    >
      <!-- 头像和勾选框**共用一个位置**，照 Gmail。
           这一列只有 280–400px 宽，左边每多一个控件，主题就少一截；而头像和
           勾选框恰好可以合并：平时是头像（一列信里最快认出「谁」的东西），
           鼠标压上来或者已经勾上时变成勾选框。多选是低频动作，不值得为它
           长期占一格。

           投递记录不给勾，理由和它没有星标是同一个：邮件服务器上没有这封信
           的正本，标记无处可写。它只显示头像。 -->
      <span class="face">
        <el-checkbox
          v-if="!isRecordOnly(m)"
          class="pick"
          :model-value="isPicked(m)"
          :aria-label="t('emails.selectOne')"
          @click.stop
          @change="togglePick(m)"
        />
        <span class="avatar" :style="{ background: avatarColor(m) }" aria-hidden="true">
          {{ initial(m) }}
        </span>
      </span>

      <!-- el-tooltip rather than a title attribute. The browser's own tooltip
           takes about a second to appear, which is far too slow for a row of
           unlabelled icons: by the time it arrives the cursor has usually
           moved on, so the icons read as unexplained. show-after 0 puts the
           name under the cursor the moment it lands, which is what Gmail
           does and the only reason its icon-only toolbar is usable. -->
      <!-- No star on a delivery record: there is no message on the host to
           write the flag to. Same reason it gets no checkbox. -->
      <el-tooltip
        v-if="folder !== 'junk' && !isRecordOnly(m)"
        :content="t(m.isStarred ? 'emails.unstar' : 'emails.star')"
        placement="top"
        :show-after="0"
        :hide-after="0"
      >
        <button
          type="button"
          class="star"
          :class="{ on: m.isStarred }"
          :aria-label="t(m.isStarred ? 'emails.unstar' : 'emails.star')"
          :aria-pressed="m.isStarred"
          @click.stop="emit('star', m)"
        >{{ m.isStarred ? '★' : '☆' }}</button>
      </el-tooltip>

      <span v-else-if="folder !== 'junk'" class="star-gap" aria-hidden="true" />

      <!-- The row's own hit area. A button rather than a link because opening
           a mail is a state change in this app, not a document to fetch; the
           action buttons then sit outside it, which a link could not do
           without nesting interactive elements inside itself. -->
      <!-- 三行，照 Foxmail：发件人+日期 / 主题 / 摘要。
           从前是一行 —— 发件人 | 主题 — 摘要 | 日期 —— 那是给一条**整页宽**
           的列表设计的。改成三栏之后列表列只有 280–400px，一行里塞不下四样
           东西，于是它们直接叠在一起（发件人压着主题、主题压着日期），因为
           .who 是定宽不收缩的而 .body 没有 overflow:hidden 兜住溢出。

           窄列里横着排不下的东西，竖着排得下：每一行自己占满整列的宽度，
           长了就省略号，不会挤到别人身上。这也正是所有窄列表（Foxmail、
           Outlook 的紧凑列、手机上的邮件 app）都是多行的原因。 -->
      <div
        class="body"
        role="button"
        tabindex="0"
        :aria-label="ariaFor(m)"
        @click="emit('open', m)"
        @keydown.enter.prevent="emit('open', m)"
        @keydown.space.prevent="emit('open', m)"
      >
        <!-- 第一行：谁，以及什么时候。一列邮件先被扫的就是这两样。 -->
        <span class="l1">
          <span class="who">
            <!-- A sent mail is about who it went to; a received one about who
                 it came from. Same column, different question. -->
            {{ folder === 'sent' ? sentWho(m) : (m.fromName || m.fromEmail) }}
          </span>
          <!-- One row per conversation; this is how many messages it holds. -->
          <span v-if="Number(m.threadCount) > 1" class="tcount">{{ m.threadCount }}</span>
          <!-- 对方是否已读，只在已发送里出现。三态和详情页同一套，措辞也
               同一套——「可能已打开」而不是「已读」：像素被加载只是参考，
               客户回信才是确凿的已读，列表上把它说成事实等于替系统编造一个
               关于客户的事实。第三态（没带追踪）压最淡但不省略：省略了它，
               「未打开」的缺席就有两种读法。

               **不挂 tooltip**：措辞本身已经把话说完了（「可能已打开」这四个
               字就是那句提示的意思），再弹一块解释只是让人扫一列已发送时
               一路蹦出气泡。 -->
          <span v-if="folder === 'sent'" class="readmark" :class="readMark(m).cls">
            {{ readMark(m).label }}
          </span>
          <!-- 按大小排的时候把大小摆出来——否则排了也看不出排了什么。 -->
          <span v-if="showSize" class="size">{{ humanSize(m.rawSize) }}</span>
          <time class="when" :datetime="m.receivedAt" :title="zonedStamp(m.receivedAt)">{{ listTime(m.receivedAt) }}</time>
        </span>

        <!-- 第二行：主题，以及它属于哪儿。标签跟着主题走而不是跟着发件人，
             照 Gmail：标签回答的是「这封信被归到哪里」，和主题是一句话。 -->
        <span class="l2">
          <!-- title 带完整地址：窄列里这个标签会被压成「fangch…」，认得出是
               另一个箱但认不出是哪个，鼠标停一下就知道了。 -->
          <span v-if="otherMailbox(m)" class="in-mailbox" :title="otherMailbox(m)">{{ otherMailbox(m) }}</span>
          <span v-if="m.matchFolder" class="in-folder">{{ folderLabel(m.matchFolder) }}</span>
          <!-- Split into text runs and rendered as elements rather than
               through v-html: the matched string is whatever somebody typed
               into a search box, and the snippet is text from a stranger's
               mail. Neither may become markup. -->
          <span class="subj">
            <template v-for="(part, i) in split(m.subject || t('emails.noSubject'))" :key="i">
              <mark v-if="part.hit">{{ part.text }}</mark>
              <template v-else>{{ part.text }}</template>
            </template>
          </span>
          <el-tooltip
            v-if="m.hasAttachments"
            :content="t('emails.attachments')"
            placement="top"
            :show-after="0"
            :hide-after="0"
          >
            <el-icon class="clip"><Paperclip /></el-icon>
          </el-tooltip>
        </span>

        <!-- 第三行：摘要。没有摘要就整行不出现，行高跟着缩——空着一行会让
             这一封看起来比邻居"轻"，而它并不是。 -->
        <span v-if="m.snippet" class="snip">
          <template v-for="(part, i) in split(m.snippet)" :key="i">
            <mark v-if="part.hit">{{ part.text }}</mark>
            <template v-else>{{ part.text }}</template>
          </template>
        </span>
      </div>

      <!-- 这里从前有一组悬停才出现的行内按钮（归档/删除/标为已读）。
           **删掉了**，两个理由：

           一、它们浮在行的右边，正好压住第一行的日期——三行式之前它们和
           日期共用一格（悬停时替换），三行之后日期在第一行而按钮竖向居中，
           两者重叠。
           二、同样的动作在右边阅读区的工具条上都有，而且那里是在**看着这封
           信**的时候做决定，比在列表上凭一行摘要决定要靠谱。Foxmail 的列表
           里也没有这一组。

           要批量处理仍然走勾选框 + 上方那条批量工具条。 -->
    </li>
  </ul>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listTime, zonedStamp } from '../lib/zonedtime'
import { humanSize } from '../lib/humanSize'
import { draggedRows } from '../lib/dragMails'
import type { MailSort, SortField } from '../lib/mailSort'
import { Paperclip } from '@element-plus/icons-vue'

export interface MailRow {
  id: string
  fromEmail: string
  fromName: string
  subject: string
  snippet: string
  receivedAt: string
  isRead: boolean
  isStarred: boolean
  hasAttachments: boolean
  threadCount?: number | string
  // Sent folder only: 'HOST' is a real message, 'ERP' a delivery record whose
  // copy the host never kept.
  kind?: string
  toName?: string
  toEmail?: string
  // 已发送：整段收件人（多个人时和 toEmail 不同）。
  toAll?: string
  // Search results only. The search crossed folders, so a result that does not
  // say where it was found leaves the person to open it to find out.
  matchFolder?: string
  // 同上，再往上一层：搜索也横跨信箱，所以命中还要说自己是哪个箱的。
  // 不说的话，一列里混着 263 和 Gmail 的信而没有任何区分——「哪个箱」正是
  // 搜索之前不知道、搜索之后最想知道的那件事。
  matchAccount?: number
  // Sent folder only: when the open-tracking pixel was fetched, and whether
  // this mail carried one at all. Both needed — an empty openedAt alone cannot
  // tell "nobody opened it" from "nobody was watching".
  openedAt?: string
  tracked?: boolean
  // 原件的字节数。只在按大小排的时候显示；服务器没留原件的投递记录是 0。
  rawSize?: number | string
}

const props = defineProps<{
  mails: MailRow[]
  folder: string
  loading?: boolean
  // What was searched for, so the rows can point at the words that matched.
  // Empty when this is a folder listing rather than a result set.
  highlight?: string
  // The ids currently ticked. Owned by the page, not by this list: the page is
  // what acts on a selection, and it also has to survive this component being
  // re-rendered by a reload.
  selected?: string[]
  // 信箱号 → 地址，给搜索结果上那个信箱标签用。空表就不显示标签——
  // 清单还没加载完的那一瞬间，显示一个光秃秃的号码比什么都不显示更糟。
  accounts?: Record<number, string>
  // 此刻站在哪个箱。**只有别的箱的命中才挂标签**：站在 263 里搜，每一行都
  // 标着"263"是一列一模一样的噪声，而那几行 Gmail 的正因此淹在里面。
  currentAccount?: number
  // 现在按哪一列排，以及这份列表允许按哪几列排。两个都不给就没有排序栏。
  sort?: MailSort
  sortFields?: SortField[]
}>()

// mark / purge 两个事件随行内按钮一起去掉了：现在列表上没有任何单封操作，
// 那些动作都在右边阅读区的工具条上。
const emit = defineEmits<{
  open: [MailRow]
  star: [MailRow]
  sort: [SortField]
  'update:selected': [string[]]
  // 拖起来了：带上拖的是哪几封、它们分别属于哪个信箱。页面拿着这两样去
  // 决定左栏哪些文件夹可以接（别的箱的文件夹接不了）。
  dragmails: [{ ids: string[]; accounts: number[] }]
  dragend: []
}>()

const { t } = useI18n()

const showSize = computed(() => props.sort?.by === 'size')

// 读屏器听到的是「大小，降序」，而不是一个箭头。
function sortAria(f: SortField): string {
  const name = t(`emails.sortBar.${f}`)
  if (props.sort?.by !== f) return name
  return `${name}，${t(props.sort.dir === 'asc' ? 'emails.sortBar.asc' : 'emails.sortBar.desc')}`
}

function isRecordOnly(m: MailRow) {
  return m.kind === 'ERP'
}

// 已发送那一列写给谁。发给一个人时是名字（或地址）；发给好几个人时是整段
// ——列表说「一个人」而详情列三个，同一封信两个说法，比长一点更坏。
function sentWho(m: MailRow): string {
  if (m.toAll && m.toAll.toLowerCase() !== (m.toEmail || '').toLowerCase()) return m.toAll
  return m.toName || m.toEmail || ''
}

// 对方是否已读的三态。判断顺序即优先级：真加载过 > 带着像素但没动静 > 根本没在看。
function readMark(m: MailRow): { cls: string; label: string } {
  if (m.openedAt) {
    return {
      cls: 'opened',
      label: t('emails.maybeOpened'),
    }
  }
  if (m.tracked) {
    return { cls: 'watched', label: t('emails.noOpenYet') }
  }
  return { cls: 'off', label: t('reader.noTracking') }
}

function isPicked(m: MailRow) {
  return (props.selected ?? []).includes(m.id)
}

function togglePick(m: MailRow) {
  const cur = props.selected ?? []
  emit('update:selected', isPicked(m) ? cur.filter((id) => id !== m.id) : [...cur, m.id])
}

// The three or four things worth doing to a mail without opening it, chosen
// per folder: "archive" means nothing in the archive, and offering "delete" in
// the trash would be a lie about what the button does.
// Cuts a string into alternating plain and matched runs, so the template can
// render the matched ones as <mark> elements instead of interpolating markup.
//
// Case-insensitive, and it searches for the string itself rather than building
// a RegExp from it — a query containing "(" or "*" would otherwise either
// throw or quietly mean something else. This is the same matching the server
// did with ILIKE, so what is underlined is what was actually found.
function split(text: string): { text: string; hit: boolean }[] {
  const needle = (props.highlight ?? '').trim()
  if (!needle || !text) return [{ text, hit: false }]
  const hay = text.toLowerCase()
  const find = needle.toLowerCase()
  const out: { text: string; hit: boolean }[] = []
  let from = 0
  for (;;) {
    const at = hay.indexOf(find, from)
    if (at < 0) break
    if (at > from) out.push({ text: text.slice(from, at), hit: false })
    out.push({ text: text.slice(at, at + find.length), hit: true })
    from = at + find.length
  }
  if (from < text.length) out.push({ text: text.slice(from), hit: false })
  return out
}

// Where a search result was found. Named in the person's language rather than
// shown as the stored folder key.
function folderLabel(folder: string) {
  switch (folder) {
    case 'SENT':
      return t('emails.folders.sent')
    case 'JUNK':
      return t('emails.folders.junk')
    default:
      return t('emails.folders.inbox')
  }
}

// 这封命中来自**别的**信箱时，回它的地址；否则空串（不挂标签）。
//
// 只标别的箱，不标当前这个：站在 263 里搜，每一行都标着 263 是一列一模一样
// 的噪声，而那几行 Gmail 的正因此淹在里面。人要看见的是"这封不在我以为的
// 那个箱里"。
//
// 认不出的号码也不标：清单还没加载完的那一瞬间，一个光秃秃的数字比什么都
// 不显示更糟。
function otherMailbox(m: MailRow): string {
  const id = m.matchAccount ?? 0
  if (!id || id === props.currentAccount) return ''
  return props.accounts?.[id] ?? ''
}

// ------------------------------------------------------------------ 拖拽

// 正在被拖的那几封，用来把它们画淡一点。空数组 = 没在拖。
const dragged = ref<string[]>([])
const isDragging = (m: MailRow) => dragged.value.includes(m.id)

// 投递记录拖不动：邮箱服务器上没有这封信的正本，挪无处可挪。整份列表里
// 只要有一封能拖就开着 draggable，具体那一行能不能拖在 dragstart 里挡。
const canDrag = computed(() => props.folder !== 'scheduled' && props.folder !== 'drafts')

function onDragStart(m: MailRow, ev: DragEvent) {
  // 拖整批还是拖一行，规则和它为什么是这样见 lib/dragMails。
  const rows = draggedRows(m, props.mails, props.selected ?? [], (r) => !isRecordOnly(r))
  if (!rows.length) {
    ev.preventDefault()
    return
  }
  const ids = rows.map((r) => r.id)
  const accounts = [...new Set(rows.map((r) => r.matchAccount || props.currentAccount || 0))]
  dragged.value = ids
  emit('dragmails', { ids, accounts })
  if (!ev.dataTransfer) return
  ev.dataTransfer.effectAllowed = 'move'
  // 必须写点什么进去，否则 Firefox 根本不认这是一次拖拽。内容本身不用：
  // 放下时读的是页面自己记着的那份（dragover 里读不到 dataTransfer）。
  ev.dataTransfer.setData('text/plain', ids.join(','))
  // 拖影换成一张小卡片，照 Foxmail。
  //
  // 默认拖影是**整行的半透明快照**——三行式之后那是 340×86 的一大块，跟着
  // 光标走的时候把左栏那几格文件夹遮掉大半，而那正是此刻要看清的地方。
  // 一个信封加个数字够了：手上拿着几封信，这是唯一需要知道的事。
  const chip = document.createElement('div')
  chip.className = 'drag-chip'
  chip.textContent = ids.length > 1 ? `✉ ${ids.length}` : '✉'
  document.body.appendChild(chip)
  // 偏移一点，让卡片在光标的右下方而不是压在光标底下——压着的话看不出
  // 光标尖正指着哪一格。
  ev.dataTransfer.setDragImage(chip, -8, -4)
  // 下一帧再删：setDragImage 是同步截图的，这一帧之内不能从文档里拿走。
  requestAnimationFrame(() => chip.remove())
}

function onDragEnd() {
  dragged.value = []
  emit('dragend')
}

// 头像上那个字。
//
// 取显示名的第一个字，没有名字就取地址的第一个字母。中文取一个字就够认
// （"腾"、"李"），英文取首字母并大写。
//
// 取不到就回一个圆点而不是空白：一个空的彩色圆圈看着像没加载完。
function initial(m: MailRow): string {
  const src = (props.folder === 'sent' ? sentWho(m) : (m.fromName || m.fromEmail)) || ''
  const ch = [...src.trim()].find((c) => /[\p{L}\p{N}]/u.test(c))
  return ch ? ch.toUpperCase() : '·'
}

// 头像底色。同一个地址永远同一个颜色——颜色在这里的用处正是「这一列里又是
// 他」，随机就没有意义了。
//
// 用 oklch 而不是 hsl：hsl 里同一个亮度值在黄色和蓝色上看起来差一大截，
// 一排头像会有几个亮得刺眼、几个暗得发糊。oklch 的亮度是感知亮度，固定
// 62% 就是每一个都一样深，白字压在上面都读得清。
function avatarColor(m: MailRow): string {
  const key = (props.folder === 'sent' ? (m.toEmail || '') : (m.fromEmail || '')).toLowerCase()
  let h = 0
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) % 360
  return `oklch(62% 0.13 ${h})`
}

// Screen readers get the sentence the row is: who, about what, and whether it
// still wants attention. Without it the row announces as an unlabelled button.
function ariaFor(m: MailRow) {
  const state = m.isRead ? '' : t('emails.unreadOne') + ', '
  return `${state}${m.fromName || m.fromEmail}: ${m.subject || t('emails.noSubject')}`
}
</script>

<style scoped>
.mail-list {
  margin: 0;
  padding: 0;
  list-style: none;
  border-top: 1px solid var(--mail-divider);
}

.row {
  display: flex;
  /* 顶部对齐，不是居中：勾选框和星标属于第一行（发件人那一行），
     居中的话它们会飘到主题旁边，看着像在标记主题。 */
  align-items: flex-start;
  gap: 6px;
  /* 高度由内容定。三行是常态，没有摘要的（投递记录、空正文）自然是两行，
     不必为它留一条空行——空着一行会让那一封看起来比邻居"轻"。
     三行约 76px，和 Foxmail 一档：再紧就分不出三行，再松一屏就少两封。 */
  padding: 7px 14px;
  border-bottom: 1px solid var(--mail-divider);
  background: var(--mail-surface);
  /* 箭头，不是小手。
     小手是「这是个链接/按钮」的意思，而一列邮件不是一排按钮——每一行都变成
     小手，整片列表看着像在催人点。Foxmail、Outlook、Apple Mail 的邮件列表
     都是箭头；真正的控件（星标、勾选框）才是小手，那时它还起到瞄准的作用。 */
  cursor: default;
  position: relative;
  transition: box-shadow var(--mail-fast) var(--mail-ease),
    background var(--mail-fast) var(--mail-ease);
}
.row:hover {
  /* Picked up off the ground: white and lifted together. The shadow alone was
     enough while the rows were white and the page around them was too — there
     was a colour to cast onto. Now the row is the same grey as everything
     under it, and a shadow on a field its own colour barely registers, so the
     lift gets the white it is lifting to. Still not a tint: white is the
     absence of the ground, which leaves colour free to mean the one thing it
     means in this list — a ticked row. */
  background: var(--mail-row-hover);
  box-shadow: var(--mail-hover-shadow);
  z-index: 1;
  border-bottom-color: transparent;
}
.row:focus-within {
  background: var(--mail-row-hover);
  box-shadow: var(--mail-hover-shadow);
  z-index: 1;
}
/* A ticked row is tinted, because the selection has to stay legible after the
   cursor has moved on to the toolbar — which is exactly when it matters. */
.row.picked {
  background: var(--el-color-primary-light-9);
}
/* 正在被拖走的那几行画淡：手上拿着的东西和还留在原地的东西要分得开，
   否则拖多封时看不出到底拿起了哪几封。 */
.row.dragging {
  opacity: 0.45;
}

/* 头像和勾选框叠在同一格里，靠 grid 把两者放进同一个单元格——用绝对定位
   的话这一格就撑不开，左边距离得另外写死一次。
   32px 而不是 Foxmail 的 36：那一列最窄只有 280px，左边每省 4px 主题就多
   4px。对齐第一行，不是整行居中：三行高的行里居中会让它飘到主题旁边。 */
.face {
  flex: none;
  display: grid;
  width: 32px;
  height: 32px;
  margin-right: 2px;
}
.face > * {
  grid-area: 1 / 1;
  place-self: center;
}
.avatar {
  display: grid;
  place-items: center;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  color: #fff;
  font-size: 14px;
  font-weight: 600;
  line-height: 1;
  user-select: none;
}
/* 平时看头像，压上来或已经勾上时看勾选框。两者永远只有一个可见，所以这
   一格的宽度不会变，行也不会在光标下重排。 */
.pick {
  display: none;
}
.row:hover .pick,
.row:focus-within .pick,
.row.picked .pick {
  display: block;
}
.row:hover .avatar,
.row:focus-within .avatar,
.row.picked .avatar {
  visibility: hidden;
}
/* Element Plus reserves room for a label this checkbox does not have. */
.pick :deep(.el-checkbox__label) {
  display: none;
}

.star {
  flex: none;
  display: grid;
  place-items: center;
  width: 24px;
  /* 同 .pick：跟第一行走。 */
  height: 21px;
  padding: 0;
  background: none;
  border: none;
  font-size: 16px;
  line-height: 1;
  color: var(--el-text-color-placeholder);
  cursor: pointer;
  transition: color var(--mail-fast) var(--mail-ease);
}
.star:hover {
  color: var(--el-color-warning);
}
.star.on {
  color: var(--el-color-warning);
}

.star-gap {
  flex: none;
  width: 24px;
}

/* 三行竖着排。每一行自己占满整列的宽度，长了就省略号——这正是横排做不到
   的：横排里定宽的发件人不肯收缩，一挤就溢出来盖在邻居身上。 */
.body {
  display: flex;
  flex-direction: column;
  gap: 1px;
  flex: 1;
  min-width: 0;
  border: none;
  background: none;
  text-align: start;
  font: inherit;
  color: inherit;
  /* 同 .row：整行是内容，不是按钮。 */
  cursor: default;
}
.body:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: -2px;
  border-radius: 4px;
}

/* 每一行内部都是「一个会收缩的主角 + 几个配角」。min-width: 0 是主角能收缩
   的前提：flex 子项默认不会缩到内容宽度以下，少了这一条，长地址就会把日期
   顶出列外——那正是改版前那一版的毛病。

   overflow: hidden 是兜底，不是靠它排版。上一版之所以能叠成一团，是因为
   溢出的东西可以画到框外面去；这里封住那条路，将来再有什么想不到的内容
   （超长的标签、别的语言）也只会被切掉，不会盖住邻居。 */
.l1,
.l2 {
  display: flex;
  align-items: baseline;
  gap: 6px;
  min-width: 0;
  overflow: hidden;
}
.snip {
  min-width: 0;
}

.who {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--mail-text);
  line-height: 1.45;
  color: var(--el-text-color-regular);
}
.unread .who {
  font-weight: 700;
  color: var(--el-text-color-primary);
}

.tcount {
  flex: none;
  font-size: var(--mail-meta);
  font-weight: 400;
  color: var(--el-text-color-secondary);
}

/* Subject and snippet on one line, the snippet giving up its space first:
   what the mail is about survives truncation, the preview of it does not. */
/* 主题是第二行的主角。min-width 是一条底线：标签和主题同在一行，而标签
   （信箱地址）可以很长——不设底线的话，窄列里标签会把主题挤成 0 宽，
   于是这一行只剩两个标签，看不出这封信是关于什么的。宁可标签被切短。 */
.subj {
  flex: 1 1 auto;
  min-width: 5em;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--mail-text);
  line-height: 1.45;
  color: var(--el-text-color-regular);
}
.unread .subj {
  font-weight: 700;
  color: var(--el-text-color-primary);
}
/* 摘要压一号、压一档灰：它是第三顺位的信息，和主题同样重就等于没有层次。
   单行省略——两行会让每一行的高度取决于摘要长度，一列邮件的节奏就乱了。 */
.snip {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--mail-sub);
  line-height: 1.45;
  color: var(--el-text-color-secondary);
}

.clip {
  flex: none;
  color: var(--el-text-color-secondary);
  font-size: 14px;
}
/* 带着大小的时候让它宽一点，但别把主题那一列挤没了。 */
.size {
  flex: none;
  font-size: var(--mail-meta);
  color: var(--el-text-color-secondary);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}
.unread .size {
  color: var(--el-text-color-primary);
}

.sort-bar {
  display: flex;
  align-items: center;
  gap: 2px;
  padding: var(--mail-row-pad);
  padding-top: 4px;
  padding-bottom: 4px;
  font-size: var(--mail-meta);
  color: var(--el-text-color-secondary);
}
.sort-label {
  margin-right: 6px;
}
.sort-key {
  border: 0;
  background: transparent;
  padding: 2px 9px;
  border-radius: 999px;
  font: inherit;
  color: inherit;
  cursor: pointer;
  transition: background var(--mail-fast) var(--mail-ease), color var(--mail-fast) var(--mail-ease);
}
.sort-key:hover {
  background: var(--el-fill-color);
  color: var(--el-text-color-primary);
}
.sort-key:focus-visible {
  outline: 2px solid var(--el-color-primary-light-5);
  outline-offset: 1px;
}
.sort-key.on {
  color: var(--el-color-primary);
  font-weight: 600;
  background: var(--el-color-primary-light-9);
}
.dir {
  margin-left: 2px;
}
.when {
  flex: none;
  font-size: var(--mail-meta);
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}
.unread .when {
  font-weight: 700;
  color: var(--el-text-color-primary);
}

/* Hidden until the row is under the cursor or holds focus — the list is for
   reading, and three icons on every line would make it a control panel.
   浮在右边，不占布局宽度：三行之后日期在第一行，按钮再挤进那一行就要和它
   抢那 46px，而这一列总共也就 300 出头。自带底色，好让下面的文字不透上来。 */
/* 这里从前有三条 @container：宽度不够时先让发件人变窄、再藏摘要、再整个
   藏掉发件人。**它们全部删掉了**，因为三行式布局不需要它们，而它们上一版
   造成的正是那次文字叠在一起的事故：

   容器建在 .pane 上，而 .pane 从三栏改版之后同时装着列表列**和**阅读区，
   宽度一千多；真正的列表列只有 280–400px。于是「窄了就收」的规则一条都没
   触发，定宽 184px 的发件人不肯收缩、摘要也还在，一行四样东西塞进 340px，
   直接溢出到邻居身上——发件人压着主题、主题压着日期。查了半天像是渲染
   出错，其实是一条**永远为假**的媒体查询。

   竖着排就没有这个问题：每一行是一个独立的收缩上下文，窄到什么程度都只是
   省略号更早出现。所以这几条规则连同它们要修的问题一起消失，而不是把断点
   改小——一条要靠正确的容器才成立的规则，是下一次同样事故的种子。 */


/* The matched words. A background wash rather than a colour change, so a hit
   is visible in a row that is already bold for being unread. */
.snip mark,
.subj mark {
  background: var(--el-color-warning-light-7);
  color: inherit;
  padding: 0 1px;
  border-radius: 2px;
}
/* Where this result was found. Quiet — it is context for the row, not the
   point of it, and every row in a result set carries one. */
/* 两个标签都**可以被压缩**：它们和主题同在第二行，而主题是主角。
   flex-shrink 留着（不是 flex: none），压到 3em 以下就没意义了，所以给个
   底，再往下由 .subj 的 min-width 决定谁先让位。 */
.in-folder,
.in-mailbox {
  flex: 0 1 auto;
  min-width: 3em;
  align-self: center;
  font-size: 11px;
  line-height: 1.6;
  padding: 0 6px;
  border-radius: 9px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 信箱标签比文件夹标签重：它说的是「这封不在你以为的那个箱里」，是一列
   搜索结果里最容易看漏、看漏了最费解的一件事。
   收得比从前更窄（8em），而且**允许再收缩**：它和主题同在第二行，一个不肯
   收缩的长地址会把主题挤成两三个字。地址是配角，主题是主角。 */
.in-mailbox {
  max-width: 8em;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
}

/* 已读三态：绿的一眼能扫到（这一列存在的目的），灰的居次，没追踪的压到
   最淡——它说的是「这封信没在看」，不该和「没人打开」争视线。 */
.readmark {
  flex: none;
  font-size: 12px;
  white-space: nowrap;
}
.readmark.opened {
  color: var(--el-color-success);
}
.readmark.watched {
  color: var(--el-text-color-secondary);
}
.readmark.off {
  color: var(--el-text-color-placeholder);
}
</style>
