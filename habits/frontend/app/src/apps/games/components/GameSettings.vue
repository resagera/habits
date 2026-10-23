<script setup lang="ts">
// Настройки одной игры: наборы картинок по назначениям.
//
// Картинки не копируются — это те же фоны приложения («Оформление → Фон»),
// поэтому удалённая там картинка исчезает и из игры, а место она занимает
// один раз. Загрузить новую можно прямо отсюда: она попадёт в общий список.
import { onMounted, ref } from 'vue'
import { api } from '../../../shared/api/client'
import {
  makeImageThumb, resolveBgUrl, uploadBackground, type BackgroundImageItem,
} from '../../../shared/background'
import { showToast } from '../../../shared/toast'
import type { GameImage, GameInfo, GamesState, SlotCode } from '../types'

const props = defineProps<{ game: GameInfo; state: GamesState }>()
const emit = defineEmits<{ save: [slot: SlotCode, ids: number[]] }>()

const all = ref<BackgroundImageItem[]>([])
const picking = ref<SlotCode | null>(null)
const busy = ref(false)
const fileInput = ref<HTMLInputElement>()

onMounted(loadImages)

async function loadImages() {
  try {
    const res = await api.get<{ images: BackgroundImageItem[] }>('/settings/background')
    all.value = res.images ?? []
  } catch {
    all.value = []
  }
}

function chosen(slot: SlotCode): GameImage[] {
  return props.state.backgrounds[props.game.code]?.[slot] ?? []
}

function isChosen(slot: SlotCode, id: number): boolean {
  return chosen(slot).some((i) => i.id === id)
}

function toggle(slot: SlotCode, id: number) {
  const ids = chosen(slot).map((i) => i.id)
  emit('save', slot, ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id])
}

async function onUpload(e: Event) {
  const file = (e.target as HTMLInputElement).files?.[0]
  const slot = picking.value
  if (!file || !slot) return
  busy.value = true
  try {
    const image = await uploadBackground(file, await makeImageThumb(file))
    await loadImages()
    emit('save', slot, [...chosen(slot).map((i) => i.id), image.id])
    showToast('Картинка загружена ✅')
  } catch {
    showToast('Не удалось загрузить (до 5 МБ, jpeg/png/webp/gif)')
  } finally {
    busy.value = false
    if (fileInput.value) fileInput.value.value = ''
  }
}
</script>

<template>
  <p class="hint">
    Картинки берутся из «Настройки → Оформление → Фон приложения» — это тот же
    список, копии не создаются. Чем больше набор, тем реже повторяется картинка.
  </p>

  <div v-for="slot in game.slots" :key="slot.code" class="slot">
    <div class="head">
      <span class="title">{{ slot.title }}</span>
      <button class="btn small" @click="picking = picking === slot.code ? null : slot.code">
        {{ picking === slot.code ? 'Готово' : '＋ Картинки' }}
      </button>
    </div>
    <p class="use">{{ slot.hint }}</p>

    <div v-if="chosen(slot.code).length" class="grid">
      <div v-for="img in chosen(slot.code)" :key="img.id" class="thumb">
        <img :src="resolveBgUrl(img.thumb || img.url)" loading="lazy" />
        <button class="mini" title="Убрать" @click="toggle(slot.code, img.id)">✕</button>
      </div>
    </div>
    <p v-else class="empty">Картинок нет — будут цвета темы.</p>

    <template v-if="picking === slot.code">
      <div class="row">
        <button class="btn small" :disabled="busy" @click="fileInput?.click()">📤 Загрузить</button>
        <input ref="fileInput" type="file" accept="image/*" class="hidden" @change="onUpload" />
      </div>
      <p v-if="!all.length" class="empty">
        Своих картинок пока нет — загрузите здесь или в «Оформлении».
      </p>
      <div v-else class="grid pick">
        <div v-for="img in all" :key="img.id" class="thumb"
             :class="{ on: isChosen(slot.code, img.id) }" @click="toggle(slot.code, img.id)">
          <img :src="resolveBgUrl(img.thumb || img.url)" loading="lazy" />
          <span v-if="isChosen(slot.code, img.id)" class="check">✓</span>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.hint {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 0 0 12px;
}

.slot {
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 10px;
}

.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.title {
  font-size: 15px;
}

.use {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 4px 0 8px;
}

.empty {
  font-size: 12px;
  color: var(--text-secondary);
  margin: 6px 0;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(72px, 1fr));
  gap: 6px;
  margin-bottom: 6px;
}

.grid.pick {
  margin-top: 8px;
}

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

.thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.mini {
  position: absolute;
  right: 2px;
  top: 2px;
  background: rgba(0, 0, 0, 0.45);
  border: none;
  border-radius: 6px;
  color: #fff;
  cursor: pointer;
  font-size: 11px;
  padding: 2px 5px;
}

.check {
  position: absolute;
  right: 4px;
  bottom: 2px;
  color: #fff;
  font-size: 13px;
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.9);
}

.row {
  display: flex;
  gap: 8px;
  margin: 8px 0;
}

.hidden {
  display: none;
}

.btn {
  background: var(--bg-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  cursor: pointer;
  font-size: 14px;
  padding: 8px 12px;
}

.btn.small {
  font-size: 13px;
  padding: 6px 10px;
}

.btn:disabled {
  opacity: 0.6;
}
</style>
