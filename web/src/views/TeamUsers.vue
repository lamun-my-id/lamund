<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuth } from '../stores/auth'
import UsersView from './Users.vue'
import TeamsView from './Teams.vue'

const { t } = useI18n()
const auth = useAuth()

// Superadmin: default ke tab Pengguna (kelola user). Selain itu hanya Team.
const tab = ref<'users' | 'team'>(auth.isSuperadmin ? 'users' : 'team')
</script>

<template>
  <div class="tu-tabs">
    <button v-if="auth.isSuperadmin" :class="{ on: tab === 'users' }" @click="tab = 'users'">
      {{ t('users.title') }}
    </button>
    <button :class="{ on: tab === 'team' }" @click="tab = 'team'">
      {{ t('team.title') }}
    </button>
  </div>

  <UsersView v-if="tab === 'users' && auth.isSuperadmin" />
  <TeamsView v-else />
</template>

<style scoped>
.tu-tabs {
  display: flex;
  gap: 6px;
  margin-bottom: 22px;
}
.tu-tabs button {
  border: 1px solid var(--line);
  background: var(--card);
  padding: 8px 18px;
  border-radius: 10px;
  font: inherit;
  font-weight: 600;
  font-size: 14px;
  color: var(--muted);
  cursor: pointer;
  transition: 0.15s;
}
.tu-tabs button:hover {
  border-color: var(--brand-l);
  color: var(--brand);
}
.tu-tabs button.on {
  background: var(--tint);
  color: var(--brand);
  border-color: transparent;
}
</style>
