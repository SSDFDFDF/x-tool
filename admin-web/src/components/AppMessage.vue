<script setup lang="ts">
import { computed } from 'vue'
import { AlertCircle, AlertTriangle, CheckCircle2, Info, X } from 'lucide-vue-next'
import { hideMessage, messageState } from '../utils/message'

const iconComponent = computed(() => {
  switch (messageState.type.value) {
    case 'success':
      return CheckCircle2
    case 'error':
      return AlertCircle
    case 'warning':
      return AlertTriangle
    default:
      return Info
  }
})

const toneClass = computed(() => {
  switch (messageState.type.value) {
    case 'success':
      return 'border-emerald-400/30 bg-emerald-500/15 text-emerald-100 shadow-[0_12px_40px_rgba(16,185,129,0.15)]'
    case 'error':
      return 'border-red-400/30 bg-red-500/15 text-red-100 shadow-[0_12px_40px_rgba(239,68,68,0.18)]'
    case 'warning':
      return 'border-amber-400/30 bg-amber-500/15 text-amber-100 shadow-[0_12px_40px_rgba(245,158,11,0.16)]'
    default:
      return 'border-sky-400/30 bg-sky-500/15 text-sky-100 shadow-[0_12px_40px_rgba(14,165,233,0.16)]'
  }
})
</script>

<template>
  <transition name="message-slide">
    <div
      v-if="messageState.visible.value"
      class="fixed right-4 top-4 z-[60] w-[min(420px,calc(100vw-2rem))] rounded-2xl border backdrop-blur-xl"
      :class="toneClass"
      role="status"
      aria-live="polite"
    >
      <div class="flex items-start gap-3 p-4">
        <component :is="iconComponent" class="mt-0.5 h-5 w-5 shrink-0" />

        <div class="min-w-0 flex-1">
          <div v-if="messageState.title.value" class="text-sm font-semibold">
            {{ messageState.title.value }}
          </div>
          <div class="text-sm leading-6 whitespace-pre-line opacity-95">
            {{ messageState.message.value }}
          </div>
        </div>

        <button
          type="button"
          class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full border border-white/10 bg-black/10 text-current transition-colors hover:bg-black/20"
          @click="hideMessage"
        >
          <X class="h-4 w-4" />
        </button>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.message-slide-enter-active,
.message-slide-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}

.message-slide-enter-from,
.message-slide-leave-to {
  opacity: 0;
  transform: translateY(-10px);
}
</style>
