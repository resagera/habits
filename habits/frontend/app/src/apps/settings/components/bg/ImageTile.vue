<script setup lang="ts">
// Плитка картинки фона: своя (с папкой и удалением) или из общей галереи.
import { resolveBgUrl } from '../../../../shared/background'

defineProps<{
  img: { id: number; url: string; thumb?: string }
  /** своя картинка или из общей галереи */
  own: boolean
  /** стоит фоном прямо сейчас */
  active?: boolean
  /** режим выделения: клик по плитке отмечает, а не ставит фон */
  selectMode?: boolean
  selected?: boolean
  /** вес файла — показывается только в режиме разбора тяжёлых картинок */
  size?: string
}>()

defineEmits<{
  use: []
  zoom: []
  move: []
  remove: []
  toggle: []
}>()
</script>

<template>
  <div class="thumb" :class="{ on: active, sel: selected }"
       @click="selectMode ? $emit('toggle') : $emit('use')">
    <img :src="resolveBgUrl(img.thumb || img.url)" loading="lazy" />

    <span v-if="selectMode" class="check">{{ selected ? '✓' : '' }}</span>
    <template v-else>
      <template v-if="own">
        <button class="mini fld" title="В папку" @click.stop="$emit('move')">📁</button>
        <button class="mini del" title="Удалить" @click.stop="$emit('remove')">✕</button>
      </template>
      <button class="mini zoom" title="Развернуть" @click.stop="$emit('zoom')">⤢</button>
    </template>
    <span v-if="size" class="size">{{ size }}</span>
  </div>
</template>

<style scoped>
.thumb {
  position: relative;
  aspect-ratio: 1;
  border-radius: 8px;
  overflow: hidden;
  border: 2px solid transparent;
  cursor: pointer;
}

.thumb.on {
  border-color: var(--accent-color);
}

.thumb.sel {
  border-color: var(--accent-color);
}

.thumb.sel img {
  opacity: 0.55;
}

.thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.check {
  position: absolute;
  top: 4px;
  right: 4px;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  border: 2px solid #fff;
  background: rgba(0, 0, 0, 0.35);
  color: #fff;
  font-size: 13px;
  line-height: 19px;
  text-align: center;
}

.thumb.sel .check {
  background: var(--accent-color);
}

.mini {
  position: absolute;
  top: 2px;
  background: rgba(0, 0, 0, 0.45);
  border: none;
  border-radius: 6px;
  color: #fff;
  cursor: pointer;
  font-size: 11px;
  padding: 2px 5px;
}

.mini.fld {
  left: 2px;
}

.mini.del {
  right: 2px;
}

/* «развернуть» — в нижнем углу, чтобы не спорить с папкой и крестиком */
.size {
  position: absolute;
  right: 2px;
  bottom: 2px;
  background: rgba(0, 0, 0, 0.55);
  border-radius: 6px;
  color: #fff;
  font-size: 10px;
  padding: 1px 4px;
}

.mini.zoom {
  top: auto;
  bottom: 2px;
  left: 2px;
  font-size: 12px;
}
</style>
