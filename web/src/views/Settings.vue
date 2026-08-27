<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { confirmDialog } from '../lib/dialog'
import QRCode from 'qrcode'
import { api, type ApiKey, type Connection, type EmailSettings } from '../lib/api'
import { useAuth } from '../stores/auth'
import { THEMES, getTheme, applyTheme, type Theme } from '../lib/theme'
import { LOCALES, getLocale, setLocale, type Locale } from '../i18n'

// tipe internal untuk form DNS settings
interface DnsSettingsForm {
  ns1: string
  ns2: string
  hostmaster: string
  public_ip: string
}

const { t } = useI18n()
const auth = useAuth()

const theme = ref<Theme>(getTheme())
const locale = ref<Locale>(getLocale())

// UI-state: modal terbuka saat ini ('' = tertutup)
const modal = ref('')
function openM(m: string) {
  modal.value = m
}
function closeM() {
  modal.value = ''
}

const THEME_LABEL: Record<string, string> = {
  editorial: 'Editorial',
  terminal: 'Terminal',
  seagrass: 'Seagrass',
}

// Menyimpan preferensi ke server agar sinkron lintas peramban (best-effort).
function persistPrefs() {
  api.setPrefs({ theme: theme.value === 'editorial' ? '' : theme.value, locale: locale.value }).catch(() => {})
}
function setTheme(tm: Theme) {
  theme.value = tm
  applyTheme(tm)
  persistPrefs()
}
function pickLocale(l: Locale) {
  locale.value = l
  setLocale(l)
  persistPrefs()
}

// profil
const profile = ref({ name: auth.name, email: auth.email })
const profileMsg = ref('')
const profileBusy = ref(false)
async function saveProfile() {
  profileMsg.value = ''
  profileBusy.value = true
  try {
    await api.updateAccount({ name: profile.value.name.trim(), email: profile.value.email.trim() })
    auth.setProfile(profile.value.name.trim(), profile.value.email.trim())
    profileMsg.value = t('settings.profileSaved')
  } catch (e) {
    profileMsg.value = (e as Error).message
  } finally {
    profileBusy.value = false
  }
}

const keys = ref<ApiKey[]>([])
const loading = ref(true)
const error = ref('')
const newName = ref('')
const created = ref<{ name: string; key: string } | null>(null)
const busy = ref(false)

const pw = ref({ current: '', next: '' })
const pwMsg = ref('')
const pwErr = ref('')
const pwBusy = ref(false)

async function changePassword() {
  pwMsg.value = ''
  pwErr.value = ''
  pwBusy.value = true
  try {
    await api.changePassword(pw.value.current, pw.value.next)
    pw.value = { current: '', next: '' }
    pwMsg.value = t('settings.passwordChanged')
  } catch (e) {
    pwErr.value = (e as Error).message
  } finally {
    pwBusy.value = false
  }
}

// akun terhubung
const conns = ref<Connection[]>([])
const connToken = ref<Record<string, string>>({})
const connBusy = ref('')
const connErr = ref('')

const ghDevice = ref<{ user_code: string; verification_uri: string } | null>(null)
const ghWaiting = ref(false)
let ghTimer: number | undefined

async function githubConnect() {
  ghWaiting.value = true
  connErr.value = ''
  try {
    const d = await api.githubDeviceStart()
    ghDevice.value = { user_code: d.user_code, verification_uri: d.verification_uri }
    window.open(d.verification_uri, '_blank')
    clearInterval(ghTimer)
    ghTimer = window.setInterval(async () => {
      const r = await api.githubDevicePoll().catch(() => ({ status: 'error' as const }))
      if (r.status === 'connected') {
        clearInterval(ghTimer)
        ghDevice.value = null
        ghWaiting.value = false
        await loadConns()
      } else if (r.status === 'error') {
        clearInterval(ghTimer)
        ghWaiting.value = false
        ghDevice.value = null
        connErr.value = t('settings.githubConnectFailed')
      }
    }, Math.max(2, d.interval) * 1000)
  } catch (e) {
    ghWaiting.value = false
    connErr.value = (e as Error).message
  }
}

const PROVIDERS = [
  { id: 'github', label: 'GitHub' },
  { id: 'openai', label: 'OpenAI' },
  { id: 'anthropic', label: 'Anthropic' },
  { id: 'gemini', label: 'Gemini' },
]

function connOf(p: string): Connection | undefined {
  return conns.value.find((c) => c.provider === p)
}
const connCount = computed(() => PROVIDERS.filter((p) => connOf(p.id)?.connected).length)
// Inisial badge provider untuk .conn .ic (mengikuti mockup)
const PROVIDER_BADGE: Record<string, { txt: string; bg: string }> = {
  github: { txt: 'GH', bg: '#24292f' },
  openai: { txt: 'AI', bg: '#0b8a6b' },
  anthropic: { txt: 'A', bg: '#d97757' },
  gemini: { txt: 'G', bg: '#4285f4' },
}
const githubDevice = ref(false) // apakah "Hubungkan GitHub" satu-klik aktif di server
async function loadConns() {
  try {
    const r = await api.listConnections()
    conns.value = r.connections
    githubDevice.value = r.github_device
  } catch {
    /* opsional */
  }
}
async function connect(p: string) {
  const token = (connToken.value[p] || '').trim()
  if (token.length < 8) return
  connBusy.value = p
  connErr.value = ''
  try {
    await api.setConnection(p, token)
    connToken.value[p] = ''
    await loadConns()
  } catch (e) {
    connErr.value = (e as Error).message
  } finally {
    connBusy.value = ''
  }
}
async function disconnect(p: string) {
  if (!(await confirmDialog({ message: `${t('settings.disconnect')} ${p}?` }))) return
  try {
    await api.deleteConnection(p)
    await loadConns()
  } catch (e) {
    connErr.value = (e as Error).message
  }
}

async function load() {
  loading.value = true
  try {
    keys.value = await api.listKeys()
    await loadConns()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function create() {
  if (!newName.value.trim()) return
  busy.value = true
  error.value = ''
  try {
    const r = await api.createKey(newName.value.trim())
    created.value = { name: r.name, key: r.key }
    newName.value = ''
    await load()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}

async function remove(k: ApiKey) {
  if (!(await confirmDialog({ message: t('settings.apiKeyDeleteConfirm', { name: k.name }) }))) return
  try {
    await api.deleteKey(k.id)
    await load()
  } catch (e) {
    error.value = (e as Error).message
  }
}

function copy(v: string) {
  navigator.clipboard?.writeText(v)
}

// ---- setelan email (superadmin saja) ----
const emailForm = ref<EmailSettings>({ backend: 'off', host: '', port: 587, username: '', password: '', from: '', tls: true, api_base: '', api_key: '' })
const emailMsg = ref('')
const emailErr = ref('')
const emailBusy = ref(false)
const emailTestBusy = ref(false)

async function loadEmailSettings() {
  if (!auth.isSuperadmin) return
  try {
    const s = await api.getEmailSettings()
    emailForm.value = {
      backend: s.backend,
      host: s.host ?? '',
      port: s.port ?? 587,
      username: s.username ?? '',
      password: '',
      from: s.from ?? '',
      tls: s.tls ?? true,
      api_base: s.api_base ?? '',
      api_key: '',
    }
  } catch {
    /* best-effort */
  }
}

async function saveEmailSettings() {
  emailMsg.value = ''
  emailErr.value = ''
  emailBusy.value = true
  try {
    const body: EmailSettings = { ...emailForm.value }
    await api.putEmailSettings(body)
    emailMsg.value = t('settings.emailSaved')
  } catch (e) {
    emailErr.value = (e as Error).message
  } finally {
    emailBusy.value = false
  }
}

async function sendTestEmail() {
  emailMsg.value = ''
  emailErr.value = ''
  emailTestBusy.value = true
  try {
    await api.testEmail()
    emailMsg.value = t('settings.emailTestOk')
  } catch (e) {
    emailErr.value = (e as Error).message
  } finally {
    emailTestBusy.value = false
  }
}

// ---- setelan DNS nameserver (superadmin saja) ----
const dnsForm = ref<DnsSettingsForm>({ ns1: '', ns2: '', hostmaster: '', public_ip: '' })
const dnsMsg = ref('')
const dnsErr = ref('')
const dnsBusy = ref(false)

async function loadDnsSettings() {
  if (!auth.isSuperadmin) return
  try {
    const s = await api.dnsSettings()
    dnsForm.value = {
      ns1: s.ns1 ?? '',
      ns2: s.ns2 ?? '',
      hostmaster: s.hostmaster ?? '',
      public_ip: s.public_ip ?? '',
    }
  } catch {
    /* best-effort */
  }
}

async function saveDnsSettings() {
  dnsMsg.value = ''
  dnsErr.value = ''
  dnsBusy.value = true
  try {
    await api.setDnsSettings({ ns1: dnsForm.value.ns1, ns2: dnsForm.value.ns2, hostmaster: dnsForm.value.hostmaster })
    dnsMsg.value = t('dns.settingsSaved')
  } catch (e) {
    dnsErr.value = (e as Error).message
  } finally {
    dnsBusy.value = false
  }
}

// ---- Keamanan (MFA) — milik pengguna sendiri ----
const mfaEnabled = ref(false)
const mfaMsg = ref('')
const mfaErr = ref('')
const mfaBusy = ref(false)
// Alur enrollment: setup → verify → recovery codes.
const mfaSetup = ref<{ secret: string; uri: string } | null>(null)
const mfaQr = ref('') // data-URL QR dari uri
const mfaCode = ref('')
const mfaRecovery = ref<string[] | null>(null)
// Alur nonaktifkan: minta kode.
const mfaDisableCode = ref('')

async function loadMfaStatus() {
  try {
    const s = await api.mfaStatus()
    mfaEnabled.value = s.enabled
  } catch {
    /* best-effort */
  }
}

async function startMfaSetup() {
  mfaMsg.value = ''
  mfaErr.value = ''
  mfaBusy.value = true
  try {
    const s = await api.mfaSetup()
    mfaSetup.value = s
    mfaQr.value = await QRCode.toDataURL(s.uri, { margin: 1, width: 220 })
    mfaCode.value = ''
  } catch (e) {
    mfaErr.value = (e as Error).message
  } finally {
    mfaBusy.value = false
  }
}

async function verifyMfaSetup() {
  if (!mfaCode.value.trim()) return
  mfaMsg.value = ''
  mfaErr.value = ''
  mfaBusy.value = true
  try {
    const r = await api.mfaVerify(mfaCode.value.trim())
    mfaRecovery.value = r.recovery_codes
    mfaEnabled.value = true
    mfaSetup.value = null
    mfaQr.value = ''
    mfaCode.value = ''
  } catch (e) {
    mfaErr.value = (e as Error).message || t('mfa.invalidCode')
  } finally {
    mfaBusy.value = false
  }
}

function cancelMfaSetup() {
  mfaSetup.value = null
  mfaQr.value = ''
  mfaCode.value = ''
  mfaErr.value = ''
}

async function disableMfa() {
  if (!mfaDisableCode.value.trim()) return
  mfaMsg.value = ''
  mfaErr.value = ''
  mfaBusy.value = true
  try {
    await api.mfaDisable(mfaDisableCode.value.trim())
    mfaEnabled.value = false
    mfaDisableCode.value = ''
    mfaRecovery.value = null
    mfaMsg.value = t('mfa.disabled')
  } catch (e) {
    mfaErr.value = (e as Error).message || t('mfa.invalidCode')
  } finally {
    mfaBusy.value = false
  }
}

function dismissRecovery() {
  mfaRecovery.value = null
  mfaMsg.value = t('mfa.enabled')
}

onUnmounted(() => clearInterval(ghTimer))
onMounted(async () => {
  await load()
  await loadMfaStatus()
  await loadEmailSettings()
  await loadDnsSettings()
})
</script>

<template>
  <div class="page-head">
    <div>
      <div class="h1">{{ t('settings.title') }}</div>
      <div class="sub">{{ t('settings.subtitle') }}</div>
    </div>
  </div>

  <div v-if="error" class="alert err">{{ error }}</div>

  <!-- ===== Akun ===== -->
  <div class="sec set-group"><h3>{{ t('settings.account') }}</h3></div>
  <div class="menu set-menu">
    <button type="button" class="mrow" @click="openM('account')">
      <span class="mic">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="12" cy="8" r="3.2" /><path d="M5 20a7 7 0 0 1 14 0" /></svg>
      </span>
      <span class="ml"><b>{{ t('settings.account') }}</b><small>{{ t('settings.accountHint') }}</small></span>
      <span class="mval">{{ profile.name }}</span>
      <span class="chev"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M9 6l6 6-6 6" /></svg></span>
    </button>
    <button type="button" class="mrow" @click="openM('password')">
      <span class="mic">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="5" y="11" width="14" height="9" rx="2" /><path d="M8 11V8a4 4 0 0 1 8 0v3" /></svg>
      </span>
      <span class="ml"><b>{{ t('settings.password') }}</b><small>{{ t('settings.passwordChange') }}</small></span>
      <span class="chev"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M9 6l6 6-6 6" /></svg></span>
    </button>
    <button type="button" class="mrow" @click="openM('conn')">
      <span class="mic">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M10 13a5 5 0 0 0 7 0l2-2a5 5 0 0 0-7-7l-1 1M14 11a5 5 0 0 0-7 0l-2 2a5 5 0 0 0 7 7l1-1" /></svg>
      </span>
      <span class="ml"><b>{{ t('settings.connections') }}</b><small>{{ t('settings.connectionsHint') }}</small></span>
      <span class="mval">{{ connCount }} / {{ PROVIDERS.length }}</span>
      <span class="chev"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M9 6l6 6-6 6" /></svg></span>
    </button>
    <button type="button" class="mrow" @click="openM('keys')">
      <span class="mic">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="8" cy="15" r="4" /><path d="M10.5 12.5L20 3M17 6l2 2M15 8l2 2" /></svg>
      </span>
      <span class="ml"><b>{{ t('settings.apiKeys') }}</b><small>{{ t('settings.apiKeyName') }}</small></span>
      <span class="mval">{{ keys.length }}</span>
      <span class="chev"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M9 6l6 6-6 6" /></svg></span>
    </button>
  </div>

  <!-- ===== Preferensi ===== -->
  <div class="sec set-group"><h3>Preferensi</h3></div>
  <div class="menu set-menu">
    <button type="button" class="mrow" @click="openM('theme')">
      <span class="mic">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="12" cy="12" r="9" /><path d="M12 3a9 9 0 0 0 0 18 4.5 4.5 0 0 1 0-9 4.5 4.5 0 0 0 0-9z" /></svg>
      </span>
      <span class="ml"><b>{{ t('settings.theme') }}</b><small>{{ t('settings.themeHint') }}</small></span>
      <span class="mval">{{ THEME_LABEL[theme] }}</span>
      <span class="chev"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M9 6l6 6-6 6" /></svg></span>
    </button>
    <button type="button" class="mrow" @click="openM('lang')">
      <span class="mic">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="12" cy="12" r="9" /><path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18" /></svg>
      </span>
      <span class="ml"><b>{{ t('settings.language') }}</b><small>{{ t('settings.languageHint') }}</small></span>
      <span class="mval">{{ LOCALES.find((l) => l.id === locale)?.native }}</span>
      <span class="chev"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M9 6l6 6-6 6" /></svg></span>
    </button>
  </div>

  <!-- ===== Keamanan (MFA) — semua pengguna ===== -->
  <div class="sec set-group"><h3>{{ t('mfa.title') }}</h3></div>
  <div class="card set-card" style="padding: 20px; margin-bottom: 22px">
    <div v-if="mfaMsg" class="alert ok" style="margin-bottom: 14px">{{ mfaMsg }}</div>
    <div v-if="mfaErr" class="alert err" style="margin-bottom: 14px">{{ mfaErr }}</div>

    <!-- Recovery codes (ditampilkan sekali setelah verifikasi) -->
    <template v-if="mfaRecovery">
      <div class="alert warn" style="display: block; margin-bottom: 14px">
        <b>{{ t('mfa.recoveryTitle') }}</b>
        <p style="margin: 6px 0 0">{{ t('mfa.recoveryWarn') }}</p>
      </div>
      <div class="recovery-box">
        <code v-for="rc in mfaRecovery" :key="rc">{{ rc }}</code>
      </div>
      <div style="display: flex; gap: 10px; flex-wrap: wrap; margin-top: 14px">
        <button class="btn" @click="copy(mfaRecovery.join('\n'))">{{ t('common.copy') }}</button>
        <button class="btn pri" @click="dismissRecovery">{{ t('mfa.recoverySaved') }}</button>
      </div>
    </template>

    <!-- MFA aktif -->
    <template v-else-if="mfaEnabled">
      <p style="margin: 0 0 14px; color: var(--slate)">
        <b style="color: var(--ok, #0b8a6b)">● {{ t('mfa.active') }}</b>
      </p>
      <div class="field">
        <label>{{ t('mfa.enterCode') }}</label>
        <div class="input">
          <input v-model="mfaDisableCode" type="text" inputmode="numeric" autocomplete="one-time-code" maxlength="20" placeholder="000000" />
        </div>
      </div>
      <button class="btn danger" :disabled="mfaBusy || !mfaDisableCode.trim()" @click="disableMfa">
        {{ mfaBusy ? t('common.saving') : t('mfa.disable') }}
      </button>
    </template>

    <!-- Enrollment: setup + QR + verify -->
    <template v-else-if="mfaSetup">
      <p style="margin: 0 0 14px; color: var(--slate)">{{ t('mfa.scanQr') }}</p>
      <div class="mfa-enroll">
        <img v-if="mfaQr" :src="mfaQr" alt="MFA QR" class="mfa-qr" />
        <div class="mfa-manual">
          <label style="font-size: 13px; color: var(--muted)">{{ t('mfa.manualSecret') }}</label>
          <div class="key-reveal" style="margin-top: 6px">
            <code>{{ mfaSetup.secret }}</code>
            <button class="btn sm" style="margin-left: auto" @click="copy(mfaSetup.secret)">{{ t('common.copy') }}</button>
          </div>
          <a :href="mfaSetup.uri" class="btn sm block" style="margin-top: 8px">{{ t('mfa.openApp') }}</a>
          <p style="font-size: 12px; color: var(--muted); margin: 6px 0 0">{{ t('mfa.openAppHint') }}</p>
        </div>
      </div>
      <div class="field" style="margin-top: 16px">
        <label>{{ t('mfa.enterCode') }}</label>
        <div class="input">
          <input v-model="mfaCode" type="text" inputmode="numeric" autocomplete="one-time-code" maxlength="6" placeholder="000000" />
        </div>
      </div>
      <div style="display: flex; gap: 10px; flex-wrap: wrap">
        <button class="btn" @click="cancelMfaSetup">{{ t('common.cancel') }}</button>
        <button class="btn pri" :disabled="mfaBusy || !mfaCode.trim()" @click="verifyMfaSetup">
          {{ mfaBusy ? t('common.saving') : t('mfa.verify') }}
        </button>
      </div>
    </template>

    <!-- MFA nonaktif -->
    <template v-else>
      <p style="margin: 0 0 14px; color: var(--slate)">
        <b style="color: var(--muted)">○ {{ t('mfa.inactive') }}</b>
      </p>
      <button class="btn pri" :disabled="mfaBusy" @click="startMfaSetup">
        {{ mfaBusy ? t('common.loading') : t('mfa.enable') }}
      </button>
    </template>
  </div>

  <!-- ===== Email (superadmin) ===== -->
  <template v-if="auth.isSuperadmin">
    <div class="sec set-group"><h3>{{ t('settings.emailSection') }}</h3></div>
    <div class="card set-card" style="padding: 20px; margin-bottom: 22px">
      <div v-if="emailMsg" class="alert ok" style="margin-bottom: 14px">{{ emailMsg }}</div>
      <div v-if="emailErr" class="alert err" style="margin-bottom: 14px">{{ emailErr }}</div>

      <div class="field">
        <label>{{ t('settings.emailBackend') }}</label>
        <div class="input">
          <select v-model="emailForm.backend" style="width: 100%; background: transparent; border: none; outline: none; font: inherit; color: inherit">
            <option value="off">{{ t('settings.emailBackendOff') }}</option>
            <option value="smtp">{{ t('settings.emailBackendSmtp') }}</option>
            <option value="lamunmail">{{ t('settings.emailBackendLamunmail') }}</option>
          </select>
        </div>
      </div>

      <!-- SMTP fields -->
      <template v-if="emailForm.backend === 'smtp'">
        <div class="field">
          <label>{{ t('settings.emailHost') }}</label>
          <div class="input"><input v-model="emailForm.host" type="text" placeholder="smtp.example.com" /></div>
        </div>
        <div class="field">
          <label>{{ t('settings.emailPort') }}</label>
          <div class="input"><input v-model.number="emailForm.port" type="number" placeholder="587" /></div>
        </div>
        <div class="field">
          <label>{{ t('settings.emailUsername') }}</label>
          <div class="input"><input v-model="emailForm.username" type="text" autocomplete="off" /></div>
        </div>
        <div class="field">
          <label>{{ t('settings.emailPassword') }}</label>
          <div class="input"><input v-model="emailForm.password" type="password" autocomplete="new-password" :placeholder="t('settings.emailPasswordPlaceholder')" /></div>
        </div>
        <div class="field">
          <label>{{ t('settings.emailFrom') }}</label>
          <div class="input"><input v-model="emailForm.from" type="email" placeholder="noreply@example.com" /></div>
        </div>
        <div class="field" style="margin-bottom: 16px">
          <label style="display: flex; align-items: center; gap: 8px; cursor: pointer">
            <input v-model="emailForm.tls" type="checkbox" />
            {{ t('settings.emailTls') }}
          </label>
        </div>
      </template>

      <!-- Lamun Mail fields -->
      <template v-if="emailForm.backend === 'lamunmail'">
        <div class="field">
          <label>{{ t('settings.emailApiBase') }}</label>
          <div class="input"><input v-model="emailForm.api_base" type="url" placeholder="https://mail.example.com" /></div>
        </div>
        <div class="field">
          <label>{{ t('settings.emailApiKey') }}</label>
          <div class="input"><input v-model="emailForm.api_key" type="password" autocomplete="new-password" :placeholder="t('settings.emailPasswordPlaceholder')" /></div>
        </div>
        <div class="field" style="margin-bottom: 16px">
          <label>{{ t('settings.emailFrom') }}</label>
          <div class="input"><input v-model="emailForm.from" type="email" placeholder="noreply@example.com" /></div>
        </div>
      </template>

      <div style="display: flex; gap: 10px; flex-wrap: wrap">
        <button class="btn pri" :disabled="emailBusy" @click="saveEmailSettings">{{ emailBusy ? t('common.saving') : t('common.save') }}</button>
        <button v-if="emailForm.backend !== 'off'" class="btn" :disabled="emailTestBusy" @click="sendTestEmail">{{ emailTestBusy ? t('settings.emailTesting') : t('settings.emailTest') }}</button>
      </div>
    </div>
  </template>

  <!-- ===== DNS Nameserver (superadmin) ===== -->
  <template v-if="auth.isSuperadmin">
    <div class="sec set-group"><h3>{{ t('dns.settingsTitle') }}</h3></div>
    <div class="card set-card" style="padding: 20px; margin-bottom: 22px">
      <div v-if="dnsMsg" class="alert ok" style="margin-bottom: 14px">{{ dnsMsg }}</div>
      <div v-if="dnsErr" class="alert err" style="margin-bottom: 14px">{{ dnsErr }}</div>

      <div class="field">
        <label>{{ t('dns.ns1') }}</label>
        <div class="input"><input v-model="dnsForm.ns1" type="text" placeholder="ns1.example.com" /></div>
      </div>
      <div class="field">
        <label>{{ t('dns.ns2') }}</label>
        <div class="input"><input v-model="dnsForm.ns2" type="text" placeholder="ns2.example.com" /></div>
      </div>
      <div class="field">
        <label>{{ t('dns.hostmaster') }}</label>
        <div class="input"><input v-model="dnsForm.hostmaster" type="text" placeholder="hostmaster.example.com" /></div>
      </div>
      <div v-if="dnsForm.public_ip" class="field" style="margin-bottom: 16px">
        <label>{{ t('dns.publicIp') }}</label>
        <div class="input" style="background: var(--bg)">
          <span class="mono" style="color: var(--muted)">{{ dnsForm.public_ip }}</span>
        </div>
      </div>

      <button class="btn pri" :disabled="dnsBusy" @click="saveDnsSettings">
        {{ dnsBusy ? t('common.saving') : t('dns.save') }}
      </button>
    </div>
  </template>

  <!-- ================= MODALS ================= -->

  <!-- Edit akun / profil -->
  <div class="modal" :class="{ show: modal === 'account' }" @click.self="closeM">
    <form class="modal-box" @submit.prevent="saveProfile">
      <div class="modal-head">
        <h3>{{ t('settings.account') }}</h3>
        <button type="button" class="modal-x" @click="closeM"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M6 6l12 12M18 6L6 18" /></svg></button>
      </div>
      <div class="modal-body">
        <div v-if="profileMsg" class="alert ok">{{ profileMsg }}</div>
        <div class="field">
          <label>{{ t('settings.profileName') }}</label>
          <div class="input"><input v-model="profile.name" type="text" autocomplete="name" /></div>
        </div>
        <div class="field" style="margin: 0">
          <label>{{ t('settings.profileEmail') }}</label>
          <div class="input"><input v-model="profile.email" type="email" autocomplete="email" /></div>
        </div>
      </div>
      <div class="modal-foot">
        <button type="button" class="btn" @click="closeM">{{ t('common.cancel') }}</button>
        <button class="btn pri" type="submit" :disabled="profileBusy">{{ profileBusy ? t('common.saving') : t('common.save') }}</button>
      </div>
    </form>
  </div>

  <!-- Ganti sandi -->
  <div class="modal" :class="{ show: modal === 'password' }" @click.self="closeM">
    <form class="modal-box" @submit.prevent="changePassword">
      <div class="modal-head">
        <h3>{{ t('settings.password') }}</h3>
        <button type="button" class="modal-x" @click="closeM"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M6 6l12 12M18 6L6 18" /></svg></button>
      </div>
      <div class="modal-body">
        <div v-if="pwMsg" class="alert ok">{{ pwMsg }}</div>
        <div v-if="pwErr" class="alert err">{{ pwErr }}</div>
        <div class="field">
          <label>{{ t('settings.passwordCurrent') }}</label>
          <div class="input"><input v-model="pw.current" type="password" autocomplete="current-password" required /></div>
        </div>
        <div class="field" style="margin: 0">
          <label>{{ t('settings.passwordNew') }}</label>
          <div class="input"><input v-model="pw.next" type="password" autocomplete="new-password" required /></div>
        </div>
      </div>
      <div class="modal-foot">
        <button type="button" class="btn" @click="closeM">{{ t('common.cancel') }}</button>
        <button class="btn pri" type="submit" :disabled="pwBusy">{{ pwBusy ? t('common.saving') : t('settings.passwordChange') }}</button>
      </div>
    </form>
  </div>

  <!-- Akun terhubung -->
  <div class="modal" :class="{ show: modal === 'conn' }" @click.self="closeM">
    <div class="modal-box">
      <div class="modal-head">
        <h3>{{ t('settings.connections') }}</h3>
        <button type="button" class="modal-x" @click="closeM"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M6 6l12 12M18 6L6 18" /></svg></button>
      </div>
      <div class="modal-body">
        <div v-if="connErr" class="alert err">{{ connErr }}</div>
        <div class="conns">
          <div v-for="p in PROVIDERS" :key="p.id" class="conn">
            <span class="ic" :style="{ background: PROVIDER_BADGE[p.id]?.bg, color: '#fff' }">{{ PROVIDER_BADGE[p.id]?.txt }}</span>
            <div class="ct">
              <b>{{ p.label }}</b>
              <small v-if="connOf(p.id)?.connected">
                {{ t('settings.connected') }}<template v-if="connOf(p.id)?.meta?.login"> · <span class="mono">@{{ connOf(p.id)?.meta?.login }}</span></template>
              </small>
              <small v-else>{{ t('settings.notConnected') }}</small>
            </div>
            <button v-if="connOf(p.id)?.connected" class="btn sm danger" @click="disconnect(p.id)">{{ t('settings.disconnect') }}</button>
            <!-- GitHub: connect via device flow (satu-klik bila diaktifkan operator) atau PAT. -->
            <template v-else-if="p.id === 'github'">
              <div v-if="!ghDevice" class="gh-connect">
                <button v-if="githubDevice" class="btn sm pri" :disabled="ghWaiting" @click="githubConnect">{{ t('settings.githubConnect') }}</button>
                <div class="input" style="margin-top: 8px"><input v-model="connToken['github']" type="password" :placeholder="t('settings.tokenPlaceholder')" /></div>
                <button class="btn sm" style="margin-top: 6px" :disabled="connBusy === 'github'" @click="connect('github')">{{ t('settings.connect') }}</button>
              </div>
              <div v-else class="alert info" style="flex: 1">
                {{ t('settings.githubDeviceHint') }}
                <div style="font-family: var(--font-mono); font-size: 22px; letter-spacing: .1em; margin: 8px 0">{{ ghDevice.user_code }}</div>
                <a class="btn sm" :href="ghDevice.verification_uri" target="_blank">{{ t('settings.githubOpenVerify') }}</a>
                <span class="mini build" style="margin-left: 10px">{{ t('settings.githubWaiting') }}</span>
              </div>
            </template>
            <!-- Provider AI: baris PAT sejajar. -->
            <template v-else>
              <div class="input conn-tok"><input v-model="connToken[p.id]" type="password" :placeholder="t('settings.tokenPlaceholder')" /></div>
              <button class="btn sm pri" :disabled="connBusy === p.id" @click="connect(p.id)">{{ t('settings.connect') }}</button>
            </template>
          </div>
        </div>
      </div>
    </div>
  </div>

  <!-- API keys -->
  <div class="modal" :class="{ show: modal === 'keys' }" @click.self="closeM">
    <div class="modal-box lg">
      <div class="modal-head">
        <h3>{{ t('settings.apiKeys') }}</h3>
        <button type="button" class="modal-x" @click="closeM"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M6 6l12 12M18 6L6 18" /></svg></button>
      </div>
      <div class="modal-body">
        <div v-if="created" class="alert ok" style="display: block">
          {{ t('settings.apiKeyCreated', { name: created.name }) }}
          <div class="key-reveal" style="margin-top: 10px">
            <code>{{ created.key }}</code>
            <button class="btn sm" style="margin-left: auto" @click="copy(created.key)">{{ t('common.copy') }}</button>
          </div>
        </div>

        <form class="keys-new" @submit.prevent="create">
          <div class="input" style="flex: 1"><input v-model="newName" type="text" :placeholder="t('settings.apiKeyName')" /></div>
          <button class="btn pri" type="submit" :disabled="busy">{{ t('settings.apiKeyCreate') }}</button>
        </form>

        <div v-if="loading" class="spin">{{ t('common.loading') }}</div>
        <table v-else-if="keys.length" class="tbl">
          <thead><tr><th>{{ t('settings.profileName') }}</th><th>{{ t('settings.apiKeyLastUsed') }}</th><th></th></tr></thead>
          <tbody>
            <tr v-for="k in keys" :key="k.id">
              <td><b>{{ k.name || '—' }}</b></td>
              <td class="mono" style="color: var(--muted)">{{ k.last_used_at || t('common.never') }}</td>
              <td style="text-align: right"><button class="btn sm danger" @click="remove(k)">{{ t('common.delete') }}</button></td>
            </tr>
          </tbody>
        </table>
        <p v-else class="sub">{{ t('settings.apiKeyEmpty') }}</p>
      </div>
    </div>
  </div>

  <!-- Tema panel -->
  <div class="modal" :class="{ show: modal === 'theme' }" @click.self="closeM">
    <div class="modal-box">
      <div class="modal-head">
        <h3>{{ t('settings.theme') }}</h3>
        <button type="button" class="modal-x" @click="closeM"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M6 6l12 12M18 6L6 18" /></svg></button>
      </div>
      <div class="modal-body">
        <div class="themes">
          <button v-for="tm in THEMES" :key="tm.id" type="button" class="theme-card" :class="{ on: theme === tm.id }" @click="setTheme(tm.id)">
            <div class="sw" :class="'sw-' + tm.id">Aa</div>
            <div class="tb"><b>{{ tm.name }}</b><small>{{ tm.desc }}</small></div>
          </button>
        </div>
      </div>
      <div class="modal-foot">
        <button type="button" class="btn pri" @click="closeM">{{ t('common.save') }}</button>
      </div>
    </div>
  </div>

  <!-- Bahasa -->
  <div class="modal" :class="{ show: modal === 'lang' }" @click.self="closeM">
    <div class="modal-box">
      <div class="modal-head">
        <h3>{{ t('settings.language') }}</h3>
        <button type="button" class="modal-x" @click="closeM"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M6 6l12 12M18 6L6 18" /></svg></button>
      </div>
      <div class="modal-body">
        <div class="langs">
          <button v-for="l in LOCALES" :key="l.id" type="button" class="lang" :class="{ on: locale === l.id }" @click="pickLocale(l.id)">
            <b>{{ l.native }}</b><small>{{ l.name }}</small>
          </button>
        </div>
      </div>
      <div class="modal-foot">
        <button type="button" class="btn pri" @click="closeM">{{ t('common.save') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Group header (Akun / Preferensi) — memakai .sec dari app.css, hanya
   memberi warna/spasi kecil ala mockup. */
.set-group {
  margin: 4px 4px 10px;
}
.set-group h3 {
  font-weight: 600;
  font-size: 15px;
  color: var(--slate);
  letter-spacing: 0;
}
.set-menu {
  margin-bottom: 22px;
}
.mrow .mval {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* API keys — baris "buat key" di dalam modal */
.keys-new {
  display: flex;
  gap: 10px;
  margin-bottom: 16px;
}

/* Akun terhubung — input token inline di dalam .conn */
.conn-tok {
  flex: 1;
  min-width: 150px;
}
.conn .ct .mono {
  font-family: var(--font-mono);
}

@media (max-width: 640px) {
  .conn {
    flex-wrap: wrap;
  }
  .conn-tok {
    min-width: 0;
    flex-basis: 100%;
  }
}

.gh-connect { display: flex; flex-direction: column; align-items: flex-start; gap: 6px; }
.gh-connect details summary { cursor: pointer; color: var(--muted); font-size: 12.5px; }

/* MFA enrollment */
.mfa-enroll { display: flex; gap: 18px; align-items: flex-start; flex-wrap: wrap; }
.mfa-qr { width: 180px; height: 180px; border-radius: 10px; background: #fff; padding: 8px; border: 1px solid var(--line, #e5e7eb); }
.mfa-manual { flex: 1; min-width: 200px; }
.mfa-manual .key-reveal code { font-family: var(--font-mono); word-break: break-all; }
.recovery-box {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px;
  padding: 14px;
  background: var(--bg);
  border-radius: 10px;
}
.recovery-box code { font-family: var(--font-mono); font-size: 14px; letter-spacing: .04em; }
@media (max-width: 480px) {
  .recovery-box { grid-template-columns: 1fr; }
}
</style>
