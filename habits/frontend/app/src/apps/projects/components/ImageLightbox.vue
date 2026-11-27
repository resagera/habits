<script setup lang="ts">
import { computed, ref, watch } from 'vue'

// Полноэкранный просмотр галереи: свайп-переключение стрелками, pinch-zoom
// двумя пальцами (pointer events), панорамирование при зуме, дабл-тап 2x.
const props = defineProps<{
  images: string[]
  index: number
}>()

const emit = defineEmits<{
  'update:index': [i: number]
  remove: [i: number]
  close: []
}>()

const scale = ref(1)
const tx = ref(0)
const ty = ref(0)
const confirmDel = ref(false)

watch(
  () => props.index,
  () => {
    scale.value = 1
    tx.value = 0
    ty.value = 0
    confirmDel.value = false
  },
)

const style = computed(() => ({
  transform: `translate(${tx.value}px, ${ty.value}px) scale(${scale.value})`,
}))

// --- жесты ---
const pointers = new Map<number, { x: number; y: number }>()
let startDist = 0
let startScale = 1
let startTx = 0
let startTy = 0
let startMid = { x: 0, y: 0 }
let lastTap = 0

function onPointerDown(e: PointerEvent) {
  ;(e.target as HTMLElement).setPointerCapture?.(e.pointerId)
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
  if (pointers.size === 2) {
    const [a, b] = [...pointers.values()]
    startDist = Math.hypot(a.x - b.x, a.y - b.y)
    startScale = scale.value
    startMid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
    startTx = tx.value
    startTy = ty.value
  } else if (pointers.size === 1) {
    startTx = tx.value
    startTy = ty.value
    startMid = { x: e.clientX, y: e.clientY }
    // дабл-тап: зум 2x / сброс
    const now = Date.now()
    if (now - lastTap < 300) {
      if (scale.value > 1) {
        scale.value = 1
        tx.value = 0
        ty.value = 0
      } else {
        scale.value = 2
      }
    }
    lastTap = now
  }
}

function onPointerMove(e: PointerEvent) {
  if (!pointers.has(e.pointerId)) return
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
  if (pointers.size === 2) {
    const [a, b] = [...pointers.values()]
    const dist = Math.hypot(a.x - b.x, a.y - b.y)
    scale.value = Math.min(6, Math.max(1, (startScale * dist) / (startDist || 1)))
    const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
    tx.value = startTx + (mid.x - startMid.x)
    ty.value = startTy + (mid.y - startMid.y)
  } else if (pointers.size === 1 && scale.value > 1) {
    tx.value = startTx + (e.clientX - startMid.x)
    ty.value = startTy + (e.clientY - startMid.y)
  }
}

function onPointerUp(e: PointerEvent) {
  pointers.delete(e.pointerId)
  if (scale.value <= 1.02) {
    scale.value = 1
    tx.value = 0
    ty.value = 0
  }
}

function prev() {
  if (props.index > 0) emit('update:index', props.index - 1)
}

function next() {
  if (props.index < props.images.length - 1) emit('update:index', props.index + 1)
}
</script>

<template>
  <Teleport to="body">
    <div class="lb" @click.self="emit('close')">
      <div
        class="lb-stage"
        @pointerdown="onPointerDown"
        @pointermove="onPointerMove"
        @pointerup="onPointerUp"
        @pointercancel="onPointerUp"
      >
        <img :src="images[index]" :style="style" alt="" draggable="false" />
      </div>

      <div class="lb-top">
        <span class="lb-count">{{ index + 1 }} / {{ images.length }}</span>
        <button class="lb-btn" @click="emit('close')">✕</button>
      </div>

      <button v-if="index > 0" class="lb-nav left" @click="prev">‹</button>
      <button v-if="index < images.length - 1" class="lb-nav right" @click="next">›</button>

      <div class="lb-bottom">
        <button v-if="!confirmDel" class="lb-btn" @click="confirmDel = true">🗑 Удалить</button>
        <button v-else class="lb-btn danger" @click="emit('remove', index)">Точно удалить?</button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.lb {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.94);
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
}

.lb-stage {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  touch-action: none;
}

.lb-stage img {
  max-width: 100vw;
  max-height: 100vh;
  user-select: none;
  transition: transform 0.05s linear;
}

.lb-top {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
}

.lb-count {
  color: #ccc;
  font-size: 13px;
}

.lb-btn {
  background: rgba(255, 255, 255, 0.12);
  border: none;
  border-radius: 8px;
  color: #fff;
  font-size: 14px;
  padding: 8px 12px;
}

.lb-btn.danger {
  background: #b91c1c;
}

.lb-nav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  background: rgba(255, 255, 255, 0.1);
  border: none;
  border-radius: 50%;
  color: #fff;
  font-size: 26px;
  width: 42px;
  height: 42px;
  line-height: 1;
}

.lb-nav.left {
  left: 8px;
}

.lb-nav.right {
  right: 8px;
}

.lb-bottom {
  position: absolute;
  bottom: 16px;
  left: 0;
  right: 0;
  display: flex;
  justify-content: center;
}
</style>
