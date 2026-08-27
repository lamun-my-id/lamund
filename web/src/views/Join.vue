<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

const status = ref<'joining' | 'ok' | 'fail'>('joining')
const errMsg = ref('')

onMounted(async () => {
  const token = route.params.token as string
  try {
    await api.acceptInvite(token)
    status.value = 'ok'
    setTimeout(() => router.replace({ name: 'team' }), 1500)
  } catch (e) {
    status.value = 'fail'
    errMsg.value = (e as Error).message
  }
})
</script>

<template>
  <div style="display: flex; justify-content: center; align-items: center; min-height: 60vh">
    <div class="card" style="padding: 32px; max-width: 380px; width: 100%; text-align: center">
      <div v-if="status === 'joining'" class="spin">{{ t('team.joining') }}</div>
      <div v-else-if="status === 'ok'" class="alert ok">{{ t('team.joinOk') }}</div>
      <div v-else class="alert err">
        <b>{{ t('team.joinFail') }}</b>
        <div v-if="errMsg" style="margin-top: 6px; font-size: 13px">{{ errMsg }}</div>
      </div>
    </div>
  </div>
</template>
