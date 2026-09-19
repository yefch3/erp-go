<!-- 密码要求，常驻在输入框底下；检查从打第一个字才开始。

     从前这几条只活在服务端：人填一个密码、提交、被拒、再猜一个。规则不是
     秘密（它拦的是弱密码，不是拦不知道规则的人），写出来就是。

     还没打字时四条全是 ○——不打勾也不打叉。第一版在这一步就给「不是常见
     密码」那几条打了勾（空字符串确实不是常见密码），看着像系统在夸一个还
     没写的密码；老板 2026-09-18 看了说：检查要等输入了密码再开始。

     规则本身在 lib/passwordPolicy，和服务端那份共用一套样例，谁改了忘了
     改另一边会有测试红。 -->
<template>
  <div class="rules">
    <div class="head">{{ t('passwordPolicy.title') }}</div>
    <ul class="list">
      <li v-for="r in rules" :key="r.key" :class="r.state">
        <span class="mark" aria-hidden="true">{{ MARKS[r.state] }}</span>
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
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ruleStates, type RuleState } from '../lib/passwordPolicy'

const props = defineProps<{
  password: string
  /** 这个人身上攻击者已经知道的那些：姓名、工号、用户名、邮箱。空的会被忽略。 */
  context?: (string | undefined)[]
}>()

const { t } = useI18n()

const MARKS: Record<RuleState, string> = { idle: '○', ok: '✓', bad: '✕' }

const rules = computed(() => ruleStates(props.password, props.context ?? []))
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
