<template>
  <!-- A list, not a table. The columns of a table promise that each cell is a
       separate fact worth comparing down the page; a mail row is one sentence
       — who, about what, when — and reading it as a sentence is what lets the
       eye take forty of them in one pass. -->
  <ul v-loading="loading" class="mail-list" role="list">
    <li
      v-for="m in mails"
      :key="m.id"
      class="row"
      :class="{ unread: !m.isRead }"
    >
      <!-- el-tooltip rather than a title attribute. The browser's own tooltip
           takes about a second to appear, which is far too slow for a row of
           unlabelled icons: by the time it arrives the cursor has usually
           moved on, so the icons read as unexplained. show-after 0 puts the
           name under the cursor the moment it lands, which is what Gmail
           does and the only reason its icon-only toolbar is usable. -->
      <el-tooltip
        v-if="folder !== 'junk'"
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

      <!-- The row's own hit area. A button rather than a link because opening
           a mail is a state change in this app, not a document to fetch; the
           action buttons then sit outside it, which a link could not do
           without nesting interactive elements inside itself. -->
      <div
        class="body"
        role="button"
        tabindex="0"
        :aria-label="ariaFor(m)"
        @click="emit('open', m)"
        @keydown.enter.prevent="emit('open', m)"
        @keydown.space.prevent="emit('open', m)"
      >
        <span class="who">
          {{ m.fromName || m.fromEmail }}
          <!-- One row per conversation; this is how many messages it holds. -->
          <span v-if="Number(m.threadCount) > 1" class="tcount">{{ m.threadCount }}</span>
        </span>
        <span class="line">
          <span class="subj">{{ m.subject || t('emails.noSubject') }}</span>
          <span v-if="m.snippet" class="snip">— {{ m.snippet }}</span>
        </span>
      </div>

      <el-tooltip
        v-if="m.hasAttachments"
        :content="t('emails.attachments')"
        placement="top"
        :show-after="0"
        :hide-after="0"
      >
        <el-icon class="clip"><Paperclip /></el-icon>
      </el-tooltip>

      <!-- Time and actions share one cell: the actions appear where the date
           was, so the row does not reflow under the cursor and the next row
           down stays where the eye left it. -->
      <div class="tail">
        <time class="when" :datetime="m.receivedAt">{{ shortTime(m.receivedAt) }}</time>
        <div class="acts">
          <el-tooltip
            v-for="a in actionsFor(m)"
            :key="a.key"
            :content="a.label"
            placement="top"
            :show-after="0"
            :hide-after="0"
          >
            <button
              type="button"
              class="act"
              :aria-label="a.label"
              @click.stop="a.run(m)"
            >
              <el-icon><component :is="a.icon" /></el-icon>
            </button>
          </el-tooltip>
        </div>
      </div>
    </li>
  </ul>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import {
  Box,
  CircleCheck,
  Delete,
  DeleteFilled,
  FolderOpened,
  Message,
  Paperclip,
  RefreshLeft,
} from '@element-plus/icons-vue'

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
}

const props = defineProps<{
  mails: MailRow[]
  folder: string
  loading?: boolean
}>()

const emit = defineEmits<{
  open: [MailRow]
  star: [MailRow]
  mark: [MailRow, Record<string, boolean>]
  purge: [MailRow]
}>()

const { t } = useI18n()

// The three or four things worth doing to a mail without opening it, chosen
// per folder: "archive" means nothing in the archive, and offering "delete" in
// the trash would be a lie about what the button does.
function actionsFor(m: MailRow) {
  const mark = (flags: Record<string, boolean>) => () => emit('mark', m, flags)
  const readToggle = {
    key: 'read',
    icon: Message,
    label: t(m.isRead ? 'emails.markUnread' : 'emails.markRead'),
    run: mark({ read: !m.isRead }),
  }
  switch (props.folder) {
    case 'trash':
      return [
        { key: 'restore', icon: RefreshLeft, label: t('emails.restore'), run: mark({ deleted: false }) },
        { key: 'purge', icon: DeleteFilled, label: t('emails.purge'), run: () => emit('purge', m) },
      ]
    case 'junk':
      return [
        { key: 'notjunk', icon: CircleCheck, label: t('emails.notJunk'), run: mark({ notJunk: true }) },
        { key: 'trash', icon: Delete, label: t('emails.toTrash'), run: mark({ deleted: true }) },
      ]
    case 'archive':
      return [
        { key: 'unarchive', icon: FolderOpened, label: t('emails.unarchive'), run: mark({ archived: false }) },
        { key: 'trash', icon: Delete, label: t('emails.toTrash'), run: mark({ deleted: true }) },
        readToggle,
      ]
    default:
      return [
        { key: 'archive', icon: Box, label: t('emails.archive'), run: mark({ archived: true }) },
        { key: 'trash', icon: Delete, label: t('emails.toTrash'), run: mark({ deleted: true }) },
        readToggle,
      ]
  }
}

// Screen readers get the sentence the row is: who, about what, and whether it
// still wants attention. Without it the row announces as an unlabelled button.
function ariaFor(m: MailRow) {
  const state = m.isRead ? '' : t('emails.unreadOne') + ', '
  return `${state}${m.fromName || m.fromEmail}: ${m.subject || t('emails.noSubject')}`
}

function shortTime(v: string) {
  if (!v) return ''
  const at = new Date(v)
  if (Number.isNaN(at.getTime())) return v.slice(0, 16).replace('T', ' ')
  // Gmail's rule, and it is the right one: this year needs a day, older mail
  // needs a year, and today only needs the hour.
  const now = new Date()
  const sameDay =
    at.getFullYear() === now.getFullYear() &&
    at.getMonth() === now.getMonth() &&
    at.getDate() === now.getDate()
  if (sameDay) {
    return new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }).format(at)
  }
  if (at.getFullYear() === now.getFullYear()) {
    return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' }).format(at)
  }
  return new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'short', day: 'numeric' }).format(at)
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
  align-items: center;
  gap: 6px;
  height: var(--mail-row-h);
  padding: var(--mail-row-pad);
  border-bottom: 1px solid var(--mail-divider);
  background: var(--mail-surface);
  cursor: pointer;
  position: relative;
  transition: box-shadow var(--mail-fast) var(--mail-ease);
}
.row:hover {
  /* Lifted, and only lifted. With every row white the hover could now be a
     tint instead, but the shadow says "this one is under the cursor" without
     spending a colour — and colour is what the unread state is saving. */
  box-shadow: var(--mail-hover-shadow);
  z-index: 1;
  border-bottom-color: transparent;
}
.row:focus-within {
  box-shadow: var(--mail-hover-shadow);
  z-index: 1;
}

.star {
  flex: none;
  display: grid;
  place-items: center;
  width: 24px;
  height: 100%;
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

.body {
  display: flex;
  /* Centred, not baseline-aligned. On a baseline the text sits against the
     top of a fixed-height row and every line in the list reads as if it had
     slipped upwards — which is exactly how it looked. */
  align-items: center;
  gap: 12px;
  flex: 1;
  min-width: 0;
  height: 100%;
  border: none;
  background: none;
  text-align: start;
  font: inherit;
  color: inherit;
  cursor: pointer;
}
.body:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: -2px;
  border-radius: 4px;
}

.who {
  flex: none;
  width: var(--mail-who-w);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  /* A line box of its own, so the sender and the subject sit on the same
     centre line rather than each finding its own. */
  line-height: var(--mail-row-h);
  font-size: var(--mail-text);
  color: var(--el-text-color-regular);
}
.unread .who {
  font-weight: 700;
  color: var(--el-text-color-primary);
}

.tcount {
  margin-left: 4px;
  font-size: var(--mail-meta);
  font-weight: 400;
  color: var(--el-text-color-secondary);
}

/* Subject and snippet on one line, the snippet giving up its space first:
   what the mail is about survives truncation, the preview of it does not. */
.line {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  line-height: var(--mail-row-h);
  font-size: var(--mail-text);
  color: var(--el-text-color-secondary);
}
.subj {
  color: var(--el-text-color-regular);
}
.unread .subj {
  font-weight: 700;
  color: var(--el-text-color-primary);
}
.snip {
  margin-left: 6px;
}

.clip {
  flex: none;
  color: var(--el-text-color-secondary);
  font-size: 14px;
}

.tail {
  flex: none;
  width: var(--mail-when-w);
  display: flex;
  justify-content: flex-end;
  align-items: center;
}
.when {
  font-size: var(--mail-meta);
  color: var(--el-text-color-secondary);
  white-space: nowrap;
}
.unread .when {
  font-weight: 700;
  color: var(--el-text-color-primary);
}

/* Hidden until the row is under the cursor or holds focus — the list is for
   reading, and three icons on every line would make it a control panel. */
.acts {
  display: none;
  gap: 2px;
}
.row:hover .when,
.row:focus-within .when {
  display: none;
}
.row:hover .acts,
.row:focus-within .acts {
  display: flex;
}

.act {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: none;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  transition: background var(--mail-fast) var(--mail-ease),
    color var(--mail-fast) var(--mail-ease);
}
.act:hover {
  background: var(--el-fill-color);
  color: var(--el-text-color-primary);
}
.act:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: -2px;
}

/* Narrow: the sender column gives up width first, then the snippet goes. The
   subject is the last thing standing, because it is the only part with a
   chance of being read at this size.

   Keyed to the mailbox container, not the viewport — this list sits beside a
   rail inside an app shell, so the window's width says very little about how
   much room the row actually has. */
/* Measured against the pane, which is far narrower than the window: at a
   1280px screen the app's nav and this page's rail leave it about 770px. The
   first cut here was set as if it had the whole window and dropped the snippet
   on an ordinary desktop. */
@container mailbox (max-width: 700px) {
  .who {
    width: 132px;
  }
}
@container mailbox (max-width: 520px) {
  .snip {
    display: none;
  }
}
@container mailbox (max-width: 400px) {
  .who {
    display: none;
  }
}
</style>
