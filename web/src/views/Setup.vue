<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api, ApiError, setToken } from '../lib/api'
import { useAuth } from '../stores/auth'
import { LOCALES, setLocale, getLocale, type Locale } from '../i18n'
import { THEMES, applyTheme, getTheme, type Theme } from '../lib/theme'
import logo from '../assets/lamun-logo.png'

const { t } = useI18n()
const router = useRouter()
const auth = useAuth()

const locale = ref<Locale>(getLocale())
const theme = ref<Theme>(getTheme())
const name = ref('')
const email = ref('')
const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

function pickLocale(l: Locale) {
  locale.value = l
  setLocale(l)
}
function pickTheme(tm: Theme) {
  theme.value = tm
  applyTheme(tm)
}

async function submit() {
  error.value = ''
  if (password.value.length < 8) {
    error.value = t('setup.passwordHint')
    return
  }
  busy.value = true
  try {
    const r = await api.setup({
      username: username.value.trim(),
      password: password.value,
      name: name.value.trim(),
      email: email.value.trim(),
      locale: locale.value,
      theme: theme.value === 'editorial' ? '' : theme.value,
    })
    setToken(r.token)
    auth.token = r.token
    auth.role = r.role
    await auth.hydrate()
    router.push('/sites')
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : 'Gagal terhubung ke server'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="auth-wrap">
    <form class="auth-box" @submit.prevent="submit">
      <div class="auth-head">
        <div class="mk"><img :src="logo" alt="Lamun" /></div>
        <h1>{{ t('setup.title') }}</h1>
        <p>{{ t('setup.subtitle') }}</p>
      </div>

      <div class="steps"><span class="s on"></span><span class="s on"></span><span class="s"></span></div>

      <div class="card panel">
        <div v-if="error" class="alert err">{{ error }}</div>

        <div class="field">
          <label>{{ t('setup.language') }}</label>
          <div class="langs">
            <button v-for="l in LOCALES" :key="l.id" type="button" class="lang" :class="{ on: locale === l.id }" @click="pickLocale(l.id)">
              <b>{{ l.native }}</b>
              <small>{{ l.name }}</small>
            </button>
          </div>
        </div>

        <div class="grid-2">
          <div class="field">
            <label>{{ t('setup.name') }} <span class="muted">({{ t('common.optional') }})</span></label>
            <div class="input"><input v-model="name" type="text" autocomplete="name" /></div>
          </div>
          <div class="field">
            <label>{{ t('setup.email') }} <span class="muted">({{ t('common.optional') }})</span></label>
            <div class="input"><input v-model="email" type="email" autocomplete="email" /></div>
          </div>
        </div>

        <div class="field">
          <label>{{ t('setup.username') }}</label>
          <div class="input"><input v-model="username" type="text" autocomplete="username" placeholder="admin" required /></div>
        </div>

        <div class="field">
          <label>{{ t('setup.password') }}</label>
          <div class="input"><input v-model="password" type="password" autocomplete="new-password" required /></div>
          <small class="muted">{{ t('setup.passwordHint') }}</small>
        </div>

        <div class="field" style="margin-bottom: 20px">
          <label>{{ t('setup.theme') }}</label>
          <div class="themes">
            <button v-for="tm in THEMES" :key="tm.id" type="button" class="theme-card" :class="{ on: theme === tm.id }" @click="pickTheme(tm.id)">
              <span class="sw" :class="'sw-' + tm.id">{{ tm.name.charAt(0) }}</span>
              <span class="tb"><b>{{ tm.name }}</b></span>
            </button>
          </div>
        </div>

        <button class="btn pri block" type="submit" :disabled="busy">
          {{ busy ? t('setup.creating') : t('setup.create') }}
        </button>
      </div>

      <div class="auth-foot">lamund</div>
    </form>
  </div>
</template>

<style scoped>
.muted { color: var(--muted); font-weight: 400; }
</style>
