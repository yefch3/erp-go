<template>
  <section class="todo-settings" v-loading="loading">
    <header><h3>{{ t('todoReception.title') }}</h3><p>{{ t('todoReception.hint') }}</p></header>
    <div class="approval-reception">
      <strong>{{ t('todoReception.personalApprovals') }}</strong>
      <span aria-hidden="true">·</span>
      <span>{{ t('todoReception.alwaysReceived') }}</span>
    </div>
    <el-alert v-if="failed" :title="t('todoReception.loadFailed')" type="warning" :closable="false" />
    <template v-else-if="initial">
      <div class="business-reception">
        <div class="business-reception-heading">
          <h4 id="business-todo-heading">{{ t('todoReception.businessTodos') }}</h4>
          <el-switch v-model="approvalsOnly" :disabled="!editable || saving"
            :active-text="t('todoReception.approvalsOnly')" :aria-label="t('todoReception.approvalsOnly')" />
        </div>
        <p v-if="target === 'employee'" class="setting-note">{{ t(approvalsOnly ? 'todoReception.personal' : initial.approvalsOnly && !initial.employeeOverride ? 'todoReception.inheritedApprovals' : 'todoReception.inherit') }}</p>
        <template v-else>
          <el-checkbox-group v-model="categories" :disabled="!editable || saving || approvalsOnly"
            class="category-options" aria-labelledby="business-todo-heading">
            <el-checkbox v-for="category in todoCategories" :key="category" :value="category">
              {{ t(`todoReception.categories.${category}`) }}
            </el-checkbox>
          </el-checkbox-group>
          <p class="setting-note">{{ t('todoReception.rolePriority') }}</p>
        </template>
      </div>
      <el-button v-if="editable && changed" type="primary" size="small" :loading="saving" @click="save">
        {{ t('common.save') }}
      </el-button>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { getTodoReception, saveTodoReception, todoCategories, type TodoCategory, type TodoReception } from '../lib/todoReception'

const props = defineProps<{ target: 'role' | 'employee'; targetId: string; editable: boolean }>()
const { t } = useI18n()
const path = computed(() => `/${props.target === 'role' ? 'roles' : 'employees'}/${props.targetId}/todo-reception`)
const initial = ref<TodoReception | null>(null)
const approvalsOnly = ref(false)
const categories = ref<TodoCategory[]>([])
const failed = ref(false)
const loading = ref(false)
const saving = ref(false)

const changed = computed(() => !!initial.value && (
  approvalsOnly.value !== (props.target === 'role' ? initial.value.approvalsOnly : initial.value.employeeOverride) ||
  (props.target === 'role' && (categories.value.length !== initial.value.categories.length ||
    categories.value.some(category => !initial.value?.categories.includes(category))))
))

function apply(settings: TodoReception) {
  initial.value = settings
  approvalsOnly.value = props.target === 'role' ? settings.approvalsOnly : settings.employeeOverride
  categories.value = [...settings.categories]
}

watch(path, async current => {
  initial.value = null
  failed.value = false
  loading.value = true
  try {
    const settings = await getTodoReception(current)
    if (current === path.value) apply(settings)
  } catch {
    if (current === path.value) failed.value = true
  } finally {
    if (current === path.value) loading.value = false
  }
}, { immediate: true })

async function save() {
  const current = path.value
  saving.value = true
  try {
    const settings = await saveTodoReception(current, approvalsOnly.value, props.target === 'role' ? categories.value : undefined)
    if (current === path.value) apply(settings)
    ElMessage.success(t('todoReception.saved'))
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.todo-settings { margin: 16px 0; padding: 14px 0; border-top: 1px solid var(--el-border-color-light); }
.todo-settings h3 { margin: 0 0 5px; font-size: 16px; }
.todo-settings p { margin: 0 0 9px; color: var(--el-text-color-secondary); font-size: 13px; }
.approval-reception {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin: 10px 0 14px;
  padding: 8px 12px;
  border-radius: 6px;
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary-dark-2);
  font-size: 14px;
}
.business-reception-heading { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 16px; }
.business-reception-heading h4 { margin: 0; font-size: 14px; }
.category-options { display: flex; flex-wrap: wrap; column-gap: 20px; margin-top: 4px; }
.category-options :deep(.el-checkbox) { margin-inline-end: 0; }
.todo-settings .setting-note { margin-top: 7px; }
</style>
