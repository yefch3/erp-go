<template>
  <span
    class="draggable-table-header"
    :class="{ 'is-dragging': dragging }"
    draggable="true"
    :title="hint"
    @dragstart="emit('start', $event)"
    @dragover.prevent
    @drop="emit('drop', $event)"
    @dragend="emit('end')"
  ><span class="drag-grip" aria-hidden="true">⋮⋮</span><span>{{ label }}</span></span>
</template>

<script setup lang="ts">
defineProps<{ label: string; hint: string; dragging?: boolean }>()
const emit = defineEmits<{ start: [event: DragEvent]; drop: [event: DragEvent]; end: [] }>()
</script>

<style scoped>
.draggable-table-header{display:inline-flex;align-items:center;gap:5px;max-width:100%;cursor:grab;user-select:none}.draggable-table-header:active{cursor:grabbing}.drag-grip{color:#92a5ae;font-size:12px;letter-spacing:-4px;opacity:.7;transform:rotate(90deg)}.draggable-table-header:hover .drag-grip{color:#159fdc;opacity:1}.is-dragging{opacity:.45}
</style>
