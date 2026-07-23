<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import { fetchLimits, updateLimits, USER_TYPE_LABELS, type TypeLimits } from '../adminApi'

// Лимиты страницы Projects по типам пользователей (тип назначается в карточке
// пользователя). Блоки — на проект, картинки/файлы — на пользователя.
const limits = ref<TypeLimits[]>([])
const loading = ref(true)
const open = ref(false)
const savingType = ref('')

onMounted(async () => {
  try {
    limits.value = (await fetchLimits()).limits
  } catch {
    /* секция просто останется пустой */
  } finally {
    loading.value = false
  }
})

async function save(l: TypeLimits) {
  savingType.value = l.type
  try {
    await updateLimits(l)
    showToast(`Лимиты «${USER_TYPE_LABELS[l.type]}» сохранены ✅`)
  } catch {
    showToast('Не удалось сохранить лимиты')
  } finally {
    savingType.value = ''
  }
}

const FIELDS: { key: keyof Omit<TypeLimits, 'type'>; label: string }[] = [
  { key: 'max_blocks', label: 'Блоков в проекте' },
  { key: 'max_images', label: 'Картинок (всего)' },
  { key: 'max_files', label: 'Файлов (всего)' },
  { key: 'max_image_mb', label: 'Картинка, МБ' },
  { key: 'max_file_mb', label: 'Файл, МБ' },
]
</script>

<template>
  <section class="section">
    <button class="head-btn" @click="open = !open">
      <h3>📐 Лимиты Projects по типам {{ open ? '▴' : '▾' }}</h3>
    </button>

    <template v-if="open">
      <p v-if="loading" class="hint">Загрузка…</p>
      <div v-for="l in limits" v-else :key="l.type" class="type-card">
        <div class="type-name">{{ USER_TYPE_LABELS[l.type] }} <code>{{ l.type }}</code></div>
        <div class="grid">
          <label v-for="f in FIELDS" :key="f.key" class="field">
            <span>{{ f.label }}</span>
            <input v-model.number="l[f.key]" type="number" min="0" />
          </label>
        </div>
        <button class="btn" :disabled="savingType === l.type" @click="save(l)">
          {{ savingType === l.type ? '…' : '💾 Сохранить' }}
        </button>
      </div>
      <p class="hint">
        Блоки — лимит на один проект (считается по владельцу проекта); картинки и файлы — суммарно
        на пользователя; размеры — на один файл. Тип пользователя меняется в его карточке выше.
      </p>
    </template>
  </section>
</template>

<style scoped>
.section {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 14px;
}

.head-btn {
  width: 100%;
  background: none;
  border: none;
  color: var(--text-color);
  text-align: left;
  padding: 0;
}

.head-btn h3 {
  margin: 0;
  font-size: 16px;
}

.type-card {
  border-top: 1px solid var(--bg-secondary);
  margin-top: 10px;
  padding-top: 8px;
}

.type-name {
  font-size: 13px;
  font-weight: 700;
  margin-bottom: 6px;
}

.type-name code {
  font-weight: 400;
  color: var(--text-secondary);
  font-size: 11px;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 6px;
}

.field span {
  display: block;
  font-size: 10px;
  color: var(--text-secondary);
  margin-bottom: 2px;
}

.field input {
  width: 100%;
}

.btn {
  margin-top: 8px;
  padding: 7px 12px;
  border: none;
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-color);
  font-size: 13px;
}

.btn:disabled {
  opacity: 0.5;
}

.hint {
  font-size: 11px;
  color: var(--text-secondary);
  margin: 8px 0 0;
}
</style>
