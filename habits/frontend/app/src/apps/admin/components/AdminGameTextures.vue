<script setup lang="ts">
// Текстуры стен для лабиринта: админ пополняет набор, игра берёт случайную
// вместе с нарисованными кодом стилями.
//
// Плитка размножается по всей стене, поэтому картинка должна быть бесшовной —
// проверить это сервер не может, пишем прямо в интерфейсе.
import { onMounted, ref } from 'vue'
import { api } from '../../../shared/api/client'
import { resolveBgUrl } from '../../../shared/background'
import { confirmAction } from '../../../shared/telegram'
import { showToast } from '../../../shared/toast'

interface Texture {
  id: number
  game: string
  title: string
  url: string
}

const open = ref(false)
const list = ref<Texture[]>([])
const busy = ref(false)
const title = ref('')
const fileInput = ref<HTMLInputElement>()

onMounted(load)

async function load() {
  try {
    const res = await api.get<{ textures: Texture[] }>('/games/textures?game=maze')
    list.value = res.textures ?? []
  } catch {
    list.value = []
  }
}

async function upload(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  busy.value = true
  try {
    const form = new FormData()
    form.append('game', 'maze')
    form.append('title', title.value.trim())
    form.append('file', file)
    await api.upload('/admin/games/textures', form)
    title.value = ''
    await load()
    showToast('Текстура добавлена ✅')
  } catch {
    showToast('Не удалось загрузить (jpeg/png/webp/gif, до 8 МБ)')
  } finally {
    busy.value = false
    if (fileInput.value) fileInput.value.value = ''
  }
}

async function remove(t: Texture) {
  if (!(await confirmAction(`Удалить текстуру «${t.title || t.id}»?`))) return
  try {
    await api.delete(`/admin/games/textures/${t.id}`)
    list.value = list.value.filter((x) => x.id !== t.id)
  } catch {
    showToast('Не удалось удалить')
  }
}
</script>

<template>
  <section class="section">
    <button class="collapse-head" @click="open = !open">
      <h3>Текстуры стен (Лабиринт)</h3>
      <span class="chev">{{ open ? '▾' : '▸' }}</span>
    </button>

    <div v-show="open" class="body">
      <p class="hint">
        Плитка размножается по всей стене, поэтому картинка должна быть
        бесшовной — иначе стыки будут видны. Хороший размер — от 64×64 до
        256×256. В игре стиль выбирается случайно: загруженные текстуры идут
        вместе с шестью нарисованными стилями.
      </p>

      <div class="row">
        <input v-model="title" class="in" placeholder="Название (для себя)" />
        <button class="btn" :disabled="busy" @click="fileInput?.click()">📤 Загрузить</button>
        <input ref="fileInput" type="file" accept="image/*" class="hidden" @change="upload" />
      </div>

      <p v-if="!list.length" class="hint">Загруженных текстур пока нет.</p>
      <div v-else class="grid">
        <div v-for="t in list" :key="t.id" class="tile">
          <span class="sample" :style="{ backgroundImage: `url(${resolveBgUrl(t.url)})` }"></span>
          <span class="name">{{ t.title || '—' }}</span>
          <button class="mini" title="Удалить" @click="remove(t)">✕</button>
        </div>
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

.row {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
}

.in {
  flex: 1;
  min-width: 0;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  padding: 8px 10px;
}

.hidden {
  display: none;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  gap: 8px;
}

.tile {
  position: relative;
  background: var(--bg-color);
  border-radius: 8px;
  padding: 6px;
  text-align: center;
}

/* показываем именно плиткой: сразу видно, бесшовная картинка или нет */
.sample {
  display: block;
  height: 64px;
  border-radius: 6px;
  background-repeat: repeat;
  background-size: 32px 32px;
}

.name {
  display: block;
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mini {
  position: absolute;
  right: 4px;
  top: 4px;
  background: rgba(0, 0, 0, 0.45);
  border: none;
  border-radius: 6px;
  color: #fff;
  cursor: pointer;
  font-size: 11px;
  padding: 2px 5px;
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

.btn:disabled {
  opacity: 0.6;
}
</style>
