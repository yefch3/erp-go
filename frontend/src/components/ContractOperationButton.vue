<template>
 <span class="contract-operation">
  <el-tooltip :content="disabled && unavailable ? `${description} 当前不可用：${unavailable}` : description" placement="top" :show-after="250">
   <span tabindex="0"><el-button :type="type" :disabled="disabled" :loading="loading" size="small" @click="$emit('click')">{{label}}</el-button></span>
  </el-tooltip>

 </span>
</template>
<script setup lang="ts">
defineProps<{label:string;description:string;disabled?:boolean;unavailable?:string;loading?:boolean;type?:'primary'|'success'|'warning'|'danger'|'info'}>()
defineEmits<{click:[]}>()
</script>
<style scoped>
.contract-operation{display:inline-flex;align-items:center}
.contract-operation > span:focus-visible{outline:2px solid #409eff;outline-offset:3px;border-radius:4px}
</style>
