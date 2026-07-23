<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { showToast } from '../../../shared/toast'
import * as foodApi from '../api'
import MealModal from './MealModal.vue'
import {
  addDays,
  assetUrl,
  fmtDay,
  MEAL_TYPE_LABELS,
  MEAL_TYPES,
  r0,
  r1,
  todayStr,
  type FoodDiary,
  type FoodMeal,
  type FoodRecipe,
  type FoodTemplate,
} from '../types'

const day = ref(todayStr())
const diary = ref<FoodDiary | null>(null)
const loading = ref(true)
const failed = ref(false)

async function load() {
  loading.value = true
  failed.value = false
  try {
    diary.value = await foodApi.fetchDiary(day.value)
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(day, load)

const groups = computed(() => {
  const by: Record<string, FoodMeal[]> = {}
  for (const m of diary.value?.meals ?? []) (by[m.meal_type] ??= []).push(m)
  return MEAL_TYPES.map((t) => ({
    type: t,
    label: MEAL_TYPE_LABELS[t],
    meals: by[t] ?? [],
    totals: (by[t] ?? []).reduce(
      (acc, m) => ({
        c: acc.c + m.calories,
        p: acc.p + m.protein,
        f: acc.f + m.fat,
        cb: acc.cb + m.carbs,
      }),
      { c: 0, p: 0, f: 0, cb: 0 },
    ),
  }))
})

// прогресс: доля цели (для бара — обрезаем 100%, текст показывает превышение)
function pct(v: number, goal: number): number {
  if (goal <= 0) return 0
  return Math.min(100, (v / goal) * 100)
}

function over(v: number, goal: number): number {
  return goal > 0 && v > goal ? v - goal : 0
}

// --- добавление ---
const addMenu = ref(false)
const mealModal = ref(false)
const editingMeal = ref<FoodMeal | null>(null)

function openCreate() {
  editingMeal.value = null
  mealModal.value = true
  addMenu.value = false
}

function openEdit(m: FoodMeal) {
  editingMeal.value = m
  mealModal.value = true
}

function onSaved() {
  mealModal.value = false
  load()
}

// из шаблона / рецепта
const tplPicker = ref(false)
const templates = ref<FoodTemplate[]>([])
const recipePicker = ref(false)
const recipes = ref<FoodRecipe[]>([])
const recipeFor = ref<FoodRecipe | null>(null)
const recipeGrams = ref<number | null>(null)
const recipePortions = ref<number | null>(null)

async function openTplPicker() {
  addMenu.value = false
  try {
    templates.value = (await foodApi.fetchTemplates()).templates.filter((t) => !t.archived)
  } catch {
    showToast('Не удалось загрузить шаблоны')
    return
  }
  tplPicker.value = true
}

async function useTemplate(t: FoodTemplate) {
  try {
    await foodApi.templateToMeal(t.id, day.value)
    tplPicker.value = false
    showToast('Добавлено из шаблона ✅')
    load()
  } catch {
    showToast('Не удалось добавить')
  }
}

async function openRecipePicker() {
  addMenu.value = false
  try {
    recipes.value = (await foodApi.fetchRecipes()).recipes.filter((r) => !r.archived)
  } catch {
    showToast('Не удалось загрузить рецепты')
    return
  }
  recipePicker.value = true
}

async function useRecipe() {
  const rec = recipeFor.value
  if (!rec) return
  const grams = recipeGrams.value ?? 0
  const portions = recipePortions.value ?? 0
  if (grams <= 0 && portions <= 0) {
    showToast('Укажите вес или число порций')
    return
  }
  try {
    await foodApi.recipeToMeal(rec.id, day.value, {
      grams: grams > 0 ? grams : undefined,
      portions: grams > 0 ? undefined : portions,
    })
    recipeFor.value = null
    recipePicker.value = false
    showToast('Добавлено из рецепта ✅')
    load()
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось добавить')
  }
}

// --- действия над записью ---
const menuFor = ref<FoodMeal | null>(null)
const confirmDelete = ref(false)
const moveFor = ref<FoodMeal | null>(null)
const moveDay = ref('')

function openMenu(m: FoodMeal) {
  menuFor.value = m
  confirmDelete.value = false
}

async function del() {
  const m = menuFor.value
  if (!m) return
  if (!confirmDelete.value) {
    confirmDelete.value = true
    return
  }
  try {
    await foodApi.deleteMeal(m.id)
    menuFor.value = null
    showToast('Удалено')
    load()
  } catch {
    showToast('Не удалось удалить')
  }
}

async function duplicate() {
  const m = menuFor.value
  if (!m) return
  try {
    await foodApi.duplicateMeal(m.id, day.value)
    menuFor.value = null
    showToast('Продублировано ✅')
    load()
  } catch {
    showToast('Не удалось продублировать')
  }
}

function openMove() {
  moveFor.value = menuFor.value
  moveDay.value = addDays(day.value, 1)
  menuFor.value = null
}

async function move() {
  const m = moveFor.value
  if (!m || !moveDay.value) return
  try {
    await foodApi.updateMeal(m.id, { day: moveDay.value })
    moveFor.value = null
    showToast('Перенесено на ' + fmtDay(moveDay.value) + ' ✅')
    load()
  } catch {
    showToast('Не удалось перенести')
  }
}

async function saveAsTemplate() {
  const m = menuFor.value
  if (!m) return
  try {
    await foodApi.mealToTemplate(m.id)
    menuFor.value = null
    showToast('Сохранено как шаблон ✅')
  } catch {
    showToast('Не удалось сохранить шаблон')
  }
}

async function makeRecipe() {
  const m = menuFor.value
  if (!m) return
  try {
    await foodApi.createRecipe({
      name: m.name || 'Рецепт',
      description: m.description,
      photo: m.photo,
      items: m.items,
      final_weight: m.items.reduce((s, i) => s + i.grams, 0),
    })
    menuFor.value = null
    showToast('Рецепт создан — вкладка Рецепты ✅')
  } catch {
    showToast('Не удалось создать рецепт')
  }
}
</script>

<template>
  <!-- выбор даты -->
  <div class="date-nav">
    <button class="nav-btn" @click="day = addDays(day, -1)">‹</button>
    <label class="date-label">
      <b>{{ day === todayStr() ? 'Сегодня' : fmtDay(day) }}</b>
      <input v-model="day" type="date" class="date-input" />
    </label>
    <button class="nav-btn" @click="day = addDays(day, 1)">›</button>
    <button v-if="day !== todayStr()" class="today-btn" @click="day = todayStr()">Сегодня</button>
  </div>

  <div v-if="loading" class="skeleton"></div>
  <p v-else-if="failed" class="hint">
    Не удалось загрузить <button class="retry" @click="load">повторить</button>
  </p>

  <template v-else-if="diary">
    <!-- дневная сводка -->
    <div class="summary card-glass">
      <div class="cal-line">
        <b>{{ r0(diary.summary.calories) }}</b>
        <template v-if="diary.goal"> / {{ r0(diary.goal.calories) }}</template> ккал
        <span v-if="diary.goal && !over(diary.summary.calories, diary.goal.calories)" class="rest">
          осталось {{ r0(diary.goal.calories - diary.summary.calories) }}
        </span>
        <span v-else-if="diary.goal" class="over-txt">
          превышение {{ r0(over(diary.summary.calories, diary.goal.calories)) }} ккал
        </span>
      </div>
      <div v-if="diary.goal" class="bar">
        <div
          class="fill"
          :class="{ over: over(diary.summary.calories, diary.goal.calories) > 0 }"
          :style="{ width: pct(diary.summary.calories, diary.goal.calories) + '%' }"
        ></div>
      </div>
      <div
        v-for="row in ([
          ['Белки', diary.summary.protein, diary.goal?.protein ?? 0],
          ['Жиры', diary.summary.fat, diary.goal?.fat ?? 0],
          ['Углеводы', diary.summary.carbs, diary.goal?.carbs ?? 0],
        ] as [string, number, number][])"
        :key="row[0]"
        class="macro"
      >
        <span class="m-name">{{ row[0] }}</span>
        <div class="bar small">
          <div
            class="fill"
            :class="{ over: over(row[1], row[2]) > 0 }"
            :style="{ width: pct(row[1], row[2]) + '%' }"
          ></div>
        </div>
        <span class="m-val">
          {{ r0(row[1]) }}<template v-if="row[2]"> / {{ r0(row[2]) }}</template> г
          <b v-if="over(row[1], row[2]) > 0" class="over-txt">+{{ r0(over(row[1], row[2])) }}</b>
        </span>
      </div>
      <p v-if="!diary.goal" class="no-goal">Цель не задана — настройте профиль питания ⚙️</p>
    </div>

    <!-- группы приёмов пищи -->
    <p v-if="diary.meals.length === 0" class="hint">
      {{ day === todayStr() ? 'Сегодня' : 'В этот день' }} ещё нет записей о питании.<br />
      Добавьте первый приём пищи 👇
    </p>

    <div v-for="g in groups" :key="g.type" class="group card-glass" :class="{ empty: g.meals.length === 0 }">
      <div class="g-head">
        <span class="g-name">{{ g.label }}</span>
        <span v-if="g.meals.length" class="g-totals">
          {{ r0(g.totals.c) }} ккал · Б {{ r1(g.totals.p) }} · Ж {{ r1(g.totals.f) }} · У {{ r1(g.totals.cb) }}
        </span>
        <span v-else class="g-totals">—</span>
      </div>
      <div v-for="m in g.meals" :key="m.id" class="meal" @click="openEdit(m)">
        <img v-if="m.photo" :src="assetUrl(m.photo)" class="m-photo" loading="lazy" alt="" />
        <div class="m-body">
          <div class="m-title">
            <span v-if="m.time" class="m-time">{{ m.time }}</span>
            {{ m.name || 'Без названия' }}
            <span v-if="m.source_type === 'template'" title="Из шаблона">📋</span>
            <span v-if="m.source_type === 'recipe'" title="Из рецепта">📖</span>
          </div>
          <div v-if="m.description" class="m-desc">{{ m.description }}</div>
          <div class="m-kbju">
            <b>{{ r0(m.calories) }} ккал</b> · Б {{ r1(m.protein) }} · Ж {{ r1(m.fat) }} · У {{ r1(m.carbs) }}
            <span v-if="m.items.length" class="m-count">· {{ m.items.length }} эл.</span>
          </div>
        </div>
        <button class="m-menu" @click.stop="openMenu(m)">⋮</button>
      </div>
    </div>

    <!-- добавить -->
    <button class="add-main" @click="addMenu = true">＋ Добавить приём пищи</button>
  </template>

  <!-- меню добавления -->
  <div v-if="addMenu" class="modal" @click.self="addMenu = false">
    <div class="modal-content menu-box">
      <h3>Добавить</h3>
      <button class="btn" @click="openCreate">🍽 Вручную</button>
      <button class="btn" @click="openTplPicker">📋 Из шаблона</button>
      <button class="btn" @click="openRecipePicker">📖 Из рецепта</button>
      <button class="btn" @click="addMenu = false">Отмена</button>
    </div>
  </div>

  <!-- пикер шаблона -->
  <div v-if="tplPicker" class="modal" @click.self="tplPicker = false">
    <div class="modal-content menu-box">
      <h3>📋 Шаблон</h3>
      <p v-if="templates.length === 0" class="hint">Шаблонов пока нет — сохраните приём пищи как шаблон через меню ⋮</p>
      <button v-for="t in templates" :key="t.id" class="btn pick" @click="useTemplate(t)">
        {{ t.name }} <span class="pick-sub">{{ r0(t.calories) }} ккал</span>
      </button>
      <button class="btn" @click="tplPicker = false">Отмена</button>
    </div>
  </div>

  <!-- пикер рецепта -->
  <div v-if="recipePicker" class="modal" @click.self="recipePicker = false; recipeFor = null">
    <div class="modal-content menu-box">
      <template v-if="!recipeFor">
        <h3>📖 Рецепт</h3>
        <p v-if="recipes.length === 0" class="hint">Рецептов пока нет — создайте на вкладке Рецепты</p>
        <button v-for="rec in recipes" :key="rec.id" class="btn pick" @click="recipeFor = rec; recipeGrams = null; recipePortions = null">
          {{ rec.name }} <span class="pick-sub">{{ r0(rec.calories) }} ккал всего</span>
        </button>
        <button class="btn" @click="recipePicker = false">Отмена</button>
      </template>
      <template v-else>
        <h3>{{ recipeFor.name }}</h3>
        <label v-if="recipeFor.final_weight > 0" class="rc-row">
          <span>Съеденный вес, г</span>
          <input v-model.number="recipeGrams" type="number" min="1" step="any" />
        </label>
        <label v-if="recipeFor.portions > 0" class="rc-row">
          <span>Или порций</span>
          <input v-model.number="recipePortions" type="number" min="0.1" step="any" />
        </label>
        <p v-if="recipeFor.final_weight === 0 && recipeFor.portions === 0" class="hint">
          В рецепте не указан ни итоговый вес, ни порции — добавьте их в рецепт
        </p>
        <button class="btn primary" @click="useRecipe">💾 Добавить в дневник</button>
        <button class="btn" @click="recipeFor = null">← Назад</button>
      </template>
    </div>
  </div>

  <!-- меню записи -->
  <div v-if="menuFor" class="modal" @click.self="menuFor = null">
    <div class="modal-content menu-box">
      <h3>{{ menuFor.name || 'Приём пищи' }}</h3>
      <button class="btn" @click="openEdit(menuFor); menuFor = null">✏️ Редактировать</button>
      <button class="btn" @click="duplicate">📄 Дублировать</button>
      <button class="btn" @click="openMove">📅 Перенести на дату</button>
      <button class="btn" @click="saveAsTemplate">📋 Сохранить как шаблон</button>
      <button v-if="menuFor.items.length" class="btn" @click="makeRecipe">📖 Создать рецепт</button>
      <button class="btn danger" @click="del">{{ confirmDelete ? 'Точно удалить?' : '🗑 Удалить' }}</button>
      <button class="btn" @click="menuFor = null">Отмена</button>
    </div>
  </div>

  <!-- перенос на дату -->
  <div v-if="moveFor" class="modal" @click.self="moveFor = null">
    <div class="modal-content menu-box">
      <h3>📅 Перенести</h3>
      <input v-model="moveDay" type="date" class="move-date" />
      <button class="btn primary" @click="move">Перенести</button>
      <button class="btn" @click="moveFor = null">Отмена</button>
    </div>
  </div>

  <MealModal v-if="mealModal" :meal="editingMeal" :day="day" @saved="onSaved" @close="mealModal = false" />
</template>

<style scoped>
.date-nav {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}

.nav-btn {
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  padding: 6px 14px;
  font-size: 16px;
  color: var(--text-color);
}

.date-label {
  position: relative;
  flex: 1;
  text-align: center;
  font-size: 15px;
}

.date-input {
  position: absolute;
  inset: 0;
  opacity: 0;
  width: 100%;
}

.today-btn {
  background: none;
  border: 1px solid var(--hover-bg-color);
  border-radius: 8px;
  padding: 5px 10px;
  font-size: 12px;
  color: var(--accent-color);
}

.skeleton {
  height: 140px;
  border-radius: 8px;
  background: linear-gradient(90deg, var(--card-color), var(--bg-secondary), var(--card-color));
  background-size: 200% 100%;
  animation: shine 1.2s infinite;
}

@keyframes shine {
  to {
    background-position: -200% 0;
  }
}

.summary {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 12px;
}

.cal-line {
  font-size: 17px;
  margin-bottom: 6px;
}

.rest {
  font-size: 12px;
  color: var(--text-secondary);
  margin-left: 8px;
}

.over-txt {
  font-size: 12px;
  color: #ef4444;
  margin-left: 6px;
}

.bar {
  height: 8px;
  border-radius: 4px;
  background: var(--bg-secondary);
  overflow: hidden;
  margin-bottom: 8px;
}

.bar.small {
  height: 5px;
  flex: 1;
  margin-bottom: 0;
}

.fill {
  height: 100%;
  background: var(--accent-color);
  border-radius: 4px;
  transition: width 0.25s;
}

.fill.over {
  background: #ef4444;
}

.macro {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
  font-size: 12px;
}

.m-name {
  width: 68px;
  flex: none;
  color: var(--text-secondary);
}

.m-val {
  flex: none;
  min-width: 90px;
  text-align: right;
}

.no-goal {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 4px 0 0;
}

.group {
  background: var(--card-color);
  border-radius: 8px;
  padding: 8px 12px;
  margin-bottom: 10px;
}

.group.empty {
  padding: 6px 12px;
  opacity: 0.75;
}

.g-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}

.g-name {
  font-weight: 600;
  font-size: 14px;
}

.g-totals {
  font-size: 11px;
  color: var(--text-secondary);
  text-align: right;
}

.meal {
  display: flex;
  gap: 10px;
  align-items: flex-start;
  border-top: 1px solid var(--bg-secondary);
  margin-top: 8px;
  padding-top: 8px;
  cursor: pointer;
}

.m-photo {
  width: 52px;
  height: 52px;
  object-fit: cover;
  border-radius: 8px;
  flex: none;
}

.m-body {
  flex: 1;
  min-width: 0;
}

.m-title {
  font-size: 14px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.m-time {
  color: var(--text-secondary);
  font-weight: 400;
  font-size: 12px;
  margin-right: 4px;
}

.m-desc {
  font-size: 12px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}

.m-kbju {
  font-size: 12px;
  margin-top: 2px;
}

.m-count {
  color: var(--text-secondary);
}

.m-menu {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 16px;
  padding: 2px 6px;
  flex: none;
}

.add-main {
  position: sticky;
  bottom: 12px;
  display: block;
  width: 100%;
  padding: 12px;
  border: none;
  border-radius: 10px;
  background: var(--accent-color);
  color: #fff;
  font-size: 15px;
  font-weight: 600;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.3);
}

.hint {
  text-align: center;
  color: var(--text-secondary);
  font-size: 13px;
  padding: 14px 0;
}

.retry {
  background: none;
  border: none;
  color: var(--accent-color);
  text-decoration: underline;
}

.menu-box {
  text-align: left;
}

.menu-box h3 {
  text-align: center;
}

.btn {
  display: block;
  width: 100%;
  margin-top: 8px;
  padding: 10px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
  text-align: left;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
  text-align: center;
}

.btn.danger {
  background: #b91c1c;
  color: #fff;
}

.btn.pick {
  display: flex;
  justify-content: space-between;
  gap: 8px;
}

.pick-sub {
  color: var(--text-secondary);
  font-size: 12px;
  flex: none;
}

.btn.pick .pick-sub {
  color: inherit;
  opacity: 0.7;
}

.rc-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  font-size: 13px;
}

.rc-row span {
  flex: 1;
  color: var(--text-secondary);
}

.rc-row input {
  width: 110px;
}

.move-date {
  width: 100%;
  margin-top: 8px;
}
</style>
