<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, type Site } from '../lib/api'
import { useScope } from '../stores/scope'
import Pulse from '../components/Pulse.vue'

const { t } = useI18n()
const scope = useScope()

const sites = ref<Site[]>([])
const appSet = ref<Set<string>>(new Set())
const loading = ref(true)
const error = ref('')
const filter = ref<'all' | 'static' | 'proxy'>('all')

// Deployment yang didukung run-app (punya proses) — dibedakan badge "app".
function isApp(domain: string): boolean {
  return appSet.value.has(domain)
}

// Situs dalam scope aktif (Personal / tim tertentu).
const inScope = computed(() => sites.value.filter((s) => scope.matches(s.owner_type, s.owner_id)))
const shown = computed(() =>
  filter.value === 'all' ? inScope.value : inScope.value.filter((s) => s.type === filter.value),
)
const activeCount = computed(() => inScope.value.filter((s) => s.status === 'active').length)

function pulseState(s: Site): 'on' | 'warn' | 'off' {
  if (s.status === 'active') return 'on'
  if (s.status === 'disabled') return 'off'
  return 'warn'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    sites.value = await api.listSites()
    // App = deployment berproses; domainnya juga muncul sbg situs proxy.
    const apps = await api.listApps().catch(() => [])
    appSet.value = new Set(apps.map((a) => a.domain))
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>

<template>
  <div class="page-head">
    <div>
      <div class="h1">{{ t('sites.title') }}</div>
      <div class="sub" v-if="!loading">
        {{ t('sites.summary', { active: activeCount, total: inScope.length }) }}
      </div>
    </div>
    <RouterLink class="btn pri" :to="{ name: 'site-new' }">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M12 5v14M5 12h14"/></svg>
      {{ t('sites.add') }}
    </RouterLink>
  </div>

  <div v-if="error" class="alert err">{{ error }}</div>
  <div v-if="loading" class="spin">{{ t('common.loading') }}</div>

  <template v-else>
    <div v-if="inScope.length" class="tabs">
      <button :class="{ on: filter === 'all' }" @click="filter = 'all'">{{ t('sites.filterAll') }}</button>
      <button :class="{ on: filter === 'static' }" @click="filter = 'static'">{{ t('sites.filterStatic') }}</button>
      <button :class="{ on: filter === 'proxy' }" @click="filter = 'proxy'">{{ t('sites.filterProxy') }}</button>
    </div>

    <div v-if="shown.length" class="list">
      <div v-for="s in shown" :key="s.domain" class="row">
        <Pulse :state="pulseState(s)" />
        <div>
          <RouterLink class="dom" :to="{ name: 'site-detail', params: { domain: s.domain } }">{{ s.domain }}</RouterLink>
          <div class="meta">
            <span class="tag" :class="isApp(s.domain) ? 't-app' : s.type">{{ isApp(s.domain) ? 'app' : s.type }}</span>
            <span>{{ s.type === 'proxy' ? '→ ' + s.proxy_target : s.root_path }}</span>
          </div>
        </div>
        <div class="side-info"><b>{{ s.status }}</b></div>
        <div style="display: flex; gap: 8px">
          <RouterLink v-if="isApp(s.domain)" class="btn sm" :to="{ name: 'apps' }">{{ t('nav.apps') }}</RouterLink>
          <RouterLink class="btn sm" :to="{ name: 'site-detail', params: { domain: s.domain } }">{{ t('common.manage') }}</RouterLink>
        </div>
      </div>
    </div>

    <div v-else class="card empty">
      <h3>{{ t('sites.empty') }}</h3>
      <p>{{ t('sites.emptyHint') }}</p>
      <RouterLink class="btn pri" style="margin-top: 16px" :to="{ name: 'site-new' }">{{ t('sites.add') }}</RouterLink>
    </div>
  </template>
</template>
