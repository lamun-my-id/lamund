<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'
import { useAuth } from '../stores/auth'

const { t } = useI18n()
const router = useRouter()
const auth = useAuth()

interface ZoneEntry {
  domain: string
  owner_type: string
  owner_id: number
  record_count: number
  serial: number
}

const zones = ref<ZoneEntry[]>([])
const loading = ref(true)
const error = ref('')
const nsNotSet = ref(false)

// modal tambah zona
const showAdd = ref(false)
const newDomain = ref('')
const addBusy = ref(false)
const addErr = ref('')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const r = await api.dnsZones()
    zones.value = r.zones ?? []
    // cek settings nameserver hanya untuk superadmin
    if (auth.isSuperadmin) {
      try {
        const s = await api.dnsSettings()
        nsNotSet.value = !s.ns1
      } catch {
        nsNotSet.value = false
      }
    }
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function addZone() {
  const d = newDomain.value.trim()
  if (!d) return
  addBusy.value = true
  addErr.value = ''
  try {
    await api.createDnsZone(d)
    newDomain.value = ''
    showAdd.value = false
    await load()
  } catch (e) {
    addErr.value = (e as Error).message
  } finally {
    addBusy.value = false
  }
}

function manage(domain: string) {
  router.push('/domain/' + domain)
}

onMounted(load)
</script>

<template>
  <div class="page-head">
    <div>
      <div class="h1">{{ t('dns.title') }}</div>
      <div class="sub">{{ t('dns.subtitle') }}</div>
    </div>
    <button class="btn pri" @click="showAdd = true">{{ t('dns.addZone') }}</button>
  </div>

  <!-- banner nameserver belum dikonfigurasi (superadmin) -->
  <div v-if="auth.isSuperadmin && nsNotSet" class="alert err" style="margin-bottom: 16px">
    {{ t('dns.nsNotSet') }}
    <RouterLink to="/settings" style="margin-left: 8px; text-decoration: underline">{{ t('dns.goSettings') }}</RouterLink>
  </div>

  <div v-if="error" class="alert err">{{ error }}</div>
  <div v-if="loading" class="spin">{{ t('common.loading') }}</div>

  <template v-else>
    <div v-if="zones.length === 0" class="card empty">
      <h3>{{ t('dns.empty') }}</h3>
      <p>{{ t('dns.emptyHint') }}</p>
    </div>

    <table v-else class="tbl">
      <thead>
        <tr>
          <th>{{ t('dns.zoneName') }}</th>
          <th>{{ t('dns.records') }}</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="z in zones" :key="z.domain">
          <td class="mono">{{ z.domain }}</td>
          <td style="color: var(--muted)">{{ z.record_count ?? '—' }}</td>
          <td style="text-align: right">
            <button class="btn sm" @click="manage(z.domain)">{{ t('common.manage') }}</button>
          </td>
        </tr>
      </tbody>
    </table>
  </template>

  <!-- modal tambah zona -->
  <div class="modal" :class="{ show: showAdd }" @click.self="showAdd = false">
    <form class="modal-box" @submit.prevent="addZone">
      <div class="modal-head">
        <h3>{{ t('dns.addZone') }}</h3>
        <button type="button" class="modal-x" @click="showAdd = false">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M6 6l12 12M18 6L6 18" /></svg>
        </button>
      </div>
      <div class="modal-body">
        <div v-if="addErr" class="alert err" style="margin-bottom: 12px">{{ addErr }}</div>
        <div class="field">
          <label>{{ t('dns.zoneName') }}</label>
          <div class="input"><input v-model="newDomain" type="text" placeholder="example.com" autofocus required /></div>
        </div>
      </div>
      <div class="modal-foot">
        <button type="button" class="btn" @click="showAdd = false">{{ t('common.cancel') }}</button>
        <button class="btn pri" type="submit" :disabled="addBusy">
          {{ addBusy ? t('common.saving') : t('dns.addZone') }}
        </button>
      </div>
    </form>
  </div>
</template>
