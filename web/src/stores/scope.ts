import { defineStore } from 'pinia'
import { api, type Team } from '../lib/api'

// Scope = lensa kepemilikan aktif di panel: Personal (owner_type=user) atau
// sebuah Team (owner_type=team). Dipakai untuk memfilter daftar deployment dan
// sebagai default owner saat membuat resource baru.
export interface Scope {
  type: 'user' | 'team'
  id: number // 0 untuk personal (owner_id disetel server = user.id)
  label: string
}

const KEY = 'lamund.scope'

interface State {
  teams: Team[]
  current: Scope
}

const PERSONAL: Scope = { type: 'user', id: 0, label: 'Personal' }

export const useScope = defineStore('scope', {
  state: (): State => ({ teams: [], current: loadSaved() }),
  getters: {
    // Semua pilihan scope: Personal + tiap team anggota.
    options(s): Scope[] {
      return [PERSONAL, ...s.teams.map((t) => ({ type: 'team' as const, id: t.id, label: t.name }))]
    },
    isPersonal: (s) => s.current.type === 'user',
  },
  actions: {
    async loadTeams() {
      try {
        this.teams = await api.listTeams()
      } catch {
        this.teams = []
      }
      // Jika scope tersimpan menunjuk team yang tak lagi ada, jatuh ke Personal.
      if (this.current.type === 'team' && !this.teams.some((t) => t.id === this.current.id)) {
        this.setScope(PERSONAL)
      }
    },
    setScope(sc: Scope) {
      this.current = sc
      localStorage.setItem(KEY, JSON.stringify(sc))
    },
    // matches menentukan apakah resource (owner_type/owner_id) masuk scope aktif.
    matches(ownerType?: string, ownerId?: number): boolean {
      if (this.current.type === 'user') return ownerType === 'user'
      return ownerType === 'team' && ownerId === this.current.id
    },
  },
})

function loadSaved(): Scope {
  try {
    const raw = localStorage.getItem(KEY)
    if (raw) {
      const s = JSON.parse(raw)
      if (s && (s.type === 'user' || s.type === 'team')) return s
    }
  } catch {
    /* abaikan */
  }
  return PERSONAL
}
