<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { confirmDialog, promptDialog } from '../lib/dialog'
import { api, type Site, type Cert, type SiteFile, type RouteRule, type App, type DomainReport } from '../lib/api'
import { useAuth } from '../stores/auth'
import Pulse from '../components/Pulse.vue'

const { t } = useI18n()
const auth = useAuth()

const props = defineProps<{ domain: string }>()

const activeTab = ref<'stats' | 'routing' | 'files' | 'logs' | 'settings' | 'domains' | 'git'>('stats')
const router = useRouter()

const site = ref<Site | null>(null)
const cert = ref<Cert | null>(null)
const logs = ref<string[]>([])
const files = ref<SiteFile[]>([])
const loading = ref(true)
const error = ref('')
const notice = ref('')

// deploy
const picked = ref<File | null>(null)
const deploying = ref(false)
const deployErr = ref('')

// deploy-git (git-backed sites)
const deployingGit = ref(false)
const deployLog = ref<string[]>([])
const deployStatus = ref('')
const deployLogVisible = ref(false)
type Deploy = { id: number; status: string; trigger: string; commit: string; message: string; started_at: string; finished_at: string }
const deploys = ref<Deploy[]>([])

// connect-git (menghubungkan situs static non-git ke repo)
const cRepo = ref('')
const cBranch = ref('main')
const cBuild = ref('')
const cOut = ref('')
const connecting = ref(false)

// GitHub import ala Vercel: picker repo existing + bikin repo baru.
type GhRepo = { full_name: string; private: boolean; clone_url: string }
const ghRepos = ref<GhRepo[]>([])
const ghConnected = ref(true) // optimistic; loadGhRepos set false bila gagal/belum connect
const ghLoaded = ref(false)
const repoQuery = ref('')
const gitMode = ref<'pick' | 'create'>('pick')
const newRepoName = ref('')
const newRepoPrivate = ref(true)
const creatingRepo = ref(false)

const filteredGhRepos = computed(() => {
  const q = repoQuery.value.toLowerCase().trim()
  const list = q ? ghRepos.value.filter((r) => r.full_name.toLowerCase().includes(q)) : ghRepos.value
  return list.slice(0, 12)
})

async function loadGhRepos() {
  if (ghLoaded.value) return
  ghLoaded.value = true
  try {
    ghRepos.value = await api.githubRepos()
    ghConnected.value = true
  } catch {
    ghConnected.value = false
  }
}

async function createRepo() {
  if (creatingRepo.value) return
  creatingRepo.value = true
  error.value = ''
  try {
    site.value = await api.siteCreateRepo(props.domain, {
      name: newRepoName.value.trim(),
      private: newRepoPrivate.value,
      branch: cBranch.value.trim() || 'main',
      build_cmd: cBuild.value.trim(),
      output_dir: cOut.value.trim(),
    })
    startDeployPoll()
    loadDeploys()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    creatingRepo.value = false
  }
}

async function connectGit() {
  if (!cRepo.value.trim()) return
  connecting.value = true
  error.value = ''
  try {
    site.value = await api.siteConnectGit(props.domain, {
      repo_url: cRepo.value.trim(), branch: cBranch.value.trim() || 'main',
      build_cmd: cBuild.value.trim(), output_dir: cOut.value.trim(),
    })
    startDeployPoll()   // pantau deploy awal
    loadDeploys()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    connecting.value = false
  }
}
async function disconnectGit() {
  if (!(await confirmDialog({ message: t('siteDetail.disconnectConfirm') }))) return
  try {
    site.value = await api.siteDisconnectGit(props.domain)
    deploys.value = []
    webhook.value = null
    deployLogVisible.value = false
  } catch (e) {
    error.value = (e as Error).message
  }
}
let deployPollTimer: ReturnType<typeof setTimeout> | null = null
let deployPollCount = 0

// domain aliases
const aliases = ref<string[]>([])
const newAlias = ref('')
const aliasBusy = ref(false)
const aliasErr = ref('')
// status koneksi DNS per-domain (utama + alias)
type DomStatus = { domain: string; primary: boolean; points_here: boolean; resolved: string[]; error?: string }
const domStatus = ref<Record<string, DomStatus>>({})
const publicIP = ref('')
const domStatusLoading = ref(false)
async function loadDomainStatus() {
  if (!site.value) return
  domStatusLoading.value = true
  try {
    const res = await api.siteDomainStatus(site.value.domain)
    publicIP.value = res.public_ip
    domStatus.value = Object.fromEntries(res.domains.map((d) => [d.domain, d]))
  } catch { /* ignore */ } finally {
    domStatusLoading.value = false
  }
}

async function addAlias() {
  if (!newAlias.value.trim() || aliasBusy.value || !site.value) return
  aliasBusy.value = true
  aliasErr.value = ''
  try {
    const res = await api.addSiteDomain(site.value.domain, newAlias.value.trim())
    aliases.value = res.domains
    newAlias.value = ''
    loadDomainStatus()
  } catch (e) {
    aliasErr.value = (e as Error).message
  } finally {
    aliasBusy.value = false
  }
}

async function removeAlias(a: string) {
  if (!site.value) return
  try {
    const res = await api.removeSiteDomain(site.value.domain, a)
    aliases.value = res.domains
  } catch (e) {
    aliasErr.value = (e as Error).message
  }
}

// routing lanjutan
const routes = ref<RouteRule[]>([])
const routesBusy = ref(false)
const routesErr = ref('')
const routesMsg = ref('')
// daftar app (untuk mount app-by-name di route)
const apps = ref<App[]>([])

// analitik per-deployment (sparkline 24 jam + error terakhir)
const report = ref<DomainReport | null>(null)
const maxReq = computed(() => Math.max(1, ...(report.value?.series.map((p) => p.requests) ?? [1])))
function barTitle(hour: string, reqs: number): string {
  return `${hour.replace('T', ' ')}:00 · ${reqs} req`
}

function addRule() {
  // aturan baru diletakkan di ATAS route default agar diprioritaskan.
  // Tenant default ke 'app' (mount app sendiri); operator boleh 'proxy' custom.
  routes.value.unshift(
    auth.isSuperadmin
      ? { path_prefix: '/api', type: 'proxy', upstream: '127.0.0.1:7788', cache: false }
      : { path_prefix: '/api', type: 'app', app: '', cache: false },
  )
}
function removeRule(i: number) {
  routes.value.splice(i, 1)
}

// Drag-reorder aturan routing (urutan = prioritas first-match, atas→bawah).
const dragIdx = ref<number | null>(null)
function onRuleDragStart(i: number) { dragIdx.value = i }
function onRuleDragOver(i: number) {
  // Reorder live saat melewati baris lain: pindahkan objek yg diseret ke posisi i.
  const from = dragIdx.value
  if (from === null || from === i) return
  const moved = routes.value.splice(from, 1)[0]
  routes.value.splice(i, 0, moved)
  dragIdx.value = i
}
function onRuleDragEnd() { dragIdx.value = null }
async function saveRoutes() {
  routesBusy.value = true
  routesErr.value = ''
  routesMsg.value = ''
  try {
    await api.putRoutes(props.domain, routes.value)
    routesMsg.value = t('siteDetail.routingSaved')
  } catch (e) {
    routesErr.value = (e as Error).message
  } finally {
    routesBusy.value = false
  }
}

const dragging = ref(false)

// webhook auto-deploy
const origin = window.location.origin
const webhook = ref<{ webhook_path: string; webhook_secret: string } | null>(null)
const webhookBusy = ref(false)
async function loadWebhook() {
  webhookBusy.value = true
  try {
    webhook.value = await api.siteWebhook(props.domain)
  } catch (e) {
    deployErr.value = (e as Error).message
  } finally {
    webhookBusy.value = false
  }
}
async function regenWebhook() {
  if (!(await confirmDialog({ message: t('siteDetail.regenConfirm') }))) return
  webhookBusy.value = true
  try {
    webhook.value = await api.siteWebhookRegen(props.domain)
  } catch (e) {
    deployErr.value = (e as Error).message
  } finally {
    webhookBusy.value = false
  }
}
function copy(text: string) {
  navigator.clipboard?.writeText(text)
}

function setPicked(f: File | null) {
  deployErr.value = ''
  if (f && !/\.zip$/i.test(f.name)) {
    deployErr.value = t('siteDetail.zipOnly')
    picked.value = null
    return
  }
  picked.value = f
}
function onPick(e: Event) {
  setPicked((e.target as HTMLInputElement).files?.[0] ?? null)
}
function onDrop(e: DragEvent) {
  dragging.value = false
  setPicked(e.dataTransfer?.files?.[0] ?? null)
}

function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

// ── editor berkas (tab Berkas, situs non-git) ──────────────────────────────
const editorOpen = ref(false)
const editorPath = ref('')
const editorContent = ref('')
const editorNew = ref(false)
const editorBusy = ref(false)
const fileErr = ref('')
// git-managed = read-only (deploy git reset-hard menimpa editan manual)
const filesReadonly = computed(() => !!site.value?.repo_url)

async function reloadFiles() {
  files.value = await api.siteFiles(props.domain).catch(() => [])
}
async function openEditor(path: string) {
  fileErr.value = ''
  try {
    const res = await api.siteReadFile(props.domain, path)
    editorPath.value = res.path
    editorContent.value = res.content
    editorNew.value = false
    editorOpen.value = true
  } catch (e) {
    fileErr.value = (e as Error).message
  }
}
async function newFile() {
  const name = await promptDialog({ message: t('siteDetail.newFilePrompt') })
  if (!name || !name.trim()) return
  editorPath.value = name.trim()
  editorContent.value = ''
  editorNew.value = true
  editorOpen.value = true
  fileErr.value = ''
}
async function newFolder() {
  const name = await promptDialog({ message: t('siteDetail.newFolderPrompt') })
  if (!name || !name.trim()) return
  fileErr.value = ''
  try {
    await api.siteMkdir(props.domain, name.trim())
    await reloadFiles()
  } catch (e) {
    fileErr.value = (e as Error).message
  }
}
async function saveFile() {
  if (!editorPath.value.trim()) return
  editorBusy.value = true
  fileErr.value = ''
  try {
    await api.siteWriteFile(props.domain, editorPath.value.trim(), editorContent.value)
    editorOpen.value = false
    await reloadFiles()
  } catch (e) {
    fileErr.value = (e as Error).message
  } finally {
    editorBusy.value = false
  }
}
async function deleteFile(path: string) {
  if (!(await confirmDialog({ message: t('siteDetail.deleteFileConfirm', { path }) }))) return
  fileErr.value = ''
  try {
    await api.siteDeleteFile(props.domain, path)
    await reloadFiles()
  } catch (e) {
    fileErr.value = (e as Error).message
  }
}
function isEditable(path: string): boolean {
  // heuristik ringan: sembunyikan tombol edit utk ekstensi biner umum
  return !/\.(png|jpe?g|gif|webp|ico|woff2?|ttf|otf|eot|zip|gz|pdf|mp4|webm|mp3|wasm)$/i.test(path)
}

async function deploy() {
  if (!picked.value) return
  deploying.value = true
  deployErr.value = ''
  try {
    await api.deploy(props.domain, picked.value)
    picked.value = null
    notice.value = t('siteDetail.deployed')
    files.value = await api.siteFiles(props.domain).catch(() => [])
  } catch (e) {
    deployErr.value = (e as Error).message
  } finally {
    deploying.value = false
  }
}

function startDeployPoll() {
  deployPollCount = 0
  deployStatus.value = 'building'
  deployLogVisible.value = true
  if (deployPollTimer) clearTimeout(deployPollTimer)
  async function poll() {
    deployPollCount++
    try {
      const res = await api.siteDeployLog(props.domain)
      deployLog.value = res.lines
      deployStatus.value = res.status
      if (res.status === 'success' || res.status === 'failed' || deployPollCount >= 15) {
        loadDeploys() // refresh riwayat setelah deploy tuntas
        return
      }
    } catch { /* ignore poll errors */ }
    deployPollTimer = setTimeout(poll, 2000)
  }
  poll()
}

async function loadDeploys() {
  try {
    const res = await api.siteDeploys(props.domain)
    deploys.value = res.deploys
  } catch { /* ignore */ }
}

// Format ISO/SQL timestamp jadi label lokal ringkas untuk daftar deploy.
function deployTime(d: Deploy): string {
  const raw = d.finished_at || d.started_at
  if (!raw) return '—'
  const iso = raw.includes('T') ? raw : raw.replace(' ', 'T') + 'Z'
  const t = new Date(iso)
  if (isNaN(t.getTime())) return raw
  return t.toLocaleString('id-ID', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
}

async function deployGit() {
  deployingGit.value = true
  notice.value = ''
  error.value = ''
  deployLog.value = []
  deployStatus.value = ''
  deployLogVisible.value = false
  try {
    await api.siteDeployGit(props.domain)
    startDeployPoll()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    deployingGit.value = false
  }
}

async function loadDeployLog() {
  deployLogVisible.value = true
  try {
    const res = await api.siteDeployLog(props.domain)
    deployLog.value = res.lines
    deployStatus.value = res.status
  } catch { /* ignore */ }
}

const certState = () => {
  if (!cert.value || cert.value.status === 'none') return 'off'
  if (cert.value.status === 'valid') return 'on'
  return 'warn'
}
const certLabel = () => {
  switch (cert.value?.status) {
    case 'valid': return t('siteDetail.certActive')
    case 'expiring': return t('siteDetail.certExpiring')
    case 'expired': return t('siteDetail.certExpired')
    default: return t('siteDetail.certPending')
  }
}
const certDays = () => {
  if (!cert.value?.not_after || cert.value.status === 'none') return null
  return Math.round((new Date(cert.value.not_after).getTime() - Date.now()) / 86_400_000)
}
const certDate = () => {
  if (!cert.value?.not_after || cert.value.status === 'none') return '—'
  return new Date(cert.value.not_after).toLocaleDateString('id-ID', { day: 'numeric', month: 'short', year: 'numeric' })
}

async function load() {
  loading.value = true
  try {
    site.value = await api.getSite(props.domain)
    // cert & log tak fatal bila gagal — tampilkan seadanya
    cert.value = await api.siteCert(props.domain).catch(() => null)
    logs.value = await api.siteLogs(props.domain, 80).catch(() => [])
    if (site.value.type === 'static') {
      files.value = await api.siteFiles(props.domain).catch(() => [])
    }
    routes.value = await api.getRoutes(props.domain).catch(() => [])
    apps.value = await api.listApps().catch(() => [])
    report.value = await api.siteAnalytics(props.domain).catch(() => null)
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function toggle() {
  if (!site.value) return
  const next = site.value.status === 'active' ? 'disabled' : 'active'
  try {
    site.value = await api.patchSite(props.domain, { status: next })
    notice.value = next === 'active' ? t('siteDetail.enabled') : t('siteDetail.disabled')
  } catch (e) {
    error.value = (e as Error).message
  }
}

async function remove() {
  if (!(await confirmDialog({ message: t('siteDetail.deleteConfirm', { domain: props.domain }) }))) return
  try {
    await api.deleteSite(props.domain)
    router.push({ name: 'sites' })
  } catch (e) {
    error.value = (e as Error).message
  }
}

onMounted(load)
onUnmounted(() => { if (deployPollTimer) clearTimeout(deployPollTimer) })

watch(activeTab, async (tab) => {
  if (tab === 'domains' && site.value) {
    aliasErr.value = ''
    try {
      const res = await api.siteDomains(site.value.domain)
      aliases.value = res.domains
    } catch (e) {
      aliasErr.value = (e as Error).message
    }
    loadDomainStatus()
  }
  if (tab === 'git' && site.value) {
    loadDeploys()
    loadDeployLog() // status + log terakhir tanpa memicu deploy baru
    if (!site.value.repo_url) loadGhRepos() // picker repo utk site belum terhubung
  }
})
</script>

<template>
  <div class="crumb">
    <RouterLink :to="{ name: 'sites' }">{{ t('sites.title') }}</RouterLink>
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M9 6l6 6-6 6"/></svg>
    {{ domain }}
  </div>

  <div v-if="loading" class="spin">{{ t('common.loading') }}</div>
  <div v-else-if="error && !site" class="alert err">{{ error }}</div>

  <template v-else-if="site">
    <div class="page-head">
      <div>
        <div class="h1" style="display: flex; align-items: center; gap: 12px">
          <Pulse :state="site.status === 'active' ? 'on' : 'off'" />
          <span class="mono">{{ site.domain }}</span>
        </div>
        <div class="sub">
          <span class="tag" :class="site.type">{{ site.type }}</span>
          {{ site.type === 'proxy' ? '→ ' + site.proxy_target : site.root_path }}
        </div>
      </div>
      <a class="btn" :href="'https://' + site.domain" target="_blank" rel="noopener">{{ t('siteDetail.openSite') }}</a>
    </div>

    <div v-if="notice" class="alert ok">{{ notice }}</div>
    <div v-if="error" class="alert err">{{ error }}</div>

    <div class="tabs">
      <button :class="{ on: activeTab === 'stats' }" @click="activeTab = 'stats'">{{ t('siteDetail.tabStats') }}</button>
      <button :class="{ on: activeTab === 'routing' }" @click="activeTab = 'routing'">{{ t('siteDetail.tabRouting') }}</button>
      <button :class="{ on: activeTab === 'domains' }" @click="activeTab = 'domains'">{{ t('siteDetail.tabDomain') }}</button>
      <button v-if="site && site.type === 'static'" :class="{ on: activeTab === 'files' }" @click="activeTab = 'files'">{{ t('siteDetail.tabFiles') }}</button>
      <button v-if="site && site.type === 'static'" :class="{ on: activeTab === 'git' }" @click="activeTab = 'git'">{{ t('siteDetail.tabGit') }}</button>
      <button :class="{ on: activeTab === 'logs' }" @click="activeTab = 'logs'">{{ t('siteDetail.tabLogs') }}</button>
      <button :class="{ on: activeTab === 'settings' }" @click="activeTab = 'settings'">{{ t('siteDetail.tabSettings') }}</button>
    </div>

    <div v-if="activeTab === 'stats'" class="grid-2" style="margin-bottom: 20px">
      <div class="card" style="padding: 18px 20px">
        <div class="sec"><h3>{{ t('siteDetail.sslCert') }}</h3></div>
        <div style="display: flex; align-items: center; gap: 9px; margin-top: 2px; margin-bottom: 12px">
          <Pulse :state="certState()" />
          <span style="font-weight: 700; color: var(--ink)">{{ certLabel() }}</span>
          <span v-if="certDays() !== null" class="tag" :class="cert?.status === 'valid' ? 'static' : 'proxy'" style="margin-left: auto">
            {{ t('siteDetail.daysLeft', { days: certDays() }) }}
          </span>
        </div>
        <div class="clines">
          <div><span>{{ t('siteDetail.issuer') }}</span><b>{{ cert && cert.status !== 'none' ? cert.issuer : '—' }}</b></div>
          <div><span>{{ t('siteDetail.validUntil') }}</span><b>{{ certDate() }}</b></div>
          <div><span>{{ t('siteDetail.https') }}</span><b>{{ t('siteDetail.httpsValue') }}</b></div>
        </div>
      </div>
      <div class="card" style="padding: 18px 20px">
        <div class="sec"><h3>{{ t('siteDetail.status') }}</h3></div>
        <div class="statline">
          <div><div class="n" style="text-transform: capitalize">{{ site.status }}</div><div class="l">{{ t('siteDetail.condition') }}</div></div>
          <div><div class="n">{{ site.created_at?.slice(0, 10) || '—' }}</div><div class="l">{{ t('siteDetail.created') }}</div></div>
        </div>
      </div>
    </div>


    <div v-if="activeTab === 'files' && site.type === 'static'" class="card" style="padding: 18px 20px; margin-bottom: 20px">
      <div class="sec"><h3>{{ t('siteDetail.deploy') }}</h3></div>
      <p class="sub" style="margin-bottom: 12px">
        <i18n-t keypath="siteDetail.deployHint" tag="span">
          <template #zip><b>.zip</b></template>
          <template #dist><code class="mono">dist/</code></template>
        </i18n-t>
      </p>
      <div v-if="deployErr" class="alert err">{{ deployErr }}</div>
      <label
        class="dropzone"
        :class="{ drag: dragging }"
        @dragover.prevent="dragging = true"
        @dragenter.prevent="dragging = true"
        @dragleave.prevent="dragging = false"
        @drop.prevent="onDrop"
      >
        <input type="file" accept=".zip,application/zip" class="dz-input" @change="onPick" />
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="dz-ic"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
        <div v-if="picked" class="dz-file mono">{{ picked.name }}</div>
        <div v-else class="dz-text">{{ t('siteDetail.dropHint') }}</div>
      </label>
      <div style="margin-top: 12px">
        <button class="btn pri" :disabled="!picked || deploying" @click="deploy">
          {{ deploying ? t('siteDetail.uploading') : t('siteDetail.deployZip') }}
        </button>
      </div>

      <!-- Manajemen berkas -->
      <div class="sec" style="margin-top: 22px; display: flex; align-items: center; justify-content: space-between">
        <h3>{{ t('siteDetail.filesTitle') }}</h3>
        <div v-if="!filesReadonly" style="display: flex; gap: 8px">
          <button class="btn sm" @click="newFolder">{{ t('siteDetail.newFolder') }}</button>
          <button class="btn sm pri" @click="newFile">{{ t('siteDetail.newFile') }}</button>
        </div>
      </div>
      <div v-if="filesReadonly" class="alert" style="background: rgba(217,164,65,.12); border-color: rgba(217,164,65,.3); color: #8a6d1f">
        {{ t('siteDetail.filesGitReadonly') }}
      </div>
      <div v-if="fileErr" class="alert err">{{ fileErr }}</div>

      <div v-if="files.length" style="margin-top: 10px">
        <div class="sub" style="margin-bottom: 6px">{{ t('siteDetail.files', { count: files.length }) }}</div>
        <table class="tbl ftbl">
          <tbody>
            <tr v-for="f in files.slice(0, 100)" :key="f.path">
              <td class="mono fpath" :class="{ ed: !filesReadonly && isEditable(f.path) }"
                  @click="!filesReadonly && isEditable(f.path) && openEditor(f.path)">{{ f.path }}</td>
              <td style="text-align: right; color: var(--muted); white-space: nowrap">{{ fmtSize(f.size) }}</td>
              <td v-if="!filesReadonly" style="text-align: right; white-space: nowrap">
                <button v-if="isEditable(f.path)" class="btn sm" @click="openEditor(f.path)">{{ t('siteDetail.edit') }}</button>
                <button class="btn sm danger" style="margin-left: 6px" @click="deleteFile(f.path)">✕</button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="files.length > 100" class="sub" style="margin-top: 6px">{{ t('siteDetail.moreFiles', { count: files.length - 100 }) }}</div>
      </div>
      <div v-else class="sub" style="margin-top: 10px; color: var(--muted)">{{ t('siteDetail.noFiles') }}</div>

      <!-- Editor berkas (modal) -->
      <div v-if="editorOpen" class="ed-overlay" @click.self="editorOpen = false">
        <div class="ed-modal">
          <div class="ed-head">
            <input v-if="editorNew" class="input mono" v-model="editorPath" :placeholder="t('siteDetail.filePathPlaceholder')" />
            <b v-else class="mono">{{ editorPath }}</b>
            <button class="ed-x" @click="editorOpen = false" :title="t('common.cancel')">✕</button>
          </div>
          <textarea v-model="editorContent" class="ed-area mono" spellcheck="false"></textarea>
          <div class="ed-foot">
            <button class="btn" @click="editorOpen = false">{{ t('common.cancel') }}</button>
            <button class="btn pri" :disabled="editorBusy || !editorPath.trim()" @click="saveFile">
              {{ editorBusy ? t('common.saving') : t('siteDetail.saveFile') }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <div v-if="activeTab === 'routing'" class="card" style="padding: 18px 20px; margin-bottom: 20px">
      <div class="sec"><h3>{{ t('siteDetail.advancedRouting') }}</h3></div>
      <p class="sub" style="margin-bottom: 12px">
        <i18n-t keypath="siteDetail.routingHint" tag="span">
          <template #topToBottom><b>{{ t('siteDetail.topToBottom') }}</b></template>
          <template #api><code class="mono">/api</code></template>
          <template #root><code class="mono">/</code></template>
        </i18n-t>
      </p>
      <div v-if="routesErr" class="alert err">{{ routesErr }}</div>
      <div v-if="routesMsg" class="alert ok">{{ routesMsg }}</div>

      <div class="rrows">
        <div v-for="(rt, i) in routes" :key="i" class="rrow" :class="{ dragging: dragIdx === i }"
             @dragover.prevent="onRuleDragOver(i)" @drop.prevent="onRuleDragEnd">
          <span class="rgrip" draggable="true" :title="t('siteDetail.dragReorder')"
                @dragstart="onRuleDragStart(i)" @dragend="onRuleDragEnd">⠿</span>
          <div class="input rp"><span class="pre">path</span><input v-model="rt.path_prefix" class="mono" placeholder="/api" /></div>
          <select v-model="rt.type" class="rsel">
            <option value="static">{{ t('siteDetail.routeStatic') }}</option>
            <option v-if="auth.isSuperadmin" value="proxy">{{ t('siteDetail.routeProxy') }}</option>
            <option value="app">{{ t('siteDetail.routeApp') }}</option>
          </select>
          <div v-if="rt.type === 'proxy'" class="input ru"><span class="pre">http://</span><input v-model="rt.upstream" class="mono" placeholder="127.0.0.1:7788" /></div>
          <select v-else-if="rt.type === 'app'" v-model="rt.app" class="rsel ru">
            <option value="" disabled>{{ t('siteDetail.pickApp') }}</option>
            <option v-for="a in apps" :key="a.domain" :value="a.domain">{{ a.domain }}</option>
          </select>
          <div v-else class="rstatic">{{ t('siteDetail.managedFolder') }}</div>
          <label v-if="rt.type === 'static'" class="rcache" :title="t('siteDetail.spaTitle')"><input type="checkbox" v-model="rt.spa" /> SPA</label>
          <label class="rcache" :title="t('siteDetail.cacheTitle')"><input type="checkbox" v-model="rt.cache" /> {{ t('siteDetail.cache') }}</label>
          <button class="btn danger rdel" @click="removeRule(i)" :title="t('siteDetail.deleteRule')">✕</button>
        </div>
      </div>

      <div style="display: flex; gap: 10px; margin-top: 12px; flex-wrap: wrap">
        <button class="btn" @click="addRule">{{ t('siteDetail.addRule') }}</button>
        <button class="btn pri" :disabled="routesBusy" @click="saveRoutes">{{ routesBusy ? t('common.saving') : t('siteDetail.saveRouting') }}</button>
      </div>
    </div>

    <div v-if="activeTab === 'domains'" class="card" style="padding: 18px 20px; margin-bottom: 20px">
      <div class="sec"><h3>{{ t('siteDetail.tabDomain') }}</h3></div>

      <div class="drow">
        <span class="mono">{{ site.domain }}</span>
        <span class="tag static" style="margin-left: 8px">{{ t('siteDomains.primary') }}</span>
        <span v-if="domStatus[site.domain]" class="dstat" :class="domStatus[site.domain].points_here ? 'ok' : 'wait'"
              :title="domStatus[site.domain].resolved.length ? domStatus[site.domain].resolved.join(', ') : (domStatus[site.domain].error || '')">
          <span class="dstat-dot"></span>{{ domStatus[site.domain].points_here ? t('siteDomains.connected') : t('siteDomains.waitingDns') }}
        </span>
      </div>

      <div v-if="aliases.length">
        <div v-for="a in aliases" :key="a" class="drow">
          <span class="mono">{{ a }}</span>
          <span v-if="domStatus[a]" class="dstat" :class="domStatus[a].points_here ? 'ok' : 'wait'"
                :title="domStatus[a].resolved.length ? domStatus[a].resolved.join(', ') : (domStatus[a].error || '')">
            <span class="dstat-dot"></span>{{ domStatus[a].points_here ? t('siteDomains.connected') : t('siteDomains.waitingDns') }}
          </span>
          <button class="btn danger" style="padding: 4px 10px; margin-left: 8px" @click="removeAlias(a)">{{ t('siteDomains.remove') }}</button>
        </div>
      </div>
      <div v-else class="sub" style="padding: 6px 0; color: var(--muted)">{{ t('siteDomains.empty') }}</div>

      <!-- Petunjuk koneksi: arahkan A record ke IP server -->
      <div v-if="publicIP" class="dns-howto">
        <b>{{ t('siteDomains.howtoTitle') }}</b>
        <p>{{ t('siteDomains.howtoHint') }}</p>
        <div class="dns-rec">
          <span>A</span><span class="mono">@ / subdomain</span><span class="mono">{{ publicIP.split(',')[0] }}</span>
          <button class="btn sm" @click="copy(publicIP.split(',')[0])">{{ t('team.copyLink') }}</button>
        </div>
      </div>

      <div style="display: flex; gap: 8px; align-items: center; flex-wrap: wrap; margin-top: 14px">
        <input
          v-model="newAlias"
          class="input"
          style="flex: 1; min-width: 180px; padding: 8px 11px"
          :placeholder="t('siteDomains.placeholder')"
          @keyup.enter="addAlias"
        />
        <button class="btn pri" :disabled="aliasBusy" @click="addAlias">{{ t('siteDomains.add') }}</button>
      </div>
      <div v-if="aliasErr" class="alert err" style="margin-top: 8px">{{ aliasErr }}</div>
      <div class="sub" style="margin-top: 10px; color: var(--muted)">{{ t('siteDomains.httpsHint') }}</div>
    </div>

    <div v-if="activeTab === 'stats' && report" class="card" style="padding: 18px 20px; margin-bottom: 20px">
      <div class="sec"><h3>{{ t('analytics.title') }}</h3></div>
      <div class="an-nums">
        <div><b>{{ report.requests.toLocaleString() }}</b><span>{{ t('analytics.requests') }}</span></div>
        <div><b :class="{ bad: report.errors > 0 }">{{ report.errors.toLocaleString() }}</b><span>{{ t('analytics.errors') }}</span></div>
      </div>
      <div class="spark-bars" role="img" :aria-label="t('analytics.title')">
        <i v-for="(p, i) in report.series" :key="i" :style="{ height: Math.round((p.requests / maxReq) * 100) + '%' }" :title="barTitle(p.hour, p.requests)"></i>
      </div>
      <div v-if="report.recent_errors.length" class="errs" style="margin-top: 14px">
        <div v-for="(e, i) in report.recent_errors" :key="i" class="e">
          <span class="dot" :class="e.status >= 500 ? 'e5' : 'e4'"></span>
          <span class="code" :class="e.status >= 500 ? 'c5' : 'c4'">{{ e.status }}</span>
          <span class="path">{{ e.path }}</span>
        </div>
      </div>
    </div>

    <div v-if="activeTab === 'logs'" class="card" style="padding: 18px 20px; margin-bottom: 20px">
      <div class="sec"><h3>{{ t('siteDetail.accessLog') }} <span class="sub" style="text-transform: none; font-weight: 400; margin: 0">{{ t('siteDetail.accessLogLines') }}</span></h3></div>
      <div v-if="logs.length" class="logs">{{ logs.join('\n') }}</div>
      <div v-else class="sub" style="padding: 8px 0">{{ t('siteDetail.noTraffic') }}</div>
    </div>

    <!-- Git tab (ala Vercel): Connect (non-git) atau Source + Deployments + Auto-deploy -->
    <div v-if="activeTab === 'git'" class="card" style="padding: 18px 20px; margin-bottom: 20px">

    <!-- Belum terhubung: pilih repo GitHub / bikin repo baru / URL manual -->
    <template v-if="!site.repo_url">
      <div class="sec"><h3>{{ t('siteDetail.connectRepo') }}</h3></div>
      <p class="sub" style="margin-bottom: 14px">{{ t('siteDetail.connectHint') }}</p>

      <!-- Belum connect GitHub → ajak hubungkan (URL manual tetap tersedia di bawah) -->
      <div v-if="!ghConnected" class="gh-connect">
        <RouterLink class="btn pri" :to="{ name: 'settings' }">{{ t('siteDetail.connectGithubBtn') }}</RouterLink>
        <span class="sub" style="font-size: 12px">{{ t('siteDetail.connectGithubHint') }}</span>
      </div>

      <!-- Sudah connect GitHub → mode Pilih / Bikin baru -->
      <template v-else>
        <div class="cg-modes">
          <button :class="{ on: gitMode === 'pick' }" @click="gitMode = 'pick'">{{ t('siteDetail.gitModePick') }}</button>
          <button :class="{ on: gitMode === 'create' }" @click="gitMode = 'create'">{{ t('siteDetail.gitModeCreate') }}</button>
        </div>

        <!-- Pilih repo existing -->
        <div v-if="gitMode === 'pick'" class="cg-form">
          <input class="input" v-model="repoQuery" :placeholder="t('siteDetail.searchRepo')" />
          <div class="repo-list">
            <button v-for="r in filteredGhRepos" :key="r.full_name" class="repo-item" :class="{ on: cRepo === r.clone_url }" @click="cRepo = r.clone_url">
              <span class="mono">{{ r.full_name }}</span>
              <span v-if="r.private" class="repo-lock">{{ t('siteDetail.repoPrivate') }}</span>
            </button>
            <div v-if="!filteredGhRepos.length" class="sub" style="font-size: 12px; padding: 6px 0">{{ t('siteDetail.noRepos') }}</div>
          </div>
        </div>

        <!-- Bikin repo baru -->
        <div v-else class="cg-form">
          <label>{{ t('siteDetail.repoName') }}</label>
          <input class="input" v-model="newRepoName" :placeholder="props.domain" />
          <label class="cg-check"><input type="checkbox" v-model="newRepoPrivate" /> {{ t('siteDetail.makePrivate') }}</label>
        </div>
      </template>

      <!-- Detail deploy (branch/build/output) + tombol aksi — tampil bila ada pilihan -->
      <div v-if="ghConnected && (gitMode === 'create' || cRepo)" class="cg-form" style="margin-top: 6px">
        <div class="cg-grid">
          <div>
            <label>{{ t('wizard.branch') }}</label>
            <input class="input" v-model="cBranch" placeholder="main" />
          </div>
          <div>
            <label>{{ t('wizard.outputDir') }}</label>
            <input class="input" v-model="cOut" :placeholder="cBuild ? 'dist' : '.'" />
          </div>
        </div>
        <label>{{ t('wizard.buildCmd') }} <span class="cg-opt">({{ t('siteDetail.optional') }})</span></label>
        <input class="input" v-model="cBuild" placeholder="npm run build" />
        <p class="sub" style="font-size: 12px; margin: 2px 0 12px">{{ t('siteDetail.connectBuildHint') }}</p>
        <button v-if="gitMode === 'create'" class="btn pri" :disabled="creatingRepo" @click="createRepo">
          {{ creatingRepo ? t('siteDetail.creatingRepo') : t('siteDetail.createRepoBtn') }}
        </button>
        <button v-else class="btn pri" :disabled="connecting || !cRepo.trim()" @click="connectGit">
          {{ connecting ? t('siteDetail.connecting') : t('siteDetail.connect') }}
        </button>
      </div>

      <!-- URL manual (fallback, selalu tersedia) -->
      <details class="cg-manual">
        <summary>{{ t('siteDetail.manualUrl') }}</summary>
        <div class="cg-form" style="margin-top: 8px">
          <input class="input" v-model="cRepo" placeholder="https://github.com/user/repo.git" />
          <button class="btn" :disabled="connecting || !cRepo.trim()" @click="connectGit">
            {{ connecting ? t('siteDetail.connecting') : t('siteDetail.connect') }}
          </button>
        </div>
      </details>

      <div v-if="deployLogVisible" style="margin-top: 14px">
        <div v-if="deployLog.length" class="logs">{{ deployLog.join('\n') }}</div>
      </div>
    </template>

    <!-- Sudah terhubung: Source + Deployments + Auto-deploy -->
    <template v-else>
      <div class="sec" style="display: flex; align-items: center; justify-content: space-between">
        <h3>{{ t('siteDetail.gitSource') }}</h3>
        <button class="btn sm danger" @click="disconnectGit">{{ t('siteDetail.disconnect') }}</button>
      </div>
      <div class="clines" style="margin-bottom: 6px">
        <div><span>{{ t('siteDetail.gitRepo') }}</span><b class="mono">{{ site.repo_url }}</b></div>
        <div v-if="site.branch"><span>{{ t('wizard.branch') }}</span><b class="mono">{{ site.branch }}</b></div>
      </div>

      <div class="sec" style="margin-top: 18px; display: flex; align-items: center; justify-content: space-between">
        <h3>{{ t('siteDetail.deployments') }}</h3>
        <button class="btn pri sm" :disabled="deployingGit" @click="deployGit">
          {{ deployingGit ? t('siteDetail.deploying') : t('siteDetail.deployNow') }}
        </button>
      </div>

      <!-- Log deploy langsung (saat tombol Deploy ditekan / dilihat) -->
      <div v-if="deployLogVisible" style="margin-bottom: 12px">
        <div v-if="deployLog.length" class="logs">{{ deployLog.join('\n') }}</div>
        <div v-else class="sub" style="font-size: 12.5px; padding: 4px 0; color: var(--muted)">{{ t('siteDetail.deploying') }}…</div>
      </div>

      <!-- Riwayat deploy -->
      <div v-if="deploys.length" class="dep-list">
        <div v-for="d in deploys" :key="d.id" class="dep-row">
          <span class="dep-dot" :class="d.status"></span>
          <div class="dep-main">
            <div class="dep-top">
              <b class="mono" v-if="d.commit">{{ d.commit }}</b>
              <b class="mono muted-text" v-else>—</b>
              <span class="dep-trig">{{ t('siteDetail.trigger_' + d.trigger) }}</span>
            </div>
            <div class="dep-msg" v-if="d.message">{{ d.message }}</div>
          </div>
          <div class="dep-meta">
            <span class="dep-status" :class="d.status">
              {{ d.status === 'success' ? t('siteDetail.deploySuccess') : d.status === 'failed' ? t('siteDetail.deployFailed') : t('siteDetail.deploying') }}
            </span>
            <span class="dep-time">{{ deployTime(d) }}</span>
          </div>
        </div>
      </div>
      <p v-else class="sub" style="font-size: 13px; color: var(--muted)">{{ t('siteDetail.noDeploys') }}</p>

      <!-- Auto-deploy (webhook) -->
      <div class="sec" style="margin-top: 20px"><h3>{{ t('siteDetail.autoDeploy') }}</h3></div>
      <p class="sub" style="margin-bottom: 10px">{{ t('siteDetail.autoDeployHint') }}</p>
      <button v-if="!webhook" class="btn" :disabled="webhookBusy" @click="loadWebhook">{{ t('siteDetail.showWebhook') }}</button>
      <div v-else class="wh-box">
        <label>{{ t('siteDetail.webhookUrl') }}</label>
        <div class="wh-row">
          <code class="mono">{{ origin + webhook.webhook_path }}</code>
          <button class="btn sm" @click="copy(origin + webhook.webhook_path)">{{ t('team.copyLink') }}</button>
        </div>
        <label>{{ t('siteDetail.webhookSecret') }}</label>
        <div class="wh-row">
          <code class="mono">{{ webhook.webhook_secret }}</code>
          <button class="btn sm" @click="copy(webhook.webhook_secret)">{{ t('team.copyLink') }}</button>
          <button class="btn sm" :disabled="webhookBusy" @click="regenWebhook">{{ t('siteDetail.regenerate') }}</button>
        </div>
        <p class="sub" style="font-size: 12px; margin-top: 6px">{{ t('siteDetail.webhookSetup') }}</p>
      </div>
    </template>
    </div>

    <div v-if="activeTab === 'settings'" class="card" style="padding: 18px 20px">
      <div class="sec"><h3>{{ t('siteDetail.actions') }}</h3></div>
      <div style="display: flex; gap: 10px; flex-wrap: wrap">
        <button class="btn" @click="toggle">{{ site.status === 'active' ? t('siteDetail.disable') : t('siteDetail.enable') }}</button>
        <RouterLink class="btn" :to="{ name: 'site-edit', params: { domain: site.domain } }">{{ t('siteDetail.editConfig') }}</RouterLink>
        <button class="btn danger" @click="remove">{{ t('siteDetail.deleteSite') }}</button>
      </div>
    </div>
  </template>
</template>

<style scoped>
/* inline status figures inside the status card (mockup: horizontal stats) */
.statline { display: flex; gap: 26px; margin-top: 2px; flex-wrap: wrap; }
.statline .n { font-family: var(--font-display); font-weight: 700; font-size: 22px; letter-spacing: -.5px; line-height: 1; }
.statline .l { color: var(--muted); font-size: 12px; margin-top: 6px; }

/* analytics summary numbers */
.an-nums { display: flex; gap: 28px; margin-bottom: 14px; }
.an-nums > div { display: flex; flex-direction: column; gap: 4px; }
.an-nums b { font-family: var(--font-display); font-weight: 700; font-size: 24px; letter-spacing: -.5px; line-height: 1; }
.an-nums b.bad { color: var(--danger); }
.an-nums span { color: var(--muted); font-size: 12px; }

/* deploy status text helpers */
.ok-text { color: var(--ok); }
.err-text { color: var(--danger); }
.muted-text { color: var(--muted); }

/* manajemen berkas (tab Berkas) */
.ftbl td { padding: 7px 8px; }
.fpath.ed { cursor: pointer; }
.fpath.ed:hover { color: var(--brand, #6b4eff); text-decoration: underline; }
.ed-overlay { position: fixed; inset: 0; background: rgba(20,20,35,.45); display: flex; align-items: center; justify-content: center; z-index: 50; padding: 20px; }
.ed-modal { background: var(--card); border-radius: 14px; width: min(820px, 100%); max-height: 86vh; display: flex; flex-direction: column; box-shadow: 0 20px 60px rgba(0,0,0,.3); overflow: hidden; }
.ed-head { display: flex; align-items: center; gap: 10px; padding: 14px 16px; border-bottom: 1px solid var(--line); }
.ed-head b, .ed-head .input { flex: 1; font-size: 13.5px; }
.ed-x { background: none; border: none; cursor: pointer; color: var(--muted); font-size: 16px; padding: 2px 6px; }
.ed-area { flex: 1; min-height: 46vh; resize: vertical; border: none; padding: 14px 16px; font-size: 13px; line-height: 1.55; background: var(--bg-soft, #fafafb); color: var(--slate); outline: none; }
.ed-foot { display: flex; justify-content: flex-end; gap: 10px; padding: 12px 16px; border-top: 1px solid var(--line); }

/* status koneksi domain (tab Domain) */
.dstat { display: inline-flex; align-items: center; gap: 5px; margin-left: 8px; font-size: 12px; font-weight: 600; padding: 2px 9px; border-radius: 20px; }
.dstat-dot { width: 7px; height: 7px; border-radius: 50%; }
.dstat.ok { color: var(--ok); background: rgba(60,170,110,.12); }
.dstat.ok .dstat-dot { background: var(--ok); }
.dstat.wait { color: #b7791f; background: rgba(217,164,65,.14); }
.dstat.wait .dstat-dot { background: #d9a441; }
.dns-howto { margin-top: 16px; padding: 14px 16px; border: 1px dashed var(--line); border-radius: 12px; }
.dns-howto b { font-size: 13px; }
.dns-howto p { color: var(--muted); font-size: 12.5px; margin: 5px 0 10px; }
.dns-rec { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; font-size: 13px; }
.dns-rec > span:first-child { font-weight: 700; background: var(--bg-soft, rgba(120,120,140,.1)); padding: 2px 9px; border-radius: 6px; }

/* connect-git form (Git tab, situs belum terhubung) */
.cg-form { max-width: 560px; }
.cg-form label { display: block; font-size: 12.5px; font-weight: 600; color: var(--slate); margin: 12px 0 5px; }
.cg-form .input { width: 100%; }
.cg-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.cg-opt { font-weight: 400; color: var(--muted); }

.gh-connect { display: flex; align-items: center; gap: 12px; margin-bottom: 14px; }
.cg-modes { display: inline-flex; gap: 4px; padding: 3px; background: var(--line-2); border-radius: var(--r-btn); margin-bottom: 14px; }
.cg-modes button { border: 0; background: transparent; padding: 6px 14px; border-radius: calc(var(--r-btn) - 2px); font: inherit; font-size: 13px; font-weight: 600; color: var(--muted); cursor: pointer; }
.cg-modes button.on { background: var(--card); color: var(--brand); box-shadow: 0 1px 3px rgba(0,0,0,.08); }
.repo-list { display: flex; flex-direction: column; gap: 4px; margin-top: 10px; max-height: 260px; overflow-y: auto; }
.repo-item { display: flex; align-items: center; justify-content: space-between; gap: 10px; padding: 9px 12px; border: 1px solid var(--line); border-radius: var(--r-input); background: var(--card); cursor: pointer; font-size: 13px; text-align: left; }
.repo-item:hover { border-color: var(--brand-l); }
.repo-item.on { border-color: var(--brand); background: var(--line-2); }
.repo-lock { font-size: 11px; color: var(--muted); }
.cg-check { display: flex !important; align-items: center; gap: 8px; margin-top: 12px !important; font-weight: 500 !important; }
.cg-check input { width: auto; }
.cg-manual { margin-top: 14px; }
.cg-manual summary { cursor: pointer; font-size: 12.5px; color: var(--muted); }

/* deploy history list (Git tab) */
.dep-list { display: flex; flex-direction: column; border: 1px solid var(--line); border-radius: 12px; overflow: hidden; }
.dep-row { display: flex; align-items: center; gap: 12px; padding: 11px 14px; border-top: 1px solid var(--line); }
.dep-row:first-child { border-top: none; }
.dep-dot { width: 8px; height: 8px; border-radius: 50%; flex: none; background: var(--muted); }
.dep-dot.success { background: var(--ok); }
.dep-dot.failed { background: var(--danger); }
.dep-dot.building { background: #d9a441; }
.dep-main { flex: 1; min-width: 0; }
.dep-top { display: flex; align-items: center; gap: 10px; }
.dep-top b { font-size: 13px; }
.dep-trig { font-size: 11px; text-transform: uppercase; letter-spacing: .4px; color: var(--muted); background: var(--bg-soft, rgba(120,120,140,.1)); padding: 1px 7px; border-radius: 6px; }
.dep-msg { font-size: 12.5px; color: var(--muted); margin-top: 3px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.dep-meta { display: flex; flex-direction: column; align-items: flex-end; gap: 3px; flex: none; }
.dep-status { font-size: 12px; font-weight: 600; color: var(--muted); }
.dep-status.success { color: var(--ok); }
.dep-status.failed { color: var(--danger); }
.dep-status.building { color: #d9a441; }
.dep-time { font-size: 11.5px; color: var(--muted); }

/* routing rules editor (.rrows not in global stylesheet) */
.rrows { display: flex; flex-direction: column; gap: 8px; }
.rrow { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; border-radius: 10px; }
.rrow.dragging { opacity: .55; background: var(--bg-soft, rgba(120,120,140,.08)); }
.rgrip { cursor: grab; color: var(--muted); font-size: 16px; line-height: 1; padding: 0 2px; user-select: none; letter-spacing: -2px; }
.rgrip:active { cursor: grabbing; }
.rrow .input { padding: 8px 11px; }
.rrow .rp { width: 150px; }
.rrow .ru { flex: 1; min-width: 180px; }
.rsel { font: inherit; font-size: 13.5px; padding: 9px 11px; border-radius: 10px; border: 1px solid var(--line); background: var(--card); color: var(--slate); }
.rstatic { flex: 1; min-width: 180px; color: var(--muted); font-size: 13px; }
.rcache { display: inline-flex; align-items: center; gap: 5px; font-size: 12.5px; color: var(--slate); white-space: nowrap; }
.rdel { padding: 8px 12px; }

/* domain alias rows */
.drow { display: flex; align-items: center; padding: 7px 0; border-bottom: 1px solid var(--line); }
.drow:last-of-type { border-bottom: none; }

/* dropzone unggah zip */
.dropzone {
  display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 8px; padding: 26px; text-align: center; cursor: pointer;
  border: 2px dashed var(--line); border-radius: var(--r-input, 11px);
  background: var(--line-2); color: var(--muted); transition: .15s;
}
.dropzone:hover { border-color: var(--brand-l, #8f68e8); color: var(--brand); }
.dropzone.drag { border-color: var(--brand); background: var(--tint); color: var(--brand); }
.dz-input { display: none; }
.dz-ic { width: 26px; height: 26px; }
.dz-text { font-size: 13.5px; }
.dz-file { font-size: 13px; color: var(--ink); font-weight: 600; }

/* webhook auto-deploy */
.wh-box label { display: block; font-size: 12px; font-weight: 600; color: var(--muted); margin: 10px 0 4px; }
.wh-row { display: flex; gap: 8px; align-items: center; }
.wh-row code { flex: 1; background: var(--line-2); padding: 8px 10px; border-radius: 8px; font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
