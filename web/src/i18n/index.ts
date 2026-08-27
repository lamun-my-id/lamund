// i18n panel — vue-i18n dengan 4 locale. Default `en`; `id` lengkap; `ja`/`su`
// sebagian (fallback ke en untuk kunci yang belum diterjemahkan).
import { createI18n } from 'vue-i18n'
import en from './en.json'
import id from './id.json'
import ja from './ja.json'
import su from './su.json'

export type Locale = 'en' | 'id' | 'ja' | 'su'

const KEY = 'lamund.locale'

export const LOCALES: { id: Locale; name: string; native: string }[] = [
  { id: 'en', name: 'English', native: 'English' },
  { id: 'id', name: 'Indonesian', native: 'Bahasa Indonesia' },
  { id: 'ja', name: 'Japanese', native: '日本語' },
  { id: 'su', name: 'Sundanese', native: 'Basa Sunda' },
]

function detect(): Locale {
  const saved = localStorage.getItem(KEY)
  if (saved === 'en' || saved === 'id' || saved === 'ja' || saved === 'su') return saved
  const nav = (navigator.language || 'en').slice(0, 2)
  if (nav === 'id' || nav === 'ja' || nav === 'su') return nav
  return 'en'
}

export const i18n = createI18n({
  legacy: false,
  locale: detect(),
  fallbackLocale: 'en',
  messages: { en, id, ja, su },
})

export function getLocale(): Locale {
  return i18n.global.locale.value as Locale
}

// setLocale menerapkan bahasa live + menyimpan ke peramban. `document.lang`
// diperbarui untuk aksesibilitas.
export function setLocale(l: Locale) {
  i18n.global.locale.value = l
  localStorage.setItem(KEY, l)
  document.documentElement.setAttribute('lang', l)
}

// applyServerLocale dipakai setelah /me: kalau server punya preferensi & beda
// dari yang tersimpan, ikuti server (sinkron lintas peramban).
export function applyServerLocale(l: string) {
  if (l === 'en' || l === 'id' || l === 'ja' || l === 'su') setLocale(l)
}
