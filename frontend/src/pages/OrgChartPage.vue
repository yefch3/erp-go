<template>
  <div>
    <BasicDataEmployeeNav />

    <div class="page-head">
      <h2>{{ t('orgChart.title') }}</h2>
      <span class="counted">{{ t('orgChart.counted', { n: shownCount, total: members.length }) }}</span>
    </div>

    <el-card shadow="never">
      <div class="bar">
        <!-- 两棵树切换。默认汇报线——「出了事找谁」比「公司长什么样」
             更常被问到，而后者点一下就有。 -->
        <el-radio-group v-model="treeType">
          <el-radio-button value="reporting">{{ t('orgChart.reporting') }}</el-radio-button>
          <el-radio-button value="department">{{ t('orgChart.department') }}</el-radio-button>
        </el-radio-group>
        <el-input
          v-model="keyword"
          clearable
          :placeholder="t('orgChart.search')"
          style="width: 260px"
        />
        <span class="grow" />
        <el-button @click="setExpanded(true)">{{ t('orgChart.expandAll') }}</el-button>
        <el-button @click="setExpanded(false)">{{ t('orgChart.collapseAll') }}</el-button>
      </div>

      <!-- 汇报线是森林：多个根很正常，一句话说清楚，免得人以为图坏了。 -->
      <el-alert
        v-if="treeType === 'reporting' && orphanCount > 0"
        type="warning"
        :closable="false"
        show-icon
        class="notice"
        :title="t('orgChart.orphanNotice', { n: orphanCount })"
      />

      <el-tree
        ref="treeRef"
        :key="treeKey"
        v-loading="loading"
        :data="tree"
        node-key="key"
        :default-expand-all="expandAll"
        :expand-on-click-node="false"
        :filter-node-method="filterNode"
        class="org-tree"
      >
        <template #default="{ data }">
          <!-- 部门节点：名字 + 这个部门直属几个人 -->
          <div v-if="data.kind === 'department'" class="node dept">
            <el-icon><OfficeBuilding /></el-icon>
            <span class="dept-name">{{ data.label }}</span>
            <span class="sub">{{ t('orgChart.directCount', { n: directMembers(data) }) }}</span>
          </div>
          <!-- 人：头像、姓名（英文名）、岗位，右边挂标签 -->
          <div v-else class="node person">
            <el-avatar :size="26" :src="data.member.avatarUrl" class="face">
              {{ data.label.slice(0, 1) }}
            </el-avatar>
            <span class="who">{{ data.label }}</span>
            <span v-if="data.member.englishName" class="sub">{{ data.member.englishName }}</span>
            <span v-if="data.member.position" class="pos">{{ data.member.position }}</span>
            <span v-if="treeType === 'reporting'" class="sub">{{ data.member.departmentName }}</span>
            <el-tag v-if="data.orphaned" size="small" type="warning" effect="plain">
              {{ t('orgChart.orphan') }}
            </el-tag>
            <el-tag v-if="data.member.leaveDate" size="small" type="danger" effect="plain">
              {{ t('orgChart.leaving', { d: data.member.leaveDate }) }}
            </el-tag>
          </div>
        </template>
      </el-tree>

      <el-empty v-if="!loading && members.length === 0" :description="t('orgChart.empty')" />
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { OfficeBuilding } from '@element-plus/icons-vue'
import { get } from '../api'
import BasicDataEmployeeNav from '../components/BasicDataEmployeeNav.vue'
import {
  buildDepartmentTree,
  buildReportingTree,
  countMembers,
  nodeMatches,
  type OrgDepartment,
  type OrgMember,
  type OrgNode,
} from '../lib/orgChart'

const { t } = useI18n()

const loading = ref(false)
const members = ref<OrgMember[]>([])
const departments = ref<OrgDepartment[]>([])
const treeType = ref<'reporting' | 'department'>('reporting')
const keyword = ref('')
const expandAll = ref(true)
// el-tree 的展开状态在组件内部，没有「全部展开/折叠」的命令式接口。
// 换 key 强制重建是最短的一条路——树最多几百个节点，重建的代价看不出来。
const treeKey = ref(0)
const treeRef = ref()

const tree = computed<OrgNode[]>(() =>
  treeType.value === 'reporting'
    ? buildReportingTree(members.value)
    : buildDepartmentTree(departments.value, members.value, t('orgChart.unassigned')),
)

// 图上画了几个人。和总数并排显示，是为了让「画丢了」这件事**看得见**——
// 少一个方块没人会发现，但 299/300 会。
const shownCount = computed(() => countMembers(tree.value))
const orphanCount = computed(() => tree.value.filter((n) => n.orphaned).length)

function directMembers(node: OrgNode): number {
  return node.children.filter((c) => c.kind === 'member').length
}

function filterNode(_value: string, data: OrgNode): boolean {
  return nodeMatches(data, keyword.value)
}

function setExpanded(on: boolean) {
  expandAll.value = on
  treeKey.value++
}

watch(keyword, (q) => {
  treeRef.value?.filter(q)
})

async function load() {
  loading.value = true
  try {
    const d = await get<{ members: OrgMember[]; departments: OrgDepartment[] }>('/org-chart')
    members.value = d.members ?? []
    departments.value = d.departments ?? []
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: baseline;
  gap: 12px;
  margin-bottom: 12px;
}

.counted {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.grow {
  flex: 1;
}

.notice {
  margin-bottom: 12px;
}

/* 行高给足：一行里有头像、姓名、岗位和标签，挤在一起就读不成一句话了。 */
.org-tree :deep(.el-tree-node__content) {
  height: 38px;
}

.node {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.face {
  flex: none;
  font-size: 12px;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
}

.dept-name {
  font-weight: 600;
}

.who {
  font-weight: 500;
}

/* 次要信息统一压一档：一行里三种字号会让眼睛无处落脚。 */
.sub,
.pos {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.pos {
  padding: 0 6px;
  border-radius: 9px;
  background: var(--el-fill-color);
}
</style>
