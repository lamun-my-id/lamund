<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { confirmDialog } from '../lib/dialog'
import { api, type DnsRecord } from '../lib/api'

const { t } = useI18n()
const router = useRouter()

const props = defineProps<{ domain: string }>()

// state utama
const zone = ref<Record<string, unknown> | null>(null)
const records = ref<DnsRecord[]>([])
const nameservers = ref<string[]>([])
const glue = ref<{ host: string; ip: string }[]>([])
const loading = ref(true)
const error = ref('')

// form tambah record
const newRec = ref({ name: '', type: 'A', value: '', ttl: 3600, priority: 10 })
const addBusy = ref(false)
const addErr = ref('')

// edit inline
interface EditState { value: string; ttl: number }
const editing = ref<Record<number, EditState>>({})
const editBusy = ref<Record<number, boolean>>({})
const editErr = ref('')

// delete record
const delBusy = ref<Record<number, boolean>>({})
const delErr = ref('')

// delete zona
const deletingZone = ref(false)
const deleteZoneErr = ref('')

const RECORD_TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'CAA']

const showPriority = computed(() => newRec.value.type === 'MX')

async function load() {
  loading.value = true
  error.value = ''
  try {
    const r = await api.dnsZone(props.domain)
    zone.value = r.zone
    records.value = r.records ?? []
    nameservers.value = r.nameservers ?? []
    glue.value = r.glue ?? []
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
}

async function addRecord() {
  addErr.value = ''
  addBusy.value = true
  try {
    const body: Partial<DnsRecord> = {
      name: newRec.value.name,
      type: newRec.value.type,
      value: newRec.value.value,
      ttl: newRec.value.ttl,
    }
    if (newRec.value.type === 'MX') {
      body.priority = newRec.value.priority
    }
    const r = await api.addDnsRecord(props.domain, body)
    records.value = r.records ?? []
    newRec.value = { name: '', type: 'A', value: '', ttl: 3600, priority: 10 }
  } catch (e) {
    addErr.value = (e as Error).message
  } finally {
    addBusy.value = false
  }
}

function startEdit(rec: DnsRecord) {
  editing.value[rec.id] = { value: rec.value, ttl: rec.ttl }
  editErr.value = ''
}

function cancelEdit(id: number) {
  delete editing.value[id]
  editErr.value = ''
}

async function saveEdit(rec: DnsRecord) {
  const ed = editing.value[rec.id]
  if (!ed) return
  editBusy.value[rec.id] = true
  editErr.value = ''
  try {
    const r = await api.patchDnsRecord(props.domain, rec.id, { value: ed.value, ttl: ed.ttl })
    records.value = r.records ?? []
    delete editing.value[rec.id]
  } catch (e) {
    editErr.value = (e as Error).message
  } finally {
    delete editBusy.value[rec.id]
  }
}

async function deleteRecord(id: number) {
  delBusy.value[id] = true
  delErr.value = ''
  try {
    const r = await api.deleteDnsRecord(props.domain, id)
    records.value = r.records ?? []
  } catch (e) {
    delErr.value = (e as Error).message
  } finally {
    delete delBusy.value[id]
  }
}

async function deleteZone() {
  if (!(await confirmDialog({ message: t('dns.confirmDeleteZone', { domain: props.domain }) }))) return
  deletingZone.value = true
  deleteZoneErr.value = ''
  try {
    await api.deleteDnsZone(props.domain)
    router.push('/domain')
  } catch (e) {
    deleteZoneErr.value = (e as Error).message
    deletingZone.value = false
  }
}

function copy(text: string) {
  navigator.clipboard?.writeText(text)
}

onMounted(load)
</script>

<template>
  <div class="crumb">
    <RouterLink to="/domain">{{ t('dns.title') }}</RouterLink>
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M9 6l6 6-6 6" /></svg>
    {{ domain }}
  </div>

  <div v-if="loading" class="spin">{{ t('common.loading') }}</div>
  <div v-else-if="error && !zone" class="alert err">{{ error }}</div>

  <template v-else-if="zone">
    <div class="page-head">
      <div>
        <div class="h1 mono">{{ domain }}</div>
        <div class="sub">{{ t('dns.subtitle') }}</div>
      </div>
    </div>

    <!-- Banner delegasi -->
    <div class="card" style="padding: 16px 20px; margin-bottom: 20px">
      <div class="sec"><h3>{{ t('dns.delegationTitle') }}</h3></div>
      <p class="sub" style="margin-bottom: 10px">{{ t('dns.delegationHint') }}</p>

      <template v-if="nameservers.length >= 2">
        <div class="delg-ns">
          <span v-for="ns in nameservers" :key="ns" class="tag static mono" style="margin-right: 6px; margin-bottom: 6px">{{ ns }}</span>
        </div>

        <template v-if="glue.length">
          <div class="sec" style="margin-top: 14px"><h3>{{ t('dns.glueTitle') }}</h3></div>
          <table class="tbl" style="margin-top: 6px">
            <thead>
              <tr>
                <th>{{ t('dns.glueHost') }}</th>
                <th>{{ t('dns.glueIP') }}</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="g in glue" :key="g.host">
                <td class="mono">{{ g.host }}</td>
                <td class="mono">{{ g.ip }}</td>
                <td style="text-align: right">
                  <button class="btn sm" @click="copy(g.ip)">{{ t('common.copy') }}</button>
                </td>
              </tr>
            </tbody>
          </table>
        </template>
      </template>
      <div v-else class="alert err" style="margin-top: 6px; margin-bottom: 0">{{ t('dns.nsNotSet') }}</div>
    </div>

    <!-- Tabel record -->
    <div class="card" style="padding: 16px 20px; margin-bottom: 20px">
      <div class="sec"><h3>{{ t('dns.records') }}</h3></div>

      <div v-if="editErr" class="alert err" style="margin-bottom: 10px">{{ editErr }}</div>
      <div v-if="delErr" class="alert err" style="margin-bottom: 10px">{{ delErr }}</div>

      <table class="tbl" style="margin-bottom: 16px">
        <thead>
          <tr>
            <th>{{ t('dns.recordName') }}</th>
            <th>{{ t('dns.recordType') }}</th>
            <th>{{ t('dns.recordValue') }}</th>
            <th>{{ t('dns.ttl') }}</th>
            <th>{{ t('dns.priority') }}</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="records.length === 0">
            <td colspan="6" style="text-align: center; color: var(--muted)">{{ t('dns.empty') }}</td>
          </tr>
          <template v-for="rec in records" :key="rec.id">
            <!-- baris normal -->
            <tr v-if="!editing[rec.id]">
              <td class="mono">{{ rec.name }}</td>
              <td><span class="tag" :class="rec.type === 'A' || rec.type === 'AAAA' ? 'static' : 'proxy'">{{ rec.type }}</span></td>
              <td class="mono" style="max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ rec.value }}</td>
              <td style="color: var(--muted)">{{ rec.ttl }}</td>
              <td style="color: var(--muted)">{{ rec.type === 'MX' ? rec.priority : '—' }}</td>
              <td style="text-align: right; display: flex; gap: 6px; justify-content: flex-end">
                <button class="btn sm" @click="startEdit(rec)">{{ t('common.edit') }}</button>
                <button class="btn sm danger" :disabled="!!delBusy[rec.id]" @click="deleteRecord(rec.id)">{{ t('common.delete') }}</button>
              </td>
            </tr>
            <!-- baris edit inline -->
            <tr v-else>
              <td class="mono">{{ rec.name }}</td>
              <td><span class="tag" :class="rec.type === 'A' || rec.type === 'AAAA' ? 'static' : 'proxy'">{{ rec.type }}</span></td>
              <td>
                <div class="input" style="padding: 5px 9px">
                  <input v-model="editing[rec.id].value" type="text" style="width: 100%; min-width: 160px" />
                </div>
              </td>
              <td>
                <div class="input" style="padding: 5px 9px; width: 80px">
                  <input v-model.number="editing[rec.id].ttl" type="number" min="60" style="width: 70px" />
                </div>
              </td>
              <td style="color: var(--muted)">{{ rec.type === 'MX' ? rec.priority : '—' }}</td>
              <td style="text-align: right; display: flex; gap: 6px; justify-content: flex-end">
                <button class="btn sm pri" :disabled="!!editBusy[rec.id]" @click="saveEdit(rec)">
                  {{ editBusy[rec.id] ? t('common.saving') : t('common.save') }}
                </button>
                <button class="btn sm" @click="cancelEdit(rec.id)">{{ t('common.cancel') }}</button>
              </td>
            </tr>
          </template>
        </tbody>
      </table>

      <!-- form tambah record -->
      <div class="sec"><h3>{{ t('dns.addRecord') }}</h3></div>
      <div v-if="addErr" class="alert err" style="margin-bottom: 10px">{{ addErr }}</div>
      <form class="rec-form" @submit.prevent="addRecord">
        <div class="input rf-name"><input v-model="newRec.name" type="text" :placeholder="t('dns.recordName')" /></div>
        <select v-model="newRec.type" class="rsel">
          <option v-for="tp in RECORD_TYPES" :key="tp" :value="tp">{{ tp }}</option>
        </select>
        <div class="input rf-val"><input v-model="newRec.value" type="text" :placeholder="t('dns.recordValue')" required /></div>
        <div class="input rf-ttl"><input v-model.number="newRec.ttl" type="number" min="60" :placeholder="t('dns.ttl')" /></div>
        <div v-if="showPriority" class="input rf-prio"><input v-model.number="newRec.priority" type="number" min="0" :placeholder="t('dns.priority')" /></div>
        <button class="btn pri" type="submit" :disabled="addBusy">{{ addBusy ? t('common.saving') : t('dns.addRecord') }}</button>
      </form>
    </div>

    <!-- Hapus zona -->
    <div class="card" style="padding: 16px 20px; margin-bottom: 20px">
      <div class="sec"><h3>{{ t('dns.deleteZone') }}</h3></div>
      <div v-if="deleteZoneErr" class="alert err" style="margin-bottom: 10px">{{ deleteZoneErr }}</div>
      <button class="btn danger" :disabled="deletingZone" @click="deleteZone">
        {{ deletingZone ? t('common.saving') : t('dns.deleteZone') }}
      </button>
    </div>
  </template>
</template>

<style scoped>
.delg-ns {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 4px;
}

.rec-form {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  margin-top: 10px;
}

.rec-form .input {
  padding: 8px 11px;
}

.rf-name { width: 130px; }
.rf-val { flex: 1; min-width: 160px; }
.rf-ttl { width: 90px; }
.rf-prio { width: 80px; }

.rsel {
  font: inherit;
  font-size: 13.5px;
  padding: 9px 11px;
  border-radius: 10px;
  border: 1px solid var(--line);
  background: var(--card);
  color: var(--slate);
}
</style>
