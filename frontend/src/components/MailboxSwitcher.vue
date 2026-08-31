<template>
  <!-- 左侧的信箱切换器。

       口径是**按邮箱分**，不做统一收件箱：切到哪个箱，列表就只显示那个箱
       的信。两个箱同时收到同一条会话时，那是两行，不是一行（00044）——
       统一收件箱会把它们合成一行，而"这封信该从哪个箱回"就答不上来了。

       只有一个信箱时整块不显示：一个选项的选择器是噪音。 -->
  <div v-if="boxes.length > 1 || canAdd" class="rail-scope">
    <div class="rail-label">{{ t('mailGate.emailLabel') }}</div>

    <!-- 一行两个动作：切过去，和设为默认。所以是并排两个按钮，不是一个
         按钮里套另一个——嵌套的可点击元素在 HTML 里是非法的，浏览器会把
         内层拆出去，而键盘和读屏软件对拆完的结果各有各的理解。 -->
    <div
      v-for="b in boxes"
      :key="b.id"
      class="mbox-row"
      :class="{ on: modelValue === b.id }"
    >
      <button
        class="mbox"
        type="button"
        :title="b.email"
        :aria-current="modelValue === b.id ? 'true' : undefined"
        @click="emit('update:modelValue', b.id)"
      >
        <span
          class="mbox-dot"
          :class="{ bad: !!b.lastError }"
          :title="b.lastError || undefined"
        />
        <span class="mbox-name">{{ b.email }}</span>
        <span v-if="b.isDefault" class="mbox-tag">{{ t('mailGate.isDefault') }}</span>
      </button>
      <!-- 设为默认只在非默认的那几行上出现，而且要点两次才生效：它改的是
           「以后写信从哪个地址发出去」，而客户看到的发件人跟着变。

           是 el-button 而不是一个透明的 span：span 上的透明度动画在触屏上
           没有 hover 这回事，那颗看不见的星星会一直盖在那儿吃掉点击；
           而且 span 用键盘 tab 不到。 -->
      <el-popconfirm
        v-if="!b.isDefault"
        :title="t('mailGate.setDefault')"
        @confirm="setDefault(b)"
      >
        <template #reference>
          <el-button
            link
            class="mbox-star"
            :aria-label="t('mailGate.setDefault')"
            :title="t('mailGate.setDefault')"
          >
            ☆
          </el-button>
        </template>
      </el-popconfirm>
    </div>

    <el-button v-if="canAdd" link class="mbox-add" @click="adding = true">
      ＋ {{ t('mailGate.addMailbox') }}
    </el-button>

    <el-dialog v-model="adding" :title="t('mailGate.addMailboxTitle')" width="440px">
      <MailboxCredentialsForm
        :submit-label="t('mailGate.addMailbox')"
        @bound="onAdded"
      />
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import MailboxCredentialsForm from './MailboxCredentialsForm.vue'

export interface Mailbox {
  id: number
  email: string
  isDefault: boolean
  lastError: string
}

defineProps<{ modelValue: number; canAdd: boolean }>()
const emit = defineEmits<{
  'update:modelValue': [number]
  /** 信箱清单变了（新绑了一个、换了默认），页面要跟着重新取列表。 */
  changed: [Mailbox[]]
}>()

const { t } = useI18n()
const boxes = ref<Mailbox[]>([])
const adding = ref(false)

async function load() {
  const d = await get<{ accounts?: Mailbox[] }>('/my-mailboxes')
  // 服务端按「默认排最前」返回，这个顺序就是"用哪个箱发信"的答案，
  // 不要在前端再排一次。
  boxes.value = (d.accounts ?? []).map((a) => ({
    id: Number(a.id ?? 0),
    email: a.email ?? '',
    isDefault: !!a.isDefault,
    lastError: a.lastError ?? '',
  }))
  emit('changed', boxes.value)
}

onMounted(load)

async function setDefault(b: Mailbox) {
  await post('/my-mailboxes/default', { accountId: b.id })
  ElMessage.success(t('mailGate.setDefaultDone', { email: b.email }))
  await load()
}

async function onAdded(d: { accountId: number; email: string }) {
  adding.value = false
  ElMessage.success(t('mailGate.addMailboxDone', { email: d.email }))
  await load()
  // 绑完直接切过去：人刚填完一个地址，想看的就是它。
  if (d.accountId) emit('update:modelValue', d.accountId)
}

defineExpose({ reload: load })
</script>

<style scoped>
.rail-scope {
  margin-top: 22px;
  padding: 0 12px;
}
.rail-label {
  margin-bottom: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.mbox-row {
  display: flex;
  align-items: center;
  border-radius: 6px;
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
  padding: 6px 8px;
  border: none;
  border-radius: 6px;
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
.mbox-dot {
  flex: none;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--el-color-success);
}
/* 这个箱最近一次收发出过错。红点比一句横幅省地方，鼠标停上去看得到地址，
   点进去才是完整的错误——多信箱之后横幅说不清是哪个箱在报错。 */
.mbox-dot.bad {
  background: var(--el-color-danger);
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
/* 淡出而不是消失：opacity 0 的元素照样占位、照样可点，在触屏上就是一颗
   看不见的按钮盖在那儿。所以未激活时是浅色而不是透明，指针设备上悬停才
   变深——键盘 tab 过去也一样看得见。 */
.mbox-star {
  flex: none;
  padding: 0 8px;
  font-size: 13px;
  color: var(--el-text-color-placeholder);
}
.mbox-row:hover .mbox-star,
.mbox-star:focus-visible {
  color: var(--el-color-primary);
}
.mbox-add {
  margin-top: 4px;
  font-size: 12px;
}
</style>
