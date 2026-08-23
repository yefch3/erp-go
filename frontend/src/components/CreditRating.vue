<template>
  <section class="credit">
    <header class="credit-head">
      <div class="credit-now">
        <span class="credit-label">{{ t('credit.currentGrade') }}</span>
        <strong v-if="grade" class="credit-grade" :class="`is-${grade.toLowerCase()}`">{{ grade }}</strong>
        <span v-else class="credit-none">{{ t('credit.neverRated') }}</span>
        <!-- 「多久没评了」和评级本身一样重要：一个三年没动过的 A，看着和
             上周刚判的 A 一模一样，而它们说的完全不是一回事。 -->
        <span class="credit-age" :class="{ 'is-stale': stale }">{{ ageLabel }}</span>
      </div>
      <el-button v-if="canRate" type="primary" plain @click="openRate">
        {{ grade ? t('credit.rerate') : t('credit.rateNow') }}
      </el-button>
    </header>

    <!-- 评级该看什么。现在大多没数据——明说没有，不显示成 0：一个凭空的
         0 看着像结论，而「暂无数据」是实话。 -->
    <div class="credit-dims">
      <div v-for="d in dimensions" :key="d.key" class="credit-dim">
        <span class="dim-name">{{ d.label }}</span>
        <span class="dim-value" :class="{ 'is-empty': !d.value }">{{ d.value || t('credit.noData') }}</span>
      </div>
    </div>

    <el-timeline v-if="history.length" class="credit-history">
      <el-timeline-item
        v-for="r in history"
        :key="r.id"
        :timestamp="formatTime(r.ratedAt)"
        :type="toneOf(r)"
      >
        <div class="credit-move">
          <template v-if="r.previousGrade">
            <span class="credit-from">{{ r.previousGrade }}</span>
            <span class="credit-arrow">→</span>
          </template>
          <strong class="credit-grade is-inline" :class="`is-${r.grade.toLowerCase()}`">{{ r.grade }}</strong>
          <span class="credit-by">{{ r.ratedByName || '—' }}</span>
        </div>
        <p class="credit-basis">{{ r.basis }}</p>
        <p v-if="evidenceText(r)" class="credit-evidence">{{ evidenceText(r) }}</p>
      </el-timeline-item>
    </el-timeline>
    <el-empty v-else :description="t('credit.emptyHistory')" :image-size="60" />

    <el-dialog v-model="rateOpen" :title="t('credit.dialogTitle')" width="520px">
      <el-form label-width="82px">
        <el-form-item :label="t('credit.grade')" required>
          <el-radio-group v-model="form.grade">
            <el-radio-button v-for="g in grades" :key="g" :value="g">{{ g }}</el-radio-button>
          </el-radio-group>
          <div class="form-note">{{ t(`credit.gradeHint.${form.grade}`) }}</div>
        </el-form-item>
        <el-form-item :label="t('credit.basis')" required>
          <el-input
            v-model="form.basis"
            type="textarea"
            :rows="3"
            :placeholder="t('credit.basisPlaceholder')"
          />
          <div class="form-note">{{ t('credit.basisNote') }}</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="rateOpen = false">{{ t('common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submit">{{ t('common.save') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { get, post } from '../api'

// 客户和供应商共用一块：问的问题不同（客户看会不会按时付钱，供应商看交期、
// 供货量、质量），但「当前几档 / 多久没评 / 依据是什么 / 历史怎么走的」
// 是同一个形状。
const props = defineProps<{
  /** 'customers' | 'suppliers' —— 直接就是接口路径的那一段 */
  party: 'customers' | 'suppliers'
  partyId: string | number
  grade: string
  gradedAt: string
  canRate: boolean
}>()
const emit = defineEmits<{ (e: 'rated', grade: string): void }>()

const { t } = useI18n()
const grades = ['A', 'B', 'C', 'D']

interface Rating {
  id: string
  grade: string
  previousGrade: string
  basis: string
  evidence: Record<string, string>
  ratedByName: string
  ratedAt: string
}

const history = ref<Rating[]>([])
const rateOpen = ref(false)
const saving = ref(false)
const form = reactive({ grade: 'B', basis: '' })

// 超过半年没动过就算旧了。这个数字不是精确科学，作用是让「早该重评了」
// 在页面上自己冒出来，而不是等人想起来。
const staleDays = 183

const ageDays = computed(() => {
  if (!props.gradedAt) return -1
  const then = new Date(props.gradedAt).getTime()
  if (!Number.isFinite(then)) return -1
  return Math.floor((Date.now() - then) / 86_400_000)
})
const stale = computed(() => ageDays.value >= staleDays)
const ageLabel = computed(() => {
  if (!props.grade) return ''
  if (ageDays.value < 0) return ''
  if (ageDays.value === 0) return t('credit.ratedToday')
  return t('credit.ratedDaysAgo', { n: ageDays.value })
})

// 评级该看的维度。客户看还款，供应商看交期、供货量、质量——都是业务定的。
// 数值目前一律空着：合同和采购单还没跑起来，算出来只会是 0。
const dimensions = computed(() => {
  const latest = history.value[0]?.evidence ?? {}
  const keys = props.party === 'customers'
    ? ['overdue_count', 'max_overdue_days', 'open_amount']
    : ['on_time_rate', 'full_supply_rate', 'quality_pass_rate']
  return keys.map((key) => ({ key, label: t(`credit.dim.${key}`), value: latest[key] ?? '' }))
})

function toneOf(r: Rating): 'primary' | 'success' | 'warning' | 'danger' {
  if (!r.previousGrade) return 'primary'
  // 降级要显眼：从 A 掉到 C 是要有人过问的事。
  return r.grade > r.previousGrade ? 'danger' : r.grade < r.previousGrade ? 'success' : 'primary'
}

function evidenceText(r: Rating): string {
  const parts = Object.entries(r.evidence ?? {})
    .filter(([, v]) => String(v).trim() !== '')
    .map(([k, v]) => `${t(`credit.dim.${k}`, k)} ${v}`)
  return parts.join(' · ')
}

function formatTime(v: string): string {
  if (!v) return '—'
  const d = new Date(v)
  return Number.isFinite(d.getTime()) ? d.toLocaleString() : v
}

async function load() {
  const d = await get<{ ratings: Rating[] }>(`/${props.party}/${props.partyId}/credit-ratings`)
  history.value = d.ratings ?? []
}

function openRate() {
  form.grade = props.grade || 'B'
  form.basis = ''
  rateOpen.value = true
}

async function submit() {
  if (!form.basis.trim()) {
    ElMessage.warning(t('credit.basisRequired'))
    return
  }
  saving.value = true
  try {
    await post(`/${props.party}/${props.partyId}/credit-ratings`, {
      grade: form.grade,
      basis: form.basis,
    })
    rateOpen.value = false
    ElMessage.success(t('credit.saved'))
    emit('rated', form.grade)
    await load()
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.credit {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.credit-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.credit-now {
  display: flex;
  align-items: baseline;
  gap: 10px;
}
.credit-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
/* 四档用颜色分开，但不做成红绿灯：C 不是「危险」，是「要盯着」。 */
.credit-grade {
  font-size: 30px;
  line-height: 1;
  font-weight: 700;
}
.credit-grade.is-inline {
  font-size: 16px;
}
.credit-grade.is-a { color: var(--el-color-success); }
.credit-grade.is-b { color: var(--el-color-primary); }
.credit-grade.is-c { color: var(--el-color-warning); }
.credit-grade.is-d { color: var(--el-color-danger); }
.credit-none {
  font-size: 15px;
  color: var(--el-text-color-placeholder);
}
.credit-age {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.credit-age.is-stale {
  color: var(--el-color-warning);
  font-weight: 600;
}
.credit-dims {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 10px;
  padding: 12px 14px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
  background: var(--el-fill-color-blank);
}
.credit-dim {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.dim-name {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.dim-value {
  font-size: 15px;
  font-variant-numeric: tabular-nums;
}
.dim-value.is-empty {
  font-size: 13px;
  color: var(--el-text-color-placeholder);
}
.credit-history {
  padding-left: 4px;
}
.credit-move {
  display: flex;
  align-items: baseline;
  gap: 6px;
}
.credit-from {
  color: var(--el-text-color-placeholder);
  text-decoration: line-through;
}
.credit-arrow {
  color: var(--el-text-color-placeholder);
}
.credit-by {
  margin-left: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.credit-basis {
  margin: 4px 0 0;
  color: var(--el-text-color-regular);
}
.credit-evidence {
  margin: 2px 0 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.form-note {
  margin-top: 4px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--el-text-color-secondary);
}
</style>
