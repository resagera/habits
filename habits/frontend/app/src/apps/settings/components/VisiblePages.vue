<script setup lang="ts">
/**
 * «Какие страницы мне видны» — личная видимость страниц.
 *
 * Это НЕ доступ: доступы выдаёт админ, и здесь показаны только те страницы,
 * которые пользователю и так доступны. Снятая галочка убирает страницу из
 * меню и с плиток главной — по прямой ссылке и в поиске она продолжает
 * работать. Смысл — убрать с глаз то, чем не пользуешься, а не запретить.
 */
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { pageAllowed } from '../../../shared/access'
import { isAdmin } from '../../../shared/me'
import { isPageHidden, setHiddenPages, setPageHidden } from '../../../shared/pageOrder'

const router = useRouter()

// «Настроек» в списке нет: спрятав их, пользователь потерял бы путь обратно.
const pages = computed(() =>
  [
    ...router.getRoutes().filter((r) => r.meta.app && pageAllowed(String(r.name))),
    ...(isAdmin.value ? router.getRoutes().filter((r) => r.meta.admin) : []),
  ].filter((r) => r.name !== 'settings'),
)

const hiddenCount = computed(() => pages.value.filter((r) => isPageHidden(String(r.name))).length)

function onToggle(code: string, e: Event) {
  // галочка = «видна», поэтому скрываем, когда её сняли
  void setPageHidden(code, !(e.target as HTMLInputElement).checked)
}

function showAll() {
  void setHiddenPages([])
}
</script>

<template>
  <section class="section">
    <h3>Какие страницы мне видны</h3>
    <p class="hint-text">
      Снятая галочка убирает страницу из меню и с плиток главной — только у вас.
      Данные остаются на месте: по прямой ссылке и из результатов поиска
      страница откроется как обычно.
    </p>

    <label v-for="p in pages" :key="p.path" class="row">
      <input
        type="checkbox"
        :checked="!isPageHidden(String(p.name))"
        @change="onToggle(String(p.name), $event)"
      />
      <span class="icon">{{ p.meta.icon }}</span>
      <span class="name">{{ p.meta.title }}</span>
    </label>

    <button v-if="hiddenCount > 0" class="btn-link" @click="showAll">
      Показать все ({{ hiddenCount }} скрыто)
    </button>
  </section>
</template>

<style scoped>
.section {
  background: var(--card-color);
  border-radius: 8px;
  padding: 12px 14px;
  margin-bottom: 14px;
}

.section h3 {
  margin: 0 0 10px;
  font-size: 16px;
}

.hint-text {
  margin: 0 0 10px;
  font-size: 12px;
  color: var(--text-secondary);
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 0;
  font-size: 15px;
  cursor: pointer;
}

.icon {
  width: 22px;
  text-align: center;
}

.name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.btn-link {
  margin-top: 8px;
  padding: 0;
  border: none;
  background: none;
  color: var(--accent-color);
  font-size: 13px;
  cursor: pointer;
}
</style>
