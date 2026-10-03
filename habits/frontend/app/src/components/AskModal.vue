<script setup lang="ts">
// Окно ввода строки (см. shared/ask.ts). Одно на приложение, живёт в App.vue.
import { nextTick, ref, watch } from 'vue'
import { answerAsk, askState } from '../shared/ask'

const input = ref<HTMLInputElement>()

// фокус ставим после отрисовки: на телефоне это сразу поднимает клавиатуру
watch(askState, async (v) => {
  if (!v) return
  await nextTick()
  input.value?.focus()
  input.value?.select()
})
</script>

<template>
  <Teleport to="body">
    <div v-if="askState" class="ask" @click.self="answerAsk(null)">
      <div class="ask-box">
        <h4>{{ askState.title }}</h4>
        <input
          ref="input"
          v-model="askState.value"
          :placeholder="askState.placeholder"
          @keyup.enter="answerAsk(askState.value)"
          @keyup.esc="answerAsk(null)"
        />
        <div class="ask-row">
          <button class="btn" @click="answerAsk(null)">Отмена</button>
          <button class="btn primary" :disabled="!askState.value.trim()"
                  @click="answerAsk(askState.value)">
            {{ askState.okLabel }}
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.ask {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  z-index: 2500;
}

.ask-box {
  background: var(--bg-color);
  border-radius: 12px;
  padding: 14px;
  width: 100%;
  max-width: 360px;
}

.ask-box h4 {
  margin: 0 0 10px;
  font-size: 15px;
}

.ask-box input {
  width: 100%;
  box-sizing: border-box;
  background: var(--bg-secondary, var(--card-color));
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  padding: 10px;
  font-size: 15px;
}

.ask-row {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

.btn {
  flex: 1;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  cursor: pointer;
  font-size: 14px;
  padding: 10px;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn:disabled {
  opacity: 0.6;
}
</style>
