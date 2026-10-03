<script setup lang="ts">
// Выбор категории из дерева любой вложенности: вложенность передаётся
// отступом в подписи — нативный <select> вложенных групп не умеет, а
// собственный выпадающий список в мини-аппе ведёт себя хуже родного.
import { computed } from 'vue'
import { flattenCategories, type FinanceCategory } from '../types'

const props = withDefaults(defineProps<{
  modelValue: number | null
  categories: FinanceCategory[]
  kind?: 'expense' | 'income' | 'all'
  emptyLabel?: string
}>(), { kind: 'all', emptyLabel: 'Без категории' })

const emit = defineEmits<{ 'update:modelValue': [number | null] }>()

const items = computed(() =>
  flattenCategories(props.categories)
    .filter(({ cat }) => props.kind === 'all' || cat.kind === props.kind),
)

const value = computed({
  get: () => props.modelValue ?? 0,
  set: (v: number) => emit('update:modelValue', v > 0 ? Number(v) : null),
})

function label(cat: FinanceCategory, depth: number): string {
  return `${'　'.repeat(depth)}${cat.icon ? cat.icon + ' ' : ''}${cat.name}`
}
</script>

<template>
  <select v-model.number="value">
    <option :value="0">{{ emptyLabel }}</option>
    <option v-for="{ cat, depth } in items" :key="cat.id" :value="cat.id">
      {{ label(cat, depth) }}
    </option>
  </select>
</template>
