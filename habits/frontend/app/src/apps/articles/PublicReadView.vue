<script setup lang="ts">
// Публичная страница чтения статьи по read-токену: открывается в обычном
// браузере без авторизации (эндпоинт /articles/public/{token} вне auth).
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../../shared/api/client'
import MarkdownView from './components/MarkdownView.vue'

const route = useRoute()
const title = ref('')
const content = ref('')
const state = ref<'loading' | 'ok' | 'notfound'>('loading')

onMounted(async () => {
  try {
    const data = await api.get<{ title: string; content: string }>(
      `/articles/public/${route.params.token}`,
    )
    title.value = data.title
    content.value = data.content
    document.title = data.title
    state.value = 'ok'
  } catch {
    state.value = 'notfound'
  }
})
</script>

<template>
  <div class="pub">
    <p v-if="state === 'loading'" class="hint">Загрузка…</p>
    <p v-else-if="state === 'notfound'" class="hint">Статья не найдена или ссылка отозвана</p>
    <template v-else>
      <h1 class="pub-title">{{ title }}</h1>
      <MarkdownView :source="content" />
    </template>
  </div>
</template>

<style scoped>
.hint {
  text-align: center;
  color: var(--text-secondary);
  padding: 24px 0;
}

.pub {
  padding-bottom: 40px;
}

.pub-title {
  font-size: 24px;
  margin: 4px 0 14px;
}
</style>
