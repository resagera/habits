<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import RecipientPicker from '../../../components/RecipientPicker.vue'
import { showToast } from '../../../shared/toast'
import * as trackerApi from '../api'
import { errorText } from '../composables/useTracker'
import { colorCss, colorValue } from '../display'
import type {
  Category,
  CategoryColor,
  CategoryPatch,
  ShareUser,
  TrackerKind,
  TrackerStyle,
} from '../types'

const props = defineProps<{ category: Category }>()

const emit = defineEmits<{
  save: [patch: CategoryPatch]
  remove: []
  leave: []
  openHistory: []
  close: []
}>()

const name = ref(props.category.name)
const color = ref(props.category.color)
const daily = ref(props.category.daily)
const kind = ref<TrackerKind>(props.category.kind)
const style = ref<TrackerStyle>(props.category.style)
const multi = ref(props.category.multi)
const emoji = ref(props.category.emoji)
// копия набора: правки применяются только по «Сохранить», как и всё остальное
const palette = ref<CategoryColor[]>(props.category.palette.map((c) => ({ ...c })))
const confirmDelete = ref(false)
const confirmLeave = ref(false)

const isEmojiStyle = computed(() => kind.value === 'marks' && style.value === 'emoji')
const showColor = computed(() => kind.value === 'counter' || style.value !== 'emoji')
// набор именованных цветов есть только у отметок цветом: у эмодзи свой набор,
// у счётчика цвет один на всё
const showPalette = computed(() => kind.value === 'marks' && style.value !== 'emoji')

function addColor() {
  palette.value.push({ color: color.value, name: `Цвет ${palette.value.length + 1}` })
  // набор без мультицвета бессмысленен: все отметки были бы одного цвета
  multi.value = true
}

function removeColor(i: number) {
  palette.value.splice(i, 1)
}

/** Галочка «градиент»: второй цвет пустой — значит сплошная заливка. */
function toggleGradient(i: number, on: boolean) {
  palette.value[i].color2 = on ? palette.value[i].color2 || color.value : ''
}

function paletteChanged(): boolean {
  const was = props.category.palette
  if (was.length !== palette.value.length) return true
  return palette.value.some(
    (c, i) => c.color !== was[i].color || (c.color2 ?? '') !== (was[i].color2 ?? '') || c.name !== was[i].name,
  )
}

// --- совместный доступ ---
const shareTo = ref('')
const members = ref<ShareUser[]>([])
const sharing = ref(false)

onMounted(async () => {
  if (!props.category.mine) return
  try {
    members.value = (await trackerApi.fetchShares(props.category.id)).users
  } catch {
    /* список просто не покажем */
  }
})

async function share() {
  const to = shareTo.value.trim()
  if (!to || sharing.value) return
  sharing.value = true
  try {
    const { shared_with } = await trackerApi.shareCategory(props.category.id, to)
    if (!members.value.some((u) => u.id === shared_with.id)) {
      members.value.push(shared_with)
    }
    shareTo.value = ''
    showToast(`Доступ открыт: ${shared_with.first_name || '@' + shared_with.username}`)
  } catch (e) {
    showToast(errorText(e))
  } finally {
    sharing.value = false
  }
}

async function revoke(u: ShareUser) {
  try {
    await trackerApi.revokeShare(props.category.id, u.id)
    members.value = members.value.filter((m) => m.id !== u.id)
  } catch (e) {
    showToast(errorText(e))
  }
}

function memberLabel(u: ShareUser): string {
  return u.first_name || (u.username ? '@' + u.username : `#${u.id}`)
}

function save() {
  const patch: CategoryPatch = {}
  if (name.value.trim() !== props.category.name) patch.name = name.value.trim()
  if (color.value !== props.category.color) patch.color = color.value
  if (daily.value !== props.category.daily) patch.daily = daily.value
  if (kind.value !== props.category.kind) patch.kind = kind.value
  if (style.value !== props.category.style) patch.style = style.value
  if (multi.value !== props.category.multi) patch.multi = multi.value
  if (emoji.value.trim() && emoji.value.trim() !== props.category.emoji)
    patch.emoji = emoji.value.trim()
  if (paletteChanged()) {
    // пустые названия сервер не примет — подставляем понятное умолчание
    patch.palette = palette.value.map((c, i) => ({
      color: c.color,
      color2: c.color2 || undefined,
      name: c.name.trim() || `Цвет ${i + 1}`,
    }))
  }
  if (Object.keys(patch).length === 0) {
    emit('close')
    return
  }
  emit('save', patch)
}
</script>

<template>
  <div class="modal" @click.self="emit('close')">
    <div class="modal-content">
      <h3>Настройки категории</h3>

      <template v-if="category.mine">
        <label class="field">
          <span>Название</span>
          <input v-model="name" type="text" maxlength="100" />
        </label>

        <label class="field">
          <span>Вид трекера</span>
          <select v-model="kind">
            <option value="marks">Отметки</option>
            <option value="counter">Счётчик (клик +1, долгое нажатие −1)</option>
          </select>
        </label>

        <label v-if="kind === 'marks'" class="field">
          <span>Стиль отметок</span>
          <select v-model="style">
            <option value="square">Квадратики</option>
            <option value="circle">Кружки</option>
            <option value="emoji">Эмодзи</option>
          </select>
        </label>

        <label v-if="isEmojiStyle" class="field">
          <span>Эмодзи по умолчанию</span>
          <input v-model="emoji" type="text" maxlength="8" />
        </label>

        <label v-if="showColor" class="field">
          <span>Цвет отметок</span>
          <input v-model="color" type="color" class="color-input" />
        </label>

        <div v-if="showPalette" class="palette-block">
          <div class="palette-title">Набор цветов</div>
          <p class="check-hint">
            Названия видны только здесь. Когда в наборе есть цвета, на карточке
            вместо одного кружка появляется весь набор, и клик по кружку выбирает
            цвет для отметок — палитра там уже не открывается.
          </p>
          <div v-for="(c, i) in palette" :key="i" class="pal-row">
            <span class="pal-preview" :style="{ background: colorCss(colorValue(c)) }"></span>
            <input v-model="c.color" type="color" class="pal-color" />
            <input
              v-if="c.color2"
              v-model="c.color2"
              type="color"
              class="pal-color"
              title="Второй цвет градиента"
            />
            <input v-model="c.name" type="text" maxlength="40" placeholder="Название" class="pal-name" />
            <label class="pal-grad" title="Градиент из двух цветов">
              <input
                type="checkbox"
                :checked="!!c.color2"
                @change="toggleGradient(i, ($event.target as HTMLInputElement).checked)"
              />
              <span>◐</span>
            </label>
            <button class="pal-del" title="Убрать цвет" @click="removeColor(i)">✕</button>
          </div>
          <button class="btn small" :disabled="palette.length >= 12" @click="addColor">
            ➕ Добавить цвет
          </button>
        </div>

        <label v-if="kind === 'marks'" class="check-line">
          <input v-model="multi" type="checkbox" :disabled="showPalette && palette.length > 0" />
          <span>{{ isEmojiStyle ? 'Мульти-эмодзи — в разные даты разные эмодзи' : 'Мультицвет — в разные даты разный цвет' }}</span>
        </label>
        <p v-if="showPalette && palette.length > 0" class="check-hint">
          С набором цветов мультицвет обязателен: иначе все отметки были бы
          одного цвета и выбирать было бы нечего.
        </p>
        <p v-else-if="kind === 'marks' && multi" class="check-hint">
          Рядом с 📅 появится иконка активного {{ isEmojiStyle ? 'эмодзи' : 'цвета' }}: клик по дню
          ставит отметку активным {{ isEmojiStyle ? 'эмодзи' : 'цветом' }}, клик по отметке другого
          {{ isEmojiStyle ? 'эмодзи' : 'цвета' }} — перекрашивает, того же — снимает.
        </p>

        <label class="check-line">
          <input v-model="daily" type="checkbox" />
          <span>Daily — ежедневная привычка</span>
        </label>
        <p class="check-hint">
          На странице Reminders для неё можно создать напоминание, которое придёт, только если день ещё не отмечен.
        </p>
      </template>

      <p v-else class="foreign-note">
        👥 Совместный трекер — настройки меняет владелец.
      </p>

      <button class="btn" @click="emit('openHistory')">🗓 Вся история</button>

      <template v-if="category.mine">
        <div class="share-block">
          <div class="share-title">Совместный доступ</div>
          <div v-if="members.length" class="members">
            <span v-for="u in members" :key="u.id" class="member-chip">
              {{ memberLabel(u) }}
              <button class="member-x" title="Отозвать доступ" @click="revoke(u)">✕</button>
            </span>
          </div>
          <RecipientPicker v-model="shareTo" />
          <button class="btn small" :disabled="!shareTo.trim() || sharing" @click="share">
            👥 Поделиться доступом
          </button>
          <p class="check-hint">
            Участники видят трекер у себя и могут ставить отметки вместе с вами.
          </p>
        </div>

        <button class="btn primary" @click="save">💾 Сохранить</button>

        <button v-if="!confirmDelete" class="btn danger" @click="confirmDelete = true">
          🗑 Удалить категорию
        </button>
        <button v-else class="btn danger" @click="emit('remove')">
          Точно удалить? Отметки будут потеряны
        </button>
      </template>

      <template v-else>
        <button v-if="!confirmLeave" class="btn danger" @click="confirmLeave = true">
          🚪 Покинуть трекер
        </button>
        <button v-else class="btn danger" @click="emit('leave')">
          Точно покинуть? Трекер исчезнет из вашего списка
        </button>
      </template>

      <button class="btn" @click="emit('close')">Отмена</button>
    </div>
  </div>
</template>

<style scoped>
.field {
  display: block;
  text-align: left;
  margin-bottom: 12px;
}

.field span {
  display: block;
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.field input,
.field select {
  width: 100%;
}

.color-input {
  height: 42px;
  padding: 2px;
}

.palette-block {
  margin-top: 12px;
  text-align: left;
}

.palette-title {
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 4px;
}

.pal-row {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
}

.pal-preview {
  width: 22px;
  height: 22px;
  flex: none;
  border-radius: 50%;
  box-shadow: 0 0 0 1px var(--text-secondary);
}

.pal-color {
  width: 38px;
  height: 34px;
  flex: none;
  padding: 2px;
}

.pal-name {
  flex: 1;
  min-width: 0;
}

.pal-grad {
  display: flex;
  align-items: center;
  gap: 2px;
  flex: none;
  font-size: 13px;
  color: var(--text-secondary);
}

.pal-grad input {
  width: 16px !important;
  height: 16px;
  margin: 0;
}

.pal-del {
  flex: none;
  background: none;
  border: none;
  color: var(--text-secondary);
  padding: 4px 2px;
}

.check-line {
  display: flex;
  align-items: center;
  gap: 8px;
  text-align: left;
  font-size: 14px;
  cursor: pointer;
  margin-top: 10px;
}

.check-line input[type='checkbox'] {
  width: 18px !important;
  height: 18px;
  flex: none;
  margin: 0;
}

.check-hint {
  margin: 4px 0 0;
  font-size: 11px;
  color: var(--text-secondary);
  text-align: left;
}

.foreign-note {
  text-align: left;
  font-size: 13px;
  color: var(--text-secondary);
}

.share-block {
  margin-top: 14px;
  padding-top: 10px;
  border-top: 1px solid var(--bg-secondary);
  text-align: left;
}

.share-title {
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 6px;
}

.members {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.member-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: var(--bg-secondary);
  border-radius: 12px;
  padding: 4px 6px 4px 11px;
  font-size: 12px;
}

.member-x {
  background: none;
  border: none;
  padding: 0 4px;
  font-size: 11px;
  color: var(--text-secondary);
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

.btn.small {
  font-size: 13px;
  padding: 8px;
}

.btn:disabled {
  opacity: 0.5;
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
