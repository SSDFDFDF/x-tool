import type { Ref } from 'vue'
import { ref, computed } from 'vue'
import { showMessage } from '../utils/message'
import type { UpstreamFormModel, ConfigFormModel } from '../types/config'

export type PromptInjectionTarget = 'auto' | 'message' | 'system' | 'instructions'
export type UpstreamProtocol = 'openai_compat' | 'responses' | 'anthropic'

export type UpstreamModelsSyncResponse = {
  status: string
  models: string[]
}

export type RoleOption = { value: string; label: string }

// 协议选项
export const upstreamProtocolOptions: Array<{ value: UpstreamProtocol; label: string; desc: string }> = [
  { value: 'openai_compat', label: 'OpenAI 兼容', desc: '转为 Chat Completions 格式，支持所有入口协议' },
  { value: 'responses', label: 'Responses', desc: '原生透传 Responses API，仅接受 /v1/responses 入口' },
  { value: 'anthropic', label: 'Anthropic', desc: '原生透传 Anthropic Messages API，仅接受 /v1/messages 入口' }
]

// 全局 target 选项（不区分协议）
export const globalPromptInjectionTargetOptions: Array<{ value: PromptInjectionTarget | ''; label: string }> = [
  { value: '', label: '默认 (auto)' },
  { value: 'auto', label: 'auto' },
  { value: 'message', label: 'message' },
  { value: 'system', label: 'system' },
  { value: 'instructions', label: 'instructions' }
]

// 工具函数
export const normalizePromptInjectionTarget = (value: string): PromptInjectionTarget | '' => {
  if (value === 'auto' || value === 'message' || value === 'system' || value === 'instructions') {
    return value
  }
  return ''
}

export const splitNonEmptyLines = (value: string) =>
  value
    .split('\n')
    .map((item) => item.trim())
    .filter((item) => item.length > 0)

export const countNonEmptyLines = (value: string) => splitNonEmptyLines(value).length

export const getPromptInjectionRoleMode = (value: string) => {
  const trimmed = value?.trim()
  if (!trimmed) return ''
  if (trimmed === 'system' || trimmed === 'user' || trimmed === 'assistant' || trimmed === 'developer') {
    return trimmed
  }
  return 'custom'
}

// 按上游协议返回合法的 target 选项
export const targetOptionsForProtocol = (protocol: string): Array<{ value: string; label: string; desc: string }> => {
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

// 按上游协议 + target 返回合法的 role 选项
export const roleOptionsForProtocol = (protocol: string, target: string): RoleOption[] => {
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

// 协议切换时自动修正不兼容的 target/role
export const onUpstreamProtocolChange = (upstream: UpstreamFormModel) => {
  const protocol = upstream.upstreamProtocol || 'openai_compat'
  const validTargets = targetOptionsForProtocol(protocol).map((o) => o.value)
  if (!validTargets.includes(upstream.promptInjectionTarget)) {
    upstream.promptInjectionTarget = validTargets[0] ?? ''
  }
  const validRoles = roleOptionsForProtocol(protocol, upstream.promptInjectionTarget).map((o) => o.value)
  const currentMode = getPromptInjectionRoleMode(upstream.promptInjectionRole)
  if (!validRoles.includes(currentMode)) {
    upstream.promptInjectionRole = ''
  }
}

// target 切换时自动修正不兼容的 role
export const onUpstreamTargetChange = (upstream: UpstreamFormModel) => {
  const protocol = upstream.upstreamProtocol || 'openai_compat'
  const validRoles = roleOptionsForProtocol(protocol, upstream.promptInjectionTarget).map((o) => o.value)
  const currentMode = getPromptInjectionRoleMode(upstream.promptInjectionRole)
  if (!validRoles.includes(currentMode)) {
    upstream.promptInjectionRole = ''
  }
}

export const updatePromptInjectionRoleMode = (mode: string, onUpdate: (next: string) => void) => {
  if (mode === 'custom') {
    onUpdate('custom')
    return
  }
  onUpdate(mode)
}

// 模型同步相关
export const syncedModelSet = (upstream: UpstreamFormModel) => new Set(upstream.syncedModels)

export const selectedSyncedModels = (upstream: UpstreamFormModel) => {
  const managed = syncedModelSet(upstream)
  return splitNonEmptyLines(upstream.modelsText).filter((modelID) => managed.has(modelID))
}

export const selectedSyncedModelsCount = (upstream: UpstreamFormModel) => selectedSyncedModels(upstream).length

export const isSyncedModelSelected = (upstream: UpstreamFormModel, modelID: string) =>
  selectedSyncedModels(upstream).includes(modelID)

export const applySyncedModelSelection = (upstream: UpstreamFormModel, nextSelected: string[]) => {
  const managed = syncedModelSet(upstream)
  const manualEntries = splitNonEmptyLines(upstream.modelsText).filter((entry) => !managed.has(entry))
  const selectedSet = new Set(nextSelected)
  const selectedInDisplayOrder = upstream.syncedModels.filter((modelID) => selectedSet.has(modelID))
  upstream.modelsText = [...manualEntries, ...selectedInDisplayOrder].join('\n')
}

export const updateSyncedModelSelection = (upstream: UpstreamFormModel, modelID: string, checked: boolean) => {
  const next = new Set(selectedSyncedModels(upstream))
  if (checked) {
    next.add(modelID)
  } else {
    next.delete(modelID)
  }
  applySyncedModelSelection(upstream, Array.from(next))
}

export const selectAllSyncedModels = (upstream: UpstreamFormModel) => {
  applySyncedModelSelection(upstream, upstream.syncedModels)
}

export const clearSyncedModelSelection = (upstream: UpstreamFormModel) => {
  applySyncedModelSelection(upstream, [])
}

export const syncUpstreamModels = async (upstream: UpstreamFormModel) => {
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

    const payload = (await res.json()) as UpstreamModelsSyncResponse
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

// 面板展开状态
export const getUpstreamPanelKey = (upstream: UpstreamFormModel, idx: number) =>
  upstream.originalName.trim() || `draft:${idx}`

export const syncExpandedUpstreamPanels = (
  form: ConfigFormModel,
  expandedPanels: Ref<Record<string, boolean>>,
  defaultExpanded: boolean = true
) => {
  const nextState: Record<string, boolean> = {}
  form.upstreams.forEach((upstream, idx) => {
    const key = getUpstreamPanelKey(upstream, idx)
    nextState[key] = expandedPanels.value[key] ?? defaultExpanded
  })
  expandedPanels.value = nextState
}

export const isUpstreamExpanded = (upstream: UpstreamFormModel, idx: number, expandedPanels: Ref<Record<string, boolean>>, defaultExpanded: boolean = true) =>
  expandedPanels.value[getUpstreamPanelKey(upstream, idx)] ?? defaultExpanded

export const toggleUpstreamExpanded = (upstream: UpstreamFormModel, idx: number, expandedPanels: Ref<Record<string, boolean>>, defaultExpanded: boolean = true) => {
  const key = getUpstreamPanelKey(upstream, idx)
  expandedPanels.value = {
    ...expandedPanels.value,
    [key]: !isUpstreamExpanded(upstream, idx, expandedPanels, defaultExpanded)
  }
}

// 协议和配置获取
export const getEffectiveUpstreamProtocol = (upstream: UpstreamFormModel) =>
  upstream.upstreamProtocol.trim() || 'openai_compat'

export const getFeaturePromptInjectionTargetLabel = (value: string) =>
  normalizePromptInjectionTarget(value) || 'auto'

export const getEffectiveUpstreamPromptInjectionTarget = (upstream: UpstreamFormModel, globalTarget: string = '') =>
  normalizePromptInjectionTarget(upstream.promptInjectionTarget) || getFeaturePromptInjectionTargetLabel(globalTarget)

export const getEffectiveSoftToolPromptProfileBinding = (upstream: UpstreamFormModel, defaultProfileID: string = '') =>
  upstream.softToolPromptProfileID.trim() || defaultProfileID.trim() || '无'

// 密钥生成
export const generateRandomKey = (): string => {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
  return `sk-${hex}`
}

export const appendRandomClientKey = (upstream: UpstreamFormModel) => {
  const key = generateRandomKey()
  const current = upstream.clientKeysText.trim()
  upstream.clientKeysText = current ? `${current}\n${key}` : key
}