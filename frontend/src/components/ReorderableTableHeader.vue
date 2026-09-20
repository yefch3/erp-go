<template>
  <span class="reorderable-table-header" :title="hint">
    <button v-if="canMoveLeft" type="button" class="column-move column-move--left" :aria-label="`${label} ${moveLeftLabel}`" :title="moveLeftLabel" @click.stop="emit('moveLeft')">‹</button>
    <span class="column-reorder-hint" aria-hidden="true">↔</span>
    <span class="column-label">{{ label }}</span>
    <button v-if="canMoveRight" type="button" class="column-move column-move--right" :aria-label="`${label} ${moveRightLabel}`" :title="moveRightLabel" @click.stop="emit('moveRight')">›</button>
  </span>
</template>

<script setup lang="ts">
defineProps<{ label: string; hint: string; moveLeftLabel: string; moveRightLabel: string; canMoveLeft: boolean; canMoveRight: boolean }>()
const emit = defineEmits<{ moveLeft: []; moveRight: [] }>()
</script>

<style scoped>
.reorderable-table-header{position:relative;display:inline-flex;align-items:center;justify-content:center;gap:4px;max-width:100%;min-height:26px;padding:0 17px;user-select:none}.column-label{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.column-reorder-hint{color:#8197a1;font-size:10px;font-weight:400;opacity:.28;transition:opacity .15s ease}.column-move{position:absolute;top:50%;display:grid;width:20px;height:22px;padding:0;border:1px solid #cfe0e6;border-radius:5px;background:#fff;color:#28768d;font-size:18px;line-height:1;opacity:0;cursor:pointer;pointer-events:none;transform:translateY(-50%);transition:opacity .15s ease,background .15s ease,border-color .15s ease}.column-move--left{left:-2px}.column-move--right{right:-2px}.reorderable-table-header:hover .column-move,.reorderable-table-header:focus-within .column-move{opacity:1;pointer-events:auto}.reorderable-table-header:hover .column-reorder-hint{opacity:.7}.column-move:hover,.column-move:focus-visible{border-color:#69b8cf;background:#eef8fb;outline:none}.column-move:active{background:#dff1f6}
</style>
