<template>
  <div class="manager-trend" role="group" :aria-label="label">
    <div class="trend-legend">
      <span v-for="(name, index) in series" :key="name" class="legend-item">
        <i :class="['trend-swatch', `series-${index}`]" aria-hidden="true"></i>{{ name }}
      </span>
    </div>
    <div class="trend-overflow">
      <div class="trend-layout" :style="{ '--periods': points.length }">
        <div class="trend-axis" aria-hidden="true">
          <span v-for="tick in ticks" :key="tick">{{ tick }}</span>
        </div>
        <div class="trend-stations">
          <button
            v-for="point in points"
            :key="point.key"
            type="button"
            class="trend-station"
            :class="{ selected: point.key === selected }"
            :aria-label="pointLabel(point)"
            :aria-pressed="point.key === selected"
            :title="pointLabel(point)"
            @click="$emit('select', point.key)"
          >
            <span class="trend-bars" aria-hidden="true">
              <span v-for="(value, index) in point.values" :key="index" class="trend-lane">
                <span v-if="value === null" class="empty-value">—</span>
                <span
                  v-else-if="value > 0"
                  :class="['trend-bar', `series-${index}`]"
                  :style="{ height: `${value / chartMax * 100}%` }"
                ><span class="bar-value">{{ value }}</span></span>
              </span>
            </span>
            <span class="trend-label">{{ point.shortLabel }}</span>
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

interface TrendPoint {
  key: string
  label: string
  shortLabel: string
  values: (number | null)[]
}

const props = defineProps<{
  label: string
  series: string[]
  points: TrendPoint[]
  selected: string
}>()
defineEmits<{ select: [key: string] }>()

const step = computed(() => {
  const peak = Math.max(0, ...props.points.flatMap(point => point.values.map(value => value ?? 0)))
  const target = Math.max(1, peak / 3)
  const power = 10 ** Math.floor(Math.log10(target))
  const multiple = [1, 2, 5, 10].find(value => value * power >= target) ?? 10
  return multiple * power
})
const chartMax = computed(() => step.value * 3)
const ticks = computed(() => [chartMax.value, step.value * 2, step.value, 0])

function pointLabel(point: TrendPoint) {
  const values = props.series.map((name, index) => `${name} ${point.values[index] ?? '—'}`)
  return [point.label, ...values].join(' · ')
}
</script>

<style scoped>
.manager-trend {
  --plot-height: 116px;
  --axis-label-height: 29px;
  --series-first: #3b9bd0;
  --series-second: #3b9a7e;
  padding: 0 14px 5px;
}
.trend-legend { display: flex; gap: 16px; margin: 0 0 10px; color: #526575; font-size: 12px; }
.legend-item { display: inline-flex; gap: 6px; align-items: center; }
.trend-swatch { width: 9px; height: 9px; border-radius: 2px; }
.series-0 { background: var(--series-first); }
.series-1 { background: var(--series-second); }
.trend-overflow { overflow-x: auto; padding-top: 14px; }
.trend-layout {
  min-width: max(0px, calc(var(--periods) * 56px + 44px));
  display: grid;
  grid-template-columns: minmax(36px, auto) minmax(0, 1fr);
}
.trend-axis {
  height: calc(var(--plot-height) + 11px);
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  align-items: end;
  padding-right: 10px;
  color: #60717c;
  font-size: 11px;
  line-height: 1;
  font-variant-numeric: tabular-nums;
  transform: translateY(-5.5px);
}
.trend-stations {
  display: grid;
  grid-template-columns: repeat(var(--periods), minmax(0, 1fr));
  background: repeating-linear-gradient(to bottom, #e1ebf1 0 1px, transparent 1px calc(var(--plot-height) / 3))
    0 0 / 100% calc(var(--plot-height) + 1px) no-repeat;
}
.trend-station {
  display: grid;
  grid-template-rows: var(--plot-height) var(--axis-label-height);
  min-width: 0;
  min-height: 0;
  padding: 0;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: #526575;
  cursor: pointer;
  font: inherit;
}
.trend-station:hover { background: #ecf7fc9c; }
.trend-station.selected { background: #e6f4fdc2; color: #126595; }
.trend-station:focus-visible { outline: 2px solid #1680bb; outline-offset: -2px; }
.trend-bars {
  height: var(--plot-height);
  display: flex;
  justify-content: center;
  align-items: stretch;
  gap: 4px;
  border-bottom: 1px solid #cfdee8;
}
.trend-lane { width: min(22%, 22px); height: 100%; display: flex; justify-content: center; align-items: end; }
.trend-bar { width: 100%; min-height: 2px; border-radius: 4px 4px 0 0; position: relative; }
.bar-value {
  position: absolute;
  bottom: calc(100% + 3px);
  left: 50%;
  transform: translateX(-50%);
  color: #2e4557;
  font: 600 10px/1 sans-serif;
  font-variant-numeric: tabular-nums;
}
.empty-value { font-size: 12px; padding-bottom: 3px; color: #60717c; }
.trend-label { align-self: center; font-size: 11px; font-variant-numeric: tabular-nums; white-space: nowrap; }
</style>
