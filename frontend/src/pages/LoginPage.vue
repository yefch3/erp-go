<template>
  <div class="login-wrap">
    <div class="lang-corner"><LangSwitcher light /></div>
    <div class="login-brand">
      <div class="brand-mark">ERP</div>
      <h1>{{ t('login.title') }}</h1>
      <p>{{ t('login.subtitle') }}</p>
    </div>
    <el-card class="login-card" shadow="never">
      <el-form :model="form" label-position="top" @keyup.enter="submit">
        <el-form-item :label="t('login.email')">
          <!-- The domain of this address selects the company, so there is no
               company field and none should be added. -->
          <el-input v-model="form.email" type="email" placeholder="you@yourcompany.com" autofocus />
        </el-form-item>
        <el-form-item :label="t('login.password')">
          <el-input v-model="form.password" type="password" show-password placeholder="••••••••" />
        </el-form-item>
        <el-button type="primary" class="login-btn" :loading="loading" @click="submit">
          {{ t('login.submit') }}
        </el-button>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../stores/auth'
import LangSwitcher from '../components/LangSwitcher.vue'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const route = useRoute()
const loading = ref(false)
// Prefilled when arriving straight from activation, which is the one moment
// somebody has just learned that their email is now their login name and has
// no reason to know it yet. Harmless from any other source: an address in a
// query string is not a credential, and a wrong one just fails to log in.
const form = reactive({
  email: typeof route.query.email === 'string' ? route.query.email : '',
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

async function submit() {
  if (!form.email || !form.password) return
  loading.value = true
  try {
    await auth.login(form.email, form.password)
    router.push(landing())
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
</style>
