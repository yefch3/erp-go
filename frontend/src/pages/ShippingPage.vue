<template>
  <div>
    <div class="page-head"><h2>{{ t('shipping.title') }}</h2><el-button v-if="auth.can('shipping:schedule:write')" type="primary" @click="dialogOpen=true">{{ t('shipping.create') }}</el-button></div>
    <el-row :gutter="14" class="stats">
      <el-col v-for="item in statItems" :key="item.label" :xs="12" :sm="6"><el-card shadow="never"><div class="stat-label">{{item.label}}</div><div class="stat-value">{{item.value}}</div></el-card></el-col>
    </el-row>
    <el-card shadow="never">
      <div class="filters">
        <el-input v-model="filter.keyword" clearable :placeholder="t('shipping.searchPlaceholder')" @keyup.enter="search" @clear="search" />
        <el-select v-model="filter.status" :placeholder="t('shipping.allStatuses')">
          <el-option value="ACTIVE" label="进行中" />
          <el-option value="ARCHIVED" label="历史记录（已完成/已取消）" />
          <el-option value="" label="全部状态" />
          <el-option v-for="s in SHIPPING_STATUSES" :key="s" :value="s" :label="t(`shipping.statuses.${s}`)" />
        </el-select>
        <el-input v-model="filter.portOfLoading" clearable :placeholder="t('shipping.loadingPort')" />
        <el-input v-model="filter.portOfDischarge" clearable :placeholder="t('shipping.dischargePort')" />
        <el-button @click="more=!more">{{ t('shipping.dateFilters') }}</el-button><el-button type="primary" @click="search">{{ t('common.query') }}</el-button>
      </div>
      <div v-if="more" class="date-filters"><span>ETD</span><el-date-picker v-model="filter.etdRange" type="daterange" value-format="YYYY-MM-DD" range-separator="—" /><span>ETA</span><el-date-picker v-model="filter.etaRange" type="daterange" value-format="YYYY-MM-DD" range-separator="—" /></div>
      <el-table :data="rows" v-loading="loading" @row-dblclick="detail">
        <el-table-column :label="t('shipping.scheduleNo')" width="180"><template #default="{row}"><el-link type="primary" @click="detail(row)">{{ row.scheduleNo }}</el-link></template></el-table-column>
        <el-table-column prop="customerName" :label="t('shipping.customer')" min-width="140" />
        <el-table-column :label="t('shipping.vesselVoyage')" min-width="160"><template #default="{row}">{{ row.vesselName }} / {{ row.voyageNo }}</template></el-table-column>
        <el-table-column label="路线" min-width="170"><template #default="{row}">{{row.portOfLoading}} → {{row.portOfDischarge}}</template></el-table-column>
        <el-table-column prop="eta" label="最新ETA" width="115" />
        <el-table-column prop="currentProgress" label="当前进度" min-width="150" />
        <el-table-column label="变化" min-width="160"><template #default="{row}"><el-tag v-if="row.delayDays>0" :type="row.delayDays>=4?'danger':'warning'" class="change-tag">+{{row.delayDays}}天</el-tag><el-tag v-if="row.hasTemporaryCall" type="warning" class="change-tag">临时挂港</el-tag><span v-if="!row.delayDays&&!row.hasTemporaryCall">—</span></template></el-table-column>
        <el-table-column :label="t('common.status')" width="110"><template #default="{row}"><el-tag :type="statusTag(row.status)">{{ t(`shipping.statuses.${row.status}`) }}</el-tag></template></el-table-column>
        <el-table-column prop="responsibleName" :label="t('shipping.responsible')" width="120" />
        <el-table-column v-if="auth.can('shipping:schedule:write')" :label="t('common.actions')" fixed="right" width="90"><template #default="{row}"><el-button link type="primary" :disabled="['COMPLETED','CANCELLED'].includes(row.status)" @click.stop="editing=row;dialogOpen=true">{{ t('common.edit') }}</el-button></template></el-table-column>
      </el-table>
      <el-empty v-if="!loading&&!rows.length" :description="t('shipping.empty')" />
      <el-pagination class="pager" layout="total, sizes, prev, pager, next" :total="total" :page-size="pageSize" :current-page="page" :page-sizes="[10,20,50]" @current-change="changePage" @size-change="changeSize" />
    </el-card>
    <ShippingScheduleDialog v-model="dialogOpen" :schedule="editing" @saved="saved" />
  </div>
</template>
<script setup lang="ts">
import { computed,onMounted,reactive,ref,watch } from 'vue';import { useI18n } from 'vue-i18n';import { useRouter } from 'vue-router'
import { get } from '../api';import ShippingScheduleDialog from '../components/ShippingScheduleDialog.vue';import { SHIPPING_STATUSES,statusTag,type ShippingSchedule,type ShippingStatistics } from '../shipping';import { useAuthStore } from '../stores/auth'
const {t}=useI18n();const router=useRouter();const auth=useAuthStore();const rows=ref<ShippingSchedule[]>([]);const loading=ref(false);const total=ref(0);const page=ref(1);const pageSize=ref(20);const more=ref(false);const dialogOpen=ref(false);const editing=ref<ShippingSchedule>()
const filter=reactive({keyword:'',status:'ACTIVE',portOfLoading:'',portOfDischarge:'',etdRange:[] as string[],etaRange:[] as string[]})
const stats=ref<ShippingStatistics>({inTransit:'0',arrivingWithin7Days:'0',delayed:'0',temporaryCall:'0'});const statItems=computed(()=>[{label:'运输中',value:stats.value.inTransit},{label:'7天内到港',value:stats.value.arrivingWithin7Days},{label:'已延误',value:stats.value.delayed},{label:'临时挂港',value:stats.value.temporaryCall}])
watch(dialogOpen,v=>{if(!v)editing.value=undefined})
async function load(){loading.value=true;try{const [data,summary]=await Promise.all([get<{schedules:ShippingSchedule[];meta:{total:string}}>('/shipping/schedules',{page:page.value,page_size:pageSize.value,keyword:filter.keyword,status:filter.status,port_of_loading:filter.portOfLoading,port_of_discharge:filter.portOfDischarge,etd_from:filter.etdRange?.[0]??'',etd_to:filter.etdRange?.[1]??'',eta_from:filter.etaRange?.[0]??'',eta_to:filter.etaRange?.[1]??''}),get<ShippingStatistics>('/shipping/statistics')]);rows.value=data.schedules;total.value=Number(data.meta.total);stats.value=summary}finally{loading.value=false}}
function search(){page.value=1;load()}function changePage(v:number){page.value=v;load()}function changeSize(v:number){pageSize.value=v;page.value=1;load()}function detail(row:ShippingSchedule){router.push(`/shipping/${row.id}`)}function saved(){load()}onMounted(load)
</script>
<style scoped>.page-head{display:flex;align-items:center;justify-content:space-between;margin-bottom:16px}.page-head h2{margin:0}.stats{margin-bottom:14px}.stat-label{color:var(--el-text-color-secondary);font-size:13px}.stat-value{font-size:26px;font-weight:600;margin-top:6px}.filters,.date-filters{display:flex;gap:10px;margin-bottom:14px;align-items:center}.filters .el-input{width:190px}.filters .el-select{width:220px}.pager{margin-top:14px;justify-content:flex-end}.change-tag{margin-right:5px}</style>
