<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import MarkdownView from '../../articles/components/MarkdownView.vue'
import CheckerGroupView from './CheckerGroupView.vue'
import { showToast } from '../../../shared/toast'
import * as projApi from '../api'
import type {
  BlockKind,
  FileContent,
  ImagesContent,
  LocationContent,
  Project,
  ProjectBlock,
  ResolvedArticle,
  ResolvedCheckGroup,
  ResolvedTask,
  ResolvedTaskCategory,
  ShareUser,
  TextContent,
} from '../types'
import { assetUrl, fmtDate, fmtSize, KIND_LABELS, STATUS_LABELS, userLabel } from '../types'
import ImageLightbox from './ImageLightbox.vue'

const props = defineProps<{
  project: Project
  expanded: boolean
}>()

const emit = defineEmits<{
  toggle: []
  viewed: []
  settings: []
  meta: [project: Project]
}>()

const blocks = ref<ProjectBlock[]>([])
const members = ref<ShareUser[]>([])
const loading = ref(false)
const loaded = ref(false)

let timer: number | undefined

// раскрыт → грузим свежие данные (лениво + realtime-обновление раз в минуту)
watch(
  () => props.expanded,
  (open) => {
    if (open) {
      loadFull()
      timer = window.setInterval(() => loadFull(true), 60_000)
    } else if (timer) {
      clearInterval(timer)
      timer = undefined
    }
  },
  { immediate: true },
)

onUnmounted(() => {
  if (timer) clearInterval(timer)
})

async function loadFull(silent = false) {
  if (!silent) loading.value = !loaded.value
  try {
    const data = await projApi.fetchProject(props.project.id)
    blocks.value = data.blocks
    members.value = data.members
    loaded.value = true
    emit('viewed')
  } catch {
    if (!silent) showToast('Не удалось загрузить проект')
  } finally {
    loading.value = false
  }
}

// --- содержимое блоков (типизированные геттеры) ---

const asText = (b: ProjectBlock) => b.content as TextContent
const asImages = (b: ProjectBlock) => b.content as ImagesContent
const asFile = (b: ProjectBlock) => b.content as FileContent
const asLocation = (b: ProjectBlock) => b.content as LocationContent
const asGroup = (b: ProjectBlock) => b.data as ResolvedCheckGroup | undefined
const asArticle = (b: ProjectBlock) => b.data as ResolvedArticle | undefined
const asTask = (b: ProjectBlock) => b.data as ResolvedTask | undefined
const asTaskCat = (b: ProjectBlock) => b.data as ResolvedTaskCategory | undefined

function isMissing(b: ProjectBlock): boolean {
  return !!b.data && 'missing' in (b.data as object) && (b.data as { missing?: boolean }).missing === true
}

function blockTitle(b: ProjectBlock): string {
  switch (b.kind) {
    case 'text':
      return asText(b).text.split('\n')[0]?.slice(0, 60) || 'Текст'
    case 'images':
      return `${asImages(b).images.length} фото`
    case 'file':
      return asFile(b).name
    case 'location':
      return asLocation(b).label || 'Точка на карте'
    case 'checker_group':
      return asGroup(b)?.name ?? 'Чек-лист'
    case 'article':
      return asArticle(b)?.title ?? 'Статья'
    case 'task':
      return asTask(b)?.title ?? 'Задача'
    case 'task_category':
      return asTaskCat(b)?.name ?? 'Категория задач'
  }
}

function groupProgress(g: ResolvedCheckGroup): string {
  let done = 0
  let total = 0
  const walk = (x: ResolvedCheckGroup) => {
    for (const i of x.items) {
      total++
      if (i.done) done++
    }
    x.subgroups?.forEach(walk)
  }
  walk(g)
  return total ? `${done}/${total}` : ''
}

const PRIORITY_COLORS = ['transparent', '#8bc34a', '#ff9800', '#f44336']

function fileHref(url: string): string {
  return assetUrl(url)
}

// --- операции с блоками ---

const confirmDeleteId = ref<number | null>(null)
const menuBlockId = ref<number | null>(null)

async function toggleBlockCollapsed(b: ProjectBlock) {
  b.collapsed = !b.collapsed
  try {
    await projApi.updateBlock(b.id, { collapsed: b.collapsed })
  } catch {
    b.collapsed = !b.collapsed
  }
}

async function removeBlock(b: ProjectBlock) {
  try {
    await projApi.deleteBlock(b.id)
    blocks.value = blocks.value.filter((x) => x.id !== b.id)
    confirmDeleteId.value = null
    menuBlockId.value = null
  } catch {
    showToast('Не удалось удалить блок')
  }
}

async function setBg(b: ProjectBlock, ev: Event) {
  const bg = (ev.target as HTMLInputElement).value
  try {
    const { block } = await projApi.updateBlock(b.id, { bg })
    replaceBlock(block)
  } catch {
    showToast('Не удалось сменить фон')
  }
}

function replaceBlock(nb: ProjectBlock) {
  const i = blocks.value.findIndex((x) => x.id === nb.id)
  if (i >= 0) blocks.value[i] = nb
}

async function moveBlock(b: ProjectBlock, dir: -1 | 1) {
  const i = blocks.value.findIndex((x) => x.id === b.id)
  const j = i + dir
  if (i < 0 || j < 0 || j >= blocks.value.length) return
  const other = blocks.value[j]
  const a = b.position
  const bp = other.position
  const posA = a === bp ? bp + dir : bp
  try {
    await Promise.all([
      projApi.updateBlock(b.id, { position: posA }),
      projApi.updateBlock(other.id, { position: a === bp ? a : a }),
    ])
    await loadFull(true)
  } catch {
    showToast('Не удалось переместить')
  }
}

// --- добавление блоков ---

// null — меню закрыто; число — позиция вставки (после блока); -1 — в конец
const addMenuAt = ref<number | null>(null)
const imagesInput = ref<HTMLInputElement | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const appendImagesBlock = ref<ProjectBlock | null>(null)

function insertPosition(): number | undefined {
  return addMenuAt.value !== null && addMenuAt.value >= 0 ? addMenuAt.value : undefined
}

const addMenuTop = ref(false)

function openAddMenu(afterBlock?: ProjectBlock) {
  menuBlockId.value = null
  addMenuTop.value = false
  addMenuAt.value = afterBlock ? afterBlock.position + 1 : -1
}

/** Вставка в самое начало (верхняя кнопка «＋ Блок»). */
function openAddMenuTop() {
  menuBlockId.value = null
  addMenuTop.value = true
  addMenuAt.value = 0
}

async function pickKind(kind: BlockKind) {
  const pos = insertPosition()
  addMenuAt.value = null
  addMenuTop.value = false
  switch (kind) {
    case 'text':
      openTextEditor(null, pos)
      break
    case 'images':
      appendImagesBlock.value = null
      pendingPos.value = pos
      imagesInput.value?.click()
      break
    case 'file':
      pendingPos.value = pos
      fileInput.value?.click()
      break
    case 'location':
      openLocationEditor(null, pos)
      break
    default:
      openRefPicker(kind, pos)
  }
}

const pendingPos = ref<number | undefined>(undefined)
const uploading = ref(false)

async function onImagesPicked(ev: Event) {
  const input = ev.target as HTMLInputElement
  const files = [...(input.files ?? [])]
  input.value = ''
  if (!files.length) return
  uploading.value = true
  try {
    const urls: string[] = []
    for (const f of files) {
      const up = await projApi.uploadFile(props.project.id, f)
      if (!up.image) {
        showToast(`«${f.name}» — не картинка, пропущено`)
        continue
      }
      urls.push(up.url)
    }
    if (!urls.length) return
    if (appendImagesBlock.value) {
      const c = asImages(appendImagesBlock.value)
      const { block } = await projApi.updateBlock(appendImagesBlock.value.id, {
        kind: 'images',
        content: { images: [...c.images, ...urls] },
      })
      replaceBlock(block)
    } else {
      const { block } = await projApi.createBlock(props.project.id, {
        kind: 'images',
        content: { images: urls },
        position: pendingPos.value,
      })
      insertBlockLocal(block)
    }
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось загрузить картинки')
  } finally {
    uploading.value = false
    appendImagesBlock.value = null
  }
}

async function onFilePicked(ev: Event) {
  const input = ev.target as HTMLInputElement
  const f = input.files?.[0]
  input.value = ''
  if (!f) return
  uploading.value = true
  try {
    const up = await projApi.uploadFile(props.project.id, f)
    const { block } = await projApi.createBlock(props.project.id, {
      kind: 'file',
      content: { url: up.url, name: up.name, size: up.size },
      position: pendingPos.value,
    })
    insertBlockLocal(block)
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось загрузить файл')
  } finally {
    uploading.value = false
  }
}

function insertBlockLocal(b: ProjectBlock) {
  // позиции соседей могли сдвинуться на сервере — перерисуем список целиком
  loadFull(true)
  // мгновенная вставка без ожидания
  const idx = blocks.value.findIndex((x) => x.position >= b.position)
  if (idx < 0) blocks.value.push(b)
  else blocks.value.splice(idx, 0, b)
}

function addPhotosTo(b: ProjectBlock) {
  menuBlockId.value = null
  appendImagesBlock.value = b
  imagesInput.value?.click()
}

// --- текстовый блок: редактор + разбиение ---

const textModal = ref<{ block: ProjectBlock | null; pos?: number } | null>(null)
const textDraft = ref('')
const textRich = ref(false)
const textArea = ref<HTMLTextAreaElement | null>(null)

function openTextEditor(b: ProjectBlock | null, pos?: number) {
  menuBlockId.value = null
  textModal.value = { block: b, pos }
  textDraft.value = b ? asText(b).text : ''
  textRich.value = b ? asText(b).rich : false
}

async function saveText() {
  const m = textModal.value
  if (!m) return
  try {
    if (m.block) {
      const { block } = await projApi.updateBlock(m.block.id, {
        kind: 'text',
        content: { text: textDraft.value, rich: textRich.value },
      })
      replaceBlock(block)
    } else {
      const { block } = await projApi.createBlock(props.project.id, {
        kind: 'text',
        content: { text: textDraft.value, rich: textRich.value },
        position: m.pos,
      })
      insertBlockLocal(block)
    }
    textModal.value = null
  } catch {
    showToast('Не удалось сохранить текст')
  }
}

/** Разбить текстовый блок по курсору на два (между ними можно вставлять блоки). */
async function splitText() {
  const m = textModal.value
  if (!m?.block) return
  const cursor = textArea.value?.selectionStart ?? Math.floor(textDraft.value.length / 2)
  const before = textDraft.value.slice(0, cursor).replace(/\n+$/, '')
  const after = textDraft.value.slice(cursor).replace(/^\n+/, '')
  if (!before || !after) {
    showToast('Поставьте курсор внутри текста')
    return
  }
  try {
    const { block } = await projApi.updateBlock(m.block.id, {
      kind: 'text',
      content: { text: before, rich: textRich.value },
    })
    replaceBlock(block)
    await projApi.createBlock(props.project.id, {
      kind: 'text',
      content: { text: after, rich: textRich.value },
      position: m.block.position + 1,
    })
    textModal.value = null
    loadFull(true)
  } catch {
    showToast('Не удалось разбить блок')
  }
}

// --- геолокация ---

const locModal = ref<{ block: ProjectBlock | null; pos?: number } | null>(null)
const locLat = ref('')
const locLon = ref('')
const locLabel = ref('')

function openLocationEditor(b: ProjectBlock | null, pos?: number) {
  menuBlockId.value = null
  locModal.value = { block: b, pos }
  locLat.value = b ? String(asLocation(b).lat) : ''
  locLon.value = b ? String(asLocation(b).lon) : ''
  locLabel.value = b ? asLocation(b).label : ''
}

function useMyLocation() {
  navigator.geolocation?.getCurrentPosition(
    (pos) => {
      locLat.value = pos.coords.latitude.toFixed(6)
      locLon.value = pos.coords.longitude.toFixed(6)
    },
    () => showToast('Геолокация недоступна'),
  )
}

async function saveLocation() {
  const m = locModal.value
  if (!m) return
  const lat = parseFloat(locLat.value)
  const lon = parseFloat(locLon.value)
  if (isNaN(lat) || isNaN(lon)) {
    showToast('Укажите координаты')
    return
  }
  const content = { lat, lon, label: locLabel.value.trim() }
  try {
    if (m.block) {
      const { block } = await projApi.updateBlock(m.block.id, { kind: 'location', content })
      replaceBlock(block)
    } else {
      const { block } = await projApi.createBlock(props.project.id, {
        kind: 'location',
        content,
        position: m.pos,
      })
      insertBlockLocal(block)
    }
    locModal.value = null
  } catch {
    showToast('Не удалось сохранить')
  }
}

function mapHref(c: LocationContent): string {
  return `https://www.openstreetmap.org/?mlat=${c.lat}&mlon=${c.lon}#map=16/${c.lat}/${c.lon}`
}

// --- ref-блоки: выбор существующего или создание нового ---

const refModal = ref<{ kind: BlockKind; pos?: number } | null>(null)
const refOptions = ref<{ id: number; label: string }[]>([])
const refLoading = ref(false)
const refNewName = ref('')

async function openRefPicker(kind: BlockKind, pos?: number) {
  refModal.value = { kind, pos }
  refNewName.value = ''
  refOptions.value = []
  refLoading.value = true
  try {
    if (kind === 'checker_group') {
      const { groups } = await projApi.fetchCheckerGroups()
      refOptions.value = groups.filter((g) => !g.parent_id).map((g) => ({ id: g.id, label: g.name }))
    } else if (kind === 'article') {
      const { articles } = await projApi.fetchArticles()
      refOptions.value = articles.map((a) => ({ id: a.id, label: a.title }))
    } else if (kind === 'task' || kind === 'task_category') {
      const data = await projApi.fetchTasksAll()
      refOptions.value =
        kind === 'task'
          ? data.tasks.map((t) => ({ id: t.id, label: t.title }))
          : data.projects.map((p) => ({ id: p.id, label: p.name }))
    }
  } catch {
    showToast('Не удалось загрузить список')
  } finally {
    refLoading.value = false
  }
}

async function addRef(refId?: number) {
  const m = refModal.value
  if (!m) return
  try {
    const { block } = await projApi.createBlock(props.project.id, {
      kind: m.kind,
      content: refId ? { ref_id: refId } : undefined,
      create_name: refId ? undefined : refNewName.value.trim(),
      collapsed: m.kind === 'article', // статьи по умолчанию — спойлером
      position: m.pos,
    })
    insertBlockLocal(block)
    refModal.value = null
  } catch {
    showToast('Не удалось добавить блок')
  }
}

// --- лайтбокс ---

const lightbox = ref<{ block: ProjectBlock; index: number } | null>(null)

async function deleteImage(b: ProjectBlock, index: number) {
  const imgs = [...asImages(b).images]
  imgs.splice(index, 1)
  try {
    if (imgs.length === 0) {
      await removeBlock(b)
      lightbox.value = null
      return
    }
    const { block } = await projApi.updateBlock(b.id, { kind: 'images', content: { images: imgs } })
    replaceBlock(block)
    if (lightbox.value) {
      if (index >= imgs.length) lightbox.value.index = imgs.length - 1
      lightbox.value = { block, index: Math.min(lightbox.value.index, imgs.length - 1) }
    }
  } catch {
    showToast('Не удалось удалить фото')
  }
}

const metaLine = computed(() => {
  const p = props.project
  const parts: string[] = []
  if (p.ptype) parts.push(p.ptype)
  if (p.start_date || p.due_date)
    parts.push(`${p.start_date ? fmtDate(p.start_date) : '…'} → ${p.due_date ? fmtDate(p.due_date) : '…'}`)
  if (p.tz) parts.push(p.tz)
  return parts.join(' · ')
})
</script>

<template>
  <div class="proj" :class="{ open: expanded }">
    <div class="head">
      <button class="head-main" @click="emit('toggle')">
        <span class="chevron" :class="{ open: expanded }">▸</span>
        <span class="icon-dot" :style="{ backgroundColor: project.color }">{{ project.icon || '📦' }}</span>
        <span class="name">
          {{ project.name }}
          <span v-if="project.changed" class="star" title="Изменён другим участником">★</span>
        </span>
        <span class="status">{{ STATUS_LABELS[project.status] }}</span>
      </button>
      <button class="gear" title="Настройки" @click="emit('settings')">⚙️</button>
    </div>

    <template v-if="expanded">
      <img v-if="project.cover" class="cover" :src="assetUrl(project.cover)" alt="" />

      <p v-if="project.description" class="descr">{{ project.description }}</p>
      <div v-if="metaLine || project.tags.length || !project.mine" class="meta">
        <span v-if="!project.mine" class="owner">👤 {{ project.owner_name }}</span>
        <span v-if="metaLine">{{ metaLine }}</span>
        <span v-for="t in project.tags" :key="t" class="tag">#{{ t }}</span>
        <span v-if="members.length" class="owner" :title="members.map(userLabel).join(', ')">
          👥 {{ members.length }}
        </span>
      </div>

      <div v-if="loading" class="loading">Загрузка…</div>

      <template v-else>
        <!-- вставка в самое начало -->
        <div v-if="addMenuTop && addMenuAt !== null" class="add-menu top-menu">
          <button v-for="(label, kind) in KIND_LABELS" :key="kind" @click="pickKind(kind as BlockKind)">
            {{ label }}
          </button>
          <button class="cancel" @click="addMenuAt = null; addMenuTop = false">✕ Отмена</button>
        </div>
        <button v-else-if="blocks.length" class="add-block-btn top-add" @click="openAddMenuTop">
          ＋ Блок
        </button>

        <div
          v-for="b in blocks"
          :key="b.id"
          class="block"
          :style="b.bg ? { backgroundColor: b.bg } : {}"
        >
          <div class="b-head">
            <button class="b-toggle" @click="toggleBlockCollapsed(b)">
              <span class="chevron sm" :class="{ open: !b.collapsed }">▸</span>
              <span class="b-kind">{{ KIND_LABELS[b.kind].split(' ')[0] }}</span>
              <span v-if="b.collapsed || b.kind !== 'text'" class="b-title">{{ blockTitle(b) }}</span>
              <span v-if="b.kind === 'checker_group' && asGroup(b) && !isMissing(b)" class="b-count">
                {{ groupProgress(asGroup(b)!) }}
              </span>
            </button>
            <div class="b-actions">
              <button class="b-menu-btn" @click="menuBlockId = menuBlockId === b.id ? null : b.id">⋮</button>
            </div>
          </div>

          <!-- меню блока -->
          <div v-if="menuBlockId === b.id" class="b-menu">
            <button v-if="b.kind === 'text'" @click="openTextEditor(b)">✏️ Изменить</button>
            <button v-if="b.kind === 'location'" @click="openLocationEditor(b)">✏️ Изменить</button>
            <button v-if="b.kind === 'images'" @click="addPhotosTo(b)">🖼 Добавить фото</button>
            <label class="bg-pick">
              🎨 Фон
              <input type="color" :value="b.bg || '#2b2b2b'" @change="setBg(b, $event)" />
            </label>
            <button @click="moveBlock(b, -1); menuBlockId = null">▲ Выше</button>
            <button @click="moveBlock(b, 1); menuBlockId = null">▼ Ниже</button>
            <button @click="openAddMenu(b)">➕ Блок ниже</button>
            <button v-if="confirmDeleteId !== b.id" class="danger" @click="confirmDeleteId = b.id">
              🗑 Удалить
            </button>
            <button v-else class="danger" @click="removeBlock(b)">Точно удалить блок?</button>
          </div>

          <div v-if="!b.collapsed" class="b-body">
            <p v-if="isMissing(b)" class="missing">⚠️ Источник удалён на своей странице</p>

            <!-- текст -->
            <template v-else-if="b.kind === 'text'">
              <MarkdownView v-if="asText(b).rich" :source="asText(b).text" />
              <p v-else class="plain" @click="openTextEditor(b)">{{ asText(b).text }}</p>
            </template>

            <!-- картинки -->
            <template v-else-if="b.kind === 'images'">
              <img
                v-if="asImages(b).images.length === 1"
                class="img-single"
                :src="assetUrl(asImages(b).images[0])"
                loading="lazy"
                alt=""
                @click="lightbox = { block: b, index: 0 }"
              />
              <div v-else class="img-grid">
                <img
                  v-for="(u, i) in asImages(b).images"
                  :key="u"
                  :src="assetUrl(u)"
                  loading="lazy"
                  alt=""
                  @click="lightbox = { block: b, index: i }"
                />
              </div>
            </template>

            <!-- файл -->
            <a
              v-else-if="b.kind === 'file'"
              class="file-row"
              :href="fileHref(asFile(b).url)"
              target="_blank"
              :download="asFile(b).name"
            >
              📎 {{ asFile(b).name }}
              <span class="file-size">{{ fmtSize(asFile(b).size) }}</span>
            </a>

            <!-- геолокация -->
            <a
              v-else-if="b.kind === 'location'"
              class="file-row"
              :href="mapHref(asLocation(b))"
              target="_blank"
            >
              📍 {{ asLocation(b).label || 'Точка на карте' }}
              <span class="file-size">{{ asLocation(b).lat.toFixed(4) }}, {{ asLocation(b).lon.toFixed(4) }}</span>
            </a>

            <!-- чек-лист (подгруппы любой глубины) -->
            <template v-else-if="b.kind === 'checker_group' && asGroup(b)">
              <CheckerGroupView :group="asGroup(b)!" />
              <router-link class="src-link" to="/checker">открыть в Checker →</router-link>
            </template>

            <!-- статья -->
            <template v-else-if="b.kind === 'article' && asArticle(b)">
              <MarkdownView :source="asArticle(b)!.content" />
              <router-link class="src-link" to="/articles">открыть в Articles →</router-link>
            </template>

            <!-- задача -->
            <div v-else-if="b.kind === 'task' && asTask(b)" class="task-row">
              <span
                class="prio"
                :style="{ backgroundColor: PRIORITY_COLORS[asTask(b)!.priority] ?? 'transparent' }"
              ></span>
              <span class="task-title" :class="{ struck: asTask(b)!.status_kind === 'done' }">
                {{ asTask(b)!.title }}
              </span>
              <span class="task-meta">
                {{ asTask(b)!.status }}
                <template v-if="asTask(b)!.due_date"> · 📅 {{ asTask(b)!.due_date }}</template>
                <template v-if="asTask(b)!.checklist_total">
                  · ☑ {{ asTask(b)!.checklist_done }}/{{ asTask(b)!.checklist_total }}
                </template>
              </span>
            </div>

            <!-- категория задач -->
            <template v-else-if="b.kind === 'task_category' && asTaskCat(b)">
              <div
                v-for="t in asTaskCat(b)!.tasks"
                :key="t.id"
                class="task-row"
              >
                <span class="prio" :style="{ backgroundColor: PRIORITY_COLORS[t.priority] ?? 'transparent' }"></span>
                <span class="task-title">{{ t.title }}</span>
                <span class="task-meta">
                  {{ t.status }}<template v-if="t.due_date"> · 📅 {{ t.due_date }}</template>
                </span>
              </div>
              <p v-if="!asTaskCat(b)!.tasks.length" class="missing">Нет открытых задач</p>
              <router-link class="src-link" to="/tasks">открыть в Tasks →</router-link>
            </template>
          </div>
        </div>

        <p v-if="uploading" class="loading">Загрузка файлов…</p>

        <!-- меню добавления (внизу) -->
        <div v-if="addMenuAt !== null && !addMenuTop" class="add-menu">
          <button v-for="(label, kind) in KIND_LABELS" :key="kind" @click="pickKind(kind as BlockKind)">
            {{ label }}
          </button>
          <button class="cancel" @click="addMenuAt = null">✕ Отмена</button>
        </div>
        <button v-else class="add-block-btn" @click="openAddMenu()">＋ Блок</button>
      </template>
    </template>

    <input ref="imagesInput" type="file" accept="image/*" multiple hidden @change="onImagesPicked" />
    <input ref="fileInput" type="file" hidden @change="onFilePicked" />

    <!-- редактор текста -->
    <Teleport to="body">
      <div v-if="textModal" class="modal" @click.self="textModal = null">
        <div class="modal-content card tcard">
          <h3>{{ textModal.block ? 'Текст' : 'Новый текст' }}</h3>
          <textarea ref="textArea" v-model="textDraft" rows="10" placeholder="Текст…"></textarea>
          <label class="check-line">
            <input v-model="textRich" type="checkbox" />
            <span>Rich text (Markdown: **жирный**, списки, заголовки)</span>
          </label>
          <button class="btn primary" @click="saveText">💾 Сохранить</button>
          <button v-if="textModal.block" class="btn" @click="splitText">
            ✂️ Разбить по курсору на два блока
          </button>
          <button class="btn" @click="textModal = null">Отмена</button>
        </div>
      </div>
    </Teleport>

    <!-- редактор геолокации -->
    <Teleport to="body">
      <div v-if="locModal" class="modal" @click.self="locModal = null">
        <div class="modal-content card tcard">
          <h3>📍 Геолокация</h3>
          <input v-model="locLabel" placeholder="Подпись (необязательно)" maxlength="300" />
          <div class="loc-row">
            <input v-model="locLat" placeholder="Широта" inputmode="decimal" />
            <input v-model="locLon" placeholder="Долгота" inputmode="decimal" />
          </div>
          <button class="btn" @click="useMyLocation">🧭 Моё местоположение</button>
          <button class="btn primary" @click="saveLocation">💾 Сохранить</button>
          <button class="btn" @click="locModal = null">Отмена</button>
        </div>
      </div>
    </Teleport>

    <!-- выбор существующей сущности / создание новой -->
    <Teleport to="body">
      <div v-if="refModal" class="modal" @click.self="refModal = null">
        <div class="modal-content card tcard">
          <h3>{{ KIND_LABELS[refModal.kind] }}</h3>
          <p class="hint">
            Выберите существующий элемент — блок показывает его живые данные. Или создайте новый:
            он появится и на своей странице.
          </p>
          <div v-if="refLoading" class="loading">Загрузка…</div>
          <div v-else class="ref-list">
            <button v-for="o in refOptions" :key="o.id" class="ref-item" @click="addRef(o.id)">
              {{ o.label }}
            </button>
            <p v-if="!refOptions.length" class="missing">Пока нет существующих элементов</p>
          </div>
          <form class="ref-new" @submit.prevent="addRef()">
            <input v-model="refNewName" placeholder="Или название нового…" maxlength="200" />
            <button type="submit" :disabled="!refNewName.trim()">Создать</button>
          </form>
          <button class="btn" @click="refModal = null">Отмена</button>
        </div>
      </div>
    </Teleport>

    <ImageLightbox
      v-if="lightbox"
      :images="asImages(lightbox.block).images.map(assetUrl)"
      :index="lightbox.index"
      @update:index="lightbox && (lightbox.index = $event)"
      @remove="deleteImage(lightbox.block, $event)"
      @close="lightbox = null"
    />
  </div>
</template>

<style scoped>
.proj {
  background: var(--card-color);
  border-radius: 12px;
  padding: 10px 12px;
  margin-bottom: 10px;
}

.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
}

.head-main {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  background: none;
  border: none;
  color: var(--text-color);
  text-align: left;
  padding: 2px 0;
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
  font-size: 12px;
  flex: none;
}

.chevron.open {
  transform: rotate(90deg);
}

.chevron.sm {
  font-size: 10px;
}

.icon-dot {
  flex: none;
  width: 30px;
  height: 30px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
}

.name {
  flex: 1;
  min-width: 0;
  font-weight: 700;
  font-size: 15px;
  overflow-wrap: anywhere;
}

.star {
  color: #f44336;
  margin-left: 2px;
}

.status {
  flex: none;
  font-size: 11px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.gear {
  background: none;
  border: none;
  padding: 2px 6px;
  font-size: 15px;
  flex: none;
}

.cover {
  width: 100%;
  max-height: 160px;
  object-fit: cover;
  border-radius: 10px;
  margin-top: 8px;
}

.descr {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 8px 0 0;
}

.meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  font-size: 11px;
  color: var(--text-secondary);
  margin-top: 6px;
}

.tag {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 1px 7px;
}

.owner {
  font-weight: 600;
}

.loading {
  text-align: center;
  color: var(--text-secondary);
  padding: 12px 0;
  font-size: 13px;
}

/* --- блоки --- */

.block {
  background: var(--bg-secondary);
  border-radius: 10px;
  padding: 8px 10px;
  margin-top: 8px;
}

.b-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
}

.b-toggle {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  color: var(--text-color);
  text-align: left;
  padding: 0;
  font-size: 13px;
}

.b-kind {
  flex: none;
}

.b-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 600;
}

.b-count {
  flex: none;
  font-size: 11px;
  color: var(--text-secondary);
}

.b-menu-btn {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 15px;
  padding: 0 6px;
}

.b-menu {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}

.b-menu button,
.b-menu .bg-pick {
  border: none;
  border-radius: 8px;
  background: var(--card-color);
  color: var(--text-color);
  font-size: 12px;
  padding: 6px 9px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.b-menu .danger {
  color: #f66;
}

.bg-pick input {
  width: 26px;
  height: 20px;
  padding: 0;
  border: none;
  background: none;
}

.b-body {
  margin-top: 6px;
}

.plain {
  font-size: 14px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin: 0;
}

.missing {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0;
}

.img-single {
  width: 100%;
  border-radius: 8px;
  display: block;
}

.img-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 4px;
}

.img-grid img {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  border-radius: 6px;
}

.file-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-color);
  text-decoration: none;
}

.file-size {
  font-size: 11px;
  color: var(--text-secondary);
}

.chk-item {
  font-size: 13px;
  padding: 2px 0;
}

.chk-item.done {
  color: var(--text-secondary);
  text-decoration: line-through;
}

.chk-sub {
  margin: 4px 0 0 12px;
}

.chk-sub-name {
  font-size: 12px;
  font-weight: 700;
  color: var(--text-secondary);
}

.src-link {
  display: inline-block;
  font-size: 11px;
  color: var(--accent-color);
  text-decoration: none;
  margin-top: 4px;
}

.task-row {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 3px 0;
  font-size: 13px;
}

.prio {
  flex: none;
  width: 4px;
  height: 16px;
  border-radius: 2px;
}

.task-title {
  min-width: 0;
  overflow-wrap: anywhere;
}

.task-title.struck {
  text-decoration: line-through;
  color: var(--text-secondary);
}

.task-meta {
  flex: none;
  font-size: 11px;
  color: var(--text-secondary);
}

.add-menu {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}

.add-menu button {
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
  font-size: 12px;
  padding: 7px 10px;
}

.add-menu .cancel {
  color: var(--text-secondary);
}

.add-block-btn {
  display: block;
  width: 100%;
  margin-top: 8px;
  padding: 8px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 13px;
}

.add-block-btn.top-add {
  padding: 4px;
  font-size: 12px;
  opacity: 0.85;
}

.add-menu.top-menu {
  margin-bottom: 2px;
}

/* --- модалки --- */

.tcard {
  max-height: 88vh;
  overflow-y: auto;
  text-align: left;
}

.tcard h3 {
  text-align: center;
  margin-top: 0;
}

.tcard textarea,
.tcard input {
  width: 100%;
  margin-bottom: 8px;
}

.tcard textarea {
  resize: vertical;
}

.loc-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px;
}

.check-line {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  cursor: pointer;
}

.check-line input[type='checkbox'] {
  width: 18px !important;
  height: 18px;
  flex: none;
  margin: 0;
}

.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 8px;
}

.ref-list {
  max-height: 40vh;
  overflow-y: auto;
  margin-bottom: 8px;
}

.ref-item {
  display: block;
  width: 100%;
  text-align: left;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  padding: 9px 10px;
  margin-bottom: 5px;
  font-size: 13px;
}

.ref-new {
  display: flex;
  gap: 6px;
}

.ref-new input {
  flex: 1;
  min-width: 0;
  margin-bottom: 0;
}

.ref-new button {
  flex: none;
  border: none;
  border-radius: 8px;
  padding: 0 12px;
  background: var(--accent-color);
  color: #fff;
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
</style>
