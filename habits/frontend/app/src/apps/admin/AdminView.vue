<script setup lang="ts">
// Страница «Админка» — доступна только администраторам. Роут защищён гардом
// в router/index.ts, пункт меню виден только админу. Здесь собраны блоки
// управления пользователями, лимитами и доступом к страницам.
import { isAdmin } from '../../shared/me'
import AdminUsers from './components/AdminUsers.vue'
import AdminLimits from './components/AdminLimits.vue'
import AdminPages from './components/AdminPages.vue'
import AdminGallery from './components/AdminGallery.vue'
</script>

<template>
  <template v-if="isAdmin">
    <AdminUsers />
    <AdminLimits />
    <AdminPages />
    <AdminGallery />
  </template>
  <p v-else class="hint-text">Раздел доступен только администраторам.</p>
</template>

<style scoped>
.hint-text {
  font-size: 13px;
  color: var(--text-secondary);
}
</style>
