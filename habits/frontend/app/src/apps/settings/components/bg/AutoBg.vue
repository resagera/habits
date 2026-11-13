<script setup lang="ts">
// Автосмена фона: откуда брать картинку и как часто.
//
// Источник выбирается отдельно для тёмной и светлой темы — одним механизмом
// получаются и «каждый день новый фон», и «на тёмной теме одни картинки,
// на светлой другие».
import { computed } from 'vue'
import type { BackgroundAuto } from '../../../../shared/background'
import type { BgFolder } from './types'

const props = defineProps<{ folders: BgFolder[]; auto: BackgroundAuto; busy?: boolean }>()
const emit = defineEmits<{ save: [value: BackgroundAuto]; now: [] }>()

const modes: { id: BackgroundAuto['mode']; label: string }[] = [
  { id: 'off', label: 'Выключена' },
  { id: 'open', label: 'При каждом открытии' },
  { id: 'daily', label: 'Раз в день' },
]

/** -1 — все картинки, 0 — «без папки», иначе id папки. */
const options = computed(() => [
  { value: -1, label: 'Все картинки' },
  { value: 0, label: 'Без папки' },
  ...props.folders.map((f) => ({
    value: f.id, label: (f.parent_id ? '└ ' : '') + f.name,
  })),
])

function setMode(mode: BackgroundAuto['mode']) {
  emit('save', { ...props.auto, mode })
}

function setFolder(which: 'folder_dark' | 'folder_light', value: string) {
  emit('save', { ...props.auto, [which]: Number(value) })
}

const sameFolder = computed(() => props.auto.folder_dark === props.auto.folder_light)
</script>

<template>
  <h4>Автосмена фона</h4>
  <div class="row">
    <button v-for="m in modes" :key="m.id" class="btn small"
            :class="{ primary: auto.mode === m.id }" :disabled="busy"
            @click="setMode(m.id)">
      {{ m.label }}
    </button>
  </div>

  <template v-if="auto.mode !== 'off'">
    <label class="pick">
      <span>Картинки для тёмной темы</span>
      <select :value="auto.folder_dark" :disabled="busy"
              @change="setFolder('folder_dark', ($event.target as HTMLSelectElement).value)">
        <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
      </select>
    </label>
    <label class="pick">
      <span>Картинки для светлой темы</span>
      <select :value="auto.folder_light" :disabled="busy"
              @change="setFolder('folder_light', ($event.target as HTMLSelectElement).value)">
        <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
      </select>
    </label>

    <p class="hint">
      Берутся картинки, которые лежат прямо в выбранной папке (вложенные — нет).
      <template v-if="!sameFolder">
        Папки разные, поэтому фон сменится и при переключении темы.
      </template>
      <template v-if="auto.day">Последняя смена: {{ auto.day }}.</template>
    </p>
    <div class="row">
      <button class="btn small" :disabled="busy" @click="$emit('now')">Сменить сейчас</button>
    </div>
  </template>
</template>

<style scoped>
h4 {
  margin: 14px 0 6px;
  font-size: 15px;
}

.row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin: 8px 0;
}

.btn {
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  cursor: pointer;
  font-size: 14px;
  padding: 8px 12px;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn.small {
  font-size: 13px;
  padding: 6px 10px;
}

.btn:disabled {
  opacity: 0.6;
}

.pick {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 13px;
  margin: 8px 0;
}

.pick select {
  flex: 1;
  min-width: 0;
  max-width: 55%;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  padding: 7px 8px;
  font-size: 13px;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 6px 0;
}
</style>
