<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { dialogState, resolveDialog } from '../lib/dialog'

const { t } = useI18n()
const inputEl = ref<HTMLInputElement | null>(null)

watch(
  () => dialogState.open,
  async (open) => {
    if (open && dialogState.mode === 'prompt') {
      await nextTick()
      inputEl.value?.focus()
      inputEl.value?.select()
    }
  },
)

function ok() {
  resolveDialog(dialogState.mode === 'prompt' ? dialogState.inputValue : true)
}
function cancel() {
  resolveDialog(dialogState.mode === 'prompt' ? null : false)
}
</script>

<template>
  <Teleport to="body">
    <div v-if="dialogState.open" class="dlg-overlay" @click.self="cancel">
      <div class="dlg" role="dialog" aria-modal="true">
        <h3 v-if="dialogState.title">{{ dialogState.title }}</h3>
        <p class="dlg-msg">{{ dialogState.message }}</p>
        <input
          v-if="dialogState.mode === 'prompt'"
          ref="inputEl"
          v-model="dialogState.inputValue"
          class="input"
          :placeholder="dialogState.inputPlaceholder"
          @keyup.enter="ok"
          @keyup.esc="cancel"
        />
        <div class="dlg-actions">
          <button class="btn" @click="cancel">{{ t('common.cancel') }}</button>
          <button class="btn" :class="dialogState.danger ? 'danger' : 'pri'" @click="ok">
            {{ dialogState.confirmText || (dialogState.mode === 'prompt' ? t('common.save') : t('common.yes')) }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.dlg-overlay { position: fixed; inset: 0; background: rgba(20, 20, 35, 0.45); display: flex; align-items: center; justify-content: center; z-index: 100; padding: 20px; }
.dlg { background: var(--card); border-radius: 14px; width: min(420px, 100%); padding: 22px 24px; box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3); }
.dlg h3 { margin: 0 0 8px; font-size: 16px; }
.dlg-msg { color: var(--slate); font-size: 14px; line-height: 1.5; margin: 0 0 18px; white-space: pre-wrap; }
.dlg .input { width: 100%; margin-bottom: 18px; }
.dlg-actions { display: flex; justify-content: flex-end; gap: 10px; }
</style>
