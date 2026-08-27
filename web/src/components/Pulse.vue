<script setup lang="ts">
// Denyut status: hidup (default), warn, atau off — signature visual Lamund.
defineProps<{ state?: 'on' | 'warn' | 'off' }>()
</script>

<template>
  <span class="pulse" :class="state && state !== 'on' ? state : ''"><i></i></span>
</template>
