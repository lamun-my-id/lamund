<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type AnalyticsOverview } from '../lib/api'

const { t } = useI18n()
const data = ref<AnalyticsOverview | null>(null)
const loaded = ref(false)

function human(bytes: number): string {
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let n = bytes
  let i = 0
  while (n >= 1024 && i < u.length - 1) {
    n /= 1024
    i++
  }
  return `${n.toFixed(n < 10 && i > 0 ? 1 : 0)} ${u[i]}`
}
function shortTime(iso: string): string {
  const d = new Date(iso)
  return isNaN(d.getTime()) ? iso : d.toLocaleString()
}

onMounted(async () => {
  try {
    data.value = await api.analyticsOverview()
  } catch {
    /* diamkan: analitik opsional */
  } finally {
    loaded.value = true
  }
})
</script>

<template>
  <div v-if="loaded && data" class="an-wrap">
    <div class="card an-totals">
      <div class="an-t">
        <div class="p">{{ data.total_requests.toLocaleString() }}</div>
        <div class="lbl">{{ t('analytics.requests') }}</div>
      </div>
      <div class="an-t">
        <div class="p">{{ human(data.total_bytes) }}</div>
        <div class="lbl">{{ t('analytics.bandwidth') }}</div>
      </div>
      <div class="an-t">
        <div class="p" :class="{ bad: data.total_errors > 0 }">{{ data.total_errors.toLocaleString() }}</div>
        <div class="lbl">{{ t('analytics.errors') }}</div>
      </div>
    </div>

    <div class="an-cols">
      <div class="card an-list">
        <div class="an-h">{{ t('analytics.topAccessed') }}</div>
        <template v-if="data.top_accessed.length">
          <div v-for="d in data.top_accessed" :key="d.domain" class="an-row">
            <span class="dom">{{ d.domain }}</span>
            <span class="num">{{ d.requests.toLocaleString() }}</span>
          </div>
        </template>
        <p v-else class="sub">{{ t('analytics.empty') }}</p>
      </div>

      <div class="card an-list">
        <div class="an-h">{{ t('analytics.recentErrors') }}</div>
        <template v-if="data.recent_errors.length">
          <div v-for="(e, i) in data.recent_errors" :key="i" class="an-row">
            <span class="st bad">{{ e.status }}</span>
            <span class="dom">{{ e.domain }}<small>{{ e.path }}</small></span>
            <span class="num muted">{{ shortTime(e.time) }}</span>
          </div>
        </template>
        <p v-else class="sub">{{ t('analytics.noErrors') }}</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.an-wrap { margin-bottom: 22px; display: grid; gap: 14px; }
.an-totals { display: grid; grid-template-columns: repeat(3, 1fr); gap: 24px; padding: 18px 22px; }
.an-t .p { font-family: var(--font-display); font-weight: 700; font-size: 24px; line-height: 1; }
.an-t .p.bad { color: #d64550; }
.an-t .lbl { font-size: 12px; color: var(--muted); margin-top: 6px; }
.an-cols { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; }
.an-list { padding: 16px 20px; }
.an-h { font-size: 12px; font-weight: 700; color: var(--slate); text-transform: uppercase; letter-spacing: .06em; margin-bottom: 12px; }
.an-row { display: flex; align-items: center; gap: 10px; padding: 7px 0; border-top: 1px solid var(--line); font-size: 13.5px; }
.an-row:first-of-type { border-top: none; }
.an-row .dom { font-weight: 600; color: var(--ink); display: flex; flex-direction: column; }
.an-row .dom small { color: var(--muted); font-weight: 400; font-size: 11.5px; }
.an-row .num { margin-left: auto; font-variant-numeric: tabular-nums; }
.an-row .num.muted { color: var(--muted); font-size: 12px; }
.an-row .st { font-weight: 700; font-variant-numeric: tabular-nums; }
.an-row .st.bad { color: #d64550; }
@media (max-width: 820px) { .an-cols { grid-template-columns: 1fr; } .an-totals { grid-template-columns: 1fr 1fr 1fr; } }
</style>
