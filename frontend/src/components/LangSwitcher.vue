<template>
  <el-dropdown placement="top-start" trigger="click" @command="(l: Locale) => setLocale(l)">
    <button type="button" class="lang" :class="{ light, sidebar }">
      <span v-if="sidebar" class="lang-icon" aria-hidden="true">◎</span>
      <span class="lang-copy">
        <small v-if="sidebar">Language</small>
        <strong>{{ LOCALE_LABELS[locale as Locale] }}</strong>
      </span>
      <span class="lang-arrow" aria-hidden="true">⌄</span>
    </button>
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
import { useI18n } from 'vue-i18n'
import { LOCALE_LABELS, setLocale, type Locale } from '../i18n'

defineProps<{ light?: boolean; sidebar?: boolean }>()
const { locale } = useI18n()
</script>

<style scoped>
.lang {
  border: 0;
  background: transparent;
  cursor: pointer;
  font-size: 13px;
  color: #64748b;
}
.lang.light {
  color: #94a3b8;
}
.lang.sidebar {
  width: 100%;
  min-height: 52px;
  padding: 8px 10px;
  border: 1px solid #26344a;
  border-radius: 10px;
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr) 18px;
  align-items: center;
  gap: 9px;
  color: #e2e8f0;
  background: #151f32;
  text-align: left;
  transition: border-color .18s, background .18s;
}
.lang.sidebar:hover,
.lang.sidebar:focus-visible {
  outline: none;
  border-color: #3d536d;
  background: #1a273c;
}
.lang-icon {
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  border-radius: 9px;
  color: #7dd3fc;
  background: #24364e;
  font-size: 22px;
  font-weight: 700;
}
.lang-copy { min-width: 0; display: flex; flex-direction: column; gap: 1px; }
.lang-copy small { color: #718198; font-size: 10px; letter-spacing: .04em; }
.lang-copy strong { color: #e7edf5; font-size: 13px; font-weight: 600; }
.lang-arrow { color: #718198; font-size: 17px; }
</style>
