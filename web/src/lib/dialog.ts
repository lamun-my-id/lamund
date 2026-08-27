import { reactive } from 'vue'

// Sistem dialog promise-based pengganti window.confirm/prompt bawaan (biar
// konsisten tema + bisa di-await). Satu <AppDialog> global di App.vue merender
// state ini; confirmDialog()/promptDialog() mengembalikan Promise.
interface DialogState {
  open: boolean
  mode: 'confirm' | 'prompt'
  title: string
  message: string
  danger: boolean
  confirmText: string
  inputValue: string
  inputPlaceholder: string
  resolve: ((v: boolean | string | null) => void) | null
}

export const dialogState = reactive<DialogState>({
  open: false, mode: 'confirm', title: '', message: '', danger: false,
  confirmText: '', inputValue: '', inputPlaceholder: '', resolve: null,
})

export function confirmDialog(opts: {
  message: string; title?: string; danger?: boolean; confirmText?: string
}): Promise<boolean> {
  return new Promise((resolve) => {
    Object.assign(dialogState, {
      open: true, mode: 'confirm', title: opts.title || '', message: opts.message,
      danger: opts.danger ?? true, confirmText: opts.confirmText || '',
      resolve: resolve as (v: boolean | string | null) => void,
    })
  }) as Promise<boolean>
}

export function promptDialog(opts: {
  message: string; title?: string; value?: string; placeholder?: string; confirmText?: string
}): Promise<string | null> {
  return new Promise((resolve) => {
    Object.assign(dialogState, {
      open: true, mode: 'prompt', title: opts.title || '', message: opts.message,
      danger: false, confirmText: opts.confirmText || '',
      inputValue: opts.value || '', inputPlaceholder: opts.placeholder || '',
      resolve: resolve as (v: boolean | string | null) => void,
    })
  }) as Promise<string | null>
}

export function resolveDialog(value: boolean | string | null) {
  const r = dialogState.resolve
  dialogState.open = false
  dialogState.resolve = null
  if (r) r(value)
}
