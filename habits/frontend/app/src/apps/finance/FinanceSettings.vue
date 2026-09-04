<script setup lang="ts">
// Настройки страницы Finance: базовая валюта сводки, час напоминаний,
// скрытие сумм. Открывается шестерёнкой в шапке.
import { ref } from 'vue'
import { showToast } from '../../shared/toast'
import { fetchFinanceSettings, saveFinanceSettings } from './api'

const loading = ref(true)
const base = ref('amd')
const hour = ref(10)
const hideAmounts = ref(false)

async function load() {
  try {
    const s = await fetchFinanceSettings()
    base.value = s.base_currency
    hour.value = s.notify_hour
    hideAmounts.value = s.hide_amounts
  } catch {
    showToast('Не удалось загрузить настройки')
  } finally {
    loading.value = false
  }
}
void load()

async function save() {
  try {
    await saveFinanceSettings({
      base_currency: base.value.trim().toLowerCase(),
      notify_hour: hour.value,
      hide_amounts: hideAmounts.value,
    })
    showToast('Сохранено ✅')
  } catch {
    showToast('Не удалось сохранить')
  }
}
</script>

<template>
  <div v-if="!loading" class="pane">
    <h4>Базовая валюта</h4>
    <p class="hint">
      К ней приводятся суммы в сводке. Курс берётся тот же, что на странице
      Converter; в истории платежей курс фиксируется на дату оплаты, поэтому
      прошлые месяцы не меняются задним числом.
    </p>
    <input v-model="base" class="in" placeholder="amd" @change="save" />

    <h4>Напоминания</h4>
    <p class="hint">
      Во сколько присылать напоминание о платеже (по вашему времени). За
      сколько дней предупреждать — настраивается у каждой траты отдельно.
      Автоплатежи заранее не беспокоят.
    </p>
    <input v-model.number="hour" class="in" type="number" min="0" max="23" @change="save" />

    <h4>Приватность</h4>
    <label class="chk">
      <input v-model="hideAmounts" type="checkbox" @change="save" />
      <span>Скрывать суммы (показывать «•••»)</span>
    </label>
    <p class="hint">Пригодится, когда экран видит кто-то ещё. Переключается и с самой страницы.</p>
  </div>
</template>

<style scoped>
.pane {
  font-size: 14px;
}

h4 {
  margin: 12px 0 4px;
  font-size: 15px;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 4px 0 8px;
}

.in {
  width: 100%;
  background: var(--bg-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 15px;
  padding: 9px 10px;
}

.chk {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
  cursor: pointer;
}
</style>
