<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Plus, Save, Trash2 } from 'lucide-vue-next'
import { cloneConfig, configToForm, createEmptyPromptProfileForm, formToConfig } from '../utils/config-form'
import type { AppConfig, ConfigFormModel, SoftToolPromptProfileFormModel } from '../types/config'
import { showMessage } from '../utils/message'

type SoftToolProtocol = 'xml' | 'sentinel_json' | 'markdown_block'
type PromptEditorTab = 'default_template' | 'profiles'

const rawConfig = ref<AppConfig | null>(null)
const configForm = ref<ConfigFormModel | null>(null)
const isSaving = ref(false)
const promptPreviewProtocol = ref<SoftToolProtocol>('xml')
const activePromptEditorTab = ref<PromptEditorTab>('default_template')

const promptCatalogPlaceholders = ['{tool_catalog}']
const promptProtocolPlaceholders = ['{trigger_signal}', '{protocol_rules}', '{single_call_example}', '{multi_call_example}']

const promptPlaceholderDocs = [
  { token: '{tool_catalog}', description: '协议原生工具目录。推荐新模板使用。' },
  { token: '{protocol_rules}', description: '当前协议的规则说明。推荐作为主协议段。' },
  { token: '{single_call_example}', description: '单工具调用示例。' },
  { token: '{multi_call_example}', description: '多工具调用示例。xml 下会为空。' },
  { token: '{trigger_signal}', description: 'trigger sentinel 字符串。适合手写协议文案时使用。' },
  { token: '{protocol_name}', description: '当前协议名，渲染为 XML 或 sentinel + JSON。' },
  { token: '{output_rules}', description: '兼容旧模板的空占位符。统一输出规则已并入 {protocol_rules}。' }
] as const

const promptTemplatePresets = {
  generic: `Protocol: {protocol_name}
Tools:
{tool_catalog}

{protocol_rules}

Single call example:
{single_call_example}`,
  xml: `Reply in one of two modes only: a complete text turn, or a single XML tool turn, optionally preceded by one brief sentence.
Tools: {tool_catalog}
{protocol_rules}
Single call shape:
{single_call_example}
Multiple call shape:
{multi_call_example}
Examples:
Text turn: Hello -> Hello! How can I help?
Pure tool turn:
{single_call_example}
Brief text + tool turn:
Let me check.
{single_call_example}`,
  sentinel_json: `Reply in one of two modes only: a complete text turn, or a single sentinel + JSON tool turn, optionally preceded by one brief sentence.
Tools: {tool_catalog}
{protocol_rules}
Single call shape:
{single_call_example}
Multiple call shape:
{multi_call_example}
Examples:
Text turn: Hello -> Hello! How can I help?
Pure tool turn:
{single_call_example}
Brief text + tool turn:
Let me check.
{single_call_example}`,
  markdown_block: `Reply in one of two modes only: a complete text turn, or a single Markdown fenced tool turn, optionally preceded by one brief sentence.
Tools:
{tool_catalog}
{protocol_rules}
Single call shape:
{single_call_example}
Multiple call shape:
{multi_call_example}
Examples:
Text turn: Hello -> Hello! How can I help?
Pure tool turn:
{single_call_example}
Brief text + tool turn:
Let me check.
{single_call_example}`
} as const

const sampleTriggerSignal = '<Function_AbCd_Start>'
const sampleToolCatalogByProtocol: Record<SoftToolProtocol, string> = {
  xml: '<function_list><tool id="1" name="search_web"><description>Search the web</description><parameters><parameter name="query" type="string" required="true"><description>search query</description><schema>{"description":"search query","type":"string"}</schema></parameter></parameters></tool></function_list>',
  sentinel_json: '[{"description":"Search the web","name":"search_web","parameters":{"properties":{"query":{"description":"search query","type":"string"}},"required":["query"],"type":"object"}}]',
  markdown_block: `Available tools:
- search_web
  description: Search the web
  required args: query(string) - search query`
}

const protocolRulesByProtocol: Record<SoftToolProtocol, string> = {
  xml: [
    'Output rules:',
    '- If no tool is needed: reply with a complete text turn.',
    '- If a tool is needed: output the tool turn now in this same turn, optionally preceded by ONE brief sentence.',
    '- After the tool turn starts: output NOTHING else. No explanations, no summaries, no follow-up.',
    '',
    'Format rules:',
    '- Use an XML tool turn when a tool is needed. Do not output a text turn that says you will call a tool next.',
    `- Output ${sampleTriggerSignal} alone on its own line, then exactly one <function_calls>...</function_calls> block containing one or more <invoke name="tool_name">...</invoke> elements.`,
    '- The tool name must come from the tool list. Include required parameters and use raw text inside <parameter>.'
  ].join('\n'),
  sentinel_json: [
    'Output rules:',
    '- If no tool is needed: reply with a complete text turn.',
    '- If a tool is needed: output the tool turn now in this same turn, optionally preceded by ONE brief sentence.',
    '- After the tool turn starts: output NOTHING else. No explanations, no summaries, no follow-up.',
    '',
    'Format rules:',
    '- Use a sentinel + JSON tool turn when a tool is needed. Do not output a text turn that says you will call a tool next.',
    `- Output ${sampleTriggerSignal} alone on its own line, then exactly one JSON tool block.`,
    '- Use <TOOL_CALL> for one tool call and <TOOL_CALLS> for multiple tool calls.',
    '- The tool name must come from the tool list and the arguments field must be a JSON object.'
  ].join('\n'),
  markdown_block: [
    'Output rules:',
    '- If no tool is needed: reply with a complete text turn.',
    '- If a tool is needed: output the tool turn now in this same turn, optionally preceded by ONE brief sentence.',
    '- After the tool turn starts: output NOTHING else. No explanations, no summaries, no follow-up.',
    '',
    'Format rules:',
    '- Use a Markdown fenced tool turn when a tool is needed. Do not output a text turn that says you will call a tool next.',
    `- Output ${sampleTriggerSignal} alone on its own line, then exactly one \`\`\`mbtoolcalls fenced block.`,
    '- Inside the fenced block, start each tool call with `mbcall: tool_name`.',
    '- Add arguments with line-start bracket headers, for example `mbarg[query]: value`.',
    '- For nested fields use dot paths. For arrays use key[]. Use key@json only when the value must be parsed as JSON.',
    '- For multiline or exact text, write `mbarg[name]:` and continue the value until the next line-start `mbarg[...]:` line, the next `mbcall:` line, or the closing fence.',
    '- The tool name must come from the tool list and include required parameters.'
  ].join('\n')
}

const singleCallExampleByProtocol: Record<SoftToolProtocol, string> = {
  xml: `${sampleTriggerSignal}
<function_calls>
  <invoke name="tool_name">
    <parameter name="arg_name">value</parameter>
  </invoke>
</function_calls>`,
  sentinel_json: `${sampleTriggerSignal}
<TOOL_CALL>
{"name":"tool_name","arguments":{"arg_name":"value"}}
</TOOL_CALL>`,
  markdown_block: `${sampleTriggerSignal}
\`\`\`mbtoolcalls
mbcall: tool_name
mbarg[query]: value
\`\`\``
}

const multiCallExampleByProtocol: Record<SoftToolProtocol, string> = {
  xml: `${sampleTriggerSignal}
<function_calls>
  <invoke name="tool_name">
    <parameter name="arg_name">value</parameter>
  </invoke>
  <invoke name="tool_name_2">
    <parameter name="arg_name_2">value_2</parameter>
  </invoke>
</function_calls>`,
  sentinel_json: `${sampleTriggerSignal}
<TOOL_CALLS>
[{"name":"tool_name","arguments":{"arg_name":"value"}}]
</TOOL_CALLS>`,
  markdown_block: `${sampleTriggerSignal}
\`\`\`mbtoolcalls
mbcall: tool_name
mbarg[query]: value

mbcall: tool_name_2
mbarg[prompt]:
value line 1
value line 2
\`\`\``
}

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

const selectedGlobalProtocol = computed<SoftToolProtocol>(() =>
  configForm.value?.features.soft_tool_calling_protocol === 'sentinel_json'
    ? 'sentinel_json'
    : configForm.value?.features.soft_tool_calling_protocol === 'markdown_block'
      ? 'markdown_block'
      : 'xml'
)

const currentPromptTemplate = computed(() => configForm.value?.features.prompt_template ?? '')

const containsAnyPlaceholder = (template: string, placeholders: string[]) =>
  placeholders.some((placeholder) => template.includes(placeholder))

const validateTemplateIssues = (template: string) => {
  const trimmed = template.trim()
  if (!trimmed) {
    return [] as string[]
  }

  const issues: string[] = []
  if (!containsAnyPlaceholder(trimmed, promptCatalogPlaceholders)) {
    issues.push('模板至少需要工具目录占位符：{tool_catalog}。')
  }
  if (!containsAnyPlaceholder(trimmed, promptProtocolPlaceholders)) {
    issues.push('模板至少需要一个协议占位符：{trigger_signal}、{protocol_rules}、{single_call_example} 或 {multi_call_example}。')
  }
  return issues
}

const hasProtocolOverrides = computed(() => {
  if (!configForm.value) {
    return false
  }
  return configForm.value.upstreams.some((upstream) => {
    const protocol = upstream.softToolProtocol.trim()
    return protocol !== '' && protocol !== selectedGlobalProtocol.value
  })
})

const promptTemplateIssues = computed(() => validateTemplateIssues(currentPromptTemplate.value))

const promptTemplateWarnings = computed(() => {
  const template = currentPromptTemplate.value
  const warnings: string[] = []

  if (hasProtocolOverrides.value) {
    warnings.push('存在 upstream 级协议覆盖。建议优先使用 {tool_catalog}、{protocol_rules}、{single_call_example} 这类协议无关占位符。')
  }

  if (!template.trim()) {
    return warnings
  }

  if (selectedGlobalProtocol.value === 'sentinel_json' && /<function_list>|<function_calls>|<invoke\b/i.test(template)) {
    warnings.push('当前全局协议是 sentinel_json，但模板中出现了 XML 专用结构字样。若不是故意兼容旧模板，建议改用 {tool_catalog} 和 {protocol_rules}。')
  }

  if (selectedGlobalProtocol.value === 'xml' && /<TOOL_CALLS?>/i.test(template)) {
    warnings.push('当前全局协议是 xml，但模板中出现了 sentinel_json 专用标签。若不是故意兼容双协议，建议改用 {protocol_rules} 和 {single_call_example}。')
  }

  if (selectedGlobalProtocol.value === 'markdown_block' && (/<function_list>|<function_calls>|<invoke\b/i.test(template) || /<TOOL_CALLS?>/i.test(template))) {
    warnings.push('当前全局协议是 markdown_block，但模板中出现了 XML 或 sentinel_json 专用结构字样。若不是故意兼容旧模板，建议改用 {tool_catalog}、{protocol_rules} 和 {single_call_example}。')
  }

  return warnings
})

const promptTemplatePreviewLabel = computed(() =>
  currentPromptTemplate.value.trim() ? '当前模板预览' : '当前为空，将使用内置默认模板'
)

const renderPromptTemplatePreview = (template: string, protocol: SoftToolProtocol) => {
  const replacements: Record<string, string> = {
    '{protocol_name}': protocol === 'sentinel_json' ? 'sentinel + JSON' : protocol === 'markdown_block' ? 'Markdown fenced block' : 'XML',
    '{trigger_signal}': sampleTriggerSignal,
    '{tool_catalog}': sampleToolCatalogByProtocol[protocol],
    '{protocol_rules}': protocolRulesByProtocol[protocol],
    '{single_call_example}': singleCallExampleByProtocol[protocol],
    '{multi_call_example}': multiCallExampleByProtocol[protocol],
    '{output_rules}': ''
  }

  let rendered = template
  for (const [token, value] of Object.entries(replacements)) {
    rendered = rendered.replaceAll(token, value)
  }

  return rendered
}

const promptTemplatePreview = computed(() => {
  const source = currentPromptTemplate.value.trim() || promptTemplatePresets[promptPreviewProtocol.value]
  return renderPromptTemplatePreview(source, promptPreviewProtocol.value)
})

const totalPromptProfiles = computed(() => configForm.value?.promptProfiles.length ?? 0)
const enabledPromptProfiles = computed(() => configForm.value?.promptProfiles.filter((profile) => profile.enabled).length ?? 0)
const explicitlyBoundUpstreams = computed(() => configForm.value?.upstreams.filter((upstream) => upstream.softToolPromptProfileID.trim()).length ?? 0)
const defaultTemplateMode = computed(() => currentPromptTemplate.value.trim() ? '已自定义' : '内置默认')

const profileTemplateIssues = (profile: SoftToolPromptProfileFormModel) => validateTemplateIssues(profile.template)

const boundUpstreamNames = (profileID: string) => {
  if (!configForm.value || !profileID.trim()) {
    return [] as string[]
  }
  return configForm.value.upstreams
    .filter((upstream) => upstream.softToolPromptProfileID.trim() === profileID.trim())
    .map((upstream) => upstream.name.trim() || '未命名 upstream')
}

const applyPromptTemplatePreset = (preset: keyof typeof promptTemplatePresets) => {
  if (!configForm.value) {
    return
  }

  configForm.value.features.prompt_template = promptTemplatePresets[preset]
  promptPreviewProtocol.value = preset === 'generic' ? selectedGlobalProtocol.value : preset
  showMessage.info(`已填入 ${preset === 'generic' ? '通用' : preset} Prompt 模板示例。`, '模板示例')
}

const clearPromptTemplate = () => {
  if (!configForm.value) {
    return
  }
  configForm.value.features.prompt_template = ''
  showMessage.info('已清空自定义 Prompt 模板。保存后将回退到内置默认模板。', '模板示例')
}

const appendPromptPlaceholder = (token: string) => {
  if (!configForm.value) {
    return
  }

  const current = configForm.value.features.prompt_template
  configForm.value.features.prompt_template = current
    ? `${current}${current.endsWith('\n') ? '' : '\n'}${token}`
    : token
}

const applyPromptTemplatePresetToProfile = (profile: SoftToolPromptProfileFormModel, preset: keyof typeof promptTemplatePresets) => {
  profile.template = promptTemplatePresets[preset]
  if (!profile.protocol.trim() && preset !== 'generic') {
    profile.protocol = preset
  }
}

const clearPromptTemplateForProfile = (profile: SoftToolPromptProfileFormModel) => {
  profile.template = ''
}

const addPromptProfile = () => {
  if (!configForm.value) {
    return
  }
  configForm.value.promptProfiles.push(createEmptyPromptProfileForm())
  sortPromptProfiles(configForm.value)
}

const removePromptProfile = (idx: number) => {
  if (!configForm.value) {
    return
  }
  const [removed] = configForm.value.promptProfiles.splice(idx, 1)
  if (!removed) {
    return
  }

  const removedID = removed.id.trim() || removed.originalID.trim()
  if (removedID && configForm.value.features.default_soft_tool_prompt_profile_id === removedID) {
    configForm.value.features.default_soft_tool_prompt_profile_id = ''
  }
  configForm.value.upstreams.forEach((upstream) => {
    if (upstream.softToolPromptProfileID === removedID) {
      upstream.softToolPromptProfileID = ''
    }
  })
}

const loadConfig = async () => {
  try {
    const res = await fetch('/admin/api/config')
    if (!res.ok) throw new Error('API request failed')
    const config = await res.json() as AppConfig
    rawConfig.value = cloneConfig(config)
    configForm.value = configToForm(config)
    sortPromptProfiles(configForm.value)
    promptPreviewProtocol.value = config.features?.soft_tool_calling_protocol === 'sentinel_json'
      ? 'sentinel_json'
      : config.features?.soft_tool_calling_protocol === 'markdown_block'
        ? 'markdown_block'
        : 'xml'
  } catch (e: any) {
    showMessage.error(e.message, '加载配置失败')
  }
}

const saveConfig = async () => {
  if (!configForm.value || !rawConfig.value) {
    return
  }
  if (promptTemplateIssues.value.length > 0) {
    showMessage.warning(promptTemplateIssues.value.join('\n'), 'Legacy Prompt 模板不合法')
    return
  }

  const invalidProfile = configForm.value.promptProfiles.find((profile) => profileTemplateIssues(profile).length > 0)
  if (invalidProfile) {
    const profileLabel = invalidProfile.name.trim() || invalidProfile.id.trim() || '未命名 profile'
    showMessage.warning(profileTemplateIssues(invalidProfile).join('\n'), `${profileLabel} 模板不合法`)
    return
  }

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
    window.dispatchEvent(new CustomEvent('admin:config-saved'))
    showMessage.success('Prompt 模板配置已保存。')
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
        <h1 class="font-heading font-semibold text-3xl md:text-4xl text-white">Prompt 模板管理</h1>
        <p class="mt-2 text-sm text-white/60">
          单独维护 soft-tool prompt profile、全局 fallback 模板，以及默认绑定策略。upstream 具体绑定仍在
          <router-link to="/upstreams" class="text-bitcoin hover:text-burnt transition-colors">Upstreams</router-link>
          页面设置。
        </p>
      </div>
      <button
        class="bg-bitcoin hover:bg-burnt text-white font-medium text-sm px-6 py-3 rounded-md transition-colors flex items-center justify-center gap-2 disabled:opacity-50 disabled:cursor-not-allowed"
        :disabled="isSaving"
        @click="saveConfig"
      >
        <Save class="w-4 h-4" />
        {{ isSaving ? '保存中...' : '保存 Prompt 配置' }}
      </button>
    </header>

    <div v-if="configForm" class="space-y-6">
      <div class="grid grid-cols-1 md:grid-cols-4 gap-4">
        <div class="rounded-xl border border-white/10 bg-black/30 px-5 py-4">
          <div class="text-xs uppercase tracking-widest text-muted">Profile 总数</div>
          <div class="mt-2 text-2xl font-mono text-bitcoin">{{ totalPromptProfiles }}</div>
        </div>
        <div class="rounded-xl border border-white/10 bg-black/30 px-5 py-4">
          <div class="text-xs uppercase tracking-widest text-muted">已启用 Profile</div>
          <div class="mt-2 text-2xl font-mono text-white">{{ enabledPromptProfiles }}</div>
        </div>
        <div class="rounded-xl border border-white/10 bg-black/30 px-5 py-4">
          <div class="text-xs uppercase tracking-widest text-muted">显式绑定 Upstream</div>
          <div class="mt-2 text-2xl font-mono text-white">{{ explicitlyBoundUpstreams }}</div>
        </div>
        <div class="rounded-xl border border-white/10 bg-black/30 px-5 py-4">
          <div class="text-xs uppercase tracking-widest text-muted">默认模板</div>
          <div class="mt-2 text-2xl font-mono text-white">{{ defaultTemplateMode }}</div>
        </div>
      </div>

      <section class="bg-darkmatter border border-white/10 rounded-lg overflow-hidden">
        <div class="px-6 py-5 border-b border-white/5 bg-black/40">
          <h2 class="font-heading font-semibold text-xl text-white">全局解析策略</h2>
          <p class="mt-2 text-sm text-white/60">
            运行时优先级：<code>upstream.soft_tool_prompt_profile_id</code> → <code>features.default_soft_tool_prompt_profile_id</code> → 默认模板 <code>features.prompt_template</code>。
          </p>
        </div>
        <div class="p-6 md:p-8 grid grid-cols-1 md:grid-cols-2 gap-6">
          <div class="flex flex-col gap-2">
            <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">全局软工具协议 (soft_tool_calling_protocol)</label>
            <select v-model="configForm.features.soft_tool_calling_protocol" class="h-12 w-full font-mono px-4 bg-white/10 border-b-2 border-white/20 text-white rounded-md">
              <option value="xml">xml</option>
              <option value="sentinel_json">sentinel_json</option>
              <option value="markdown_block">markdown_block</option>
            </select>
          </div>

          <div class="flex flex-col gap-2">
            <label class="text-sm font-semibold uppercase tracking-wider text-muted font-heading">默认 Prompt Profile (default_soft_tool_prompt_profile_id)</label>
            <select v-model="configForm.features.default_soft_tool_prompt_profile_id" class="h-12 w-full font-mono px-4 bg-white/10 border-b-2 border-white/20 text-white rounded-md">
              <option value="">不使用默认 Profile，直接走默认模板</option>
              <option v-for="profile in promptProfileOptions" :key="`global-prompt-profile-${profile.value || 'empty'}`" :value="profile.value">
                {{ profile.label }}{{ profile.protocol ? ` (${profile.protocol})` : '' }}{{ profile.enabled ? '' : ' [disabled]' }}
              </option>
            </select>
          </div>
        </div>
      </section>

      <section class="bg-darkmatter border border-white/10 rounded-lg overflow-hidden">
        <div class="px-6 py-5 border-b border-white/5 bg-black/40">
          <div class="flex flex-wrap items-center gap-2">
            <button
              type="button"
              class="px-4 py-2 text-sm font-medium rounded-md border transition-colors"
              :class="activePromptEditorTab === 'default_template' ? 'border-bitcoin/30 bg-bitcoin/10 text-bitcoin' : 'border-white/10 bg-white/5 text-white/70 hover:text-white hover:bg-white/10'"
              @click="activePromptEditorTab = 'default_template'"
            >
              默认模板（Fallback）
            </button>
            <button
              type="button"
              class="px-4 py-2 text-sm font-medium rounded-md border transition-colors"
              :class="activePromptEditorTab === 'profiles' ? 'border-bitcoin/30 bg-bitcoin/10 text-bitcoin' : 'border-white/10 bg-white/5 text-white/70 hover:text-white hover:bg-white/10'"
              @click="activePromptEditorTab = 'profiles'"
            >
              Prompt Profiles
            </button>
          </div>
        </div>

        <div v-show="activePromptEditorTab === 'default_template'" class="p-6 md:p-8 space-y-4">
          <div>
            <h2 class="font-heading font-semibold text-xl text-white">默认模板（Fallback）</h2>
            <p class="mt-2 text-sm text-white/60">
              这是全局默认模板内容。只有在没有命中 profile，或命中的 profile.template 为空时，运行时才会回退到这里。
            </p>
          </div>

          <div class="flex flex-wrap items-center gap-3">
            <span class="text-xs font-mono px-2.5 py-1 rounded-full border border-bitcoin/30 bg-bitcoin/10 text-bitcoin">
              当前全局协议: {{ selectedGlobalProtocol }}
            </span>
            <span v-if="hasProtocolOverrides" class="text-xs font-mono px-2.5 py-1 rounded-full border border-amber-400/30 bg-amber-500/10 text-amber-100">
              检测到 upstream 协议覆盖
            </span>
          </div>

          <div class="flex flex-wrap gap-2">
            <button type="button" class="px-3 py-2 text-xs font-medium rounded-md border border-white/15 bg-white/5 text-white hover:bg-white/10 transition-colors" @click="applyPromptTemplatePreset('generic')">
              填入通用模板
            </button>
            <button type="button" class="px-3 py-2 text-xs font-medium rounded-md border border-white/15 bg-white/5 text-white hover:bg-white/10 transition-colors" @click="applyPromptTemplatePreset('xml')">
              填入 XML 模板
            </button>
            <button type="button" class="px-3 py-2 text-xs font-medium rounded-md border border-white/15 bg-white/5 text-white hover:bg-white/10 transition-colors" @click="applyPromptTemplatePreset('sentinel_json')">
              填入 sentinel_json 模板
            </button>
            <button type="button" class="px-3 py-2 text-xs font-medium rounded-md border border-white/15 bg-white/5 text-white hover:bg-white/10 transition-colors" @click="applyPromptTemplatePreset('markdown_block')">
              填入 markdown_block 模板
            </button>
            <button type="button" class="px-3 py-2 text-xs font-medium rounded-md border border-white/15 bg-transparent text-white/70 hover:text-white hover:bg-white/5 transition-colors" @click="clearPromptTemplate">
              清空并使用内置默认
            </button>
          </div>

          <div class="space-y-2">
            <p class="text-[11px] uppercase tracking-widest text-muted font-semibold">可用占位符</p>
            <div class="flex flex-wrap gap-2">
              <button
                v-for="item in promptPlaceholderDocs"
                :key="item.token"
                type="button"
                class="px-2.5 py-1.5 text-[11px] font-mono rounded-md border border-white/10 bg-black/40 text-white/80 hover:border-bitcoin/40 hover:text-white transition-colors"
                :title="item.description"
                @click="appendPromptPlaceholder(item.token)"
              >
                {{ item.token }}
              </button>
            </div>
          </div>

          <textarea
            v-model="configForm.features.prompt_template"
            class="p-4 bg-white/10 border-b-2 border-white/20 text-white text-xs focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md font-mono resize-y min-h-[420px] leading-relaxed"
          ></textarea>

          <div v-if="promptTemplateIssues.length > 0" class="rounded-xl border border-red-400/30 bg-red-500/10 px-4 py-3 text-sm text-red-100">
            <div class="font-semibold mb-2">保存前需要修正</div>
            <ul class="space-y-1">
              <li v-for="issue in promptTemplateIssues" :key="issue">{{ issue }}</li>
            </ul>
          </div>
          <div v-else-if="promptTemplateWarnings.length > 0" class="rounded-xl border border-amber-400/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-100">
            <div class="font-semibold mb-2">兼容性提示</div>
            <ul class="space-y-1">
              <li v-for="warning in promptTemplateWarnings" :key="warning">{{ warning }}</li>
            </ul>
          </div>

          <div class="rounded-xl border border-white/10 bg-black/30 p-4 md:p-5 space-y-4">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div>
                <div class="text-sm font-semibold text-white">{{ promptTemplatePreviewLabel }}</div>
                <div class="text-xs text-white/55">预览仅用于帮助理解占位符如何渲染，最终内容以后端实现为准。</div>
              </div>
              <div class="flex gap-2">
                <button
                  type="button"
                  class="px-3 py-1.5 text-xs font-mono rounded-md border transition-colors"
                  :class="promptPreviewProtocol === 'xml' ? 'border-bitcoin/40 bg-bitcoin/10 text-bitcoin' : 'border-white/10 bg-white/5 text-white/70 hover:text-white'"
                  @click="promptPreviewProtocol = 'xml'"
                >
                  预览 xml
                </button>
                <button
                  type="button"
                  class="px-3 py-1.5 text-xs font-mono rounded-md border transition-colors"
                  :class="promptPreviewProtocol === 'sentinel_json' ? 'border-bitcoin/40 bg-bitcoin/10 text-bitcoin' : 'border-white/10 bg-white/5 text-white/70 hover:text-white'"
                  @click="promptPreviewProtocol = 'sentinel_json'"
                >
                  预览 sentinel_json
                </button>
                <button
                  type="button"
                  class="px-3 py-1.5 text-xs font-mono rounded-md border transition-colors"
                  :class="promptPreviewProtocol === 'markdown_block' ? 'border-bitcoin/40 bg-bitcoin/10 text-bitcoin' : 'border-white/10 bg-white/5 text-white/70 hover:text-white'"
                  @click="promptPreviewProtocol = 'markdown_block'"
                >
                  预览 markdown_block
                </button>
              </div>
            </div>
            <pre class="p-4 bg-black/50 border border-white/10 text-[11px] text-white/80 rounded-lg overflow-x-auto whitespace-pre-wrap break-words leading-6">{{ promptTemplatePreview }}</pre>
          </div>
        </div>

        <div v-show="activePromptEditorTab === 'profiles'" class="p-6 md:p-8 space-y-4">
          <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div>
              <h2 class="font-heading font-semibold text-xl text-white">Prompt Profiles</h2>
              <p class="mt-2 text-sm text-white/60">每个 profile 可以单独覆盖协议和模板，再由 Upstreams 页面绑定到具体上游。</p>
            </div>
            <button
              type="button"
              class="px-3 py-2 text-xs font-medium rounded-md border border-bitcoin/30 bg-bitcoin/10 text-bitcoin hover:bg-bitcoin/20 transition-colors flex items-center gap-1.5"
              @click="addPromptProfile"
            >
              <Plus class="w-3.5 h-3.5" />
              添加 Profile
            </button>
          </div>

          <div v-if="configForm.promptProfiles.length === 0" class="rounded-lg border border-dashed border-white/10 bg-black/20 px-4 py-8 text-sm text-white/50">
            还没有自定义 profile。可以先只用默认模板，也可以新增多个 profile 给不同 upstream 绑定。
          </div>

          <div v-for="(profile, idx) in configForm.promptProfiles" :key="profile.originalID || `prompt-profile-${idx}`" class="rounded-xl border border-white/10 bg-black/20 p-4 md:p-5 space-y-4">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-xs font-mono px-2.5 py-1 rounded-full border border-white/10 bg-white/5 text-white/75">
                  {{ profile.id || `draft-${idx + 1}` }}
                </span>
                <span class="text-xs font-mono px-2.5 py-1 rounded-full border" :class="profile.enabled ? 'border-bitcoin/30 bg-bitcoin/10 text-bitcoin' : 'border-white/10 bg-white/5 text-white/55'">
                  {{ profile.enabled ? 'enabled' : 'disabled' }}
                </span>
                <span v-if="configForm.features.default_soft_tool_prompt_profile_id === profile.id" class="text-xs font-mono px-2.5 py-1 rounded-full border border-emerald-400/30 bg-emerald-500/10 text-emerald-100">
                  global default
                </span>
              </div>

              <button
                type="button"
                class="text-muted hover:text-red-400 p-2 rounded-lg hover:bg-white/5 transition-colors"
                title="删除 profile"
                @click="removePromptProfile(Number(idx))"
              >
                <Trash2 class="w-4 h-4" />
              </button>
            </div>

            <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-12 gap-x-6 gap-y-4">
              <div class="flex flex-col gap-2 xl:col-span-3">
                <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">ID</label>
                <input v-model="profile.id" type="text" placeholder="weak-model-markdown" class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono" />
              </div>

              <div class="flex flex-col gap-2 xl:col-span-4">
                <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">名称</label>
                <input v-model="profile.name" type="text" placeholder="Weak Model Markdown" class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all" />
              </div>

              <div class="flex flex-col gap-2 xl:col-span-3">
                <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">协议覆盖 (protocol)</label>
                <select v-model="profile.protocol" class="h-10 w-full font-mono px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white rounded-md">
                  <option value="">继承 upstream / 全局</option>
                  <option value="xml">xml</option>
                  <option value="sentinel_json">sentinel_json</option>
                  <option value="markdown_block">markdown_block</option>
                </select>
              </div>

              <div class="flex items-end xl:col-span-2">
                <label class="flex items-center gap-3 cursor-pointer group pb-2">
                  <div class="relative flex items-center justify-center">
                    <input v-model="profile.enabled" type="checkbox" class="peer sr-only">
                    <div class="w-5 h-5 rounded border border-white/30 peer-checked:bg-bitcoin peer-checked:border-bitcoin flex items-center justify-center transition-all bg-black/50 shadow-inner group-hover:border-white/50">
                      <svg class="w-3.5 h-3.5 text-white opacity-0 peer-checked:opacity-100 drop-shadow-md" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path></svg>
                    </div>
                  </div>
                  <span class="text-sm font-medium text-white/80 group-hover:text-white transition-colors">启用</span>
                </label>
              </div>

              <div class="flex flex-col gap-2 xl:col-span-12">
                <label class="text-[11px] font-semibold uppercase tracking-widest text-muted">说明</label>
                <input v-model="profile.description" type="text" placeholder="适用于某类上游 / 模型能力的模板策略" class="h-10 px-3 bg-white/10 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all" />
              </div>
            </div>

            <div class="rounded-lg border border-white/10 bg-black/30 px-4 py-3 text-xs leading-6 text-white/60">
              <div>当前生效协议来源：<code>{{ profile.protocol || 'inherit' }}</code></div>
              <div>
                显式绑定的 upstream：
                <span v-if="boundUpstreamNames(profile.id).length === 0">无</span>
                <span v-else>{{ boundUpstreamNames(profile.id).join('、') }}</span>
              </div>
            </div>

            <div class="flex flex-wrap gap-2">
              <button type="button" class="px-3 py-2 text-xs font-medium rounded-md border border-white/15 bg-white/5 text-white hover:bg-white/10 transition-colors" @click="applyPromptTemplatePresetToProfile(profile, 'generic')">
                填入通用模板
              </button>
              <button type="button" class="px-3 py-2 text-xs font-medium rounded-md border border-white/15 bg-white/5 text-white hover:bg-white/10 transition-colors" @click="applyPromptTemplatePresetToProfile(profile, 'xml')">
                XML
              </button>
              <button type="button" class="px-3 py-2 text-xs font-medium rounded-md border border-white/15 bg-white/5 text-white hover:bg-white/10 transition-colors" @click="applyPromptTemplatePresetToProfile(profile, 'sentinel_json')">
                sentinel_json
              </button>
              <button type="button" class="px-3 py-2 text-xs font-medium rounded-md border border-white/15 bg-white/5 text-white hover:bg-white/10 transition-colors" @click="applyPromptTemplatePresetToProfile(profile, 'markdown_block')">
                markdown_block
              </button>
              <button type="button" class="px-3 py-2 text-xs font-medium rounded-md border border-white/15 bg-transparent text-white/70 hover:text-white hover:bg-white/5 transition-colors" @click="clearPromptTemplateForProfile(profile)">
                清空模板并回退默认模板
              </button>
            </div>

            <textarea
              v-model="profile.template"
              class="p-4 bg-white/10 border-b-2 border-white/20 text-white text-xs focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md font-mono resize-y min-h-[260px] leading-relaxed"
              placeholder="为空时将回退到全局默认模板 prompt_template"
            ></textarea>

            <div v-if="profileTemplateIssues(profile).length > 0" class="rounded-xl border border-red-400/30 bg-red-500/10 px-4 py-3 text-sm text-red-100">
              <div class="font-semibold mb-2">当前 Profile 模板需要修正</div>
              <ul class="space-y-1">
                <li v-for="issue in profileTemplateIssues(profile)" :key="`${profile.id}-${issue}`">{{ issue }}</li>
              </ul>
            </div>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>
