<script setup lang="ts">
// Секция страницы настроек: заголовок со стрелкой, сворачивание (состояние
// хранится на сервере) и участие в поиске по настройкам.
//
// Во время поиска секции не сворачиваются: если человек что-то нашёл, он
// хочет это видеть, а не открывать ещё раз.
import { computed, inject, ref } from 'vue'
import { collapseKey, matchSection, searchKey, type SectionDef } from '../sections'

const props = defineProps<{ s: SectionDef }>()

const search = inject(searchKey, ref(''))
const collapse = inject(collapseKey, { isOpen: () => true, toggle: () => {} })

const searching = computed(() => search.value.trim() !== '')
const matched = computed(() => matchSection(props.s, search.value))
const open = computed(() => (searching.value ? true : collapse.isOpen(props.s.id)))
</script>

<template>
  <section v-if="matched" class="section">
    <button class="head" :disabled="searching" @click="collapse.toggle(s.id)">
      <h3>{{ s.title }}</h3>
      <span v-if="!searching" class="chev">{{ open ? '▾' : '▸' }}</span>
    </button>
    <div v-show="open">
      <slot />
    </div>
  </section>
</template>

<style scoped>
.section {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 14px;
}

.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  background: none;
  border: none;
  padding: 0;
  margin-bottom: 10px;
  color: var(--text-color);
  cursor: pointer;
}

.head:disabled {
  cursor: default;
}

.head h3 {
  margin: 0;
  font-size: 16px;
}

.chev {
  color: var(--text-secondary);
}
</style>
