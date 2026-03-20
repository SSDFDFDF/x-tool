<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RefreshCw, Play, Pause, Trash2, FileTerminal, AlignLeft } from 'lucide-vue-next'

type LogEntry = {
  id: number
  time: string
  level: string
  message: string
  attrs?: Record<string, unknown>
  text: string
}

type LogMeta = {
  path: string
  exists: boolean
  size_bytes: number
  updated_at?: string
}

const entries = ref<LogEntry[]>([])
const paused = ref(false)
const connected = ref(false)
const search = ref('')
const levelFilter = ref('')
const limit = ref('200')
const autoScroll = ref(true)
const streamMeta = ref('尚未收到事件')
const rawTail = ref('加载中...')
const meta = ref<LogMeta | null>(null)
const eventSource = ref<EventSource | null>(null)
const activeTab = ref<'logs' | 'tail'>('logs')

const visibleEntries = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  return entries.value.filter((entry) => {
    if (levelFilter.value && entry.level !== levelFilter.value) {
      return false
    }
    if (!keyword) {
      return true
    }
    return JSON.stringify(entry).toLowerCase().includes(keyword)
  })
})

const latestLevel = computed(() => {
  if (entries.value.length === 0) {
    return '-'
  }
  return entries.value[entries.value.length - 1].level
})

const connectionLabel = computed(() => connected.value ? 'Live' : 'Offline')

const formatBytes = (value: number) => {
  if (!Number.isFinite(value) || value < 1) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB']
  let size = value
  let unitIndex = 0
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex += 1
  }
  return `${size.toFixed(size >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`
}

const loadSnapshot = async () => {
  const response = await fetch(`/admin/api/logs?limit=${encodeURIComponent(limit.value)}`)
  const payload = await response.json()
  entries.value = Array.isArray(payload.entries) ? payload.entries : []
}

const loadMeta = async () => {
  const response = await fetch('/admin/api/logs/meta')
  const payload = await response.json()
  meta.value = payload.meta || null
}

const loadRawTail = async () => {
  const response = await fetch(`/admin/api/logs/raw?limit=${encodeURIComponent(limit.value)}`)
  rawTail.value = await response.text()
}

const connectStream = () => {
  eventSource.value?.close()
  const source = new EventSource('/admin/api/logs/stream')
  eventSource.value = source

  source.onopen = () => {
    connected.value = true
  }
  source.onerror = () => {
    connected.value = false
  }
  source.addEventListener('log', (event) => {
    if (paused.value) {
      return
    }
    const entry = JSON.parse(event.data) as LogEntry
    entries.value.push(entry)

    const max = Number(limit.value)
    if (entries.value.length > max) {
      entries.value = entries.value.slice(entries.value.length - max)
    }

    if (meta.value) {
      meta.value.updated_at = entry.time
    }
    streamMeta.value = `最近事件 ${new Date(entry.time).toLocaleString('zh-CN')}`

    if (autoScroll.value) {
      requestAnimationFrame(() => {
        const tableContainer = document.getElementById('log-table-container')
        if (tableContainer) {
            tableContainer.scrollTo({ top: tableContainer.scrollHeight, behavior: 'smooth' })
        }
      })
    }
  })
}

const reloadAll = async () => {
  await loadSnapshot()
  await loadMeta()
  await loadRawTail()
}

const clearPage = () => {
  entries.value = []
}

onMounted(async () => {
  await reloadAll()
  connectStream()
})

onBeforeUnmount(() => {
  eventSource.value?.close()
})
</script>

<template>
  <div class="p-6 md:p-8 w-full h-full flex flex-col pt-[calc(3rem)] overflow-y-auto">
    <header class="flex items-center justify-between mb-8 pb-4 border-b border-white/10 shrink-0">
      <h1 class="font-heading font-semibold text-3xl md:text-4xl text-white">
        日志中心
      </h1>
      <div class="flex items-center gap-3">
        <button class="bg-black/40 hover:bg-white/10 text-white font-medium text-xs px-4 py-2 rounded-md border border-white/20 transition-all flex items-center gap-1.5" @click="loadRawTail">
          <AlignLeft class="w-3.5 h-3.5" />
          刷新文件块
        </button>
        <button class="bg-bitcoin hover:bg-burnt text-white font-medium text-xs px-5 py-2 rounded-md transition-colors flex items-center gap-1.5" @click="reloadAll">
          <RefreshCw class="w-3.5 h-3.5" />
          全量刷新
        </button>
      </div>
    </header>

    <div class="flex-1 w-full flex flex-col min-h-0">
      
      <!-- Tabs Header -->
      <div class="flex gap-2 overflow-x-auto mb-6 border-b border-white/10 pb-1 w-full shrink-0">
        <button 
          @click="activeTab = 'logs'"
          class="px-6 py-3 font-medium text-sm rounded-t-lg transition-all duration-300 relative shrink-0 flex items-center gap-2"
          :class="activeTab === 'logs' ? 'text-bitcoin' : 'text-muted hover:text-white hover:bg-white/5'"
        >
          <AlignLeft class="w-4 h-4" />
          流式日志 (Stream)
          <div v-show="activeTab === 'logs'" class="absolute bottom-[-1px] left-0 w-full h-[2px] bg-bitcoin shadow-[0_-2px_8px_rgba(247,147,26,0.6)]"></div>
        </button>
        <button 
          @click="activeTab = 'tail'"
          class="px-6 py-3 font-medium text-sm rounded-t-lg transition-all duration-300 relative shrink-0 flex items-center gap-2"
          :class="activeTab === 'tail' ? 'text-bitcoin' : 'text-muted hover:text-white hover:bg-white/5'"
        >
          <FileTerminal class="w-4 h-4" />
          物理文件 (Tail)
          <div v-show="activeTab === 'tail'" class="absolute bottom-[-1px] left-0 w-full h-[2px] bg-bitcoin shadow-[0_-2px_8px_rgba(247,147,26,0.6)]"></div>
        </button>
      </div>

      <!-- Live Logs View -->
      <transition name="fade" mode="out-in">
        <div v-show="activeTab === 'logs'" class="flex-1 flex flex-col min-h-0 bg-darkmatter border border-white/10 rounded-lg overflow-hidden">
          
          <div class="p-6 shrink-0 border-b border-white/5 bg-black/40">
            <!-- Stats Row -->
            <div class="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
              <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between">
                <div class="text-[10px] text-muted mb-2 uppercase tracking-widest font-semibold">连接状态</div>
                <div class="font-mono text-xl flex items-center gap-2" :class="connected ? 'text-[#10b981]' : 'text-muted'">
                  <span class="w-2 h-2 rounded-full" :class="connected ? 'bg-[#10b981]' : 'bg-muted'"></span>
                  {{ connectionLabel }}
                </div>
              </div>
              <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between">
                <div class="text-[10px] text-muted mb-2 uppercase tracking-widest font-semibold">当前可见</div>
                <div class="font-mono text-xl text-white">{{ visibleEntries.length }}</div>
              </div>
              <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between">
                <div class="text-[10px] text-muted mb-2 uppercase tracking-widest font-semibold">当前缓存</div>
                <div class="font-mono text-xl text-white">{{ entries.length }}</div>
              </div>
              <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between">
                <div class="text-[10px] text-muted mb-2 uppercase tracking-widest font-semibold">最近级别</div>
                <div class="font-mono text-xl" :class="{'text-bitcoin': latestLevel === 'WARN', 'text-[#ef4444]': latestLevel === 'ERROR', 'text-[#10b981]': latestLevel === 'INFO', 'text-muted': latestLevel === 'DEBUG'}">{{ latestLevel }}</div>
              </div>
            </div>

            <!-- Filters Row -->
            <div class="grid grid-cols-1 md:grid-cols-12 gap-4 items-end">
              <div class="flex flex-col gap-1.5 md:col-span-6 lg:col-span-5">
                <label class="text-[10px] font-semibold uppercase tracking-widest text-muted">全文搜索 (RegEx/Text)</label>
                <input v-model="search" type="text" placeholder="定位错误关键词..." class="h-10 px-3 bg-black/50 border-b-[1.5px] border-white/20 text-white text-sm focus-visible:outline-none focus-visible:border-bitcoin transition-all font-mono" />
              </div>
              <div class="flex flex-col gap-1.5 md:col-span-3 lg:col-span-2">
                <label class="text-[10px] font-semibold uppercase tracking-widest text-muted">过滤级别</label>
                <select v-model="levelFilter" class="h-10 w-full font-mono px-3">
                  <option value="">ALL LEVELS</option>
                  <option value="DEBUG">DEBUG</option>
                  <option value="INFO">INFO</option>
                  <option value="WARN">WARN</option>
                  <option value="ERROR">ERROR</option>
                </select>
              </div>
              <div class="flex flex-col gap-1.5 md:col-span-3 lg:col-span-2">
                <label class="text-[10px] font-semibold uppercase tracking-widest text-muted">最大持有量</label>
                <select v-model="limit" @change="reloadAll" class="h-10 w-full font-mono px-3">
                  <option value="100">100 Lines</option>
                  <option value="200">200 Lines</option>
                  <option value="500">500 Lines</option>
                  <option value="1000">1000 Lines</option>
                </select>
              </div>
              <div class="flex flex-col gap-1.5 md:col-span-12 lg:col-span-3 pt-4 lg:pt-0">
                <div class="flex items-center justify-end gap-3 h-10">
                  <label class="flex items-center gap-2 cursor-pointer group mr-auto lg:mr-0 pl-1">
                    <div class="relative flex items-center justify-center">
                      <input type="checkbox" v-model="autoScroll" class="peer sr-only">
                      <div class="w-4 h-4 rounded border border-white/30 peer-checked:bg-bitcoin peer-checked:border-bitcoin flex items-center justify-center transition-all bg-black/50 shadow-inner">
                        <svg class="w-3 h-3 text-white opacity-0 peer-checked:opacity-100 drop-shadow-md" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7"></path></svg>
                      </div>
                    </div>
                    <span class="text-[11px] uppercase tracking-wider font-semibold text-white/50 group-hover:text-white transition-colors">自动跟随</span>
                  </label>
                  
                  <button @click="paused = !paused" class="h-8 px-3 rounded text-[11px] uppercase tracking-widest font-bold border transition-all flex items-center gap-1.5" :class="paused ? 'border-bitcoin bg-bitcoin/20 text-bitcoin' : 'border-white/20 bg-black/40 text-muted hover:text-white hover:bg-white/10 hover:border-white/50'">
                    <component :is="paused ? Play : Pause" class="w-3 h-3" />
                    {{ paused ? '继续' : '暂停' }}
                  </button>
                  <button @click="clearPage" class="h-8 px-3 rounded text-[11px] uppercase tracking-widest font-bold border border-white/20 bg-black/40 text-muted hover:text-red-400 hover:bg-red-400/10 hover:border-red-400/50 transition-all flex items-center gap-1.5" title="清空画布">
                    <Trash2 class="w-3 h-3" />
                  </button>
                </div>
              </div>
            </div>
          </div>

          <!-- Logs Table -->
          <div class="px-4 py-2 bg-black text-[10px] font-mono text-muted/50 border-b border-white/5 shrink-0 flex items-center justify-between">
            <span>{{ streamMeta }}</span>
            <span v-if="paused" class="text-bitcoin font-bold animate-pulse inline-flex items-center gap-1">
              <span class="w-2 h-2 rounded-full bg-bitcoin"></span>
              STREAM PAUSED
            </span>
          </div>

          <div id="log-table-container" class="flex-1 overflow-y-auto bg-black/40 relative">
            <div v-if="visibleEntries.length === 0" class="absolute inset-0 flex items-center justify-center text-muted font-mono text-sm opacity-50 flex-col gap-4">
              <div class="h-16 w-16 border border-white/10 rounded-full flex items-center justify-center"><AlignLeft class="w-6 h-6" /></div>
              <span>无匹配日志条目</span>
            </div>
            
            <table v-else class="w-full text-left border-collapse table-auto">
              <thead class="sticky top-0 bg-darkmatter z-10 shadow-md">
                <tr>
                  <th class="w-24 px-4 py-2 text-[10px] uppercase tracking-widest text-muted font-semibold border-b border-white/10">时间</th>
                  <th class="w-20 px-4 py-2 text-[10px] uppercase tracking-widest text-muted font-semibold border-b border-white/10">级别</th>
                  <th class="px-4 py-2 text-[10px] uppercase tracking-widest text-muted font-semibold border-b border-white/10">消息</th>
                  <th class="px-4 py-2 text-[10px] uppercase tracking-widest text-muted font-semibold border-b border-white/10">负载</th>
                </tr>
              </thead>
              <tbody class="font-mono text-xs">
                <tr v-for="entry in visibleEntries" :key="entry.id" class="border-b border-white/5 hover:bg-white/5 transition-colors group align-top">
                  <td class="px-4 py-3 text-muted/70 group-hover:text-white/90">
                    {{ entry.time ? new Date(entry.time).toLocaleTimeString('zh-CN', { hour12: false }) : '-' }}
                  </td>
                  <td class="px-4 py-3">
                    <span class="px-2 py-0.5 rounded text-[10px] font-bold tracking-wider" 
                          :class="{
                            'bg-bitcoin/20 text-bitcoin' : entry.level === 'WARN',
                            'bg-[#ef4444]/20 text-[#ef4444]' : entry.level === 'ERROR',
                            'bg-[#10b981]/20 text-[#10b981]' : entry.level === 'INFO',
                            'bg-white/10 text-muted' : entry.level === 'DEBUG'
                          }">
                      {{ entry.level }}
                    </span>
                  </td>
                  <td class="px-4 py-3 text-white/90 break-words font-body text-sm leading-relaxed">
                    {{ entry.message }}
                  </td>
                  <td class="px-4 py-3 text-muted group-hover:text-white/80 whitespace-pre-wrap break-words leading-relaxed">
                    {{ entry.text }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </transition>

      <!-- Tail File View -->
      <transition name="fade" mode="out-in">
        <div v-show="activeTab === 'tail'" class="flex-1 flex flex-col min-h-0 bg-darkmatter border border-white/10 rounded-lg overflow-hidden mb-12">
          
          <div class="p-6 shrink-0 border-b border-white/5 bg-black/40">
            <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
              <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between">
                <div class="text-[10px] text-muted mb-2 uppercase tracking-widest font-semibold">日志路径</div>
                <div class="font-mono text-xs text-white/80 break-all leading-tight">{{ meta?.path || '-' }}</div>
              </div>
              <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between">
                <div class="text-[10px] text-muted mb-2 uppercase tracking-widest font-semibold">文件状态</div>
                <div class="font-mono text-lg font-bold flex items-center gap-2" :class="meta?.exists ? 'text-[#10b981]' : 'text-[#ef4444]'">
                  <span class="w-2 h-2 rounded-full" :class="meta?.exists ? 'bg-[#10b981]' : 'bg-[#ef4444]'"></span>
                  {{ meta?.exists ? 'EXISTS' : 'MISSING' }}
                </div>
              </div>
              <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between">
                <div class="text-[10px] text-muted mb-2 uppercase tracking-widest font-semibold">文件大小</div>
                <div class="font-mono text-2xl text-bitcoin drop-shadow-md">{{ formatBytes(meta?.size_bytes || 0) }}</div>
              </div>
              <div class="bg-black/40 border border-white/5 p-4 rounded-xl flex flex-col justify-between">
                <div class="text-[10px] text-muted mb-2 uppercase tracking-widest font-semibold">最近写入</div>
                <div class="font-mono text-sm text-white/90">{{ meta?.updated_at ? new Date(meta.updated_at).toLocaleString('zh-CN') : '-' }}</div>
              </div>
            </div>
          </div>

          <div class="flex-1 p-0 overflow-hidden bg-black/40 relative">
            <div class="absolute inset-0 p-4 border border-x-0 border-b-0 border-white/5 overflow-y-auto">
              <pre class="font-mono text-xs text-white/80 whitespace-pre-wrap break-words leading-loose">{{ rawTail }}</pre>
            </div>
          </div>
        </div>
      </transition>

    </div>
  </div>
</template>
