<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { KeyRound, Save } from 'lucide-vue-next'
import { cloneConfig, configToForm, formToConfig } from '../utils/config-form'
import type { AppConfig, ConfigFormModel } from '../types/config'
import { updateAdminPassword } from '../utils/admin-auth'
import { showMessage } from '../utils/message'
import {
  getPromptInjectionRoleMode,
  updatePromptInjectionRoleMode
} from '../composables/useUpstreamConfig'

type PromptInjectionTarget = 'auto' | 'message' | 'system' | 'instructions'

const router = useRouter()

const rawConfig = ref<AppConfig | null>(null)
const configForm = ref<ConfigFormModel | null>(null)
const isSaving = ref(false)
const isUpdatingPassword = ref(false)
const activeConfigTab = ref('server')
const passwordForm = ref({
  currentPassword: '',
  newPassword: '',
  confirmNewPassword: ''
})

const globalPromptInjectionTargetOptions: Array<{ value: PromptInjectionTarget | ''; label: string }> = [
  { value: '', label: '默认 (auto)' },
  { value: 'auto', label: 'auto' },
  { value: 'message', label: 'message' },
  { value: 'system', label: 'system' },
  { value: 'instructions', label: 'instructions' }
]

const updateGlobalPromptInjectionRole = (mode: string) => {
  if (!configForm.value) {
    return
  }

  updatePromptInjectionRoleMode(mode, (next) => {
    configForm.value!.features.prompt_injection_role = next === 'custom' ? '' : next
  })
}

const loadConfig = async () => {
  try {
    const res = await fetch('/admin/api/config')
    if (!res.ok) throw new Error('API request failed')
    const config = await res.json() as AppConfig
    rawConfig.value = cloneConfig(config)
    configForm.value = configToForm(config)
  } catch (e: any) {
    showMessage.error(e.message, '加载配置失败')
  }
}

const saveConfig = async () => {
  if (!configForm.value || !rawConfig.value) return
  isSaving.value = true

  try {
    const payload = formToConfig(rawConfig.value, configForm.value)

    const res = await fetch('/admin/api/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    })

    if (!res.ok) {
      const err = await res.json()
      throw new Error(err?.error?.message || `HTTP ${res.status}`)
    }

    rawConfig.value = cloneConfig(payload)
    configForm.value = configToForm(payload)
    window.dispatchEvent(new CustomEvent('admin:config-saved'))
    showMessage.success('配置已成功保存。')
  } catch (e: any) {
    showMessage.error(e.message, '保存失败')
  } finally {
    isSaving.value = false
  }
}

const changeAdminPassword = async () => {
  if (isUpdatingPassword.value) {
    return
  }

  const currentPassword = passwordForm.value.currentPassword.trim()
  const newPassword = passwordForm.value.newPassword.trim()
  const confirmNewPassword = passwordForm.value.confirmNewPassword.trim()

  if (!currentPassword || !newPassword || !confirmNewPassword) {
    showMessage.warning('请完整填写当前口令、新口令和确认新口令。')
    return
  }

  if (newPassword !== confirmNewPassword) {
    showMessage.warning('两次输入的新口令不一致。')
    return
  }

  isUpdatingPassword.value = true

  try {
    await updateAdminPassword(currentPassword, newPassword)
    passwordForm.value = {
      currentPassword: '',
      newPassword: '',
      confirmNewPassword: ''
    }
    showMessage.success('admin 口令修改成功，请重新登录。')
    router.replace('/login')
  } catch (e: any) {
    showMessage.error(e.message, '修改 admin 口令失败')
  } finally {
    isUpdatingPassword.value = false
  }
}

onMounted(() => {
  loadConfig()
})
</script>

<template>
  <div class="p-6 md:p-8 w-full h-full flex flex-col pt-[calc(3rem)] overflow-y-auto">
    <header class="flex items-center justify-between mb-8 pb-4 border-b border-white/10 shrink-0">
      <h1 class="font-heading font-semibold text-3xl md:text-4xl text-white">
        系统配置管理
      </h1>
      <button
        class="bg-bitcoin hover:bg-burnt text-white font-medium text-sm px-6 py-3 rounded-md transition-colors flex items-center justify-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
        :disabled="isSaving"
        @click="saveConfig"
      >
        <Save class="w-4 h-4" />
        {{ isSaving ? '保存中...' : '保存配置并应用' }}
      </button>
    </header>

    <div class="flex-1 w-full" v-if="configForm">

      <!-- Tabs Header -->
      <div class="flex gap-2 overflow-x-auto mb-8 border-b border-white/10 pb-1 w-full">
        <button
          v-for="(label, key) in { server: '服务器设置', features: '全局特性', auth: '口令修改' }"
          :key="key"
          @click="activeConfigTab = key as string"
          class="px-6 py-3 font-medium text-sm rounded-t-lg transition-all duration-300 relative shrink-0"
          :class="activeConfigTab === key ? 'text-bitcoin' : 'text-muted hover:text-white hover:bg-white/5'"
        >
          {{ label }}
          <div v-show="activeConfigTab === key" class="absolute bottom-[-1px] left-0 w-full h-[2px] bg-bitcoin shadow-[0_-2px_8px_rgba(247,147,26,0.6)]"></div>
        </button>
      </div>

      <!-- Server Settings -->
      <transition name="fade" mode="out-in">
        <div v-show="activeConfigTab === 'server'" class="bg-darkmatter border border-white/10 rounded-lg overflow-hidden transition-all duration-300">
          <div class="px-6 py-5 border-b border-white/5 relative bg-black/40">
            <h2 class="font-heading font-semibold text-xl text-white">服务器基础配置</h2>
          </div>
          <div class="p-6 md:p-8 flex flex-col gap-6 max-w-2xl">
            <div class="flex flex-col gap-2">
              <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">监听端口 (Port)</label>
              <input type="number" v-model="configForm.server.port" class="h-12 px-4 bg-white/10 border-b-2 border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md font-mono" />
            </div>
            <div class="flex flex-col gap-2">
              <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">绑定地址 (Host)</label>
              <input type="text" v-model="configForm.server.host" class="h-12 px-4 bg-white/10 border-b-2 border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md font-mono" />
            </div>
            <div class="flex flex-col gap-2">
              <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">请求超时 (Timeout) 秒</label>
              <input type="number" v-model="configForm.server.timeout" class="h-12 px-4 bg-white/10 border-b-2 border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md font-mono" />
            </div>
          </div>
        </div>
      </transition>

      <!-- Feature Settings -->
      <transition name="fade" mode="out-in">
        <div v-show="activeConfigTab === 'features'" class="bg-darkmatter border border-white/10 rounded-lg overflow-hidden transition-all duration-300">
          <div class="px-6 py-5 border-b border-white/5 relative bg-black/40">
            <h2 class="font-heading font-semibold text-xl text-white">全局功能与特性</h2>
          </div>
          <div class="p-6 md:p-8 grid grid-cols-1 md:grid-cols-2 gap-x-8 gap-y-6">
            <div class="flex flex-col gap-2">
              <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">启用软工具调用协议转换</label>
              <select v-model="configForm.features.enable_function_calling" class="h-12 w-full font-mono px-4 bg-white/10 border-b-2 border-white/20 text-white rounded-md">
                <option :value="true">开启</option>
                <option :value="false">关闭</option>
              </select>
            </div>
            <div class="flex flex-col gap-2">
              <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">转换 Developer 角色</label>
              <select v-model="configForm.features.convert_developer_to_system" class="h-12 w-full font-mono px-4 bg-white/10 border-b-2 border-white/20 text-white rounded-md">
                <option :value="true">开启</option>
                <option :value="false">关闭</option>
              </select>
            </div>
            <div class="flex flex-col gap-2">
              <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">Key 透传</label>
              <select v-model="configForm.features.key_passthrough" class="h-12 w-full font-mono px-4 bg-white/10 border-b-2 border-white/20 text-white rounded-md">
                <option :value="true">开启</option>
                <option :value="false">关闭</option>
              </select>
            </div>
            <div class="flex flex-col gap-2">
              <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">全局模型透传</label>
              <select v-model="configForm.features.model_passthrough" class="h-12 w-full font-mono px-4 bg-white/10 border-b-2 border-white/20 text-white rounded-md">
                <option :value="true">开启</option>
                <option :value="false">关闭</option>
              </select>
            </div>
            <div class="flex flex-col gap-2 md:col-span-2">
              <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">日志级别 (log_level)</label>
              <select v-model="configForm.features.log_level" class="h-12 w-full md:w-1/2 font-mono px-4 bg-white/10 border-b-2 border-white/20 text-white rounded-md">
                <option value="DEBUG">DEBUG</option>
                <option value="INFO">INFO</option>
                <option value="WARNING">WARNING</option>
                <option value="ERROR">ERROR</option>
                <option value="DISABLED">DISABLED</option>
              </select>
            </div>
            <div class="flex flex-col gap-2 md:col-span-2">
              <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">提示注入目标 (prompt_injection_target)</label>
              <select v-model="configForm.features.prompt_injection_target" class="h-12 w-full md:w-1/2 font-mono px-4 bg-white/10 border-b-2 border-white/20 text-white rounded-md">
                <option v-for="option in globalPromptInjectionTargetOptions" :key="`global-target-${option.value || 'inherit'}`" :value="option.value">
                  {{ option.label }}
                </option>
              </select>
              <p class="text-xs text-white/55 leading-5">
                `auto` 会按协议自动选择注入位置：OpenAI compat 走 `message`，Responses 走 `instructions`，Anthropic 走 `system`。
                如果没有显式设置 target，但显式 role 是 `user` 或 `assistant`，Responses / Anthropic 会保持消息注入语义。
              </p>
            </div>
            <div class="flex flex-col gap-2 md:col-span-2">
              <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">提示注入角色 (prompt_injection_role)</label>
              <select
                :value="getPromptInjectionRoleMode(configForm.features.prompt_injection_role)"
                @change="updateGlobalPromptInjectionRole(($event.target as HTMLSelectElement).value)"
                class="h-12 w-full md:w-1/2 font-mono px-4 bg-white/10 border-b-2 border-white/20 text-white rounded-md"
              >
                <option value="">默认</option>
                <option value="system">system</option>
                <option value="user">user</option>
                <option value="assistant">assistant</option>
                <option value="custom">自定义</option>
              </select>
              <input
                v-if="getPromptInjectionRoleMode(configForm.features.prompt_injection_role) === 'custom'"
                v-model="configForm.features.prompt_injection_role"
                type="text"
                placeholder="输入自定义角色"
                class="h-12 px-4 bg-white/10 border-b-2 border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md font-mono"
              />
            </div>
            <div class="md:col-span-2 rounded-xl border border-bitcoin/20 bg-bitcoin/5 p-4 md:p-5 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <div class="text-sm font-semibold text-white">Prompt 配置已迁移到独立页面</div>
                <div class="mt-1 text-xs leading-6 text-white/60">
                  全局 soft-tool 协议、默认 profile、默认模板内容和全部 prompt profiles 都统一在专用页面维护，避免这里重复展示造成误导。
                </div>
              </div>
              <router-link
                to="/prompts"
                class="inline-flex items-center justify-center rounded-md border border-bitcoin/30 bg-bitcoin/10 px-4 py-2 text-sm font-medium text-bitcoin transition-colors hover:bg-bitcoin/20"
              >
                打开 Prompt 模板页面
              </router-link>
            </div>
            <div class="md:col-span-2 rounded-xl border border-white/10 bg-black/30 p-4 md:p-5 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <div class="text-sm font-semibold text-white">上游路由与绑定也使用独立页面</div>
                <div class="mt-1 text-xs leading-6 text-white/60">
                  upstream 基础地址、模型清单、client key 绑定、软工具协议覆盖和 prompt profile 绑定，请统一到专用页面维护。
                </div>
              </div>
              <router-link
                to="/upstreams"
                class="inline-flex items-center justify-center rounded-md border border-white/15 bg-white/5 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-white/10"
              >
                打开 Upstreams 页面
              </router-link>
            </div>
          </div>
        </div>
      </transition>

      <!-- Auth Settings -->
      <transition name="fade" mode="out-in">
        <div v-show="activeConfigTab === 'auth'" class="space-y-6">
          <div class="bg-darkmatter border border-white/10 rounded-lg overflow-hidden transition-all duration-300 mb-12">
            <div class="px-6 py-5 border-b border-white/5 relative bg-black/40 flex items-center gap-3">
              <KeyRound class="w-5 h-5 text-bitcoin" />
              <h2 class="font-heading font-semibold text-xl text-white">修改 admin 口令</h2>
            </div>
            <form class="p-6 md:p-8 max-w-2xl flex flex-col gap-5" @submit.prevent="changeAdminPassword">
              <p class="text-sm text-white/70 leading-relaxed">客户端访问密钥已改为在每个 upstream 内单独绑定。这里仅保留 admin 登录口令管理。修改成功后将立即退出当前登录态，并跳回登录页重新登录。</p>

              <div class="flex flex-col gap-2">
                <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">当前口令</label>
                <input v-model="passwordForm.currentPassword" type="password" autocomplete="current-password" class="h-12 px-4 bg-white/10 border-b-2 border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md" />
              </div>

              <div class="flex flex-col gap-2">
                <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">新口令</label>
                <input v-model="passwordForm.newPassword" type="password" autocomplete="new-password" class="h-12 px-4 bg-white/10 border-b-2 border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md" />
              </div>

              <div class="flex flex-col gap-2">
                <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">确认新口令</label>
                <input v-model="passwordForm.confirmNewPassword" type="password" autocomplete="new-password" class="h-12 px-4 bg-white/10 border-b-2 border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md" />
              </div>

              <div>
                <button
                  type="submit"
                  class="bg-bitcoin hover:bg-burnt text-white font-medium text-sm px-6 py-3 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                  :disabled="isUpdatingPassword"
                >
                  {{ isUpdatingPassword ? '提交中...' : '修改 admin 口令' }}
                </button>
              </div>
            </form>
          </div>
        </div>
      </transition>

    </div>
  </div>
</template>