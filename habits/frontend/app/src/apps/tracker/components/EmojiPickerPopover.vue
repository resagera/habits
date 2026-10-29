<script setup lang="ts">
import { ref } from 'vue'
import { PRESET_EMOJI } from '../display'

defineProps<{
  current: string
  recent: string[]
}>()

const emit = defineEmits<{
  pick: [emoji: string]
  close: []
}>()

const custom = ref('')

function pick(emoji: string) {
  const e = emoji.trim()
  if (!e) return
  emit('pick', e)
  emit('close')
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content picker">
      <h3>Активный эмодзи</h3>

      <div v-if="recent.length" class="section">
        <div class="section-title">Последние</div>
        <div class="grid">
          <button
            v-for="e in recent"
            :key="'r' + e"
            class="cell"
            :class="{ on: e === current }"
            @click="pick(e)"
          >
            {{ e }}
          </button>
        </div>
      </div>

      <div class="section">
        <div class="section-title">Часто используемые</div>
        <div class="grid">
          <button
            v-for="e in PRESET_EMOJI"
            :key="e"
            class="cell"
            :class="{ on: e === current }"
            @click="pick(e)"
          >
            {{ e }}
          </button>
        </div>
      </div>

      <div class="section custom-line">
        <input v-model="custom" type="text" maxlength="8" placeholder="Свой эмодзи…" />
        <button class="btn primary" :disabled="!custom.trim()" @click="pick(custom)">OK</button>
      </div>

      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.picker {
  text-align: left;
}

.picker h3 {
  text-align: center;
}

.section {
  margin-bottom: 12px;
}

.section-title {
  font-size: 12px;
  color: var(--text-secondary);
  margin-bottom: 6px;
}

.grid {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.cell {
  width: 38px;
  height: 38px;
  font-size: 20px;
  background: var(--bg-secondary);
  border: 2px solid transparent;
  border-radius: 8px;
  padding: 0;
  cursor: pointer;
}

.cell.on {
  border-color: var(--accent-color);
}

.custom-line {
  display: flex;
  gap: 8px;
}

.custom-line input {
  flex: 1;
  min-width: 0;
}

.custom-line .btn {
  width: auto;
  padding: 10px 16px;
}

.btn {
  display: block;
  width: 100%;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn:disabled {
  opacity: 0.5;
}
</style>
