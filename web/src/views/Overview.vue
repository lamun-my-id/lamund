<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type Site, type App, type Cert, type ServerStats, type AnalyticsOverview } from '../lib/api'
import { useScope } from '../stores/scope'

const { t } = useI18n()
const scope = useScope()

const sites = ref<Site[]>([])
const apps = ref<App[]>([])
const certs = ref<Cert[]>([])
const stats = ref<ServerStats | null>(null)
const an = ref<AnalyticsOverview | null>(null)

const inScope = computed(() => sites.value.filter((s) => scope.matches(s.owner_type, s.owner_id)))
const appSet = computed(() => new Set(apps.value.filter((a) => scope.matches(a.owner_type, a.owner_id)).map((a) => a.domain)))
const activeCount = computed(() => inScope.value.filter((s) => s.status === 'active').length)
const runningApps = computed(() => apps.value.filter((a) => scope.matches(a.owner_type, a.owner_id) && a.state === 'running').length)
const validCerts = computed(() => certs.value.filter((c) => c.status === 'valid').length)

// 5 deployment terbaru (dari created_at situs dalam scope).
const recent = computed(() =>
  [...inScope.value]
    .sort((a, b) => (b.created_at ?? '').localeCompare(a.created_at ?? ''))
    .slice(0, 5),
)

function pct(u: number, tot: number) {
  return tot > 0 ? Math.round((u / tot) * 100) : 0
}
function gb(b: number) {
  return (b / 1024 / 1024 / 1024).toFixed(1)
}
function human(bytes: number): string {
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  let n = bytes
  let i = 0
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(n < 10 && i > 0 ? 1 : 0)} ${u[i]}`
}
function typeTag(s: Site): { cls: string; label: string } {
  if (appSet.value.has(s.domain)) return { cls: 't-app', label: 'app' }
  return s.type === 'proxy' ? { cls: 'proxy', label: 'proxy' } : { cls: 'static', label: 'static' }
}
const mem = computed(() => (stats.value ? pct(stats.value.mem_used, stats.value.mem_total) : 0))
const swap = computed(() => (stats.value ? pct(stats.value.swap_used, stats.value.swap_total) : 0))
const disk = computed(() => (stats.value ? pct(stats.value.disk_used, stats.value.disk_total) : 0))
function barCls(p: number) {
  return p >= 90 ? 'hot' : p >= 70 ? 'warn' : ''
}

onMounted(async () => {
  sites.value = await api.listSites().catch(() => [])
  apps.value = await api.listApps().catch(() => [])
  certs.value = await api.listCerts().catch(() => [])
  stats.value = await api.serverStats().catch(() => null)
  an.value = await api.analyticsOverview().catch(() => null)
})
</script>

<template>
  <div class="page-head">
    <div>
      <div class="eyebrow"><span class="pulse"><i></i></span> {{ t('overview.healthy', { n: inScope.length }) }}</div>
      <div class="h1">{{ t('overview.heading') }}</div>
      <div class="sub">{{ t('overview.tagline') }}</div>
    </div>
    <RouterLink class="btn pri" :to="{ name: 'site-new' }">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M12 5v14M5 12h14"/></svg>
      {{ t('wizard.title') }}
    </RouterLink>
  </div>

  <div class="stats">
    <div class="stat"><div class="n">{{ activeCount }}</div><div class="l"><span class="dot"></span> {{ t('overview.activeDeploys') }}</div></div>
    <div class="stat"><div class="n">{{ runningApps }}</div><div class="l"><span class="dot"></span> {{ t('overview.runningApps') }}</div></div>
    <div class="stat"><div class="n">{{ validCerts }}</div><div class="l"><span class="dot"></span> {{ t('overview.validCerts') }}</div></div>
    <div class="stat"><div class="n" style="color: var(--brand)">{{ an ? an.total_requests.toLocaleString() : '—' }}</div><div class="l">{{ t('overview.requests24h') }}</div></div>
  </div>

  <!-- Status server -->
  <div v-if="stats" class="card" style="padding: 22px 24px; margin-bottom: 26px">
    <div class="st-sec">{{ t('server.title') }}</div>
    <div class="res">
      <div class="r">
        <div class="pct">{{ Math.round(stats.cpu_percent) }}%</div>
        <div class="top"><span class="lbl">{{ t('server.cpu') }}</span></div>
        <div class="bar"><i :class="barCls(stats.cpu_percent)" :style="{ width: stats.cpu_percent + '%' }"></i></div>
      </div>
      <div class="r">
        <div class="pct">{{ mem }}%</div>
        <div class="top"><span class="lbl">{{ t('server.ram') }}</span><span class="val">{{ gb(stats.mem_used) }} / {{ gb(stats.mem_total) }} GB</span></div>
        <div class="bar"><i :class="barCls(mem)" :style="{ width: mem + '%' }"></i></div>
      </div>
      <div class="r">
        <div class="pct">{{ swap }}%</div>
        <div class="top"><span class="lbl">{{ t('server.swap') }}</span><span class="val">{{ gb(stats.swap_used) }} / {{ gb(stats.swap_total) }} GB</span></div>
        <div class="bar"><i :class="barCls(swap)" :style="{ width: swap + '%' }"></i></div>
      </div>
      <div class="r">
        <div class="pct">{{ disk }}%</div>
        <div class="top"><span class="lbl">{{ t('server.disk') }}</span><span class="val">{{ gb(stats.disk_used) }} / {{ gb(stats.disk_total) }} GB</span></div>
        <div class="bar"><i :class="barCls(disk)" :style="{ width: disk + '%' }"></i></div>
      </div>
    </div>
  </div>

  <div class="grid-2" style="margin-bottom: 30px">
    <!-- Trafik 24 jam -->
    <div class="card" style="padding: 20px 22px">
      <div class="sec" style="margin: 0 0 8px"><h3>{{ t('analytics.title') }}</h3></div>
      <div style="display: flex; align-items: baseline; gap: 10px">
        <span style="font-family: var(--font-display); font-weight: 700; font-size: 26px">{{ an ? an.total_requests.toLocaleString() : '0' }}</span>
        <span class="sub" style="font-size: 13px">{{ t('analytics.requests').toLowerCase() }} · {{ an ? human(an.total_bytes) : '0 B' }}</span>
      </div>
    </div>
    <!-- Paling sering diakses -->
    <div class="card" style="padding: 20px 22px">
      <div class="sec" style="margin: 0 0 10px"><h3>{{ t('analytics.topAccessed') }}</h3></div>
      <div v-if="an && an.top_accessed.length" class="toplist">
        <div v-for="(d, i) in an.top_accessed" :key="d.domain" class="t">
          <span class="rank">{{ i + 1 }}</span>
          <span class="path">{{ d.domain }}</span>
          <span class="hits">{{ d.requests.toLocaleString() }}</span>
        </div>
      </div>
      <p v-else class="sub">{{ t('analytics.empty') }}</p>
    </div>
  </div>

  <div class="grid-2">
    <!-- Deployment terbaru -->
    <div class="card" style="padding: 20px 22px">
      <div class="sec" style="margin: 0 0 6px"><h3>{{ t('overview.recentDeploys') }}</h3><RouterLink :to="{ name: 'sites' }">{{ t('overview.all') }} →</RouterLink></div>
      <div v-if="recent.length" style="display: flex; flex-direction: column">
        <RouterLink v-for="s in recent" :key="s.domain" class="dep" :to="{ name: 'site-detail', params: { domain: s.domain } }">
          <span class="pulse" :class="{ off: s.status !== 'active' }"><i></i></span>
          <div class="nm">
            <div class="d">{{ s.domain }}</div>
            <small><span class="tag" :class="typeTag(s).cls">{{ typeTag(s).label }}</span></small>
          </div>
          <span class="when">{{ (s.created_at || '').slice(0, 10) }}</span>
        </RouterLink>
      </div>
      <p v-else class="sub">{{ t('sites.emptyHint') }}</p>
    </div>
    <!-- Error terakhir -->
    <div class="card" style="padding: 20px 22px">
      <div class="sec" style="margin: 0 0 10px"><h3>{{ t('analytics.recentErrors') }}</h3></div>
      <div v-if="an && an.recent_errors.length" class="errs">
        <div v-for="(e, i) in an.recent_errors" :key="i" class="e">
          <span class="dot" :class="e.status >= 500 ? 'e5' : 'e4'"></span>
          <span class="code" :class="e.status >= 500 ? 'c5' : 'c4'">{{ e.status }}</span>
          <span class="path">{{ e.path }}</span>
          <span class="src">{{ e.domain }}</span>
        </div>
      </div>
      <p v-else class="sub">{{ t('analytics.noErrors') }}</p>
    </div>
  </div>
</template>
