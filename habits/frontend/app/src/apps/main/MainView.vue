<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { pageAllowed } from '../../shared/access'
import { api } from '../../shared/api/client'
import { fetchUpcoming } from '../reminders/api'
import { fmtDateTime, type Reminder } from '../reminders/types'

const router = useRouter()
const allApps = router.getRoutes().filter((r) => r.meta.app)
const apps = computed(() => allApps.filter((r) => pageAllowed(String(r.name))))

const upcoming = ref<Reminder[]>([])

// бейдж на плитке Tasks: «сегодня + просрочено»
const tasksBadge = ref({ today: 0, overdue: 0 })
const tasksBadgeTotal = computed(() => tasksBadge.value.today + tasksBadge.value.overdue)

// --- глобальный поиск по данным всех страниц (кроме паролей) ---
interface SearchHit {
  page: string
  title: string
  subtitle: string
}
const searchQ = ref('')
const searchHits = ref<SearchHit[]>([])
const searching = ref(false)
const searched = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | undefined

const pageMeta = new Map(allApps.map((r) => [String(r.name), { icon: r.meta.icon ?? '', path: r.path }]))

const groupedHits = computed(() => {
  const groups = new Map<string, SearchHit[]>()
  for (const h of searchHits.value) {
    if (!groups.has(h.page)) groups.set(h.page, [])
    groups.get(h.page)!.push(h)
  }
  return [...groups.entries()].map(([page, hits]) => ({
    page,
    icon: pageMeta.get(page)?.icon ?? '📄',
    path: pageMeta.get(page)?.path ?? '/',
    hits,
  }))
})

function onSearchInput() {
  clearTimeout(searchTimer)
  const q = searchQ.value.trim()
  if (q.length < 2) {
    searchHits.value = []
    searched.value = false
    return
  }
  searchTimer = setTimeout(async () => {
    searching.value = true
    try {
      searchHits.value = (await api.get<{ hits: SearchHit[] }>(`/search?q=${encodeURIComponent(q)}`)).hits
      searched.value = true
    } catch {
      searchHits.value = []
    } finally {
      searching.value = false
    }
  }, 300)
}

onMounted(async () => {
  if (pageAllowed('tasks')) {
    api
      .get<{ today: number; overdue: number }>(`/tasks/summary?tz=${-new Date().getTimezoneOffset()}`)
      .then((s) => (tasksBadge.value = s))
      .catch(() => {})
  }
  try {
    upcoming.value = (await fetchUpcoming(3)).reminders
  } catch {
    // блок просто не показываем — главная должна открываться всегда
  }
})

onUnmounted(() => clearTimeout(searchTimer))
</script>

<template>
  <div class="search-box">
    <input
      v-model="searchQ"
      type="search"
      placeholder="🔍 Поиск по всем страницам…"
      @input="onSearchInput"
    />
  </div>

  <div v-if="searchQ.trim().length >= 2" class="search-results">
    <p v-if="searching" class="search-hint">Ищем…</p>
    <p v-else-if="searched && searchHits.length === 0" class="search-hint">Ничего не найдено</p>
    <RouterLink v-for="g in groupedHits" :key="g.page" :to="g.path" class="search-group">
      <div class="sg-title">{{ g.icon }} {{ g.page }}</div>
      <div v-for="(h, i) in g.hits" :key="i" class="sg-hit">
        <span class="sg-name">{{ h.title }}</span>
        <span class="sg-sub">{{ h.subtitle }}</span>
      </div>
    </RouterLink>
  </div>

  <p class="intro">Все мини-приложения в одном месте. Выберите, с чего начать:</p>

  <div class="tiles">
    <RouterLink v-for="app in apps" :key="app.path" :to="app.path" class="tile">
      <span class="tile-icon">{{ app.meta.icon }}</span>
      <span class="tile-title">{{ app.meta.title }}</span>
      <span
        v-if="app.name === 'tasks' && tasksBadgeTotal > 0"
        class="tile-badge"
        :class="{ red: tasksBadge.overdue > 0 }"
        :title="`Сегодня: ${tasksBadge.today}, просрочено: ${tasksBadge.overdue}`"
      >
        {{ tasksBadgeTotal }}
      </span>
    </RouterLink>
  </div>

  <RouterLink v-if="upcoming.length > 0" to="/reminders" class="upcoming">
    <div class="upcoming-title">🔔 Ближайшие напоминания</div>
    <div v-for="r in upcoming" :key="r.id" class="upcoming-row">
      <span class="upcoming-name">{{ r.kind === 'tracker' ? '📊 ' : '' }}{{ r.title }}</span>
      <span class="upcoming-time">{{ r.next_fire_at ? fmtDateTime(r.next_fire_at) : '' }}</span>
    </div>
  </RouterLink>
</template>

<style scoped>
.search-box input {
  width: 100%;
  margin-bottom: 12px;
}

.search-results {
  margin-bottom: 16px;
}

.search-hint {
  color: var(--text-secondary);
  font-size: 13px;
  text-align: center;
  margin: 8px 0;
}

.search-group {
  display: block;
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 8px;
  text-decoration: none;
  color: var(--text-color);
}

.sg-title {
  font-size: 12px;
  font-weight: 700;
  color: var(--text-secondary);
  text-transform: capitalize;
  margin-bottom: 4px;
}

.sg-hit {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 10px;
  padding: 2px 0;
  font-size: 13px;
}

.sg-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sg-sub {
  flex: none;
  max-width: 45%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 11px;
  color: var(--text-secondary);
}

.intro {
  color: var(--text-secondary);
  margin: 4px 0 16px;
}

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  gap: 10px;
}

.tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  aspect-ratio: 1 / 1;
  background: var(--card-color);
  border-radius: 14px;
  text-decoration: none;
  color: var(--text-color);
  transition: transform 0.12s, background 0.12s;
}

.tile:active {
  transform: scale(0.95);
  background: var(--bg-secondary);
}

.tile {
  position: relative;
}

.tile-badge {
  position: absolute;
  top: 8px;
  right: 8px;
  min-width: 20px;
  height: 20px;
  padding: 0 5px;
  border-radius: 10px;
  background: var(--accent-color);
  color: #fff;
  font-size: 12px;
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
}

.tile-badge.red {
  background: #f44336;
}

.tile-icon {
  font-size: 34px;
}

.tile-title {
  font-size: 13px;
  font-weight: 600;
}

.upcoming {
  display: block;
  margin-top: 16px;
  background: var(--card-color);
  border-radius: 12px;
  padding: 12px 14px;
  text-decoration: none;
  color: var(--text-color);
}

.upcoming-title {
  font-size: 13px;
  font-weight: 700;
  margin-bottom: 6px;
}

.upcoming-row {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 10px;
  font-size: 13px;
  padding: 3px 0;
}

.upcoming-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.upcoming-time {
  flex: none;
  color: var(--accent-color);
  font-size: 12px;
}

.mini-settings {
  margin-top: 16px;
  background: var(--card-color);
  border-radius: 12px;
  padding: 12px 14px;
}

.setting {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
}

.setting-hint {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--text-secondary);
}
</style>
