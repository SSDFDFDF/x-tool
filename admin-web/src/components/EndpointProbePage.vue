<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RefreshCw } from 'lucide-vue-next'

const props = withDefaults(
  defineProps<{
    title: string
    description: string
    endpoint: string
    requiresBearerToken?: boolean
    tokenPlaceholder?: string
    note?: string
  }>(),
  {
    requiresBearerToken: false,
    tokenPlaceholder: '输入 Bearer Token...',
    note: ''
  }
)

const bearerToken = ref('')
const isLoading = ref(false)
const statusText = ref('尚未请求')
const lastRequestAt = ref('')
const responseBody = ref('等待发起请求...')

const endpointLabel = computed(() => `GET ${props.endpoint}`)

const runProbe = async () => {
  if (isLoading.value) {
    return
  }

  isLoading.value = true
  statusText.value = '请求中...'
  responseBody.value = `> GET ${props.endpoint}\n`

  const headers: Record<string, string> = {}
  if (props.requiresBearerToken && bearerToken.value.trim()) {
    headers.Authorization = `Bearer ${bearerToken.value.trim()}`
  }

  try {
    const res = await fetch(props.endpoint, { headers })
    const text = await res.text()

    let formattedBody = text
    try {
      formattedBody = JSON.stringify(JSON.parse(text), null, 2)
    } catch {
      if (!formattedBody.trim()) {
        formattedBody = '[empty response body]'
      }
    }

    statusText.value = `HTTP ${res.status} ${res.statusText}`.trim()
    responseBody.value = `> GET ${props.endpoint}\n\n< ${statusText.value}\n\n${formattedBody}`
  } catch (error) {
    statusText.value = '请求失败'
    responseBody.value = `> GET ${props.endpoint}\n\n[network error]\n${String(error)}`
  } finally {
    lastRequestAt.value = new Date().toLocaleString('zh-CN', { hour12: false })
    isLoading.value = false
  }
}

onMounted(() => {
  runProbe()
})
</script>

<template>
  <div class="p-6 md:p-8 w-full h-full flex flex-col pt-[calc(3rem)] overflow-y-auto">
    <header class="flex flex-col gap-4 mb-8 pb-4 border-b border-white/10 md:flex-row md:items-center md:justify-between">
      <div class="space-y-3">
        <h1 class="font-heading font-semibold text-3xl md:text-4xl text-white">
          {{ title }}
        </h1>
        <p class="text-sm text-muted max-w-3xl leading-6">
          {{ description }}
        </p>
      </div>

      <div class="flex items-center gap-3">
        <div class="px-3 py-2 rounded-md border border-white/10 bg-black/40 font-mono text-xs text-white/80">
          {{ endpointLabel }}
        </div>
        <button
          type="button"
          class="inline-flex items-center justify-center gap-2 rounded-md bg-bitcoin px-4 py-2.5 text-sm font-medium text-white transition-colors hover:bg-burnt disabled:cursor-not-allowed disabled:opacity-60"
          :disabled="isLoading"
          @click="runProbe"
        >
          <RefreshCw class="w-4 h-4" :class="{ 'animate-spin': isLoading }" />
          {{ isLoading ? '请求中...' : '重新请求' }}
        </button>
      </div>
    </header>

    <section class="bg-darkmatter border border-white/10 rounded-lg overflow-hidden">
      <div class="px-6 py-5 border-b border-white/5 flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div class="space-y-2">
          <h2 class="font-heading font-semibold text-xl text-white">接口响应</h2>
          <div class="flex flex-wrap items-center gap-3 text-xs font-mono">
            <span class="px-3 py-1.5 rounded-md border border-white/10 bg-black/40 text-white/80">
              {{ statusText }}
            </span>
            <span v-if="lastRequestAt" class="text-muted/70">
              {{ lastRequestAt }}
            </span>
          </div>
        </div>

        <form v-if="requiresBearerToken" class="w-full max-w-xl" @submit.prevent="runProbe">
          <label class="block text-xs uppercase tracking-[0.2em] text-muted mb-2">Authorization</label>
          <div class="flex gap-3">
            <input
              v-model="bearerToken"
              type="password"
              :placeholder="tokenPlaceholder"
              class="flex-1 h-12 px-4 bg-black/50 border-b-2 border-white/20 text-white text-sm placeholder:text-white/30 focus-visible:outline-none focus-visible:border-bitcoin transition-all rounded-md"
            />
            <button
              type="submit"
              class="h-12 shrink-0 rounded-md border border-white/10 bg-white/5 px-5 text-sm font-medium text-white transition-colors hover:bg-white/10 disabled:cursor-not-allowed disabled:opacity-60"
              :disabled="isLoading"
            >
              带 Token 重试
            </button>
          </div>
        </form>
      </div>

      <div class="p-6 space-y-4">
        <p v-if="note" class="text-xs text-muted/70 font-mono">
          {{ note }}
        </p>
        <pre class="bg-[#030304] border border-white/5 text-white/80 font-mono text-xs p-5 rounded-xl min-h-[420px] overflow-auto leading-relaxed whitespace-pre-wrap break-all shadow-[inset_0_2px_10px_rgba(0,0,0,0.5)]">{{ responseBody }}</pre>
      </div>
    </section>
  </div>
</template>
