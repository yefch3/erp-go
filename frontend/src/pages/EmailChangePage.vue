<template>
  <!-- 从一封信点进来的一屏，和 ActivatePage 是同一族。
       在要人按下任何东西之前先答三件事：这是谁的账号、链接是不是真的、
       按下去之后会怎样。第三件在这一页格外要紧——按下去他会被退出登录，
       而这不是他预期的。 -->
  <div class="ec-wrap">
    <div class="lang-corner"><LangSwitcher light /></div>
    <div class="ec-brand">
      <div class="brand-mark">ERP</div>
      <h1>{{ t('emailChange.title') }}</h1>
    </div>

    <el-card class="ec-card" shadow="never">
      <div v-if="checking" v-loading="true" class="state" />

      <!-- 链接不能用。在问「确认吗」之前就说，不是之后：五个理由里有四个
           再试一次也没用，而读到这句话的人没有别的渠道可问。 -->
      <div v-else-if="fault" class="state">
        <div class="state-icon bad">✕</div>
        <p class="state-msg">{{ fault }}</p>
        <el-button link type="primary" @click="router.push('/login')">
          {{ t('emailChange.toLogin') }}
        </el-button>
      </div>

      <!-- 成功。新地址填好带去登录页——它从这一刻起就是登录名，
           而没有人告诉过他这件事。 -->
      <div v-else-if="done" class="state">
        <div class="state-icon good">✓</div>
        <p class="state-msg">{{ t('emailChange.done') }}</p>
        <p class="state-sub">{{ target.newEmail }}</p>
        <el-button type="primary" class="wide" @click="toLogin">
          {{ t('emailChange.toLogin') }}
        </el-button>
      </div>

      <div v-else class="state confirm">
        <p class="greeting">{{ t('emailChange.greeting', { name: target.name }) }}</p>

        <!-- 从哪到哪，摆出来。这一页要他确认的就是这一件事，
             用一行小字带过等于让他闭着眼睛按。 -->
        <div class="move">
          <span class="addr old">{{ target.oldEmail }}</span>
          <span class="arrow">→</span>
          <span class="addr new">{{ target.newEmail }}</span>
        </div>

        <!-- 按下去会发生什么，说全。「你会被退出登录」是这里唯一会让人
             措手不及的后果，所以它单独一行、排在最后。 -->
        <ul class="effects">
          <li>{{ t('emailChange.effectLogin', { email: target.newEmail }) }}</li>
          <li>{{ t('emailChange.effectPassword') }}</li>
          <li class="warn">{{ t('emailChange.effectSignOut') }}</li>
        </ul>

        <div v-if="error" class="form-error">{{ error }}</div>
        <el-button type="primary" class="wide" :loading="saving" @click="submit">
          {{ t('emailChange.submit') }}
        </el-button>
        <p class="note">{{ t('emailChange.notMe') }}</p>
      </div>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { http, quietErrors } from '../api'
import LangSwitcher from '../components/LangSwitcher.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const token = String(route.query.token ?? '')
const checking = ref(true)
const saving = ref(false)
const done = ref(false)
// fault 是链接为什么不能用，用服务端自己的话。和 error 分开：
// 一个替换整个表单，一个待在按钮上方。
const fault = ref('')
const error = ref('')
const target = reactive({ name: '', oldEmail: '', newEmail: '' })

// 两个请求都静默：这一页上的每个失败都该待在造成它的东西旁边，
// 而不是飘在一个本身就是错误的页面上方。
function message(e: unknown, fallback: string): string {
  return (e as { message?: string })?.message || fallback
}

onMounted(async () => {
  if (!token) {
    fault.value = t('emailChange.noToken')
    checking.value = false
    return
  }
  try {
    const resp = await http.get('/auth/email-change', { params: { token }, ...quietErrors })
    const d = resp.data.data as { name: string; oldEmail: string; newEmail: string }
    target.name = d.name
    target.oldEmail = d.oldEmail
    target.newEmail = d.newEmail
  } catch (e: unknown) {
    fault.value = message(e, t('emailChange.badLink'))
  } finally {
    checking.value = false
  }
})

async function submit() {
  saving.value = true
  error.value = ''
  try {
    await http.post('/auth/email-change', { token }, quietErrors)
    done.value = true
  } catch (e: unknown) {
    error.value = message(e, t('emailChange.failed'))
  } finally {
    saving.value = false
  }
}

// 这里不发会话，和激活页同一个道理：链接一旦被从信箱里读出来，
// 它本身绝不能成为一条进门的路。新地址带过去，好让登录框是填好的。
function toLogin() {
  router.push({ path: '/login', query: { email: target.newEmail } })
}
</script>

<style scoped>
.ec-wrap {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 28px;
  min-height: 100vh;
  background: #0f172a;
}
.lang-corner {
  position: absolute;
  top: 20px;
  right: 28px;
}
.ec-brand {
  color: #e2e8f0;
  text-align: center;
}
.brand-mark {
  display: inline-block;
  margin-bottom: 12px;
  padding: 6px 14px;
  border: 2px solid #38bdf8;
  border-radius: 6px;
  color: #38bdf8;
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 2px;
}
.ec-brand h1 {
  margin: 0;
  font-size: 22px;
  font-weight: 500;
}
/* 手机上这封信是从邮件客户端点开的，屏幕比这张卡片窄。写死宽度会让卡片
   两侧被裁掉——而被裁掉的正好是邮箱地址的头尾，也就是他要核对的东西。 */
.ec-card {
  width: min(420px, calc(100vw - 32px));
  border-radius: 10px;
}
.state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  min-height: 150px;
  text-align: center;
}
.state.confirm {
  align-items: stretch;
  text-align: start;
}
.state-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: 50%;
  font-size: 22px;
  color: #fff;
}
.state-icon.good {
  background: var(--el-color-success);
}
.state-icon.bad {
  background: var(--el-color-danger);
}
.state-msg {
  margin: 0;
  font-size: 14px;
  line-height: 1.6;
}
.state-sub {
  margin: 0;
  font-size: 13px;
  color: var(--el-text-color-secondary);
}
.greeting {
  margin: 0;
  font-size: 15px;
}

/* 从哪到哪。新地址加重，旧地址压淡并划掉——一眼就知道哪个是结果。 */
.move {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  padding: 12px;
  border-radius: 8px;
  background: var(--el-fill-color-light);
  font-size: 13px;
  word-break: break-all;
}
.addr.old {
  color: var(--el-text-color-secondary);
  text-decoration: line-through;
}
.addr.new {
  font-weight: 600;
  color: var(--el-color-primary);
}
.arrow {
  color: var(--el-text-color-secondary);
}

.effects {
  margin: 0;
  padding-inline-start: 18px;
  font-size: 13px;
  line-height: 1.9;
  color: var(--el-text-color-regular);
}
.effects .warn {
  color: var(--el-color-warning);
}
.wide {
  width: 100%;
}
.form-error {
  font-size: 12px;
  color: var(--el-color-danger);
}
.note {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-secondary);
}
</style>
