<template>
  <!-- The first screen a new employee ever sees of this system, reached from a
       mail and nothing else. It has to answer three things before asking for
       anything: whose account this is, that the link is real, and what happens
       after. Somebody who cannot tell those apart will not type a password. -->
  <div class="activate-wrap">
    <div class="lang-corner"><LangSwitcher light /></div>
    <div class="activate-brand">
      <div class="brand-mark">ERP</div>
      <h1>{{ t('activate.title') }}</h1>
    </div>

    <el-card class="activate-card" shadow="never">
      <div v-if="checking" class="state" v-loading="true" />

      <!-- A dead link. Said before a password is asked for, not after: three
           of the four reasons cannot be fixed by trying again, and the person
           reading this has no other channel to ask what went wrong. -->
      <div v-else-if="fault" class="state">
        <div class="state-icon bad">✕</div>
        <p class="state-msg">{{ fault }}</p>
        <el-button link type="primary" @click="router.push('/login')">
          {{ t('activate.toLogin') }}
        </el-button>
      </div>

      <!-- Done. The address is shown filled in on the way to the login page,
           because it is now the login name and nobody told them that. -->
      <div v-else-if="done" class="state">
        <div class="state-icon good">✓</div>
        <p class="state-msg">{{ t('activate.done') }}</p>
        <p class="state-sub">{{ target.email }}</p>
        <el-button type="primary" class="wide" @click="toLogin">
          {{ t('activate.toLogin') }}
        </el-button>
      </div>

      <el-form v-else :model="form" label-position="top" @keyup.enter="submit">
        <p class="greeting">{{ t('activate.greeting', { name: target.name }) }}</p>
        <!-- Stated, never editable. The address is what the link was mailed
             to and what will become the login name; offering a field here
             would invite somebody to change it to one they never proved. -->
        <div class="whoami">{{ target.email }}</div>

        <el-form-item :label="t('activate.password')">
          <el-input
            v-model="form.password"
            type="password"
            show-password
            autocomplete="new-password"
            :placeholder="t('activate.passwordHint')"
            autofocus
          />
        </el-form-item>
        <el-form-item :label="t('activate.confirm')">
          <el-input
            v-model="form.confirm"
            type="password"
            show-password
            autocomplete="new-password"
          />
        </el-form-item>
        <div v-if="error" class="form-error">{{ error }}</div>
        <el-button type="primary" class="wide" :loading="saving" @click="submit">
          {{ t('activate.submit') }}
        </el-button>
        <p class="note">{{ t('activate.note') }}</p>
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
// fault is why the link is unusable, in the server's own words. Kept separate
// from `error`: one replaces the whole form, the other sits above the button.
const fault = ref('')
const error = ref('')
const target = reactive({ name: '', email: '' })
const form = reactive({ password: '', confirm: '' })

// Quiet on both calls: every failure here belongs on this page next to what
// caused it, not as a toast floating over a page that is itself the error.
function message(e: unknown, fallback: string): string {
  return (e as { message?: string })?.message || fallback
}

onMounted(async () => {
  if (!token) {
    fault.value = t('activate.noToken')
    checking.value = false
    return
  }
  try {
    const resp = await http.get('/auth/invitation', {
      params: { token },
      ...quietErrors,
    })
    const d = resp.data.data as { name: string; email: string }
    target.name = d.name
    target.email = d.email
  } catch (e: unknown) {
    fault.value = message(e, t('activate.badLink'))
  } finally {
    checking.value = false
  }
})

async function submit() {
  // Both checks are repeated on the server; they are here so the answer is
  // instant and so a mistyped confirmation never costs a round trip.
  if (form.password.length < 8) {
    error.value = t('activate.tooShort')
    return
  }
  if (form.password !== form.confirm) {
    error.value = t('activate.mismatch')
    return
  }
  saving.value = true
  error.value = ''
  try {
    await http.post('/auth/activate', { token, password: form.password }, quietErrors)
    done.value = true
  } catch (e: unknown) {
    error.value = message(e, t('activate.failed'))
  } finally {
    saving.value = false
  }
}

// No session is minted here, on purpose: the link must not itself be a way in
// once it has been read out of a mailbox. The address rides along so the login
// form arrives filled in rather than asking for what we already know.
function toLogin() {
  router.push({ path: '/login', query: { email: target.email } })
}
</script>

<style scoped>
.activate-wrap {
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
.activate-brand {
  color: #e2e8f0;
  text-align: center;
}
.brand-mark {
  display: inline-block;
  margin-bottom: 12px;
  padding: 6px 14px;
  border: 2px solid #38bdf8;
  border-radius: 6px;
  color: #38bdf8;
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 2px;
}
.activate-brand h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 500;
}
.activate-card {
  width: 380px;
  border-radius: 10px;
}
.greeting {
  margin: 0 0 4px;
  font-size: 15px;
}
.whoami {
  margin-bottom: 16px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.wide {
  width: 100%;
}
.form-error {
  margin-bottom: 10px;
  font-size: 12px;
  color: var(--el-color-danger);
}
.note {
  margin: 14px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}
.state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  min-height: 150px;
  justify-content: center;
  text-align: center;
}
.state-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: 50%;
  font-size: 22px;
  color: #fff;
}
.state-icon.good {
  background: var(--el-color-success);
}
.state-icon.bad {
  background: var(--el-color-danger);
}
.state-msg {
  margin: 0;
  font-size: 14px;
  line-height: 1.6;
}
.state-sub {
  margin: 0;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
</style>
