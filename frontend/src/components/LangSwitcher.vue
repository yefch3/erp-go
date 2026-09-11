<template>
  <el-popover
    v-if="sidebar"
    v-model:visible="sidebarOpen"
    placement="right-end"
    :width="180"
    :offset="6"
    :show-arrow="false"
    trigger="hover"
    popper-class="language-flyout-popper"
  >
    <template #reference>
      <button type="button" class="lang-sidebar">
        <svg class="lang-icon" viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="12" cy="12" r="8.5" />
          <path d="M3.8 12h16.4M12 3.5c2.2 2.3 3.3 5.1 3.3 8.5S14.2 18.2 12 20.5M12 3.5C9.8 5.8 8.7 8.6 8.7 12s1.1 6.2 3.3 8.5" />
        </svg>
        <span class="lang-copy">
          <small>Language</small>
          <strong>{{ LOCALE_LABELS[locale as Locale] }}</strong>
        </span>
      </button>
    </template>
    <nav class="language-flyout" aria-label="Language">
      <div class="language-flyout-title">Language</div>
      <button
        v-for="(label, key) in LOCALE_LABELS"
        :key="key"
        type="button"
        class="language-flyout-item"
        :class="{ 'is-active': locale === key }"
        @click="chooseLocale(key as Locale)"
      >
        <span>{{ label }}</span>
        <span v-if="locale === key" aria-hidden="true">✓</span>
      </button>
    </nav>
  </el-popover>

  <el-dropdown v-else @command="(l: Locale) => setLocale(l)">
    <span class="lang-switcher-inline" :class="{ light }">{{ LOCALE_LABELS[locale as Locale] }} ▾</span>
    <template #dropdown>
      <el-dropdown-menu>
        <el-dropdown-item v-for="(label, key) in LOCALE_LABELS" :key="key" :command="key">
          {{ label }}
        </el-dropdown-item>
      </el-dropdown-menu>
    </template>
  </el-dropdown>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { LOCALE_LABELS, setLocale, type Locale } from '../i18n'

defineProps<{ light?: boolean; sidebar?: boolean }>()
const { locale } = useI18n()
const sidebarOpen = ref(false)

function chooseLocale(next: Locale) {
  setLocale(next)
  sidebarOpen.value = false
}
</script>

<style>
.lang-switcher-inline {
  border: 0;
  background: transparent;
  cursor: pointer;
  font-size: 13px;
  color: #64748b;
}
.lang-switcher-inline.light { color: #94a3b8; }
.lang-sidebar {
  width: 100%;
  min-height: 52px;
  padding: 7px 10px;
  border: 0;
  border-radius: 6px;
  display: grid;
  grid-template-columns: 30px minmax(0, 1fr);
  align-items: center;
  gap: 9px;
  color: #94a3b8;
  background: transparent;
  text-align: left;
  cursor: pointer;
  transition: color .2s, background-color .2s;
}
.lang-sidebar:hover,
.lang-sidebar:focus-visible {
  outline: none;
  color: #f8fafc;
  background: #26334a;
}
.lang-icon {
  width: 22px;
  height: 22px;
  fill: none;
  stroke: currentColor;
  stroke-width: 1.7;
  stroke-linecap: round;
  stroke-linejoin: round;
}
.lang-copy { min-width: 0; display: flex; flex-direction: column; gap: 1px; }
.lang-copy small { color: #718198; font-size: 10px; letter-spacing: .04em; }
.lang-copy strong { color: inherit; font-size: 13px; font-weight: 600; }
.language-flyout-popper.el-popper {
  --el-popover-bg-color: #172033;
  --el-popover-border-color: #334155;
  padding: 8px;
  border: 1px solid #334155 !important;
  border-radius: 10px;
  background: #172033 !important;
  box-shadow: 0 14px 34px rgb(15 23 42 / 32%);
}
.language-flyout { display: flex; flex-direction: column; }
.language-flyout-title {
  padding: 8px 10px 10px;
  color: #94a3b8;
  font-size: 12px;
  font-weight: 700;
  border-bottom: 1px solid #334155;
  margin-bottom: 3px;
}
.language-flyout-item {
  min-height: 40px;
  padding: 0 10px;
  border: 0;
  border-radius: 6px;
  color: #cbd5e1;
  background: transparent;
  font: inherit;
  font-size: 14px;
  text-align: left;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.language-flyout-item:hover,
.language-flyout-item:focus-visible {
  outline: none;
  color: #f8fafc;
  background: #26334a;
}
.language-flyout-item.is-active {
  color: #7dd3fc;
  background: #24344d;
  font-weight: 600;
}
</style>
