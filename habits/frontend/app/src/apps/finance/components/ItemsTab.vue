<script setup lang="ts">
// Товары из чеков: разметка по группам, история цен, настройка соответствия
// встроенных групп категориям.
//
// Разметка идёт по НАЗВАНИЮ товара, а не по строке чека: названия у магазина
// повторяются посимвольно, поэтому одно решение закрывает товар во всех чеках —
// и в прошлых тоже.
import { computed, onUnmounted, ref } from 'vue'
import { confirmAction } from '../../../shared/telegram'
import { showToast } from '../../../shared/toast'
import {
  assignItems, createCategory, createWordRule, deleteItemRule, deleteWordRule,
  fetchItemGroups, fetchItemPrices, fetchItemRules, fetchSuggestions, fetchTopItems,
  fetchUnclassified, seedItemGroups, setItemGroup, suggestGroups,
} from '../api'
import {
  fmtDate, fmtMoney, type CategoryItemStats, type FinanceRefs, type ItemGroup,
  type ItemPriceHistory, type ItemRule, type ItemSuggestion, type TopItem,
  type WordRule,
} from '../types'
import CategoryPicker from './CategoryPicker.vue'

const props = defineProps<{ refs: FinanceRefs | null; hide: boolean }>()
const emit = defineEmits<{ changed: [] }>()

const view = ref<'todo' | 'top' | 'setup'>('todo')
const busy = ref(false)
const unclassified = ref<TopItem[]>([])
const top = ref<TopItem[]>([])
const rules = ref<ItemRule[]>([])
const groups = ref<ItemGroup[]>([])
const groupMap = ref<Record<string, number>>({})
const dictionary = ref<Record<string, string[]>>({})
const words = ref<WordRule[]>([])
const catStats = ref<CategoryItemStats[]>([])
const openWords = ref<Record<string, boolean>>({})
const wordForm = ref<{ categoryId: number; pattern: string } | null>(null)
const catForm = ref<{ name: string; icon: string } | null>(null)
// выбранная категория для каждого неразобранного товара
const picks = ref<Record<string, number>>({})
const suggestions = ref<Record<string, ItemSuggestion>>({})
const suggestState = ref<'' | 'running' | 'done' | 'failed'>('')
const suggestNote = ref('')

const base = computed(() => props.refs?.base_currency ?? 'amd')
const catName = computed(() => {
  const m = new Map<number, string>()
  for (const c of props.refs?.categories ?? []) {
    m.set(c.id, `${c.icon ? c.icon + ' ' : ''}${c.name}`)
  }
  return m
})
const groupsReady = computed(() => Object.keys(groupMap.value).length > 0)

/**
 * Несколько групп в одной категории — это состояние, при котором вся затея
 * теряет смысл: диаграмма схлопывается в один сектор. Молчать об этом нельзя.
 */
const clashing = computed(() => {
  const seen = new Map<number, string[]>()
  for (const [code, cat] of Object.entries(groupMap.value)) {
    const title = groups.value.find((g) => g.code === code)?.title ?? code
    seen.set(cat, [...(seen.get(cat) ?? []), title])
  }
  return [...seen.values()].filter((names) => names.length > 1)
})

function money(v: number): string {
  return props.hide ? '•••' : fmtMoney(v, base.value)
}

async function load() {
  try {
    const [u, g] = await Promise.all([fetchUnclassified(), fetchItemGroups()])
    unclassified.value = u.items
    groups.value = g.groups
    groupMap.value = g.map
    dictionary.value = g.dictionary ?? {}
    words.value = g.words ?? []
    catStats.value = g.stats ?? []
  } catch {
    showToast('Не удалось загрузить товары')
  }
}
void load()

async function loadTop() {
  try {
    top.value = (await fetchTopItems({ limit: 60 })).items
  } catch {
    showToast('Не удалось загрузить сводку')
  }
}

async function loadRules() {
  try {
    rules.value = (await fetchItemRules()).rules
  } catch {
    /* правила — не критично для остального экрана */
  }
}

function switchView(v: 'todo' | 'top' | 'setup') {
  view.value = v
  if (v === 'top') void loadTop()
  if (v === 'setup') void loadRules()
}

async function seed(reset = false) {
  if (reset && !(await confirmAction(
    'Создать шесть типовых категорий и привязать группы к ним? Уже разобранные чеки будут пересобраны; разметка, сделанная руками, сохранится.',
  ))) return
  busy.value = true
  try {
    const res = await seedItemGroups(reset)
    groupMap.value = res.map
    showToast(res.reclassified
      ? `Готово ✅ Пересобрано чеков: ${res.reclassified}`
      : 'Группы готовы ✅')
    emit('changed')
    await load()
  } catch {
    showToast('Не удалось создать группы')
  } finally {
    busy.value = false
  }
}

async function remapGroup(code: string, categoryId: number) {
  if (groupMap.value[code] === categoryId) return
  busy.value = true
  try {
    const res = await setItemGroup(code, categoryId)
    groupMap.value = { ...groupMap.value, [code]: categoryId }
    // чеки пересобираются сразу: иначе смена привязки не тронула бы отчёт и
    // выглядела бы сломанной
    showToast(res.reclassified
      ? `Пересобрано чеков: ${res.reclassified}`
      : 'Сохранено')
    emit('changed')
  } catch {
    showToast('Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

/** Разметить один товар — решение применится ко всем чекам, включая прошлые. */
async function assignOne(item: TopItem) {
  const cat = picks.value[item.name_key]
  if (!cat) {
    showToast('Выберите группу')
    return
  }
  busy.value = true
  try {
    await assignItems([{
      name_key: item.name_key, name_sample: item.name, category_id: cat,
    }], { source: suggestions.value[item.name_key] ? 'ai' : 'manual' })
    await load()
    emit('changed')
  } catch {
    showToast('Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

/** Применить всё, что выбрано (в том числе подставленное подсказками). */
async function assignAll() {
  const items = unclassified.value
    .filter((i) => picks.value[i.name_key])
    .map((i) => ({
      name_key: i.name_key, name_sample: i.name, category_id: picks.value[i.name_key],
    }))
  if (!items.length) {
    showToast('Ничего не выбрано')
    return
  }
  busy.value = true
  try {
    await assignItems(items)
    picks.value = {}
    suggestions.value = {}
    await load()
    emit('changed')
    showToast(`Размечено: ${items.length} ✅`)
  } catch {
    showToast('Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

// --- подсказки от AI-агента ---

let poll: ReturnType<typeof setInterval> | undefined
onUnmounted(() => clearInterval(poll))

/**
 * Агент только ПРЕДЛАГАЕТ: подсказки подставляются в выбор, но применяет их
 * человек кнопкой «Применить выбранное».
 */
async function askAI() {
  const names = unclassified.value.map((i) => i.name).slice(0, 100)
  if (!names.length) return
  suggestState.value = 'running'
  suggestNote.value = 'Агент думает…'
  try {
    const res = await suggestGroups(names)
    suggestNote.value = res.queued_offline
      ? `Агент «${res.machine}» офлайн — задача в очереди, включите машину`
      : `Агент «${res.machine}» работает…`
    clearInterval(poll)
    poll = setInterval(() => void checkAI(res.run_id), 3000)
  } catch (e) {
    suggestState.value = 'failed'
    suggestNote.value = e instanceof Error ? e.message : 'Не удалось спросить агента'
  }
}

async function checkAI(runId: number) {
  try {
    const res = await fetchSuggestions(runId)
    if (res.status !== 'done' && res.status !== 'error') return
    clearInterval(poll)
    if (res.status === 'error' || res.parse_error) {
      suggestState.value = 'failed'
      suggestNote.value = res.parse_error || res.error || 'Агент не справился'
      return
    }
    const map: Record<string, ItemSuggestion> = {}
    for (const s of res.suggestions ?? []) {
      if (s.category_id) {
        map[s.name_key] = s
        picks.value[s.name_key] = s.category_id
      }
    }
    suggestions.value = map
    suggestState.value = 'done'
    suggestNote.value = `Предложено: ${Object.keys(map).length}. Проверьте и примените.`
  } catch {
    clearInterval(poll)
    suggestState.value = 'failed'
    suggestNote.value = 'Не удалось забрать ответ'
  }
}

// --- история цен ---

const history = ref<ItemPriceHistory | null>(null)

async function openHistory(item: TopItem) {
  try {
    history.value = await fetchItemPrices(item.name_key)
  } catch {
    showToast('Не удалось загрузить историю')
  }
}

/** Товары, сгруппированные по категории: иначе список из сотни строк нечитаем. */
const topByCategory = computed(() => {
  const groupsMap = new Map<number, { name: string; items: TopItem[]; spent: number }>()
  for (const it of top.value) {
    const key = it.category_id ?? 0
    const name = it.category_id
      ? (catName.value.get(it.category_id) ?? 'Категория удалена')
      : 'Не разобрано'
    const g = groupsMap.get(key) ?? { name, items: [], spent: 0 }
    g.items.push(it)
    g.spent += it.spent
    groupsMap.set(key, g)
  }
  return [...groupsMap.values()].sort((a, b) => b.spent - a.spent)
})

/** Свои слова, разложенные по категориям. */
const wordsByCategory = computed(() => {
  const m = new Map<number, WordRule[]>()
  for (const w of words.value) m.set(w.category_id, [...(m.get(w.category_id) ?? []), w])
  return m
})

const rulesByCategory = computed(() => {
  const m = new Map<number, ItemRule[]>()
  for (const r of rules.value) m.set(r.category_id, [...(m.get(r.category_id) ?? []), r])
  return m
})

async function addWord() {
  const f = wordForm.value
  if (!f || !f.pattern.trim()) return
  busy.value = true
  try {
    const res = await createWordRule(f.pattern.trim(), f.categoryId)
    wordForm.value = null
    showToast(res.reclassified ? `Пересобрано чеков: ${res.reclassified}` : 'Сохранено')
    await load()
    await loadRules()
    emit('changed')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

async function removeWord(wr: WordRule) {
  try {
    await deleteWordRule(wr.id)
    await load()
    emit('changed')
  } catch {
    showToast('Не удалось удалить')
  }
}

/** Своя группа — это просто категория: отдельной сущности «группа» нет. */
async function addCategory() {
  const f = catForm.value
  if (!f || !f.name.trim()) return
  busy.value = true
  try {
    await createCategory({ name: f.name.trim(), icon: f.icon.trim(), kind: 'expense' })
    catForm.value = null
    emit('changed')
    showToast('Категория создана ✅ Добавьте к ней слова')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось создать')
  } finally {
    busy.value = false
  }
}

function statFor(catId: number): CategoryItemStats | undefined {
  return catStats.value.find((s) => s.category_id === catId)
}

function priceDelta(it: TopItem): number | null {
  if (!it.first_price || it.first_price === it.last_price) return null
  return Math.round(((it.last_price - it.first_price) / it.first_price) * 100)
}

async function forgetRule(r: ItemRule) {
  if (!(await confirmAction(`Забыть, что «${r.name_sample}» — это ${catName.value.get(r.category_id)}?`))) return
  try {
    await deleteItemRule(r.id)
    await loadRules()
  } catch {
    showToast('Не удалось удалить')
  }
}

/** Высота столбика истории цен относительно самой дорогой точки. */
function barHeight(p: { price: number }): string {
  const max = Math.max(...(history.value?.points ?? []).map((x) => x.price), 1)
  return `${Math.max(4, (p.price / max) * 100)}%`
}
</script>

<template>
  <div>
    <div class="sub-tabs">
      <button :class="{ on: view === 'todo' }" @click="switchView('todo')">
        Разметка<span v-if="unclassified.length"> ({{ unclassified.length }})</span>
      </button>
      <button :class="{ on: view === 'top' }" @click="switchView('top')">Товары и цены</button>
      <button :class="{ on: view === 'setup' }" @click="switchView('setup')">Группы</button>
    </div>

    <!-- РАЗМЕТКА -->
    <template v-if="view === 'todo'">
      <div v-if="!groupsReady" class="empty">
        <p class="hint">
          Группы товаров ещё не заведены. Создам категории «Продукты», «Алкоголь»,
          «Бытовая химия», «Техника», «Сервисы» и «Доставка и сервис» — если
          такие уже есть в дереве, переиспользую их.
        </p>
        <button class="btn primary" :disabled="busy" @click="seed(false)">Создать группы</button>
      </div>

      <template v-else>
        <p class="hint">
          Здесь товары, которые встроенный словарь не опознал. Размеченное
          запоминается и применяется ко всем чекам — включая прошлые.
        </p>

        <div v-if="unclassified.length" class="head">
          <button class="btn primary grow" :disabled="busy" @click="assignAll">
            Применить выбранное
          </button>
          <button class="btn" :disabled="suggestState === 'running'" @click="askAI">
            ✨ Спросить агента
          </button>
        </div>
        <p v-if="suggestNote" class="hint small" :class="{ warn: suggestState === 'failed' }">
          {{ suggestNote }}
        </p>

        <div v-for="it in unclassified" :key="it.name_key" class="row">
          <div class="row-main">
            <span class="name">{{ it.name }}</span>
            <span class="meta">
              {{ money(it.spent) }} · брали {{ it.times }} раз(а)
              <span v-if="suggestions[it.name_key]" class="tag">
                ✨ {{ suggestions[it.name_key].group_title }}
              </span>
            </span>
          </div>
          <div class="row-right">
            <CategoryPicker v-model="picks[it.name_key]" :categories="refs?.categories ?? []"
                            kind="expense" empty-label="выберите группу" />
            <button class="mini primary" :disabled="busy" @click="assignOne(it)">✓</button>
          </div>
        </div>

        <p v-if="!unclassified.length" class="hint">
          Всё разобрано — неопознанных товаров нет.
        </p>
      </template>
    </template>

    <!-- ТОВАРЫ И ЦЕНЫ -->
    <template v-else-if="view === 'top'">
      <p class="hint">
        На что уходят деньги по товарам за год и как менялась цена за единицу.
      </p>
      <template v-for="g in topByCategory" :key="g.name">
        <div class="gsect">
          <span>{{ g.name }}</span>
          <span class="amount">{{ money(g.spent) }}</span>
        </div>
        <div v-for="it in g.items" :key="it.name_key" class="row" @click="openHistory(it)">
          <div class="row-main">
            <span class="name">{{ it.name }}</span>
            <span class="meta">
              брали {{ it.times }} раз(а)
              <span v-if="it.last_at"> · {{ fmtDate(it.last_at) }}</span>
            </span>
          </div>
          <div class="row-right">
            <span class="amount">{{ money(it.spent) }}</span>
            <span v-if="priceDelta(it) !== null" class="meta"
                  :class="{ up: priceDelta(it)! > 0, down: priceDelta(it)! < 0 }">
              {{ priceDelta(it)! > 0 ? '▲' : '▼' }}{{ Math.abs(priceDelta(it)!) }}%
            </span>
          </div>
        </div>
      </template>
      <p v-if="!top.length" class="hint">
        Товаров пока нет — они появляются из разобранных чеков.
      </p>
    </template>

    <!-- ГРУППЫ -->
    <template v-else>
      <p class="hint">
        Группа — это то, что понимает встроенный словарь («пиво» → алкоголь).
        Категория — ваша, из дерева Finance. Привязка говорит, куда класть
        опознанное: сама статистика считается по категориям, групп в ней нет.
        Поэтому если две группы смотрят в одну категорию, в отчёте они сольются.
      </p>

      <div v-if="clashing.length" class="warnbox">
        ⚠️ В одну категорию смотрят несколько групп:
        <b v-for="(names, i) in clashing" :key="i">{{ names.join(', ') }}</b>.
        В отчёте и на диаграмме они сольются в один кусок.
        <button class="btn primary wide" :disabled="busy" @click="seed(true)">
          Создать типовые категории и разнести группы
        </button>
      </div>

      <button v-if="!groupsReady" class="btn primary wide" :disabled="busy" @click="seed(false)">
        Создать группы
      </button>
      <div v-for="g in groups" :key="g.code" class="gcard">
        <div class="row">
          <div class="row-main">
            <span class="name">{{ g.icon }} {{ g.title }}</span>
            <span class="meta">
              слов в словаре: {{ (dictionary[g.code] ?? []).length }}
              <template v-if="groupMap[g.code]">
                · своих слов: {{ statFor(groupMap[g.code])?.words ?? 0 }}
                · запомнено товаров: {{ statFor(groupMap[g.code])?.items ?? 0 }}
              </template>
            </span>
          </div>
          <div class="row-right">
            <CategoryPicker :model-value="groupMap[g.code] ?? null"
                            :categories="refs?.categories ?? []" kind="expense"
                            empty-label="не задана"
                            @update:model-value="(v) => v && remapGroup(g.code, v)" />
          </div>
        </div>

        <button class="link" @click="openWords[g.code] = !openWords[g.code]">
          {{ openWords[g.code] ? '▾' : '▸' }} правила
        </button>

        <div v-if="openWords[g.code]" class="rules">
          <p class="hint small">
            Встроенный словарь (изменить нельзя, но можно перекрыть своим словом):
          </p>
          <div class="words">
            <span v-for="wd in dictionary[g.code] ?? []" :key="wd" class="word">{{ wd }}</span>
          </div>

          <template v-if="groupMap[g.code]">
            <p class="hint small">Свои слова — проверяются раньше встроенных:</p>
            <div class="words">
              <span v-for="wr in wordsByCategory.get(groupMap[g.code]) ?? []" :key="wr.id"
                    class="word own">
                {{ wr.pattern }}
                <button class="x" @click="removeWord(wr)">✕</button>
              </span>
              <button class="word add"
                      @click="wordForm = { categoryId: groupMap[g.code], pattern: '' }">
                ＋ слово
              </button>
            </div>

            <p v-if="(rulesByCategory.get(groupMap[g.code]) ?? []).length" class="hint small">
              Запомненные товары:
            </p>
            <div v-for="r in rulesByCategory.get(groupMap[g.code]) ?? []" :key="r.id"
                 class="hist-row">
              <span>{{ r.name_sample || r.name_key }}</span>
              <span class="meta">{{ r.source === 'ai' ? '✨' : '✎' }}</span>
              <button class="mini danger" @click="forgetRule(r)">✕</button>
            </div>
          </template>
        </div>
      </div>

      <h3 class="sect">Своя группа</h3>
      <p class="hint small">
        Отдельной сущности «группа» нет: группа — это категория, в которую
        складывают товары. Создайте категорию и добавьте к ней свои слова —
        дальше она будет наполняться сама.
      </p>
      <button class="btn wide" @click="catForm = { name: '', icon: '' }">
        ＋ Категория для товаров
      </button>

      <template v-if="rules.filter((r) => !Object.values(groupMap).includes(r.category_id)).length">
        <h3 class="sect">Запомненные товары вне групп</h3>
        <div v-for="r in rules.filter((r) => !Object.values(groupMap).includes(r.category_id))"
             :key="r.id" class="row">
          <div class="row-main">
            <span class="name">{{ r.name_sample || r.name_key }}</span>
            <span class="meta">
              {{ catName.get(r.category_id) }}
              <span v-if="r.source === 'ai'" class="tag">✨ от агента</span>
            </span>
          </div>
          <button class="mini danger" @click="forgetRule(r)">✕</button>
        </div>
      </template>
    </template>

    <!-- ИСТОРИЯ ЦЕН, СВОЁ СЛОВО, СВОЯ КАТЕГОРИЯ -->
    <Teleport to="body">
      <div v-if="wordForm" class="modal" @click.self="wordForm = null">
        <div class="modal-box">
          <h3>Своё слово</h3>
          <p class="hint">
            Товар попадёт в эту категорию, если его название содержит слово.
            Свои слова проверяются раньше встроенных, поэтому ими же можно
            перекрыть словарь. Регистр не важен.
          </p>
          <label>Слово или часть названия</label>
          <input v-model="wordForm.pattern" placeholder="корм, кофе, cat food" />
          <div class="modal-acts">
            <button class="btn" @click="wordForm = null">Отмена</button>
            <button class="btn primary" :disabled="busy" @click="addWord">Добавить</button>
          </div>
        </div>
      </div>

      <div v-if="catForm" class="modal" @click.self="catForm = null">
        <div class="modal-box">
          <h3>Категория для товаров</h3>
          <label>Название</label>
          <input v-model="catForm.name" placeholder="Сладости, Корм коту" />
          <label>Значок</label>
          <input v-model="catForm.icon" placeholder="🍬" maxlength="4" />
          <p class="hint">
            После создания добавьте к ней свои слова — иначе товары будут
            попадать туда только ручной разметкой.
          </p>
          <div class="modal-acts">
            <button class="btn" @click="catForm = null">Отмена</button>
            <button class="btn primary" :disabled="busy" @click="addCategory">Создать</button>
          </div>
        </div>
      </div>
      <div v-if="history" class="modal" @click.self="history = null">
        <div class="modal-box">
          <h3>{{ history.name }}</h3>
          <p class="meta">
            Брали {{ history.times }} раз(а), потрачено {{ money(history.spent) }}
          </p>
          <div v-if="history.points.length > 1" class="chart">
            <div v-for="(p, i) in history.points" :key="i" class="bar-col">
              <span class="bar-val">{{ hide ? '•' : Math.round(p.price) }}</span>
              <div class="bar"><i :style="{ height: barHeight(p) }" /></div>
              <span class="bar-lbl">{{ fmtDate(p.date).slice(0, 5) }}</span>
            </div>
          </div>
          <div v-for="(p, i) in history.points" :key="`r${i}`" class="hist-row">
            <span>{{ fmtDate(p.date) }}</span>
            <span class="meta">×{{ p.qty }}{{ p.unit ? ' ' + p.unit : '' }}</span>
            <span>{{ money(p.price) }} / ед.</span>
            <span>{{ money(p.total) }}</span>
          </div>
          <p v-if="history.times > 1 && history.first !== history.last" class="hint">
            Цена за единицу изменилась с {{ money(history.first) }} до
            {{ money(history.last) }}.
          </p>
          <div class="modal-acts">
            <button class="btn" @click="history = null">Закрыть</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.sub-tabs {
  display: flex;
  gap: 6px;
  margin-bottom: 10px;
}

.sub-tabs button {
  flex: 1;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 7px;
  cursor: pointer;
}

.sub-tabs button.on {
  background: var(--accent-color);
  color: #fff;
}

.head {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
}

.grow {
  flex: 1;
}

.empty {
  text-align: center;
  padding: 10px 0;
}

.row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  background: var(--card-color);
  border-radius: 10px;
  padding: 9px 12px;
  margin-bottom: 6px;
  backdrop-filter: var(--card-blur);
}

.row-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex: 1;
}

.name {
  font-size: 14px;
  overflow-wrap: anywhere;
}

.meta {
  font-size: 11px;
  color: var(--text-secondary);
}

.tag {
  margin-left: 6px;
  color: var(--accent-color);
}

.up {
  color: #ef4444;
}

.down {
  color: #22c55e;
}

.row-right {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-direction: column;
}

.row-right :deep(select) {
  background: var(--bg-color);
  border: none;
  border-radius: 6px;
  color: var(--text-color);
  font-size: 12px;
  padding: 6px 8px;
  max-width: 160px;
}

.amount {
  font-size: 14px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.sect {
  font-size: 14px;
  margin: 18px 0 6px;
  color: var(--text-secondary);
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 10px 0;
}

.hint.small {
  font-size: 11px;
  margin: 4px 0 8px;
}

.hint.warn {
  color: #ef4444;
}

.warnbox {
  background: rgba(245, 158, 11, 0.15);
  border-radius: 8px;
  padding: 10px;
  font-size: 12px;
  margin: 8px 0;
}

.warnbox b {
  display: block;
  margin: 4px 0;
}

.warnbox .btn {
  margin-top: 8px;
}

.mini {
  background: var(--bg-color);
  border: none;
  border-radius: 6px;
  color: var(--text-color);
  font-size: 12px;
  padding: 6px 9px;
  cursor: pointer;
}

.mini.primary {
  background: var(--accent-color);
  color: #fff;
}

.mini.danger {
  color: #ef4444;
}

.btn {
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 14px;
  padding: 10px 14px;
  cursor: pointer;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn.wide {
  width: 100%;
  margin-bottom: 8px;
}

.gcard {
  background: var(--card-color);
  border-radius: 10px;
  margin-bottom: 6px;
  padding-bottom: 4px;
  backdrop-filter: var(--card-blur);
}

.gcard .row {
  background: none;
  margin: 0;
  backdrop-filter: none;
}

.gcard .link {
  padding: 0 12px 6px;
  font-size: 12px;
}

.rules {
  padding: 0 12px 8px;
}

.words {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 6px;
}

.word {
  background: var(--bg-color);
  border: none;
  border-radius: 5px;
  color: var(--text-secondary);
  font-size: 11px;
  padding: 3px 6px;
}

.word.own {
  color: var(--text-color);
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.word.add {
  color: var(--accent-color);
  cursor: pointer;
}

.word .x {
  background: none;
  border: none;
  color: #ef4444;
  cursor: pointer;
  padding: 0;
  font-size: 10px;
}

.gsect {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 12px 2px 5px;
}

.modal {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 20px 12px;
  overflow-y: auto;
  z-index: 1300;
}

.modal-box {
  background: var(--bg-color);
  border-radius: 12px;
  padding: 14px;
  width: 100%;
  max-width: 460px;
}

.modal-box h3 {
  margin: 0 0 6px;
  font-size: 16px;
  overflow-wrap: anywhere;
}

.chart {
  display: flex;
  align-items: flex-end;
  gap: 4px;
  height: 110px;
  margin: 12px 0;
}

.bar-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  height: 100%;
}

.bar-val,
.bar-lbl {
  font-size: 10px;
  color: var(--text-secondary);
}

.bar {
  flex: 1;
  width: 100%;
  display: flex;
  align-items: flex-end;
  background: var(--card-color);
  border-radius: 4px 4px 0 0;
  overflow: hidden;
}

.bar i {
  display: block;
  width: 100%;
  background: var(--accent-color);
}

.hist-row {
  display: grid;
  grid-template-columns: auto auto 1fr auto;
  gap: 8px;
  font-size: 12px;
  padding: 4px 0;
  border-top: 1px solid var(--card-color);
}

.modal-acts {
  display: flex;
  gap: 8px;
  margin-top: 14px;
}

.modal-acts .btn {
  flex: 1;
}
</style>
