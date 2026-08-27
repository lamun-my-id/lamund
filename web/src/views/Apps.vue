<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { confirmDialog } from '../lib/dialog'
import { api, ApiError, type App } from '../lib/api'
import { useScope } from '../stores/scope'
import Pulse from '../components/Pulse.vue'

const { t } = useI18n()
const scope = useScope()
const allApps = ref<App[]>([])
// Hanya app dalam scope aktif (Personal / tim tertentu).
const apps = computed(() => allApps.value.filter((a) => scope.matches(a.owner_type, a.owner_id)))
const loading = ref(true)
const error = ref('')

const showForm = ref(false)
const na = ref({ domain: '', command: 'npm start', autostart: true, repo_url: '', branch: 'main' })
const infoFor = ref<string | null>(null)
// Webhook disajikan API (same-origin dengan panel) — bukan domain app.
const origin = window.location.origin

// env editor
const envFor = ref<string | null>(null)
const envRows = ref<{ k: string; v: string }[]>([])
const envMsg = ref('')
const envBusy = ref(false)

async function showEnv(a: App) {
  envFor.value = a.domain
  logsFor.value = null
  buildFor.value = null
  infoFor.value = null
  envMsg.value = ''
  const env = await api.getEnv(a.domain).catch(() => ({}) as Record<string, string>)
  envRows.value = Object.entries(env).map(([k, v]) => ({ k, v }))
  if (!envRows.value.length) envRows.value = [{ k: '', v: '' }]
}
function addEnvRow() {
  envRows.value.push({ k: '', v: '' })
}
async function saveEnv(a: App) {
  envBusy.value = true
  envMsg.value = ''
  const env: Record<string, string> = {}
  for (const row of envRows.value) if (row.k.trim()) env[row.k.trim()] = row.v
  try {
    await api.putEnv(a.domain, env)
    envMsg.value = a.state === 'running' ? t('apps.envSavedRestart') : t('apps.envSaved') + '.'
  } catch (e) {
    envMsg.value = (e as Error).message
  } finally {
    envBusy.value = false
  }
}
const formErr = ref('')
const busy = ref(false)

const logsFor = ref<string | null>(null)
const logLines = ref<string[]>([])

// build
const buildFor = ref<string | null>(null)
const buildLines = ref<string[]>([])
const buildStatus = ref('')
let buildTimer: number | undefined

function pulseState(s: App): 'on' | 'warn' | 'off' {
  if (s.state === 'running') return 'on'
  if (s.state === 'failed' || s.state === 'crashed') return 'warn'
  return 'off'
}

async function load() {
  loading.value = true
  try {
    allApps.value = await api.listApps()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function create() {
  formErr.value = ''
  busy.value = true
  try {
    const owner =
      scope.current.type === 'team'
        ? { owner_type: 'team', owner_id: scope.current.id }
        : { owner_type: 'user' }
    await api.createApp({ ...na.value, ...owner })
    showForm.value = false
    na.value = { domain: '', command: 'npm start', autostart: true, repo_url: '', branch: 'main' }
    await load()
  } catch (e) {
    formErr.value = e instanceof ApiError ? e.message : t('apps.createFailed')
  } finally {
    busy.value = false
  }
}

async function act(a: App, fn: (d: string) => Promise<unknown>) {
  try {
    await fn(a.domain)
    await load()
    if (logsFor.value === a.domain) await showLogs(a)
  } catch (e) {
    error.value = (e as Error).message
  }
}

async function remove(a: App) {
  if (!(await confirmDialog({ message: t('apps.deleteConfirm', { domain: a.domain }) }))) return
  await act(a, api.deleteApp)
}

async function showLogs(a: App) {
  logsFor.value = a.domain
  buildFor.value = null
  logLines.value = await api.appLogs(a.domain, 120).catch(() => [])
}

// Build: picu build asinkron lalu poll status + log sampai selesai.
async function build(a: App) {
  await runBuild(a, () => api.buildApp(a.domain))
}
// Deploy git: pull repo → build → restart (satu tombol).
async function deployGit(a: App) {
  await runBuild(a, () => api.deployGit(a.domain))
}

async function runBuild(a: App, trigger: () => Promise<unknown>) {
  buildFor.value = a.domain
  logsFor.value = null
  infoFor.value = null
  buildStatus.value = 'building'
  buildLines.value = []
  try {
    await trigger()
  } catch (e) {
    buildStatus.value = 'failed'
    buildLines.value = [(e as Error).message]
    return
  }
  clearInterval(buildTimer)
  buildTimer = window.setInterval(async () => {
    buildLines.value = await api.buildLogs(a.domain).catch(() => [])
    const st = await api.buildStatus(a.domain).catch(() => null)
    if (st) buildStatus.value = st.status
    if (st && (st.status === 'success' || st.status === 'failed')) {
      clearInterval(buildTimer)
      await load()
    }
  }, 1200)
}

onUnmounted(() => clearInterval(buildTimer))
onMounted(load)
</script>

<template>
  <div class="page-head">
    <div>
      <div class="h1">{{ t('apps.title') }}</div>
      <div class="sub">{{ t('apps.subtitle') }}</div>
    </div>
    <button class="btn pri" @click="showForm = !showForm">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M12 5v14M5 12h14"/></svg>
      {{ t('apps.add') }}
    </button>
  </div>

  <div v-if="error" class="alert err">{{ error }}</div>

  <form v-if="showForm" class="card" style="padding: 20px; margin-bottom: 18px" @submit.prevent="create">
    <div v-if="formErr" class="alert err">{{ formErr }}</div>
    <div class="field">
      <label>{{ t('apps.domain') }}</label>
      <div class="input"><input v-model="na.domain" class="mono" placeholder="app.contoh.com" required /></div>
      <div class="hint">{{ t('apps.domainHint') }}</div>
    </div>
    <div class="field">
      <label>{{ t('apps.command') }}</label>
      <div class="input"><input v-model="na.command" class="mono" placeholder="npm start" required /></div>
      <div class="hint">
        <i18n-t keypath="apps.commandHint" tag="span">
          <template #port><code class="mono">$PORT</code></template>
        </i18n-t>
      </div>
    </div>
    <div class="field">
      <label>{{ t('apps.gitRepo') }} <span style="color: var(--faint); font-weight: 400">{{ t('apps.gitRepoOptional') }}</span></label>
      <div class="input"><input v-model="na.repo_url" class="mono" placeholder="https://github.com/user/repo.git" /></div>
    </div>
    <div v-if="na.repo_url" class="field">
      <label>{{ t('apps.branch') }}</label>
      <div class="input"><input v-model="na.branch" class="mono" placeholder="main" /></div>
    </div>
    <label class="rcache" style="margin-bottom: 14px"><input type="checkbox" v-model="na.autostart" /> {{ t('apps.autostart') }}</label>
    <div><button class="btn pri" type="submit" :disabled="busy">{{ busy ? t('apps.creating') : t('apps.createRun') }}</button></div>
  </form>

  <div v-if="loading" class="spin">{{ t('common.loading') }}</div>
  <div v-else-if="!apps.length" class="card empty">
    <h3>{{ t('apps.emptyTitle') }}</h3>
    <p>{{ t('apps.emptyHint') }}</p>
  </div>

  <div v-else class="app-list">
    <div v-for="a in apps" :key="a.domain" class="card app-card">
      <div class="app-head">
        <div class="ainfo">
          <div class="dom">
            <Pulse :state="pulseState(a)" />
            <span class="mono">{{ a.domain }}</span>
          </div>
          <div class="sub meta">
            <span class="tag t-app">{{ a.command.split(' ')[0] }}</span>
            <span class="mini" :class="a.state === 'running' ? 'ok' : pulseState(a) === 'warn' ? 'build' : 'off'">{{ a.state }}</span>
            <span class="dot-sep">·</span>
            <span class="mono">:{{ a.port }}</span>
            <span class="dot-sep">·</span>
            <span class="mono cmd">{{ a.command }}</span>
            <span v-if="a.restarts" class="restarts">↻ {{ a.restarts }}</span>
          </div>
        </div>
        <div class="acts">
          <button v-if="a.repo_url" class="btn pri sm" @click="deployGit(a)">{{ t('apps.deployGit') }}</button>
          <button v-else class="btn pri sm" @click="build(a)">{{ t('apps.build') }}</button>
          <button v-if="a.state !== 'running'" class="btn sm" @click="act(a, api.startApp)">{{ t('apps.start') }}</button>
          <button v-else class="btn sm" @click="act(a, api.stopApp)">{{ t('apps.stop') }}</button>
          <button class="btn sm" @click="act(a, api.restartApp)">{{ t('apps.restart') }}</button>
          <button v-if="a.repo_url" class="btn sm" @click="build(a)">{{ t('apps.build') }}</button>
          <button class="btn sm" @click="showLogs(a)">{{ t('apps.log') }}</button>
          <button class="btn sm" @click="showEnv(a)">{{ t('apps.env') }}</button>
          <button v-if="a.webhook_path" class="btn sm" @click="infoFor = infoFor === a.domain ? null : a.domain">{{ t('apps.webhook') }}</button>
          <button class="btn sm danger" @click="remove(a)">{{ t('common.delete') }}</button>
        </div>
      </div>

      <div v-if="envFor === a.domain" class="panel">
        <div class="st-sec">{{ t('apps.env') }}</div>
        <div class="sub" style="margin-bottom: 12px">{{ t('apps.envHint') }}</div>
        <div v-if="envMsg" class="alert ok">{{ envMsg }}</div>
        <div class="env-rows">
          <div v-for="(row, i) in envRows" :key="i" class="env-row">
            <div class="input" style="width: 220px"><input v-model="row.k" class="mono" :placeholder="t('apps.envName')" /></div>
            <div class="input" style="flex: 1; min-width: 180px"><input v-model="row.v" class="mono" :placeholder="t('apps.envValue')" /></div>
            <button class="btn sm danger" @click="envRows.splice(i, 1)">✕</button>
          </div>
        </div>
        <div class="panel-acts">
          <button class="btn sm" @click="addEnvRow">{{ t('apps.addVariable') }}</button>
          <button class="btn pri sm" :disabled="envBusy" @click="saveEnv(a)">{{ envBusy ? t('apps.savingEnv') : t('apps.saveEnv') }}</button>
        </div>
      </div>

      <div v-if="infoFor === a.domain" class="panel">
        <div class="st-sec">{{ t('apps.webhook') }}</div>
        <p class="sub" style="margin-bottom: 12px">{{ t('apps.webhookIntro') }}</p>
        <div class="clines">
          <div><span>Payload URL</span><b class="mono" style="font-size: 11px; user-select: all">{{ origin }}{{ a.webhook_path }}</b></div>
          <div><span>Content type</span><b>application/json</b></div>
          <div><span>Secret</span><b class="mono" style="font-size: 11px; user-select: all">{{ a.webhook_secret }}</b></div>
          <div><span>Events</span><b>{{ t('apps.webhookEvents') }}</b></div>
        </div>
        <p class="sub" style="margin-top: 12px">
          <i18n-t keypath="apps.webhookAfter" tag="span">
            <template #push><code class="mono">git push</code></template>
            <template #branch><b>{{ a.branch }}</b></template>
          </i18n-t>
        </p>
      </div>

      <div v-if="logsFor === a.domain" class="panel">
        <div class="st-sec">{{ t('apps.log') }}</div>
        <div class="logs">{{ logLines.length ? logLines.join('\n') : t('apps.noLogs') }}</div>
      </div>

      <div v-if="buildFor === a.domain" class="panel">
        <div class="st-sec">
          {{ t('apps.buildLabel') }}
          <span class="sub" style="text-transform: none; font-weight: 400">— <b :style="{ color: buildStatus === 'failed' ? 'var(--danger)' : buildStatus === 'success' ? 'var(--ok)' : 'var(--purple)' }">{{ buildStatus }}</b></span>
        </div>
        <div class="logs">{{ buildLines.length ? buildLines.join('\n') : t('apps.buildPreparing') }}</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.app-list { display: flex; flex-direction: column; gap: 16px; }
.app-card { padding: 18px 22px; }

/* head: pulse + mono domain + state, then actions */
.app-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; flex-wrap: wrap; }
.ainfo { min-width: 0; }
.dom { display: flex; align-items: center; gap: 12px; font-family: var(--font-mono); font-size: 18px; font-weight: 500; color: var(--ink); letter-spacing: -.2px; }
.meta { display: flex; align-items: center; gap: 8px; margin-top: 8px; flex-wrap: wrap; }
.meta .dot-sep { color: var(--faint); }
.meta .cmd { color: var(--faint); }
.meta .restarts { color: var(--warn); font-weight: 600; }
.acts { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }

/* expandable panels — card-styled sections inside the app card */
.panel { margin-top: 16px; padding-top: 16px; border-top: 1px solid var(--line); }
.env-rows { display: flex; flex-direction: column; gap: 8px; }
.env-row { display: flex; gap: 8px; align-items: center; }
.panel-acts { display: flex; gap: 10px; margin-top: 12px; }
.rcache { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; color: var(--slate); }

@media (max-width: 720px) {
  .app-head { flex-direction: column; }
  .acts { width: 100%; }
}
</style>
