import { defineStore } from 'pinia'
import { api, getToken, setToken } from '../lib/api'
import { applyServerLocale } from '../i18n'
import { applyTheme, type Theme } from '../lib/theme'

interface State {
  token: string | null
  role: string
  userId: number | null
  username: string
  name: string
  email: string
}

export const useAuth = defineStore('auth', {
  state: (): State => ({ token: getToken(), role: '', userId: null, username: '', name: '', email: '' }),
  getters: {
    isAuthed: (s) => !!s.token,
    isAdmin: (s) => s.role === 'superadmin',
    isSuperadmin: (s) => s.role === 'superadmin',
    // Semua user login boleh bikin tim sendiri (jadi owner). Bikin AKUN tetap admin-only.
    canCreateTeams: (s) => !!s.token,
    // Inisial untuk avatar: pakai nama bila ada, jatuh ke username/role.
    initials: (s) => (s.name || s.username || (s.role === 'superadmin' ? 'A' : 'U')).slice(0, 1).toUpperCase(),
    displayName: (s) => s.name || s.username || (s.role === 'superadmin' ? 'Admin' : 'User'),
  },
  actions: {
    // login: bila MFA aktif, server balas {mfa_required, pending} → kembalikan
    // penanda agar Login.vue tampilkan langkah kedua. Bila tidak, selesaikan login.
    async login(username: string, password: string): Promise<{ mfaRequired: true; pending: string } | void> {
      const r = await api.login(username, password)
      if ('mfa_required' in r) {
        return { mfaRequired: true, pending: r.pending }
      }
      this.token = r.token
      this.role = r.role
      setToken(r.token)
      await this.hydrate()
    },
    // completeMFA: langkah kedua — verifikasi kode TOTP/recovery lalu simpan token.
    async completeMFA(pending: string, code: string) {
      const r = await api.loginMFA(pending, code)
      this.token = r.token
      this.role = r.role
      setToken(r.token)
      await this.hydrate()
    },
    // adopt: simpan JWT dari login-with-github (fragment #gh_token) lalu hydrate.
    async adopt(token: string) {
      this.token = token
      setToken(token)
      await this.hydrate()
    },
    // hydrate memulihkan sesi + profil/preferensi dari server saat reload.
    async hydrate() {
      if (!this.token) return
      try {
        const me = await api.me()
        this.userId = me.id
        this.role = me.role
        this.username = me.username
        this.name = me.name
        this.email = me.email
        // Terapkan preferensi server (menang atas localStorage → sinkron lintas peramban).
        if (me.theme) applyTheme(me.theme as Theme)
        if (me.locale) applyServerLocale(me.locale)
      } catch {
        this.logout()
      }
    },
    setProfile(name: string, email: string) {
      this.name = name
      this.email = email
    },
    logout() {
      this.token = null
      this.role = ''
      this.userId = null
      this.username = ''
      this.name = ''
      this.email = ''
      setToken(null)
    },
  },
})
