import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { router } from './router'
import { useAuth } from './stores/auth'
import { initTheme } from './lib/theme'
import { i18n } from './i18n'
import App from './App.vue'
import './styles/base.css'
import './styles/app.css'

initTheme() // terapkan tema tersimpan sebelum render (hindari flash)

const app = createApp(App)

// Global error handler: ubah "blank screen" (error render tak tertangani) jadi
// banner yang terlihat + log, agar bug UI mudah didiagnosis (bukan layar kosong).
app.config.errorHandler = (err, _instance, info) => {
  const e = err as Error
  console.error('[lamund] render error:', e, info)
  let bar = document.getElementById('lamund-errbar')
  if (!bar) {
    bar = document.createElement('div')
    bar.id = 'lamund-errbar'
    bar.style.cssText =
      'position:fixed;top:0;left:0;right:0;z-index:9999;background:#d64560;color:#fff;' +
      'font:13px/1.5 monospace;padding:10px 16px;white-space:pre-wrap;max-height:40vh;overflow:auto;' +
      'box-shadow:0 4px 20px rgba(0,0,0,.3)'
    document.body.appendChild(bar)
  }
  bar.textContent = 'UI error (' + info + '): ' + (e?.message || String(err)) + '\n' + (e?.stack || '')
}

app.use(createPinia())
app.use(i18n)

// Pulihkan sesi dari token tersimpan sebelum router aktif.
const auth = useAuth()

auth.hydrate().finally(() => {
  app.use(router)
  app.mount('#app')
})
