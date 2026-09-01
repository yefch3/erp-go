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
        <!-- 未读角标。**当前这个箱不显示**：你正看着它，顶上那个列表已经
             把未读说清楚了，再挂一个数字只是重复。它的用处是「另一个箱里
             有东西」——那才是需要一眼看见的。 -->
        <span v-if="b.unread > 0 && modelValue !== b.id" class="mbox-unread">
          {{ b.unread > 99 ? '99+' : b.unread }}
        </span>
        <!-- 已解绑：还在列表里，因为历史邮件的入口就是这一行。标出来是为了
             人点进去看到「不能写信」时知道为什么。 -->
        <span v-if="b.unboundAt" class="mbox-tag mbox-off">{{ t('mailGate.unbound') }}</span>
        <span v-else-if="b.isDefault" class="mbox-tag">{{ t('mailGate.isDefault') }}</span>
      </button>
      <!-- 两个动作各占一格，**没有那个动作时留空格而不是不占位**。
           不留的话每行尾部宽度不一样（默认那行没有「设为默认」），名字那一列
           跟着长短不一，一排信箱看上去是歪的。

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

    <el-button v-if="canAdd" link class="mbox-add" @click="adding = true">
      ＋ {{ t('mailGate.addMailbox') }}
    </el-button>

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
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, post } from '../api'
import { Star, SwitchButton } from '@element-plus/icons-vue'
import MailboxCredentialsForm from './MailboxCredentialsForm.vue'
import { adoptVerification, type VerifyResponse } from '../lib/mailUnlock'

export interface Mailbox {
  id: number
  email: string
  isDefault: boolean
  lastError: string
  /** 这个箱里有多少封没读。切换的理由就是它。 */
  unread: number
  /** 解绑时间，空表示还绑着。解绑的箱只能看历史，不能收发。 */
  unboundAt: string
}

const props = defineProps<{ modelValue: number; canAdd: boolean }>()
const emit = defineEmits<{
  'update:modelValue': [number]
  /** 信箱清单变了（新绑了一个、换了默认），页面要跟着重新取列表。 */
  changed: [Mailbox[]]
  /** 刚收下一批新令牌。页面据此重算「哪些箱还开着」。 */
  added: []
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
  width: 26px;
  height: 26px;
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
.mbox-add {
  margin-top: 4px;
  font-size: 12px;
}
</style>
