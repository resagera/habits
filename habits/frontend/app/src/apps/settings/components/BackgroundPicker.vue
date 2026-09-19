<script setup lang="ts">
// Фон приложения: свои картинки по вложенным папкам, общая галерея,
// размещение (масштаб, смещение, точка фокуса) и эффекты.
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { confirmAction } from '../../../shared/telegram'
import { api } from '../../../shared/api/client'
import { showToast } from '../../../shared/toast'
import {
  bgNaturalSize, deleteBackgroundImage, loadBackground, resolveBgUrl,
  setBackground, setBgPlacement, uploadBackground,
  type BackgroundState, type BgPosition,
} from '../../../shared/background'
import { placement, savePlacement } from '../../../shared/appearance'

interface Folder {
  id: number
  parent_id: number | null
  name: string
  position: number
  collapsed: boolean
}

interface GalleryCategory {
  id: number
  parent_id: number | null
  name: string
  position: number
}

interface GalleryImage {
  id: number
  category_id: number | null
  filename: string
  thumb: string
  title: string
}

const bg = ref<BackgroundState | null>(null)
const folders = ref<Folder[]>([])
const gallery = ref<{ categories: GalleryCategory[]; images: GalleryImage[] }>({
  categories: [], images: [],
})
const tab = ref<'mine' | 'gallery'>('mine')
const busy = ref(false)
const fileInput = ref<HTMLInputElement>()
const urlInput = ref('')
const moveImageId = ref<number | null>(null)
const newFolderName = ref('')

// свёрнутость блока и «Без папки» — предпочтение устройства, хватит localStorage
const OPEN_KEY = 'bg_block_open'
const ROOT_KEY = 'bg_root_open'
const open = ref(localStorage.getItem(OPEN_KEY) !== '0')
const rootOpen = ref(localStorage.getItem(ROOT_KEY) !== '0')

function toggleOpen() {
  open.value = !open.value
  localStorage.setItem(OPEN_KEY, open.value ? '1' : '0')
}

function toggleRoot() {
  rootOpen.value = !rootOpen.value
  localStorage.setItem(ROOT_KEY, rootOpen.value ? '1' : '0')
}

const position = ref<BgPosition>('cover')
const blur = ref(0)
const dim = ref(0)

onMounted(async () => {
  bg.value = await loadBackground()
  if (bg.value) {
    position.value = bg.value.position
    blur.value = bg.value.blur
    dim.value = bg.value.dim
    if (bg.value.kind === 'url') urlInput.value = bg.value.url
  }
  await Promise.all([loadFolders(), loadGallery()])
})

async function loadFolders() {
  try {
    folders.value = (await api.get<{ folders: Folder[] }>('/appearance/folders')).folders
  } catch {
    folders.value = []
  }
}

async function loadGallery() {
  try {
    gallery.value = await api.get('/appearance/gallery')
  } catch {
    gallery.value = { categories: [], images: [] }
  }
}

/** Картинки без папки и по папкам — дерево строится рекурсивно в шаблоне. */
const rootImages = computed(() =>
  (bg.value?.images ?? []).filter((i) => !imageFolder(i.id)),
)

// folder_id приходит вместе с картинками; типы фона старые, поэтому читаем мягко
function imageFolder(id: number): number | null {
  const img = (bg.value?.images ?? []).find((i) => i.id === id) as { folder_id?: number | null } | undefined
  return img?.folder_id ?? null
}

function childFolders(parent: number | null): Folder[] {
  return folders.value.filter((f) => f.parent_id === parent)
}

function folderImages(folderId: number) {
  return (bg.value?.images ?? []).filter((i) => imageFolder(i.id) === folderId)
}

type BgPatch = {
  kind?: 'none' | 'file' | 'url'
  image_id?: number
  url?: string
  position?: BgPosition
  blur?: number
  dim?: number
}

/** id картинки, которая стоит фоном сейчас (сервер хранит имя файла). */
function currentImageId(): number | null {
  const cur = bg.value
  if (!cur || cur.kind !== 'file') return null
  return cur.images.find((i) => i.url === cur.url)?.id ?? null
}

/**
 * Сохранение фона. Ручка принимает картинку целиком, а не «поправь одно поле»,
 * поэтому к любому изменению эффектов надо приложить и саму картинку: для
 * файла это image_id, для ссылки — url. Без них сервер отвечает 400, и правка
 * ползунка выглядела как «не удалось сохранить фон».
 */
async function push(patch: BgPatch) {
  const kind = patch.kind ?? bg.value?.kind ?? 'none'
  const body: BgPatch & { kind: 'none' | 'file' | 'url'; position: BgPosition; blur: number; dim: number } = {
    kind, position: position.value, blur: blur.value, dim: dim.value, ...patch,
  }
  if (kind === 'file' && body.image_id === undefined) {
    const id = currentImageId()
    if (id === null) return // картинка не из списка (например, из галереи)
    body.image_id = id
  }
  if (kind === 'url' && !body.url) {
    body.url = bg.value?.url ?? ''
  }
  busy.value = true
  try {
    bg.value = await setBackground(body)
  } catch {
    showToast('Не удалось сохранить фон')
  } finally {
    busy.value = false
  }
}

/** Своя картинка ставится по id, а не по ссылке: файл принадлежит аккаунту. */
const useImage = (id: number) => push({ kind: 'file', image_id: id })

/**
 * Картинка из общей галереи — обычной ссылкой на наш же домен: она не лежит
 * в списке картинок пользователя, а копировать её в аккаунт незачем.
 */
const useGallery = (filename: string) =>
  push({ kind: 'url', url: location.origin + import.meta.env.BASE_URL + 'uploads/gallery/' + filename })

const clearBg = () => push({ kind: 'none', url: '' })

function useUrl() {
  if (!urlInput.value.trim()) return
  return push({ kind: 'url', url: urlInput.value.trim() })
}

/**
 * Миниатюру делаем в браузере: на сервере обработки картинок нет, а без
 * превью экран выбора фона тянул бы десятки мегабайт.
 */
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

async function onUpload(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  busy.value = true
  try {
    await uploadBackground(file, await makeThumb(file))
    bg.value = await loadBackground()
    showToast('Фон загружен ✅')
  } catch {
    showToast('Не удалось загрузить (до 5 МБ, jpeg/png/webp/gif)')
  } finally {
    busy.value = false
    if (fileInput.value) fileInput.value.value = ''
  }
}

async function removeImage(id: number) {
  if (!(await confirmAction('Удалить картинку?'))) return
  await deleteBackgroundImage(id)
  bg.value = await loadBackground()
}

// --- фон из чата бота: отправляешь картинку боту — она появляется здесь ---
const botModal = ref(false)
let botTimer: ReturnType<typeof setInterval> | undefined

function openBotModal() {
  botModal.value = true
  const startCount = bg.value?.images.length ?? 0
  clearInterval(botTimer)
  botTimer = setInterval(async () => {
    if (!botModal.value) {
      clearInterval(botTimer)
      return
    }
    const fresh = await loadBackground()
    if (fresh && fresh.images.length > startCount) {
      bg.value = fresh
      clearInterval(botTimer)
      botModal.value = false
      showToast('Картинка получена — выберите её ниже 🖼')
    }
  }, 4000)
  setTimeout(() => clearInterval(botTimer), 180_000) // не поллим дольше 3 минут
}

onUnmounted(() => clearInterval(botTimer))

async function addFolder(parent: number | null) {
  const name = prompt('Название папки')?.trim()
  if (!name) return
  await api.post('/appearance/folders', { name, parent_id: parent })
  await loadFolders()
}

async function toggleFolder(f: Folder) {
  f.collapsed = !f.collapsed
  await api.patch(`/appearance/folders/${f.id}`, { collapsed: f.collapsed })
}

async function renameFolder(f: Folder) {
  const name = prompt('Новое название', f.name)?.trim()
  if (!name) return
  await api.patch(`/appearance/folders/${f.id}`, { name })
  await loadFolders()
}

async function removeFolder(f: Folder) {
  if (!(await confirmAction(`Удалить папку «${f.name}»? Картинки останутся, но выйдут из неё.`))) return
  await api.delete(`/appearance/folders/${f.id}`)
  await Promise.all([loadFolders(), loadBackground().then((b) => (bg.value = b))])
}

async function moveTo(folderId: number | null) {
  if (moveImageId.value === null) return
  await api.post(`/appearance/images/${moveImageId.value}/move`, { folder_id: folderId })
  moveImageId.value = null
  newFolderName.value = ''
  bg.value = await loadBackground()
}

/** Создать папку прямо в окне переноса — иначе приходится закрывать и начинать заново. */
async function moveToNewFolder() {
  const name = newFolderName.value.trim()
  if (!name || moveImageId.value === null) return
  const { folder } = await api.post<{ folder: Folder }>('/appearance/folders', { name })
  await loadFolders()
  await moveTo(folder.id)
}

// --- размещение ---

const p = ref({ ...placement.value })

async function pushPlacement() {
  try {
    await savePlacement(p.value)
    setBgPlacement(p.value)
  } catch {
    showToast('Не удалось сохранить размещение')
  }
}

function previewPlacement() {
  setBgPlacement(p.value) // мгновенно, без запроса — тянуть ползунок приятнее
}

function galleryUrl(img: GalleryImage, thumb = false): string {
  return resolveBgUrl(`uploads/gallery/${thumb && img.thumb ? img.thumb : img.filename}`)
}

function galleryImagesOf(cat: number | null) {
  return gallery.value.images.filter((i) => i.category_id === cat)
}

/**
 * У режимов «заполнить» и «вписать» двигать можно только ту ось, по которой
 * картинка не помещается: у широкой картинки на узком экране вертикаль просто
 * некуда сдвигать. Раньше это выглядело как «ползунок сломан».
 */
const focusAxis = computed<'x' | 'y' | 'none'>(() => {
  const size = bg.value?.url ? bgNaturalSize(bg.value.url) : null
  if (!size) return 'x'
  const screen = window.innerWidth / window.innerHeight
  const image = size.w / size.h
  if (Math.abs(image - screen) < 0.01) return 'none'
  if (position.value === 'contain') return image > screen ? 'y' : 'x'
  return image > screen ? 'x' : 'y' // cover: двигается та ось, что вылезает
})
</script>

<template>
  <div class="bgp">
    <button class="block-head" @click="toggleOpen">
      <h4>Фон приложения</h4>
      <span class="chev">{{ open ? '▾' : '▸' }}</span>
    </button>

    <template v-if="open">
    <div class="row">
      <button class="btn" :disabled="busy" @click="fileInput?.click()">📤 Загрузить</button>
      <button class="btn" :disabled="busy || bg?.kind === 'none'" @click="clearBg">Убрать фон</button>
      <button class="btn" :disabled="busy" @click="openBotModal">🤖 Из чата бота</button>
      <button class="btn" :disabled="busy" @click="addFolder(null)">📁 Папка</button>
    </div>
    <input ref="fileInput" type="file" accept="image/*" class="hidden" @change="onUpload" />

    <div class="tabs">
      <button :class="{ on: tab === 'mine' }" @click="tab = 'mine'">Мои</button>
      <button :class="{ on: tab === 'gallery' }" @click="tab = 'gallery'">Общая галерея</button>
    </div>

    <!-- свои картинки -->
    <template v-if="tab === 'mine'">
      <div v-if="rootImages.length" class="folder">
        <div class="f-head">
          <button class="f-toggle" @click="toggleRoot">
            {{ rootOpen ? '▾' : '▸' }} Без папки ({{ rootImages.length }})
          </button>
        </div>
        <div v-if="rootOpen" class="grid">
          <div v-for="img in rootImages" :key="img.id" class="thumb"
               :class="{ on: bg?.url === img.url }">
            <img :src="resolveBgUrl(img.thumb || img.url)" loading="lazy" @click="useImage(img.id)" />
            <button class="mini" title="В папку" @click="moveImageId = img.id">📁</button>
            <button class="mini del" title="Удалить" @click="removeImage(img.id)">✕</button>
          </div>
        </div>
      </div>

      <div v-for="f in childFolders(null)" :key="f.id" class="folder">
        <div class="f-head">
          <button class="f-toggle" @click="toggleFolder(f)">{{ f.collapsed ? '▸' : '▾' }} {{ f.name }}</button>
          <button class="mini" title="Подпапка" @click="addFolder(f.id)">＋</button>
          <button class="mini" title="Переименовать" @click="renameFolder(f)">✎</button>
          <button class="mini del" title="Удалить" @click="removeFolder(f)">✕</button>
        </div>
        <template v-if="!f.collapsed">
          <div v-if="folderImages(f.id).length" class="grid">
            <div v-for="img in folderImages(f.id)" :key="img.id" class="thumb"
                 :class="{ on: bg?.url === img.url }">
              <img :src="resolveBgUrl(img.thumb || img.url)" loading="lazy" @click="useImage(img.id)" />
              <button class="mini" title="Переместить" @click="moveImageId = img.id">📁</button>
              <button class="mini del" title="Удалить" @click="removeImage(img.id)">✕</button>
            </div>
          </div>
          <div v-for="sub in childFolders(f.id)" :key="sub.id" class="folder nested">
            <div class="f-head">
              <button class="f-toggle" @click="toggleFolder(sub)">
                {{ sub.collapsed ? '▸' : '▾' }} {{ sub.name }}
              </button>
              <button class="mini" title="Подпапка" @click="addFolder(sub.id)">＋</button>
              <button class="mini" title="Переименовать" @click="renameFolder(sub)">✎</button>
              <button class="mini del" title="Удалить" @click="removeFolder(sub)">✕</button>
            </div>
            <div v-if="!sub.collapsed" class="grid">
              <div v-for="img in folderImages(sub.id)" :key="img.id" class="thumb"
                   :class="{ on: bg?.url === img.url }">
                <img :src="resolveBgUrl(img.thumb || img.url)" loading="lazy" @click="useImage(img.id)" />
                <button class="mini" title="Переместить" @click="moveImageId = img.id">📁</button>
                <button class="mini del" title="Удалить" @click="removeImage(img.id)">✕</button>
              </div>
            </div>
          </div>
        </template>
      </div>

      <div class="row">
        <input v-model="urlInput" class="in" placeholder="или ссылка на картинку" />
        <button class="btn" :disabled="busy" @click="useUrl">Применить</button>
      </div>
    </template>

    <!-- общая галерея -->
    <template v-else>
      <p v-if="!gallery.images.length" class="hint">
        Галерея пока пуста — её наполняет администратор.
      </p>
      <div v-if="galleryImagesOf(null).length" class="grid">
        <div v-for="img in galleryImagesOf(null)" :key="img.id" class="thumb"
             @click="useGallery(img.filename)">
          <img :src="galleryUrl(img, true)" :alt="img.title" loading="lazy" />
        </div>
      </div>
      <div v-for="c in gallery.categories" :key="c.id" class="folder">
        <div class="f-head"><span class="f-toggle">{{ c.name }}</span></div>
        <div class="grid">
          <div v-for="img in galleryImagesOf(c.id)" :key="img.id" class="thumb"
               @click="useGallery(img.filename)">
            <img :src="galleryUrl(img, true)" :alt="img.title" loading="lazy" />
          </div>
        </div>
      </div>
    </template>

    <!-- размещение и эффекты -->
    <template v-if="bg && bg.kind !== 'none'">
      <h4>Размещение</h4>
      <div class="row">
        <button v-for="opt in (['cover', 'contain', 'center', 'repeat'] as BgPosition[])" :key="opt"
                class="btn small" :class="{ primary: position === opt }"
                @click="position = opt; push({ position: opt })">
          {{ opt === 'cover' ? 'Заполнить' : opt === 'contain' ? 'Вписать'
             : opt === 'center' ? 'По центру' : 'Замостить' }}
        </button>
      </div>

      <template v-if="position === 'cover' || position === 'contain'">
        <p class="hint">
          <template v-if="position === 'cover'">
            Картинка заполняет экран целиком: лишнее обрезается по краям, ничего
            не сжимается. Точка фокуса решает, какая часть останется видимой.
          </template>
          <template v-else>
            Картинка помещается целиком, поля остаются пустыми (цвет фона темы).
          </template>
          <template v-if="focusAxis !== 'none'">
            Двигать можно только
            {{ focusAxis === 'x' ? 'по горизонтали' : 'по вертикали' }} — по второй
            оси картинка ровно по экрану, сдвигать нечего.
          </template>
        </p>
        <label class="slider" :class="{ off: focusAxis === 'y' }">
          <span>Фокус по горизонтали: {{ p.focal_x }}%</span>
          <input v-model.number="p.focal_x" type="range" min="0" max="100"
                 :disabled="focusAxis === 'y'"
                 @input="previewPlacement" @change="pushPlacement" />
        </label>
        <label class="slider" :class="{ off: focusAxis === 'x' }">
          <span>Фокус по вертикали: {{ p.focal_y }}%</span>
          <input v-model.number="p.focal_y" type="range" min="0" max="100"
                 :disabled="focusAxis === 'x'"
                 @input="previewPlacement" @change="pushPlacement" />
        </label>
      </template>
      <template v-else>
        <label class="slider">
          <span>Масштаб: {{ p.scale }}% от размера картинки</span>
          <input v-model.number="p.scale" type="range" min="10" max="400" step="5"
                 @input="previewPlacement" @change="pushPlacement" />
        </label>
        <label class="slider">
          <span>Смещение по горизонтали: {{ p.offset_x }}</span>
          <input v-model.number="p.offset_x" type="range" min="-100" max="100"
                 @input="previewPlacement" @change="pushPlacement" />
        </label>
        <label class="slider">
          <span>Смещение по вертикали: {{ p.offset_y }}</span>
          <input v-model.number="p.offset_y" type="range" min="-100" max="100"
                 @input="previewPlacement" @change="pushPlacement" />
        </label>
      </template>

      <label class="slider">
        <span>Размытие фона: {{ blur }}px</span>
        <input v-model.number="blur" type="range" min="0" max="30" @change="push({ blur })" />
      </label>
      <label class="slider">
        <span>
          {{ dim < 0 ? `Затемнение: ${-dim}%` : dim > 0 ? `Осветление: ${dim}%` : 'Без затемнения' }}
          <button v-if="dim !== 0" class="reset" title="Убрать"
                  @click.prevent="dim = 0; push({ dim: 0 })">↺</button>
        </span>
        <input v-model.number="dim" type="range" min="-70" max="70" @change="push({ dim })" />
        <!-- ползунок двусторонний: середина — как есть, влево темнее, вправо светлее -->
        <span class="ends"><i>темнее</i><i>как есть</i><i>светлее</i></span>
      </label>
    </template>

    </template>

    <!-- Модалки — в body: у карточек есть backdrop-filter, а он создаёт
         содержащий блок для position: fixed, и окно уезжало вместе со страницей -->
    <Teleport to="body">
    <div v-if="botModal" class="modal" @click.self="botModal = false">
      <div class="modal-box">
        <h4>🤖 Фон из чата бота</h4>
        <p class="hint">
          Откройте чат с ботом (тот, в котором запущено приложение) и отправьте
          ему картинку — она появится здесь сама.
        </p>
        <p class="hint">⏳ Жду картинку…</p>
        <button class="btn wide" @click="botModal = false">Закрыть</button>
      </div>
    </div>

    <!-- выбор папки для переноса -->
    <div v-if="moveImageId !== null" class="modal" @click.self="moveImageId = null">
      <div class="modal-box">
        <h4>Переместить картинку</h4>
        <div class="new-folder">
          <input v-model="newFolderName" placeholder="Новая папка" @keyup.enter="moveToNewFolder" />
          <button class="btn" :disabled="!newFolderName.trim()" @click="moveToNewFolder">Создать</button>
        </div>
        <button class="btn wide" @click="moveTo(null)">Без папки</button>
        <button v-for="f in folders" :key="f.id" class="btn wide" @click="moveTo(f.id)">
          {{ f.parent_id ? '└ ' : '' }}{{ f.name }}
        </button>
        <button class="btn wide" @click="moveImageId = null">Отмена</button>
      </div>
    </div>
    </Teleport>
  </div>
</template>

<style scoped>
.block-head {
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

.chev {
  color: var(--text-secondary);
}

.ends {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--text-secondary);
}

.ends i {
  font-style: normal;
}

.slider.off {
  opacity: 0.45;
}

.reset {
  background: none;
  border: none;
  color: var(--accent-color);
  cursor: pointer;
  font-size: 13px;
  padding: 0 4px;
}

.new-folder {
  display: flex;
  gap: 6px;
  margin-bottom: 10px;
}

.new-folder input {
  flex: 1;
  min-width: 0;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  padding: 8px 10px;
}

h4 {
  margin: 14px 0 6px;
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
  margin: 8px 0;
}

.hidden {
  display: none;
}

.tabs {
  display: flex;
  gap: 8px;
  margin: 10px 0;
}

.tabs button {
  flex: 1;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  cursor: pointer;
  font-size: 13px;
  padding: 7px;
}

.tabs button.on {
  background: var(--accent-color);
  color: #fff;
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
  border: 2px solid transparent;
}

.thumb.on {
  border-color: var(--accent-color);
}

.thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  cursor: pointer;
  display: block;
}

.mini {
  position: absolute;
  top: 2px;
  background: rgba(0, 0, 0, 0.45);
  border: none;
  border-radius: 6px;
  color: #fff;
  cursor: pointer;
  font-size: 11px;
  padding: 2px 5px;
}

.mini:not(.del) {
  left: 2px;
}

.mini.del {
  right: 2px;
}

.folder {
  background: var(--card-color);
  border-radius: 8px;
  padding: 6px 8px;
  margin-bottom: 8px;
}

.folder.nested {
  margin-left: 10px;
  background: var(--bg-color);
}

.f-head {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-bottom: 6px;
}

.f-toggle {
  flex: 1;
  background: none;
  border: none;
  color: var(--text-color);
  cursor: pointer;
  font-size: 14px;
  text-align: left;
}

.f-head .mini {
  position: static;
  background: var(--bg-color);
  color: var(--text-color);
}

.f-head .mini.del {
  color: #ef4444;
}

.in {
  flex: 1;
  min-width: 160px;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  padding: 8px 10px;
}

.btn {
  background: var(--card-color);
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

.btn.small {
  font-size: 13px;
  padding: 6px 10px;
}

.btn.wide {
  width: 100%;
  margin-bottom: 6px;
}

.slider {
  display: block;
  font-size: 13px;
  margin: 8px 0;
}

.slider input {
  width: 100%;
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
  max-width: 360px;
  max-height: 80vh;
  overflow-y: auto;
}
</style>
