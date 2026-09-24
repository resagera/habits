<script setup lang="ts">
import { computed } from 'vue'
import type { Task } from '../types'
import { fmtDue, isOverdue, PRIORITY_COLORS } from '../types'

const props = defineProps<{
  task: Task
  /** имя категории для контекста (в «Сегодня»/«Планах») */
  categoryName?: string
  /** стрелки ручного порядка (вкладка «Все») */
  reorderable?: boolean
  done?: boolean
}>()

const emit = defineEmits<{
  complete: []
  open: []
  moveUp: []
  moveDown: []
}>()

const overdue = computed(() => isOverdue(props.task))
const strip = computed(() => PRIORITY_COLORS[props.task.priority] ?? 'transparent')
</script>

<template>
  <div class="task-row" :class="{ done }" :style="{ '--strip': strip }" @click="emit('open')">
    <button
      class="check"
      :class="{ checked: done }"
      :title="done ? 'Выполнена' : 'Выполнить'"
      @click.stop="emit('complete')"
    >
      <span v-if="done">✓</span>
    </button>
    <div class="body">
      <div class="title" :class="{ struck: done }">{{ task.title }}</div>
      <div class="meta">
        <span v-if="task.due_date" class="due" :class="{ overdue }">
          📅 {{ fmtDue(task) }}
        </span>
        <span v-if="task.checklist_total > 0" class="chip">
          ☑ {{ task.checklist_done }}/{{ task.checklist_total }}
        </span>
        <span v-if="task.repeat_kind" class="chip">🔁</span>
        <span v-if="task.remind" class="chip">🔔</span>
        <span v-if="task.note" class="chip">📝</span>
        <span v-if="task.assignee_id" class="chip">👤</span>
        <span v-if="categoryName" class="chip category">{{ categoryName }}</span>
        <span v-if="done" class="chip">{{ task.status }}</span>
      </div>
    </div>
    <div v-if="reorderable" class="reorder" @click.stop>
      <button title="Выше" @click="emit('moveUp')">▲</button>
      <button title="Ниже" @click="emit('moveDown')">▼</button>
    </div>
  </div>
</template>

<style scoped>
.task-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 8px 10px 8px 12px;
  border-radius: 8px;
  background: var(--bg-secondary);
  margin-bottom: 6px;
  cursor: pointer;
  position: relative;
  overflow: hidden;
}

/* полоска приоритета слева */
.task-row::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 4px;
  background: var(--strip);
}

.task-row.done {
  opacity: 0.75;
}

.check {
  flex: none;
  width: 22px;
  height: 22px;
  margin-top: 1px;
  border: 2px solid var(--text-secondary);
  border-radius: 50%;
  background: none;
  color: #fff;
  font-size: 13px;
  line-height: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0;
}

.check.checked {
  background: var(--accent-color);
  border-color: var(--accent-color);
}

.body {
  flex: 1;
  min-width: 0;
}

.title {
  font-size: 14px;
  line-height: 1.35;
  overflow-wrap: anywhere;
}

.title.struck {
  text-decoration: line-through;
  color: var(--text-secondary);
}

.meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 2px;
}

.meta:empty {
  display: none;
}

.due {
  font-size: 11px;
  color: var(--text-secondary);
}

.due.overdue {
  color: #f44336;
  font-weight: 600;
}

.chip {
  font-size: 11px;
  color: var(--text-secondary);
}

.chip.category {
  background: var(--card-color);
  border-radius: 8px;
  padding: 1px 7px;
}

.reorder {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: none;
}

.reorder button {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 10px;
  padding: 2px 6px;
  line-height: 1;
}

/* карточки-«стекло»: размытие под .category (класс неоднозначный — scoped) */
:root[data-card-glass] .category {
  backdrop-filter: blur(var(--card-blur, 0px));
  -webkit-backdrop-filter: blur(var(--card-blur, 0px));
}
</style>
