<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ChevronDown, ChevronRight, KeyRound, Plus, RefreshCw, Save, Trash2 } from 'lucide-vue-next'
import { cloneConfig, configToForm, createEmptyUpstreamForm, formToConfig } from '../utils/config-form'
import type { AppConfig, ConfigFormModel, SoftToolPromptProfileFormModel, UpstreamFormModel } from '../types/config'
import { updateAdminPassword } from '../utils/admin-auth'
import { showMessage } from '../utils/message'

type PromptInjectionTarget = 'auto' | 'message' | 'system' | 'instructions'
type UpstreamProtocol = 'openai_compat' | 'responses' | 'anthropic'
type UpstreamModelsSyncResponse = {
  status: string
  models: string[]
}

const router = useRouter()

const rawConfig = ref<AppConfig | null>(null)
const configForm = ref<ConfigFormModel | null>(null)
const isSaving = ref(false)
const isUpdatingPassword = ref(false)
const activeConfigTab = ref('server')
const expandedUpstreamPanels = ref<Record<string, boolean>>({})
const passwordForm = ref({
  currentPassword: '',
  newPassword: '',
  confirmNewPassword: ''
})

const getPromptInjectionRoleMode = (value: string) => {
  const trimmed = value?.trim()
  if (!trimmed) return ''
  if (trimmed === 'system' || trimmed === 'user' || trimmed === 'assistant') {
    return trimmed
  }
  return 'custom'
}

// 全局 target 选项（不区分协议，上游级别会按协议过滤）
const globalPromptInjectionTargetOptions: Array<{ value: PromptInjectionTarget | ''; label: string }> = [
  { value: '', label: '默认 (auto)' },
  { value: 'auto', label: 'auto' },
  { value: 'message', label: 'message' },
  { value: 'system', label: 'system' },
  { value: 'instructions', label: 'instructions' }
]

const upstreamProtocolOptions: Array<{ value: UpstreamProtocol; label: string; desc: string }> = [
  { value: 'openai_compat', label: 'OpenAI 兼容', desc: '转为 Chat Completions 格式，支持所有入口协议' },
  { value: 'responses', label: 'Responses', desc: '原生透传 Responses API，仅接受 /v1/responses 入口' },
  { value: 'anthropic', label: 'Anthropic', desc: '原生透传 Anthropic Messages API，仅接受 /v1/messages 入口' }
]

const comparePromptProfiles = (a: SoftToolPromptProfileFormModel, b: SoftToolPromptProfileFormModel) => {
  const aKey = a.id.trim() || a.originalID.trim() || a.name.trim()
  const bKey = b.id.trim() || b.originalID.trim() || b.name.trim()
  return aKey.localeCompare(bKey, 'zh-CN', { numeric: true, sensitivity: 'base' })
}

const sortPromptProfiles = (form: ConfigFormModel) => {
  form.promptProfiles.sort(comparePromptProfiles)
}

const promptProfileOptions = computed(() => {
  if (!configForm.value) {
    return [] as Array<{ value: string; label: string; enabled: boolean; protocol: string }>
  }
  return configForm.value.promptProfiles.map((profile) => ({
    value: profile.id,
    label: profile.name.trim() || profile.id.trim() || '未命名 profile',
    enabled: Boolean(profile.enabled),
    protocol: profile.protocol.trim()
  }))
})

const normalizePromptInjectionTarget = (value: string): PromptInjectionTarget | '' => {
  if (value === 'auto' || value === 'message' || value === 'system' || value === 'instructions') {
    return value
  }
  return ''
}

// 按上游协议返回合法的 target 选项
const targetOptionsForProtocol = (protocol: string): Array<{ value: string; label: string; desc: string }> => {
  switch (protocol) {
    case 'openai_compat':
      return [
        { value: 'message', label: 'message（固定）', desc: 'OpenAI Chat 只能作为 message 注入' }
      ]
    case 'responses':
      return [
        { value: '', label: '默认 / 继承', desc: '' },
        { value: 'auto', label: 'auto', desc: '→ instructions' },
        { value: 'instructions', label: 'instructions', desc: '注入到 instructions 字段' },
        { value: 'message', label: 'message', desc: '注入为 input 消息' }
      ]
    case 'anthropic':
      return [
        { value: '', label: '默认 / 继承', desc: '' },
        { value: 'auto', label: 'auto', desc: '→ system' },
        { value: 'system', label: 'system', desc: '注入到 system 字段' },
        { value: 'message', label: 'message', desc: '注入为 messages 消息' }
      ]
    default:
      return [
        { value: '', label: '默认 / 继承', desc: '' },
        { value: 'auto', label: 'auto', desc: '' },
        { value: 'message', label: 'message', desc: '' }
      ]
  }
}

// 按上游协议 + target 返回合法的 role 选项
type RoleOption = { value: string; label: string }
const roleOptionsForProtocol = (protocol: string, target: string): RoleOption[] => {
  const inherit: RoleOption = { value: '', label: '默认 / 继承' }

  // target 不是 message 时，role 无意义
  const effectiveTarget = target || 'auto'
  if (effectiveTarget !== 'message') {
    return [inherit]
  }

  switch (protocol) {
    case 'openai_compat':
      return [
        inherit,
        { value: 'system', label: 'system' },
        { value: 'user', label: 'user' },
        { value: 'assistant', label: 'assistant' },
        { value: 'developer', label: 'developer' },
        { value: 'custom', label: '自定义' }
      ]
    case 'responses':
      return [
        inherit,
        { value: 'developer', label: 'developer' },
        { value: 'user', label: 'user' },
        { value: 'assistant', label: 'assistant' }
      ]
    case 'anthropic':
      return [
        inherit,
        { value: 'user', label: 'user' },
        { value: 'assistant', label: 'assistant' }
      ]
    default:
      return [
        inherit,
        { value: 'system', label: 'system' },
        { value: 'user', label: 'user' },
        { value: 'assistant', label: 'assistant' },
        { value: 'custom', label: '自定义' }
      ]
  }
}

// 协议切换时自动修正不兼容的 target/role
const onUpstreamProtocolChange = (upstream: UpstreamFormModel) => {
  const protocol = upstream.upstreamProtocol || 'openai_compat'
  const validTargets = targetOptionsForProtocol(protocol).map(o => o.value)
  if (!validTargets.includes(upstream.promptInjectionTarget)) {
    upstream.promptInjectionTarget = validTargets[0] ?? ''
  }
  const validRoles = roleOptionsForProtocol(protocol, upstream.promptInjectionTarget).map(o => o.value)
  const currentMode = getPromptInjectionRoleMode(upstream.promptInjectionRole)
  if (!validRoles.includes(currentMode)) {
    upstream.promptInjectionRole = ''
  }
}

// target 切换时自动修正不兼容的 role
const onUpstreamTargetChange = (upstream: UpstreamFormModel) => {
  const protocol = upstream.upstreamProtocol || 'openai_compat'
  const validRoles = roleOptionsForProtocol(protocol, upstream.promptInjectionTarget).map(o => o.value)
  const currentMode = getPromptInjectionRoleMode(upstream.promptInjectionRole)
  if (!validRoles.includes(currentMode)) {
    upstream.promptInjectionRole = ''
  }
}

const updatePromptInjectionRoleMode = (mode: string, onUpdate: (next: string) => void) => {
  if (mode === 'custom') {
    onUpdate('custom')
    return
  }
  onUpdate(mode)
}

const updateGlobalPromptInjectionRole = (mode: string) => {
  if (!configForm.value) {
    return
  }

  updatePromptInjectionRoleMode(mode, (next) => {
    configForm.value!.features.prompt_injection_role = next === 'custom' ? '' : next
  })
}

const splitNonEmptyLines = (value: string) =>
  value
    .split('\n')
    .map((item) => item.trim())
    .filter((item) => item.length > 0)

const countNonEmptyLines = (value: string) =>
  splitNonEmptyLines(value).length

const syncedModelSet = (upstream: UpstreamFormModel) => new Set(upstream.syncedModels)

const selectedSyncedModels = (upstream: UpstreamFormModel) => {
  const managed = syncedModelSet(upstream)
  return splitNonEmptyLines(upstream.modelsText).filter((modelID) => managed.has(modelID))
}

const selectedSyncedModelsCount = (upstream: UpstreamFormModel) =>
  selectedSyncedModels(upstream).length

const isSyncedModelSelected = (upstream: UpstreamFormModel, modelID: string) =>
  selectedSyncedModels(upstream).includes(modelID)

const applySyncedModelSelection = (upstream: UpstreamFormModel, nextSelected: string[]) => {
  const managed = syncedModelSet(upstream)
  const manualEntries = splitNonEmptyLines(upstream.modelsText).filter((entry) => !managed.has(entry))
  const selectedSet = new Set(nextSelected)
  const selectedInDisplayOrder = upstream.syncedModels.filter((modelID) => selectedSet.has(modelID))
  upstream.modelsText = [...manualEntries, ...selectedInDisplayOrder].join('\n')
}

const updateSyncedModelSelection = (upstream: UpstreamFormModel, modelID: string, checked: boolean) => {
  const next = new Set(selectedSyncedModels(upstream))
  if (checked) {
    next.add(modelID)
  } else {
    next.delete(modelID)
  }
  applySyncedModelSelection(upstream, Array.from(next))
}

const selectAllSyncedModels = (upstream: UpstreamFormModel) => {
  applySyncedModelSelection(upstream, upstream.syncedModels)
}

const clearSyncedModelSelection = (upstream: UpstreamFormModel) => {
  applySyncedModelSelection(upstream, [])
}

const syncUpstreamModels = async (upstream: UpstreamFormModel) => {
  const title = upstream.name || upstream.baseURL || '当前上游'
  if (!upstream.baseURL.trim()) {
    showMessage.warning('请先填写 BaseURL，再同步模型列表。', '无法同步上游模型')
    return
  }
  if (upstream.isSyncingModels) {
    return
  }

  upstream.isSyncingModels = true
  upstream.modelSyncError = ''

  try {
    const res = await fetch('/admin/api/upstream-models', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        base_url: upstream.baseURL,
        api_key: upstream.apiKey
      })
    })

    if (!res.ok) {
      const err = await res.json()
      throw new Error(err?.error?.message || err?.error?.code || `HTTP ${res.status}`)
    }

    const payload = await res.json() as UpstreamModelsSyncResponse
    upstream.syncedModels = Array.isArray(payload.models) ? payload.models : []
    upstream.modelSyncLoaded = true
    upstream.modelSyncError = ''

    if (upstream.syncedModels.length === 0) {
      showMessage.warning('上游返回了空模型列表。', `${title} 模型同步`)
      return
    }

    showMessage.success(`已同步 ${upstream.syncedModels.length} 个模型。`, `${title} 模型同步`)
  } catch (e: any) {
    upstream.modelSyncLoaded = false
    upstream.modelSyncError = e.message || '同步失败'
    showMessage.error(upstream.modelSyncError, `${title} 模型同步失败`)
  } finally {
    upstream.isSyncingModels = false
  }
}

const getUpstreamPanelKey = (upstream: UpstreamFormModel, idx: number) =>
  upstream.originalName.trim() || `draft:${idx}`

const syncExpandedUpstreamPanels = (form: ConfigFormModel) => {
  const nextState: Record<string, boolean> = {}
  form.upstreams.forEach((upstream, idx) => {
    const key = getUpstreamPanelKey(upstream, idx)
    nextState[key] = expandedUpstreamPanels.value[key] ?? true
  })
  expandedUpstreamPanels.value = nextState
}

const isUpstreamExpanded = (upstream: UpstreamFormModel, idx: number) =>
  expandedUpstreamPanels.value[getUpstreamPanelKey(upstream, idx)] ?? true

const toggleUpstreamExpanded = (upstream: UpstreamFormModel, idx: number) => {
  const key = getUpstreamPanelKey(upstream, idx)
  expandedUpstreamPanels.value = {
    ...expandedUpstreamPanels.value,
    [key]: !isUpstreamExpanded(upstream, idx)
  }
}

const getEffectiveUpstreamProtocol = (upstream: UpstreamFormModel) =>
  upstream.upstreamProtocol.trim() || 'openai_compat'

const getFeaturePromptInjectionTargetLabel = (value: string) =>
  normalizePromptInjectionTarget(value) || 'auto'

const getEffectiveUpstreamPromptInjectionTarget = (upstream: UpstreamFormModel) =>
  normalizePromptInjectionTarget(upstream.promptInjectionTarget) || getFeaturePromptInjectionTargetLabel(configForm.value?.features.prompt_injection_target ?? '')

const getEffectiveSoftToolPromptProfileBinding = (upstream: UpstreamFormModel) =>
  upstream.softToolPromptProfileID.trim() || configForm.value?.features.default_soft_tool_prompt_profile_id?.trim() || '无'

const generateRandomKey = (): string => {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  const hex = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
  return `sk-${hex}`
}

const appendRandomClientKey = (upstream: UpstreamFormModel) => {
  const key = generateRandomKey()
  const current = upstream.clientKeysText.trim()
  upstream.clientKeysText = current ? `${current}\n${key}` : key
}

const loadConfig = async () => {
  try {
    const res = await fetch('/admin/api/config')
    if (!res.ok) throw new Error('API request failed')
    const config = await res.json() as AppConfig
    rawConfig.value = cloneConfig(config)
    configForm.value = configToForm(config)
    sortPromptProfiles(configForm.value)
    syncExpandedUpstreamPanels(configForm.value)
  } catch (e: any) {
    showMessage.error(e.message, '加载配置失败')
  }
}

const addUpstream = () => {
  if (!configForm.value) return
  configForm.value.upstreams.push(createEmptyUpstreamForm())
  syncExpandedUpstreamPanels(configForm.value)
}

const removeUpstream = (idx: number) => {
  if (!configForm.value) return
  configForm.value.upstreams.splice(idx, 1)
  syncExpandedUpstreamPanels(configForm.value)
}

const saveConfig = async () => {
  if (!configForm.value || !rawConfig.value) return
  isSaving.value = true

  try {
    sortPromptProfiles(configForm.value)
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
    sortPromptProfiles(configForm.value)
    syncExpandedUpstreamPanels(configForm.value)
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

      <!-- Upstream Settings -->
      <transition name="fade" mode="out-in">
        <div v-show="activeConfigTab === 'upstreams'" class="bg-darkmatter border border-white/10 rounded-lg overflow-hidden transition-all duration-300 mb-12">
          <div class="px-6 py-5 border-b border-white/5 relative bg-black/40 flex items-center justify-between">
            <h2 class="font-heading font-semibold text-xl text-white">上游服务配置</h2>
            <button class="bg-white/10 hover:bg-white/20 text-white font-medium text-xs px-4 py-2 rounded-md border border-white/20 transition-all flex items-center gap-1.5" @click="addUpstream">
              <Plus class="w-3.5 h-3.5" />
              添加节点
            </button>
          </div>
          <div class="p-6 md:p-8">
            <p class="text-xs text-muted mb-6 font-mono border-l-2 border-gold pl-3 py-1 bg-gold/5 inline-block">请确保至少包含一个设为“默认(is_default)”的节点，负责接管未命中的路由。</p>
            
            <div class="flex flex-col gap-6">
              <div v-for="(up, idx) in configForm.upstreams" :key="idx" class="bg-black/30 border border-white/10 py-6 px-6 lg:px-8 rounded-xl hover:border-white/30 transition-colors">
                <div class="flex items-start justify-between gap-4 mb-6 pb-4 border-b border-white/5">
                  <button type="button" class="min-w-0 flex-1 text-left" @click="toggleUpstreamExpanded(up, Number(idx))">
                    <div class="flex items-center gap-3">
                      <component :is="isUpstreamExpanded(up, Number(idx)) ? ChevronDown : ChevronRight" class="w-4 h-4 text-bitcoin shrink-0" />
                      <span class="w-6 h-6 rounded-full bg-white/10 flex items-center justify-center font-mono text-xs text-white/70">{{ Number(idx) + 1 }}</span>
                      <span class="font-medium text-lg font-heading text-white">{{ up.name || '未命名节点' }}</span>
                      <span v-if="up.isDefault" class="px-2 py-1 text-[10px] font-mono rounded border border-bitcoin/30 bg-bitcoin/10 text-bitcoin">default</span>
                      <span class="px-2 py-1 text-[10px] font-mono rounded border border-white/10 bg-white/5 text-white/60">{{ getEffectiveUpstreamProtocol(up) }}</span>
                    </div>
                    <div class="mt-3 pl-10 flex flex-wrap gap-x-4 gap-y-1 text-xs text-white/55 font-mono">
                      <span>{{ up.baseURL || '未配置基础 URL' }}</span>
                      <span>{{ countNonEmptyLines(up.modelsText) }} models</span>
                      <span>{{ countNonEmptyLines(up.clientKeysText) }} client keys</span>
                      <span>{{ isUpstreamExpanded(up, Number(idx)) ? '已展开' : '已折叠' }}</span>
                    </div>
                  </button>
                  <div class="flex items-center gap-2 shrink-0">
                    <button
                      type="button"
                      class="px-3 py-2 text-[11px] font-medium rounded-md border border-white/10 bg-white/5 text-white/75 hover:text-white hover:bg-white/10 transition-colors"
                      @click="toggleUpstreamExpanded(up, Number(idx))"
                    >
                      {{ isUpstreamExpanded(up, Number(idx)) ? '收起' : '展开' }}
                    </button>
                    <button class="text-muted hover:text-red-400 p-2 rounded-lg hover:bg-white/5 transition-colors" @click="removeUpstream(Number(idx))" title="删除节点">
                      <Trash2 class="w-4 h-4" />
                    </button>
                  </div>
                </div>

                <div v-show="isUpstreamExpanded(up, Number(idx))" class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-12 gap-x-6 gap-y-5">
                  <div class="flex flex-col gap-2 xl:col-span-3">
                    <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">标识 (Name)</label>
                    <input type="text" v-model="up.name" placeholder="如: openai/azure/gemini" class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono" />
                  </div>
                  
                  <div class="flex flex-col gap-2 xl:col-span-5">
                    <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">基础 URL (BaseURL)</label>
                    <input type="text" v-model="up.baseURL" placeholder="https://api.openai.com/v1" class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono" />
                  </div>

                  <div class="flex flex-col gap-2 xl:col-span-4 lg:col-span-2">
                    <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">上游协议 (upstream_protocol)</label>
                    <select v-model="up.upstreamProtocol" @change="onUpstreamProtocolChange(up)" class="h-10 w-full font-mono px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white rounded-md">
                      <option v-for="option in upstreamProtocolOptions" :key="`upstream-protocol-${option.value}`" :value="option.value">
                        {{ option.label }} ({{ option.value }})
                      </option>
                    </select>
                    <div class="text-xs text-white/55 leading-5">
                      {{ upstreamProtocolOptions.find(o => o.value === (up.upstreamProtocol || 'openai_compat'))?.desc }}
                    </div>
                  </div>
                  
                  <div class="flex flex-col gap-2 xl:col-span-4 lg:col-span-2">
                    <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">访问密钥 (APIKey)</label>
                    <input type="password" v-model="up.apiKey" placeholder="默认使用的 API Key" class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono" />
                  </div>
                  
                  <div class="flex flex-col gap-2 xl:col-span-6">
                    <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">描述说明 (Description)</label>
                    <input type="text" v-model="up.description" placeholder="内部备注..." class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all font-body" />
                  </div>

                  <div class="flex items-center xl:col-span-6 pt-6">
                    <label class="flex items-center gap-3 cursor-pointer group">
                      <div class="relative flex items-center justify-center">
                        <input type="checkbox" v-model="up.isDefault" class="peer sr-only">
                        <div class="w-5 h-5 rounded border border-white/30 peer-checked:bg-bitcoin peer-checked:border-bitcoin flex items-center justify-center transition-all bg-black/50 shadow-inner group-hover:border-white/50">
                          <svg class="w-3.5 h-3.5 text-white opacity-0 peer-checked:opacity-100 drop-shadow-md" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path></svg>
                        </div>
                      </div>
                      <span class="text-sm font-medium text-white/80 group-hover:text-white transition-colors">设为默认接管路由</span>
                    </label>
                  </div>

                  <div class="flex flex-col gap-2 xl:col-span-12">
                    <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">软工具调用协议覆盖 (soft_tool_calling_protocol)</label>
                    <select v-model="up.softToolProtocol" class="h-10 w-full md:w-1/2 font-mono px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white rounded-md">
                      <option value="">继承全局默认</option>
                      <option value="xml">xml</option>
                      <option value="sentinel_json">sentinel_json</option>
                      <option value="markdown_block">markdown_block</option>
                    </select>
                  </div>

                  <div class="flex flex-col gap-2 xl:col-span-12">
                    <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">Prompt Profile 绑定 (soft_tool_prompt_profile_id)</label>
                    <select v-model="up.softToolPromptProfileID" class="h-10 w-full md:w-1/2 font-mono px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white rounded-md">
                      <option value="">继承全局默认 / 不绑定</option>
                      <option v-for="profile in promptProfileOptions" :key="`config-up-prompt-profile-${profile.value || 'empty'}-${idx}`" :value="profile.value">
                        {{ profile.label }}{{ profile.protocol ? ` (${profile.protocol})` : '' }}{{ profile.enabled ? '' : ' [disabled]' }}
                      </option>
                    </select>
                    <div class="text-xs text-white/55 leading-5">
                      当前生效绑定：<code>{{ getEffectiveSoftToolPromptProfileBinding(up) }}</code>
                    </div>
                  </div>

                  <div class="flex flex-col gap-2 xl:col-span-12">
                    <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">提示注入目标 (prompt_injection_target)</label>
                    <select v-model="up.promptInjectionTarget" @change="onUpstreamTargetChange(up)" class="h-10 w-full md:w-1/2 font-mono px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white rounded-md">
                      <option v-for="option in targetOptionsForProtocol(up.upstreamProtocol || 'openai_compat')" :key="`up-target-${option.value || 'inherit'}-${idx}`" :value="option.value">
                        {{ option.label }}{{ option.desc ? ` — ${option.desc}` : '' }}
                      </option>
                    </select>
                    <div class="text-xs text-white/55 leading-5">
                      当前生效目标：<code>{{ getEffectiveUpstreamPromptInjectionTarget(up) }}</code>
                    </div>
                  </div>

                  <div class="flex flex-col gap-2 xl:col-span-12">
                    <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">提示注入角色 (prompt_injection_role)</label>
                    <select
                      :value="getPromptInjectionRoleMode(up.promptInjectionRole)"
                      @change="(event) => updatePromptInjectionRoleMode((event.target as HTMLSelectElement).value, (next) => {
                        up.promptInjectionRole = next === 'custom' ? '' : next
                      })"
                      class="h-10 w-full md:w-1/2 font-mono px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white rounded-md"
                    >
                      <option v-for="option in roleOptionsForProtocol(up.upstreamProtocol || 'openai_compat', up.promptInjectionTarget)" :key="`up-role-${option.value || 'inherit'}-${idx}`" :value="option.value">
                        {{ option.label }}
                      </option>
                    </select>
                    <input
                      v-if="getPromptInjectionRoleMode(up.promptInjectionRole) === 'custom'"
                      type="text"
                      v-model="up.promptInjectionRole"
                      placeholder="输入自定义角色"
                      class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono"
                    />
                  </div>

                  <div class="flex flex-col gap-2 xl:col-span-12">
                    <div class="flex items-center justify-between">
                      <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">客户端访问密钥绑定 (client_keys) / 每行一个</label>
                      <button
                        type="button"
                        class="px-2.5 py-1 text-[11px] font-medium rounded-md border border-bitcoin/30 bg-bitcoin/10 text-bitcoin hover:bg-bitcoin/20 transition-colors flex items-center gap-1.5"
                        @click="appendRandomClientKey(up)"
                      >
                        <KeyRound class="w-3 h-3" />
                        生成随机密钥
                      </button>
                    </div>
                    <textarea v-model="up.clientKeysText" rows="4" placeholder="sk-project-a&#10;sk-project-b" class="p-4 bg-white/10 border-b-[1.5px] border-white/20 text-white text-xs focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono resize-y leading-relaxed"></textarea>
                  </div>

                  <div class="flex flex-col gap-2 xl:col-span-12">
                    <div class="rounded-xl border border-white/10 bg-black/30 p-4 md:p-5 space-y-4">
                      <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                        <div class="space-y-1">
                          <div class="text-sm font-semibold text-white">同步上游模型列表</div>
                          <div class="text-xs leading-5 text-white/55">
                            从当前 <code>BaseURL/models</code> 拉取后直接勾选。勾选区只维护与上游同名的模型条目，像 <code>alias:actual</code> 这种手工映射仍可继续写在下方文本框里。
                          </div>
                        </div>
                        <div class="flex flex-wrap gap-2">
                          <button
                            type="button"
                            class="px-3 py-2 text-[11px] font-medium rounded-md border border-bitcoin/30 bg-bitcoin/10 text-bitcoin hover:bg-bitcoin/20 transition-colors disabled:opacity-60 disabled:cursor-not-allowed flex items-center gap-2"
                            :disabled="up.isSyncingModels"
                            @click="syncUpstreamModels(up)"
                          >
                            <RefreshCw class="w-3.5 h-3.5" :class="up.isSyncingModels ? 'animate-spin' : ''" />
                            {{ up.isSyncingModels ? '同步中...' : '同步模型' }}
                          </button>
                          <button
                            v-if="up.syncedModels.length > 0"
                            type="button"
                            class="px-3 py-2 text-[11px] font-medium rounded-md border border-white/10 bg-white/5 text-white/75 hover:text-white hover:bg-white/10 transition-colors"
                            @click="selectAllSyncedModels(up)"
                          >
                            全选已同步项
                          </button>
                          <button
                            v-if="up.syncedModels.length > 0"
                            type="button"
                            class="px-3 py-2 text-[11px] font-medium rounded-md border border-white/10 bg-transparent text-white/60 hover:text-white hover:bg-white/5 transition-colors"
                            @click="clearSyncedModelSelection(up)"
                          >
                            清空已同步项
                          </button>
                        </div>
                      </div>

                      <div v-if="up.modelSyncError" class="rounded-lg border border-red-400/30 bg-red-500/10 px-4 py-3 text-xs text-red-100">
                        {{ up.modelSyncError }}
                      </div>

                      <div v-if="up.syncedModels.length > 0" class="space-y-3">
                        <div class="flex flex-wrap items-center justify-between gap-3 text-xs text-white/60">
                          <span>已同步 {{ up.syncedModels.length }} 个模型</span>
                          <span>已选 {{ selectedSyncedModelsCount(up) }} 个</span>
                        </div>
                        <div class="max-h-64 overflow-y-auto rounded-lg border border-white/10 bg-black/30 p-3">
                          <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-2">
                            <label
                              v-for="modelID in up.syncedModels"
                              :key="modelID"
                              class="flex items-center gap-3 rounded-md border border-white/8 bg-white/5 px-3 py-2 text-xs font-mono text-white/80 hover:border-white/20 hover:text-white transition-colors"
                            >
                              <input
                                type="checkbox"
                                class="h-4 w-4 rounded border-white/20 bg-black/40 text-bitcoin focus:ring-bitcoin/50"
                                :checked="isSyncedModelSelected(up, modelID)"
                                @change="updateSyncedModelSelection(up, modelID, ($event.target as HTMLInputElement).checked)"
                              />
                              <span class="truncate">{{ modelID }}</span>
                            </label>
                          </div>
                        </div>
                      </div>

                      <div v-else-if="up.modelSyncLoaded" class="rounded-lg border border-white/10 bg-white/5 px-4 py-3 text-xs text-white/60">
                        上游返回了空模型列表，可以继续手工填写下方模型清单。
                      </div>
                    </div>

                    <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">可路由模型清单 (Models) / 每行一个</label>
                    <textarea v-model="up.modelsText" rows="4" placeholder="gpt-4o&#10;gpt-3.5-turbo&#10;alias-model:actual-upstream-model" class="p-4 bg-white/10 border-b-[1.5px] border-white/20 text-white text-xs focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono resize-y leading-relaxed"></textarea>
                  </div>

                  <div class="flex flex-col gap-2 xl:col-span-12">
                    <div class="rounded-lg border border-white/10 bg-white/5 px-4 py-3 text-xs leading-5 text-white/55">
                      同步勾选会直接更新上面的文本框；未命中的手工条目会保留。需要做别名映射时，继续使用 <code>别名:上游模型名</code> 格式。
                    </div>
                  </div>
                </div>
              </div>
            </div>
            
            <div v-if="configForm.upstreams.length === 0" class="flex flex-col items-center justify-center py-16 border border-dashed border-white/10 rounded-xl bg-white/5 mt-4">
              <span class="text-muted font-mono text-sm mb-4">当前未配置任何上游服务节点</span>
              <button class="bg-white/10 hover:bg-white/20 text-white font-medium text-xs px-4 py-2 rounded-md border border-white/20 transition-colors" @click="addUpstream">添加第一个节点</button>
            </div>
          </div>
        </div>
      </transition>

    </div>
  </div>
</template>
