<script setup lang="ts">
import { ref } from 'vue'
import { showToast } from '../../../shared/toast'
import * as autoApi from '../api'
import { PAYMENT_LABELS, type Automation } from '../types'

const props = defineProps<{ automation: Automation | null }>()
const emit = defineEmits<{ saved: []; close: [] }>()

// next_run_at приходит ISO; для <input type=datetime-local> нужен локальный формат
function toLocalInput(iso: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
}

const a = props.automation
const form = ref({
  title: a?.title ?? 'Заказ воды jur.am',
  enabled: a?.enabled ?? true,
  interval_days: a?.interval_days ?? 10,
  quantity: a?.config.quantity ?? 14,
  tare_mode: (a?.config.tare_mode ?? 'auto') as 'auto' | 'fixed',
  tare_qty: a?.config.tare_qty ?? 14,
  time_slot: a?.config.time_slot ?? 'first',
  payment: a?.config.payment ?? 'checkmo',
  comment: a?.config.comment ?? '',
  schedule: !!a?.next_run_at,
  next_local: toLocalInput(a?.next_run_at ?? null),
})
const login = ref('')
const password = ref('')
const saving = ref(false)
const confirmDel = ref(false)

async function save() {
  if (!form.value.title.trim()) {
    showToast('Укажите название')
    return
  }
  if (!props.automation && (!login.value.trim() || !password.value)) {
    showToast('Укажите логин и пароль сайта')
    return
  }
  saving.value = true
  try {
    const payload: autoApi.AutomationPayload = {
      title: form.value.title,
      enabled: form.value.enabled,
      interval_days: form.value.interval_days,
      quantity: form.value.quantity,
      tare_mode: form.value.tare_mode,
      tare_qty: form.value.tare_qty,
      time_slot: form.value.time_slot,
      payment: form.value.payment,
      comment: form.value.comment,
      next_run_at:
        form.value.schedule && form.value.next_local
          ? new Date(form.value.next_local).toISOString()
          : '',
    }
    if (login.value.trim()) payload.login = login.value.trim()
    if (password.value) payload.password = password.value
    if (props.automation) await autoApi.updateAutomation(props.automation.id, payload)
    else await autoApi.createAutomation(payload)
    emit('saved')
    showToast('Сохранено ✅')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    saving.value = false
  }
}

async function del() {
  if (!props.automation) return
  if (!confirmDel.value) {
    confirmDel.value = true
    return
  }
  try {
    await autoApi.deleteAutomation(props.automation.id)
    emit('saved')
    showToast('Удалено')
  } catch {
    showToast('Не удалось удалить')
  }
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content auto-form">
      <h3>{{ automation ? '✏️ Автоматизация' : '🤖 Новая автоматизация' }}</h3>
      <p class="sub">Заказ воды на jur.am — «Макур Джур 19л»</p>

      <input v-model="form.title" placeholder="Название" maxlength="200" />

      <h4>Доступ к сайту jur.am</h4>
      <input
        v-model="login"
        :placeholder="automation ? automation.config.login + ' (оставьте пустым — не менять)' : 'Логин (email)'"
        autocomplete="off"
        spellcheck="false"
      />
      <input
        v-model="password"
        type="password"
        :placeholder="automation ? 'Пароль (пусто — не менять)' : 'Пароль'"
        autocomplete="new-password"
      />
      <p class="hint">Пароль хранится на сервере в зашифрованном виде и наружу не отдаётся.</p>

      <h4>Заказ</h4>
      <label class="fld">
        <span>Бутылей «19л»</span>
        <input v-model.number="form.quantity" type="number" min="1" max="100" />
      </label>
      <label class="fld">
        <span>Возвратная тара</span>
        <select v-model="form.tare_mode">
          <option value="auto">Автоматически (сколько числится долга)</option>
          <option value="fixed">Фиксированное число</option>
        </select>
      </label>
      <label v-if="form.tare_mode === 'fixed'" class="fld">
        <span>Сдать тары</span>
        <input v-model.number="form.tare_qty" type="number" min="0" max="100" />
      </label>
      <label class="fld">
        <span>Время доставки</span>
        <input v-model="form.time_slot" placeholder="first — первое доступное, или напр. 12:00:00" />
      </label>
      <label class="fld">
        <span>Оплата</span>
        <select v-model="form.payment">
          <option v-for="(label, k) in PAYMENT_LABELS" :key="k" :value="k">{{ label }}</option>
        </select>
      </label>
      <input v-model="form.comment" placeholder="Комментарий к доставке (необязательно)" maxlength="500" />

      <h4>Расписание</h4>
      <label class="check">
        <input v-model="form.enabled" type="checkbox" />
        Включена
      </label>
      <label class="check">
        <input v-model="form.schedule" type="checkbox" />
        По расписанию (иначе только вручную)
      </label>
      <template v-if="form.schedule">
        <label class="fld">
          <span>Первый запуск</span>
          <input v-model="form.next_local" type="datetime-local" />
        </label>
        <label class="fld">
          <span>Повтор каждые, дней</span>
          <input v-model.number="form.interval_days" type="number" min="1" max="365" />
        </label>
        <p class="hint">
          За сутки и за час до запуска придёт уведомление в бот — можно успеть остановить.
        </p>
      </template>

      <button class="btn primary" :disabled="saving" @click="save">
        {{ saving ? 'Сохраняем…' : '💾 Сохранить' }}
      </button>
      <button v-if="automation" class="btn danger" @click="del">
        {{ confirmDel ? 'Точно удалить?' : '🗑 Удалить' }}
      </button>
      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.auto-form {
  text-align: left;
  max-height: 88vh;
  overflow-y: auto;
}

.auto-form h3 {
  text-align: center;
  margin-bottom: 2px;
}

.auto-form h4 {
  margin: 14px 0 4px;
  font-size: 14px;
}

.sub {
  text-align: center;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 8px;
}

.auto-form input,
.auto-form select {
  width: 100%;
  margin-top: 6px;
}

.fld span {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 6px;
}

.check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  margin-top: 8px;
}

.check input {
  width: auto;
  margin: 0;
}

.hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 6px 0 0;
}

.btn {
  display: block;
  width: 100%;
  margin-top: 10px;
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

.btn.danger {
  background: #b91c1c;
  color: #fff;
}

.btn:disabled {
  opacity: 0.5;
}
</style>
