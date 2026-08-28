<template>
  <div v-loading="loading" class="profile">
    <BasicDataEmployeeNav />

    <!-- ── 顶部横幅：照片、姓名、岗位，以及几条一眼就想看到的事实。
         Workday 那种版式：先给一张「这是谁」的名片，细节在下面分块摆。
         把工号、部门、上级、入职日横着排在这里，是因为这四样是别人问起
         「你是哪个部门的」时要报的，不该藏进下面某一块里。 -->
    <el-card shadow="never" class="banner">
      <div class="banner-row">
        <div class="avatar-wrap">
          <el-avatar :size="96" :src="profile.avatarUrl" class="face">
            {{ (profile.name || '—').slice(0, 1) }}
          </el-avatar>
          <!-- 换照片的入口压在头像上，鼠标移上去才出现——这是这页唯一
               「点一下就变」的东西，不该做成一个和别的按钮并排的按钮。 -->
          <button type="button" class="avatar-edit" :disabled="uploading" @click="fileInput?.click()">
            {{ uploading ? t('profile.uploading') : t('profile.changeAvatar') }}
          </button>
          <input
            ref="fileInput"
            type="file"
            :accept="acceptTypes"
            class="hidden-input"
            @change="onPick"
          />
        </div>

        <div class="headline">
          <h2 class="name">
            {{ profile.name || '—' }}
            <span v-if="profile.englishName" class="en">{{ profile.englishName }}</span>
          </h2>
          <div class="job">{{ profile.position || t('profile.noPosition') }}</div>
          <dl class="facts">
            <div><dt>{{ t('profile.code') }}</dt><dd>{{ profile.code || '—' }}</dd></div>
            <div><dt>{{ t('profile.department') }}</dt><dd>{{ profile.departmentName || '—' }}</dd></div>
            <div>
              <dt>{{ t('profile.manager') }}</dt>
              <dd>{{ profile.managerName || t('profile.noManager') }}</dd>
            </div>
            <div><dt>{{ t('profile.hireDate') }}</dt><dd>{{ profile.hireDate || '—' }}</dd></div>
          </dl>
        </div>

        <div class="banner-actions">
          <el-tag :type="profile.status === 'ACTIVE' ? 'success' : 'info'" effect="plain">
            {{ profile.status === 'ACTIVE' ? t('profile.onDuty') : t('profile.left') }}
          </el-tag>
          <el-button v-if="profile.id" link type="primary" @click="openMyOrgChart">
            {{ t('profile.seeInChart') }}
          </el-button>
        </div>
      </div>
    </el-card>

    <div class="cards">
      <!-- ── 我能改的 -->
      <el-card shadow="never" class="block">
        <template #header>
          <div class="block-head">
            <span>{{ t('profile.editableTitle') }}</span>
            <span class="block-note">{{ t('profile.editableNote') }}</span>
          </div>
        </template>
        <el-form label-position="top" :model="form">
          <el-form-item :label="t('profile.englishName')">
            <el-input v-model="form.englishName" maxlength="100" show-word-limit />
          </el-form-item>
          <el-form-item :label="t('profile.phone')">
            <el-input v-model="form.phone" maxlength="50" />
          </el-form-item>
          <el-button type="primary" :loading="saving" :disabled="!dirty" @click="save">
            {{ common('save') }}
          </el-button>
          <el-button v-if="dirty" link @click="reset">{{ common('cancel') }}</el-button>
          <el-button v-if="profile.avatarKey" link type="danger" @click="clearAvatar">
            {{ t('profile.removeAvatar') }}
          </el-button>
          <p class="hint">{{ t('profile.avatarHint') }}</p>
        </el-form>
      </el-card>

      <!-- ── 管理员维护的。照样显示，不藏起来：员工要能看到系统里记的自己
           是什么样，才发现得了记错。 -->
      <el-card shadow="never" class="block">
        <template #header>
          <div class="block-head">
            <span>{{ t('profile.managedTitle') }}</span>
            <span class="block-note">{{ t('profile.readonlyHint') }}</span>
          </div>
        </template>
        <dl class="rows">
          <div><dt>{{ t('profile.name') }}</dt><dd>{{ profile.name || '—' }}</dd></div>
          <div><dt>{{ t('profile.code') }}</dt><dd>{{ profile.code || '—' }}</dd></div>
          <div>
            <dt>{{ t('profile.email') }}</dt>
            <dd>
              {{ profile.email || '—' }}
              <el-tag
                :type="profile.emailVerified ? 'success' : 'warning'"
                size="small"
                effect="plain"
              >
                {{ profile.emailVerified ? t('profile.emailVerified') : t('profile.emailUnverified') }}
              </el-tag>
              <!-- 邮箱就是登录名，说一句免得有人以为它只是联系方式。 -->
              <span class="dd-note">{{ t('profile.emailIsLogin') }}</span>
            </dd>
          </div>
          <div><dt>{{ t('profile.department') }}</dt><dd>{{ profile.departmentName || '—' }}</dd></div>
          <div><dt>{{ t('profile.position') }}</dt><dd>{{ profile.position || '—' }}</dd></div>
          <div>
            <dt>{{ t('profile.manager') }}</dt>
            <dd>{{ profile.managerName || t('profile.noManager') }}</dd>
          </div>
          <div><dt>{{ t('profile.hireDate') }}</dt><dd>{{ profile.hireDate || '—' }}</dd></div>
        </dl>
      </el-card>

      <!-- ── 账号安全 -->
      <el-card shadow="never" class="block">
        <template #header>{{ t('profile.accountTitle') }}</template>
        <dl class="rows">
          <div><dt>{{ t('profile.loginEmail') }}</dt><dd>{{ profile.email || '—' }}</dd></div>
          <div>
            <dt>{{ t('profile.password') }}</dt>
            <dd>
              <span class="dots">••••••••</span>
              <el-button link type="primary" @click="passwordOpen = true">
                {{ t('password.title') }}
              </el-button>
            </dd>
          </div>
        </dl>
      </el-card>
    </div>

    <el-dialog v-model="passwordOpen" :title="t('password.title')" width="min(420px, calc(100vw - 24px))">
      <el-form label-width="96px">
        <el-form-item :label="t('password.current')">
          <el-input v-model="pw.oldPassword" type="password" show-password autocomplete="current-password" />
        </el-form-item>
        <el-form-item :label="t('password.new')">
          <el-input v-model="pw.newPassword" type="password" show-password autocomplete="new-password" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="passwordOpen = false">{{ common('cancel') }}</el-button>
        <el-button type="primary" :loading="changingPassword" @click="changePassword">
          {{ common('confirm') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { get, post, put } from '../api'
import BasicDataEmployeeNav from '../components/BasicDataEmployeeNav.vue'
import { useAuthStore } from '../stores/auth'

const { t } = useI18n()
const common = (k: string) => t(`common.${k}`)
const auth = useAuthStore()
const router = useRouter()

// 和 services/iam/internal/app/profile.go 的白名单是同一份名单。
// 那边才是真门，这里只是不让人白选一个必定被拒的文件。
const acceptTypes = 'image/jpeg,image/png,image/webp'
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
const changingPassword = ref(false)
const passwordOpen = ref(false)
const fileInput = ref<HTMLInputElement>()
const profile = ref<Partial<Profile>>({})
const form = reactive({ englishName: '', phone: '' })
const pw = reactive({ oldPassword: '', newPassword: '' })

// 没改过就不给按保存。一个永远可按的保存键，按下去什么也没发生，
// 会让人怀疑是不是没存上。
const dirty = computed(
  () =>
    form.englishName !== (profile.value.englishName ?? '') ||
    form.phone !== (profile.value.phone ?? ''),
)

function fillForm() {
  form.englishName = profile.value.englishName ?? ''
  form.phone = profile.value.phone ?? ''
}

async function load() {
  loading.value = true
  try {
    const d = await get<{ profile: Profile }>('/me/profile')
    profile.value = d.profile ?? {}
    fillForm()
    // 顶栏那个小头像跟着变。地址是短命的，所以每次都是现取的，不进缓存。
    auth.avatarUrl = profile.value.avatarUrl ?? ''
  } finally {
    loading.value = false
  }
}

function reset() {
  fillForm()
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
    fillForm()
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

async function changePassword() {
  if (!pw.oldPassword || !pw.newPassword) {
    ElMessage.warning(t('password.required'))
    return
  }
  changingPassword.value = true
  try {
    await post('/me/password', { oldPassword: pw.oldPassword, newPassword: pw.newPassword })
    ElMessage.success(t('password.changed'))
    passwordOpen.value = false
    pw.oldPassword = ''
    pw.newPassword = ''
  } finally {
    changingPassword.value = false
  }
}

// 「在架构图里看看我」——从自己的资料跳到自己那一圈，是这页最自然的下一步。
function openMyOrgChart() {
  router.push('/basic/employees/org')
}

onMounted(load)
</script>

<style scoped>
/* ── 顶部名片 */
.banner {
  margin-bottom: 16px;
}

.banner-row {
  display: flex;
  align-items: flex-start;
  gap: 24px;
}

.avatar-wrap {
  position: relative;
  flex: none;
}

.face {
  font-size: 36px;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
}

/* 换照片压在头像下缘，移上去才现身：这是唯一一个会改东西的入口，
   平时不该和「这是谁」抢注意力。 */
.avatar-edit {
  position: absolute;
  inset-inline: 0;
  bottom: 0;
  padding: 3px 0;
  border: none;
  border-radius: 0 0 48px 48px;
  background: rgb(0 0 0 / 55%);
  color: #fff;
  font-size: 11px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 150ms ease;
}

.avatar-wrap:hover .avatar-edit,
.avatar-edit:focus-visible {
  opacity: 1;
}

.hidden-input {
  display: none;
}

.headline {
  flex: 1;
  min-width: 0;
}

.name {
  display: flex;
  align-items: baseline;
  gap: 10px;
  margin: 0 0 4px;
  font-size: 24px;
}

.en {
  font-size: 14px;
  font-weight: 400;
  color: var(--el-text-color-secondary);
}

.job {
  margin-bottom: 14px;
  color: var(--el-text-color-regular);
}

/* 四条事实横着排。竖着排会把名片撑成一列表格，而这里要的是一眼扫过。 */
.facts {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 32px;
  margin: 0;
}

.facts > div {
  min-width: 0;
}

.facts dt {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.facts dd {
  margin: 2px 0 0;
  font-weight: 500;
}

.banner-actions {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 8px;
}

/* ── 下面三块 */
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 16px;
  align-items: start;
}

.block-head {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.block-note,
.hint,
.dd-note {
  font-size: 12px;
  font-weight: 400;
  color: var(--el-text-color-secondary);
}

.hint {
  margin: 10px 0 0;
}

.dd-note {
  display: block;
}

/* 标签在上、值在下，不用表格线。Workday 那种版式的核心就是这个——
   一条一条读，而不是在网格里找。 */
.rows {
  margin: 0;
}

.rows > div + div {
  margin-top: 14px;
}

.rows dt {
  margin-bottom: 2px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.rows dd {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin: 0;
}

.dots {
  letter-spacing: 2px;
  color: var(--el-text-color-secondary);
}

@media (max-width: 720px) {
  .banner-row {
    flex-direction: column;
  }

  .banner-actions {
    flex-direction: row;
    align-items: center;
  }
}
</style>
