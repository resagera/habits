<script setup lang="ts">
// Рекурсивный рендер чек-листа-ссылки в проекте: пункты + вложенные подгруппы
// произвольной глубины. Ссылается на себя по имени файла.
import type { ResolvedCheckGroup } from '../types'

defineProps<{ group: ResolvedCheckGroup }>()
</script>

<template>
  <div v-for="i in group.items" :key="i.id" class="chk-item" :class="{ done: i.done }">
    {{ i.done ? '☑' : '☐' }} {{ i.name }}
  </div>
  <div v-for="sg in group.subgroups ?? []" :key="sg.id" class="chk-sub">
    <div class="chk-sub-name">{{ sg.name }}</div>
    <CheckerGroupView :group="sg" />
  </div>
</template>

<style scoped>
.chk-item {
  font-size: 13px;
  padding: 2px 0;
}

.chk-item.done {
  color: var(--text-secondary);
  text-decoration: line-through;
}

.chk-sub {
  margin: 4px 0 0 12px;
}

.chk-sub-name {
  font-size: 12px;
  font-weight: 700;
  color: var(--text-secondary);
}
</style>
