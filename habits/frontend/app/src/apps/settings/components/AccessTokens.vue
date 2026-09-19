<script setup lang="ts">
// Токены доступа: вход в приложение вне Telegram (веб-версия, расширение
// браузера). Сам токен виден ОДИН раз — при создании; в БД лежит только его
// хэш. Токен не даёт админ-прав и не может выпускать новые токены.
import { onMounted, ref } from 'vue'
import { api } from '../../../shared/api/client'
import { showToast } from '../../../shared/toast'

interface AccessToken {
  id: number
  name: string
  prefix: string
  expires_at?: string
  last_used_at?: string
  last_device: string
  created_at: string
}

const tokens = ref<AccessToken[]>([])
const loading = ref(true)
const open = ref(false)
const creating = ref(false)
const form = ref({ name: '', expires_days: 0 })
/** Показывается один раз сразу после создания. */
const fresh = ref('')
const confirmRevoke = ref<number | null>(null)

onMounted(load)

async function load() {
  loading.value = true
  try {
    tokens.value = (await api.get<{ tokens: AccessToken[] }>('/settings/tokens')).tokens
  } catch {
    /* вне Telegram/нет сети — блок останется пустым */
  } finally {
    loading.value = false
  }
}

async function create() {
  creating.value = true
  try {
    const res = await api.post<{ token: string }>('/settings/tokens', {
      name: form.value.name.trim(),
      expires_days: form.value.expires_days,
    })
    fresh.value = res.token
    form.value.name = ''
    await load()
  } catch (e) {
    showToast(e instanceof Error && e.message ? e.message : 'Не удалось создать токен')
  } finally {
    creating.value = false
  }
}

async function copyFresh() {
  try {
    await navigator.clipboard.writeText(fresh.value)
    showToast('Токен скопирован')
  } catch {
    showToast('Не удалось скопировать')
  }
}

async function revoke(t: AccessToken) {
  if (confirmRevoke.value !== t.id) {
    confirmRevoke.value = t.id
    setTimeout(() => {
      if (confirmRevoke.value === t.id) confirmRevoke.value = null
    }, 3500)
    return
  }
  confirmRevoke.value = null
  try {
    await api.delete(`/settings/tokens/${t.id}`)
    tokens.value = tokens.value.filter((x) => x.id !== t.id)
    showToast('Токен отозван')
  } catch {
    showToast('Не удалось отозвать')
  }
}

/** Адрес веб-версии — тот же origin, что у приложения. */
const webUrl = location.origin + import.meta.env.BASE_URL
/**
 * Файлы расширения отдаёт сервер. Chrome — zip, который распаковывают и
 * грузят в режиме разработчика. Firefox — подписанный Mozilla .xpi: он
 * ставится постоянно (неподписанное дополнение Firefox снимает при
 * перезапуске) и обновляется сам.
 */
const chromeZip = webUrl + 'ext/habits-chrome.zip'
const firefoxXpi = webUrl + 'ext/habits-firefox.xpi'

async function copyWebUrl() {
  try {
    await navigator.clipboard.writeText(webUrl)
    showToast('Ссылка скопирована')
  } catch {
    showToast('Не удалось скопировать')
  }
}

function fmt(iso?: string): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}
</script>

<template>
  <section class="section">
    <button class="collapse-head" @click="open = !open">
      <h3>Токены доступа</h3>
      <span class="chev">{{ open ? '▾' : '▸' }}</span>
    </button>

    <div v-show="open" class="body">
      <p class="hint">
        Вход в приложение вне Telegram — в браузере или расширении. Токен открывает
        доступ к вашему аккаунту, поэтому не передавайте его никому. Админ-раздел и
        выпуск новых токенов по токену недоступны — только из Telegram.
      </p>

      <details class="where">
        <summary>Где использовать токен</summary>
        <p class="hint">
          <b>🌐 Браузер.</b> Откройте адрес приложения и вставьте токен на экране входа:
        </p>
        <pre class="link" @click="copyWebUrl">{{ webUrl }}</pre>
        <p class="hint">
          <b>🧩 Расширение — попап с приложением в панели браузера.</b>
          Скачайте файл для своего браузера:
        </p>
        <div class="row">
          <a class="btn dl" :href="chromeZip" download>⬇️ Для Chrome</a>
          <a class="btn dl" :href="firefoxXpi">⬇️ Для Firefox</a>
        </div>
        <p class="hint">
          <b>Chrome:</b> распакуйте архив в любую папку, затем
          <code>chrome://extensions</code> → включить «Режим разработчика»
          → «Загрузить распакованное расширение» → выбрать эту папку.<br />
          <b>Firefox:</b> файл подписан Mozilla — просто откройте его в браузере
          и подтвердите установку (или <code>about:addons</code> → шестерёнка →
          «Установить дополнение из файла»). Ставится постоянно, перезапуск
          браузера его не снимает, обновления приходят сами.
        </p>
        <p class="hint">
          Файлы собирает сервер, адрес этого приложения уже внутри. Интерфейс
          расширение грузит с сервера — переустанавливать его при обновлениях
          приложения не нужно.
        </p>
      </details>

      <!-- показ созданного токена: единственный раз -->
      <div v-if="fresh" class="fresh">
        <p class="hint warn">Скопируйте токен сейчас — увидеть его снова будет нельзя.</p>
        <pre class="token" @click="copyFresh">{{ fresh }}</pre>
        <div class="row">
          <button class="btn" @click="copyFresh">📋 Копировать</button>
          <button class="btn" @click="fresh = ''">Готово</button>
        </div>
      </div>

      <template v-else>
        <input v-model="form.name" class="full-w" placeholder="Название (например, Chrome на ноутбуке)" />
        <label class="lbl">Срок действия</label>
        <select v-model.number="form.expires_days" class="full-w">
          <option :value="0">Бессрочно</option>
          <option :value="30">30 дней</option>
          <option :value="90">90 дней</option>
          <option :value="365">1 год</option>
        </select>
        <button class="btn primary" :disabled="creating" @click="create">
          {{ creating ? 'Создание…' : '＋ Создать токен' }}
        </button>
      </template>

      <p v-if="loading" class="hint">Загрузка…</p>
      <p v-else-if="!tokens.length" class="hint">Токенов пока нет.</p>
      <div v-for="t in tokens" :key="t.id" class="tok">
        <div class="tok-top">
          <span class="tok-name">{{ t.name }}</span>
          <code class="tok-prefix">{{ t.prefix }}…</code>
        </div>
        <div class="tok-sub">
          Использован: {{ fmt(t.last_used_at) }}
          <template v-if="t.last_device"> · {{ t.last_device }}</template>
          <template v-if="t.expires_at"> · до {{ fmt(t.expires_at) }}</template>
        </div>
        <button class="btn danger slim" @click="revoke(t)">
          {{ confirmRevoke === t.id ? 'Точно отозвать?' : 'Отозвать' }}
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.section {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 14px;
}

.collapse-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  background: none;
  border: none;
  padding: 0;
  color: var(--text-color);
  cursor: pointer;
}

.collapse-head h3 {
  margin: 0;
  font-size: 16px;
}

.chev {
  font-size: 14px;
  color: var(--text-secondary);
}

.body {
  margin-top: 12px;
}

.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.hint.warn {
  color: #f59e0b;
}

.lbl {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 6px 0 4px;
}

.full-w {
  width: 100%;
  margin-bottom: 8px;
  font: inherit;
  background: var(--bg-secondary);
  color: var(--text-color);
  border: 1px solid var(--hover-bg-color);
  border-radius: 6px;
  padding: 8px;
}

.where {
  margin-bottom: 12px;
  font-size: 12px;
}

.where summary {
  color: var(--accent-color);
  cursor: pointer;
  margin-bottom: 6px;
}

.where a {
  color: var(--accent-color);
  word-break: break-all;
}

.where code {
  background: var(--bg-secondary);
  padding: 1px 4px;
  border-radius: 4px;
}

.btn.dl {
  display: block;
  text-align: center;
  text-decoration: none;
  font-size: 13px;
  padding: 9px;
  margin-bottom: 8px;
}

.link {
  background: var(--bg-secondary);
  border-radius: 6px;
  padding: 8px 10px;
  font-family: ui-monospace, monospace;
  font-size: 11px;
  word-break: break-all;
  white-space: pre-wrap;
  cursor: pointer;
  margin: 0 0 8px;
}

.token {
  background: var(--bg-secondary);
  border-radius: 6px;
  padding: 10px;
  font-family: ui-monospace, monospace;
  font-size: 12px;
  word-break: break-all;
  white-space: pre-wrap;
  cursor: pointer;
  margin: 0 0 8px;
}

.row {
  display: flex;
  gap: 8px;
}

.btn {
  flex: 1;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
  width: 100%;
}

.btn.danger {
  background: #b91c1c;
  color: #fff;
}

.btn.slim {
  flex: none;
  padding: 6px 12px;
  font-size: 13px;
}

.btn:disabled {
  opacity: 0.5;
}

.tok {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 10px;
  margin-top: 8px;
}

.tok-top {
  display: flex;
  align-items: center;
  gap: 8px;
  justify-content: space-between;
}

.tok-name {
  font-size: 14px;
}

.tok-prefix {
  font-size: 11px;
  color: var(--text-secondary);
}

.tok-sub {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 4px 0 8px;
}
</style>
