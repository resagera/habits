<script setup lang="ts">
import { toastMessage } from '../shared/toast'
</script>

<template>
  <Transition name="toast">
    <div v-if="toastMessage" class="toast">{{ toastMessage }}</div>
  </Transition>
</template>

<style scoped>
.toast {
  position: fixed;
  bottom: 70px;
  left: 50%;
  transform: translateX(-50%);
  background: rgba(0, 0, 0, 0.85);
  color: #fff;
  padding: 8px 16px;
  border-radius: 8px;
  z-index: 3000;
  max-width: 90vw;
}

.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.3s;
}

.toast-enter-from,
.toast-leave-to {
  opacity: 0;
}
</style>
