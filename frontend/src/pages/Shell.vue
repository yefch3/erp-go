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
        <el-menu-item v-if="auth.can('fx:rate:read')" index="/fx">{{ t('menu.fx') }}</el-menu-item>
        <el-menu-item index="/suppliers" disabled>{{ t('menu.suppliers') }}{{ t('menu.todo') }}</el-menu-item>
        <el-menu-item index="/quotations" disabled>{{ t('menu.quotations') }}{{ t('menu.todo') }}</el-menu-item>
        <el-menu-item index="/contracts" disabled>{{ t('menu.contracts') }}{{ t('menu.todo') }}</el-menu-item>
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
  </el-container>
</template>

<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../stores/auth'
import LangSwitcher from '../components/LangSwitcher.vue'

const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

function onCommand(cmd: string) {
  if (cmd === 'logout') {
    auth.logout()
    router.push('/login')
  }
}
</script>

<style scoped>
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
