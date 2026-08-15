<template>
  <!-- The tenant's mail host connection settings — one config for the whole
       company, edited by an administrator, reached from the 邮箱 page. These
       are the values off the mail host's own console (263, Gmail, ...). -->
  <el-dialog
    :model-value="modelValue"
    :title="t('mailbox.host')"
    width="560px"
    @update:model-value="emit('update:modelValue', $event)"
    @open="load"
  >
    <p class="hint scope">{{ t('mailbox.hostScope') }}</p>
    <!-- Nobody should type imap.263.net by hand — the same idea as a desktop
         mail client's provider list, but for the one person here who ever
         sees these fields: the administrator. A preset fills the connection
         values only; the domain and the quotas stay the company's own. -->
    <div v-if="canEditHost" class="preset-row">
      <span class="hint">{{ t('mailbox.presets') }}</span>
      <el-button
        v-for="p in PRESETS"
        :key="p.key"
        size="small"
        plain
        @click="applyPreset(p)"
      >
        {{ t(`mailbox.presetNames.${p.key}`) }}
      </el-button>
    </div>
    <el-form label-width="130px" :disabled="!canEditHost">
      <el-form-item :label="t('mailbox.domain')">
        <el-input v-model="host.domain" placeholder="sunrise.com" />
      </el-form-item>
      <el-form-item :label="t('mailbox.smtpHost')" required>
        <el-input v-model="host.smtpHost" placeholder="smtp.263.net" />
      </el-form-item>
      <el-form-item :label="t('mailbox.smtpPort')">
        <el-input-number v-model="host.smtpPort" :min="1" :max="65535" controls-position="right" />
        <el-select v-model="host.smtpSecurity" style="width: 130px; margin-left: 10px">
          <el-option label="SSL" value="SSL" />
          <el-option label="STARTTLS" value="STARTTLS" />
          <el-option label="NONE" value="NONE" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('mailbox.imapHost')">
        <el-input v-model="host.imapHost" placeholder="imap.263.net" />
      </el-form-item>
      <el-form-item :label="t('mailbox.imapPort')">
        <el-input-number v-model="host.imapPort" :min="1" :max="65535" controls-position="right" />
        <el-select v-model="host.imapSecurity" style="width: 130px; margin-left: 10px">
          <el-option label="SSL" value="SSL" />
          <el-option label="STARTTLS" value="STARTTLS" />
          <el-option label="NONE" value="NONE" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('mailbox.hourlyQuota')">
        <el-input-number v-model="host.hourlyQuota" :min="1" controls-position="right" />
        <span class="hint inline">{{ t('mailbox.quotaNote') }}</span>
      </el-form-item>
      <el-form-item :label="t('mailbox.dailyQuota')">
        <el-input-number v-model="host.dailyQuota" :min="1" controls-position="right" />
      </el-form-item>
    </el-form>
    <div v-if="!canEditHost" class="hint">{{ t('mailbox.hostReadOnly') }}</div>
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">{{ t('common.cancel') }}</el-button>
      <el-button v-if="canEditHost" type="primary" :loading="saving" @click="save">
        {{ t('common.save') }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { get, put } from '../api'
import { useAuthStore } from '../stores/auth'

defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const { t } = useI18n()
const auth = useAuthStore()

// Editing the host is administrative — getting it wrong stops the whole
// company sending. Others can look but not touch.
const canEditHost = auth.can('iam:role:write')

const host = reactive({
  domain: '',
  smtpHost: '',
  smtpPort: 465,
  smtpSecurity: 'SSL',
  imapHost: '',
  imapPort: 993,
  imapSecurity: 'SSL',
  hourlyQuota: 100,
  dailyQuota: 500,
})
const saving = ref(false)

async function load() {
  const d = await get<{ host: typeof host }>('/mail-host')
  Object.assign(host, d.host ?? {})
}

async function save() {
  saving.value = true
  try {
    await put('/mail-host', { host })
    ElMessage.success(t('mailbox.saved'))
    emit('update:modelValue', false)
  } finally {
    saving.value = false
  }
}

// The values off each provider's own published settings page, verbatim.
// Every entry a Chinese trading company plausibly lands on, plus Gmail —
// still the fastest way to prove the pipeline before a company mailbox
// exists (app password or Google sign-in, never the account password).
const PRESETS = [
  { key: 'p263', smtpHost: 'smtp.263.net', smtpPort: 465, smtpSecurity: 'SSL', imapHost: 'imap.263.net', imapPort: 993, imapSecurity: 'SSL' },
  { key: 'tencent', smtpHost: 'smtp.exmail.qq.com', smtpPort: 465, smtpSecurity: 'SSL', imapHost: 'imap.exmail.qq.com', imapPort: 993, imapSecurity: 'SSL' },
  { key: 'ali', smtpHost: 'smtp.qiye.aliyun.com', smtpPort: 465, smtpSecurity: 'SSL', imapHost: 'imap.qiye.aliyun.com', imapPort: 993, imapSecurity: 'SSL' },
  // NetEase enterprise publishes 994 for SMTP over SSL, not the usual 465.
  { key: 'netease', smtpHost: 'smtp.qiye.163.com', smtpPort: 994, smtpSecurity: 'SSL', imapHost: 'imap.qiye.163.com', imapPort: 993, imapSecurity: 'SSL' },
  { key: 'gmail', smtpHost: 'smtp.gmail.com', smtpPort: 587, smtpSecurity: 'STARTTLS', imapHost: 'imap.gmail.com', imapPort: 993, imapSecurity: 'SSL' },
] as const

function applyPreset(p: (typeof PRESETS)[number]) {
  const { key: _key, ...fields } = p
  Object.assign(host, fields)
}
</script>

<style scoped>
.hint {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
.hint.inline {
  margin-left: 10px;
}
.hint.scope {
  margin: 0 0 12px;
}
.preset-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin: 0 0 14px;
}
</style>
