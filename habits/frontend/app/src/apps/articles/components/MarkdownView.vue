<script setup lang="ts">
import { computed } from 'vue'
import DOMPurify from 'dompurify'
import { marked } from 'marked'
import { openExternalLink } from '../../../shared/telegram'

const props = defineProps<{ source: string }>()

marked.setOptions({ gfm: true, breaks: true })

const html = computed(() =>
  DOMPurify.sanitize(marked.parse(props.source, { async: false }), {
    FORBID_TAGS: ['style', 'form'], // input оставляем: GFM-чекбоксы задач
  }),
)

// ссылки из статьи открываем через Telegram (иначе webview уводит из приложения)
function onClick(e: MouseEvent) {
  const a = (e.target as HTMLElement).closest('a')
  if (a?.href) {
    e.preventDefault()
    openExternalLink(a.href)
  }
}
</script>

<template>
  <!-- eslint-disable-next-line vue/no-v-html — контент прогнан через DOMPurify -->
  <div class="md" @click="onClick" v-html="html"></div>
</template>

<style>
/* стили не scoped: v-html-контент не получает scope-атрибуты */
.md {
  font-size: 15px;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

.md h1,
.md h2 {
  border-bottom: 1px solid var(--hover-bg-color);
  padding-bottom: 6px;
  margin: 18px 0 10px;
}

.md h1 {
  font-size: 22px;
}

.md h2 {
  font-size: 19px;
}

.md h3 {
  font-size: 16px;
  margin: 14px 0 8px;
}

.md p {
  margin: 8px 0;
}

.md a {
  color: var(--accent-color);
}

.md code {
  background: var(--bg-secondary);
  border-radius: 4px;
  padding: 1px 5px;
  font-size: 13px;
  font-family: monospace;
}

.md pre {
  background: var(--bg-secondary);
  border-radius: 8px;
  padding: 10px 12px;
  overflow-x: auto;
  margin: 10px 0;
}

.md pre code {
  background: none;
  padding: 0;
}

.md blockquote {
  border-left: 3px solid var(--accent-color);
  margin: 10px 0;
  padding: 2px 12px;
  color: var(--text-secondary);
}

.md ul,
.md ol {
  padding-left: 24px;
  margin: 8px 0;
}

.md li {
  margin: 3px 0;
}

.md table {
  border-collapse: collapse;
  margin: 10px 0;
  display: block;
  overflow-x: auto;
  max-width: 100%;
}

.md th,
.md td {
  border: 1px solid var(--hover-bg-color);
  padding: 5px 10px;
  font-size: 13px;
}

.md th {
  background: var(--bg-secondary);
}

.md img {
  max-width: 100%;
  border-radius: 8px;
}

.md hr {
  border: none;
  border-top: 1px solid var(--hover-bg-color);
  margin: 14px 0;
}

.md input[type='checkbox'] {
  margin-right: 6px;
}
</style>
