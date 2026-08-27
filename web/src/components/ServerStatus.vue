<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type ServerStats } from '../lib/api'

const { t } = useI18n()

const s = ref<ServerStats | null>(null)
const failed = ref(false)
let timer: number | undefined

function gb(bytes: number): string {
  return (bytes / 1024 / 1024 / 1024).toFixed(1)
}
function pct(used: number, total: number): number {
  return total > 0 ? Math.round((used / total) * 100) : 0
}
const mem = computed(() => (s.value ? pct(s.value.mem_used, s.value.mem_total) : 0))
const swap = computed(() => (s.value ? pct(s.value.swap_used, s.value.swap_total) : 0))
const disk = computed(() => (s.value ? pct(s.value.disk_used, s.value.disk_total) : 0))

function cls(p: number) {
  return p >= 90 ? 'hot' : p >= 70 ? 'warn' : ''
}

async function load() {
  try {
    s.value = await api.serverStats()
    failed.value = false
  } catch {
    failed.value = true
  }
}
onMounted(() => {
  load()
  timer = window.setInterval(load, 5000)
})
onUnmounted(() => clearInterval(timer))
</script>

<template>
  <div v-if="s && !failed" class="card srv">
    <div class="srv-h">{{ t('server.title') }}</div>
    <div class="res">
      <div class="r">
        <div class="p">{{ Math.round(s.cpu_percent) }}%</div>
        <div class="lbl">{{ t('server.cpu') }}</div>
        <div class="bar"><i :class="cls(s.cpu_percent)" :style="{ width: s.cpu_percent + '%' }"></i></div>
      </div>
      <div class="r">
        <div class="p">{{ mem }}%</div>
        <div class="lbl">{{ t('server.ram') }} · {{ gb(s.mem_used) }} / {{ gb(s.mem_total) }} GB</div>
        <div class="bar"><i :class="cls(mem)" :style="{ width: mem + '%' }"></i></div>
      </div>
      <div class="r">
        <div class="p">{{ swap }}%</div>
        <div class="lbl">{{ t('server.swap') }} · {{ gb(s.swap_used) }} / {{ gb(s.swap_total) }} GB</div>
        <div class="bar"><i :class="cls(swap)" :style="{ width: swap + '%' }"></i></div>
      </div>
      <div class="r">
        <div class="p">{{ disk }}%</div>
        <div class="lbl">{{ t('server.disk') }} · {{ gb(s.disk_used) }} / {{ gb(s.disk_total) }} GB</div>
        <div class="bar"><i :class="cls(disk)" :style="{ width: disk + '%' }"></i></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.srv { padding: 18px 22px; margin-bottom: 22px; }
.srv-h { font-size: 12px; font-weight: 700; color: var(--slate); text-transform: uppercase; letter-spacing: .06em; margin-bottom: 16px; }
.res { display: grid; grid-template-columns: repeat(4, 1fr); gap: 24px; }
.res .p { font-family: var(--font-display); font-weight: 700; font-size: 22px; line-height: 1; }
.res .lbl { font-size: 12px; color: var(--muted); margin: 6px 0 8px; }
.res .bar { height: 8px; border-radius: 6px; background: var(--line-2); overflow: hidden; }
.res .bar i { display: block; height: 100%; border-radius: 6px; background: linear-gradient(90deg, var(--purple-l), var(--purple)); }
.res .bar i.warn { background: linear-gradient(90deg, #f0b64d, #dd982a); }
.res .bar i.hot { background: linear-gradient(90deg, #f0798a, #d64550); }
@media (max-width: 720px) { .res { grid-template-columns: 1fr 1fr; } }
</style>
