<template>
  <div class="quoted">
    <!-- Gmail's "•••", and for the same reason: turn sixteen of a conversation
         is turns one to fifteen stacked up, and a real one here measured
         thirteen screens of which the part somebody wrote was the top two
         inches. The button sits in our document rather than inside the mail's
         frame, because the frame runs no scripts — deliberately — so nothing
         inside it could ever be clickable. -->
    <button
      type="button"
      class="dots"
      :class="{ open }"
      :aria-expanded="open"
      :title="open ? t('reader.hideQuoted') : t('reader.showQuoted')"
      @click="open = !open"
    >
      <span class="glyph" aria-hidden="true">···</span>
      <span class="label">{{ open ? t('reader.hideQuoted') : t('reader.showQuoted') }}</span>
    </button>
    <!-- v-if, not v-show: an unopened history must not be built at all. That
         is the whole point — it is what stops fifteen folded messages fetching
         their pictures and firing their senders' tracking pixels. -->
    <!-- 选中文字转 Excel 的那两个事件原样往外递。销售转发客户询价时上面写一句
         「报18/10」，整封客户来信就被折进这里；不递的话，折起来的表格选中了
         什么都不会出现，看上去就像「转发的信不能转 Excel」（2026-09-24）。 -->
    <MailBody
      v-if="open"
      :html="html"
      @selection-context="emit('selectionContext', $event)"
      @selection-clear="emit('selectionClear')"
    />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import MailBody from './MailBody.vue'

const { t } = useI18n()
defineProps<{ html: string }>()
// 和 MailBody 的同名事件一模一样：页面那头拿同一个处理函数接两处。
const emit = defineEmits<{
  selectionContext: [payload: { text?: string; attachmentId?: string; x: number; y: number }]
  selectionClear: []
}>()
const open = ref(false)
</script>

<style scoped>
.quoted {
  margin-top: 10px;
}
.dots {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 1px 9px;
  border: 1px solid var(--el-border-color);
  border-radius: 10px;
  background: var(--el-fill-color-light);
  color: var(--el-text-color-secondary);
  font: inherit;
  font-size: 12px;
  line-height: 1.6;
  cursor: pointer;
  transition:
    background-color 120ms ease,
    border-color 120ms ease;
}
.dots:hover,
.dots:focus-visible {
  background: var(--el-fill-color);
  border-color: var(--el-border-color-darker);
  color: var(--el-text-color-primary);
}
/* The glyph carries the meaning on its own once the gesture is learned, so the
   words only appear on hover and while open — the same reason Gmail's is three
   dots and nothing else. */
.glyph {
  letter-spacing: 1px;
  font-weight: 700;
  transform: translateY(-3px);
}
.label {
  max-width: 0;
  overflow: hidden;
  white-space: nowrap;
  opacity: 0;
  transition:
    max-width 160ms ease,
    opacity 160ms ease;
}
.dots:hover .label,
.dots:focus-visible .label,
.dots.open .label {
  max-width: 12em;
  opacity: 1;
}
</style>
