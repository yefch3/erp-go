<template>
  <div>
    <BasicDataEmployeeNav />

    <div class="page-head">
      <h2>{{ t('orgChart.title') }}</h2>
      <el-radio-group v-model="view" size="small">
        <el-radio-button value="focus">{{ t('orgChart.reporting') }}</el-radio-button>
        <el-radio-button value="department">{{ t('orgChart.department') }}</el-radio-button>
      </el-radio-group>
      <span class="grow" />
      <el-select
        v-model="focusId"
        filterable
        clearable
        :placeholder="t('orgChart.jumpTo')"
        style="width: 220px"
        @clear="focusOnMe"
      >
        <el-option v-for="m in members" :key="m.id" :label="m.name" :value="m.id">
          <span>{{ m.name }}</span>
          <span class="opt-sub">{{ m.position || m.departmentName }}</span>
        </el-option>
      </el-select>
      <el-button v-if="focusId !== myID" @click="focusOnMe">{{ t('orgChart.backToMe') }}</el-button>
    </div>

    <!-- ─────────────────────────────────── 以自己为中心的关系图 -->
    <el-card v-if="view === 'focus'" v-loading="loading" shadow="never" class="chart-card">
      <template v-if="focus">
        <div class="chart">
          <!-- 上级链。竖着一路排上去，每一级都点得进去——想往上看几层就点几次。 -->
          <div v-if="ancestors.length" class="chain">
            <div v-for="(a, i) in ancestors" :key="a.id" class="chain-step">
              <OrgCard :member="a" :muted="i < ancestors.length - 1" :is-me="a.id === myID" @open="focusOn" />
              <span class="link-down" />
            </div>
          </div>
          <!-- 头上没人的时候说一句，否则那片空白像是没加载出来。 -->
          <div v-else class="top-note">{{ t('orgChart.atTheTop') }}</div>

          <!-- 同级行：和中心同一个上级的人，中心高亮。
               下属**嵌在中心那张卡片底下**，不是挂在整行底下——挂在整行底下的话，
               中心不在正中间时（比如他是最左边那个），连线会从两张卡片之间垂下来，
               看起来像是旁边那个人的下属。 -->
          <ul class="row" :class="{ 'has-parent': ancestors.length > 0 }">
            <li v-for="p in peers" :key="p.id">
              <OrgCard :member="p" :highlight="p.id === focusId" :is-me="p.id === myID" @open="focusOn" />
              <ul v-if="p.id === focusId && reports.length" class="row has-parent nested">
                <li v-for="r in reports" :key="r.id">
                  <OrgCard :member="r" :is-me="r.id === myID" @open="focusOn" />
                </li>
              </ul>
            </li>
          </ul>

          <div v-if="!reports.length" class="leaf-note">
            {{ t('orgChart.noReports', { name: focus.name }) }}
          </div>
        </div>

        <p class="legend">{{ t('orgChart.legend') }}</p>
      </template>
      <el-empty v-else-if="!loading" :description="t('orgChart.focusMissing')" />
    </el-card>

    <!-- ─────────────────────────────────────────────── 部门树 -->
    <el-card v-else v-loading="loading" shadow="never">
      <div class="bar">
        <el-input v-model="keyword" clearable :placeholder="t('orgChart.search')" style="width: 260px" />
        <span class="grow" />
        <span class="counted">{{ t('orgChart.counted', { n: shownCount, total: members.length }) }}</span>
      </div>
      <el-tree
        ref="treeRef"
        :data="deptTree"
        node-key="key"
        default-expand-all
        :expand-on-click-node="false"
        :filter-node-method="filterNode"
        class="dept-tree"
      >
        <template #default="{ data }">
          <div v-if="data.kind === 'department'" class="dept-node">
            <el-icon><OfficeBuilding /></el-icon>
            <span class="dept-name">{{ data.label }}</span>
            <span class="sub">{{ t('orgChart.directCount', { n: data.children.filter((c: OrgNode) => c.kind === 'member').length }) }}</span>
          </div>
          <div v-else class="dept-node person" @click="focusOn(data.member.id)">
            <el-avatar :size="22" :src="data.member.avatarUrl">{{ data.label.slice(0, 1) }}</el-avatar>
            <span>{{ data.label }}</span>
            <span v-if="data.member.position" class="sub">{{ data.member.position }}</span>
          </div>
        </template>
      </el-tree>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { OfficeBuilding } from '@element-plus/icons-vue'
import { get } from '../api'
import BasicDataEmployeeNav from '../components/BasicDataEmployeeNav.vue'
import OrgCard from '../components/OrgCard.vue'
import { useAuthStore } from '../stores/auth'
import {
  buildDepartmentTree,
  countMembers,
  focusView,
  nodeMatches,
  type OrgDepartment,
  type OrgMember,
  type OrgNode,
} from '../lib/orgChart'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const loading = ref(false)
const members = ref<OrgMember[]>([])
const departments = ref<OrgDepartment[]>([])
const view = ref<'focus' | 'department'>('focus')
const keyword = ref('')
const treeRef = ref()

const myID = computed(() => String(auth.employeeId || ''))
// 中心是谁写在地址里，所以「我看的是张三那一圈」这件事可以刷新、可以后退、
// 可以发给同事——和员工详情页是同一个道理。
const focusId = computed({
  get: () => String(route.query.at ?? '') || myID.value,
  set: (id: string) => {
    router.replace({ query: id && id !== myID.value ? { at: id } : {} })
  },
})

const seen = computed(() => focusView(members.value, focusId.value))
const ancestors = computed(() => seen.value.ancestors)
const focus = computed(() => seen.value.focus)
const peers = computed(() => seen.value.peers)
const reports = computed(() => seen.value.reports)

const deptTree = computed(() =>
  buildDepartmentTree(departments.value, members.value, t('orgChart.unassigned')),
)
const shownCount = computed(() => countMembers(deptTree.value))

function focusOn(id: string) {
  focusId.value = id
  view.value = 'focus'
}

function focusOnMe() {
  focusId.value = myID.value
}

function filterNode(_v: string, data: OrgNode): boolean {
  return nodeMatches(data, keyword.value)
}

watch(keyword, (q) => treeRef.value?.filter(q))

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
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.grow {
  flex: 1;
}

.opt-sub {
  float: right;
  margin-left: 16px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.chart-card :deep(.el-card__body) {
  overflow-x: auto;
}

/* 图整体居中。人少的时候靠左会显得像没画完。 */
.chart {
  display: flex;
  flex-direction: column;
  align-items: center;
  min-width: min-content;
  padding: 8px 0 4px;
}

/* ── 上级链：一列卡片，每张下面一根竖线 */
.chain {
  display: flex;
  flex-direction: column;
  align-items: center;
}

.chain-step {
  display: flex;
  flex-direction: column;
  align-items: center;
}

.link-down {
  width: 0;
  height: 22px;
  border-left: 1px solid var(--el-border-color);
}

/* ── 一行同级/下属。连线用经典的 li::before/::after 画，不测量、不用 JS，
      而且窗口一缩自己就跟着走。 */
.row {
  display: flex;
  justify-content: center;
  margin: 0;
  padding: 0;
  list-style: none;
}

.row > li {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 22px 10px 0;
}

/* 嵌套的下属行：那根从上面那张卡片垂下来的线由这一行自己画，
   所以它永远对着**那张卡片**的中心，而不是整行的中心。 */
.row.nested {
  padding-top: 22px;
}

.row.nested::before {
  content: '';
  position: absolute;
  top: 0;
  left: 50%;
  width: 0;
  height: 22px;
  border-left: 1px solid var(--el-border-color);
}

/* 有父节点时才画连接线。同级行在最顶上（中心没有上级）时不该凭空长出线头。 */
.row.has-parent > li::before,
.row.has-parent > li::after {
  content: '';
  position: absolute;
  top: 0;
  width: 50%;
  height: 22px;
  border-top: 1px solid var(--el-border-color);
}

.row.has-parent > li::before {
  right: 50%;
}

.row.has-parent > li::after {
  left: 50%;
  border-left: 1px solid var(--el-border-color);
}

/* 独苗不需要横梁，一根竖线就够。 */
.row.has-parent > li:only-child::before,
.row.has-parent > li:only-child::after {
  display: none;
}

.row.has-parent > li:only-child {
  padding-top: 22px;
}

.row.has-parent > li:only-child > :deep(*) {
  position: relative;
}

/* 最左最右的外侧半截横梁去掉，否则线会伸到没有卡片的地方去。 */
.row.has-parent > li:first-child::before,
.row.has-parent > li:last-child::after {
  border: 0 none;
}

.row.has-parent > li:last-child::before {
  border-right: 1px solid var(--el-border-color);
  border-radius: 0 6px 0 0;
}

.row.has-parent > li:first-child::after {
  border-radius: 6px 0 0 0;
}

.top-note,
.leaf-note,
.legend {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.top-note {
  margin-bottom: 6px;
}

.leaf-note {
  margin-top: 14px;
}

.legend {
  margin: 18px 0 0;
  text-align: center;
}

/* ── 部门树那一半 */
.bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.counted {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.dept-tree :deep(.el-tree-node__content) {
  height: 34px;
}

.dept-node {
  display: flex;
  align-items: center;
  gap: 8px;
}

.dept-node.person {
  cursor: pointer;
}

.dept-name {
  font-weight: 600;
}

.sub {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
