<template>
  <!-- Hand-drawn SVG rather than a charting library. One chart does not
       justify ~300KB of echarts in a bundle whose whole dependency list is
       seven packages, and a line with a hover readout is a hundred lines of
       geometry. It also inherits the theme for free: every colour here is a
       CSS variable, so light/dark follow the app instead of the library. -->
  <figure class="rate-chart" :class="{ empty: points.length < 2 }">
    <figcaption class="head">
      <div class="headline">
        <span class="pair">1 USD =</span>
        <strong class="value">{{ latestValue }}</strong>
        <span class="pair">{{ currency }}</span>
        <span v-if="changePct !== null" class="delta" :class="changeClass">
          {{ changePct >= 0 ? '▲' : '▼' }} {{ Math.abs(changePct).toFixed(2) }}%
        </span>
      </div>
      <p class="span-note">{{ spanNote }}</p>
    </figcaption>

    <p v-if="points.length < 2" class="thin">{{ t('fx.chartThin') }}</p>

    <svg
      v-else
      class="plot"
      :viewBox="`0 0 ${W} ${H}`"
      preserveAspectRatio="none"
      role="img"
      :aria-label="t('fx.chartAria', { currency, n: points.length })"
      @pointermove="onMove"
      @pointerleave="hover = null"
    >
      <defs>
        <linearGradient :id="gradientId" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" class="fill-top" />
          <stop offset="100%" class="fill-bottom" />
        </linearGradient>
      </defs>

      <!-- Three guides, not a grid: the eye needs the top, the bottom and
           the middle of the range, and anything more competes with the line
           it is there to support. -->
      <line v-for="g in guides" :key="g" class="guide" x1="0" :y1="g" :x2="W" :y2="g" />

      <path class="area" :d="areaPath" :fill="`url(#${gradientId})`" />
      <path class="line" :d="linePath" />

      <!-- The most recent point is the one people look for, so it is the one
           that is always marked. -->
      <circle class="last-dot" :cx="lastX" :cy="lastY" r="3.5" />

      <template v-if="hover">
        <line class="cursor" :x1="hover.x" y1="0" :x2="hover.x" :y2="H" />
        <circle class="cursor-dot" :cx="hover.x" :cy="hover.y" r="4" />
      </template>
    </svg>

    <!-- The readout sits outside the SVG so it can use ordinary text
         rendering and wrap like text; an SVG <text> tooltip would need its
         own box maths and would not respect the app's font settings. -->
    <div v-if="hover" class="readout" :style="{ left: readoutLeft }">
      <span class="readout-date">{{ hover.date }}</span>
      <span class="readout-value">{{ hover.label }}</span>
    </div>

    <div v-if="points.length >= 2" class="axis">
      <span>{{ points[0].date }}</span>
      <span class="axis-range">{{ t('fx.chartLow') }} {{ lowLabel }} · {{ t('fx.chartHigh') }} {{ highLabel }}</span>
      <span>{{ points[points.length - 1].date }}</span>
    </div>
  </figure>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

export interface ChartPoint {
  date: string
  value: number
}

const props = defineProps<{
  points: ChartPoint[]
  currency: string
}>()

const { t } = useI18n()

// A fixed drawing space stretched to whatever width the card gives it. The
// alternative — measuring the element — buys nothing here: the shape is the
// information, and the y scale is data-driven either way.
const W = 720
const H = 200
// Room for the line's own stroke and the last dot at the extremes.
const PAD = 8

// Unique per instance so two charts on one page cannot share a gradient.
const gradientId = `rate-fill-${Math.random().toString(36).slice(2, 9)}`

const values = computed(() => props.points.map((p) => p.value))
const low = computed(() => Math.min(...values.value))
const high = computed(() => Math.max(...values.value))

// A flat series would divide by zero and, worse, draw a line pinned to the
// top of the box. Giving a degenerate range a nominal width puts it in the
// middle, which is what "it did not move" should look like.
const span = computed(() => (high.value - low.value) || Math.max(high.value * 0.001, 1e-8))

function x(i: number): number {
  if (props.points.length < 2) return W / 2
  return PAD + (i * (W - PAD * 2)) / (props.points.length - 1)
}
function y(v: number): number {
  return H - PAD - ((v - low.value) / span.value) * (H - PAD * 2)
}

const linePath = computed(() =>
  props.points.map((p, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(2)},${y(p.value).toFixed(2)}`).join(' '),
)
const areaPath = computed(() => {
  if (props.points.length < 2) return ''
  return `${linePath.value} L${x(props.points.length - 1).toFixed(2)},${H} L${x(0).toFixed(2)},${H} Z`
})
const guides = computed(() => [PAD, H / 2, H - PAD])

const lastX = computed(() => x(props.points.length - 1))
const lastY = computed(() => y(values.value[values.value.length - 1] ?? 0))

// Rates are quoted to four or five decimals; JPY needs fewer, and a rate
// under 1 needs more to say anything at all. One rule, applied to whatever
// the feed publishes.
function fmt(v: number): string {
  if (!isFinite(v)) return '—'
  if (v >= 100) return v.toFixed(2)
  if (v >= 1) return v.toFixed(4)
  return v.toFixed(6)
}

const latestValue = computed(() => fmt(values.value[values.value.length - 1] ?? NaN))
const lowLabel = computed(() => fmt(low.value))
const highLabel = computed(() => fmt(high.value))

const changePct = computed(() => {
  if (props.points.length < 2) return null
  const first = values.value[0]
  const last = values.value[values.value.length - 1]
  if (!first) return null
  return ((last - first) / first) * 100
})
// Up and down are not good and bad here: a rising USD/CNY is good news for
// an exporter and bad for an importer. Neutral naming, and colour chosen for
// direction only.
const changeClass = computed(() => (changePct.value === null ? '' : changePct.value >= 0 ? 'up' : 'down'))

const spanNote = computed(() => {
  if (props.points.length < 2) return ''
  return t('fx.chartSpan', {
    from: props.points[0].date,
    to: props.points[props.points.length - 1].date,
    n: props.points.length,
  })
})

const hover = ref<{ x: number; y: number; date: string; label: string; ratio: number } | null>(null)

// The readout is a fact about the series under it. Switching currency or
// range replaces that series while the pointer has not moved, and a readout
// left behind would sit on the new curve quoting the old one's number —
// seen in testing as JPY's chart captioned with CNY's rate.
watch(() => [props.currency, props.points], () => { hover.value = null })

function onMove(e: PointerEvent) {
  if (props.points.length < 2) return
  const box = (e.currentTarget as SVGSVGElement).getBoundingClientRect()
  if (box.width === 0) return
  // The viewBox is stretched to the element, so the pointer's fraction of
  // the width is the same fraction of the drawing space.
  const ratio = Math.min(1, Math.max(0, (e.clientX - box.left) / box.width))
  const i = Math.round(ratio * (props.points.length - 1))
  const p = props.points[i]
  hover.value = { x: x(i), y: y(p.value), date: p.date, label: fmt(p.value), ratio: i / (props.points.length - 1) }
}

// Keeps the readout inside the card at both ends instead of hanging off it.
const readoutLeft = computed(() => `${Math.min(88, Math.max(12, (hover.value?.ratio ?? 0) * 100))}%`)
</script>

<style scoped>
.rate-chart {
  position: relative;
  margin: 0;
}
.head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.headline {
  display: flex;
  align-items: baseline;
  gap: 6px;
}
.pair {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.value {
  font-size: 26px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  letter-spacing: -0.01em;
}
.delta {
  margin-left: 6px;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  padding: 1px 7px;
  border-radius: 999px;
  background: var(--el-fill-color-light);
}
.delta.up {
  color: var(--el-color-danger);
}
.delta.down {
  color: var(--el-color-success);
}
.span-note {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.plot {
  display: block;
  width: 100%;
  height: 200px;
  overflow: visible;
  touch-action: none;
}
.guide {
  stroke: var(--el-border-color-lighter);
  stroke-width: 1;
  vector-effect: non-scaling-stroke;
}
.line {
  fill: none;
  stroke: var(--el-color-primary);
  stroke-width: 2;
  stroke-linejoin: round;
  stroke-linecap: round;
  /* The viewBox is stretched horizontally, which would otherwise stretch the
     stroke with it and give a line thicker at one axis than the other. */
  vector-effect: non-scaling-stroke;
}
.fill-top {
  stop-color: var(--el-color-primary);
  stop-opacity: 0.18;
}
.fill-bottom {
  stop-color: var(--el-color-primary);
  stop-opacity: 0;
}
.last-dot {
  fill: var(--el-color-primary);
  stroke: var(--el-bg-color);
  stroke-width: 2;
  vector-effect: non-scaling-stroke;
}
.cursor {
  stroke: var(--el-text-color-secondary);
  stroke-width: 1;
  stroke-dasharray: 3 3;
  vector-effect: non-scaling-stroke;
}
.cursor-dot {
  fill: var(--el-bg-color);
  stroke: var(--el-color-primary);
  stroke-width: 2;
  vector-effect: non-scaling-stroke;
}
.readout {
  position: absolute;
  top: 44px;
  transform: translateX(-50%);
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 6px 10px;
  border-radius: 6px;
  background: var(--el-bg-color-overlay);
  border: 1px solid var(--el-border-color-light);
  box-shadow: var(--el-box-shadow-light);
  pointer-events: none;
  font-size: 12px;
  white-space: nowrap;
}
.readout-date {
  color: var(--el-text-color-secondary);
}
.readout-value {
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}
.axis {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  font-variant-numeric: tabular-nums;
}
.axis-range {
  text-align: center;
}
.thin {
  margin: 24px 0;
  text-align: center;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
</style>
