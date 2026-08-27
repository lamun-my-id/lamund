import { createRouter, createWebHistory } from 'vue-router'
import { useAuth } from './stores/auth'
import { api } from './lib/api'

const routes = [
  { path: '/login', name: 'login', component: () => import('./views/Login.vue'), meta: { public: true } },
  { path: '/setup', name: 'setup', component: () => import('./views/Setup.vue'), meta: { public: true } },
  { path: '/forgot', name: 'forgot', component: () => import('./views/Forgot.vue'), meta: { public: true } },
  { path: '/reset/:token', name: 'reset', component: () => import('./views/Reset.vue'), meta: { public: true }, props: true },
  { path: '/verify/:token', name: 'verify', component: () => import('./views/Verify.vue'), meta: { public: true }, props: true },
  { path: '/privacy', name: 'privacy', component: () => import('./views/Legal.vue'), meta: { public: true }, props: { kind: 'privacy' } },
  { path: '/terms', name: 'terms', component: () => import('./views/Legal.vue'), meta: { public: true }, props: { kind: 'terms' } },
  { path: '/', redirect: '/overview' },
  { path: '/overview', name: 'overview', component: () => import('./views/Overview.vue') },
  { path: '/sites', name: 'sites', component: () => import('./views/Sites.vue') },
  { path: '/domain', name: 'domain', component: () => import('./views/Domain.vue') },
  { path: '/domain/:domain', name: 'domain-zone', component: () => import('./views/DomainZone.vue'), props: true },
  { path: '/sites/new', name: 'site-new', component: () => import('./views/New.vue') },
  { path: '/sites/:domain/edit', name: 'site-edit', component: () => import('./views/SiteForm.vue'), props: true },
  { path: '/sites/:domain', name: 'site-detail', component: () => import('./views/SiteDetail.vue'), props: true },
  { path: '/apps', name: 'apps', component: () => import('./views/Apps.vue') },
  { path: '/certs', name: 'certs', component: () => import('./views/Certificates.vue') },
  { path: '/team', name: 'team', component: () => import('./views/TeamUsers.vue') },
  { path: '/join/:token', name: 'join', component: () => import('./views/Join.vue') },
  { path: '/users', name: 'users', component: () => import('./views/Users.vue'), meta: { admin: true } },
  { path: '/settings', name: 'settings', component: () => import('./views/Settings.vue') },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
})

// needsSetup di-cache: hanya di-query saat belum login (instance mungkin kosong).
// Sekali tahu instance sudah disiapkan, tak perlu tanya lagi sesi ini.
let setupChecked = false
let needsSetup = false
async function checkSetup(): Promise<boolean> {
  if (setupChecked) return needsSetup
  try {
    needsSetup = (await api.setupStatus()).needs_setup
  } catch {
    needsSetup = false
  }
  setupChecked = true
  return needsSetup
}

router.beforeEach(async (to) => {
  const auth = useAuth()

  // First-run: instance tanpa user → paksa wizard setup (kecuali sudah di sana).
  if (!auth.isAuthed && to.name !== 'setup') {
    if (await checkSetup()) return { name: 'setup' }
  }
  // Sudah disiapkan / sudah login → jangan biarkan mampir ke setup.
  if (to.name === 'setup') {
    if (auth.isAuthed) return { name: 'sites' }
    if (!(await checkSetup())) return { name: 'login' }
    return true
  }

  if (!to.meta.public && !auth.isAuthed) {
    return { name: 'login', query: { next: to.fullPath } }
  }
  if (to.meta.admin && !auth.isAdmin) {
    return { name: 'sites' }
  }
  if (to.name === 'login' && auth.isAuthed) {
    return { name: 'sites' }
  }
})
