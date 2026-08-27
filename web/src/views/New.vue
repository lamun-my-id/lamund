<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '../lib/api'
import { useScope } from '../stores/scope'
import { useAuth } from '../stores/auth'

const { t } = useI18n()
const router = useRouter()
const scope = useScope()
const auth = useAuth()

// Reverse-proxy ke upstream custom hanya operator/superadmin (tenant pakai app).
const visibleTypes = computed(() => TYPES.filter((t) => t.id !== 'proxy' || auth.isSuperadmin))

// Tipe deployment → apakah ia Site (static/proxy) atau App (runtime).
type Kind = 'static' | 'spa' | 'node' | 'go' | 'python' | 'proxy'

// SVG markup per icon key. Static string — aman untuk v-html.
const ICONS: Record<string, string> = {
  static: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/></svg>`,
  spa: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/></svg>`,
  node: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="12 2 22 8.5 22 15.5 12 22 2 15.5 2 8.5 12 2"/></svg>`,
  go: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/><line x1="19" y1="12" x2="5" y2="12"/></svg>`,
  python: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>`,
  proxy: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/><path d="M3 5v14"/></svg>`,
  upload: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>`,
  github: `<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.477 2 2 6.484 2 12.017c0 4.425 2.865 8.18 6.839 9.504.5.092.682-.217.682-.483 0-.237-.008-.868-.013-1.703-2.782.605-3.369-1.343-3.369-1.343-.454-1.158-1.11-1.466-1.11-1.466-.908-.62.069-.608.069-.608 1.003.07 1.531 1.032 1.531 1.032.892 1.53 2.341 1.088 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.113-4.555-4.951 0-1.093.39-1.988 1.029-2.688-.103-.253-.446-1.272.098-2.65 0 0 .84-.27 2.75 1.026A9.564 9.564 0 0 1 12 6.844a9.59 9.59 0 0 1 2.504.337c1.909-1.296 2.747-1.027 2.747-1.027.546 1.379.202 2.398.1 2.651.64.7 1.028 1.595 1.028 2.688 0 3.848-2.339 4.695-4.566 4.943.359.309.678.92.678 1.855 0 1.338-.012 2.419-.012 2.747 0 .268.18.58.688.482A10.02 10.02 0 0 0 22 12.017C22 6.484 17.522 2 12 2z"/></svg>`,
}

const TYPES: { id: Kind; icon: string; runtime: boolean }[] = [
  { id: 'static', icon: 'static', runtime: false },
  { id: 'spa', icon: 'spa', runtime: false },
  { id: 'node', icon: 'node', runtime: true },
  { id: 'go', icon: 'go', runtime: true },
  { id: 'python', icon: 'python', runtime: true },
  { id: 'proxy', icon: 'proxy', runtime: false },
]
const defaultCommand: Record<string, string> = {
  node: 'npm start',
  go: './app',
  python: 'python main.py',
}

const step = ref(1)
const kind = ref<Kind | null>(null)
const source = ref<'upload' | 'github'>('upload')
const isRuntime = computed(() => TYPES.find((x) => x.id === kind.value)?.runtime ?? false)
const canGithub = computed(() => isRuntime.value || kind.value === 'static' || kind.value === 'spa')

const domain = ref('')
const upstream = ref('')
const command = ref('')
const repoUrl = ref('')
const branch = ref('main')
const buildCmd = ref('npm run build')
const outputDir = ref('dist')
const file = ref<File | null>(null)
const error = ref('')
const busy = ref(false)

// Repo picker GitHub: kalau akun GitHub terhubung, muat daftar repo.
const repos = ref<{ full_name: string; private: boolean; clone_url: string }[]>([])
const reposLoaded = ref(false)
const repoQuery = ref('')
const selectedRepo = ref('')   // full_name, kosong = belum pilih
const branches = ref<{ name: string }[]>([])
const branchesLoaded = ref(false)

const filteredRepos = computed(() => {
  const q = repoQuery.value.toLowerCase().trim()
  if (!q) return repos.value.slice(0, 12)
  return repos.value.filter((r) => r.full_name.toLowerCase().includes(q)).slice(0, 12)
})

async function loadRepos() {
  if (reposLoaded.value) return
  reposLoaded.value = true
  repos.value = await api.githubRepos().catch(() => [])
}

function pickSource(src: 'upload' | 'github') {
  source.value = src
  if (src === 'github') loadRepos()
}

async function selectRepo(r: { full_name: string; clone_url: string }) {
  selectedRepo.value = r.full_name
  repoUrl.value = r.clone_url
  repoQuery.value = ''
  branchesLoaded.value = false
  branches.value = []
  const parts = r.full_name.split('/')
  const owner = parts[0]
  const name = parts[1]
  try {
    const res = await api.githubBranches(owner, name)
    branches.value = res.branches
    branchesLoaded.value = true
    // Default: main → master → first
    if (branches.value.some((b) => b.name === 'main')) branch.value = 'main'
    else if (branches.value.some((b) => b.name === 'master')) branch.value = 'master'
    else if (branches.value.length > 0) branch.value = branches.value[0].name
  } catch {
    branchesLoaded.value = true // gagal → fallback ke input teks
  }
}

function clearRepo() {
  selectedRepo.value = ''
  repoUrl.value = ''
  repoQuery.value = ''
  branch.value = 'main'
  branches.value = []
  branchesLoaded.value = false
}

function pickType(k: Kind) {
  kind.value = k
  command.value = defaultCommand[k] || ''
  // Reset git-related defaults per type.
  buildCmd.value = 'npm run build'
  outputDir.value = k === 'static' ? '.' : 'dist'
  step.value = 2
}
function onFile(e: Event) {
  file.value = (e.target as HTMLInputElement).files?.[0] ?? null
}
const dropActive = ref(false)
function onDrop(e: DragEvent) {
  dropActive.value = false
  const f = e.dataTransfer?.files?.[0]
  if (f) file.value = f
}
function fileSize(n: number) {
  return n < 1024 * 1024 ? `${(n / 1024).toFixed(0)} KB` : `${(n / 1024 / 1024).toFixed(1)} MB`
}

// DNS zone-aware: info tentang zona + auto-record checkbox.
const dnsInfo = ref<{ managed: boolean; zone: string; label: string; public_ip: string } | null>(null)
const dnsAuto = ref(true)

let dnsDebounceTimer: ReturnType<typeof setTimeout> | null = null
watch(domain, (val) => {
  if (dnsDebounceTimer) clearTimeout(dnsDebounceTimer)
  const trimmed = val.trim()
  if (!trimmed.includes('.')) {
    dnsInfo.value = null
    return
  }
  dnsDebounceTimer = setTimeout(async () => {
    try {
      dnsInfo.value = await api.dnsZoneFor(trimmed)
    } catch {
      dnsInfo.value = null
    }
  }, 400)
})
onUnmounted(() => {
  if (dnsDebounceTimer) clearTimeout(dnsDebounceTimer)
})

function ownerPayload() {
  return scope.current.type === 'team'
    ? { owner_type: 'team', owner_id: scope.current.id }
    : { owner_type: 'user' }
}

async function submit() {
  error.value = ''
  busy.value = true
  try {
    // Domain wajib diisi. Pakai domain hasil (final) dari response untuk
    // langkah deploy berikutnya.
    const inputD = domain.value.trim()
    const dnsAutoVal = dnsInfo.value?.managed ? dnsAuto.value : false
    let d = inputD
    if (isRuntime.value) {
      const app = await api.createApp({
        domain: inputD,
        command: command.value.trim(),
        autostart: true,
        dns_auto: dnsAutoVal,
        ...(source.value === 'github' ? { repo_url: repoUrl.value.trim(), branch: branch.value.trim() } : {}),
        ...ownerPayload(),
      })
      d = app?.domain || inputD
      if (source.value === 'upload' && file.value) await api.deploy(d, file.value)
      else if (source.value === 'github') await api.deployGit(d).catch(() => {})
    } else if (kind.value === 'proxy') {
      await api.createSite({ domain: inputD, type: 'proxy', proxy_target: upstream.value.trim(), dns_auto: dnsAutoVal, ...ownerPayload() })
    } else {
      // static / spa → situs statis.
      if (source.value === 'github') {
        // Git-backed: backend clone → build → serve output_dir.
        const site = await api.createSite({
          domain: inputD,
          type: 'static',
          repo_url: repoUrl.value.trim(),
          branch: branch.value.trim(),
          build_cmd: kind.value === 'spa' ? buildCmd.value.trim() : '',
          output_dir: outputDir.value.trim(),
          dns_auto: dnsAutoVal,
          ...ownerPayload(),
        })
        d = site?.domain || inputD
      } else {
        // Upload path: create site, optionally upload zip; SPA needs routes.
        const site = await api.createSite({ domain: inputD, type: 'static', dns_auto: dnsAutoVal, ...ownerPayload() })
        d = site?.domain || inputD
        if (file.value) await api.deploy(d, file.value)
        if (kind.value === 'spa') {
          await api.putRoutes(d, [{ path_prefix: '/', type: 'static', spa: true, cache: true }]).catch(() => {})
        }
      }
    }
    router.push({ name: 'sites' })
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Gagal membuat deployment'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="page-head">
    <div>
      <div class="h1">{{ t('wizard.title') }}</div>
      <div class="sub">{{ t('wizard.step' + step) }} · {{ scope.current.type === 'team' ? scope.current.label : t('team.personal') }}</div>
    </div>
  </div>

  <!-- Indikator langkah 1/2/3 -->
  <div class="steps">
    <div class="s" :class="{ on: step >= 1 }"></div>
    <div class="s" :class="{ on: step >= 2 }"></div>
    <div class="s" :class="{ on: step >= 3 }"></div>
  </div>

  <div v-if="error" class="alert err">{{ error }}</div>

  <!-- Langkah 1: tipe -->
  <div v-if="step === 1" class="card wz-card">
    <div class="st-sec"><span class="stepnum">1</span>{{ t('wizard.step1') }}</div>
    <div class="wz-types">
      <button
        v-for="ty in visibleTypes"
        :key="ty.id"
        type="button"
        class="wz-type"
        :class="{ on: kind === ty.id }"
        @click="pickType(ty.id)"
      >
        <span class="ic-wrap"><span class="ic" v-html="ICONS[ty.icon]"></span></span>
        <b>{{ t('wizard.type' + ty.id.charAt(0).toUpperCase() + ty.id.slice(1)) }}</b>
        <small>{{ t('wizard.type' + ty.id.charAt(0).toUpperCase() + ty.id.slice(1) + 'Desc') }}</small>
      </button>
    </div>
  </div>

  <!-- Langkah 2: sumber (runtime/static/spa → upload/github; proxy → langsung config) -->
  <div v-else-if="step === 2" class="card wz-card">
    <div class="st-sec"><span class="stepnum">2</span>{{ t('wizard.step2') }}</div>
    <template v-if="canGithub">
      <!-- Kartu sumber: Upload atau GitHub -->
      <div class="wz-types wz-sources" style="margin-bottom: 18px">
        <button type="button" class="wz-type" :class="{ on: source === 'upload' }" @click="pickSource('upload')">
          <span class="ic-wrap"><span class="ic" v-html="ICONS.upload"></span></span>
          <b>{{ t('wizard.srcUpload') }}</b>
          <small>{{ t('wizard.srcUploadDesc') }}</small>
        </button>
        <button type="button" class="wz-type" :class="{ on: source === 'github' }" @click="pickSource('github')">
          <span class="ic-wrap"><span class="ic" v-html="ICONS.github"></span></span>
          <b>{{ t('wizard.srcGithub') }}</b>
          <small>{{ t('wizard.srcGithubDesc') }}</small>
        </button>
      </div>

      <!-- Upload -->
      <div v-if="source === 'upload'" class="field">
        <label>{{ t('wizard.srcUpload') }} ({{ t('common.optional') }})</label>
        <label class="dropzone" :class="{ on: dropActive }" @dragover.prevent="dropActive = true" @dragleave.prevent="dropActive = false" @drop.prevent="onDrop">
          <input type="file" accept=".zip" class="dz-input" @change="onFile" />
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M12 16V4M7 9l5-5 5 5"/><path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/></svg>
          <span v-if="file" class="dz-file"><b>{{ file.name }}</b> · {{ fileSize(file.size) }}</span>
          <span v-else class="dz-hint">{{ t('wizard.dropHint') }}</span>
        </label>
      </div>

      <!-- GitHub -->
      <template v-else>
        <!-- GitHub tersambung: tampilkan repo search -->
        <template v-if="reposLoaded && repos.length > 0">
          <!-- Repo terpilih: chip read-only -->
          <div v-if="selectedRepo" class="field">
            <label>{{ t('wizard.srcGithub') }}</label>
            <div class="repo-chip">
              <span class="rc-icon" v-html="ICONS.github"></span>
              <span class="rc-name mono">{{ selectedRepo }}</span>
              <button type="button" class="btn sm" @click="clearRepo">{{ t('wizard.changeRepo') }}</button>
            </div>
          </div>
          <!-- Belum pilih repo: input search + daftar -->
          <div v-else class="field">
            <label>{{ t('wizard.searchRepo') }}</label>
            <div class="input">
              <input v-model="repoQuery" :placeholder="t('wizard.searchRepo') + '…'" />
            </div>
            <div v-if="filteredRepos.length" class="repo-list">
              <button
                v-for="r in filteredRepos"
                :key="r.full_name"
                type="button"
                class="repo-item"
                @click="selectRepo(r)"
              >
                <span class="ri-icon" v-html="ICONS.github"></span>
                <span class="ri-name mono">{{ r.full_name }}</span>
                <span v-if="r.private" class="ri-priv"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="width:13px;height:13px;vertical-align:middle"><rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg></span>
              </button>
            </div>
          </div>
        </template>
        <!-- GitHub tidak tersambung: input URL manual -->
        <template v-else>
          <div class="field">
            <label>{{ t('wizard.repoUrl') }}</label>
            <div class="input"><input v-model="repoUrl" class="mono" placeholder="https://github.com/user/repo" /></div>
          </div>
        </template>

        <!-- Branch: dropdown (jika branches dimuat) atau input teks -->
        <div v-if="selectedRepo" class="field">
          <label>{{ t('wizard.branch') }}</label>
          <div v-if="branchesLoaded && branches.length > 0" class="input">
            <select v-model="branch">
              <option v-for="b in branches" :key="b.name" :value="b.name">{{ b.name }}</option>
            </select>
          </div>
          <div v-else class="input">
            <input v-model="branch" class="mono" placeholder="main" />
          </div>
        </div>
        <div v-else class="field">
          <label>{{ t('wizard.branch') }}</label>
          <div class="input"><input v-model="branch" class="mono" placeholder="main" /></div>
        </div>

        <!-- Build command: only for SPA -->
        <div v-if="kind === 'spa'" class="field">
          <label>{{ t('wizard.buildCmd') }}</label>
          <div class="input"><input v-model="buildCmd" class="mono" placeholder="npm run build" /></div>
        </div>
        <!-- Output dir: for static and spa, dengan tooltip -->
        <div v-if="kind === 'static' || kind === 'spa'" class="field">
          <label>
            {{ t('wizard.outputDir') }}
            <span class="tip-icon" :title="t('wizard.outputDirTip')">?</span>
          </label>
          <div class="input"><input v-model="outputDir" class="mono" :placeholder="kind === 'static' ? '.' : 'dist'" /></div>
        </div>
      </template>
    </template>
    <template v-else-if="kind === 'proxy'">
      <p class="sub">{{ t('wizard.typeProxyDesc') }}</p>
    </template>
    <div class="wz-actions">
      <button class="btn" @click="step = 1">{{ t('common.back') }}</button>
      <button class="btn pri" :disabled="canGithub && source === 'github' && !repoUrl.trim()" @click="step = 3">{{ t('common.next') }}</button>
    </div>
  </div>

  <!-- Langkah 3: config -->
  <form v-else class="card wz-card" @submit.prevent="submit">
    <div class="st-sec"><span class="stepnum">3</span>{{ t('wizard.step3') }}</div>
    <div class="field">
      <label>{{ t('wizard.domain') }}</label>
      <div class="input"><input v-model="domain" class="mono" :placeholder="isRuntime ? 'app.example.com' : t('wizard.domainPlaceholder')" required /></div>
      <div v-if="dnsInfo?.managed" class="dns-hint dns-managed">
        <span class="tag t-app">&#10003; {{ t('dns.autoManaged', { zone: dnsInfo.zone }) }}</span>
        <label class="dns-check">
          <input type="checkbox" v-model="dnsAuto" />
          {{ t('dns.autoCreate') }}
        </label>
      </div>
      <div v-else-if="dnsInfo && !dnsInfo.managed" class="field-hint muted">
        {{ t('dns.manualPoint', { ip: dnsInfo.public_ip || '—' }) }}
      </div>
    </div>
    <div v-if="kind === 'proxy'" class="field"><label>{{ t('wizard.upstream') }}</label><div class="input"><input v-model="upstream" class="mono" placeholder="https://backend.example.com" required /></div></div>
    <div v-if="isRuntime" class="field"><label>{{ t('wizard.command') }}</label><div class="input"><input v-model="command" class="mono" required /></div></div>
    <div class="wz-actions">
      <button class="btn" type="button" @click="step = 2">{{ t('common.back') }}</button>
      <button class="btn pri" type="submit" :disabled="busy">{{ busy ? t('wizard.creating') : t('wizard.create') }}</button>
    </div>
  </form>
</template>

<style scoped>
.wz-card { padding: 24px 26px; margin-bottom: 16px; }

/* section header dengan nomor langkah */
.st-sec { display: flex; align-items: center; }
.stepnum {
  width: 26px; height: 26px; border-radius: 50%;
  background: var(--brand); color: #fff;
  display: inline-grid; place-items: center;
  font-weight: 700; font-size: 13px; margin-right: 10px;
  text-transform: none; letter-spacing: 0;
}

/* grid tipe deployment — kartu berdesain */
.wz-types { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; margin-bottom: 24px; }
.wz-sources { grid-template-columns: 1fr 1fr; }

.wz-type {
  border: 1.5px solid var(--line); border-radius: var(--r-card);
  padding: 18px 16px 16px; cursor: pointer; background: var(--card);
  text-align: left; font: inherit; color: var(--ink);
  display: flex; flex-direction: column; gap: 6px;
  transition: border-color .15s, box-shadow .15s, transform .15s, background .15s;
  box-shadow: 0 2px 8px -4px rgba(0,0,0,.08);
}
.wz-type:hover {
  border-color: var(--brand-l);
  box-shadow: 0 6px 18px -8px rgba(77,48,149,.22);
  transform: translateY(-2px);
}
.wz-type.on {
  border-color: var(--brand);
  background: var(--tint);
  box-shadow: 0 0 0 3px var(--tint), 0 6px 18px -8px rgba(77,48,149,.2);
}

/* Ikon dalam bulatan tint */
.ic-wrap {
  width: 38px; height: 38px; border-radius: 10px;
  background: var(--tint); display: grid; place-items: center;
  margin-bottom: 4px; flex: 0 0 38px;
}
.wz-type.on .ic-wrap { background: rgba(77,48,149,.15); }
.wz-type .ic { display: flex; align-items: center; justify-content: center; color: var(--brand); }
/* :deep — SVG disisipkan via v-html tak dapat atribut scoped, jadi selektor
   biasa tak match; :deep menembusnya agar ukuran ikon berlaku. */
.wz-type .ic :deep(svg) { width: 22px; height: 22px; }

.wz-type b { font-family: var(--font-display); font-size: 14.5px; font-weight: 700; letter-spacing: -.2px; }
.wz-type small { display: block; color: var(--muted); font-size: 12px; line-height: 1.4; }

.wz-actions { display: flex; gap: 10px; margin-top: 8px; }

/* drag & drop upload */
.dropzone {
  display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px;
  padding: 34px 20px; border: 2px dashed var(--line); border-radius: var(--r-input);
  background: var(--card); color: var(--muted); cursor: pointer; text-align: center;
  transition: border-color .15s, background .15s, color .15s;
}
.dropzone:hover { border-color: var(--brand-l); }
.dropzone.on { border-color: var(--brand); background: var(--tint); color: var(--brand); }
.dropzone svg { width: 30px; height: 30px; stroke-width: 1.8; color: var(--brand); }
.dz-input { position: absolute; width: 1px; height: 1px; opacity: 0; overflow: hidden; }
.dz-hint { font-size: 13px; }
.dz-file { font-size: 13.5px; color: var(--ink); }
.dz-file b { color: var(--brand); }

/* Tooltip icon pada label */
.tip-icon {
  display: inline-grid; place-items: center;
  width: 16px; height: 16px; border-radius: 50%;
  background: var(--tint); color: var(--brand);
  font-size: 10px; font-weight: 700; font-style: normal;
  cursor: help; margin-left: 5px; vertical-align: middle;
  border: 1px solid var(--brand-l);
}

/* Chip repo terpilih */
.repo-chip {
  display: flex; align-items: center; gap: 10px;
  padding: 10px 14px; border-radius: var(--r-input);
  background: var(--tint); border: 1px solid var(--brand-l);
}
.rc-icon { display: flex; align-items: center; color: var(--brand); }
.rc-icon svg { width: 18px; height: 18px; }
.rc-name { flex: 1; font-size: 13.5px; color: var(--ink); font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* Daftar repo pencarian */
.repo-list {
  margin-top: 6px; border: 1px solid var(--line); border-radius: var(--r-input);
  background: var(--card); overflow: hidden;
  max-height: 220px; overflow-y: auto;
}
.repo-item {
  display: flex; align-items: center; gap: 8px;
  width: 100%; padding: 9px 13px;
  background: none; border: 0; border-bottom: 1px solid var(--line-2);
  font: inherit; color: var(--ink); cursor: pointer; text-align: left;
  transition: background .1s;
}
.repo-item:last-child { border-bottom: 0; }
.repo-item:hover { background: var(--tint); }
.ri-icon { display: flex; align-items: center; color: var(--muted); flex: 0 0 16px; }
.ri-icon :deep(svg) { width: 16px; height: 16px; }
.ri-name { flex: 1; font-size: 13px; }
.ri-priv { font-size: 12px; color: var(--muted); }

/* DNS zone-aware badge + checkbox */
.dns-hint { margin-top: 8px; display: flex; flex-direction: column; gap: 6px; }
.dns-check { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--ink); cursor: pointer; }
.field-hint { color: var(--muted); font-size: 12.5px; margin-top: 6px; }

@media (max-width: 820px) {
  .wz-types { grid-template-columns: 1fr 1fr; }
  .wz-sources { grid-template-columns: 1fr 1fr; }
}
@media (max-width: 640px) {
  .wz-types { grid-template-columns: 1fr; }
  .wz-sources { grid-template-columns: 1fr; }
}
</style>
