<script setup lang="ts">
// «Уведомления от бота»: какие рассылки приходят и в какие часы бот молчит.
//
// Тихие часы не глушат сообщение, а откладывают его: напоминание, которое
// просто не пришло, — потерянное напоминание. Досылает очередь на сервере.
import { onMounted, ref } from 'vue'
import { api } from '../../../shared/api/client'
import { showToast } from '../../../shared/toast'

interface Prefs {
  quiet_enabled: boolean
  quiet_from: number
  quiet_to: number
  tz_off: number
  off: string[]
}

/** Виды знает приложение: сервер их не перечисляет, чтобы не мигрировать базу. */
const KINDS: { id: string; label: string; hint: string }[] = [
  { id: 'reminder', label: 'Напоминания', hint: 'страница «Напоминания» и отметки в Tracker' },
  { id: 'checker', label: 'Чек-листы', hint: 'сброс списков и напоминания по пунктам' },
  { id: 'tasks', label: 'Задачи', hint: 'сроки и общие проекты' },
  { id: 'finance', label: 'Платежи', hint: 'предстоящие оплаты из Finance' },
  { id: 'servers', label: 'Серверы', hint: 'сервер пропал со связи, тревоги агентов' },
  { id: 'automation', label: 'Автоматизация', hint: 'результаты запусков (заказ воды)' },
  { id: 'mail', label: 'Почта и чеки', hint: 'письма на resager.ru и разобранные чеки' },
]

const prefs = ref<Prefs>({
  quiet_enabled: false, quiet_from: 23 * 60, quiet_to: 8 * 60,
  tz_off: -new Date().getTimezoneOffset(), off: [],
})
const busy = ref(false)

/**
 * Время в полях — своё состояние, а не вычисляемое из prefs. Раньше поле
 * писало в prefs на каждый набранный символ, ответ сервера возвращался и
 * перерисовывал значение — браузер сбрасывал курсор на первый сегмент, и
 * второй символ было уже не напечатать.
 */
const fromTime = ref('23:00')
const toTime = ref('08:00')

onMounted(async () => {
  try {
    const p = await api.get<Prefs>('/settings/notifications')
    // часовой пояс всегда берём у устройства: человек переезжает, настройка нет
    prefs.value = { ...p, off: p.off ?? [], tz_off: -new Date().getTimezoneOffset() }
    fromTime.value = toHHMM(prefs.value.quiet_from)
    toTime.value = toHHMM(prefs.value.quiet_to)
  } catch {
    /* нет сети — остаются значения по умолчанию */
  }
})

function toHHMM(minutes: number): string {
  const h = String(Math.floor(minutes / 60)).padStart(2, '0')
  const m = String(minutes % 60).padStart(2, '0')
  return `${h}:${m}`
}

function fromHHMM(value: string): number {
  const [h, m] = value.split(':').map(Number)
  return (h || 0) * 60 + (m || 0)
}

/**
 * Сохраняем по «change» и по уходу из поля, а недописанное время пропускаем:
 * пока набран только час, браузер отдаёт пустую строку.
 */
function saveTimes() {
  if (!fromTime.value || !toTime.value) return
  const from = fromHHMM(fromTime.value)
  const to = fromHHMM(toTime.value)
  if (from === prefs.value.quiet_from && to === prefs.value.quiet_to) return
  return save({ quiet_from: from, quiet_to: to })
}

async function save(patch: Partial<Prefs>) {
  const next = { ...prefs.value, ...patch }
  prefs.value = next
  busy.value = true
  try {
    prefs.value = await api.put<Prefs>('/settings/notifications', next)
  } catch {
    showToast('Не удалось сохранить настройки уведомлений')
  } finally {
    busy.value = false
  }
}

function toggleKind(id: string, on: boolean) {
  const off = on ? prefs.value.off.filter((k) => k !== id) : [...prefs.value.off, id]
  return save({ off })
}
</script>

<template>
  <p class="hint">
    Здесь только то, что бот присылает сам. Сообщения, которые вы отправляете
    кнопкой «в бот», приходят всегда.
  </p>

  <label class="chk">
    <input type="checkbox" :checked="prefs.quiet_enabled" :disabled="busy"
           @change="save({ quiet_enabled: ($event.target as HTMLInputElement).checked })" />
    <span>Тихие часы</span>
  </label>

  <div v-if="prefs.quiet_enabled" class="times">
    <label>с <input v-model="fromTime" type="time" @change="saveTimes" @blur="saveTimes" /></label>
    <label>до <input v-model="toTime" type="time" @change="saveTimes" @blur="saveTimes" /></label>
  </div>
  <p v-if="prefs.quiet_enabled" class="hint">
    В эти часы бот молчит, а накопленное пришлёт, когда они закончатся —
    ничего не теряется. Время берётся по часам вашего устройства.
  </p>

  <p class="sub-title">Что присылать</p>
  <label v-for="k in KINDS" :key="k.id" class="chk row">
    <input type="checkbox" :checked="!prefs.off.includes(k.id)" :disabled="busy"
           @change="toggleKind(k.id, ($event.target as HTMLInputElement).checked)" />
    <span>
      {{ k.label }}
      <i class="hint">— {{ k.hint }}</i>
    </span>
  </label>
</template>

<style scoped>
.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

i.hint {
  font-style: normal;
  display: inline;
}

.chk {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 0;
  font-size: 15px;
  cursor: pointer;
}

.chk.row {
  align-items: flex-start;
  font-size: 14px;
}

.times {
  display: flex;
  gap: 12px;
  margin: 6px 0 8px;
  font-size: 14px;
}

.times input {
  margin-left: 4px;
}

.sub-title {
  margin: 12px 0 4px;
  font-size: 14px;
}
</style>
