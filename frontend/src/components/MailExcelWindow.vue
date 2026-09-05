<template>
  <Teleport to="body">
    <section
      v-if="open"
      class="mail-excel-window"
      :class="{ 'is-minimized': minimized }"
      :style="windowStyle"
      role="dialog"
      aria-modal="false"
      :aria-label="title"
    >
      <button
        v-if="!minimized"
        type="button"
        class="mail-excel-window__resize"
        :aria-label="resizeLabel"
        :title="resizeLabel"
        @pointerdown="startResize"
      >
        <span aria-hidden="true" />
      </button>
      <header
        class="mail-excel-window__header"
        :title="minimized ? restoreLabel : undefined"
        @dblclick="toggleMinimized"
      >
        <button
          v-if="minimized"
          type="button"
          class="mail-excel-window__title"
          :aria-label="restoreLabel"
          @click="toggleMinimized"
        >
          {{ title }}
        </button>
        <strong v-else class="mail-excel-window__title">{{ title }}</strong>
        <div class="mail-excel-window__controls">
          <button
            type="button"
            class="mail-excel-window__control"
            :aria-label="minimized ? restoreLabel : minimizeLabel"
            :title="minimized ? restoreLabel : minimizeLabel"
            @click="toggleMinimized"
          >
            <svg v-if="minimized" class="mail-excel-window__icon" viewBox="0 0 20 20" aria-hidden="true">
              <rect x="4" y="4.5" width="12" height="11" rx="0.75" />
            </svg>
            <svg v-else class="mail-excel-window__icon" viewBox="0 0 20 20" aria-hidden="true">
              <path d="M4 10h12" />
            </svg>
          </button>
          <button
            type="button"
            class="mail-excel-window__control"
            :aria-label="closeLabel"
            :title="closeLabel"
            @click="emit('update:open', false)"
          >
            <svg class="mail-excel-window__icon" viewBox="0 0 20 20" aria-hidden="true">
              <path d="m5 5 10 10M15 5 5 15" />
            </svg>
          </button>
        </div>
      </header>

      <template v-if="!minimized">
        <div class="mail-excel-window__content">
          <slot />
        </div>
        <footer class="mail-excel-window__footer">
          <slot name="footer" />
        </footer>
      </template>
    </section>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'

const props = defineProps<{
  open: boolean
  minimized: boolean
  title: string
  minimizeLabel: string
  restoreLabel: string
  closeLabel: string
  resizeLabel: string
}>()

const emit = defineEmits<{
  'update:open': [value: boolean]
  'update:minimized': [value: boolean]
}>()

function toggleMinimized() {
  emit('update:minimized', !props.minimized)
}

const customSize = ref<{ width: number; height: number } | null>(null)
const windowStyle = computed(() => {
  if (props.minimized || !customSize.value) return undefined
  return {
    width: `${customSize.value.width}px`,
    height: `${customSize.value.height}px`,
  }
})

let stopResize: (() => void) | null = null

function startResize(event: PointerEvent) {
  const panel = (event.currentTarget as HTMLElement).closest<HTMLElement>('.mail-excel-window')
  if (!panel || props.minimized) return
  event.preventDefault()
  event.stopPropagation()

  const startX = event.clientX
  const startY = event.clientY
  const rect = panel.getBoundingClientRect()

  const onMove = (moveEvent: PointerEvent) => {
    const maxWidth = Math.max(280, window.innerWidth - 28)
    const maxHeight = Math.max(220, window.innerHeight - 24)
    const minWidth = Math.min(520, maxWidth)
    const minHeight = Math.min(320, maxHeight)
    customSize.value = {
      width: Math.min(maxWidth, Math.max(minWidth, rect.width + startX - moveEvent.clientX)),
      height: Math.min(maxHeight, Math.max(minHeight, rect.height + startY - moveEvent.clientY)),
    }
  }

  const finish = () => stopResize?.()
  stopResize = () => {
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', finish)
    window.removeEventListener('pointercancel', finish)
    stopResize = null
  }
  window.addEventListener('pointermove', onMove)
  window.addEventListener('pointerup', finish)
  window.addEventListener('pointercancel', finish)
}

onUnmounted(() => stopResize?.())
</script>

<style scoped>
.mail-excel-window {
  position: fixed;
  right: 20px;
  bottom: 16px;
  z-index: 3900;
  display: flex;
  flex-direction: column;
  width: min(920px, calc(100vw - 40px));
  max-height: calc(100vh - 32px);
  overflow: hidden;
  border: 1px solid var(--el-border-color-light);
  border-radius: 10px 10px 6px 6px;
  background: var(--el-bg-color-overlay);
  box-shadow: 0 12px 36px rgb(0 0 0 / 24%);
}

.mail-excel-window.is-minimized {
  width: min(330px, calc(100vw - 40px));
}

.mail-excel-window__resize {
  position: absolute;
  top: 0;
  left: 0;
  z-index: 2;
  width: 18px;
  height: 18px;
  padding: 0;
  border: 0;
  background: transparent;
  cursor: nwse-resize;
}

.mail-excel-window__resize span,
.mail-excel-window__resize span::before,
.mail-excel-window__resize span::after {
  position: absolute;
  top: 4px;
  left: 4px;
  width: 7px;
  height: 1px;
  background: rgb(255 255 255 / 72%);
  content: '';
  transform: rotate(-45deg);
  transform-origin: left center;
}

.mail-excel-window__resize span::before {
  top: 3px;
  left: 3px;
  width: 5px;
}

.mail-excel-window__resize span::after {
  top: 6px;
  left: 6px;
  width: 3px;
}

.mail-excel-window__header {
  display: flex;
  flex: 0 0 44px;
  align-items: center;
  justify-content: space-between;
  min-width: 0;
  padding-left: 16px;
  background: var(--el-color-primary-dark-2);
  color: var(--el-color-white);
  user-select: none;
}

.mail-excel-window__title {
  min-width: 0;
  overflow: hidden;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  font-weight: 600;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
}

button.mail-excel-window__title {
  align-self: stretch;
  flex: 1;
  cursor: pointer;
}

.mail-excel-window__controls {
  display: flex;
  align-self: stretch;
  flex: 0 0 auto;
}

.mail-excel-window__control {
  display: grid;
  width: 42px;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
  place-items: center;
}

.mail-excel-window__icon {
  width: 18px;
  height: 18px;
  fill: none;
  stroke: currentColor;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-width: 2;
}

.mail-excel-window__control:hover,
.mail-excel-window__control:focus-visible {
  background: rgb(255 255 255 / 16%);
  outline: none;
}

.mail-excel-window__control:focus-visible {
  box-shadow: inset 0 0 0 2px rgb(255 255 255 / 70%);
}

.mail-excel-window__content {
  min-height: 0;
  padding: 16px 18px 0;
  overflow: auto;
}

.mail-excel-window__footer {
  display: flex;
  flex: 0 0 auto;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 18px 16px;
  background: var(--el-bg-color-overlay);
}

@media (max-width: 640px) {
  .mail-excel-window {
    right: 8px;
    bottom: 8px;
    width: calc(100vw - 16px);
    max-height: calc(100vh - 16px);
  }

  .mail-excel-window.is-minimized {
    width: min(330px, calc(100vw - 16px));
  }
}
</style>
