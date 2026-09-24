<template>
  <div class="trend-chart">
    <div v-if="!dates.length" class="empty">{{ emptyLabel }}</div>
    <template v-else>
      <svg viewBox="0 0 900 270" preserveAspectRatio="none" role="img" :aria-label="title" @pointermove="move" @pointerleave="hoverDate = ''">
        <line v-for="step in 5" :key="step" x1="42" :y1="20+(step-1)*50" x2="890" :y2="20+(step-1)*50" stroke="#e6eef4" />
        <text v-for="step in 5" :key="`label-${step}`" x="2" :y="24+(step-1)*50" fill="#73889a" font-size="11">{{ axisValue(step) }}</text>
        <polyline v-for="line in lines" :key="line.name" :points="points(line)" fill="none" :stroke="line.color" stroke-width="2.5" stroke-linejoin="round" stroke-linecap="round" />
        <template v-for="line in lines" :key="`dots-${line.name}`">
          <circle v-for="point in line.points" :key="`${line.name}-${point.date}`" :cx="x(point.date)" :cy="y(point.value)" r="3.5" :fill="line.color" />
        </template>
        <line v-if="hoverDate" :x1="x(hoverDate)" y1="20" :x2="x(hoverDate)" y2="220" stroke="#8caec0" stroke-dasharray="4 4" />
        <text x="42" y="255" fill="#73889a" font-size="11">{{ dates[0] }}</text>
        <text x="890" y="255" fill="#73889a" font-size="11" text-anchor="end">{{ dates[dates.length-1] }}</text>
      </svg>
      <div v-if="hoverDate" class="tooltip"><strong>{{ hoverDate }}</strong><span v-for="line in hoverLines" :key="line.name"><i :style="{background:line.color}" />{{ line.name }}: {{ line.value }}<small v-if="line.change"> {{ line.change }}</small></span></div>
      <div class="legend"><span v-for="line in lines" :key="line.name"><i :style="{background:line.color}" />{{ line.name }}</span></div>
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
export interface TrendPoint { date: string; value: number; change?: string }
export interface TrendLine { name: string; color: string; points: TrendPoint[] }
const props = defineProps<{ title: string; lines: TrendLine[]; emptyLabel: string }>()
const hoverDate = ref('')
const dates = computed(() => [...new Set(props.lines.flatMap(line => line.points.map(p => p.date)))].sort())
const values = computed(() => props.lines.flatMap(line => line.points.map(p => p.value)))
const low = computed(() => Math.min(...values.value))
const high = computed(() => Math.max(...values.value))
function x(date: string) { return 42 + (dates.value.indexOf(date) / Math.max(1, dates.value.length-1)) * 848 }
function y(value: number) { return 220 - ((value-low.value) / (high.value-low.value || 1)) * 200 }
function points(line: TrendLine) { return line.points.map(p => `${x(p.date)},${y(p.value)}`).join(' ') }
function axisValue(step: number) { return (high.value - (step-1) * (high.value-low.value)/4).toFixed(2).replace(/\.00$/, '') }
function move(event: PointerEvent) {
  const rect = (event.currentTarget as SVGSVGElement).getBoundingClientRect()
  const position = (event.clientX-rect.left)/rect.width
  const index = Math.min(dates.value.length-1, Math.max(0,Math.round(((position*900-42)/848)*(dates.value.length-1))))
  hoverDate.value=dates.value[index] || ''
}
const hoverLines = computed(() => props.lines.flatMap(line => {
  const point = line.points.find(p=>p.date===hoverDate.value)
  return point ? [{name:line.name,color:line.color,value:point.value,change:point.change}] : []
}))
</script>
<style scoped>
.trend-chart{position:relative;min-height:260px}.trend-chart svg{width:100%;height:270px;display:block}.empty{min-height:230px;display:grid;place-items:center;color:#93a1ae}.legend{display:flex;gap:16px;flex-wrap:wrap;font-size:12px;color:#526578;padding:0 42px}.legend span,.tooltip span{display:flex;align-items:center;gap:5px}.legend i,.tooltip i{display:inline-block;width:9px;height:9px;border-radius:50%}.tooltip{position:absolute;top:8px;right:12px;z-index:2;display:grid;gap:4px;padding:8px 12px;background:#fff;border:1px solid #d7e6f0;border-radius:8px;box-shadow:0 4px 12px #10304516;font-size:12px}.tooltip small{color:#6f8595}
</style>
