<script setup lang="ts">
// Справочник категорий: дерево любой вложенности. Удаление узла НЕ трогает
// деньги — сервер поднимает подкатегории и записи к родителю.
import { computed, ref } from 'vue'
import { confirmAction } from '../../../shared/telegram'
import { showToast } from '../../../shared/toast'
import { createCategory, deleteCategory, seedCategories, updateCategory } from '../api'
import { flattenCategories, type FinanceCategory } from '../types'

const props = defineProps<{ categories: FinanceCategory[] }>()
const emit = defineEmits<{ close: []; changed: [] }>()

const busy = ref(false)
const items = computed(() => flattenCategories(props.categories))

const form = ref<{
  id: number | null
  parent_id: number | null
  name: string
  kind: 'expense' | 'income'
  icon: string
} | null>(null)

function open(cat?: FinanceCategory, parent?: FinanceCategory) {
  form.value = {
    id: cat?.id ?? null,
    parent_id: cat ? cat.parent_id : (parent?.id ?? null),
    name: cat?.name ?? '',
    kind: cat?.kind ?? parent?.kind ?? 'expense',
    icon: cat?.icon ?? '',
  }
}

async function save() {
  const f = form.value
  if (!f) return
  if (!f.name.trim()) {
    showToast('Впишите название')
    return
  }
  busy.value = true
  try {
    const body = { name: f.name.trim(), kind: f.kind, icon: f.icon.trim(), parent_id: f.parent_id }
    if (f.id) await updateCategory(f.id, body)
    else await createCategory(body)
    form.value = null
    emit('changed')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

async function remove(cat: FinanceCategory) {
  const kids = props.categories.filter((c) => c.parent_id === cat.id).length
  const tail = kids
    ? ` Подкатегории (${kids}) и записи перейдут на уровень выше.`
    : ' Записи этой категории перейдут на уровень выше.'
  if (!(await confirmAction(`Удалить «${cat.name}»?${tail}`))) return
  try {
    await deleteCategory(cat.id)
    emit('changed')
  } catch {
    showToast('Не удалось удалить')
  }
}

async function seed() {
  busy.value = true
  try {
    const res = await seedCategories()
    showToast(res.created ? `Добавлено категорий: ${res.created}` : 'Всё уже на месте')
    emit('changed')
  } catch {
    showToast('Не удалось создать')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <Teleport to="body">
    <div class="modal" @click.self="emit('close')">
      <div class="modal-box">
        <h3>Категории</h3>
        <p class="hint">
          Вложенность любая: «Еда → Продукты → Овощи». Отчёт складывает
          подкатегории в родителя.
        </p>

        <div v-if="!items.length" class="empty">
          <p class="hint">Категорий пока нет.</p>
          <button class="btn primary" :disabled="busy" @click="seed">
            Создать типовые
          </button>
        </div>

        <div v-for="{ cat, depth } in items" :key="cat.id" class="crow"
             :style="{ paddingLeft: 4 + depth * 16 + 'px' }">
          <span class="cname">
            <span v-if="cat.icon" class="ico">{{ cat.icon }}</span>{{ cat.name }}
            <span v-if="cat.kind === 'income'" class="tag">доход</span>
          </span>
          <span class="cacts">
            <button class="mini" title="Подкатегория" @click="open(undefined, cat)">＋</button>
            <button class="mini" title="Изменить" @click="open(cat)">✎</button>
            <button class="mini danger" title="Удалить" @click="remove(cat)">✕</button>
          </span>
        </div>

        <div class="modal-acts">
          <button class="btn" @click="emit('close')">Закрыть</button>
          <button class="btn primary" @click="open()">＋ Категория</button>
        </div>
      </div>
    </div>

    <div v-if="form" class="modal top" @click.self="form = null">
      <div class="modal-box">
        <h3>{{ form.id ? 'Категория' : 'Новая категория' }}</h3>
        <label>Название</label>
        <input v-model="form.name" placeholder="Продукты" />
        <div class="two">
          <div>
            <label>Значок</label>
            <input v-model="form.icon" placeholder="🍎" maxlength="4" />
          </div>
          <div>
            <label>Тип</label>
            <select v-model="form.kind">
              <option value="expense">расход</option>
              <option value="income">доход</option>
            </select>
          </div>
        </div>
        <label>Внутри категории</label>
        <select v-model.number="form.parent_id">
          <option :value="null">— верхний уровень —</option>
          <option v-for="{ cat, depth } in items.filter((x) => x.cat.id !== form!.id)"
                  :key="cat.id" :value="cat.id">
            {{ '　'.repeat(depth) }}{{ cat.name }}
          </option>
        </select>
        <div class="modal-acts">
          <button class="btn" @click="form = null">Отмена</button>
          <button class="btn primary" :disabled="busy" @click="save">Сохранить</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
/* модалка через Teleport: backdrop-filter карточек ломает position: fixed */
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

.modal.top {
  z-index: 1400;
}

.modal-box {
  background: var(--bg-color);
  border-radius: 12px;
  padding: 14px;
  width: 100%;
  max-width: 460px;
}

.modal-box h3 {
  margin: 0 0 10px;
  font-size: 16px;
}

.modal-box label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 8px 0 4px;
}

.modal-box input,
.modal-box select {
  width: 100%;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 15px;
  padding: 9px 10px;
}

.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 10px;
}

.empty {
  text-align: center;
  padding: 10px 0;
}

.crow {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 7px 4px;
  border-bottom: 1px solid var(--card-color);
}

.cname {
  font-size: 14px;
  overflow-wrap: anywhere;
}

.ico {
  margin-right: 6px;
}

.tag {
  font-size: 11px;
  color: var(--text-secondary);
  margin-left: 6px;
}

.cacts {
  display: flex;
  gap: 4px;
  white-space: nowrap;
}

.mini {
  background: var(--card-color);
  border: none;
  border-radius: 6px;
  color: var(--text-color);
  font-size: 12px;
  padding: 5px 8px;
  cursor: pointer;
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

.two {
  display: flex;
  gap: 8px;
}

.two > div {
  flex: 1;
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
