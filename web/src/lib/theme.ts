// Tema panel — disimpan di localStorage, diterapkan via <html data-theme>.
// (Nanti R3: sinkron ke preferensi per-user via API.)
export type Theme = 'editorial' | 'terminal' | 'seagrass'

const KEY = 'lamund.theme'

export const THEMES: { id: Theme; name: string; desc: string }[] = [
  { id: 'editorial', name: 'Editorial', desc: 'Terang · lapang · ungu' },
  { id: 'terminal', name: 'Terminal', desc: 'Gelap · fokus · violet' },
  { id: 'seagrass', name: 'Seagrass', desc: 'Terang · teal + ungu' },
]

export function getTheme(): Theme {
  const t = localStorage.getItem(KEY)
  return t === 'terminal' || t === 'seagrass' ? t : 'editorial'
}

export function applyTheme(t: Theme) {
  if (t === 'editorial') document.documentElement.removeAttribute('data-theme')
  else document.documentElement.setAttribute('data-theme', t)
  localStorage.setItem(KEY, t)
}

export function initTheme() {
  applyTheme(getTheme())
}
