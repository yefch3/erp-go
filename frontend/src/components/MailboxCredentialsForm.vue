<template>
  <!-- 一套字段，两个用处：第一次登录邮箱（MailboxGate），和后来再加一个
       信箱（AddMailboxDialog）。这两件事问的是同样的三个问题——哪个地址、
       哪家服务商、什么授权码——所以是一个组件，不是两份长得像的表单。

       地址**是一个字段**。从前不是：它取自登录令牌，请求体里故意没有这一
       项。那条约束在「一个人可以绑多个信箱、而且不必是公司域名的」面前站
       不住了，所以它退役，换成了服务端的四道检查（见网关 verifyMailbox 的
       注释）。 -->
  <div class="cred-form">
    <el-input
      v-model="email"
      class="cred-field"
      autocomplete="username"
      :placeholder="t('mailGate.emailPlaceholder')"
      @blur="guessProvider"
      @keyup.enter="submit"
    />

    <!-- 挑一家，服务器就不用填了。这是老板要的那件事：「他们只要负责登录
         就行，我们也省心」。 -->
    <el-select
      v-model="provider"
      class="cred-field"
      :placeholder="t('mailGate.providerPlaceholder')"
      @change="onProviderChange"
    >
      <el-option
        v-for="p in providers"
        :key="p.code"
        :label="p.label"
        :value="p.code"
      />
      <el-option :label="t('mailGate.providerOther')" :value="PROVIDER_OTHER" />
    </el-select>

    <!-- 每家的「授权码去哪儿开」。员工卡住的地方从来不是填哪个服务器，
         是这一句。 -->
    <p v-if="hint" class="cred-hint">{{ hint }}</p>

    <!-- 「其他」才展开。主机仍然由服务端校验：只放行公网域名和标准邮件
         端口，写 IP 或内网地址会被拒。 -->
    <template v-if="provider === PROVIDER_OTHER">
      <div class="cred-row">
        <el-input v-model="imapHost" :placeholder="t('mailGate.imapHost')" />
        <el-input-number v-model="imapPort" :controls="false" class="cred-port" />
      </div>
      <div class="cred-row">
        <el-input v-model="smtpHost" :placeholder="t('mailGate.smtpHost')" />
        <el-input-number v-model="smtpPort" :controls="false" class="cred-port" />
      </div>
    </template>

    <el-input
      v-model="secret"
      type="password"
      show-password
      autocomplete="current-password"
      class="cred-field"
      :placeholder="t('mailGate.codePlaceholder')"
      @keyup.enter="submit"
    />

    <div v-if="error" class="cred-error">{{ error }}</div>
    <el-button type="primary" class="cred-btn" :loading="busy" @click="submit">
      {{ submitLabel || t('mailGate.signInEmail') }}
    </el-button>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { http, mailHostRequest, quietErrors } from '../api'
import {
  MAIL_PROVIDERS,
  PROVIDER_OTHER,
  providerByCode,
  providerForAddress,
} from '../lib/mailProviders'

const props = defineProps<{
  /** 预填的地址。第一次登录时是这个人的公司邮箱，加信箱时是空的。 */
  initialEmail?: string
  submitLabel?: string
}>()
const emit = defineEmits<{ bound: [{ token: string; accountId: number; email: string }] }>()

const { t } = useI18n()
const providers = MAIL_PROVIDERS

const email = ref(props.initialEmail ?? '')
const provider = ref('')
const secret = ref('')
const smtpHost = ref('')
const smtpPort = ref(465)
const imapHost = ref('')
const imapPort = ref(993)
const busy = ref(false)
const error = ref('')

const hint = computed(() => {
  if (provider.value === PROVIDER_OTHER) return t('mailGate.otherHint')
  return providerByCode(provider.value)?.hint ?? ''
})

// 填完地址就替他挑一家。只对个人邮箱有用——企业邮用公司自己的域名，
// 从 me@sunrise.com 看不出托管在腾讯还是 263，那种情况留空，服务端会落回
// 这家公司自己配的那套。
function guessProvider() {
  if (provider.value) return
  const p = providerForAddress(email.value.trim().toLowerCase())
  if (p) provider.value = p.code
}

function onProviderChange(code: string) {
  error.value = ''
  // 挑到一家已经关掉密码登录的（Outlook），当场说清楚，别让人对着
  // 「授权码错误」猜半天。
  const p = providerByCode(code)
  if (p?.needsOAuth) error.value = p.hint
}

async function submit() {
  const addr = email.value.trim().toLowerCase()
  if (!addr) {
    error.value = t('mailGate.emailRequired')
    return
  }
  if (!secret.value) {
    error.value = t('mailGate.codeRequired')
    return
  }
  busy.value = true
  error.value = ''
  try {
    // 打邮件主机的请求必须带 mailHostRequest：一次真实的 IMAP 登录经常超过
    // axios 默认的那点超时，漏带的症状是浏览器报超时而服务端其实登录成功
    // 并存好了凭据——两边状态不一致。
    // quietErrors：授权码错是这里意料之中的答案，显示在字段旁边，不要再
    // 弹一个浮层把同一句话说第二遍。
    const resp = await http.post(
      '/mailbox/verify',
      {
        email: addr,
        secret: secret.value,
        provider: provider.value,
        ...(provider.value === PROVIDER_OTHER
          ? {
              smtpHost: smtpHost.value.trim(),
              smtpPort: smtpPort.value,
              smtpSecurity: 'SSL',
              imapHost: imapHost.value.trim(),
              imapPort: imapPort.value,
              imapSecurity: 'SSL',
            }
          : {}),
      },
      { ...mailHostRequest, ...quietErrors },
    )
    const data = resp.data.data as { token: string; accountId?: number; email?: string }
    secret.value = ''
    // 回来的地址是**落库后**那个（规范化过大小写和空格），显示它才不会
    // 和列表里那一行对不上。
    emit('bound', {
      token: data.token,
      accountId: Number(data.accountId ?? 0),
      email: data.email || addr,
    })
  } catch (e: unknown) {
    error.value = (e as { message?: string })?.message || t('mailGate.failed')
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.cred-field {
  width: 100%;
  margin-bottom: 10px;
}
.cred-row {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
}
.cred-row .el-input {
  flex: 1;
}
.cred-port {
  width: 96px;
}
.cred-hint {
  margin: -2px 0 10px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
  text-align: left;
}
.cred-error {
  margin: 2px 0 8px;
  font-size: 12px;
  color: var(--el-color-danger);
  word-break: break-all;
  text-align: left;
}
.cred-btn {
  width: 100%;
}
</style>
