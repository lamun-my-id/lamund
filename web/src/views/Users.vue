<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { confirmDialog } from '../lib/dialog'
import { api, ApiError, type User } from '../lib/api'
import { useAuth } from '../stores/auth'

const { t } = useI18n()
const auth = useAuth()

const users = ref<User[]>([])
const loading = ref(true)
const error = ref('')

// toolbar / filter state
const q = ref('')
const roleFilter = ref('all')
const statusFilter = ref('all')

// per-baris menu terbuka (id user) & modal buat
const openMenu = ref<number | null>(null)
const showCreate = ref(false)

// form buat pengguna
const nu = ref({ username: '', email: '', password: '', role: 'member', max_sites: 1 })
const formErr = ref('')
const busy = ref(false)

const filtered = computed(() => {
  const term = q.value.trim().toLowerCase()
  return users.value.filter((u) => {
    if (term && !u.username.toLowerCase().includes(term) && !(u.email || '').toLowerCase().includes(term)) return false
    if (roleFilter.value !== 'all' && u.role !== roleFilter.value) return false
    if (statusFilter.value === 'active' && u.disabled) return false
    if (statusFilter.value === 'disabled' && !u.disabled) return false
    return true
  })
})

function initial(u: User): string {
  return (u.username || '?').charAt(0).toUpperCase()
}

function roleClass(role: string): string {
  if (role === 'superadmin') return 'role-super'
  if (role === 'team_manager') return 'role-mgr'
  return 'role-member'
}

function roleLabel(role: string): string {
  if (role === 'superadmin') return t('users.roleSuper')
  if (role === 'team_manager') return t('users.roleManager')
  return t('users.roleMember')
}

async function load() {
  loading.value = true
  try {
    users.value = await api.listUsers()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

function toggleMenu(id: number) {
  openMenu.value = openMenu.value === id ? null : id
}
function closeMenu() {
  openMenu.value = null
}

async function create() {
  formErr.value = ''
  busy.value = true
  try {
    await api.createUser({
      username: nu.value.username.trim(),
      password: nu.value.password,
      role: nu.value.role,
      max_sites: nu.value.max_sites,
    })
    showCreate.value = false
    nu.value = { username: '', email: '', password: '', role: 'member', max_sites: 1 }
    await load()
  } catch (e) {
    formErr.value = e instanceof ApiError ? e.message : t('users.createFailed')
  } finally {
    busy.value = false
  }
}

async function toggleStatus(u: User) {
  closeMenu()
  try {
    await api.setUserStatus(u.id, !u.disabled)
    await load()
  } catch (e) {
    error.value = (e as Error).message
  }
}

// Modal kuota: admin setel batas per-user (0 = pakai default sistem).
interface QuotaForm { user: User; maxSites: number; maxTeams: number; maxApps: number; maxMemory: number; maxCpu: number }
const quotaModal = ref<QuotaForm | null>(null)
const quotaBusy = ref(false)
const quotaErr = ref('')

function setQuota(u: User) {
  closeMenu()
  quotaErr.value = ''
  quotaModal.value = {
    user: u,
    maxSites: u.max_sites ?? 0,
    maxTeams: u.max_teams ?? 0,
    maxApps: u.max_apps ?? 0,
    maxMemory: u.max_memory_mb ?? 0,
    maxCpu: u.max_cpu_percent ?? 0,
  }
}

async function saveQuota() {
  const q = quotaModal.value
  if (!q) return
  quotaBusy.value = true
  quotaErr.value = ''
  try {
    await api.setQuota(q.user.id, {
      max_sites: Math.max(0, q.maxSites || 0),
      max_teams: Math.max(0, q.maxTeams || 0),
      max_apps: Math.max(0, q.maxApps || 0),
      max_memory_mb: Math.max(0, q.maxMemory || 0),
      max_cpu_percent: Math.max(0, q.maxCpu || 0),
      max_storage_mb: 0,
      max_bandwidth_gb: 0,
    })
    quotaModal.value = null
    await load()
  } catch (e) {
    quotaErr.value = (e as Error).message
  } finally {
    quotaBusy.value = false
  }
}

async function resetMfa(u: User) {
  closeMenu()
  try {
    await api.adminResetMfa(u.id)
    await load()
  } catch (e) {
    error.value = (e as Error).message
  }
}

async function removeUser(u: User) {
  closeMenu()
  if (u.id === auth.userId) {
    error.value = t('users.cantDeleteSelf')
    return
  }
  if (!(await confirmDialog({ message: t('users.confirmDelete', { name: u.username }) }))) return
  try {
    await api.deleteUser(u.id)
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) {
      error.value = t('users.hasResources')
    } else {
      error.value = (e as Error).message
    }
  }
}

onMounted(() => {
  load()
  document.addEventListener('click', closeMenu)
})
onBeforeUnmount(() => document.removeEventListener('click', closeMenu))
</script>

<template>
  <div class="page-head">
    <div>
      <div class="h1">{{ t('users.title') }}</div>
      <div class="sub">
        {{ t('users.subtitle') }} <b>{{ users.length }}</b>
      </div>
    </div>
    <button v-if="auth.isSuperadmin" class="btn pri" @click="showCreate = true">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M12 5v14M5 12h14" /></svg>
      {{ t('users.add') }}
    </button>
  </div>

  <div v-if="error" class="alert err">{{ error }}</div>

  <div class="toolbar">
    <div class="search">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="11" cy="11" r="7" /><path d="m21 21-4.3-4.3" /></svg>
      <input v-model="q" type="text" :placeholder="t('users.searchPlaceholder')" />
    </div>
    <select v-model="roleFilter" class="mini" :aria-label="t('users.filterRole')">
      <option value="all">{{ t('users.allRoles') }}</option>
      <option value="superadmin">{{ t('users.roleSuper') }}</option>
      <option value="team_manager">{{ t('users.roleManager') }}</option>
      <option value="member">{{ t('users.roleMember') }}</option>
    </select>
    <select v-model="statusFilter" class="mini" :aria-label="t('users.filterStatus')">
      <option value="all">{{ t('users.allStatus') }}</option>
      <option value="active">{{ t('users.statusActive') }}</option>
      <option value="disabled">{{ t('users.statusDisabled') }}</option>
    </select>
  </div>

  <div v-if="loading" class="spin">{{ t('common.loading') }}</div>
  <div v-else class="card tbl-card">
    <table class="tbl">
      <thead>
        <tr>
          <th>{{ t('users.colUser') }}</th>
          <th>{{ t('users.colRole') }}</th>
          <th>{{ t('users.colMFA') }}</th>
          <th>{{ t('users.colQuota') }}</th>
          <th>{{ t('users.colStatus') }}</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="u in filtered" :key="u.id">
          <td>
            <div class="u-cell">
              <div class="u-av">{{ initial(u) }}</div>
              <div>
                <b>{{ u.username }}</b>
                <small v-if="u.email">{{ u.email }}</small>
              </div>
            </div>
          </td>
          <td><span class="tag" :class="roleClass(u.role)">{{ roleLabel(u.role) }}</span></td>
          <td>
            <span v-if="u.mfa_enabled" class="tag on">{{ t('users.mfaOn') }}</span>
            <span v-else class="tag off">{{ t('users.mfaOff') }}</span>
          </td>
          <td>
            <span class="tag mono">{{ (u.max_sites ?? 0) === 0 ? t('users.unlimited') : u.max_sites }}</span>
          </td>
          <td>
            <span v-if="u.disabled" class="tag off">{{ t('users.statusDisabled') }}</span>
            <span v-else class="tag on">{{ t('users.statusActive') }}</span>
          </td>
          <td class="act-cell">
            <div v-if="auth.isSuperadmin" class="row-wrap" @click.stop>
              <button class="menu-btn" @click="toggleMenu(u.id)">⋯</button>
              <div v-if="openMenu === u.id" class="rowmenu">
                <button @click="toggleStatus(u)">{{ u.disabled ? t('users.enable') : t('users.disable') }}</button>
                <button @click="setQuota(u)">{{ t('users.setQuota') }}</button>
                <button v-if="u.mfa_enabled" @click="resetMfa(u)">{{ t('users.resetMfa') }}</button>
                <button v-if="u.id !== auth.userId" class="danger" @click="removeUser(u)">{{ t('users.deleteUser') }}</button>
              </div>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>

  <!-- Modal: Tambah pengguna -->
  <div class="modal" :class="{ show: showCreate }" @click.self="showCreate = false">
    <div class="modal-box">
      <div class="modal-head">
        <h3>{{ t('users.createTitle') }}</h3>
        <button class="modal-x" @click="showCreate = false">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M18 6 6 18M6 6l12 12" /></svg>
        </button>
      </div>
      <form @submit.prevent="create">
        <div class="modal-body">
          <div v-if="formErr" class="alert err" style="margin-bottom: 12px">{{ formErr }}</div>
          <div class="field">
            <label>{{ t('users.username') }}</label>
            <div class="input"><input v-model="nu.username" type="text" required autofocus /></div>
          </div>
          <div class="field">
            <label>{{ t('users.email') }}</label>
            <div class="input"><input v-model="nu.email" type="email" /></div>
          </div>
          <div class="field">
            <label>{{ t('users.password') }}</label>
            <div class="input"><input v-model="nu.password" type="password" required /></div>
          </div>
          <div class="field">
            <label>{{ t('users.role') }}</label>
            <div class="input">
              <select v-model="nu.role">
                <option value="member">{{ t('users.roleMember') }}</option>
                <option value="team_manager">{{ t('users.roleManager') }}</option>
                <option value="superadmin">{{ t('users.roleSuper') }}</option>
              </select>
            </div>
          </div>
          <div class="field">
            <label>{{ t('users.quota') }}</label>
            <div class="input"><input v-model.number="nu.max_sites" type="number" min="0" /></div>
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn" type="button" @click="showCreate = false">{{ t('common.cancel') }}</button>
          <button class="btn pri" type="submit" :disabled="busy">
            {{ busy ? t('common.saving') : t('users.create') }}
          </button>
        </div>
      </form>
    </div>
  </div>

  <!-- Modal: setel kuota (situs + tim) -->
  <div class="modal" :class="{ show: quotaModal !== null }" @click.self="quotaModal = null">
    <div v-if="quotaModal" class="modal-box">
      <div class="modal-head">
        <h3>{{ t('users.quotaTitle') }}</h3>
        <button class="modal-x" @click="quotaModal = null">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M18 6 6 18M6 6l12 12" /></svg>
        </button>
      </div>
      <form @submit.prevent="saveQuota">
        <div class="modal-body">
          <div v-if="quotaErr" class="alert err" style="margin-bottom: 12px">{{ quotaErr }}</div>
          <p class="quota-sub">{{ t('users.quotaFor', { name: quotaModal.user.username }) }}</p>
          <div class="field">
            <label>{{ t('users.maxSites') }}</label>
            <div class="input"><input v-model.number="quotaModal.maxSites" type="number" min="0" /></div>
          </div>
          <div class="field">
            <label>{{ t('users.maxTeams') }}</label>
            <div class="input"><input v-model.number="quotaModal.maxTeams" type="number" min="0" /></div>
          </div>
          <div class="field">
            <label>{{ t('users.maxApps') }}</label>
            <div class="input"><input v-model.number="quotaModal.maxApps" type="number" min="0" /></div>
          </div>
          <div class="field">
            <label>{{ t('users.maxMemory') }}</label>
            <div class="input"><input v-model.number="quotaModal.maxMemory" type="number" min="0" step="128" placeholder="512" /></div>
          </div>
          <div class="field">
            <label>{{ t('users.maxCpu') }}</label>
            <div class="input"><input v-model.number="quotaModal.maxCpu" type="number" min="0" step="10" placeholder="50" /></div>
          </div>
          <p class="quota-hint">{{ t('users.quotaHint') }}</p>
        </div>
        <div class="modal-foot">
          <button class="btn" type="button" @click="quotaModal = null">{{ t('common.cancel') }}</button>
          <button class="btn pri" type="submit" :disabled="quotaBusy">
            {{ quotaBusy ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<style scoped>
.page-head { display: flex; align-items: flex-end; justify-content: space-between; gap: 20px; flex-wrap: wrap; }

.toolbar { display: flex; gap: 10px; align-items: center; margin-bottom: 16px; flex-wrap: wrap; }
.search {
  flex: 1;
  max-width: 320px;
  display: flex;
  align-items: center;
  gap: 8px;
  background: var(--card);
  border: 1px solid var(--line);
  border-radius: var(--r-input);
  padding: 10px 12px;
}
.search input { border: 0; outline: 0; background: 0; font: inherit; font-size: 14px; width: 100%; color: var(--ink); }
.search svg { width: 16px; height: 16px; color: var(--faint); flex-shrink: 0; stroke-width: 2; }
select.mini {
  font: inherit;
  font-size: 13px;
  padding: 8px 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--card);
  color: var(--slate);
  cursor: pointer;
}

.tbl-card { padding: 0; overflow: visible; margin-bottom: 26px; }

.u-cell { display: flex; align-items: center; gap: 11px; }
.u-av {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  background: linear-gradient(135deg, var(--brand-l), var(--brand));
  color: #fff;
  display: grid;
  place-items: center;
  font-weight: 700;
  font-size: 14px;
  flex-shrink: 0;
}
.u-cell b { display: block; font-size: 14px; }
.u-cell small { color: var(--muted); font-size: 12px; }

.tag.role-super { background: #efe7ff; color: #5b34c6; }
.tag.role-mgr { background: #e4f4ff; color: #1f74b8; }
.tag.role-member { background: var(--line-2); color: var(--slate); }

.act-cell { text-align: right; }
.row-wrap { position: relative; display: inline-block; }
.menu-btn {
  border: 1px solid var(--line);
  background: var(--card);
  border-radius: 9px;
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  cursor: pointer;
  color: var(--muted);
  font-size: 18px;
  line-height: 1;
}
.menu-btn:hover { border-color: var(--brand-l); color: var(--brand); }
.rowmenu {
  position: absolute;
  right: 0;
  top: calc(100% + 6px);
  z-index: 20;
  min-width: 170px;
  background: var(--card);
  border: 1px solid var(--line);
  border-radius: 12px;
  box-shadow: 0 18px 44px -18px rgba(25, 21, 39, 0.4);
  padding: 6px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.rowmenu button {
  text-align: left;
  border: 0;
  background: none;
  font: inherit;
  font-size: 13px;
  font-weight: 500;
  color: var(--slate);
  padding: 9px 11px;
  border-radius: 8px;
  cursor: pointer;
}
.rowmenu button:hover { background: var(--line-2); color: var(--brand); }
.rowmenu button.danger { color: var(--danger); }
.rowmenu button.danger:hover { background: #fdf2f5; color: var(--danger); }

.modal-box .field .input select { border: 0; outline: 0; background: 0; font: inherit; font-size: 14px; width: 100%; color: var(--ink); cursor: pointer; }

.quota-sub { margin: 0 0 14px; font-size: 14px; }
.quota-hint { margin: 4px 0 0; font-size: 12px; color: var(--muted); }
</style>
