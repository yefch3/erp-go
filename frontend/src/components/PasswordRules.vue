<!-- 密码不合格时才冒出来，而且只说差的那几条。

     从前这份清单常驻在输入框底下（四条规则加一句说明，还没打字就摆着），
     老板 2026-09-18 看了说太吵：不通过再跳出来，说哪条不符合就行。

     「不通过」怎么判：打字停下来半秒多、密码还没过，才显示——一个字一个字
     敲的时候「至少 10 位」必然亮红，那不是问题，是还没打完。停下来了还差，
     才是要告诉人的事。全过了就收起来，一个字没打也不显示。

     规则本身在 lib/passwordPolicy，和服务端那份共用一套样例，谁改了忘了
     改另一边会有测试红。 -->
<template>
  <div v-if="shown" class="rules" role="status">
    <div class="head">{{ t('passwordPolicy.unmet') }}</div>
    <ul class="list">
      <li v-for="r in unmet" :key="r.key">
        <span class="mark" aria-hidden="true">✕</span>
        <span>
          {{ t(`passwordPolicy.${r.key}`) }}
          <template v-if="r.detail">
            —— {{ t('passwordPolicy.borrowed', { w: r.detail }) }}
          </template>
        </span>
      </li>
    </ul>
    <!-- 直接回答那个每次都会被问到的问题。不写的话，人只会加个感叹号再试。 -->
    <div class="note">{{ t('passwordPolicy.noComposition') }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { unmetRules } from '../lib/passwordPolicy'

const props = defineProps<{
  password: string
  /** 这个人身上攻击者已经知道的那些：姓名、工号、用户名、邮箱。空的会被忽略。 */
  context?: (string | undefined)[]
}>()

const { t } = useI18n()

// 打字停下来多久算「停下来」。太短会在敲字中途闪；太长人会以为没在检查。
const SETTLE_MS = 600

const unmet = computed(() => unmetRules(props.password, props.context ?? []))

// 每次密码一变就先收起来，停够 SETTLE_MS 再亮——所以敲字过程中它不闪。
const settled = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined
watch(
  () => props.password,
  () => {
    settled.value = false
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => {
      settled.value = true
    }, SETTLE_MS)
  },
)
onBeforeUnmount(() => {
  if (timer) clearTimeout(timer)
})

const shown = computed(() => settled.value && unmet.value.length > 0)
</script>

<style scoped>
.rules {
  margin-top: 6px;
  padding: 8px 10px;
  border-radius: 6px;
  background: var(--el-color-danger-light-9);
  font-size: 12px;
  line-height: 1.7;
}
.head {
  color: var(--el-color-danger);
  font-weight: 600;
}
.list {
  margin: 2px 0 0;
  padding: 0;
  list-style: none;
  color: var(--el-color-danger);
}
.list li {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}
.mark {
  flex: none;
  width: 12px;
  text-align: center;
}
.note {
  margin-top: 4px;
  color: var(--el-text-color-secondary);
}
</style>
