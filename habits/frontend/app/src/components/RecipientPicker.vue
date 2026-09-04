<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api } from '../shared/api/client'

// Поле «кому отправить» + чипы тех, с кем уже делился (любым способом).
// Тап по чипу подставляет получателя, повторный — снимает выбор.

interface Recipient {
  id: number
  username: string
  first_name: string
}

const model = defineModel<string>({ required: true })

// список общий для всех форм шаринга — кэшируем на модуль
let cache: Recipient[] | null = null

const recent = ref<Recipient[]>([])

onMounted(async () => {
  if (cache) {
    recent.value = cache
    return
  }
  try {
    cache = (await api.get<{ users: Recipient[] }>('/share/recipients')).users
    recent.value = cache
  } catch {
    /* подсказки просто не покажем */
  }
})

/** Значение, которое чип подставляет в поле. */
function refOf(u: Recipient): string {
  return u.username ? '@' + u.username : String(u.id)
}

function label(u: Recipient): string {
  return u.first_name || (u.username ? '@' + u.username : `#${u.id}`)
}

const selected = computed(() => model.value.trim())

function pick(u: Recipient) {
  model.value = selected.value === refOf(u) ? '' : refOf(u)
}
</script>

<template>
  <input v-model="model" class="rp-input" placeholder="id или @логин получателя" spellcheck="false" />
  <div v-if="recent.length" class="rp-recent">
    <span class="rp-hint">Уже делились:</span>
    <button
      v-for="u in recent"
      :key="u.id"
      type="button"
      class="rp-chip"
      :class="{ on: selected === refOf(u) }"
      @click="pick(u)"
    >
      {{ label(u) }}
    </button>
  </div>
</template>

<style scoped>
.rp-input {
  display: block;
  width: 100%;
  margin-top: 8px;
}

.rp-recent {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
}

.rp-hint {
  font-size: 11px;
  color: var(--text-secondary);
}

.rp-chip {
  background: var(--bg-secondary);
  border: 1px solid transparent;
  border-radius: 12px;
  padding: 5px 11px;
  font-size: 12px;
  color: var(--text-color);
}

.rp-chip.on {
  border-color: var(--accent-color);
  background: var(--accent-color);
  color: #fff;
}
</style>
