<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { confirmDialog } from '../lib/dialog'
import { api, type Team, type TeamMember } from '../lib/api'
import { useScope } from '../stores/scope'
import { useAuth } from '../stores/auth'

const { t } = useI18n()
const scope = useScope()
const auth = useAuth()

const teams = ref<Team[]>([])
const loading = ref(true)
const error = ref('')

// form buat tim
const newTeam = ref('')
const creating = ref(false)

// modal state
const showCreate = ref(false)
const openTeam = ref<Team | null>(null)
const members = ref<TeamMember[]>([])
const membersLoaded = ref(false)
const memBusy = ref(false)
// Invite HANYA by username (user yang sudah punya akun). Bikin akun = admin-only,
// jadi tak ada jalur email-invite / invite-link / create-user dari sini.
const invite = ref({ username: '', role: 'member' })

// Pencarian user ala GitHub: ketik min 4 huruf → search live (toleransi typo,
// exact di atas). Pilih dari dropdown → set invite.username.
const userQuery = ref('')
const userResults = ref<{ username: string; name: string }[]>([])
const searchBusy = ref(false)
const showResults = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | null = null

function onUserSearch() {
  invite.value.username = '' // reset pilihan saat mengetik ulang
  const q = userQuery.value.trim()
  if (searchTimer) clearTimeout(searchTimer)
  if (q.length < 4) {
    userResults.value = []
    showResults.value = false
    return
  }
  searchBusy.value = true
  showResults.value = true
  searchTimer = setTimeout(async () => {
    try {
      userResults.value = await api.searchUsers(q)
    } catch {
      userResults.value = []
    } finally {
      searchBusy.value = false
    }
  }, 300)
}

function pickUser(u: { username: string; name: string }) {
  invite.value.username = u.username
  userQuery.value = u.username
  showResults.value = false
}

// Apakah caller punya hak kelola tim (owner/admin) atau superadmin
function canManageTeam(team: Team): boolean {
  if (auth.isSuperadmin) return true
  return team.role === 'owner' || team.role === 'admin'
}

async function load() {
  loading.value = true
  try {
    teams.value = await api.listTeams()
    await scope.loadTeams()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function create() {
  if (newTeam.value.trim().length < 2) return
  creating.value = true
  error.value = ''
  try {
    await api.createTeam(newTeam.value.trim())
    newTeam.value = ''
    showCreate.value = false
    await load()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    creating.value = false
  }
}

async function openManage(team: Team) {
  openTeam.value = team
  members.value = []
  membersLoaded.value = false
  try {
    const detail = await api.getTeam(team.id)
    members.value = detail.members
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    membersLoaded.value = true
  }
}

function closeManage() {
  openTeam.value = null
  members.value = []
  membersLoaded.value = false
}

async function changeRole(team: Team, m: TeamMember, newRole: string) {
  try {
    await api.addMember(team.id, m.username, newRole)
    const detail = await api.getTeam(team.id)
    members.value = detail.members
  } catch (e) {
    error.value = (e as Error).message
  }
}

async function addMember(team: Team) {
  if (!invite.value.username.trim()) return
  memBusy.value = true
  try {
    await api.addMember(team.id, invite.value.username.trim(), invite.value.role)
    invite.value.username = ''
    userQuery.value = ''
    userResults.value = []
    showResults.value = false
    const detail = await api.getTeam(team.id)
    members.value = detail.members
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    memBusy.value = false
  }
}

async function removeMember(team: Team, m: TeamMember) {
  if (!(await confirmDialog({ message: `${t('common.delete')} ${m.username}?` }))) return
  try {
    await api.removeMember(team.id, m.user_id)
    members.value = members.value.filter((x) => x.user_id !== m.user_id)
  } catch (e) {
    error.value = (e as Error).message
  }
}

async function remove(team: Team) {
  if (!(await confirmDialog({ message: `${t('common.delete')} "${team.name}"?` }))) return
  try {
    await api.deleteTeam(team.id)
    closeManage()
    await load()
  } catch (e) {
    error.value = (e as Error).message
  }
}

// Card helpers
const AVATAR_GRADIENTS = [
  'linear-gradient(135deg,var(--purple-l),var(--purple))',
  'linear-gradient(135deg,#5ab0f0,#1f74b8)',
  'linear-gradient(135deg,#b3aecb,#847e9c)',
]
const AVATAR_LETTERS = ['A', 'B', 'C']

function avatarStyle(i: number): string {
  return `background:${AVATAR_GRADIENTS[(i - 1) % AVATAR_GRADIENTS.length]}`
}

function avatarLetter(i: number): string {
  return AVATAR_LETTERS[(i - 1) % AVATAR_LETTERS.length]
}

function roleClass(role: string): string {
  if (role === 'owner') return 'role-owner'
  if (role === 'admin') return 'role-admin'
  if (role === 'member') return 'role-member'
  return 't-app'
}

onMounted(load)
</script>

<template>
  <div class="page-head">
    <div>
      <div class="h1">{{ t('team.title') }}</div>
      <div class="sub">{{ t('team.subtitle') }}</div>
    </div>
    <button v-if="auth.canCreateTeams" class="btn pri" @click="showCreate = true">
      {{ t('team.newTeam') }}
    </button>
  </div>

  <div v-if="error" class="alert err">{{ error }}</div>

  <div v-if="loading" class="spin">{{ t('common.loading') }}</div>

  <div v-else-if="teams.length" class="team-grid">
    <div v-for="team in teams" :key="team.id" class="card team-card">
      <div class="tc-top">
        <div>
          <h3>{{ team.name }}</h3>
          <span class="tc-slug">@{{ team.slug }}</span>
        </div>
        <span v-if="team.role" :class="['tag', roleClass(team.role)]">{{ team.role }}</span>
      </div>
      <div v-if="team.member_count != null" class="tc-meta">
        <span class="av-row">
          <span
            v-for="i in Math.min(team.member_count, 3)"
            :key="i"
            class="u-av"
            :style="avatarStyle(i)"
          >{{ avatarLetter(i) }}</span>
          <span v-if="team.member_count > 3" class="u-av av-more">+{{ team.member_count - 3 }}</span>
        </span>
        <span>{{ t('team.memberCount', { n: team.member_count }) }}</span>
      </div>
      <div class="tc-actions">
        <button class="btn sm pri" @click="openManage(team)">{{ canManageTeam(team) ? t('common.manage') : t('team.viewMembers') }}</button>
      </div>
    </div>
  </div>

  <div v-else class="card empty">
    <h3>{{ t('team.teams') }}</h3>
    <p>{{ t('team.emptyTeam') }}</p>
    <button v-if="auth.canCreateTeams" class="btn pri" style="margin-top: 14px" @click="showCreate = true">
      {{ t('team.newTeam') }}
    </button>
  </div>

  <!-- Modal: Buat tim baru -->
  <div class="modal" :class="{ show: showCreate }" @click.self="showCreate = false">
    <div class="modal-box">
      <div class="modal-head">
        <h3>{{ t('team.newTeam') }}</h3>
        <button class="modal-x" @click="showCreate = false">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M18 6 6 18M6 6l12 12"/></svg>
        </button>
      </div>
      <form @submit.prevent="create">
        <div class="modal-body">
          <div class="field">
            <label>{{ t('team.teamName') }}</label>
            <div class="input"><input v-model="newTeam" type="text" :placeholder="t('team.teamName')" autofocus /></div>
          </div>
          <div v-if="error" class="alert err" style="margin-top: 8px">{{ error }}</div>
        </div>
        <div class="modal-foot">
          <button class="btn" type="button" @click="showCreate = false">{{ t('common.cancel') }}</button>
          <button class="btn pri" type="submit" :disabled="creating || newTeam.trim().length < 2">
            {{ creating ? t('common.saving') : t('common.create') }}
          </button>
        </div>
      </form>
    </div>
  </div>

  <!-- Modal: Kelola tim -->
  <div class="modal" :class="{ show: openTeam !== null }" @click.self="closeManage">
    <div v-if="openTeam" class="modal-box lg">
      <div class="modal-head">
        <h3>{{ openTeam.name }}</h3>
        <button class="modal-x" @click="closeManage">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M18 6 6 18M6 6l12 12"/></svg>
        </button>
      </div>

      <div class="modal-body">
        <!-- Daftar anggota -->
        <div class="field-label">{{ t('team.members') }}</div>
        <div class="mem-list">
          <div v-for="m in members" :key="m.user_id" class="mem-row">
            <div class="mem-info">
              <b>{{ m.name || m.username }}</b>
              <span class="slug">@{{ m.username }}</span>
            </div>
            <select
              v-if="canManageTeam(openTeam)"
              class="input role-sel"
              :value="m.role"
              :disabled="m.role === 'owner' && !auth.isSuperadmin"
              @change="changeRole(openTeam, m, ($event.target as HTMLSelectElement).value)"
            >
              <option value="member">{{ t('team.member') }}</option>
              <option value="admin">{{ t('team.admin') }}</option>
              <option value="owner">{{ t('team.owner') }}</option>
            </select>
            <span v-else class="tag t-app">{{ m.role }}</span>
            <button
              v-if="canManageTeam(openTeam) && m.role !== 'owner'"
              class="btn sm danger"
              @click="removeMember(openTeam, m)"
            >✕</button>
          </div>
          <div v-if="!membersLoaded" class="mem-empty">{{ t('common.loading') }}</div>
          <div v-else-if="membersLoaded && !members.length" class="mem-empty">{{ t('team.noMembers') }}</div>
        </div>

        <!-- Kontrol kelola: hanya untuk owner/admin/superadmin -->
        <template v-if="canManageTeam(openTeam)">
          <!-- Tambah member: cari user (ala GitHub) → pilih → add -->
          <div class="field-label" style="margin-top: 18px">{{ t('team.invite') }}</div>
          <form class="invite-row" @submit.prevent="addMember(openTeam)">
            <div class="usearch">
              <input class="input" v-model="userQuery" type="text" :placeholder="t('team.searchUser')" autocomplete="off" @input="onUserSearch" @focus="onUserSearch" />
              <div v-if="showResults" class="usearch-pop">
                <div v-if="searchBusy" class="usearch-msg">{{ t('common.loading') }}</div>
                <button
                  v-for="u in userResults"
                  :key="u.username"
                  type="button"
                  class="usearch-item"
                  :class="{ on: invite.username === u.username }"
                  @click="pickUser(u)"
                >
                  <span class="usearch-un">@{{ u.username }}</span>
                  <span v-if="u.name" class="usearch-nm">{{ u.name }}</span>
                </button>
                <div v-if="!searchBusy && !userResults.length" class="usearch-msg">{{ t('team.noUserFound') }}</div>
              </div>
            </div>
            <select v-model="invite.role" class="input" style="width: auto; padding: 8px">
              <option value="member">{{ t('team.member') }}</option>
              <option value="admin">{{ t('team.admin') }}</option>
              <option v-if="auth.isSuperadmin || (openTeam && openTeam.role === 'owner')" value="owner">{{ t('team.owner') }}</option>
            </select>
            <button class="btn pri" type="submit" :disabled="memBusy || !invite.username">{{ t('common.add') }}</button>
          </form>
          <p class="invite-hint">{{ invite.username ? t('team.willInvite', { u: invite.username }) : t('team.inviteHint') }}</p>
        </template>
      </div>

      <div class="modal-foot">
        <button v-if="canManageTeam(openTeam)" class="btn danger" style="margin-right: auto" @click="remove(openTeam)">
          {{ t('common.delete') }}
        </button>
        <button class="btn" @click="closeManage">{{ t('common.close') }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }

.team-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(290px, 1fr)); gap: 16px; }

.team-card { padding: 18px 20px; }

/* card top: name+slug left, role badge right */
.tc-top { display: flex; align-items: flex-start; justify-content: space-between; gap: 10px; }
.tc-top h3 { margin: 0; font-family: var(--font-display, 'Bricolage Grotesque', system-ui, sans-serif); font-weight: 700; font-size: 18px; line-height: 1.2; }
.tc-slug { color: var(--muted); font-size: 12px; font-family: var(--font-mono, monospace); display: block; margin-top: 2px; }

/* meta row: avatar stack + member count */
.tc-meta { display: flex; align-items: center; gap: 14px; margin-top: 14px; color: var(--muted); font-size: 13px; }
.av-row { display: flex; align-items: center; }
.av-row .u-av { width: 26px; height: 26px; font-size: 11px; border-radius: 8px; margin-left: -6px; border: 2px solid var(--card, #fff); color: #fff; display: grid; place-items: center; font-weight: 700; }
.av-row .u-av:first-child { margin-left: 0; }
.av-row .av-more { background: var(--line-2, #f4f1fb); color: var(--muted); font-size: 10px; font-weight: 700; }

/* role badge colour overrides */
.tag.role-owner { background: #fff0e6; color: #c2620f; }
.tag.role-admin  { background: #e4f4ff; color: #1f74b8; }
.tag.role-member { background: var(--line-2, #f4f1fb); color: var(--slate, #3d3a52); }

/* action row */
.tc-actions { margin-top: 16px; display: flex; gap: 8px; }

/* modal anggota */
.field-label { font-size: 12px; font-weight: 600; text-transform: uppercase; letter-spacing: .5px; color: var(--muted); margin-bottom: 8px; }
.mem-list { display: flex; flex-direction: column; gap: 2px; border: 1px solid var(--line); border-radius: var(--r-input); overflow: hidden; }
.mem-row { display: flex; align-items: center; gap: 10px; padding: 10px 14px; border-bottom: 1px solid var(--line-2); }
.mem-row:last-child { border-bottom: 0; }
.mem-info { flex: 1; min-width: 0; }
.mem-info b { display: block; font-size: 14px; }
.mem-info .slug { font-size: 12px; }
.mem-empty { padding: 14px; color: var(--muted); font-size: 13px; text-align: center; }

.role-sel { width: auto; padding: 5px 8px; font-size: 13px; }

.invite-row { display: flex; gap: 8px; align-items: flex-start; }
.invite-hint { color: var(--muted); font-size: 12px; margin: 8px 0 0; }

/* combobox pencarian user */
.usearch { position: relative; flex: 1; }
.usearch .input { width: 100%; }
.usearch-pop { position: absolute; top: calc(100% + 4px); left: 0; right: 0; z-index: 20; background: var(--card); border: 1px solid var(--line); border-radius: var(--r-input); box-shadow: 0 10px 30px rgba(0,0,0,.12); max-height: 240px; overflow-y: auto; padding: 4px; }
.usearch-item { display: flex; flex-direction: column; gap: 1px; width: 100%; text-align: left; padding: 7px 10px; border: 0; background: transparent; border-radius: 8px; cursor: pointer; }
.usearch-item:hover, .usearch-item.on { background: var(--line-2); }
.usearch-un { font-size: 13px; font-weight: 600; font-family: var(--font-mono, monospace); }
.usearch-nm { font-size: 12px; color: var(--muted); }
.usearch-msg { padding: 8px 10px; font-size: 12px; color: var(--muted); }
</style>
