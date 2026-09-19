<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { showToast } from '../../../shared/toast'
import { totpCode, totpSecondsLeft } from '../totp'

const props = defineProps<{ secret: string }>()

const code = ref('······')
const left = ref(30)
let timer: ReturnType<typeof setInterval> | undefined
let lastCounter = -1

async function tick() {
  left.value = totpSecondsLeft()
  const counter = Math.floor(Date.now() / 1000 / 30)
  if (counter === lastCounter) return
  lastCounter = counter
  code.value = (await totpCode(props.secret)) ?? 'ошибка'
}

async function copy() {
  if (!/^\d{6}$/.test(code.value)) return
  try {
    await navigator.clipboard.writeText(code.value)
    showToast('Код скопирован')
  } catch {
    showToast('Не удалось скопировать')
  }
}

onMounted(() => {
  tick()
  timer = setInterval(tick, 1000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <button class="totp" title="Копировать код" @click.stop="copy">
    <span class="totp-code">{{ code.slice(0, 3) }} {{ code.slice(3) }}</span>
    <span class="totp-left" :class="{ ending: left <= 5 }">{{ left }}</span>
  </button>
</template>

<style scoped>
.totp {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: var(--bg-secondary);
  border: none;
  border-radius: 6px;
  padding: 2px 8px;
  margin-top: 2px;
}

.totp-code {
  font-family: monospace;
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 1px;
  color: var(--accent-color);
}

.totp-left {
  font-size: 10px;
  color: var(--text-secondary);
  min-width: 14px;
  text-align: right;
}

.totp-left.ending {
  color: #ef4444;
}
</style>
