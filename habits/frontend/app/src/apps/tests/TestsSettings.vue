<script setup lang="ts">
// Индивидуальные настройки страницы «Тесты». Открывается из шестерёнки в шапке.
import { ref } from 'vue'
import { showToast } from '../../shared/toast'
import { fetchTestsSettings, saveTestsSettings } from './api'

const passStreak = ref(1)
const loading = ref(true)

async function load() {
  try {
    passStreak.value = (await fetchTestsSettings()).pass_streak
  } catch {
    showToast('Не удалось загрузить настройки')
  } finally {
    loading.value = false
  }
}
void load()

async function save(n: number) {
  passStreak.value = n
  try {
    await saveTestsSettings(n)
    showToast('Сохранено ✅')
  } catch {
    showToast('Не удалось сохранить')
  }
}
</script>

<template>
  <div v-if="!loading" class="pane">
    <h4>Когда вопрос считается пройденным</h4>
    <p class="hint">
      Пройденные вопросы не попадают в набор «Продолжить». Чем выше порог, тем
      меньше шансов, что ответ просто угадан или заучен с первого раза.
    </p>
    <div class="row">
      <button v-for="n in 3" :key="n" class="opt" :class="{ on: passStreak === n }"
              @click="save(n)">
        {{ n }} {{ n === 1 ? 'верный ответ' : 'верных подряд' }}
      </button>
    </div>
    <p class="hint">
      Ошибка всегда сбрасывает серию и возвращает вопрос в набор.
    </p>
  </div>
</template>

<style scoped>
.pane {
  font-size: 14px;
}

h4 {
  margin: 0 0 6px;
  font-size: 15px;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 6px 0;
}

.row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin: 10px 0;
}

.opt {
  flex: 1;
  min-width: 110px;
  background: var(--bg-color);
  border: 2px solid transparent;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 8px 10px;
  cursor: pointer;
}

.opt.on {
  border-color: var(--accent-color);
}
</style>
