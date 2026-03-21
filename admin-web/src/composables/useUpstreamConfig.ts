import { computed, ref, type Ref } from 'vue'
import type { UpstreamFormModel, ConfigFormModel } from '../types/config'
import { showMessage } from '../utils/message'

export type PromptInjectionTarget = 'auto' | 'message' | 'system' | 'instructions'
export type UpstreamProtocol = 'openai_compat' | 'responses' | 'anthropic'
export type RoleOption = { value: string; label: string }

export type UpstreamModelsSyncResponse = {
  status: string
  models: string[]
}

export const upstreamProtocolOptions: Array<{ value: UpstreamProtocol; label: string; desc: string }> = [
  { value: 'openai_compat', label: 'OpenAI 兼容', desc: '转为 Chat Completions 格式，支持所有入口协议' },
  { value: 'responses', label: 'Responses', desc: '原生透传 Responses API，仅接受 /v1/responses 入口' },
  { value: 'anthropic', label: 'Anthropic', desc: '原生透传 Anthropic Messages API，仅接受 /v1/messages 入口' }
]

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

export const onUpstreamProtocolChange = (upstream: UpstreamFormModel) => {
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

export const onUpstreamTargetChange = (upstream: UpstreamFormModel) => {
  const protocol = upstream.upstreamProtocol || 'openai_compat'
  const validRoles = roleOptionsForProtocol(protocol, upstream.promptInjectionTarget).map((option) => option.value)
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

export const generateRandomKey = (): string => {
  const bytes = new Uint8Array(32)
  crypto.getRandomValues(bytes)
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
  return `sk-${hex}`
}

export const appendRandomClientKey = (upstream: UpstreamFormModel) => {
  const key = generateRandomKey()
  const current = upstream.clientKeysText.trim()
  upstream.clientKeysText = current ? `${current}\n${key}` : key
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

export const getEffectiveUpstreamProtocol = (upstream: UpstreamFormModel) =>
  upstream.upstreamProtocol.trim() || 'openai_compat'

export const getEffectiveUpstreamPromptInjectionTarget = (upstream: UpstreamFormModel, globalTarget?: string) =>
  normalizePromptInjectionTarget(upstream.promptInjectionTarget) || normalizePromptInjectionTarget(globalTarget ?? '') || 'auto'

export const getEffectiveSoftToolPromptProfileBinding = (
  upstream: UpstreamFormModel,
  defaultProfileID?: string
) => upstream.softToolPromptProfileID.trim() || defaultProfileID?.trim() || '无'

export const compareUpstreamByID = (a: UpstreamFormModel, b: UpstreamFormModel) => {
  const readRawID = (upstream: UpstreamFormModel) => {
    const maybeID = (upstream as UpstreamFormModel & { id?: number | string }).id
    if (maybeID !== undefined && maybeID !== null && String(maybeID).trim() !== '') {
      return String(maybeID).trim()
    }
    return upstream.name.trim() || upstream.originalName.trim()
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

export const sortUpstreamsByID = (form: ConfigFormModel) => {
  form.upstreams.sort(compareUpstreamByID)
}

export const getUpstreamPanelKey = (upstream: UpstreamFormModel, idx: number) =>
  upstream.originalName.trim() || `draft:${idx}`

export const useUpstreamPanelState = (configForm: Ref<ConfigFormModel | null>) => {
  const expandedPanels = ref<Record<string, boolean>>({})

  const syncExpandedPanels = (form: ConfigFormModel, defaultExpanded = false) => {
    const nextState: Record<string, boolean> = {}
    form.upstreams.forEach((upstream, idx) => {
      const key = getUpstreamPanelKey(upstream, idx)
      nextState[key] = expandedPanels.value[key] ?? defaultExpanded
    })
    expandedPanels.value = nextState
  }

  const isExpanded = (upstream: UpstreamFormModel, idx: number) =>
    expandedPanels.value[getUpstreamPanelKey(upstream, idx)] ?? false

  const toggle = (upstream: UpstreamFormModel, idx: number) => {
    const key = getUpstreamPanelKey(upstream, idx)
    expandedPanels.value = {
      ...expandedPanels.value,
      [key]: !isExpanded(upstream, idx)
    }
  }

  const expandAll = () => {
    if (!configForm.value) return
    const nextState: Record<string, boolean> = {}
    configForm.value.upstreams.forEach((upstream, idx) => {
      nextState[getUpstreamPanelKey(upstream, idx)] = true
    })
    expandedPanels.value = nextState
  }

  const collapseAll = () => {
    if (!configForm.value) return
    const nextState: Record<string, boolean> = {}
    configForm.value.upstreams.forEach((upstream, idx) => {
      nextState[getUpstreamPanelKey(upstream, idx)] = false
    })
    expandedPanels.value = nextState
  }

  const setExpanded = (upstream: UpstreamFormModel, idx: number, value: boolean) => {
    const key = getUpstreamPanelKey(upstream, idx)
    expandedPanels.value = {
      ...expandedPanels.value,
      [key]: value
    }
  }

  return {
    expandedPanels,
    syncExpandedPanels,
    isExpanded,
    toggle,
    expandAll,
    collapseAll,
    setExpanded
  }
}

export const useSoftToolPromptProfileOptions = (configForm: Ref<ConfigFormModel | null>) => {
  return computed(() => {
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
}

export const useUpstreamStats = (configForm: Ref<ConfigFormModel | null>) => {
  const totalModelsCount = computed(() => {
    if (!configForm.value) return 0
    return configForm.value.upstreams.reduce(
      (sum, upstream) => sum + countNonEmptyLines(upstream.modelsText),
      0
    )
  })

  const defaultUpstreamsCount = computed(() => {
    if (!configForm.value) return 0
    return configForm.value.upstreams.filter((upstream) => upstream.isDefault).length
  })

  return {
    totalModelsCount,
    defaultUpstreamsCount
  }
}