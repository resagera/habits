<script setup lang="ts">
import { ref } from 'vue'
import { showToast } from '../../../shared/toast'
import { itemComponents, type ChartType, type ComponentDef, type ItemConfig, type MetricItem } from '../types'

const props = defineProps<{
  chartTypes: ChartType[]
  item: MetricItem | null // null = создание нового
}>()

const emit = defineEmits<{
  save: [name: string, chartType: string, config: ItemConfig]
  remove: []
  close: []
}>()

const name = ref(props.item?.name ?? '')
const chartType = ref(props.item?.chart_type ?? 'line')
const components = ref<ComponentDef[]>(
  props.item ? itemComponents(props.item.config).map((c) => ({ ...c })) : [{ key: '', label: '', color: '#60a5fa' }],
)
const nodeColor = ref(props.item?.config.line?.nodeColor ?? '')
const lineFill = ref(props.item?.config.line?.fill ?? false)
const barWidth = ref(props.item?.config.bars?.width ?? 14)
const barGap = ref(props.item?.config.bars?.gap ?? 6)
const tubeColor = ref(props.item?.config.tubes?.tubeColor ?? '')
const bgEnabled = ref(!!props.item?.config.background?.from)
const bgFrom = ref(props.item?.config.background?.from ?? '#1e293b')
const bgTo = ref(props.item?.config.background?.to ?? '')
const confirmDelete = ref(false)

let keyCounter = components.value.length

function addComponent() {
  components.value.push({ key: `c${++keyCounter}${Date.now() % 1000}`, label: '', color: '#7dd3fc' })
}

function removeComponent(i: number) {
  if (components.value.length <= 1) return
  components.value.splice(i, 1)
}

function save() {
  if (!name.value.trim()) {
    showToast('Введите название')
    return
  }
  const config: ItemConfig = {
    components: components.value,
    line: { ...(nodeColor.value ? { nodeColor: nodeColor.value } : {}), fill: lineFill.value },
    bars: { width: Number(barWidth.value) || 14, gap: Number(barGap.value) || 6 },
    ...(tubeColor.value ? { tubes: { tubeColor: tubeColor.value } } : {}),
    ...(bgEnabled.value ? { background: { from: bgFrom.value, ...(bgTo.value ? { to: bgTo.value } : {}) } } : {}),
  }
  emit('save', name.value.trim(), chartType.value, config)
}

function onRemove() {
  if (!confirmDelete.value) {
    confirmDelete.value = true
    setTimeout(() => (confirmDelete.value = false), 3500)
    return
  }
  emit('remove')
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content settings">
      <h3>{{ item ? 'Настройки графика' : 'Новый элемент' }}</h3>

      <input v-model="name" placeholder="Название (например, Вес)" maxlength="200" />

      <label class="field-label">Тип графика</label>
      <select v-model="chartType">
        <option v-for="t in chartTypes" :key="t.code" :value="t.code">{{ t.name }}</option>
      </select>

      <label class="field-label">
        Компоненты (серии)
        <button class="mini-btn" @click="addComponent">＋</button>
      </label>
      <div v-for="(c, i) in components" :key="c.key" class="comp-row">
        <input v-model="c.color" type="color" class="color-mini" />
        <input v-model="c.label" placeholder="Название серии" class="comp-label" />
        <button class="mini-btn" :disabled="components.length <= 1" @click="removeComponent(i)">✕</button>
      </div>
      <p v-if="chartType === 'tubes'" class="hint-line">
        Для диапазона в трубке добавьте компоненты с названиями min и max — или
        одиночные значения дадут короткий сегмент.
      </p>
      <p v-if="chartType === 'bars' && components.length > 1" class="hint-line">
        Первый компонент — весь столбец, остальные — сегменты снизу вверх.
      </p>

      <!-- Настройки линейного -->
      <template v-if="chartType === 'line'">
        <label class="inline-field">
          <span>Цвет узлов (пусто — как линия)</span>
          <span class="color-wrap">
            <input v-model="nodeColor" type="color" class="color-mini" />
            <button v-if="nodeColor" class="mini-btn" @click="nodeColor = ''">✕</button>
          </span>
        </label>
        <label class="check-line">
          <input v-model="lineFill" type="checkbox" />
          <span>Градиент под линией</span>
        </label>
      </template>

      <!-- Настройки столбиков -->
      <template v-if="chartType === 'bars'">
        <label class="inline-field">
          <span>Ширина столбика</span>
          <input v-model.number="barWidth" type="number" min="2" max="60" class="num" />
        </label>
        <label class="inline-field">
          <span>Расстояние между</span>
          <input v-model.number="barGap" type="number" min="0" max="60" class="num" />
        </label>
      </template>

      <!-- Настройки трубок -->
      <template v-if="chartType === 'tubes'">
        <label class="inline-field">
          <span>Цвет трубки (фон)</span>
          <span class="color-wrap">
            <input v-model="tubeColor" type="color" class="color-mini" />
            <button v-if="tubeColor" class="mini-btn" @click="tubeColor = ''">✕</button>
          </span>
        </label>
      </template>

      <!-- Фон графика -->
      <label class="check-line">
        <input v-model="bgEnabled" type="checkbox" />
        <span>Фон графика (иначе прозрачный)</span>
      </label>
      <template v-if="bgEnabled">
        <label class="inline-field">
          <span>Цвет</span>
          <input v-model="bgFrom" type="color" class="color-mini" />
        </label>
        <label class="inline-field">
          <span>Градиент к (пусто — один цвет)</span>
          <span class="color-wrap">
            <input v-model="bgTo" type="color" class="color-mini" />
            <button v-if="bgTo" class="mini-btn" @click="bgTo = ''">✕</button>
          </span>
        </label>
      </template>

      <button class="btn primary" @click="save">💾 Сохранить</button>
      <button v-if="item" class="btn danger" @click="onRemove">
        {{ confirmDelete ? 'Точно удалить элемент со всеми значениями?' : '🗑 Удалить элемент' }}
      </button>
      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.settings {
  text-align: left;
  max-height: 85vh;
  overflow-y: auto;
}

.settings h3 {
  text-align: center;
}

.settings input,
.settings select {
  width: 100%;
  margin-top: 8px;
}

.field-label {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
  color: var(--text-secondary);
  margin-top: 12px;
}

.comp-row {
  display: flex;
  gap: 6px;
  align-items: center;
}

.comp-row .comp-label {
  flex: 1;
  min-width: 0;
}

.color-mini {
  width: 42px !important;
  height: 34px;
  padding: 2px;
  flex: none;
}

.mini-btn {
  flex: none;
  background: var(--bg-secondary);
  border: none;
  border-radius: 6px;
  padding: 4px 9px;
  color: var(--text-color);
}

.hint-line {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 6px 0 0;
}

.inline-field {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  font-size: 13px;
}

.inline-field .num {
  width: 72px !important;
  margin: 0;
}

.inline-field .color-mini {
  margin: 0;
}

.color-wrap {
  display: flex;
  align-items: center;
  gap: 4px;
}

.check-line {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 10px;
  cursor: pointer;
  font-size: 13px;
}

/* чекбокс не должен наследовать width:100% от .settings input */
.check-line input[type='checkbox'] {
  width: 18px !important;
  height: 18px;
  flex: none;
  margin: 0;
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
