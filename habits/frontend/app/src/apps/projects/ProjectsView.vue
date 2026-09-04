<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { loadCollapsed, saveCollapsed } from '../../shared/collapsed'
import { showToast } from '../../shared/toast'
import * as projApi from './api'
import type { Project, ProjectCategory } from './types'
import ProjectCard from './components/ProjectCard.vue'
import ProjectSettingsModal from './components/ProjectSettingsModal.vue'

const categories = ref<ProjectCategory[]>([])
const projects = ref<Project[]>([])
const types = ref<string[]>([])
const loading = ref(true)

// свёрнутость: проекты и категории — отдельные ключи на сервере
const collapsedProjects = ref(new Set<number>())
const collapsedCats = ref(new Set<number>())

const creating = ref(false)
const editProject = ref<Project | null>(null)

// категории: добавление и настройка
const addingCat = ref(false)
const newCatName = ref('')
const editCat = ref<ProjectCategory | null>(null)
const editCatName = ref('')

async function load() {
  try {
    const data = await projApi.fetchProjects()
    categories.value = data.categories
    projects.value = data.projects
    types.value = data.types
  } catch {
    showToast('Не удалось загрузить проекты')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  load()
  loadCollapsed('projects').then((s) => (collapsedProjects.value = s))
  loadCollapsed('projects_cat').then((s) => (collapsedCats.value = s))
})

function toggleProject(id: number) {
  if (collapsedProjects.value.has(id)) collapsedProjects.value.delete(id)
  else collapsedProjects.value.add(id)
  collapsedProjects.value = new Set(collapsedProjects.value)
  saveCollapsed('projects', collapsedProjects.value)
}

function toggleCat(id: number) {
  if (collapsedCats.value.has(id)) collapsedCats.value.delete(id)
  else collapsedCats.value.add(id)
  collapsedCats.value = new Set(collapsedCats.value)
  saveCollapsed('projects_cat', collapsedCats.value)
}

const grouped = computed(() => {
  const byCat = new Map<number, Project[]>()
  const general: Project[] = []
  for (const p of projects.value) {
    if (p.category_id !== null && categories.value.some((c) => c.id === p.category_id)) {
      if (!byCat.has(p.category_id)) byCat.set(p.category_id, [])
      byCat.get(p.category_id)!.push(p)
    } else {
      general.push(p)
    }
  }
  return { byCat, general }
})

function replaceProject(p: Project) {
  const i = projects.value.findIndex((x) => x.id === p.id)
  if (i >= 0) projects.value[i] = p
  else projects.value.push(p)
}

function onSaved(p: Project) {
  replaceProject(p)
  creating.value = false
  editProject.value = null
  // новые типы попадают в подсказки
  if (p.ptype && !types.value.includes(p.ptype)) types.value.push(p.ptype)
}

async function onRemoved(id: number) {
  projects.value = projects.value.filter((p) => p.id !== id)
  editProject.value = null
}

/** Проект открыт (загружен) — звёздочка «изменён другим» гаснет. */
function onViewed(id: number) {
  const p = projects.value.find((x) => x.id === id)
  if (p) p.changed = false
}

async function addCategory() {
  const name = newCatName.value.trim()
  if (!name) return
  try {
    const { category } = await projApi.createCategory(name)
    categories.value.push(category)
    newCatName.value = ''
    addingCat.value = false
  } catch {
    showToast('Не удалось создать категорию')
  }
}

function openCatSettings(c: ProjectCategory) {
  editCat.value = c
  editCatName.value = c.name
}

async function saveCat() {
  const c = editCat.value
  const name = editCatName.value.trim()
  if (!c || !name) return
  try {
    await projApi.renameCategory(c.id, name)
    c.name = name
    editCat.value = null
  } catch {
    showToast('Не удалось переименовать')
  }
}

async function removeCat() {
  const c = editCat.value
  if (!c) return
  try {
    await projApi.deleteCategory(c.id)
    categories.value = categories.value.filter((x) => x.id !== c.id)
    // проекты категории падают в общий список
    for (const p of projects.value) if (p.category_id === c.id) p.category_id = null
    editCat.value = null
  } catch {
    showToast('Не удалось удалить категорию')
  }
}
</script>

<template>
  <div v-if="loading" class="loading">Загрузка…</div>

  <template v-else>
    <!-- категории -->
    <div v-for="c in categories" :key="'c' + c.id" class="cat">
      <div class="cat-head">
        <button class="cat-toggle" @click="toggleCat(c.id)">
          <span class="chevron" :class="{ open: !collapsedCats.has(c.id) }">▸</span>
          🗂 {{ c.name }}
          <span class="count">{{ grouped.byCat.get(c.id)?.length ?? 0 }}</span>
        </button>
        <button class="gear" title="Настройки категории" @click="openCatSettings(c)">⚙️</button>
      </div>
      <template v-if="!collapsedCats.has(c.id)">
        <ProjectCard
          v-for="p in grouped.byCat.get(c.id) ?? []"
          :key="p.id"
          :project="p"
          :expanded="!collapsedProjects.has(p.id)"
          @toggle="toggleProject(p.id)"
          @viewed="onViewed(p.id)"
          @settings="editProject = p"
          @meta="replaceProject"
        />
        <p v-if="!(grouped.byCat.get(c.id)?.length)" class="cat-empty">Пусто</p>
      </template>
    </div>

    <!-- общий список -->
    <ProjectCard
      v-for="p in grouped.general"
      :key="p.id"
      :project="p"
      :expanded="!collapsedProjects.has(p.id)"
      @toggle="toggleProject(p.id)"
      @viewed="onViewed(p.id)"
      @settings="editProject = p"
      @meta="replaceProject"
    />

    <p v-if="projects.length === 0" class="empty">
      Проект — сборник всего: заметки, картинки, файлы, чек-листы, статьи и задачи в одном месте.
    </p>

    <button class="add-btn" @click="creating = true">＋ Проект</button>

    <form v-if="addingCat" class="add-cat" @submit.prevent="addCategory">
      <input v-model="newCatName" placeholder="Название категории…" maxlength="200" />
      <button type="submit" :disabled="!newCatName.trim()">Создать</button>
      <button type="button" @click="addingCat = false">✕</button>
    </form>
    <button v-else class="add-cat-btn" @click="addingCat = true">＋ Категория</button>
  </template>

  <ProjectSettingsModal
    v-if="creating || editProject"
    :key="editProject?.id ?? 0"
    :project="editProject"
    :categories="categories"
    :types="types"
    @saved="onSaved"
    @removed="onRemoved"
    @close="creating = false; editProject = null"
  />

  <!-- настройки категории -->
  <Teleport to="body">
    <div v-if="editCat" class="modal" @click.self="editCat = null">
      <div class="modal-content card">
        <h3>Категория</h3>
        <input v-model="editCatName" maxlength="200" />
        <button class="btn primary" :disabled="!editCatName.trim()" @click="saveCat">
          💾 Сохранить
        </button>
        <button class="btn danger" @click="removeCat">
          🗑 Удалить (проекты — в общий список)
        </button>
        <button class="btn" @click="editCat = null">Отмена</button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.loading,
.empty {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.cat {
  margin-bottom: 10px;
}

.cat-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.cat-toggle {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 6px;
  background: none;
  border: none;
  color: var(--text-color);
  font-weight: 700;
  font-size: 15px;
  text-align: left;
  padding: 8px 2px;
}

.chevron {
  display: inline-block;
  transition: transform 0.15s;
  color: var(--text-secondary);
  font-size: 12px;
}

.chevron.open {
  transform: rotate(90deg);
}

.count {
  font-weight: 400;
  font-size: 12px;
  color: var(--text-secondary);
}

.gear {
  background: none;
  border: none;
  padding: 2px 6px;
  font-size: 15px;
}

.cat-empty {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 8px 20px;
}

.add-btn {
  display: block;
  width: 100%;
  margin-top: 12px;
  padding: 11px;
  border: none;
  border-radius: 10px;
  background: var(--accent-color);
  color: #fff;
  font-size: 14px;
}

.add-cat {
  display: flex;
  gap: 6px;
  margin-top: 8px;
}

.add-cat input {
  flex: 1;
  min-width: 0;
}

.add-cat button {
  flex: none;
  border: none;
  border-radius: 8px;
  padding: 0 12px;
  background: var(--bg-secondary);
  color: var(--text-color);
}

.add-cat-btn {
  display: block;
  width: 100%;
  margin-top: 8px;
  padding: 10px;
  border: none;
  border-radius: 10px;
  background: var(--card-color);
  color: var(--text-secondary);
}

.modal-content h3 {
  text-align: center;
  margin-top: 0;
}

.modal-content input {
  width: 100%;
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
</style>
