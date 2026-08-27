<script setup lang="ts">
import { ref, nextTick } from 'vue'
import { useRouter, useRoute, RouterLink } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAuth } from '../stores/auth'
import { ApiError } from '../lib/api'
import logo from '../assets/lamun-logo.png'

const { t } = useI18n()
const auth = useAuth()
const router = useRouter()
const route = useRoute()

const username = ref('')
const password = ref('')
const show = ref(false)
const error = ref('')
const busy = ref(false)

// Langkah kedua MFA.
const step = ref<1 | 2>(1)
const pending = ref('')
const code = ref('')
const useRecovery = ref(false)
const codeInput = ref<HTMLInputElement | null>(null)

function goNext() {
  const next = (route.query.next as string) || '/sites'
  router.push(next)
}

// Reset ke langkah 1 (mis. pending kedaluwarsa).
function backToLogin() {
  step.value = 1
  pending.value = ''
  code.value = ''
  useRecovery.value = false
  password.value = ''
}

async function submit() {
  error.value = ''
  busy.value = true
  try {
    const r = await auth.login(username.value.trim(), password.value)
    if (r && r.mfaRequired) {
      pending.value = r.pending
      step.value = 2
      code.value = ''
      await nextTick()
      codeInput.value?.focus()
      return
    }
    goNext()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('login.connectError')
  } finally {
    busy.value = false
  }
}

async function submitMFA() {
  error.value = ''
  busy.value = true
  try {
    await auth.completeMFA(pending.value, code.value.trim())
    goNext()
  } catch (e) {
    if (e instanceof ApiError) {
      // Sesi MFA tak valid/kedaluwarsa (pending expired) → kembali ke langkah 1.
      if (e.status === 401 && /pending|sesi|session|expired|kedaluwarsa/i.test(e.message)) {
        error.value = t('mfa.expired')
        backToLogin()
      } else if (e.status === 429) {
        error.value = t('mfa.rateLimited')
      } else {
        error.value = t('mfa.invalidCode')
      }
    } else {
      error.value = t('login.connectError')
    }
    code.value = ''
    await nextTick()
    codeInput.value?.focus()
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="auth-wrap">
    <form class="auth-box" @submit.prevent="step === 1 ? submit() : submitMFA()">
      <div class="auth-head">
        <div class="mk"><img :src="logo" alt="Lamun" /></div>
        <h1>{{ t('login.title') }}</h1>
        <p>{{ step === 1 ? t('login.subtitle') : t('mfa.mfaPrompt') }}</p>
      </div>

      <div class="card panel">
        <div v-if="error" class="alert err">{{ error }}</div>

        <!-- Langkah 1: username + password -->
        <template v-if="step === 1">
          <div class="field">
            <label for="u">{{ t('login.username') }}</label>
            <div class="input">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><circle cx="12" cy="8" r="3.2"/><path d="M5 20a7 7 0 0 1 14 0"/></svg>
              <input id="u" v-model="username" type="text" autocomplete="username" placeholder="admin" required />
            </div>
          </div>

          <div class="field" style="margin-bottom: 4px">
            <label for="p">{{ t('login.password') }}</label>
            <div class="input">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>
              <input id="p" v-model="password" :type="show ? 'text' : 'password'" autocomplete="current-password" required />
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" style="cursor: pointer" @click="show = !show"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/></svg>
            </div>
          </div>
          <div style="display: flex; justify-content: flex-end; margin-bottom: 16px">
            <RouterLink :to="{ name: 'forgot' }" style="font-size: 13px; color: var(--muted); text-decoration: none">{{ t('login.forgot') }}</RouterLink>
          </div>

          <button class="btn pri block" type="submit" :disabled="busy">
            {{ busy ? t('login.signingIn') : t('login.submit') }}
          </button>
        </template>

        <!-- Langkah 2: kode MFA (TOTP atau recovery) -->
        <template v-else>
          <div class="field" style="margin-bottom: 12px">
            <label for="mfa">{{ useRecovery ? t('mfa.enterRecovery') : t('mfa.enterCode') }}</label>
            <div class="input">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>
              <input
                id="mfa"
                ref="codeInput"
                v-model="code"
                type="text"
                :inputmode="useRecovery ? 'text' : 'numeric'"
                autocomplete="one-time-code"
                :maxlength="useRecovery ? 20 : 6"
                :placeholder="useRecovery ? 'xxxxx-xxxxx' : '000000'"
                required
              />
            </div>
          </div>
          <div style="text-align: right; margin-bottom: 16px">
            <a href="#" style="font-size: 13px; color: var(--muted); text-decoration: none" @click.prevent="useRecovery = !useRecovery; code = ''">
              {{ useRecovery ? t('mfa.useTotp') : t('mfa.useRecovery') }}
            </a>
          </div>

          <button class="btn pri block" type="submit" :disabled="busy">
            {{ busy ? t('login.signingIn') : t('mfa.verify') }}
          </button>
          <button class="btn block" type="button" style="margin-top: 8px" :disabled="busy" @click="backToLogin">
            {{ t('common.cancel') }}
          </button>
        </template>
      </div>

      <div class="auth-foot">lamund</div>
    </form>
  </div>
</template>

<style scoped>
</style>
