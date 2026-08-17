<template>
  <!-- Reached from a mail and nothing else, by somebody locked out. Same
       contract as the activation page it mirrors: say whose account and
       whether the link is alive BEFORE asking for a password. -->
  <div class="reset-wrap">
    <div class="lang-corner"><LangSwitcher light /></div>
    <div class="reset-brand">
      <div class="brand-mark">ERP</div>
      <h1>{{ t('reset.title') }}</h1>
    </div>

    <el-card class="reset-card" shadow="never">
      <div v-if="checking" class="state" v-loading="true" />

      <div v-else-if="fault" class="state">
        <div class="state-icon bad">✕</div>
        <p class="state-msg">{{ fault }}</p>
        <el-button link type="primary" @click="router.push('/login')">
          {{ t('reset.toLogin') }}
        </el-button>
      </div>

      <div v-else-if="done" class="state">
        <div class="state-icon good">✓</div>
        <p class="state-msg">{{ t('reset.done') }}</p>
        <p class="state-sub">{{ target.email }}</p>
        <el-button type="primary" class="wide" @click="toLogin">
          {{ t('reset.toLogin') }}
        </el-button>
      </div>

      <el-form v-else :model="form" label-position="top" @keyup.enter="submit">
        <p class="greeting">{{ t('reset.greeting', { name: target.name }) }}</p>
        <p class="sub-note">{{ t('reset.explain') }}</p>
        <el-form-item :label="t('reset.password')">
          <el-input
            v-model="form.password"
            type="password"
            show-password
            autocomplete="new-password"
          />
        </el-form-item>
        <el-form-item :label="t('reset.confirm')">
          <el-input
            v-model="form.confirm"
            type="password"
            show-password
            autocomplete="new-password"
          />
        </el-form-item>
        <div v-if="error" class="form-error">{{ error }}</div>
        <el-button type="primary" class="wide" :loading="saving" @click="submit">
          {{ t('reset.submit') }}
        </el-button>
        <p class="note">{{ t('reset.note') }}</p>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { http, quietErrors } from '../api'
import LangSwitcher from '../components/LangSwitcher.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const token = String(route.query.token ?? '')
const checking = ref(true)
const saving = ref(false)
const done = ref(false)
const fault = ref('')
const error = ref('')
const target = reactive({ name: '', email: '' })
const form = reactive({ password: '', confirm: '' })

function message(e: unknown, fallback: string): string {
  return (e as { message?: string })?.message || fallback
}

onMounted(async () => {
  if (!token) {
    fault.value = t('reset.noToken')
    checking.value = false
    return
  }
  try {
    const resp = await http.get('/auth/reset', { params: { token }, ...quietErrors })
    const d = resp.data.data as { name: string; email: string }
    target.name = d.name
    target.email = d.email
  } catch (e: unknown) {
    fault.value = message(e, t('reset.badLink'))
  } finally {
    checking.value = false
  }
})

async function submit() {
  if (form.password.length < 10) {
    error.value = t('reset.tooShort')
    return
  }
  if (form.password !== form.confirm) {
    error.value = t('reset.mismatch')
    return
  }
  saving.value = true
  error.value = ''
  try {
    await http.post('/auth/reset', { token, password: form.password }, quietErrors)
    done.value = true
  } catch (e: unknown) {
    error.value = message(e, t('reset.failed'))
  } finally {
    saving.value = false
  }
}

// No session is minted here, same as activation: a link read out of a
// mailbox must not itself be a way in.
function toLogin() {
  router.push({ path: '/login', query: { email: target.email } })
}
</script>

<style scoped>
.reset-wrap {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 28px;
  min-height: 100vh;
  background: #0f172a;
}
.lang-corner {
  position: absolute;
  top: 20px;
  right: 28px;
}
.reset-brand {
  color: #e2e8f0;
  text-align: center;
}
.brand-mark {
  display: inline-block;
  padding: 4px 12px;
  border: 1px solid #38bdf8;
  border-radius: 8px;
  color: #38bdf8;
  font-weight: 700;
  letter-spacing: 2px;
  margin-bottom: 12px;
}
.reset-brand h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 600;
}
.reset-card {
  width: min(420px, calc(100vw - 32px));
}
.state {
  min-height: 180px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  text-align: center;
}
.state-icon {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 22px;
  color: #fff;
}
.state-icon.bad {
  background: var(--el-color-danger);
}
.state-icon.good {
  background: var(--el-color-success);
}
.state-msg {
  margin: 0;
  font-size: 15px;
}
.state-sub {
  margin: 0;
  color: var(--el-text-color-secondary);
}
.greeting {
  margin: 0 0 4px;
  font-size: 16px;
  font-weight: 600;
}
.sub-note {
  margin: 0 0 16px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.form-error {
  color: var(--el-color-danger);
  margin-bottom: 12px;
  font-size: 13px;
}
.wide {
  width: 100%;
}
.note {
  margin: 12px 0 0;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
