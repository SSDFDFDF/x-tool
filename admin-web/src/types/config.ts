export type ServerConfig = {
  port: number
  host: string
  timeout: number
}

export type UpstreamService = {
  name: string
  base_url: string
  api_key: string
  models: string[]
  client_keys: string[]
  description: string
  prompt_injection_role: string
  prompt_injection_target: string
  soft_tool_calling_protocol: string
  soft_tool_prompt_profile_id: string
  soft_tool_retry_attempts: number
  upstream_protocol: string
  is_default: boolean
}

export type SoftToolPromptProfile = {
  id: string
  name: string
  description: string
  protocol: string
  template: string
  enabled: boolean
}

export type FeaturesConfig = {
  enable_function_calling: boolean
  log_level: string
  convert_developer_to_system: boolean
  prompt_template: string
  default_soft_tool_prompt_profile_id: string
  prompt_injection_role: string
  prompt_injection_target: string
  soft_tool_calling_protocol: string
  soft_tool_retry_attempts: number
  key_passthrough: boolean
  model_passthrough: boolean
}

export type AppConfig = {
  server: ServerConfig
  upstream_services: UpstreamService[]
  soft_tool_prompt_profiles: SoftToolPromptProfile[]
  features: FeaturesConfig
}

export type UpstreamFormModel = {
  originalName: string
  name: string
  baseURL: string
  apiKey: string
  description: string
  promptInjectionRole: string
  promptInjectionTarget: string
  softToolProtocol: string
  softToolPromptProfileID: string
  softToolRetryAttempts: number
  upstreamProtocol: string
  isDefault: boolean
  modelsText: string
  clientKeysText: string
  syncedModels: string[]
  modelSyncLoaded: boolean
  modelSyncError: string
  isSyncingModels: boolean
}

export type SoftToolPromptProfileFormModel = {
  originalID: string
  id: string
  name: string
  description: string
  protocol: string
  template: string
  enabled: boolean
}

export type ConfigFormModel = {
  server: ServerConfig
  features: FeaturesConfig
  upstreams: UpstreamFormModel[]
  promptProfiles: SoftToolPromptProfileFormModel[]
}
