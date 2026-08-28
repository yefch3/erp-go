<template>
  <div v-loading="loading" class="profile">
    <div class="page-head">
      <h2>{{ t('profile.title') }}</h2>
    </div>

    <el-card shadow="never">
      <!-- 头像在最上面，因为它是这页唯一「点一下就变」的东西，其余都是表单。 -->
      <div class="avatar-row">
        <el-avatar :size="88" :src="profile.avatarUrl" class="big-avatar">
          {{ (profile.name || '—').slice(0, 1) }}
        </el-avatar>
        <div class="avatar-actions">
          <!-- 原生 input 藏起来、按钮代劳：el-upload 会自己发请求，
               而我们要的是先换预签名地址、再 PUT 到对象存储、最后回调落库。 -->
          <input
            ref="fileInput"
            type="file"
            :accept="acceptTypes"
            class="hidden-input"
            @change="onPick"
          />
          <el-button :loading="uploading" @click="fileInput?.click()">
            {{ t('profile.changeAvatar') }}
          </el-button>
          <el-button v-if="profile.avatarKey" link type="danger" @click="clearAvatar">
            {{ t('profile.removeAvatar') }}
          </el-button>
          <div class="hint">{{ t('profile.avatarHint') }}</div>
        </div>
      </div>

      <el-divider />

      <el-form :model="form" label-width="120px" style="max-width: 560px">
        <!-- 能改的两项在最前面。把可编辑的和只读的分开排，比让人在一列
             灰框里找哪个能点要快。 -->
        <el-form-item :label="t('profile.englishName')">
          <el-input v-model="form.englishName" maxlength="100" show-word-limit />
        </el-form-item>
        <el-form-item :label="t('profile.phone')">
          <el-input v-model="form.phone" maxlength="50" />
        </el-form-item>
        <div class="save-row">
          <el-button type="primary" :loading="saving" @click="save">
            {{ common('save') }}
          </el-button>
        </div>

        <el-divider />

        <!-- 只读的照样显示，不藏起来：员工要能看到系统里记的自己是什么样，
             发现记错了才有得可说。禁用而不是隐藏，配一句去哪儿改。 -->
        <div class="readonly-note">{{ t('profile.readonlyHint') }}</div>
        <el-form-item :label="t('profile.name')">
          <el-input :model-value="profile.name" disabled />
        </el-form-item>
        <el-form-item :label="t('profile.code')">
          <el-input :model-value="profile.code" disabled />
        </el-form-item>
        <el-form-item :label="t('profile.email')">
          <el-input :model-value="profile.email" disabled>
            <template #append>
              <el-tag v-if="profile.emailVerified" type="success" size="small" effect="plain">
                {{ t('profile.emailVerified') }}
              </el-tag>
              <el-tag v-else type="warning" size="small" effect="plain">
                {{ t('profile.emailUnverified') }}
              </el-tag>
            </template>
          </el-input>
        </el-form-item>
        <el-form-item :label="t('profile.department')">
          <el-input :model-value="profile.departmentName" disabled />
        </el-form-item>
        <el-form-item :label="t('profile.position')">
          <el-input :model-value="profile.position || '—'" disabled />
        </el-form-item>
        <el-form-item :label="t('profile.manager')">
          <el-input :model-value="profile.managerName || t('profile.noManager')" disabled />
        </el-form-item>
        <el-form-item :label="t('profile.hireDate')">
          <el-input :model-value="profile.hireDate || '—'" disabled />
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { get, post, put } from '../api'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const common = (k: string) => t(`common.${k}`)
const auth = useAuthStore()

// 后端认这三种。和 services/iam/internal/app/profile.go 的白名单是同一份名单——
// 那边才是真门，这里只是不让人白选一个必定被拒的文件。
const acceptTypes = 'image/jpeg,image/png,image/webp'
// 和后端的 avatarMaxBytes 一致。前端先拦一道，是为了不让人白等一次上传。
const maxBytes = 2 * 1024 * 1024

interface Profile {
  id: string
  code: string
  name: string
  englishName: string
  departmentName: string
  position: string
  email: string
  phone: string
  status: string
  managerName: string
  hireDate: string
  avatarKey: string
  avatarUrl: string
  version: number
  emailVerified: boolean
}

const loading = ref(false)
const saving = ref(false)
const uploading = ref(false)
const fileInput = ref<HTMLInputElement>()
const profile = ref<Partial<Profile>>({})
const form = reactive({ englishName: '', phone: '' })

async function load() {
  loading.value = true
  try {
    const d = await get<{ profile: Profile }>('/me/profile')
    profile.value = d.profile ?? {}
    form.englishName = profile.value.englishName ?? ''
    form.phone = profile.value.phone ?? ''
    // 让顶栏那个小头像跟着变。地址是短命的，所以每次都是现取的，不进缓存。
    auth.avatarUrl = profile.value.avatarUrl ?? ''
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    const d = await put<{ profile: Profile }>('/me/profile', {
      englishName: form.englishName,
      phone: form.phone,
      expectedVersion: profile.value.version,
    })
    profile.value = d.profile ?? {}
    ElMessage.success(t('profile.saved'))
  } finally {
    saving.value = false
  }
}

// 上传三步：换一个短命的直传地址 → PUT 到对象存储 → 回调把 key 落库。
//
// 图片不经过我们的服务器，所以第二步用原生 fetch 而不是项目的 axios 实例——
// 那个实例会给每个请求带上登录 cookie 和 CSRF 头，而这里的目标是对象存储，
// 把我们的凭据发给它既没用也不该。
async function onPick(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  // 先清空，否则连续选同一个文件不会再触发 change。
  input.value = ''
  if (!file) return
  if (!acceptTypes.split(',').includes(file.type)) {
    ElMessage.warning(t('profile.avatarType'))
    return
  }
  if (file.size > maxBytes) {
    ElMessage.warning(t('profile.avatarTooLarge'))
    return
  }
  uploading.value = true
  try {
    const p = await post<{ fileKey: string; uploadUrl: string }>('/me/avatar/presign', {
      fileName: file.name,
      contentType: file.type,
    })
    const uploaded = await fetch(p.uploadUrl, {
      method: 'PUT',
      body: file,
      headers: { 'Content-Type': file.type },
    })
    if (!uploaded.ok) {
      // 说清楚是哪一步断的。「保存失败」在这条链路上有三种可能，
      // 而能修的人需要知道是哪一种。
      ElMessage.error(t('profile.avatarUploadFailed', { status: uploaded.status }))
      return
    }
    await post('/me/avatar', { fileKey: p.fileKey })
    await load()
    ElMessage.success(t('profile.avatarSaved'))
  } finally {
    uploading.value = false
  }
}

async function clearAvatar() {
  await ElMessageBox.confirm(t('profile.removeAvatarAsk'), t('profile.removeAvatar'), {
    type: 'warning',
  })
  await post('/me/avatar', { fileKey: '' })
  await load()
  ElMessage.success(t('profile.avatarRemoved'))
}

onMounted(load)
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.avatar-row {
  display: flex;
  align-items: center;
  gap: 20px;
}

.big-avatar {
  flex: none;
  font-size: 32px;
  background: var(--el-color-primary-light-7);
  color: var(--el-color-primary);
}

.avatar-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.hidden-input {
  display: none;
}

.hint,
.readonly-note {
  width: 100%;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.readonly-note {
  margin: 0 0 14px 120px;
}

.save-row {
  margin-left: 120px;
}
</style>
