<script setup lang="ts">
import { useRoute } from 'vue-router'

const route = useRoute()
</script>

<template>
  <div class="stub">
    <div class="stub-icon">{{ route.meta.icon }}</div>
    <p>{{ route.meta.title }} — скоро здесь</p>
  </div>
</template>

<style scoped>
.stub {
  text-align: center;
  padding: 48px 0;
  color: var(--text-secondary);
}

.stub-icon {
  font-size: 48px;
}
</style>
