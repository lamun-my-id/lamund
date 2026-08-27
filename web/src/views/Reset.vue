<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError } from '../lib/api'
import logo from '../assets/lamun-logo.png'

const props = defineProps<{ token: string }>()

const { t } = useI18n()
const router = useRouter()

const newPassword = ref('')
const show = ref(false)
const error = ref('')
const busy = ref(false)

async function submit() {
  error.value = ''
  busy.value = true
  try {
    await api.resetPassword(props.token, newPassword.value)
    router.push({ name: 'login', query: { reset: '1' } })
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : (e as Error).message
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="auth-wrap">
    <div class="auth-box">
      <div class="auth-head">
        <div class="mk"><img :src="logo" alt="Lamun" /></div>
        <h1>{{ t('reset.title') }}</h1>
        <p>{{ t('reset.subtitle') }}</p>
      </div>

      <form class="card panel" @submit.prevent="submit">
        <div v-if="error" class="alert err">{{ error }}</div>

        <div class="field" style="margin-bottom: 20px">
          <label for="np">{{ t('reset.newPassword') }}</label>
          <div class="input">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>
            <input id="np" v-model="newPassword" :type="show ? 'text' : 'password'" autocomplete="new-password" minlength="8" required />
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style="cursor: pointer" @click="show = !show"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/></svg>
          </div>
        </div>

        <button class="btn pri block" type="submit" :disabled="busy">
          {{ busy ? t('reset.resetting') : t('reset.submit') }}
        </button>
      </form>

      <div class="auth-foot">lamund</div>
    </div>
  </div>
</template>
