import type {
  AppConfig,
  ConfigFormModel,
  SoftToolPromptProfile,
  SoftToolPromptProfileFormModel,
  UpstreamFormModel,
  UpstreamService
} from '../types/config'

export const cloneConfig = (config: AppConfig): AppConfig => JSON.parse(JSON.stringify(config)) as AppConfig

const splitLines = (value: string): string[] =>
  value
    .split('\n')
    .map((item) => item.trim())
    .filter((item) => item.length > 0)

const buildUpstreamFormModel = (upstream?: Partial<UpstreamService>): UpstreamFormModel => ({
  originalName: upstream?.name ?? '',
  name: upstream?.name ?? '',
  baseURL: upstream?.base_url ?? '',
  apiKey: upstream?.api_key ?? '',
  description: upstream?.description ?? '',
  promptInjectionRole: upstream?.prompt_injection_role ?? '',
  promptInjectionTarget: upstream?.prompt_injection_target ?? '',
  softToolProtocol: upstream?.soft_tool_calling_protocol ?? '',
  softToolPromptProfileID: upstream?.soft_tool_prompt_profile_id ?? '',
  upstreamProtocol: upstream?.upstream_protocol ?? 'openai_compat',
  isDefault: upstream?.is_default ?? false,
  modelsText: (upstream?.models ?? []).join('\n'),
  clientKeysText: (upstream?.client_keys ?? []).join('\n'),
  syncedModels: [],
  modelSyncLoaded: false,
  modelSyncError: '',
  isSyncingModels: false
})

export const createEmptyUpstreamForm = (): UpstreamFormModel => buildUpstreamFormModel()

const buildPromptProfileFormModel = (profile?: Partial<SoftToolPromptProfile>): SoftToolPromptProfileFormModel => ({
  originalID: profile?.id ?? '',
  id: profile?.id ?? '',
  name: profile?.name ?? '',
  description: profile?.description ?? '',
  protocol: profile?.protocol ?? '',
  template: profile?.template ?? '',
  enabled: profile?.enabled ?? true
})

export const createEmptyPromptProfileForm = (): SoftToolPromptProfileFormModel => buildPromptProfileFormModel()

export const configToForm = (config: AppConfig): ConfigFormModel => ({
  server: {
    port: config.server?.port ?? 0,
    host: config.server?.host ?? '',
    timeout: config.server?.timeout ?? 180
  },
  features: {
    enable_function_calling: config.features?.enable_function_calling ?? false,
    convert_developer_to_system: config.features?.convert_developer_to_system ?? false,
    key_passthrough: config.features?.key_passthrough ?? false,
    model_passthrough: config.features?.model_passthrough ?? false,
    log_level: config.features?.log_level ?? 'INFO',
    prompt_template: config.features?.prompt_template ?? '',
    default_soft_tool_prompt_profile_id: config.features?.default_soft_tool_prompt_profile_id ?? '',
    prompt_injection_role: config.features?.prompt_injection_role ?? '',
    prompt_injection_target: config.features?.prompt_injection_target ?? '',
    soft_tool_calling_protocol: config.features?.soft_tool_calling_protocol ?? 'xml'
  },
  upstreams: (config.upstream_services ?? []).map(buildUpstreamFormModel),
  promptProfiles: (config.soft_tool_prompt_profiles ?? []).map(buildPromptProfileFormModel)
})

export const formToConfig = (rawConfig: AppConfig, form: ConfigFormModel): AppConfig => {
  const next = cloneConfig(rawConfig)
  const promptProfileIDRemap = new Map<string, string>()

  next.server = {
    ...next.server,
    port: Number(form.server.port),
    host: form.server.host,
    timeout: Number(form.server.timeout)
  }

  next.features = {
    ...next.features,
    enable_function_calling: Boolean(form.features.enable_function_calling),
    convert_developer_to_system: Boolean(form.features.convert_developer_to_system),
    key_passthrough: Boolean(form.features.key_passthrough),
    model_passthrough: Boolean(form.features.model_passthrough),
    log_level: form.features.log_level,
    prompt_template: form.features.prompt_template,
    default_soft_tool_prompt_profile_id: form.features.default_soft_tool_prompt_profile_id,
    prompt_injection_role: form.features.prompt_injection_role,
    prompt_injection_target: form.features.prompt_injection_target,
    soft_tool_calling_protocol: form.features.soft_tool_calling_protocol
  }

  const originalUpstreams = rawConfig.upstream_services ?? []
  next.upstream_services = form.upstreams.map((upstream) => {
    const matched = originalUpstreams.find((item) => item.name === (upstream.originalName || upstream.name))

    return {
      ...matched,
      name: upstream.name,
      base_url: upstream.baseURL,
      api_key: upstream.apiKey,
      description: upstream.description,
      prompt_injection_role: upstream.promptInjectionRole,
      prompt_injection_target: upstream.promptInjectionTarget,
      soft_tool_calling_protocol: upstream.softToolProtocol,
      soft_tool_prompt_profile_id: upstream.softToolPromptProfileID,
      upstream_protocol: upstream.upstreamProtocol,
      is_default: Boolean(upstream.isDefault),
      models: splitLines(upstream.modelsText),
      client_keys: splitLines(upstream.clientKeysText)
    }
  })

  const originalProfiles = rawConfig.soft_tool_prompt_profiles ?? []
  next.soft_tool_prompt_profiles = form.promptProfiles.map((profile) => {
    const matched = originalProfiles.find((item) => item.id === (profile.originalID || profile.id))
    const originalID = profile.originalID.trim()
    const nextID = profile.id.trim()

    if (originalID && nextID && originalID !== nextID) {
      promptProfileIDRemap.set(originalID, nextID)
    }

    return {
      ...matched,
      id: nextID,
      name: profile.name,
      description: profile.description,
      protocol: profile.protocol,
      template: profile.template,
      enabled: Boolean(profile.enabled)
    }
  })

  const rewritePromptProfileID = (value: string) => {
    const normalized = value.trim()
    return promptProfileIDRemap.get(normalized) ?? normalized
  }

  next.features.default_soft_tool_prompt_profile_id = rewritePromptProfileID(next.features.default_soft_tool_prompt_profile_id)
  next.upstream_services = next.upstream_services.map((upstream) => ({
    ...upstream,
    soft_tool_prompt_profile_id: rewritePromptProfileID(upstream.soft_tool_prompt_profile_id)
  }))

  return next
}
