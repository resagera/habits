<script setup lang="ts">
// Общая галерея фонов: наполняется админом, видна всем пользователям в
// выборе фона. Категории вложенные, загрузка пачкой.
//
// Миниатюру к каждой картинке готовит браузер (canvas) и отправляет вместе с
// оригиналом: на сервере обработки изображений нет, а без превью экран выбора
// фона тянул бы десятки мегабайт.
import { computed, onMounted, ref } from 'vue'
import { confirmAction } from '../../../shared/telegram'
import { api } from '../../../shared/api/client'
import { showToast } from '../../../shared/toast'
import { resolveBgUrl } from '../../../shared/background'

interface Category {
  id: number
  parent_id: number | null
  name: string
  position: number
}

interface Image {
  id: number
  category_id: number | null
  filename: string
  thumb: string
  title: string
}

const categories = ref<Category[]>([])
const images = ref<Image[]>([])
const open = ref(false)
const busy = ref(false)
const progress = ref('')
const target = ref<number | null>(null) // категория для загрузки
const fileInput = ref<HTMLInputElement>()

// перенос из личной коллекции: свои фоны уже лежат на сервере, качать и
// загружать их заново незачем — копируем файл на месте
const ownOpen = ref(false)
const ownImages = ref<{ id: number; url: string; thumb?: string }[]>([])

async function openOwn(categoryId: number | null) {
  target.value = categoryId
  ownOpen.value = true
  try {
    const bg = await api.get<{ images: { id: number; url: string; thumb?: string }[] }>(
      '/settings/background',
    )
    ownImages.value = bg.images
  } catch {
    ownImages.value = []
  }
}

async function copyOwn(id: number) {
  busy.value = true
  try {
    await api.post('/admin/gallery/images/from-background', {
      image_id: id, category_id: target.value ?? undefined,
    })
    ownOpen.value = false
    await load()
    showToast('Скопировано в галерею ✅')
  } catch {
    showToast('Не удалось скопировать')
  } finally {
    busy.value = false
  }
}

onMounted(load)

async function load() {
  try {
    const res = await api.get<{ categories: Category[]; images: Image[] }>('/appearance/gallery')
    categories.value = res.categories
    images.value = res.images
  } catch {
    showToast('Не удалось загрузить галерею')
  }
}

const rootImages = computed(() => images.value.filter((i) => i.category_id === null))
const imagesOf = (id: number) => images.value.filter((i) => i.category_id === id)
const childrenOf = (parent: number | null) => categories.value.filter((c) => c.parent_id === parent)

function url(img: Image, thumb = true): string {
  return resolveBgUrl(`uploads/gallery/${thumb && img.thumb ? img.thumb : img.filename}`)
}

async function addCategory(parent: number | null) {
  const name = prompt('Название категории')?.trim()
  if (!name) return
  await api.post('/admin/gallery/categories', { name, parent_id: parent })
  await load()
}

async function renameCategory(c: Category) {
  const name = prompt('Новое название', c.name)?.trim()
  if (!name) return
  await api.patch(`/admin/gallery/categories/${c.id}`, { name })
  await load()
}

async function removeCategory(c: Category) {
  if (!(await confirmAction(`Удалить категорию «${c.name}»? Картинки останутся, но выйдут из неё.`))) return
  await api.delete(`/admin/gallery/categories/${c.id}`)
  await load()
}

async function makeThumb(file: File, max = 320): Promise<Blob | null> {
  try {
    const bitmap = await createImageBitmap(file)
    const k = Math.min(1, max / Math.max(bitmap.width, bitmap.height))
    const canvas = document.createElement('canvas')
    canvas.width = Math.round(bitmap.width * k)
    canvas.height = Math.round(bitmap.height * k)
    canvas.getContext('2d')?.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
    return await new Promise((res) => canvas.toBlob((b) => res(b), 'image/webp', 0.8))
  } catch {
    return null
  }
}

function pick(categoryId: number | null) {
  target.value = categoryId
  fileInput.value?.click()
}

async function onUpload(e: Event) {
  const files = [...((e.target as HTMLInputElement).files ?? [])]
  if (!files.length) return
  busy.value = true
  let done = 0
  try {
    for (const file of files) {
      progress.value = `${++done} из ${files.length}…`
      const form = new FormData()
      form.append('file', file)
      const thumb = await makeThumb(file)
      if (thumb) form.append('thumb', thumb, 'thumb.webp')
      if (target.value !== null) form.append('category_id', String(target.value))
      form.append('title', file.name.replace(/\.[^.]+$/, ''))
      await api.upload('/admin/gallery/images', form)
    }
    await load()
    showToast(`Загружено: ${done} ✅`)
  } catch {
    showToast('Не удалось загрузить часть картинок')
    await load()
  } finally {
    busy.value = false
    progress.value = ''
    if (fileInput.value) fileInput.value.value = ''
  }
}

async function removeImage(img: Image) {
  if (!(await confirmAction('Удалить картинку из галереи?'))) return
  await api.delete(`/admin/gallery/images/${img.id}`)
  await load()
}

async function moveImage(img: Image) {
  const list = categories.value.map((c, i) => `${i + 1}. ${c.name}`).join('\n')
  const answer = prompt(`В какую категорию перенести?\n0. Без категории\n${list}`)
  if (answer === null) return
  const n = Number(answer)
  if (Number.isNaN(n) || n < 0 || n > categories.value.length) return
  await api.patch(`/admin/gallery/images/${img.id}`,
    n === 0 ? { move_to_root: true } : { category_id: categories.value[n - 1].id })
  await load()
}
</script>

<template>
  <section class="section">
    <button class="head" @click="open = !open">
      <h3>🖼 Общая галерея фонов</h3>
      <span class="chev">{{ open ? '▾' : '▸' }}</span>
    </button>

    <div v-show="open">
      <p class="hint">
        Эти картинки видят все пользователи в выборе фона (вкладка «Общая
        галерея»). Загруженное сжимается в превью прямо в браузере.
      </p>
      <div class="row">
        <button class="btn" :disabled="busy" @click="pick(null)">📤 Загрузить</button>
        <button class="btn" :disabled="busy" @click="openOwn(null)">📥 Из моих фонов</button>
        <button class="btn" :disabled="busy" @click="addCategory(null)">📁 Категория</button>
        <span v-if="progress" class="hint">{{ progress }}</span>
      </div>
      <input ref="fileInput" type="file" accept="image/*" multiple class="hidden" @change="onUpload" />

      <div v-if="rootImages.length" class="grid">
        <div v-for="img in rootImages" :key="img.id" class="thumb">
          <img :src="url(img)" :alt="img.title" loading="lazy" />
          <button class="mini" title="В категорию" @click="moveImage(img)">📁</button>
          <button class="mini del" title="Удалить" @click="removeImage(img)">✕</button>
        </div>
      </div>

      <div v-for="c in childrenOf(null)" :key="c.id" class="cat">
        <div class="c-head">
          <b>{{ c.name }}</b>
          <button class="mini" title="Загрузить сюда" @click="pick(c.id)">📤</button>
          <button class="mini" title="Из моих фонов" @click="openOwn(c.id)">📥</button>
          <button class="mini" title="Подкатегория" @click="addCategory(c.id)">＋</button>
          <button class="mini" title="Переименовать" @click="renameCategory(c)">✎</button>
          <button class="mini del" title="Удалить" @click="removeCategory(c)">✕</button>
        </div>
        <div v-if="imagesOf(c.id).length" class="grid">
          <div v-for="img in imagesOf(c.id)" :key="img.id" class="thumb">
            <img :src="url(img)" :alt="img.title" loading="lazy" />
            <button class="mini" title="Перенести" @click="moveImage(img)">📁</button>
            <button class="mini del" title="Удалить" @click="removeImage(img)">✕</button>
          </div>
        </div>
        <div v-for="sub in childrenOf(c.id)" :key="sub.id" class="cat nested">
          <div class="c-head">
            <b>{{ sub.name }}</b>
            <button class="mini" title="Загрузить сюда" @click="pick(sub.id)">📤</button>
            <button class="mini" title="Переименовать" @click="renameCategory(sub)">✎</button>
            <button class="mini del" title="Удалить" @click="removeCategory(sub)">✕</button>
          </div>
          <div class="grid">
            <div v-for="img in imagesOf(sub.id)" :key="img.id" class="thumb">
              <img :src="url(img)" :alt="img.title" loading="lazy" />
              <button class="mini" title="Перенести" @click="moveImage(img)">📁</button>
              <button class="mini del" title="Удалить" @click="removeImage(img)">✕</button>
            </div>
          </div>
        </div>
      </div>

      <p v-if="!images.length" class="hint">Пока пусто — загрузите первые картинки.</p>
    </div>

    <!-- выбор из личной коллекции; в body, иначе размытие карточек ломает fixed -->
    <Teleport to="body">
      <div v-if="ownOpen" class="modal" @click.self="ownOpen = false">
        <div class="modal-box">
          <h4>Из моих фонов<span v-if="target"> → в категорию</span></h4>
          <p class="hint">Файл копируется в галерею — свой фон останется на месте.</p>
          <div v-if="ownImages.length" class="grid">
            <div v-for="img in ownImages" :key="img.id" class="thumb" @click="copyOwn(img.id)">
              <img :src="resolveBgUrl(img.thumb || img.url)" loading="lazy" />
            </div>
          </div>
          <p v-else class="hint">Своих фонов пока нет — загрузите их в Настройках.</p>
          <button class="btn wide" @click="ownOpen = false">Закрыть</button>
        </div>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.section {
  background: var(--card-color);
  border-radius: 12px;
  padding: 12px 14px;
  margin-bottom: 12px;
  backdrop-filter: var(--card-blur);
}

.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  background: none;
  border: none;
  color: var(--text-color);
  cursor: pointer;
  padding: 0;
}

.head h3 {
  margin: 0;
  font-size: 16px;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 8px 0;
}

.row {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
  margin: 8px 0;
}

.hidden {
  display: none;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(84px, 1fr));
  gap: 6px;
  margin-bottom: 8px;
}

.thumb {
  position: relative;
  aspect-ratio: 1;
  border-radius: 8px;
  overflow: hidden;
}

.thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.mini {
  background: rgba(0, 0, 0, 0.45);
  border: none;
  border-radius: 6px;
  color: #fff;
  cursor: pointer;
  font-size: 11px;
  padding: 2px 5px;
}

.grid .mini {
  position: absolute;
  top: 2px;
  left: 2px;
}

.grid .mini.del {
  left: auto;
  right: 2px;
}

.cat {
  background: var(--bg-color);
  border-radius: 8px;
  padding: 8px 10px;
  margin-bottom: 8px;
}

.cat.nested {
  margin-left: 10px;
  background: var(--card-color);
}

.c-head {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 6px;
}

.c-head b {
  flex: 1;
  font-size: 14px;
}

.c-head .mini {
  background: var(--card-color);
  color: var(--text-color);
}

.c-head .mini.del {
  color: #ef4444;
}

.modal {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  z-index: 1200;
}

.modal-box {
  background: var(--bg-color);
  border-radius: 12px;
  padding: 14px;
  width: 100%;
  max-width: 520px;
  max-height: 80vh;
  overflow-y: auto;
}

.modal-box .thumb {
  cursor: pointer;
}

.btn.wide {
  width: 100%;
  margin-top: 8px;
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

.chev {
  color: var(--text-secondary);
}
</style>
