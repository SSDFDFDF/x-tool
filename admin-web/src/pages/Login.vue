<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { LockKeyhole } from 'lucide-vue-next'
import { loginWithPassword } from '../utils/admin-auth'

const router = useRouter()
const route = useRoute()

const password = ref('')
const errorMessage = ref('')
const isSubmitting = ref(false)

const redirectTarget = computed(() => {
  const redirect = route.query.redirect
  return typeof redirect === 'string' && redirect.startsWith('/') ? redirect : '/'
})

const submitLogin = async () => {
  if (isSubmitting.value) {
    return
  }

  if (!password.value) {
    errorMessage.value = '请输入 admin 口令。'
    return
  }

  errorMessage.value = ''
  isSubmitting.value = true

  try {
    await loginWithPassword(password.value)
    await router.replace(redirectTarget.value)
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '登录失败，请稍后重试。'
  } finally {
    isSubmitting.value = false
  }
}
</script>

<template>
  <div class="min-h-screen bg-void text-white flex items-center justify-center px-6 py-10">
    <div class="absolute inset-0 bg-grid-pattern opacity-40"></div>

    <div class="relative z-10 w-full max-w-md rounded-2xl border border-white/10 bg-darkmatter/95 shadow-2xl overflow-hidden">
      <div class="px-8 py-8 border-b border-white/10 bg-black/30">
        <div class="flex items-center gap-3 mb-6">
          <img :src="'/admin/logo.png'" alt="X-Tool Logo" class="h-10 w-auto object-contain" />
          <div>
            <div class="text-xs uppercase tracking-[0.35em] text-muted font-heading">X-Tool</div>
            <div class="text-2xl font-heading font-semibold text-white mt-1">管理后台登录</div>
          </div>
        </div>
        <p class="text-sm text-white/70 leading-relaxed">请输入 admin 单口令以进入后台。登录成功后将进入 Overview 页面。</p>
      </div>

      <form class="px-8 py-8 space-y-6" @submit.prevent="submitLogin">
        <div class="flex flex-col gap-2">
          <label for="admin-password" class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">Admin 口令</label>
          <div class="relative">
            <LockKeyhole class="absolute left-4 top-1/2 -translate-y-1/2 w-4 h-4 text-white/40" />
            <input
              id="admin-password"
              v-model="password"
              type="password"
              autocomplete="current-password"
              placeholder="请输入当前 admin 口令"
              class="w-full h-12 pl-11 pr-4 bg-black/50 border-b-2 border-white/20 text-white text-sm placeholder:text-white/30 focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md"
            />
          </div>
        </div>

        <div v-if="errorMessage" class="rounded-lg border border-red-400/30 bg-red-500/10 px-4 py-3 text-sm text-red-200">
          {{ errorMessage }}
        </div>

        <button
          type="submit"
          class="w-full h-12 rounded-md bg-bitcoin text-white font-medium text-sm hover:bg-burnt transition-colors disabled:cursor-not-allowed disabled:opacity-60"
          :disabled="isSubmitting"
        >
          {{ isSubmitting ? '登录中...' : '登录' }}
        </button>
      </form>
    </div>
  </div>
</template>
