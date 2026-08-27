<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'
import logo from '../assets/lamun-logo.png'

const { t } = useI18n()

const email = ref('')
const sent = ref(false)
const busy = ref(false)

async function submit() {
  busy.value = true
  try {
    await api.forgotPassword(email.value.trim())
  } catch {
    /* always show the neutral message — don't leak enumeration */
  } finally {
    busy.value = false
    sent.value = true
  }
}
</script>

<template>
  <div class="auth-wrap">
    <div class="auth-box">
      <div class="auth-head">
        <div class="mk"><img :src="logo" alt="Lamun" /></div>
        <h1>{{ t('forgot.title') }}</h1>
        <p>{{ t('forgot.subtitle') }}</p>
      </div>

      <div class="card panel">
        <template v-if="!sent">
          <form @submit.prevent="submit">
            <div class="field" style="margin-bottom: 20px">
              <label for="fe">{{ t('forgot.email') }}</label>
              <div class="input">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="2" y="4" width="20" height="16" rx="2"/><path d="M2 7l10 7 10-7"/></svg>
                <input id="fe" v-model="email" type="email" autocomplete="email" required />
              </div>
            </div>
            <button class="btn pri block" type="submit" :disabled="busy">
              {{ busy ? t('forgot.sending') : t('forgot.submit') }}
            </button>
          </form>
        </template>
        <template v-else>
          <div class="alert ok">{{ t('forgot.checkEmail') }}</div>
        </template>
      </div>

      <div class="auth-foot">
        <RouterLink :to="{ name: 'login' }" style="font-size: 13px; color: var(--muted); text-decoration: none">
          ← {{ t('forgot.backToLogin') }}
        </RouterLink>
      </div>
    </div>
  </div>
</template>
