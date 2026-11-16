<template>
  <section class="purchase-order-remark" :aria-label="t('purchaseOrderContext.remark')">
    <h4>{{ t('purchaseOrderContext.remark') }}</h4>
    <p>{{ remark?.trim() ? remark : t('purchaseOrderContext.emptyRemark') }}</p>
    <small>{{ t('purchaseOrderContext.remarkHint') }}</small>
  </section>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ remark?: string }>()
const { t } = useI18n()
</script>
<style scoped>
.purchase-order-remark{margin:12px 0 16px;padding:14px 16px;border:1px solid #cfe2eb;border-radius:8px;background:#f5fafc}
h4{margin:0 0 8px;color:#244b60;font-size:14px}
p{margin:0;color:#243f50;white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.65}
small{display:block;margin-top:8px;color:#718390;line-height:1.5}
</style>
