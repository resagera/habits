<script setup lang="ts">
// Картинка фона во весь экран: посмотреть, полистать соседние, примерить
// на настоящей странице и только потом решать, ставить ли её фоном.
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { humanSize, type ViewerItem } from './types'

const props = defineProps<{
  items: ViewerItem[]
  index: number
  /** адрес картинки, которая стоит фоном сейчас */
  currentUrl: string
  busy?: boolean
}>()

const emit = defineEmits<{
  close: []
  'update:index': [value: number]
  apply: [item: ViewerItem]
  fit: [item: ViewerItem]
  crop: [item: ViewerItem]
}>()

const item = computed<ViewerItem | null>(() => props.items[props.index] ?? null)
const isCurrent = computed(() => !!item.value && item.value.url === props.currentUrl)

/** Натуральный размер: он известен только после загрузки самой картинки. */
const natural = ref<{ w: number; h: number } | null>(null)

watch(item, () => (natural.value = null))

function onLoad(e: Event) {
  const img = e.target as HTMLImageElement
  natural.value = { w: img.naturalWidth, h: img.naturalHeight }
}

/** Картинка меньше экрана растянется и будет мылить — про это честно пишем. */
const tooSmall = computed(() => {
  const n = natural.value
  if (!n) return false
  return n.w < window.innerWidth || n.h < window.innerHeight
})

const info = computed(() => {
  const parts: string[] = []
  if (natural.value) parts.push(`${natural.value.w}×${natural.value.h}`)
  const size = humanSize(item.value?.size ?? 0)
  if (size) parts.push(size)
  if (props.items.length > 1) parts.push(`${props.index + 1} из ${props.items.length}`)
  return parts.join(' · ')
})

function step(delta: number) {
  if (props.items.length < 2) return
  const next = (props.index + delta + props.items.length) % props.items.length
  emit('update:index', next)
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
  else if (e.key === 'ArrowRight') step(1)
  else if (e.key === 'ArrowLeft') step(-1)
}

// свайп по картинке: на телефоне стрелок нет, а листать хочется
let touchX = 0
function onTouchStart(e: TouchEvent) {
  touchX = e.changedTouches[0].clientX
}
function onTouchEnd(e: TouchEvent) {
  const dx = e.changedTouches[0].clientX - touchX
  if (Math.abs(dx) > 50) step(dx < 0 ? 1 : -1)
}

onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => window.removeEventListener('keydown', onKey))
</script>

<template>
  <div v-if="item" class="viewer">
    <div class="pic" @click.self="$emit('close')"
         @touchstart.passive="onTouchStart" @touchend.passive="onTouchEnd">
      <img :src="item.src" @load="onLoad" />
      <button v-if="items.length > 1" class="nav prev" aria-label="Предыдущая"
              @click.stop="step(-1)">‹</button>
      <button v-if="items.length > 1" class="nav next" aria-label="Следующая"
              @click.stop="step(1)">›</button>
    </div>

    <p v-if="info" class="info">
      {{ info }}
      <span v-if="tooSmall" class="warn">· меньше экрана, будет мылить</span>
    </p>

    <div class="bar">
      <button class="btn" @click="$emit('close')">Закрыть</button>
      <button class="btn" :disabled="busy" @click="$emit('fit', item)">Примерить</button>
      <button v-if="item.id !== null" class="btn" :disabled="busy"
              @click="$emit('crop', item)">Кадрировать</button>
      <button class="btn primary" :disabled="busy || isCurrent" @click="$emit('apply', item)">
        {{ isCurrent ? 'Установлен ✓' : 'Установить' }}
      </button>
    </div>
  </div>
</template>

<style scoped>
/* просмотрщик выше модалок: из него открывается только кадрирование */
.viewer {
  position: fixed;
  inset: 0;
  z-index: 1300;
  background: rgba(0, 0, 0, 0.96);
  display: flex;
  flex-direction: column;
}

.pic {
  position: relative;
  flex: 1;
  min-height: 0; /* иначе картинка распирает колонку и кнопки уезжают за экран */
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 10px;
}

.pic img {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
  border-radius: 8px;
}

.nav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  background: rgba(0, 0, 0, 0.45);
  border: none;
  border-radius: 50%;
  color: #fff;
  cursor: pointer;
  font-size: 26px;
  line-height: 1;
  width: 40px;
  height: 40px;
  padding: 0 0 4px;
}

.nav.prev {
  left: 8px;
}

.nav.next {
  right: 8px;
}

.info {
  margin: 0;
  padding: 0 14px 6px;
  color: rgba(255, 255, 255, 0.7);
  font-size: 12px;
  text-align: center;
}

.warn {
  color: #fbbf24;
}

.bar {
  display: flex;
  gap: 8px;
  padding: 10px 12px calc(10px + env(safe-area-inset-bottom));
}

.btn {
  flex: 1;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  cursor: pointer;
  font-size: 14px;
  padding: 12px 6px;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn:disabled {
  opacity: 0.6;
}
</style>
