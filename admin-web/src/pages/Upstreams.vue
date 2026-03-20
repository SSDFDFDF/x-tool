<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ChevronDown, ChevronRight, KeyRound, Plus, RefreshCw, Save, Trash2 } from 'lucide-vue-next'
import { cloneConfig, configToForm, createEmptyUpstreamForm, formToConfig } from '../utils/config-form'
import type { AppConfig, ConfigFormModel, UpstreamFormModel } from '../types/config'
import { showMessage } from '../utils/message'

type PromptInjectionTarget = 'auto' | 'message' | 'system' | 'instructions'
type UpstreamProtocol = 'openai_compat' | 'responses' | 'anthropic'
type UpstreamModelsSyncResponse = {
  status: string
  models: string[]
}
type RoleOption = { value: string; label: string }

const rawConfig = ref<AppConfig | null>(null)
const configForm = ref<ConfigFormModel | null>(null)
const isSaving = ref(false)
const expandedUpstreamPanels = ref<Record<string, boolean>>({})

const upstreamProtocolOptions: Array<{ value: UpstreamProtocol; label: string; desc: string }> = [
  { value: 'openai_compat', label: 'OpenAI 兼容', desc: '转为 Chat Completions 格式，支持所有入口协议' },
  { value: 'responses', label: 'Responses', desc: '原生透传 Responses API，仅接受 /v1/responses 入口' },
  { value: 'anthropic', label: 'Anthropic', desc: '原生透传 Anthropic Messages API，仅接受 /v1/messages 入口' }
]

const softToolPromptProfileOptions = computed(() => {
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

const splitNonEmptyLines = (value: string) =>
  value
    .split('\n')
    .map((item) => item.trim())
    .filter((item) => item.length > 0)

const countNonEmptyLines = (value: string) => splitNonEmptyLines(value).length

const getPromptInjectionRoleMode = (value: string) => {
  const trimmed = value?.trim()
  if (!trimmed) return ''
  if (trimmed === 'system' || trimmed === 'user' || trimmed === 'assistant' || trimmed === 'developer') {
    return trimmed
  }
  return 'custom'
}

const compareUpstreamByID = (a: UpstreamFormModel, b: UpstreamFormModel) => {
  const readRawID = (upstream: UpstreamFormModel) => {
    const maybeID = (upstream as UpstreamFormModel & { id?: number | string }).id
    if (maybeID !== undefined && maybeID !== null && String(maybeID).trim() !== '') {
      return String(maybeID).trim()
    }
    const nameLikeID = upstream.name.trim() || upstream.originalName.trim()
    return nameLikeID
  }

  const aRaw = readRawID(a)
  const bRaw = readRawID(b)

  const aNum = Number(aRaw)
  const bNum = Number(bRaw)
  const aIsNum = aRaw !== '' && Number.isFinite(aNum)
  const bIsNum = bRaw !== '' && Number.isFinite(bNum)

  if (aIsNum && bIsNum) {
    return aNum - bNum
  }
  if (aIsNum && !bIsNum) {
    return -1
  }
  if (!aIsNum && bIsNum) {
    return 1
  }

  return aRaw.localeCompare(bRaw, 'zh-CN', { numeric: true, sensitivity: 'base' })
}

const sortUpstreamsByID = (form: ConfigFormModel) => {
  form.upstreams.sort(compareUpstreamByID)
}

const targetOptionsForProtocol = (protocol: string): Array<{ value: string; label: string; desc: string }> => {
  switch (protocol) {
    case 'openai_compat':
      return [{ value: 'message', label: 'message（固定）', desc: 'OpenAI Chat 只能作为 message 注入' }]
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

const roleOptionsForProtocol = (protocol: string, target: string): RoleOption[] => {
  const inherit: RoleOption = { value: '', label: '默认 / 继承' }
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

const onUpstreamProtocolChange = (upstream: UpstreamFormModel) => {
  const protocol = upstream.upstreamProtocol || 'openai_compat'
  const validTargets = targetOptionsForProtocol(protocol).map((option) => option.value)
  if (!validTargets.includes(upstream.promptInjectionTarget)) {
    upstream.promptInjectionTarget = validTargets[0] ?? ''
  }

  const validRoles = roleOptionsForProtocol(protocol, upstream.promptInjectionTarget).map((option) => option.value)
  const currentMode = getPromptInjectionRoleMode(upstream.promptInjectionRole)
  if (!validRoles.includes(currentMode)) {
    upstream.promptInjectionRole = ''
  }
}

const onUpstreamTargetChange = (upstream: UpstreamFormModel) => {
  const protocol = upstream.upstreamProtocol || 'openai_compat'
  const validRoles = roleOptionsForProtocol(protocol, upstream.promptInjectionTarget).map((option) => option.value)
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

const syncedModelSet = (upstream: UpstreamFormModel) => new Set(upstream.syncedModels)

const selectedSyncedModels = (upstream: UpstreamFormModel) => {
  const managed = syncedModelSet(upstream)
  return splitNonEmptyLines(upstream.modelsText).filter((modelID) => managed.has(modelID))
}

const selectedSyncedModelsCount = (upstream: UpstreamFormModel) => selectedSyncedModels(upstream).length

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

const getUpstreamPanelKey = (upstream: UpstreamFormModel, idx: number) => upstream.originalName.trim() || `draft:${idx}`

const syncExpandedUpstreamPanels = (form: ConfigFormModel) => {
  const nextState: Record<string, boolean> = {}
  form.upstreams.forEach((upstream, idx) => {
    const key = getUpstreamPanelKey(upstream, idx)
    nextState[key] = expandedUpstreamPanels.value[key] ?? false
  })
  expandedUpstreamPanels.value = nextState
}

const isUpstreamExpanded = (upstream: UpstreamFormModel, idx: number) =>
  expandedUpstreamPanels.value[getUpstreamPanelKey(upstream, idx)] ?? false

const toggleUpstreamExpanded = (upstream: UpstreamFormModel, idx: number) => {
  const key = getUpstreamPanelKey(upstream, idx)
  expandedUpstreamPanels.value = {
    ...expandedUpstreamPanels.value,
    [key]: !isUpstreamExpanded(upstream, idx)
  }
}

const expandAllUpstreams = () => {
  if (!configForm.value) return
  const nextState: Record<string, boolean> = {}
  configForm.value.upstreams.forEach((upstream, idx) => {
    nextState[getUpstreamPanelKey(upstream, idx)] = true
  })
  expandedUpstreamPanels.value = nextState
}

const collapseAllUpstreams = () => {
  if (!configForm.value) return
  const nextState: Record<string, boolean> = {}
  configForm.value.upstreams.forEach((upstream, idx) => {
    nextState[getUpstreamPanelKey(upstream, idx)] = false
  })
  expandedUpstreamPanels.value = nextState
}

const getEffectiveUpstreamProtocol = (upstream: UpstreamFormModel) => upstream.upstreamProtocol.trim() || 'openai_compat'

const getFeaturePromptInjectionTargetLabel = (value: string) => normalizePromptInjectionTarget(value) || 'auto'

const getEffectiveUpstreamPromptInjectionTarget = (upstream: UpstreamFormModel) =>
  normalizePromptInjectionTarget(upstream.promptInjectionTarget) || getFeaturePromptInjectionTargetLabel(configForm.value?.features.prompt_injection_target ?? '')

const getEffectiveSoftToolPromptProfileBinding = (upstream: UpstreamFormModel) =>
  upstream.softToolPromptProfileID.trim() || configForm.value?.features.default_soft_tool_prompt_profile_id?.trim() || '无'

const generateRandomKey = (): string => {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
  return `sk-${hex}`
}

const appendRandomClientKey = (upstream: UpstreamFormModel) => {
  const key = generateRandomKey()
  const current = upstream.clientKeysText.trim()
  upstream.clientKeysText = current ? `${current}\n${key}` : key
}

const totalModelsCount = computed(() => {
  if (!configForm.value) return 0
  return configForm.value.upstreams.reduce((sum, upstream) => sum + countNonEmptyLines(upstream.modelsText), 0)
})

const defaultUpstreamsCount = computed(() => {
  if (!configForm.value) return 0
  return configForm.value.upstreams.filter((upstream) => upstream.isDefault).length
})

const loadConfig = async () => {
  try {
    const res = await fetch('/admin/api/config')
    if (!res.ok) throw new Error('API request failed')
    const config = await res.json() as AppConfig
    rawConfig.value = cloneConfig(config)
    configForm.value = configToForm(config)
    sortUpstreamsByID(configForm.value)
    syncExpandedUpstreamPanels(configForm.value)
  } catch (e: any) {
    showMessage.error(e.message, '加载配置失败')
  }
}

const addUpstream = () => {
  if (!configForm.value) return
  configForm.value.upstreams.push(createEmptyUpstreamForm())
  syncExpandedUpstreamPanels(configForm.value)
  const idx = configForm.value.upstreams.length - 1
  const upstream = configForm.value.upstreams[idx]
  if (upstream) {
    expandedUpstreamPanels.value = {
      ...expandedUpstreamPanels.value,
      [getUpstreamPanelKey(upstream, idx)]: true
    }
  }
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
    sortUpstreamsByID(configForm.value)
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
    syncExpandedUpstreamPanels(configForm.value)
    window.dispatchEvent(new CustomEvent('admin:config-saved'))
    showMessage.success('上游配置已保存。')
  } catch (e: any) {
    showMessage.error(e.message, '保存失败')
  } finally {
    isSaving.value = false
  }
}

onMounted(() => {
  loadConfig()
})
</script>

<template>
  <div class="p-6 md:p-8 w-full h-full flex flex-col pt-[calc(3rem)] overflow-y-auto">
    <header class="flex flex-col gap-4 md:flex-row md:items-center md:justify-between mb-8 pb-4 border-b border-white/10 shrink-0">
      <div>
        <h1 class="font-heading font-semibold text-3xl md:text-4xl text-white">上游服务配置</h1>
        <p class="mt-2 text-sm text-white/60">独立管理路由目标、模型清单和密钥绑定。默认全部折叠，优先看摘要再逐个展开编辑。</p>
      </div>
      <button
        class="bg-bitcoin hover:bg-burnt text-white font-medium text-sm px-6 py-3 rounded-md transition-colors flex items-center justify-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
        :disabled="isSaving"
        @click="saveConfig"
      >
        <Save class="w-4 h-4" />
        {{ isSaving ? '保存中...' : '保存上游配置' }}
      </button>
    </header>

    <div v-if="configForm" class="space-y-6">
      <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div class="rounded-xl border border-white/10 bg-black/30 px-5 py-4">
          <div class="text-xs uppercase tracking-widest text-muted">节点总数</div>
          <div class="mt-2 text-2xl font-mono text-bitcoin">{{ configForm.upstreams.length }}</div>
        </div>
        <div class="rounded-xl border border-white/10 bg-black/30 px-5 py-4">
          <div class="text-xs uppercase tracking-widest text-muted">默认节点</div>
          <div class="mt-2 text-2xl font-mono text-white">{{ defaultUpstreamsCount }}</div>
        </div>
        <div class="rounded-xl border border-white/10 bg-black/30 px-5 py-4">
          <div class="text-xs uppercase tracking-widest text-muted">模型总条目</div>
          <div class="mt-2 text-2xl font-mono text-white">{{ totalModelsCount }}</div>
        </div>
      </div>

      <div class="bg-darkmatter border border-white/10 rounded-lg overflow-hidden">
        <div class="px-6 py-5 border-b border-white/5 bg-black/40 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <p class="text-xs text-muted font-mono border-l-2 border-gold pl-3 py-1 bg-gold/5">请确保至少包含一个设为“默认(is_default)”的节点，负责接管未命中的路由。</p>
          <div class="flex flex-wrap items-center gap-2">
            <button
              type="button"
              class="px-3 py-2 text-[11px] font-medium rounded-md border border-white/10 bg-white/5 text-white/75 hover:text-white hover:bg-white/10 transition-colors"
              @click="expandAllUpstreams"
            >
              展开全部
            </button>
            <button
              type="button"
              class="px-3 py-2 text-[11px] font-medium rounded-md border border-white/10 bg-white/5 text-white/75 hover:text-white hover:bg-white/10 transition-colors"
              @click="collapseAllUpstreams"
            >
              收起全部
            </button>
            <button
              class="bg-white/10 hover:bg-white/20 text-white font-medium text-xs px-4 py-2 rounded-md border border-white/20 transition-all flex items-center gap-1.5"
              @click="addUpstream"
            >
              <Plus class="w-3.5 h-3.5" />
              添加节点
            </button>
          </div>
        </div>

        <div class="p-6 md:p-8">
          <div class="flex flex-col gap-4">
            <div
              v-for="(up, idx) in configForm.upstreams"
              :key="idx"
              class="bg-black/30 border border-white/10 rounded-xl transition-colors hover:border-white/20"
            >
              <div class="px-5 py-4 border-b border-white/5">
                <div class="flex items-start justify-between gap-4">
                  <button type="button" class="min-w-0 flex-1 text-left" @click="toggleUpstreamExpanded(up, Number(idx))">
                    <div class="flex items-center gap-3">
                      <component :is="isUpstreamExpanded(up, Number(idx)) ? ChevronDown : ChevronRight" class="w-4 h-4 text-bitcoin shrink-0" />
                      <span class="w-6 h-6 rounded-full bg-white/10 flex items-center justify-center font-mono text-xs text-white/70">{{ Number(idx) + 1 }}</span>
                      <span class="truncate font-medium text-base font-heading text-white">{{ up.name || '未命名节点' }}</span>
                      <span v-if="up.isDefault" class="px-2 py-1 text-[10px] font-mono rounded border border-bitcoin/30 bg-bitcoin/10 text-bitcoin">default</span>
                      <span class="px-2 py-1 text-[10px] font-mono rounded border border-white/10 bg-white/5 text-white/60">{{ getEffectiveUpstreamProtocol(up) }}</span>
                    </div>
                    <div class="mt-3 pl-10 grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-x-4 gap-y-1 text-xs text-white/55 font-mono">
                      <span class="truncate">{{ up.baseURL || '未配置基础 URL' }}</span>
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
                    <button
                      type="button"
                      class="px-3 py-2 text-[11px] font-medium rounded-md border border-bitcoin/30 bg-bitcoin/10 text-bitcoin hover:bg-bitcoin/20 transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-1.5"
                      :disabled="isSaving"
                      @click="saveConfig"
                    >
                      <Save class="w-3.5 h-3.5" />
                      {{ isSaving ? '保存中...' : '保存' }}
                    </button>
                    <button class="text-muted hover:text-red-400 p-2 rounded-lg hover:bg-white/5 transition-colors" title="删除节点" @click="removeUpstream(Number(idx))">
                      <Trash2 class="w-4 h-4" />
                    </button>
                  </div>
                </div>
              </div>

              <div v-show="isUpstreamExpanded(up, Number(idx))" class="px-5 py-5 md:px-6 md:py-6 space-y-4">
                <section class="rounded-lg border border-white/10 bg-black/25 p-4 md:p-5 space-y-4">
                  <div class="text-[11px] uppercase tracking-widest text-muted font-semibold">基础信息</div>
                  <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-12 gap-x-6 gap-y-4">
                    <div class="flex flex-col gap-2 xl:col-span-3">
                      <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">标识 (Name)</label>
                      <input v-model="up.name" type="text" placeholder="如: openai/azure/gemini" class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono" />
                    </div>

                    <div class="flex flex-col gap-2 xl:col-span-6">
                      <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">基础 URL (BaseURL)</label>
                      <input v-model="up.baseURL" type="text" placeholder="https://api.openai.com/v1" class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono" />
                    </div>

                    <div class="flex flex-col gap-2 xl:col-span-3">
                      <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">访问密钥 (APIKey)</label>
                      <input v-model="up.apiKey" type="password" placeholder="默认使用的 API Key" class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono" />
                    </div>

                    <div class="flex flex-col gap-2 xl:col-span-8">
                      <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">描述说明 (Description)</label>
                      <input v-model="up.description" type="text" placeholder="内部备注..." class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all" />
                    </div>

                    <div class="flex items-end xl:col-span-4">
                      <label class="flex items-center gap-3 cursor-pointer group pb-2">
                        <div class="relative flex items-center justify-center">
                          <input v-model="up.isDefault" type="checkbox" class="peer sr-only">
                          <div class="w-5 h-5 rounded border border-white/30 peer-checked:bg-bitcoin peer-checked:border-bitcoin flex items-center justify-center transition-all bg-black/50 shadow-inner group-hover:border-white/50">
                            <svg class="w-3.5 h-3.5 text-white opacity-0 peer-checked:opacity-100 drop-shadow-md" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path></svg>
                          </div>
                        </div>
                        <span class="text-sm font-medium text-white/80 group-hover:text-white transition-colors">设为默认接管路由</span>
                      </label>
                    </div>
                  </div>
                </section>

                <section class="rounded-lg border border-white/10 bg-black/25 p-4 md:p-5 space-y-4">
                  <div class="text-[11px] uppercase tracking-widest text-muted font-semibold">协议与注入策略</div>
                  <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-12 gap-x-6 gap-y-4">
                    <div class="flex flex-col gap-2 xl:col-span-4">
                      <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">上游协议 (upstream_protocol)</label>
                      <select v-model="up.upstreamProtocol" class="h-10 w-full font-mono px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white rounded-md" @change="onUpstreamProtocolChange(up)">
                        <option v-for="option in upstreamProtocolOptions" :key="`upstream-protocol-${option.value}`" :value="option.value">
                          {{ option.label }} ({{ option.value }})
                        </option>
                      </select>
                      <div class="text-xs text-white/55 leading-5">
                        {{ upstreamProtocolOptions.find((option) => option.value === (up.upstreamProtocol || 'openai_compat'))?.desc }}
                      </div>
                    </div>

                    <div class="flex flex-col gap-2 xl:col-span-4">
                      <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">软工具调用协议覆盖 (soft_tool_calling_protocol)</label>
                      <select v-model="up.softToolProtocol" class="h-10 w-full font-mono px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white rounded-md">
                        <option value="">继承全局默认</option>
                        <option value="xml">xml</option>
                        <option value="sentinel_json">sentinel_json</option>
                        <option value="markdown_block">markdown_block</option>
                      </select>
                    </div>

                    <div class="flex flex-col gap-2 xl:col-span-4">
                      <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">Prompt Profile 绑定 (soft_tool_prompt_profile_id)</label>
                      <select v-model="up.softToolPromptProfileID" class="h-10 w-full font-mono px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white rounded-md">
                        <option value="">继承全局默认 / 不绑定</option>
                        <option v-for="profile in softToolPromptProfileOptions" :key="`up-prompt-profile-${profile.value || 'empty'}-${idx}`" :value="profile.value">
                          {{ profile.label }}{{ profile.protocol ? ` (${profile.protocol})` : '' }}{{ profile.enabled ? '' : ' [disabled]' }}
                        </option>
                      </select>
                      <div class="text-xs text-white/55 leading-5">
                        当前生效绑定：<code>{{ getEffectiveSoftToolPromptProfileBinding(up) }}</code>
                      </div>
                    </div>

                    <div class="flex flex-col gap-2 xl:col-span-4">
                      <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">提示注入目标 (prompt_injection_target)</label>
                      <select v-model="up.promptInjectionTarget" class="h-10 w-full font-mono px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white rounded-md" @change="onUpstreamTargetChange(up)">
                        <option
                          v-for="option in targetOptionsForProtocol(up.upstreamProtocol || 'openai_compat')"
                          :key="`up-target-${option.value || 'inherit'}-${idx}`"
                          :value="option.value"
                        >
                          {{ option.label }}{{ option.desc ? ` — ${option.desc}` : '' }}
                        </option>
                      </select>
                      <div class="text-xs text-white/55 leading-5">
                        当前生效目标：<code>{{ getEffectiveUpstreamPromptInjectionTarget(up) }}</code>
                      </div>
                    </div>

                    <div class="flex flex-col gap-2 xl:col-span-6">
                      <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">提示注入角色 (prompt_injection_role)</label>
                      <select
                        :value="getPromptInjectionRoleMode(up.promptInjectionRole)"
                        class="h-10 w-full font-mono px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white rounded-md"
                        @change="(event) => updatePromptInjectionRoleMode((event.target as HTMLSelectElement).value, (next) => { up.promptInjectionRole = next === 'custom' ? '' : next })"
                      >
                        <option
                          v-for="option in roleOptionsForProtocol(up.upstreamProtocol || 'openai_compat', up.promptInjectionTarget)"
                          :key="`up-role-${option.value || 'inherit'}-${idx}`"
                          :value="option.value"
                        >
                          {{ option.label }}
                        </option>
                      </select>
                      <input
                        v-if="getPromptInjectionRoleMode(up.promptInjectionRole) === 'custom'"
                        v-model="up.promptInjectionRole"
                        type="text"
                        placeholder="输入自定义角色"
                        class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono"
                      />
                    </div>
                  </div>
                </section>

                <section class="rounded-lg border border-white/10 bg-black/25 p-4 md:p-5 space-y-4">
                  <div class="text-[11px] uppercase tracking-widest text-muted font-semibold">密钥与模型</div>

                  <div class="flex flex-col gap-2">
                    <div class="flex items-center justify-between gap-3">
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

                  <div class="flex flex-col gap-2">
                    <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">可路由模型清单 (Models) / 每行一个</label>
                    <textarea v-model="up.modelsText" rows="4" placeholder="gpt-4o&#10;gpt-3.5-turbo&#10;alias-model:actual-upstream-model" class="p-4 bg-white/10 border-b-[1.5px] border-white/20 text-white text-xs focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono resize-y leading-relaxed"></textarea>
                  </div>

                  <div class="rounded-lg border border-white/10 bg-white/5 px-4 py-3 text-xs leading-5 text-white/55">
                    同步勾选会直接更新上面的文本框；未命中的手工条目会保留。需要做别名映射时，继续使用 <code>别名:上游模型名</code> 格式。
                  </div>
                </section>
              </div>
            </div>

            <div v-if="configForm.upstreams.length === 0" class="flex flex-col items-center justify-center py-16 border border-dashed border-white/10 rounded-xl bg-white/5 mt-4">
              <span class="text-muted font-mono text-sm mb-4">当前未配置任何上游服务节点</span>
              <button class="bg-white/10 hover:bg-white/20 text-white font-medium text-xs px-4 py-2 rounded-md border border-white/20 transition-colors" @click="addUpstream">添加第一个节点</button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
