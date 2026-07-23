<script setup lang="ts">
import { ref } from 'vue'
import { PRESET_COLORS } from '../display'

const props = defineProps<{
  current: string
  recent: string[]
}>()

const emit = defineEmits<{
  pick: [color: string]
  close: []
}>()

const custom = ref(props.current)

function pick(color: string) {
  emit('pick', color)
  emit('close')
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content picker">
      <h3>Активный цвет</h3>

      <div v-if="recent.length" class="section">
        <div class="section-title">Последние</div>
        <div class="swatches">
          <button
            v-for="c in recent"
            :key="'r' + c"
            class="swatch"
            :class="{ on: c === current }"
            :style="{ backgroundColor: c }"
            @click="pick(c)"
          ></button>
        </div>
      </div>

      <div class="section">
        <div class="section-title">Палитра</div>
        <div class="swatches">
          <button
            v-for="c in PRESET_COLORS"
            :key="c"
            class="swatch"
            :class="{ on: c === current }"
            :style="{ backgroundColor: c }"
            @click="pick(c)"
          ></button>
        </div>
      </div>

      <div class="section custom-line">
        <input v-model="custom" type="color" class="color-input" />
        <button class="btn primary" @click="pick(custom)">Выбрать этот цвет</button>
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

.swatches {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.swatch {
  width: 34px;
  height: 34px;
  border-radius: 8px;
  border: 2px solid transparent;
  cursor: pointer;
  padding: 0;
}

.swatch.on {
  border-color: var(--text-color);
}

.custom-line {
  display: flex;
  gap: 8px;
  align-items: center;
}

.color-input {
  width: 46px;
  height: 40px;
  flex: none;
  padding: 2px;
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
</style>
