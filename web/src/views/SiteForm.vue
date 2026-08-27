<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '../lib/api'
import { useScope } from '../stores/scope'

const { t } = useI18n()

const props = defineProps<{ domain?: string }>()
const router = useRouter()
const scope = useScope()

const editing = computed(() => !!props.domain)
const type = ref<'static' | 'proxy'>('static')
const domain = ref(props.domain ?? '')
const rootPath = ref('')
const target = ref('')
const error = ref('')
const busy = ref(false)
const loading = ref(false)

// Field target menampilkan "http://" sebagai awalan; tambahkan skema bila belum ada.
function withScheme(v: string): string {
  return /^https?:\/\//.test(v) ? v : `http://${v}`
}

onMounted(async () => {
  if (!props.domain) return
  loading.value = true
  try {
    const s = await api.getSite(props.domain)
    type.value = s.type
    rootPath.value = s.root_path ?? ''
    target.value = (s.proxy_target ?? '').replace(/^https?:\/\//, '')
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    loading.value = false
  }
})

async function submit() {
  error.value = ''
  busy.value = true
  try {
    const payload = {
      type: type.value,
      root_path: type.value === 'static' ? rootPath.value.trim() : '',
      proxy_target: type.value === 'proxy' ? withScheme(target.value.trim()) : '',
    }
    if (editing.value) {
      await api.patchSite(props.domain!, payload)
      router.push({ name: 'site-detail', params: { domain: props.domain } })
    } else {
      // Owner = scope aktif: Personal → server pakai user.id; Team → owner_id tim.
      const owner =
        scope.current.type === 'team'
          ? { owner_type: 'team', owner_id: scope.current.id }
          : { owner_type: 'user' }
      await api.createSite({ domain: domain.value.trim(), ...payload, ...owner })
      router.push({ name: 'sites' })
    }
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('siteForm.saveFailed')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="crumb">
    <RouterLink :to="{ name: 'sites' }">{{ t('sites.title') }}</RouterLink>
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M9 6l6 6-6 6"/></svg>
    {{ editing ? domain : t('siteForm.breadcrumbAdd') }}
  </div>
  <div class="h1" style="margin-bottom: 4px">{{ editing ? t('siteForm.editTitle') : t('siteForm.addTitle') }}</div>
  <div class="sub" style="margin-bottom: 24px">
    {{ editing ? t('siteForm.editSubtitle') : t('siteForm.addSubtitle') }}
  </div>

  <div v-if="loading" class="spin">{{ t('common.loading') }}</div>

  <form v-else class="card" style="padding: 24px" @submit.prevent="submit">
    <div v-if="error" class="alert err">{{ error }}</div>

    <div class="field">
      <label>{{ t('siteForm.siteType') }}</label>
      <div class="seg">
        <button type="button" :class="{ on: type === 'static' }" @click="type = 'static'">
          <b>{{ t('siteForm.static') }}</b><span>{{ t('siteForm.staticDesc') }}</span>
        </button>
        <button type="button" :class="{ on: type === 'proxy' }" @click="type = 'proxy'">
          <b>{{ t('siteForm.proxy') }}</b><span>{{ t('siteForm.proxyDesc') }}</span>
        </button>
      </div>
    </div>

    <div class="field">
      <label for="d">{{ t('siteForm.domain') }}</label>
      <div class="input">
        <input id="d" v-model="domain" class="mono" type="text" placeholder="toko.contoh.com" :disabled="editing" required />
      </div>
      <div class="hint">{{ editing ? t('siteForm.domainEditHint') : t('siteForm.domainAddHint') }}</div>
    </div>

    <div v-if="type === 'static'" class="field">
      <label>{{ t('siteForm.siteFiles') }}</label>
      <div class="hint" style="margin-top: 0">
        <i18n-t keypath="siteForm.siteFilesHint" tag="span">
          <template #deployZip><b>{{ t('siteForm.deployZip') }}</b></template>
        </i18n-t>
      </div>
    </div>

    <div v-else class="field">
      <label for="t">{{ t('siteForm.upstreamTarget') }}</label>
      <div class="input">
        <span class="pre">http://</span>
        <input id="t" v-model="target" class="mono" type="text" placeholder="127.0.0.1:3000" required />
      </div>
      <div class="hint">{{ t('siteForm.upstreamHint') }}</div>
    </div>

    <div style="display: flex; gap: 10px; margin-top: 8px">
      <button class="btn pri" type="submit" :disabled="busy">{{ busy ? t('common.saving') : editing ? t('siteForm.saveChanges') : t('siteForm.saveSite') }}</button>
      <RouterLink class="btn" :to="editing ? { name: 'site-detail', params: { domain } } : { name: 'sites' }">{{ t('common.cancel') }}</RouterLink>
    </div>
  </form>
</template>
