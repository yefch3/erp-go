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
      <el-button v-if="canEditHost" link type="primary" @click="fillGmail">
        {{ t('mailbox.presetGmail') }}
      </el-button>
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

// Gmail is the fastest way to prove the pipeline before a company mailbox
// exists. It needs an app password or a Google sign-in, not the account
// password.
function fillGmail() {
  Object.assign(host, {
    smtpHost: 'smtp.gmail.com',
    smtpPort: 587,
    smtpSecurity: 'STARTTLS',
    imapHost: 'imap.gmail.com',
    imapPort: 993,
    imapSecurity: 'SSL',
  })
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
</style>
