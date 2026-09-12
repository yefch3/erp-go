<template>
  <div class="login-wrap">
    <div class="lang-corner"><LangSwitcher light /></div>
    <div class="login-brand">
      <div class="brand-mark">ERP</div>
      <h1>{{ t('login.title') }}</h1>
      <p>{{ t('login.subtitle') }}</p>
    </div>
    <el-card class="login-card" shadow="never">
      <!-- 忘记密码. One field, one sentence back — and always the same
           sentence: whether an address has an account here is not something
           this page tells strangers. -->
      <template v-if="forgotOpen">
        <el-form label-position="top" @keyup.enter="sendForgot">
          <p class="forgot-title">{{ t('login.forgotTitle') }}</p>
          <template v-if="forgotSent">
            <el-alert type="success" :title="t('login.forgotSent')" :closable="false" show-icon />
            <el-button class="login-btn" @click="forgotOpen = false">{{ t('login.backToLogin') }}</el-button>
          </template>
          <template v-else>
            <p class="forgot-note">{{ t('login.forgotExplain') }}</p>
            <p class="forgot-note">{{ t('login.forgotUsernameHint') }}</p>
            <el-form-item :label="t('login.email')">
              <el-input v-model="forgotEmail" type="email" placeholder="you@yourcompany.com" />
            </el-form-item>
            <el-button type="primary" class="login-btn" :loading="loading" @click="sendForgot">
              {{ t('login.forgotSubmit') }}
            </el-button>
            <el-button link class="forgot-link" @click="forgotOpen = false">{{ t('login.backToLogin') }}</el-button>
          </template>
        </el-form>
      </template>

      <el-form v-else :model="form" label-position="top" @keyup.enter="submit">
        <el-form-item :label="t('login.account')">
          <!-- 用户名或邮箱，一个框。没有「选公司」这一步，也不该加：一个登录页
               服务所有公司，靠名字本身系统内唯一（邮箱天然唯一，用户名靠
               全局唯一索引）。服务端按有没有 @ 分两条路。 -->
          <el-input v-model="form.account" placeholder="用户名或邮箱" autofocus autocomplete="username" @input="errorKey = ''" />
        </el-form-item>
        <el-form-item :label="t('login.password')">
          <el-input v-model="form.password" type="password" show-password placeholder="••••••••" @input="errorKey = ''" />
        </el-form-item>
        <el-alert
          v-if="errorKey"
          class="login-error"
          type="error"
          :title="t(errorKey)"
          :closable="false"
          show-icon
        />
        <el-button type="primary" class="login-btn" :loading="loading" @click="submit">
          {{ t('login.submit') }}
        </el-button>
        <el-button link class="forgot-link" @click="openForgot">{{ t('login.forgot') }}</el-button>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { post, quietErrors } from '../api'
import { useAuthStore } from '../stores/auth'
import LangSwitcher from '../components/LangSwitcher.vue'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const loading = ref(false)
// Keep the translation key rather than the rendered sentence so an error
// already on screen follows the language switcher immediately.
const errorKey = ref('')
// Prefilled when arriving straight from activation, which is the one moment
// somebody has just learned that their email is now their login name and has
// no reason to know it yet. Harmless from any other source: an address in a
// query string is not a credential, and a wrong one just fails to log in.
const form = reactive({
  // 激活页和重置页跳回来时把地址带在 ?email= 里，这里预填；那些页面还叫它
  // email，因为它们那儿确实只有邮箱。
  account: typeof route.query.email === 'string' ? route.query.email : '',
  password: '',
})

// Back to the page the session died on, when there is one. A relative path
// only: ?redirect= comes from the address bar, and following an absolute URL
// out of it would make this form an open redirect.
function landing() {
  const to = route.query.redirect
  if (typeof to !== 'string' || !to.startsWith('/') || to.startsWith('//')) return '/'
  return to
}

const forgotOpen = ref(false)
const forgotSent = ref(false)
const forgotEmail = ref('')

function openForgot() {
  // Whatever address is already typed rides along; retyping it would be the
  // only cost of the panel being a panel.
  forgotEmail.value = form.account.includes('@') ? form.account : ''
  forgotSent.value = false
  forgotOpen.value = true
}

async function sendForgot() {
  const addr = forgotEmail.value.trim()
  if (!addr || !addr.includes('@')) return
  loading.value = true
  try {
    await post('/auth/forgot-password', { email: addr }, quietErrors)
  } catch {
    // The answer on screen is the same either way — the server already
    // answers identically on purpose, and a network hiccup must not become
    // the one distinguishable outcome.
  } finally {
    forgotSent.value = true
    loading.value = false
  }
}

async function submit() {
  if (!form.account || !form.password) return
  errorKey.value = ''
  loading.value = true
  try {
    await auth.login(form.account, form.password)
    router.push(landing())
  } catch (e) {
    const code = (e as { code?: string })?.code
    const messages: Record<string, string> = {
      IAM_BAD_CREDENTIALS: 'login.invalidCredentials',
      IAM_ACCOUNT_LOCKED: 'login.accountLocked',
      GATEWAY_TOO_MANY_ATTEMPTS: 'login.tooManyAttempts',
    }
    errorKey.value = messages[code ?? ''] ?? 'login.failed'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-wrap {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 28px;
  background: #0f172a;
  position: relative;
}
.lang-corner {
  position: absolute;
  top: 20px;
  right: 28px;
}
.login-brand {
  text-align: center;
  color: #e2e8f0;
}
.brand-mark {
  display: inline-block;
  padding: 6px 14px;
  border: 2px solid #38bdf8;
  color: #38bdf8;
  font-weight: 700;
  font-size: 20px;
  letter-spacing: 2px;
  border-radius: 6px;
  margin-bottom: 12px;
}
.login-brand h1 {
  font-size: 22px;
  font-weight: 500;
  margin: 0 0 6px;
}
.login-brand p {
  font-size: 13px;
  color: #64748b;
  margin: 0;
  letter-spacing: 1px;
}
.login-card {
  width: 360px;
  border-radius: 10px;
}
.login-btn {
  width: 100%;
  margin-top: 4px;
}
.forgot-link {
  width: 100%;
  margin: 10px 0 0;
}
.forgot-title {
  margin: 0 0 6px;
  font-size: 16px;
  font-weight: 600;
}
.forgot-note {
  margin: 0 0 14px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.login-error {
  margin-bottom: 16px;
}
</style>
