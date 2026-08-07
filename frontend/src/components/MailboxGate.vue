<template>
  <!-- The mailbox sign-in, and nothing else.
       
       There used to be a notion of "binding" here, with a bound state, an
       "enter" button for it and a separate "switch account" link beside. All
       of that described bookkeeping rather than anything the person wanted:
       which address gets used was settled the moment they logged in to the
       ERP, so there was never a choice to present. What is left is the only
       question there ever was — are you signed in to your mailbox or not.

       Two doors, because two kinds of mail host exist. Google's own login
       page, or the password / client authorisation code that 263 and the rest
       want. Neither asks for an address: it comes from the session. -->
  <div class="gate">
    <el-card shadow="never" class="gate-card">
      <div class="gate-icon">✉️</div>
      <h3 class="gate-title">{{ t('mailGate.title') }}</h3>
      <p class="gate-text">{{ t('mailGate.explain') }}</p>
      <div class="gate-whoami">{{ signedInAs }}</div>

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

      <!-- The other door: whatever the host honours in the password slot, the
           account password or a client authorisation code. One field serves
           both; the host decides which it accepts. -->
      <el-input
        v-model="secret"
        type="password"
        show-password
        autocomplete="current-password"
        class="gate-field"
        :placeholder="t('mailGate.codePlaceholder')"
        @keyup.enter="emailSignIn"
      />
      <div v-if="error" class="gate-error">{{ error }}</div>
      <el-button type="primary" class="gate-btn" :loading="emailBusy" @click="emailSignIn">
        {{ t('mailGate.signInEmail') }}
      </el-button>

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
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, http, mailHostRequest, quietErrors } from '../api'
import { useAuthStore } from '../stores/auth'

const emit = defineEmits<{ unlocked: []; hostSettings: [] }>()
const { t } = useI18n()
const auth = useAuthStore()

// The host settings are one config for the whole company, so the entry only
// shows for whoever holds the administrative permission.
const canEditHost = auth.can('iam:role:write')

const account = reactive({ email: '', username: '', authKind: '' })
// Which mailbox this is about. Stated, never asked: the address was settled
// when they logged in to the ERP, and both doors take it from the session —
// the password one ignores any address a caller sends, and the Google one
// refuses a grant for a different account.
const signedInAs = computed(() =>
  auth.employeeEmail ? t('mailGate.forMailbox', { email: auth.employeeEmail }) : '',
)
const secret = ref('')
const googleBusy = ref(false)
const emailBusy = ref(false)
const error = ref('')


onMounted(async () => {
  // Show whose mailbox is being asked about — typing a code into an
  // anonymous prompt is how people type it into the wrong place.
  try {
    const d = await get<{ account: { email: string; username: string; authKind: string } }>(
      '/my-mail-account',
    )
    account.email = d.account?.email ?? ''
    account.username = d.account?.username ?? ''
    account.authKind = d.account?.authKind ?? ''
  } catch {
    /* the gate still works without the label */
  }
})


// A full-page departure, not a popup: popups get blocked, and Google's page
// is exactly where the person should see themselves go.
async function startOAuth() {
  googleBusy.value = true
  try {
    const d = await get<{ url: string }>('/oauth/google/start')
    window.location.href = d.url
  } finally {
    googleBusy.value = false
  }
}


// Sign-in with the address and code is also what binds the mailbox. The
// server verifies the typed pair by a live login FIRST and stores it only on
// success — so a mistyped code never overwrites a working credential and a
// failed password attempt never destroys a Google binding. Entering a
// different address rebinds (the server clears the old mailbox's synced
// mail); a rotated app password heals itself on the next successful sign-in.
async function emailSignIn() {
  if (!secret.value) {
    error.value = t('mailGate.codeRequired')
    return
  }
  emailBusy.value = true
  error.value = ''
  try {
    await verify(secret.value)
  } catch (e: unknown) {
    error.value = (e as { message?: string })?.message || t('mailGate.failed')
  } finally {
    emailBusy.value = false
  }
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
async function verify(code: string) {
  // Verifying is a live IMAP login against the person's own mail host, which
  // is nothing like a database call: the default client timeout would give up
  // on a slow but perfectly good sign-in.
  // Quiet: a wrong code is an expected answer, shown next to the field that
  // produced it rather than as a toast over the whole page.
  const resp = await http.post('/mailbox/verify', { secret: code }, {
    ...mailHostRequest,
    ...quietErrors,
  })
  const data = resp.data.data as { token: string }
  localStorage.setItem('mailUnlock', data.token)
  secret.value = ''
  emit('unlocked')
}
</script>

<style scoped>
.gate-whoami {
  /* Reads as a statement of fact, not a field: this is the mailbox that will
     be bound, and there is nothing here to change. */
  margin-bottom: 10px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
  text-align: center;
}
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
.gate-field {
  margin-bottom: 10px;
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
