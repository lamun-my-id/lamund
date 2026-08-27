import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Panel di-embed ke binary lamund & disajikan di port admin.
// Proxy /api/v1 ke server API lokal saat `npm run dev`.
export default defineConfig({
  plugins: [vue()],
  build: { outDir: 'dist', emptyOutDir: true },
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: true },
    },
  },
})
