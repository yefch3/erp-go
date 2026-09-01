<template>
  <!-- 登录邮箱，兼第一次绑定。

       两扇门，因为邮件服务器有两种：Google 自己的登录页，和 263 那一类要
       密码/授权码的。

       **地址现在是一个字段。** 从前不是——它取自登录令牌，这里连问都不问，
       组件注释写的是 "it comes from the session"。那条约束防的是「以公司
       地址登录、却绑一个私人信箱」，而新的业务口径正是要允许这件事：一个人
       可以绑多个信箱，ERP 账号是 263 的人邮箱这边可以只绑 Gmail。约束换成
       了服务端的四道检查（归属只来自令牌、地址唯一、必须活体登录成功、
       服务器由服务端查表），见网关 verifyMailbox 的注释。 -->
  <div class="gate">
    <el-card shadow="never" class="gate-card">
      <div class="gate-icon">✉️</div>
      <h3 class="gate-title">{{ t('mailGate.title') }}</h3>
      <p class="gate-text">{{ t('mailGate.explain') }}</p>

      <!-- One Google door, and it always goes to Google.
           
           It used to branch: a stored grant was verified silently, and getting
           to Google's page needed the separate link below. Two controls for
           what a person experiences as one act, and the silent path could
           reuse whichever account the browser was signed in to. Now every
           click is a fresh authorisation with the account chooser shown — so
           you can always see which mailbox you are opening. -->
      <button class="google-btn" type="button" :disabled="googleBusy" @click="startOAuth">
        <svg class="g-logo" viewBox="0 0 48 48" width="18" height="18" aria-hidden="true">
          <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z"/>
          <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z"/>
          <path fill="#FBBC05" d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z"/>
          <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z"/>
        </svg>
        <span>{{ t('mailGate.signInGoogle') }}</span>
      </button>

      <el-divider class="gate-or">{{ t('mailGate.or') }}</el-divider>

      <!-- 另一扇门：地址 + 服务商 + 授权码。和「再加一个信箱」用的是同一
           套字段，所以它是一个共享组件——两处各写一份表单，改了一处忘了
           另一处是这类界面最常见的死法。 -->
      <MailboxCredentialsForm :initial-email="account.email || auth.employeeEmail" @bound="onBound" />
      <div v-if="error" class="gate-error">{{ error }}</div>

      <p class="gate-hint">{{ t('mailGate.hint') }}</p>

      <div class="gate-foot">
        <el-button v-if="!account.email" link class="foot-link" @click="skipUnbound">
          {{ t('mailGate.skipUnbound') }}
        </el-button>
        <el-button v-if="canEditHost" link class="foot-link" @click="emit('hostSettings')">
          ⚙️ {{ t('mailGate.hostSettings') }}
        </el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { get, http, mailHostRequest, quietErrors } from '../api'
import { useAuthStore } from '../stores/auth'
import MailboxCredentialsForm from './MailboxCredentialsForm.vue'
import { adoptVerification, type VerifyResponse } from '../lib/mailUnlock'

const props = defineProps<{
  /**
   * 正在问哪个信箱。0 = 默认箱（还没绑过、或者只绑了一个）。
   *
   * 门上必须写对名字。绑了两个箱的人被挡在 163 那个门前、门上却写着默认的
   * QQ 地址，人就会把 163 的授权码填进去——而那是「在一个没写名字的框里输
   * 密码」那条注释本来要防的事，多信箱之后它反倒成了错的名字。
   */
  accountId?: number
}>()
const emit = defineEmits<{ unlocked: []; hostSettings: [] }>()
const { t } = useI18n()
const auth = useAuthStore()

// The host settings are one config for the whole company, so the entry only
// shows for whoever holds the administrative permission.
const canEditHost = auth.can('iam:role:write')

const account = reactive({ email: '', username: '', authKind: '' })
const googleBusy = ref(false)
const error = ref('')


// 门上写的是**此刻要开的那个箱**，而不是这道门刚出现时的那个。
//
// 从前这里是 onMounted：门一挂上就问一次，之后再也不问。而门在锁着的时候
// **一直挂着不重建**——全部退出之后点左栏另一个箱，currentAccount 变了，
// 门上的地址纹丝不动。
//
// 后果不只是标签不对：那个地址会被当成「要授权哪个 Google 账号」的提示，
// 于是人在 Google 上挑了自己真正要开的那个，回来被一句
// 「你授权的是 A，但这一步要授权的是 B」挡下来——而 B 正是门上那个过期的名字。
//
// watch + immediate 而不是 onMounted：挂上时问一次，之后每次换箱再问。
watch(
  () => props.accountId,
  async () => {
    // 先清空。上一个箱的地址留在屏幕上直到新的回来，比空着更糟——那几百
    // 毫秒里门上写着一个错的名字，而人正要往里填授权码。
    account.email = ''
    account.username = ''
    account.authKind = ''
    error.value = ''
    try {
      const d = await get<{ account: { email: string; username: string; authKind: string } }>(
        '/my-mail-account',
        props.accountId ? { accountId: props.accountId } : undefined,
      )
      account.email = d.account?.email ?? ''
      account.username = d.account?.username ?? ''
      account.authKind = d.account?.authKind ?? ''
    } catch {
      /* the gate still works without the label */
    }
  },
  { immediate: true },
)


// A full-page departure, not a popup: popups get blocked, and Google's page
// is exactly where the person should see themselves go.
async function startOAuth() {
  googleBusy.value = true
  try {
    // 门知道自己在问哪个箱时（account.email 由上面那次探测填的），把地址
    // 带给 Google 当提示。**只在续一个已经绑好的箱时有值**：第一次绑还没有
    // 箱可指，那时不带才对——带上会把选择器预选成一个还不存在的绑定。
    const d = await get<{ url: string }>('/oauth/google/start', {
      email: account.email || '',
    })
    window.location.href = d.url
  } finally {
    googleBusy.value = false
  }
}


// 表单验成功之后拿到的令牌。服务端先真的登录一次，成功了才落库——所以
// 一次输错的授权码既不会覆盖能用的凭据，也不会毁掉一个 Google 绑定。
//
// 填一个**新地址**是新增一个信箱，不是把原来那个改掉：冲突键是地址。
function onBound(d: VerifyResponse) {
  // 服务端把这个人**每个箱**的令牌都发下来了，全存着——切换箱只是换一把，
  // 不用重新输密码。收下的动作在 adoptVerification 里，三个调用点共用一份。
  adoptVerification(d)
  emit('unlocked')
}

// An account with no mailbox bound has nothing to verify; the server answers
// with a pass so the campaigns and drafts views stay reachable.
async function skipUnbound() {
  error.value = ''
  try {
    await verify('')
  } catch (e: unknown) {
    error.value = (e as { message?: string })?.message || t('mailGate.failed')
  }
}

// Raw client rather than the helper: a wrong code is an expected answer here,
// to be shown in place instead of as a floating toast.
// 「暂不绑定」走的也是这个：不带地址、不带授权码 = 「复验已经绑好的那个」。
// 一个都没绑过的人拿到的是一句「无需验证」和一把令牌，好让活动和草稿那几
// 个页面仍然进得去。
async function verify(code: string) {
  const resp = await http.post('/mailbox/verify', { secret: code }, {
    ...mailHostRequest,
    ...quietErrors,
  })
  adoptVerification(resp.data.data as VerifyResponse)
  emit('unlocked')
}
</script>

<style scoped>
.gate {
  display: flex;
  justify-content: center;
  padding: 60px 0;
}
.gate-card {
  width: 440px;
  text-align: center;
  padding: 16px 12px;
}
.gate-icon {
  font-size: 34px;
}
.gate-title {
  margin: 10px 0 6px;
  font-size: 20px;
}
.gate-text {
  margin: 0 0 6px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.gate-account {
  margin: 0 0 16px;
  font-size: 13px;
  font-weight: 600;
}
.google-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  width: 100%;
  padding: 10px 0;
  background: #fff;
  border: 1px solid var(--el-border-color);
  border-radius: 6px;
  font-size: 14px;
  color: var(--el-text-color-primary);
  cursor: pointer;
  transition: border-color 0.2s, box-shadow 0.2s;
}
.google-btn:hover {
  border-color: var(--el-color-primary);
  box-shadow: 0 1px 6px rgb(0 0 0 / 8%);
}
.google-btn:disabled {
  opacity: 0.6;
  cursor: default;
}
.g-logo {
  flex: none;
}
.switch-google {
  margin-top: 6px;
  font-size: 12px;
}
.gate-or {
  margin: 20px 0;
}
.gate-or :deep(.el-divider__text) {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.gate-error {
  margin: 2px 0 8px;
  font-size: 12px;
  color: var(--el-color-danger);
  word-break: break-all;
  text-align: left;
}
.gate-btn {
  width: 100%;
}
.gate-hint {
  margin: 14px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
  text-align: left;
}
.gate-foot {
  display: flex;
  justify-content: center;
  gap: 14px;
  margin-top: 10px;
}
.foot-link {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
