<template>
  <div class="pending-uploads">
    <ul v-if="modelValue.length" class="pending-list">
      <li v-for="item in modelValue" :key="item.id" class="pending-row" :class="`is-${item.status}`">
        <el-select
          class="pending-category"
          size="small"
          :model-value="item.category"
          :disabled="disabled || item.status === 'uploading'"
          :aria-label="t('uploadList.category')"
          @update:model-value="(value: string) => change(patchPending(modelValue, item.id, { category: value }))"
        >
          <el-option v-for="(label, key) in categories" :key="key" :value="key" :label="label" />
        </el-select>
        <span class="pending-file" :title="item.file.name">
          <strong>{{ item.file.name }}</strong>
          <small v-if="item.status === 'failed'" class="pending-reason" role="alert">{{ t('uploadList.failed', { reason: item.error }) }}</small>
          <small v-else-if="item.status === 'uploading'">{{ t('uploadList.uploading') }}</small>
          <small v-else>
            {{ formatFileSize(item.file.size) }} · {{ t('uploadList.waiting') }}
            <span v-if="takenNames.includes(item.file.name)" class="pending-note"> · {{ t('uploadList.sameName') }}</span>
          </small>
        </span>
        <el-button
          link
          type="danger"
          size="small"
          :disabled="disabled || item.status === 'uploading'"
          @click="change(removePending(modelValue, item.id))"
        >{{ t('common.remove') }}</el-button>
      </li>
    </ul>
    <div class="pending-add">
      <label class="pending-pick" :class="{ 'is-disabled': disabled }">
        <input type="file" multiple :disabled="disabled" @change="pick" />
        <el-icon aria-hidden="true"><Plus /></el-icon>
        <span>{{ modelValue.length ? t('uploadList.addMore') : t('uploadList.add') }}</span>
      </label>
      <span class="pending-hint">{{ t('uploadList.hint', { max: maxMb }) }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
// 「待上传」清单的样子：一份文件一行，各选各的类型，能单独拿掉；下面一颗
// 「添加文件」，每点一次是往清单里**加**，不是换。
//
// 清单本身的规矩（去重、上限、默认类型）在 lib/pendingUploads.ts，那里有测试；
// 这里只管把它画出来、把人的操作翻译成「一张新清单」交还给页面。上传不在这里
// 做——传到哪个接口是页面的事，这个组件不该知道。
import { computed } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'

import {
  MAX_UPLOAD_BYTES,
  addPicked,
  categoryForNext,
  formatFileSize,
  patchPending,
  removePending,
  type PendingUpload,
} from '../lib/pendingUploads'

const props = withDefaults(
  defineProps<{
    modelValue: PendingUpload[]
    /** 类型的取值和给人看的名字，例如 { INVOICE: '发票' }。 */
    categories: Record<string, string>
    /** 清单空着时，新加的文件用哪个类型。 */
    defaultCategory: string
    /**
     * 已经交上去的文件名。清单里有同名的就在那一行提醒一句——只提醒不拦：
     * 交上去的凭证删不掉，重复交了很烦；但两份不同的文件同名也是常事。
     */
    takenNames?: string[]
    /** 页面在保存、上传时置真：这期间清单不能动。 */
    disabled?: boolean
    maxBytes?: number
  }>(),
  { takenNames: () => [], disabled: false, maxBytes: MAX_UPLOAD_BYTES },
)
const emit = defineEmits<{ 'update:modelValue': [list: PendingUpload[]] }>()
const { t, locale } = useI18n()

const maxMb = computed(() => Math.floor(props.maxBytes / (1024 * 1024)))

// 只要在这一张清单里不重复就行。
let sequence = 0
const nextId = () => `pending-${++sequence}`

function change(list: PendingUpload[]) {
  emit('update:modelValue', list)
}

function pick(event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  // 清掉浏览器记着的选择：不清的话，拿掉一份之后再选同一份，浏览器认为
  // 「没变」，不触发 change，看起来就是「选了没反应」。
  input.value = ''
  if (!files.length) return

  const outcome = addPicked(
    props.modelValue,
    files,
    categoryForNext(props.modelValue, props.defaultCategory),
    nextId,
    props.maxBytes,
  )
  const names = (list: string[]) => list.join(locale.value === 'zh' ? '、' : ', ')
  if (outcome.tooLarge.length) {
    ElMessage.warning(t('uploadList.tooLarge', { names: names(outcome.tooLarge), max: maxMb.value }))
  }
  if (outcome.empty.length) ElMessage.warning(t('uploadList.empty', { names: names(outcome.empty) }))
  if (outcome.duplicates.length) ElMessage.info(t('uploadList.duplicate', { names: names(outcome.duplicates) }))
  if (outcome.added) change(outcome.list)
}
</script>

<style scoped>
.pending-uploads {
  --pu-accent: var(--el-color-primary, #159fdc);
  --pu-danger: var(--el-color-danger, #d9534f);
  --pu-line: #dceaf0;
  --pu-ink: #294a58;
  --pu-muted: #80909f;
  --pu-tint: #f6fbfd;
  --pu-fast: 150ms;

  display: grid;
  gap: 8px;
  width: 100%;
  line-height: 1.4;
}

.pending-list {
  max-height: 236px;
  margin: 0;
  padding: 0;
  overflow-y: auto;
  border: 1px solid var(--pu-line);
  border-radius: 10px;
  background: var(--pu-tint);
  list-style: none;
}

.pending-row {
  position: relative;
  display: grid;
  grid-template-columns: 112px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  min-height: 46px;
  padding: 6px 10px;
  border-bottom: 1px solid var(--pu-line);
}
.pending-row:last-child {
  border-bottom: 0;
}

/* 失败的那一行要一眼找得到：左边一道红，原因写在文件名下面。 */
.pending-row.is-failed {
  background: #fdf4f3;
  box-shadow: inset 3px 0 0 var(--pu-danger);
}

/* 正在传的那一行底下有一道来回扫的光。只动 transform，不碰布局。 */
.pending-row.is-uploading {
  overflow: hidden;
}
.pending-row.is-uploading::after {
  content: '';
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  height: 2px;
  background: linear-gradient(90deg, transparent, var(--pu-accent), transparent);
  transform: translateX(-100%);
  animation: pending-sweep 1.2s ease-in-out infinite;
}
@keyframes pending-sweep {
  to {
    transform: translateX(100%);
  }
}
@media (prefers-reduced-motion: reduce) {
  .pending-row.is-uploading::after {
    transform: none;
    animation: none;
    opacity: 0.6;
  }
}

.pending-file {
  display: grid;
  gap: 1px;
  min-width: 0;
}
.pending-file strong {
  overflow: hidden;
  color: var(--pu-ink);
  font-size: 13px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pending-file small {
  color: var(--pu-muted);
  font-size: 12px;
}
.pending-file .pending-reason {
  color: var(--pu-danger);
  overflow-wrap: anywhere;
}
.pending-file .pending-note {
  color: #b7791f;
}

.pending-add {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
}

.pending-pick {
  position: relative;
  display: inline-flex;
  flex: none;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border: 1px dashed var(--pu-accent);
  border-radius: 8px;
  background: #fff;
  color: var(--pu-accent);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition:
    background-color var(--pu-fast) ease,
    transform var(--pu-fast) ease;
}
.pending-pick:hover {
  background: #eef8fd;
}
.pending-pick:active {
  transform: translateY(1px);
}
/* 键盘停到按钮上时才画焦点框。不认 :has 的老浏览器退回 :focus-within（鼠标点了也会画，无妨）。 */
.pending-pick:has(input:focus-visible) {
  outline: 2px solid var(--pu-accent);
  outline-offset: 2px;
}
@supports not selector(:has(*)) {
  .pending-pick:focus-within {
    outline: 2px solid var(--pu-accent);
    outline-offset: 2px;
  }
}
.pending-pick.is-disabled {
  cursor: not-allowed;
  opacity: 0.55;
}
/* 选择框盖在整颗按钮上、看不见：点得到，键盘也停得住（display:none 就停不住了）。 */
.pending-pick input {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  cursor: inherit;
  opacity: 0;
}

.pending-hint {
  flex: 1 1 240px;
  color: var(--pu-muted);
  font-size: 12px;
}

/* 认得 color-mix 的浏览器按主题色调浅底；写在上面那两条规则里当「后备」不成立——带 var() 的
   声明解析时总算合法，前一条会被它顶掉，到了算值时才失效成透明。 */
@supports (background: color-mix(in srgb, red 10%, white)) {
  .pending-row.is-failed {
    background: color-mix(in srgb, var(--pu-danger) 7%, #fff);
  }
  .pending-pick:hover {
    background: color-mix(in srgb, var(--pu-accent) 9%, #fff);
  }
}

@media (max-width: 600px) {
  .pending-row {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .pending-category {
    grid-column: 1 / -1;
  }
}
</style>
