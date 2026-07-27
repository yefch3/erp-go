<template>
  <el-config-provider :locale="epLocale">
    <router-view />
  </el-config-provider>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'
import es from 'element-plus/es/locale/lang/es'

// Element Plus 自带组件文案（分页、空表格等）跟随应用语言。
const { locale } = useI18n()
const epLocale = computed(() => ({ zh: zhCn, en, es }[locale.value] ?? zhCn))
</script>

<style>
body {
  margin: 0;
  font-family: 'PingFang SC', 'Helvetica Neue', Arial, sans-serif;
  background: #f6f7f9;
}
</style>
