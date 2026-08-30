<template>
  <div class="shipping-sourcing-page">
    <header>
      <div><small>PRE-SALES SHIPPING SOURCING</small><h1>售前船运询价</h1><p>共享案件、多人询价、货物报价组和船运经理统一方案。</p></div>
      <el-button @click="router.push('/shipping/schedules')">进入正式船期管理</el-button>
    </header>
    <SourcingShippingPanel />
  </div>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router'
import SourcingShippingPanel from '../components/SourcingShippingPanel.vue'
const router = useRouter()
</script>

<style scoped>
.shipping-sourcing-page{max-width:1500px;margin:0 auto;padding:22px}.shipping-sourcing-page>header{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;margin-bottom:22px;padding:20px 22px;border:1px solid #dce8e5;border-radius:12px;background:linear-gradient(135deg,#f3faf8,#fff)}small{color:#168274;font-weight:700;letter-spacing:.09em}h1{margin:6px 0;color:#172b4d;font-size:27px}p{margin:0;color:#6b778c}@media(max-width:760px){.shipping-sourcing-page{padding:12px}.shipping-sourcing-page>header{flex-direction:column}}
</style>
