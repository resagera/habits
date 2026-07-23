<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import * as foodApi from './api'
import DiaryTab from './components/DiaryTab.vue'
import ProfileModal from './components/ProfileModal.vue'
import RecipesTab from './components/RecipesTab.vue'
import SharedTab from './components/SharedTab.vue'
import StatsTab from './components/StatsTab.vue'
import type { FoodProfile } from './types'

// Food: 4 вкладки, активная — в query (?tab=), восстанавливается после
// обновления страницы. Первый заход без профиля → onboarding.
const route = useRoute()
const router = useRouter()

const TABS = [
  { key: 'diary', label: '📔 Дневник' },
  { key: 'shared', label: '👥 Общие' },
  { key: 'recipes', label: '📖 Рецепты' },
  { key: 'stats', label: '📈 Статистика' },
] as const
type TabKey = (typeof TABS)[number]['key']

const tab = computed<TabKey>(() => {
  const t = route.query.tab
  return TABS.some((x) => x.key === t) ? (t as TabKey) : 'diary'
})

function setTab(t: TabKey) {
  router.replace({ query: { ...route.query, tab: t === 'diary' ? undefined : t } })
}

// --- профиль ---
const profile = ref<FoodProfile | null>(null)
const profileModal = ref(false)
const checked = ref(false)
const diaryKey = ref(0) // перезагрузка дневника после смены целей

onMounted(async () => {
  try {
    const res = await foodApi.fetchProfile()
    profile.value = res.profile
    if (!res.profile) profileModal.value = true
  } catch {
    /* дневник работает и без профиля */
  } finally {
    checked.value = true
  }
})

async function onProfileSaved() {
  profileModal.value = false
  try {
    profile.value = (await foodApi.fetchProfile()).profile
  } catch {
    /* ок */
  }
  diaryKey.value++
}
</script>

<template>
  <div class="food-head">
    <div class="tabs">
      <button
        v-for="t in TABS"
        :key="t.key"
        class="tab"
        :class="{ on: tab === t.key }"
        @click="setTab(t.key)"
      >
        {{ t.label }}
      </button>
    </div>
    <button class="gear" title="Профиль питания и цели" @click="profileModal = true">⚙️</button>
  </div>

  <DiaryTab v-if="tab === 'diary'" :key="diaryKey" />
  <SharedTab v-else-if="tab === 'shared'" />
  <RecipesTab v-else-if="tab === 'recipes'" />
  <StatsTab v-else />

  <ProfileModal
    v-if="profileModal && checked"
    :profile="profile"
    @saved="onProfileSaved"
    @close="profileModal = false"
  />
</template>

<style scoped>
.food-head {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 12px;
}

.tabs {
  flex: 1;
  min-width: 0;
  display: flex;
  gap: 4px;
  overflow-x: auto;
  scrollbar-width: none;
}

.tabs::-webkit-scrollbar {
  display: none;
}

.tab {
  flex: none;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 7px 10px;
  font-size: 13px;
  color: var(--text-color);
  white-space: nowrap;
}

.tab.on {
  background: var(--accent-color);
  color: #fff;
}

.gear {
  flex: none;
  background: none;
  border: none;
  font-size: 18px;
  padding: 4px 6px;
}
</style>
