<template>
  <el-tabs :model-value="active" class="employee-nav" @tab-change="go">
    <el-tab-pane :label="t('basicData.employeeList')" name="/basic/employees" />
    <el-tab-pane :label="t('basicData.departments')" name="/basic/employees/departments" />
    <el-tab-pane :label="t('basicData.roles')" name="/basic/employees/roles" />
    <el-tab-pane :label="t('basicData.orgChart')" name="/basic/employees/org" />
  </el-tabs>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const active = computed(() => route.path)

// 员工、部门和角色共用同一个二级导航，避免三个页面各自维护跳转规则。
function go(path: string | number) {
  router.push(String(path))
}
</script>

<style scoped>
.employee-nav { margin-bottom: 16px; }
</style>
