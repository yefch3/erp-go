<template>
  <el-tabs :model-value="active" class="employee-nav" @tab-change="go">
    <el-tab-pane
      v-for="tab in tabs"
      :key="tab.path"
      :label="tab.label"
      :name="tab.path"
    />
  </el-tabs>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

// 员工、部门、角色、架构图和「我的信息」共用同一个二级导航，
// 避免五个页面各自维护跳转规则。
//
// **按权限过滤**：普通员工只有「我的信息」，其余四个他打不开。
// 摆一排点了报「没有权限」的标签，比不摆更糟——那是在告诉人「这里有东西，
// 但不给你」，而实情是这些页面本来就不是给他的。
const tabs = computed(() => [
  // 我的信息排第一，而且不看权限：每个员工都有一份自己的资料，
  // 这是这一组里唯一人人都能打开的页面。
  { path: '/basic/employees/me', label: t('profile.title') },
  ...(auth.can('iam:employee:read')
    ? [{ path: '/basic/employees', label: t('basicData.employeeList') }]
    : []),
  ...(auth.can('iam:department:read')
    ? [{ path: '/basic/employees/departments', label: t('basicData.departments') }]
    : []),
  ...(auth.can('iam:role:read')
    ? [{ path: '/basic/employees/roles', label: t('basicData.roles') }]
    : []),
  ...(auth.can('iam:employee:read')
    ? [{ path: '/basic/employees/org', label: t('basicData.orgChart') }]
    : []),
])

// 员工详情（/basic/employees/3）也算在「员工列表」这一项下面——
// 从列表点进去之后，上面那一排不该整个失去高亮。
const active = computed(() => {
  const p = route.path
  if (tabs.value.some((tab) => tab.path === p)) return p
  if (/^\/basic\/employees\/\d+$/.test(p)) return '/basic/employees'
  return p
})

function go(path: string | number) {
  router.push(String(path))
}
</script>

<style scoped>
.employee-nav {
  margin-bottom: 16px;
}
</style>
