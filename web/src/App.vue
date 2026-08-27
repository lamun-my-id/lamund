<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter, RouterLink, RouterView } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuth } from './stores/auth'
import { useScope } from './stores/scope'
import AppDialog from './components/AppDialog.vue'
import logo from './assets/lamun-logo.png'

const { t } = useI18n()
const auth = useAuth()
const scope = useScope()
const route = useRoute()
const router = useRouter()

// Layar publik (login) tampil polos tanpa shell.
const bare = computed(() => route.meta.public === true || !auth.isAuthed)

// Muat daftar tim milik user begitu terautentikasi (untuk scope switcher).
watch(
  () => auth.isAuthed,
  (authed) => {
    if (authed) scope.loadTeams()
  },
  { immediate: true },
)

// Scope switcher: index terpilih di antara options (Personal + tiap tim).
const scopeIndex = computed({
  get: () => scope.options.findIndex((o) => o.type === scope.current.type && o.id === scope.current.id),
  set: (i: number) => scope.setScope(scope.options[i] ?? scope.options[0]),
})

const confirmLogout = ref(false)
function askLogout() { confirmLogout.value = true }
function doLogout() { confirmLogout.value = false; auth.logout(); router.push({ name: 'login' }) }
</script>

<template>
  <AppDialog />
  <RouterView v-if="bare" />

  <div v-else class="app">
    <aside class="side">
      <div class="brand">
        <div class="mk"><img :src="logo" alt="Lamun" /></div>
        <div>
          <b>Lamund</b>
          <small>hosting platform</small>
        </div>
      </div>

      <div class="scope">
        <span class="dot"></span>
        <div class="sc-t">
          <select v-model.number="scopeIndex" :aria-label="t('team.scope')">
            <option v-for="(o, i) in scope.options" :key="o.type + o.id" :value="i">{{ o.type === 'user' ? t('team.personal') : o.label }}</option>
          </select>
          <small>{{ auth.email || auth.displayName }}</small>
        </div>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M8 9l4 4 4-4"/></svg>
      </div>

      <nav class="nav">
        <span class="lbl">{{ t('nav.manage') }}</span>
        <RouterLink :to="{ name: 'overview' }" :class="{ on: route.name === 'overview' }">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/></svg>
          {{ t('nav.overview') }}
        </RouterLink>
        <RouterLink :to="{ name: 'sites' }" :class="{ on: route.name === 'sites' || route.name === 'site-detail' || route.name === 'site-new' || route.name === 'apps' }">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M3 4h18v6H3zM3 14h18v6H3z"/><path d="M7 7h.01M7 17h.01"/></svg>
          {{ t('nav.sites') }}
        </RouterLink>
        <RouterLink :to="{ name: 'domain' }" :class="{ on: route.name === 'domain' }">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18"/></svg>
          {{ t('nav.domain') }}
        </RouterLink>
        <RouterLink :to="{ name: 'certs' }" :class="{ on: route.name === 'certs' }">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M12 3l7 3v6c0 4-3 7-7 9-4-2-7-5-7-9V6z"/></svg>
          {{ t('nav.certs') }}
        </RouterLink>

        <span class="lbl">{{ t('nav.account') }}</span>
        <RouterLink :to="{ name: 'team' }" :class="{ on: route.name === 'team' || route.name === 'users' }">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="9" cy="8" r="3"/><path d="M3 20a6 6 0 0 1 12 0"/><path d="M16 6a3 3 0 0 1 0 6"/></svg>
          {{ t('nav.team') }}
        </RouterLink>
        <RouterLink :to="{ name: 'settings' }" :class="{ on: route.name === 'settings' }">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3"/></svg>
          {{ t('nav.settings') }}
        </RouterLink>
      </nav>

      <div class="foot">
        <div class="ava">{{ auth.initials }}</div>
        <div class="who">{{ auth.displayName }}<small>{{ auth.isAdmin ? t('nav.admin') : t('nav.user') }}</small></div>
        <div class="foot-actions">
          <RouterLink class="ficon" :to="{ name: 'settings' }" :title="t('nav.settings')" aria-label="settings">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3"/></svg>
          </RouterLink>
          <button class="ficon" :title="t('nav.logout')" aria-label="logout" @click="askLogout">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9"/></svg>
          </button>
        </div>
      </div>
    </aside>

    <main class="main">
      <RouterView />
    </main>

    <div class="modal" :class="{ show: confirmLogout }" @click.self="confirmLogout = false">
      <div class="modal-box">
        <div class="modal-head"><h3>{{ t('nav.logoutConfirmTitle') }}</h3><button class="modal-x" @click="confirmLogout = false">✕</button></div>
        <div class="modal-body">{{ t('nav.logoutConfirmBody') }}</div>
        <div class="modal-foot">
          <button class="btn" @click="confirmLogout = false">{{ t('common.no') }}</button>
          <button class="btn pri" @click="doLogout">{{ t('common.yes') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.foot-actions { display: flex; gap: 4px; margin-left: auto; }
.ficon {
  width: 32px; height: 32px;
  display: grid; place-items: center;
  border-radius: var(--r-btn);
  border: 1px solid transparent;
  background: transparent;
  color: var(--muted);
  cursor: pointer;
  text-decoration: none;
  transition: background .15s, border-color .15s, color .15s;
}
.ficon:hover { background: var(--line-2); border-color: var(--brand-l); color: var(--brand); }
.ficon svg { width: 16px; height: 16px; stroke-width: 2; }
</style>
