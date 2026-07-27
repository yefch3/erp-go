<template>
  <el-dropdown @command="(l: Locale) => setLocale(l)">
    <span class="lang" :class="{ light }">{{ LOCALE_LABELS[locale as Locale] }} ▾</span>
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

defineProps<{ light?: boolean }>()
const { locale } = useI18n()
</script>

<style scoped>
.lang {
  cursor: pointer;
  font-size: 13px;
  color: #64748b;
}
.lang.light {
  color: #94a3b8;
}
</style>
