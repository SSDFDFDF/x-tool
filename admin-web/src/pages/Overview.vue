<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'

const overview = ref<any>(null)
const globalStatus = ref({ type: 'warning', text: '正在获取数据...' })

const probeKey = ref('')
const probeOutput = ref('等待发起请求...')

const fetchOverview = async () => {
  try {
    const res = await fetch('/admin/api/overview')
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const data = await res.json()
    overview.value = data
    if (data.status === 'ok') {
      globalStatus.value = { type: 'success', text: '系统正常' }
    } else {
      globalStatus.value = { type: 'error', text: data.status || '异常' }
    }
  } catch (error: any) {
    globalStatus.value = { type: 'error', text: '连接失败' }
    probeOutput.value = String(error)
  }
}

const doProbe = async () => {
  probeOutput.value = "> 发送请求至 GET /v1/models ...\n"
  const headers: Record<string, string> = {}
  if (probeKey.value.trim()) {
    headers['Authorization'] = `Bearer ${probeKey.value.trim()}`
  }
  
  try {
    const res = await fetch('/v1/models', { headers })
    const text = await res.text()
    let body = text
    try {
      body = JSON.stringify(JSON.parse(text), null, 2)
    } catch {}
    probeOutput.value += `\n< HTTP ${res.status}\n\n${body}`
  } catch (err) {
    probeOutput.value += `\n[网络错误]: ${String(err)}`
  }
}

const handleConfigSaved = () => {
  fetchOverview()
}

onMounted(() => {
  window.addEventListener('admin:config-saved', handleConfigSaved)
  fetchOverview()
})

onUnmounted(() => {
  window.removeEventListener('admin:config-saved', handleConfigSaved)
})
</script>

<template>
  <div class="p-6 md:p-8 w-full h-full flex flex-col pt-[calc(3rem)] overflow-y-auto">
    <header class="flex items-center justify-between mb-8 pb-4 border-b border-white/10">
      <h1 class="font-heading font-semibold text-3xl md:text-4xl text-white">
        系统状态总览
      </h1>
      <div class="flex items-center gap-4">
        <div class="flex items-center gap-2 px-4 py-2 rounded-md border border-white/10 bg-black/40">
          <span class="w-2.5 h-2.5 rounded-full inline-block" 
                :class="{
                  'bg-gold': globalStatus.type === 'warning',
                  'bg-[#10b981]': globalStatus.type === 'success',
                  'bg-[#ef4444]': globalStatus.type === 'error'
                }">
          </span>
          <span class="text-sm font-medium tracking-wide text-white/90">{{ globalStatus.text }}</span>
        </div>
      </div>
    </header>

    <div class="grid grid-cols-12 gap-6 w-full">
      
      <!-- Runtime Settings -->
      <div class="col-span-12 bg-darkmatter border border-white/10 rounded-lg overflow-hidden transition-all duration-300">
        <div class="px-6 py-5 border-b border-white/5 relative overflow-hidden">
          <h2 class="font-heading font-semibold text-xl text-white relative z-10">运行时监控 (Runtime)</h2>
        </div>
        <div class="p-6">
          <div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4" v-if="overview">
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">可用上游节点</div>
              <div class="font-mono text-3xl text-bitcoin">{{ overview.summary.upstreams_count }}</div>
            </div>
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">总暴露模型数</div>
              <div class="font-mono text-3xl text-bitcoin">{{ overview.summary.visible_models_count }}</div>
            </div>
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">已配置别名组</div>
              <div class="font-mono text-3xl text-white/90">{{ overview.summary.aliases_count }}</div>
            </div>
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">授权凭证(Keys)</div>
              <div class="font-mono text-3xl text-white/90">{{ overview.auth.client_keys_count }}</div>
            </div>
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">全局超时时限</div>
              <div class="font-mono text-3xl text-white/90">{{ overview.server.timeout_seconds }}s</div>
            </div>
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">服务启动时间</div>
              <div class="font-mono text-lg text-white/90 mt-1 truncate">{{ new Date(overview.runtime.started_at).toLocaleTimeString("zh-CN", { hour12: false }) }}</div>
            </div>
          </div>
        </div>
      </div>

      <div class="col-span-12 bg-darkmatter border border-white/10 rounded-lg overflow-hidden transition-all duration-300">
        <div class="px-6 py-5 border-b border-white/5 relative overflow-hidden">
          <h2 class="font-heading font-semibold text-xl text-white relative z-10">请求统计 (Stats)</h2>
        </div>
        <div class="p-6">
          <div class="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-4" v-if="overview && overview.stats">
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">总请求数</div>
              <div class="font-mono text-3xl text-bitcoin">{{ overview.stats.total_requests ?? 0 }}</div>
            </div>
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">处理中请求</div>
              <div class="font-mono text-3xl text-white/90">{{ overview.stats.inflight_requests ?? 0 }}</div>
            </div>
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">流式请求数</div>
              <div class="font-mono text-3xl text-white/90">{{ overview.stats.stream_requests ?? 0 }}</div>
            </div>
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">2xx 响应数</div>
              <div class="font-mono text-3xl text-[#10b981]">{{ overview.stats.status_2xx ?? 0 }}</div>
            </div>
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">4xx 响应数</div>
              <div class="font-mono text-3xl text-gold">{{ overview.stats.status_4xx ?? 0 }}</div>
            </div>
            <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between hover:bg-white/5 transition-colors duration-200">
              <div class="text-xs text-muted mb-2 uppercase tracking-wider font-semibold">5xx 响应数</div>
              <div class="font-mono text-3xl text-[#ef4444]">{{ overview.stats.status_5xx ?? 0 }}</div>
            </div>
          </div>
        </div>
      </div>

      <!-- Upstreams List -->
      <div class="col-span-12 lg:col-span-8 bg-darkmatter border border-white/10 rounded-lg overflow-hidden transition-all duration-300">
        <div class="px-6 py-5 border-b border-white/5 relative">
          <h2 class="font-heading font-semibold text-xl text-white">上游路由目标 (Upstreams)</h2>
        </div>
        <div class="p-0">
          <ul class="flex flex-col" v-if="overview && overview.upstreams.length">
            <li v-for="(up, idx) in overview.upstreams" :key="idx" class="px-6 py-4 flex items-center justify-between border-b border-white/5 last:border-b-0 hover:bg-white/5 transition-colors">
              <div class="flex flex-col gap-1.5 w-full">
                <div class="flex items-center gap-3">
                  <span class="font-medium text-[15px] font-heading">{{ up.name }}</span>
                  <span v-if="up.is_default" class="px-2 py-0.5 text-[10px] font-bold tracking-widest uppercase rounded bg-gold/20 text-gold border border-gold/30">Default</span>
                  <span class="px-2 py-0.5 text-[10px] font-mono rounded bg-white/5 text-white/70 border border-white/10">{{ up.upstream_protocol || 'openai_compat' }}</span>
                </div>
                <span class="font-mono text-xs text-muted/70 truncate">{{ up.base_url }}</span>
                <span class="font-mono text-[11px] text-white/45">prompt target: {{ up.prompt_injection_target || 'auto' }}</span>
              </div>
              <div class="shrink-0 text-right">
                <div class="font-mono text-sm text-white/70 bg-white/5 px-3 py-1.5 rounded-lg border border-white/5">{{ up.models_count }} Models</div>
              </div>
            </li>
          </ul>
          <div v-else class="p-8 text-center text-muted font-mono text-sm">未配置上游服务</div>
        </div>
      </div>

      <!-- Features -->
      <div class="col-span-12 lg:col-span-4 bg-darkmatter border border-white/10 rounded-lg overflow-hidden transition-all duration-300">
        <div class="px-6 py-5 border-b border-white/5 relative">
          <h2 class="font-heading font-semibold text-xl text-white">功能配置 (Features)</h2>
        </div>
        <div class="p-0">
          <ul class="flex flex-col" v-if="overview">
            <li class="px-6 py-4 flex items-center justify-between border-b border-white/5">
              <div class="flex flex-col gap-1">
                <span class="font-medium text-sm">Soft Tool Calling</span>
                <span class="font-mono text-xs text-muted">启用软工具调用协议转换</span>
              </div>
              <span class="px-2 py-1 text-xs font-mono rounded" :class="overview.features.enable_function_calling ? 'bg-white/5 text-muted border border-white/10' : 'bg-bitcoin/20 text-bitcoin border border-bitcoin/30'">{{ overview.features.enable_function_calling ? "ON" : "OFF" }}</span>
            </li>
            <li class="px-6 py-4 flex items-center justify-between border-b border-white/5">
              <div class="flex flex-col gap-1">
                <span class="font-medium text-sm">Dev to System</span>
                <span class="font-mono text-xs text-muted">转换 Developer 角色</span>
              </div>
              <span class="px-2 py-1 text-xs font-mono rounded" :class="overview.features.convert_developer_to_system ? 'bg-white/5 text-muted border border-white/10' : 'bg-bitcoin/20 text-bitcoin border border-bitcoin/30'">{{ overview.features.convert_developer_to_system ? "ON" : "OFF" }}</span>
            </li>
            <li class="px-6 py-4 flex items-center justify-between border-b border-white/5">
              <div class="flex flex-col gap-1">
                <span class="font-medium text-sm">Key Passthrough</span>
                <span class="font-mono text-xs text-muted">API Key 直接透传</span>
              </div>
              <span class="px-2 py-1 text-xs font-mono rounded" :class="overview.features.key_passthrough ? 'bg-white/5 text-muted border border-white/10' : 'bg-bitcoin/20 text-bitcoin border border-bitcoin/30'">{{ overview.features.key_passthrough ? "ON" : "OFF" }}</span>
            </li>
            <li class="px-6 py-4 flex items-center justify-between border-b border-white/5">
              <div class="flex flex-col gap-1">
                <span class="font-medium text-sm">Model Passthrough</span>
                <span class="font-mono text-xs text-muted">未知模型透传请求</span>
              </div>
              <span class="px-2 py-1 text-xs font-mono rounded" :class="overview.features.model_passthrough ? 'bg-white/5 text-muted border border-white/10' : 'bg-bitcoin/20 text-bitcoin border border-bitcoin/30'">{{ overview.features.model_passthrough ? "ON" : "OFF" }}</span>
            </li>
            <li class="px-6 py-4 flex items-center justify-between border-b border-white/5">
              <div class="flex flex-col gap-1">
                <span class="font-medium text-sm">Prompt Template</span>
                <span class="font-mono text-xs text-muted">应用内置 Prompt 模板</span>
              </div>
              <span class="px-2 py-1 text-xs font-mono rounded" :class="overview.features.custom_prompt_template ? 'bg-white/5 text-muted border border-white/10' : 'bg-bitcoin/20 text-bitcoin border border-bitcoin/30'">{{ overview.features.custom_prompt_template ? "ON" : "OFF" }}</span>
            </li>
            <li class="px-6 py-4 flex items-center justify-between border-b border-white/5">
              <div class="flex flex-col gap-1">
                <span class="font-medium text-sm">Prompt Target</span>
                <span class="font-mono text-xs text-muted">全局提示注入目标</span>
              </div>
              <span class="px-2 py-1 text-xs font-mono rounded bg-white/5 text-white/80 border border-white/10">{{ overview.features.prompt_injection_target || "auto" }}</span>
            </li>
          </ul>
        </div>
      </div>

      <!-- Catalog -->
      <div class="col-span-12 lg:col-span-8 bg-darkmatter border border-white/10 rounded-lg overflow-hidden transition-all duration-300">
        <div class="px-6 py-5 border-b border-white/5">
          <h2 class="font-heading font-semibold text-xl text-white">对外暴露模型 (Catalog)</h2>
        </div>
        <div class="p-6">
          <div class="flex flex-wrap gap-2" v-if="overview && overview.models.length">
            <span class="font-mono text-xs px-3 py-1.5 bg-black/50 border border-white/10 rounded-lg text-white/80 hover:border-gold/50 hover:text-gold transition-colors cursor-default" v-for="m in overview.models" :key="m">{{ m }}</span>
          </div>
          <div v-else class="p-8 text-center text-muted font-mono text-sm">列表中未找到模型</div>
        </div>
      </div>

      <!-- Aliases -->
      <div class="col-span-12 lg:col-span-4 bg-darkmatter border border-white/10 rounded-lg overflow-hidden transition-all duration-300">
        <div class="px-6 py-5 border-b border-white/5">
          <h2 class="font-heading font-semibold text-xl text-white">模型别名组 (Aliases)</h2>
        </div>
        <div class="p-0">
          <ul class="flex flex-col" v-if="overview && overview.aliases.length">
            <li class="px-6 py-4 flex flex-col items-start gap-2 border-b border-white/5 last:border-b-0" v-for="(alias, idx) in overview.aliases" :key="idx">
              <div class="font-medium text-sm text-bitcoin">{{ alias.alias }}</div>
              <div class="flex flex-wrap gap-1.5">
                <span class="font-mono text-[10px] px-2 py-1 bg-white/5 border border-white/10 rounded text-muted" v-for="t in alias.targets" :key="t">{{ t }}</span>
              </div>
            </li>
          </ul>
          <div v-else class="p-8 text-center text-muted font-mono text-sm">暂无别名映射记录</div>
        </div>
      </div>

      <!-- Access Probe -->
      <div class="col-span-12 bg-darkmatter border border-white/10 rounded-lg overflow-hidden transition-all duration-300 mb-12">
        <div class="px-6 py-5 border-b border-white/5">
          <h2 class="font-heading font-semibold text-xl text-white">访问调试 (Access Probe)</h2>
        </div>
        <div class="p-6">
          <form class="mb-4 max-w-2xl" @submit.prevent="doProbe">
            <div class="flex gap-4">
              <input v-model="probeKey" type="password" placeholder="输入客户端 Bearer Token 进行验证..." class="flex-1 h-12 px-4 bg-black/50 border-b-2 border-white/20 text-white text-sm placeholder:text-white/30 focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md" />
              <button class="bg-bitcoin text-white font-medium text-sm px-6 rounded-md hover:bg-burnt transition-colors min-w-[120px] h-12 flex items-center justify-center shrink-0" type="submit">发送请求</button>
            </div>
            <p class="text-xs text-muted/60 mt-3 font-mono">前端仅作临时请求凭证存储，页面刷新后即丢弃。</p>
          </form>
          <pre class="bg-[#030304] border border-white/5 text-white/80 font-mono text-xs p-5 rounded-xl h-[300px] overflow-y-auto leading-relaxed whitespace-pre-wrap break-all shadow-[inset_0_2px_10px_rgba(0,0,0,0.5)]">{{ probeOutput }}</pre>
        </div>
      </div>

    </div>
  </div>
</template>
