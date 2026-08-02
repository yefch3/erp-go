<template>
  <el-container class="shell">
    <el-aside width="220px" class="side">
      <div class="side-brand">
        <span class="mark">ERP</span>
        <span class="txt">{{ t('login.title') }}</span>
      </div>
      <el-menu :default-active="route.path" router class="side-menu">
        <el-menu-item v-if="auth.can('approval:task:act')" index="/todos">
          {{ t('menu.todos') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('masterdata:customer:read')" index="/customers">
          {{ t('menu.customers') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('product:product:read')" index="/products">
          {{ t('menu.products') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('export:quotation:read')" index="/quotations">
          {{ t('menu.quotations') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('export:contract:read')" index="/contracts">
          {{ t('menu.contracts') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('export:shipment:read')" index="/shipments">
          {{ t('menu.shipments') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('export:receipt:read')" index="/receipts">
          {{ t('menu.receipts') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('inventory:stock:read')" index="/stocks">
          {{ t('menu.stocks') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('inventory:stock:read')" index="/outbounds">
          {{ t('menu.outbounds') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('procurement:requirement:read')" index="/requirements">
          {{ t('menu.requirements') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('procurement:order:read')" index="/purchase-orders">
          {{ t('menu.purchaseOrders') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('mail:email:read')" index="/emails">
          {{ t('menu.emails') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('fx:rate:read')" index="/fx">{{ t('menu.fx') }}</el-menu-item>
        <!-- Only rendered for somebody whose scope reaches past themselves;
             the server enforces it regardless. -->
        <el-menu-item v-if="auth.can('mail:email:read')" index="/team-mail">
          {{ t('menu.teamMail') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('iam:employee:read')" index="/settings/employees">
          {{ t('menu.employees') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('iam:role:read')" index="/settings/roles">
          {{ t('menu.roles') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('approval:flow:read')" index="/settings/approvals">
          {{ t('menu.approvalFlows') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('mail:email:write')" index="/settings/signatures">
          {{ t('menu.signatures') }}
        </el-menu-item>
        <el-menu-item index="/suppliers" disabled>{{ t('menu.suppliers') }}{{ t('menu.todo') }}</el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="topbar">
        <span />
        <div class="topbar-right">
          <LangSwitcher />
          <el-dropdown @command="onCommand">
            <span class="user">{{ auth.employeeName || '—' }}</span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="password">{{ t('password.title') }}</el-dropdown-item>
                <el-dropdown-item command="logout">{{ t('common.logout') }}</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>
      <el-main>
        <router-view />
      </el-main>
    </el-container>

    <el-dialog v-model="passwordOpen" :title="t('password.title')" width="420px">
      <el-form label-width="110px">
        <el-form-item :label="t('password.current')">
          <el-input v-model="pw.oldPassword" type="password" show-password autocomplete="current-password" />
        </el-form-item>
        <el-form-item :label="t('password.new')">
          <el-input v-model="pw.newPassword" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <el-form-item :label="t('password.confirm')">
          <el-input v-model="pw.confirm" type="password" show-password autocomplete="new-password" />
        </el-form-item>
      </el-form>
      <p class="pw-hint">{{ t('password.hint') }}</p>
      <template #footer>
        <el-button @click="passwordOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="changePassword">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </el-container>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { post } from '../api'
import { useAuthStore } from '../stores/auth'
import LangSwitcher from '../components/LangSwitcher.vue'
import { onLive, startLive, stopLive } from '../live'

const passwordOpen = ref(false)
const saving = ref(false)
const pw = reactive({ oldPassword: '', newPassword: '', confirm: '' })

const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

// One stream for the whole session, opened once the user is inside the shell
// and closed when they leave it.
onMounted(startLive)
onUnmounted(stopLive)

// Desktop notification for new mail — but only when the person is NOT
// looking at the mailbox: on another ERP page, another window, or another
// app. Somebody watching the inbox already sees the list refresh itself,
// and ringing a bell at them is noise. Permission is requested from the
// mailbox page, never demanded here.
onUnmounted(
  onLive((e) => {
    if (e.type !== 'mail.inbound') return
    if (!('Notification' in window) || Notification.permission !== 'granted') return
    if (!document.hidden && route.path === '/emails') return
    const n = new Notification(t('emails.notifTitle'), {
      body: t('emails.notifBody'),
      // One collapsed notification however many mails arrive in a burst.
      tag: 'erp-new-mail',
    })
    n.onclick = () => {
      window.focus()
      router.push('/emails')
      n.close()
    }
  }),
)

function onCommand(cmd: string) {
  if (cmd === 'logout') {
    stopLive()
    auth.logout()
    router.push('/login')
  }
  if (cmd === 'password') {
    passwordOpen.value = true
  }
}

async function changePassword() {
  if (!pw.oldPassword || !pw.newPassword) {
    ElMessage.warning(t('password.required'))
    return
  }
  if (pw.newPassword !== pw.confirm) {
    ElMessage.warning(t('password.mismatch'))
    return
  }
  saving.value = true
  try {
    await post('/me/password', { oldPassword: pw.oldPassword, newPassword: pw.newPassword })
    ElMessage.success(t('password.changed'))
    passwordOpen.value = false
    Object.assign(pw, { oldPassword: '', newPassword: '', confirm: '' })
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.pw-hint {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.shell {
  min-height: 100vh;
}
.side {
  background: #0f172a;
  color: #cbd5e1;
}
.side-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 18px 20px;
}
.side-brand .mark {
  border: 1.5px solid #38bdf8;
  color: #38bdf8;
  font-weight: 700;
  font-size: 13px;
  padding: 2px 7px;
  border-radius: 5px;
  letter-spacing: 1px;
}
.side-brand .txt {
  font-size: 14px;
}
.side-menu {
  background: transparent;
  border-right: none;
  --el-menu-text-color: #94a3b8;
  --el-menu-hover-bg-color: #1e293b;
  --el-menu-active-color: #38bdf8;
}
.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #fff;
  border-bottom: 1px solid #e5e7eb;
}
.topbar-right {
  display: flex;
  align-items: center;
  gap: 18px;
}
.user {
  cursor: pointer;
  color: #334155;
  font-size: 14px;
}
</style>
