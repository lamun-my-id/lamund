<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'
import logo from '../assets/lamun-logo.png'

const props = defineProps<{ token: string }>()
const { t } = useI18n()

const state = ref<'verifying' | 'ok' | 'fail'>('verifying')

onMounted(async () => {
  try {
    await api.verifyEmail(props.token)
    state.value = 'ok'
  } catch {
    state.value = 'fail'
  }
})
</script>

<template>
  <div class="auth-wrap">
    <div class="auth-box">
      <div class="auth-head">
        <div class="mk"><img :src="logo" alt="Lamun" /></div>
        <h1>{{ t('verify.title') }}</h1>
      </div>
      <div class="card panel">
        <div v-if="state === 'verifying'" class="alert">{{ t('verify.verifying') }}…</div>
        <div v-else-if="state === 'ok'" class="alert ok">{{ t('verify.success') }}</div>
        <div v-else class="alert err">{{ t('verify.failed') }}</div>
      </div>
      <div class="auth-foot">
        <RouterLink :to="{ name: 'login' }" style="font-size: 13px; color: var(--muted); text-decoration: none">
          {{ t('verify.toLogin') }} →
        </RouterLink>
      </div>
    </div>
  </div>
</template>
