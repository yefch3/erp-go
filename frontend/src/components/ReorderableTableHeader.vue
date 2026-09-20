<template>
  <span class="reorderable-table-header">
    <button v-if="canMoveLeft" type="button" class="column-move column-move--left" :aria-label="`${label} ${moveLeftLabel}`" :title="moveLeftLabel" @click.stop="emit('moveLeft')"><el-icon><ArrowLeft /></el-icon></button>
    <span class="column-label">{{ label }}</span>
    <button v-if="canMoveRight" type="button" class="column-move column-move--right" :aria-label="`${label} ${moveRightLabel}`" :title="moveRightLabel" @click.stop="emit('moveRight')"><el-icon><ArrowRight /></el-icon></button>
  </span>
</template>

<script setup lang="ts">
import { ArrowLeft, ArrowRight } from '@element-plus/icons-vue'

defineProps<{ label: string; hint: string; moveLeftLabel: string; moveRightLabel: string; canMoveLeft: boolean; canMoveRight: boolean }>()
const emit = defineEmits<{ moveLeft: []; moveRight: [] }>()
</script>

<style scoped>
.reorderable-table-header{position:relative;display:flex;box-sizing:border-box;align-items:center;justify-content:center;width:100%;max-width:100%;min-height:30px;padding:0 30px;border-radius:6px;user-select:none;transition:background-color .15s ease,box-shadow .15s ease}.column-label{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.column-move{position:absolute;z-index:1;top:50%;display:grid;box-sizing:border-box;width:24px;height:24px;padding:0;border:1px solid #d4e5f2;border-radius:50%;background:#fff;color:#236c9d;box-shadow:0 2px 6px rgb(30 91 128 / 12%);opacity:0;cursor:pointer;pointer-events:none;place-items:center;transform:translateY(-50%) scale(.92);transition:opacity .15s ease,transform .15s ease,background-color .15s ease,border-color .15s ease}.column-move .el-icon{font-size:13px}.column-move--left{left:4px}.column-move--right{right:4px}.reorderable-table-header:hover,.reorderable-table-header:focus-within{background:#edf6ff;box-shadow:inset 0 0 0 1px #d3e8f8}.reorderable-table-header:hover .column-move,.reorderable-table-header:focus-within .column-move{opacity:1;pointer-events:auto;transform:translateY(-50%) scale(1)}.column-move:hover,.column-move:focus-visible{border-color:#8fc6e8;background:#f5fbff;outline:none}.column-move:active{background:#dfeffa}
</style>
