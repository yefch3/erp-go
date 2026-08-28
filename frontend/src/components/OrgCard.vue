<template>
  <!-- 架构图上的一个人。
       button 而不是 div：整张卡片是可点的（点谁就以谁为中心重画），
       而可点的东西应该能用键盘走到、能被读屏念出来。 -->
  <button
    type="button"
    class="org-card"
    :class="{ me: highlight, muted }"
    :aria-current="highlight ? 'true' : undefined"
    :title="titleText"
    @click="emit('open', member.id)"
  >
    <el-avatar :size="34" :src="member.avatarUrl" class="face">
      {{ (member.name || '—').slice(0, 1) }}
    </el-avatar>
    <span class="text">
      <span class="name">
        {{ member.name }}
        <!-- 「我」跟着**本人**走，不跟着中心走。中心可以是任何人——看别人那一圈
             的时候，把中心标成「我」就是在说谎，而且会让人找不到自己在哪。 -->
        <el-tag v-if="isMe" size="small" :type="highlight ? 'success' : 'primary'" effect="dark" class="me-tag">
          {{ t('orgChart.you') }}
        </el-tag>
      </span>
      <span class="line2">{{ member.position || member.departmentName || '—' }}</span>
      <!-- 已经录了离职日期的人带一个标签。交接和排班都得知道谁快走了，
           而这张图正是想这件事的时候会打开的地方。 -->
      <span v-if="member.leaveDate" class="leaving">{{ t('orgChart.leaving', { d: member.leaveDate }) }}</span>
    </span>
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OrgMember } from '../lib/orgChart'

const props = defineProps<{
  member: OrgMember
  /** 中心那个人：整张卡片高亮。中心不等于本人——中心是「现在在看谁」。 */
  highlight?: boolean
  /** 这张卡片是不是登录的这个人自己。和 highlight 无关，两个都可能成立。 */
  isMe?: boolean
  /** 上级链里靠上的几层：压淡，让视线落在中心附近。 */
  muted?: boolean
}>()

const emit = defineEmits<{ open: [string] }>()
const { t } = useI18n()

// 卡片上放不下的都进 title：工号、部门、英文名。鼠标停一下就有，
// 不用为了看一个工号点进详情页。
const titleText = computed(() =>
  [props.member.name, props.member.englishName, props.member.code, props.member.departmentName]
    .filter(Boolean)
    .join(' · '),
)
</script>

<style scoped>
.org-card {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 208px;
  padding: 10px 12px;
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  background: var(--el-bg-color);
  text-align: start;
  cursor: pointer;
  transition:
    border-color 150ms ease,
    box-shadow 150ms ease,
    transform 150ms ease;
}

.org-card:hover {
  border-color: var(--el-color-primary);
  box-shadow: 0 2px 10px rgb(0 0 0 / 8%);
}

.org-card:focus-visible {
  outline: 2px solid var(--el-color-primary);
  outline-offset: 2px;
}

/* 中心那张：实心主色，一眼能找到自己。这是「以谁为中心」这件事唯一的
   视觉锚点，颜色要够重，压过 hover 才行。 */
.org-card.me {
  border-color: var(--el-color-primary);
  background: var(--el-color-primary);
  box-shadow: 0 4px 14px var(--el-color-primary-light-5);
}

.org-card.me .name,
.org-card.me .line2 {
  color: #fff;
}

.org-card.me .line2 {
  opacity: 0.85;
}

/* 上级链里靠上的几层压淡：视线该落在中心那一圈，而不是被最上面的老板拽走。 */
.org-card.muted {
  opacity: 0.62;
}

.org-card.muted:hover {
  opacity: 1;
}

.face {
  flex: none;
  font-size: 14px;
  background: var(--el-color-primary-light-8);
  color: var(--el-color-primary);
}

.text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.name {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.me-tag {
  flex: none;
}

.line2 {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.leaving {
  font-size: 11px;
  color: var(--el-color-danger);
}
</style>
