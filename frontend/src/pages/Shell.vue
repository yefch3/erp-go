<template>
  <el-container class="shell">
    <el-aside width="220px" class="side">
      <div class="side-brand">
        <span class="mark">ERP</span>
        <span class="txt">{{ t('login.title') }}</span>
      </div>
      <el-menu :default-active="menuActive" router class="side-menu">
        <!-- No permission gate: 我的待办 is the landing page and the one
             surface every employee owns. Somebody with no approval role sees
             an empty list (and, with no roles at all, a hint to ask the
             administrator) — hiding the page would leave them nowhere. -->
        <el-menu-item index="/todos">
          {{ t('menu.todos') }}
        </el-menu-item>
        <el-popover
          v-if="auth.can('iam:employee:read') || auth.can('iam:department:read') || auth.can('masterdata:customer:read') || auth.can('masterdata:port:read') || auth.can('masterdata:supplier:read')"
          v-model:visible="basicDataOpen"
          placement="right-start"
          :width="200"
          :offset="6"
          :show-arrow="false"
          trigger="hover"
          popper-class="module-flyout-popper"
        >
          <template #reference>
            <button
              type="button"
              class="module-menu-trigger"
              :class="{ 'is-active': route.path.startsWith('/basic/') }"
              @click="basicDataOpen = !basicDataOpen"
            >
              <span>{{ t('menu.basicData') }}</span>
              <span class="module-menu-arrow" aria-hidden="true">›</span>
            </button>
          </template>
          <nav class="module-flyout" :aria-label="t('menu.basicData')">
            <div class="module-flyout-title">{{ t('menu.basicData') }}</div>
            <button
              v-for="item in basicDataItems"
              :key="item.path"
              type="button"
              class="module-flyout-item"
              :class="{ 'is-active': route.path.startsWith(item.activePrefix) }"
              @click="goBasicData(item.path)"
            >
              <span>{{ item.label }}</span>
              <span v-if="item.todo" class="module-flyout-badge">{{ t('menu.todo') }}</span>
            </button>
          </nav>
        </el-popover>
        <el-menu-item v-if="auth.can('product:product:read')" index="/products">
          {{ t('menu.products') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('export:contract:read')" index="/contracts">
          {{ t('menu.contracts') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('export:contract:read')" index="/contract-execution">
          {{ t('menu.contractExecution') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('export:shipment:read')" index="/shipments">
          {{ t('menu.shipments') }}
        </el-menu-item>
        <el-menu-item
          v-if="auth.can('shipping:schedule:read')"
          index="/shipping"
          @click="pullShippingReminders"
        >
          {{ t('menu.shipping') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('inventory:stock:read')" index="/stocks">
          {{ t('menu.stocks') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('inventory:stock:read')" index="/outbounds">
          {{ t('menu.outbounds') }}
        </el-menu-item>
        <el-popover
          v-if="hasProcurement"
          v-model:visible="procurementOpen"
          placement="right-start"
          :width="200"
          :offset="6"
          :show-arrow="false"
          trigger="hover"
          popper-class="module-flyout-popper"
        >
          <template #reference>
            <button
              type="button"
              class="module-menu-trigger"
              :class="{ 'is-active': procurementActive }"
              @click="procurementOpen = !procurementOpen"
            >
              <span>{{ t('menu.procurement') }}</span>
              <span class="module-menu-arrow" aria-hidden="true">›</span>
            </button>
          </template>
          <nav class="module-flyout" :aria-label="t('menu.procurement')">
            <div class="module-flyout-title">{{ t('menu.procurement') }}</div>
            <button
              v-for="item in procurementItems"
              :key="item.path"
              type="button"
              class="module-flyout-item"
              :class="{ 'is-active': route.path === item.path }"
              @click="goProcurement(item.path)"
            >
              {{ item.label }}
            </button>
          </nav>
        </el-popover>
        <el-popover
          v-if="hasFinance"
          v-model:visible="financeOpen"
          placement="right-start"
          :width="220"
          :offset="6"
          :show-arrow="false"
          trigger="hover"
          popper-class="module-flyout-popper"
        >
          <template #reference>
            <button
              type="button"
              class="module-menu-trigger"
              :class="{ 'is-active': financeActive }"
              @click="financeOpen = !financeOpen"
            >
              <span>{{ t('menu.finance') }}</span>
              <span class="module-menu-arrow" aria-hidden="true">›</span>
            </button>
          </template>
          <nav class="module-flyout" :aria-label="t('menu.finance')">
            <div class="module-flyout-title">{{ t('menu.finance') }}</div>
            <template v-for="group in financeGroups" :key="group.key">
              <div class="module-flyout-group">{{ group.label }}</div>
              <button
                v-for="item in group.items"
                :key="item.path"
                type="button"
                class="module-flyout-item"
                :class="{ 'is-active': route.path === item.path }"
                @click="goFinance(item.path)"
              >
                {{ item.label }}
              </button>
            </template>
          </nav>
        </el-popover>
        <el-menu-item v-if="auth.can('mail:email:read')" index="/emails">
          {{ t('menu.emails') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('fx:rate:read')" index="/fx">{{ t('menu.fx') }}</el-menu-item>
        <!-- Only rendered for somebody whose scope reaches past themselves;
             the server enforces it regardless. -->
        <el-menu-item v-if="auth.can('mail:email:read')" index="/team-mail">
          {{ t('menu.teamMail') }}
        </el-menu-item>
        <!-- Oversight rather than use: who took a conversation out of the
             system. Its own permission, held by managers and administrators. -->
        <el-menu-item v-if="auth.can('mail:export:audit')" index="/mail/export-log">
          {{ t('menu.exportLog') }}
        </el-menu-item>
        <el-menu-item v-if="auth.can('approval:flow:read')" index="/settings/approvals">
          {{ t('menu.approvalFlows') }}
        </el-menu-item>
      </el-menu>
    </el-aside>
    <el-container class="pane-col">
      <el-header class="topbar">
        <span />
        <div class="topbar-right">
          <ShippingArrivalNotifications
            v-if="auth.can('shipping:schedule:read') && route.path.startsWith('/shipping')"
            ref="shippingNotifications"
          />
          <!-- 应收提醒常驻：逾期的钱不该只在打开某个页面时才看得见。 -->
          <ReceivableReminders v-if="auth.can('export:receipt:read')" />
          <!-- 提单签发提醒（E2）：船开了正本还没签，整个船务部门都收得到。 -->
          <BLReminders v-if="auth.can('shipping:schedule:read')" />
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
      <el-main class="content">
        <router-view />
      </el-main>
    </el-container>

    <!-- One dialog, two moods. Voluntary: opened from the user menu, closable.
         Obligatory: the password that logged this session in was typed by an
         administrator (must_change_password), so the dialog is already open,
         cannot be dismissed, and the rest of the app waits behind it. -->
    <el-dialog
      :model-value="passwordOpen || auth.mustChangePassword"
      :title="t('password.title')"
      width="420px"
      :close-on-click-modal="!auth.mustChangePassword"
      :close-on-press-escape="!auth.mustChangePassword"
      :show-close="!auth.mustChangePassword"
      @update:model-value="(v: boolean) => { if (!auth.mustChangePassword) passwordOpen = v }"
    >
      <el-alert
        v-if="auth.mustChangePassword"
        type="warning"
        :title="t('password.mustChange')"
        :closable="false"
        show-icon
        class="pw-must"
      />
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
        <el-button v-if="!auth.mustChangePassword" @click="passwordOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="changePassword">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </el-container>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { post } from '../api'
import { useAuthStore } from '../stores/auth'
import LangSwitcher from '../components/LangSwitcher.vue'
import ShippingArrivalNotifications from '../components/ShippingArrivalNotifications.vue'
import ReceivableReminders from '../components/ReceivableReminders.vue'
import BLReminders from '../components/BLReminders.vue'
import { onLive, startLive, stopLive } from '../live'

const passwordOpen = ref(false)
const saving = ref(false)
const pw = reactive({ oldPassword: '', newPassword: '', confirm: '' })

const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const shippingNotifications = ref<InstanceType<typeof ShippingArrivalNotifications> | null>(null)
const basicDataOpen = ref(false)
const procurementOpen = ref(false)
const financeOpen = ref(false)
const hasProcurement = computed(() => [
  'procurement:sourcing:read',
  'procurement:requirement:read',
  'procurement:order:read',
].some(auth.can))
const procurementActive = computed(() => procurementItems.value.some((item) => route.path === item.path))
const menuActive = computed(() => route.path)

// 基础数据的子模块集中在右侧浮层中，避免展开后挤压左侧主导航。
const basicDataItems = computed(() => [
  ...(auth.can('iam:employee:read') || auth.can('iam:department:read')
    ? [{ path: '/basic/employees', activePrefix: '/basic/employees', label: t('menu.employees'), todo: false }]
    : []),
  ...(auth.can('masterdata:customer:read')
    ? [{ path: '/basic/customers', activePrefix: '/basic/customers', label: t('menu.customers'), todo: false }]
    : []),
  ...(auth.can('masterdata:port:read')
    ? [{ path: '/basic/ports', activePrefix: '/basic/ports', label: t('menu.ports'), todo: false }]
    : []),
  ...(auth.can('masterdata:supplier:read')
    ? [{ path: '/basic/suppliers', activePrefix: '/basic/suppliers', label: t('menu.suppliers'), todo: false }]
    : []),
])

// 采购管理与基础数据使用同一种浮层导航，子页面不再各自重复一排按钮。
const procurementItems = computed(() => [
  { path: '/procurement', label: t('procurementNav.workbench'), allowed: true },
  { path: '/procurement/intakes', label: t('procurementNav.intakes'), allowed: auth.can('procurement:sourcing:read') },
  { path: '/sourcing-cases', label: t('procurementNav.sourcing'), allowed: auth.can('procurement:sourcing:read') },
  { path: '/requirements', label: t('procurementNav.requirements'), allowed: auth.can('procurement:requirement:read') },
  { path: '/purchase-orders', label: t('procurementNav.orders'), allowed: auth.can('procurement:order:read') },
].filter((item) => item.allowed))

// 财务对账集中一处：应收看客户、应付看供应商、银行流水居中对照两边。
// 数据仍住在各自的服务里（应收在出口、应付在采购），这里只是把入口
// 摆到财务的动线上——同一个人对账不用在两个业务模块之间来回找。
const financeGroups = computed(() => [
  {
    key: 'receivable',
    label: t('financeNav.receivable'),
    items: auth.can('export:receipt:read')
      ? [
          { path: '/receivable-due', label: t('financeNav.receivableDue') },
          { path: '/receipts', label: t('financeNav.receipts') },
        ]
      : [],
  },
  {
    key: 'payable',
    label: t('financeNav.payable'),
    items: [
      ...(auth.can('procurement:invoice:read')
        ? [{ path: '/supplier-invoices', label: t('financeNav.supplierInvoices') }]
        : []),
      ...(auth.can('procurement:payment:read')
        ? [{ path: '/supplier-payments', label: t('financeNav.supplierPayments') }]
        : []),
      ...(auth.can('procurement:recon:read')
        ? [{ path: '/supplier-statements', label: t('financeNav.supplierStatements') }]
        : []),
    ],
  },
  {
    key: 'bank',
    label: t('financeNav.bank'),
    items: auth.can('procurement:payment:read')
      ? [{ path: '/bank-transactions', label: t('financeNav.bankTransactions') }]
      : [],
  },
].filter((group) => group.items.length > 0))
const hasFinance = computed(() => financeGroups.value.length > 0)
const financeActive = computed(() =>
  financeGroups.value.some((group) => group.items.some((item) => route.path === item.path)),
)

function goBasicData(path: string) {
  basicDataOpen.value = false
  router.push(path)
}

function goProcurement(path: string) {
  procurementOpen.value = false
  router.push(path)
}

function goFinance(path: string) {
  financeOpen.value = false
  router.push(path)
}

// 即使用户已经位于船期页面，再次点击菜单也会主动拉取并展示未读提醒。
function pullShippingReminders() {
  shippingNotifications.value?.refreshAndPopup()
}

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
    // Pays off the must-change debt too: the server cleared its flag the
    // moment an owner-chosen password landed, and the gate follows.
    auth.passwordChanged()
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
.pw-must {
  margin-bottom: 14px;
}
/* The shell is exactly the viewport, and the content column is the only
   thing that scrolls. It used to be the document: el-main asked for
   overflow:auto but nothing capped its height, so it grew with its content
   and the page scrolled instead — taking the navigation and the mail folder
   rail off the top of the screen with it, and breaking every position:sticky
   inside, since sticky resolves against a scroller that never scrolled. */
.shell {
  height: 100vh;
  overflow: hidden;
}
.side {
  background: #0f172a;
  color: #cbd5e1;
  /* A long menu on a short screen scrolls here rather than pushing the
     window taller. overflow-y auto, not scroll: no phantom scrollbar. */
  height: 100%;
  overflow-y: auto;
}
.pane-col {
  /* min-height:0 is what lets a flex child shrink below its content and
     hand the overflow to el-main. Without it the column stays as tall as
     the page and the scrollbar reappears on the window. */
  min-width: 0;
  min-height: 0;
}
.content {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
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
.module-menu-trigger {
  width: 100%;
  height: 56px;
  padding: 0 20px;
  border: 0;
  background: transparent;
  color: #94a3b8;
  /* `font: inherit` alone made 基础数据 and 采购管理 the two odd items in the
     sidebar: it inherits from el-menu (16px), while every el-menu-item sizes
     itself from --el-menu-item-font-size (14px) applied to the item, not the
     container. Reading Element's own variable keeps the two kinds of row in
     step through a theme change instead of pinning a number here. */
  font: inherit;
  font-size: var(--el-menu-item-font-size, var(--el-font-size-base, 14px));
  text-align: left;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: space-between;
  transition: color 0.2s, background-color 0.2s;
}
.module-menu-trigger:hover,
.module-menu-trigger:focus-visible {
  outline: none;
  color: #e2e8f0;
  background: #1e293b;
}
.module-menu-trigger.is-active {
  color: #38bdf8;
  background: #172033;
}
.module-menu-arrow {
  font-size: 22px;
  line-height: 1;
  transition: transform 0.2s;
}
.module-menu-trigger:hover .module-menu-arrow,
.module-menu-trigger:focus-visible .module-menu-arrow {
  transform: translateX(3px);
}
:global(.module-flyout-popper.el-popper) {
  padding: 8px;
  border: 1px solid #334155;
  border-radius: 10px;
  background: #172033;
  box-shadow: 0 14px 34px rgb(15 23 42 / 32%);
}
.module-flyout {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.module-flyout-title {
  padding: 8px 10px 10px;
  color: #94a3b8;
  font-size: 12px;
  font-weight: 600;
  border-bottom: 1px solid #334155;
  margin-bottom: 3px;
}
.module-flyout-item {
  min-height: 40px;
  padding: 0 10px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: #cbd5e1;
  font: inherit;
  text-align: left;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.module-flyout-item:hover,
.module-flyout-item:focus-visible {
  outline: none;
  color: #f8fafc;
  background: #26334a;
}
.module-flyout-item.is-active {
  color: #7dd3fc;
  background: #24344d;
  font-weight: 600;
}
.module-flyout-badge {
  color: #fbbf24;
  font-size: 11px;
}
.module-flyout-group {
  padding: 10px 10px 4px;
  color: #64748b;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.4px;
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
