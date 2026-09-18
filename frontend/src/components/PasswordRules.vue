<!-- 密码要求，边打边对。

     从前这几条只活在服务端：人填一个密码、提交、被拒、再猜一个。规则不是
     秘密（它拦的是弱密码，不是拦不知道规则的人），写出来就是。

     规则本身在 lib/passwordPolicy，和服务端那份共用一套样例，谁改了忘了
     改另一边会有测试红。 -->
<template>
  <div class="rules">
    <div class="head">{{ t('passwordPolicy.title') }}</div>
    <ul class="list">
      <li v-for="r in rules" :key="r.key" :class="state(r.ok)">
        <span class="mark" aria-hidden="true">{{ r.ok ? '✓' : (touched ? '✕' : '○') }}</span>
        <span>
          {{ t(`passwordPolicy.${r.key}`) }}
          <template v-if="!r.ok && r.detail">
            —— {{ t('passwordPolicy.borrowed', { w: r.detail }) }}
          </template>
        </span>
      </li>
    </ul>
    <!-- 直接回答那个每次都会被问到的问题。不写的话，人只会一遍遍试。 -->
    <div class="note">{{ t('passwordPolicy.noComposition') }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { passwordRules } from '../lib/passwordPolicy'

const props = defineProps<{
  password: string
  /** 这个人身上攻击者已经知道的那些：姓名、工号、用户名、邮箱。空的会被忽略。 */
  context?: (string | undefined)[]
}>()

const { t } = useI18n()

// 还一个字都没打时不标红：那不是「错了」，是「还没开始」。
const touched = computed(() => props.password.length > 0)
const rules = computed(() => passwordRules(props.password, props.context ?? []))
function state(ok: boolean) {
  if (ok) return 'ok'
  return touched.value ? 'bad' : 'idle'
}
</script>

<style scoped>
.rules {
  margin-top: 6px;
  padding: 8px 10px;
  border-radius: 6px;
  background: var(--el-fill-color-lighter);
  font-size: 12px;
  line-height: 1.7;
}
.head {
  color: var(--el-text-color-regular);
  font-weight: 600;
}
.list {
  margin: 2px 0 0;
  padding: 0;
  list-style: none;
}
.list li {
  display: flex;
  align-items: flex-start;
  gap: 6px;
}
/* 勾是定宽的：三种符号宽窄不一样，不定宽的话每打一个字，后面的文字就抖一下。 */
.mark {
  flex: none;
  width: 12px;
  text-align: center;
}
.ok {
  color: var(--el-color-success);
}
.bad {
  color: var(--el-color-danger);
}
.idle {
  color: var(--el-text-color-secondary);
}
.note {
  margin-top: 4px;
  color: var(--el-text-color-secondary);
}
</style>
