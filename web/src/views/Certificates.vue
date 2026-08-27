<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { api, type Cert } from '../lib/api'
import Pulse from '../components/Pulse.vue'

const { t } = useI18n()

const certs = ref<Cert[]>([])
const loading = ref(true)
const error = ref('')

const counts = computed(() => ({
  valid: certs.value.filter((c) => c.status === 'valid').length,
  attention: certs.value.filter((c) => c.status === 'expiring' || c.status === 'expired' || c.status === 'none').length,
}))

function daysLeft(c: Cert): number | null {
  if (c.status === 'none' || !c.not_after) return null
  const ms = new Date(c.not_after).getTime() - Date.now()
  return Math.round(ms / 86_400_000)
}

// Bar 0–90 hari (masa berlaku LE); warna ungu untuk sehat, warn saat menipis.
function barWidth(c: Cert): number {
  const d = daysLeft(c)
  if (d === null) return 0
  return Math.max(0, Math.min(100, Math.round((d / 90) * 100)))
}

function label(c: Cert): string {
  switch (c.status) {
    case 'valid': return t('certs.statusValid')
    case 'expiring': return t('certs.statusExpiring')
    case 'expired': return t('certs.statusExpired')
    default: return t('certs.statusPending')
  }
}
function stClass(c: Cert): string {
  return c.status === 'valid' ? 'ok' : c.status === 'none' ? 'none' : 'soon'
}

async function load() {
  loading.value = true
  try {
    certs.value = await api.listCerts()
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>

<template>
  <div class="page-head">
    <div>
      <div class="h1">{{ t('certs.title') }}</div>
      <div class="sub">{{ t('certs.subtitle') }}</div>
    </div>
    <div v-if="!loading" class="head-stats">
      <div class="hs">
        <div class="n">{{ counts.valid }}</div>
        <div class="sub">{{ t('certs.valid') }}</div>
      </div>
      <div class="hs">
        <div class="n warn">{{ counts.attention }}</div>
        <div class="sub">{{ t('certs.attention') }}</div>
      </div>
    </div>
  </div>

  <div v-if="error" class="alert err">{{ error }}</div>
  <div v-if="loading" class="spin">{{ t('certs.loading') }}</div>

  <template v-else>
    <div v-if="certs.length" class="list">
      <div v-for="c in certs" :key="c.domain" class="row cert">
        <div class="info">
          <div class="dom mono">{{ c.domain }}</div>
          <div class="meta">
            <span>{{ c.status === 'none' ? t('certs.waitingDns') : c.issuer }}</span>
          </div>
        </div>

        <div class="life">
          <div class="lrow">
            <span>{{ c.status === 'none' ? t('certs.status') : t('certs.remaining') }}</span>
            <span class="mono">{{ daysLeft(c) === null ? '—' : daysLeft(c) + ' ' + t('certs.days') }}</span>
          </div>
          <div class="bar"><i :style="{ width: barWidth(c) + '%' }" :class="{ warn: c.status === 'expiring' || c.status === 'expired' }"></i></div>
        </div>

        <div class="mini" :class="stClass(c)">
          <Pulse v-if="c.status === 'valid'" state="on" />
          {{ label(c) }}
        </div>
      </div>
    </div>
    <div v-else class="card empty">
      <h3>{{ t('certs.emptyTitle') }}</h3>
      <p>{{ t('certs.emptyHint') }}</p>
    </div>
  </template>
</template>

<style scoped>
/* Header stat cluster (valid / needs-attention), mirrors mockup .page-head numbers */
.head-stats { display: flex; gap: 26px; }
.head-stats .hs .n { font-family: var(--font-display); font-weight: 700; font-size: 26px; line-height: 1; }
.head-stats .hs .n.warn { color: var(--warn); }
.head-stats .hs .sub { font-size: 11px; margin-top: 4px; }

/* Certificate row: reuse global .list/.row, override grid to domain | lifebar | status */
.cert { grid-template-columns: 1fr 200px auto; gap: 22px; }
.cert .info { min-width: 0; }
.cert .dom { font-family: var(--font-mono); font-weight: 500; font-size: 14px; letter-spacing: 0; }

/* Remaining-days lifebar */
.life .lrow { display: flex; justify-content: space-between; font-size: 12px; margin-bottom: 5px; color: var(--muted); }
.life .bar { height: 6px; border-radius: 4px; background: var(--line-2); overflow: hidden; }
.life .bar i { display: block; height: 100%; border-radius: 4px; background: linear-gradient(90deg, var(--brand-l), var(--brand)); }
.life .bar i.warn { background: var(--warn); }

/* Status uses global .mini (.ok/.build/.off); map expiring/expired -> soon, pending -> none */
.mini { white-space: nowrap; }
.mini.soon { color: var(--warn); }
.mini.none { color: var(--off); }

@media (max-width: 820px) {
  .cert { grid-template-columns: 1fr; }
}
</style>
