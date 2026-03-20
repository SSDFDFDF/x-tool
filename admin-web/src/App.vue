<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { LayoutDashboard, Settings, FileText, ScrollText, RadioReceiver, Network, Server, LogOut } from 'lucide-vue-next'
import { authState, logoutAdmin } from './utils/admin-auth'
import AppMessage from './components/AppMessage.vue'
import { showMessage } from './utils/message'

const route = useRoute()
const router = useRouter()
const isLoggingOut = ref(false)

const isLoginPage = computed(() => route.name === 'login')
const authPending = computed(() => !authState.isInitialized.value || authState.isCheckingStatus.value)

const handleLogout = async () => {
  if (isLoggingOut.value) {
    return
  }

  isLoggingOut.value = true

  try {
    await logoutAdmin()
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    showMessage.error(message, '登出失败')
  } finally {
    isLoggingOut.value = false
    router.push({ name: 'login' })
  }
}
</script>

<template>
  <div class="relative">
    <AppMessage />

    <div v-if="authPending" class="min-h-screen bg-void text-white flex items-center justify-center px-6">
      <div class="w-full max-w-md rounded-2xl border border-white/10 bg-darkmatter/90 p-8 text-center shadow-2xl">
        <div class="text-sm uppercase tracking-[0.3em] text-muted font-heading mb-4">X-Tool Admin</div>
        <div class="text-xl font-semibold text-white">正在检查登录状态...</div>
      </div>
    </div>

    <router-view v-else-if="isLoginPage" v-slot="{ Component }">
      <transition name="fade" mode="out-in">
        <component :is="Component" />
      </transition>
    </router-view>

    <div v-else class="flex h-screen overflow-hidden bg-void text-white font-body">
      <!-- Sidebar -->
      <aside class="w-64 bg-darkmatter border-r border-white/10 flex flex-col shrink-0 flex-shrink-0 relative z-20">
        <div class="h-16 flex items-center px-6 border-b border-white/10 relative overflow-hidden">
          <div class="flex items-center gap-3 relative z-10">
            <img :src="'/admin/logo.png'" alt="X-Tool Logo" class="h-9 w-auto object-contain" />
            <span class="font-heading font-bold text-xl tracking-tight text-white">X-Tool</span>
          </div>
        </div>

        <nav class="flex-1 px-3 py-6 overflow-y-auto">
          <div class="text-xs uppercase text-muted font-bold tracking-widest pl-3 mb-4 flex items-center gap-2">
            <span>控制台</span>
            <div class="h-px bg-white/10 flex-1"></div>
          </div>

          <router-link to="/" class="flex items-center gap-3 px-3 py-2.5 rounded-md text-muted hover:text-white transition-all duration-300 hover:bg-white/5 group mb-2" active-class="bg-bitcoin/10 !text-bitcoin">
            <LayoutDashboard class="w-5 h-5" />
            <span class="font-medium text-sm">总览 (Overview)</span>
          </router-link>

          <router-link to="/config" class="flex items-center gap-3 px-3 py-2.5 rounded-md text-muted hover:text-white transition-all duration-300 hover:bg-white/5 group mb-2" active-class="bg-bitcoin/10 !text-bitcoin">
            <Settings class="w-5 h-5" />
            <span class="font-medium text-sm">配置管理 (Settings)</span>
          </router-link>

          <router-link to="/prompts" class="flex items-center gap-3 px-3 py-2.5 rounded-md text-muted hover:text-white transition-all duration-300 hover:bg-white/5 group mb-2" active-class="bg-bitcoin/10 !text-bitcoin">
            <FileText class="w-5 h-5" />
            <span class="font-medium text-sm">Prompt 模板 (Prompts)</span>
          </router-link>

          <router-link to="/upstreams" class="flex items-center gap-3 px-3 py-2.5 rounded-md text-muted hover:text-white transition-all duration-300 hover:bg-white/5 group mb-2" active-class="bg-bitcoin/10 !text-bitcoin">
            <Server class="w-5 h-5" />
            <span class="font-medium text-sm">上游服务 (Upstreams)</span>
          </router-link>

          <router-link to="/logs" class="flex items-center gap-3 px-3 py-2.5 rounded-md text-muted hover:text-white transition-all duration-300 hover:bg-white/5 group mb-2" active-class="bg-bitcoin/10 !text-bitcoin">
            <ScrollText class="w-5 h-5" />
            <span class="font-medium text-sm">日志中心 (Logs)</span>
          </router-link>

          <div class="text-xs uppercase text-muted font-bold tracking-widest pl-3 mt-8 mb-4 flex items-center gap-2">
            <span>快捷链接</span>
            <div class="h-px bg-white/10 flex-1"></div>
          </div>

          <router-link to="/probe/models" class="flex items-center gap-3 px-3 py-2 rounded-md text-muted hover:text-bitcoin transition-all duration-300 hover:bg-white/5 text-sm mb-1 group" active-class="bg-bitcoin/10 !text-bitcoin">
            <Network class="w-4 h-4 opacity-50 group-hover:opacity-100" />
            <span>模型 API (/v1/models)</span>
          </router-link>
          <router-link to="/probe/root" class="flex items-center gap-3 px-3 py-2 rounded-md text-muted hover:text-bitcoin transition-all duration-300 hover:bg-white/5 text-sm group" active-class="bg-bitcoin/10 !text-bitcoin">
            <RadioReceiver class="w-4 h-4 opacity-50 group-hover:opacity-100" />
            <span>根路由探活 (/)</span>
          </router-link>
        </nav>

        <div class="p-4 border-t border-white/10 relative overflow-hidden space-y-3">
          <button
            class="w-full inline-flex items-center justify-center gap-2 rounded-md border border-red-400/30 bg-red-500/10 px-4 py-2.5 text-sm font-medium text-red-200 transition-colors hover:bg-red-500/20 disabled:cursor-not-allowed disabled:opacity-60"
            :disabled="isLoggingOut"
            @click="handleLogout"
          >
            <LogOut class="w-4 h-4" />
            {{ isLoggingOut ? '登出中...' : '退出登录' }}
          </button>
          <div class="text-[10px] text-muted text-center font-mono opacity-50">X-TOOL PROXY V1.0</div>
        </div>
      </aside>

      <!-- Main Content -->
      <main class="flex-1 flex flex-col min-w-0 relative">
        <div class="flex-1 overflow-y-auto relative z-10 w-full h-full">
          <router-view v-slot="{ Component }">
            <transition name="fade" mode="out-in">
              <component :is="Component" />
            </transition>
          </router-view>
        </div>
      </main>
    </div>
  </div>
</template>

<style>
/* Global page transitions */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease, transform 0.3s ease;
}

.fade-enter-from {
  opacity: 0;
  transform: translateY(10px);
}

.fade-leave-to {
  opacity: 0;
  transform: translateY(-10px);
}
</style>
