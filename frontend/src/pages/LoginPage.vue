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
        <el-form-item :label="t('login.username')">
          <el-input v-model="form.username" placeholder="admin" autofocus />
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
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../stores/auth'
import LangSwitcher from '../components/LangSwitcher.vue'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const loading = ref(false)
const form = reactive({ username: '', password: '' })

async function submit() {
  if (!form.username || !form.password) return
  loading.value = true
  try {
    await auth.login(form.username, form.password)
    router.push('/')
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
