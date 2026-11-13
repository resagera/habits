<script setup lang="ts">
// Перенос оформления между аккаунтами и устройствами: тема, своя тема,
// сохранённые темы, эффекты фона, скрытые страницы и закреплённые заголовки —
// одной строкой JSON.
//
// Картинки не переносятся: файл принадлежит аккаунту, и в чужом его нет.
// Переезжают настройки поверх картинки (размещение, размытие, затемнение), а
// ссылка на картинку — только если фон был задан ссылкой.
import { ref } from 'vue'
import { api } from '../../../shared/api/client'
import { readClipboardText } from '../../../shared/clipboard'
import { confirmAction } from '../../../shared/telegram'
import { showToast } from '../../../shared/toast'
import {
  loadAppearance, placement, savePlacement, savedThemes, setAppearanceState, state,
  type AppearanceState, type BackgroundPlacement,
} from '../../../shared/appearance'
import { loadBackground, setBackground, type BgPosition } from '../../../shared/background'
import { menuHidden, setHiddenPages } from '../../../shared/pageOrder'
import { pinAllHeaders, pinnedPages, setHeaderPinned, setPinAllHeaders } from '../../../shared/pinnedHeader'

interface Bundle {
  app: string
  kind: string
  version: number
  created: string
  state: AppearanceState
  themes: { name: string; kind: string; tokens: unknown }[]
  placement: BackgroundPlacement
  background: { position: BgPosition; blur: number; dim: number; url: string }
  hidden_pages: string[]
  pinned_pages: string[]
  pin_all_headers: boolean
}

const busy = ref(false)
const text = ref('')
const showPaste = ref(false)

async function collect(): Promise<Bundle> {
  const bg = await loadBackground()
  return {
    app: 'habits',
    kind: 'appearance',
    version: 1,
    created: new Date().toISOString(),
    state: state.value,
    themes: savedThemes.value.map((t) => ({ name: t.name, kind: t.kind, tokens: t.tokens })),
    placement: placement.value,
    background: {
      position: bg?.position ?? 'cover',
      blur: bg?.blur ?? 0,
      dim: bg?.dim ?? 0,
      // имя файла чужому аккаунту ничего не даст, ссылка — даст
      url: bg?.kind === 'url' ? bg.url : '',
    },
    hidden_pages: [...menuHidden.value],
    pinned_pages: [...pinnedPages.value],
    pin_all_headers: pinAllHeaders.value,
  }
}

async function copyAll() {
  busy.value = true
  try {
    const json = JSON.stringify(await collect())
    await navigator.clipboard.writeText(json)
    showToast('Настройки скопированы')
  } catch {
    // буфер в вебвью Telegram бывает закрыт — тогда показываем текст руками
    text.value = JSON.stringify(await collect())
    showPaste.value = true
    showToast('Скопируйте текст из поля ниже')
  } finally {
    busy.value = false
  }
}

async function saveFile() {
  busy.value = true
  try {
    const json = JSON.stringify(await collect(), null, 2)
    const url = URL.createObjectURL(new Blob([json], { type: 'application/json' }))
    const a = document.createElement('a')
    a.href = url
    a.download = 'habits-appearance.json'
    a.click()
    setTimeout(() => URL.revokeObjectURL(url), 5000)
  } finally {
    busy.value = false
  }
}

async function pasteFromClipboard() {
  showPaste.value = true
  const fromClipboard = await readClipboardText()
  if (fromClipboard) text.value = fromClipboard
}

function onFile(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    text.value = String(reader.result ?? '')
    showPaste.value = true
  }
  reader.readAsText(file)
}

/** Применение: каждая часть отдельной ручкой, пропущенное просто не трогаем. */
async function applyBundle() {
  let data: Partial<Bundle>
  try {
    data = JSON.parse(text.value)
  } catch {
    showToast('Это не похоже на настройки Habits')
    return
  }
  if (data.app !== 'habits' || data.kind !== 'appearance') {
    showToast('Это не похоже на настройки Habits')
    return
  }
  busy.value = true
  try {
    if (data.state) await setAppearanceState(data.state)
    for (const t of data.themes ?? []) {
      await api.post('/appearance/themes', { name: t.name, kind: t.kind, tokens: t.tokens })
    }
    if (data.placement) await savePlacement(data.placement)
    if (data.background) await applyBackground(data.background)
    if (data.hidden_pages) await setHiddenPages(data.hidden_pages)
    if (typeof data.pin_all_headers === 'boolean') setPinAllHeaders(data.pin_all_headers)
    for (const p of data.pinned_pages ?? []) setHeaderPinned(p, true)
    await loadAppearance()
    showPaste.value = false
    text.value = ''
    showToast('Настройки применены ✅')
  } catch {
    showToast('Не удалось применить настройки')
  } finally {
    busy.value = false
  }
}

/**
 * Фон: ручка принимает картинку целиком, поэтому к эффектам надо приложить и
 * её. Своя картинка остаётся на месте — переезжают только её настройки.
 */
async function applyBackground(b: Bundle['background']) {
  const cur = await loadBackground()
  const common = { position: b.position, blur: b.blur, dim: b.dim }
  if (b.url) {
    await setBackground({ kind: 'url', url: b.url, ...common })
    return
  }
  if (cur?.kind === 'file') {
    const id = cur.images.find((i) => i.url === cur.url)?.id
    if (id !== undefined) await setBackground({ kind: 'file', image_id: id, ...common })
    return
  }
  if (cur?.kind === 'url') await setBackground({ kind: 'url', url: cur.url, ...common })
}

async function reset() {
  if (!(await confirmAction(
    'Сбросить оформление? Тема, свои цвета, фон, скрытые страницы и закреплённые заголовки вернутся к обычным. Загруженные картинки останутся.',
  ))) return
  busy.value = true
  try {
    await setAppearanceState({ mode: 'auto', theme_id: 'night', auto_light: 'day', auto_dark: 'night' })
    await savePlacement({ scale: 100, offset_x: 0, offset_y: 0, focal_x: 50, focal_y: 50 })
    await setBackground({ kind: 'none', url: '', position: 'cover', blur: 0, dim: 0 })
    await setHiddenPages([])
    setPinAllHeaders(false)
    await loadAppearance()
    showToast('Оформление сброшено')
  } catch {
    showToast('Не удалось сбросить')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <p class="hint">
    Тема, свои цвета, сохранённые темы, настройки фона, скрытые страницы и
    закреплённые заголовки — одной строкой. Загруженные картинки не переносятся:
    файл принадлежит аккаунту.
  </p>

  <div class="row">
    <button class="btn" :disabled="busy" @click="copyAll">📋 Скопировать</button>
    <button class="btn" :disabled="busy" @click="saveFile">⬇️ Файлом</button>
    <button class="btn" :disabled="busy" @click="pasteFromClipboard">📥 Вставить</button>
  </div>

  <div v-if="showPaste" class="paste">
    <textarea v-model="text" rows="4" placeholder="Вставьте сюда строку настроек"></textarea>
    <div class="row">
      <input type="file" accept="application/json,.json" @change="onFile" />
    </div>
    <div class="row">
      <button class="btn primary" :disabled="busy || !text.trim()" @click="applyBundle">
        Применить
      </button>
      <button class="btn" @click="showPaste = false; text = ''">Отмена</button>
    </div>
  </div>

  <div class="row">
    <button class="btn danger" :disabled="busy" @click="reset">↺ Сбросить оформление</button>
  </div>
</template>

<style scoped>
.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin: 8px 0;
}

.btn {
  background: var(--bg-color);
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

.btn.danger {
  color: #ef4444;
}

.btn:disabled {
  opacity: 0.6;
}

.paste {
  margin: 8px 0;
}

.paste textarea {
  width: 100%;
  box-sizing: border-box;
  font-family: ui-monospace, monospace;
  font-size: 12px;
}
</style>
