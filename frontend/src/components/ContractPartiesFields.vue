<template>
  <section class="contract-parties">
    <header><strong>签约双方</strong><span>草稿可暂缺，提交上级确认前须填写完整</span></header>
    <el-form label-position="top" class="parties-grid">
      <el-form-item label="买方抬头"><el-input :model-value="modelValue.buyerName" placeholder="按合同填写买方完整名称" @update:model-value="update('buyerName',$event)" /></el-form-item>
      <el-form-item label="卖方抬头"><el-input :model-value="modelValue.sellerName" placeholder="按合同填写实际签约卖方完整名称" @update:model-value="update('sellerName',$event)" /></el-form-item>
      <el-form-item label="买方合同地址"><el-input :model-value="modelValue.buyerAddress" type="textarea" :rows="2" placeholder="核对并填写合同中的买方地址" @update:model-value="update('buyerAddress',$event)" /></el-form-item>
      <el-form-item label="卖方合同地址"><el-input :model-value="modelValue.sellerAddress" type="textarea" :rows="2" placeholder="核对并填写合同中的卖方地址" @update:model-value="update('sellerAddress',$event)" /></el-form-item>
    </el-form>
  </section>
</template>
<script setup lang="ts">
import type {ContractParties} from '../lib/contractParties'
const props=defineProps<{modelValue:ContractParties}>()
const emit=defineEmits<{'update:modelValue':[ContractParties]}>()
function update(key:keyof ContractParties,value:string){emit('update:modelValue',{...props.modelValue,[key]:value})}
</script>
<style scoped>
.contract-parties{margin:14px 0 18px;padding:16px 18px 0;background:#f6fafc;border:1px solid #dce7ed;border-radius:6px}
header{display:flex;align-items:baseline;gap:14px;margin-bottom:14px}header span{font-size:12px;color:#64748b}
.parties-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));column-gap:18px}
@media(max-width:600px){header{align-items:flex-start;flex-direction:column;gap:6px}.parties-grid{grid-template-columns:1fr}}
</style>
