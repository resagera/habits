<script setup lang="ts">
/**
 * Узел дерева условий. Рисует себя рекурсивно: группа «и»/«или» содержит
 * условия и такие же группы, вложенность — любая (до limits.depth).
 *
 * Узел правится НА МЕСТЕ: дерево живёт в состоянии страницы, и прокидывать
 * наверх новый объект на каждое нажатие клавиши значило бы пересобирать всё
 * дерево ради одного поля. Поэтому компонент меняет переданный узел, а не
 * копию, и сообщает наружу только о своём удалении.
 */
import { computed } from 'vue'
import ConditionNode from './ConditionNode.vue'
import { isGroup, type ConnectorMeta, type ConnectorNode, type Trace } from '../types'

const props = defineProps<{
  node: ConnectorNode
  meta: ConnectorMeta
  /** поля доступны те, что есть у выбранных источников правила */
  fields: { code: string; title: string; kind: string; hint?: string }[]
  depth: number
  trace?: Trace | null
  removable?: boolean
}>()

const emit = defineEmits<{ remove: [] }>()

const group = computed(() => isGroup(props.node))
const nodes = computed(() => props.node.nodes ?? [])
const tooDeep = computed(() => props.depth >= props.meta.limits.depth)

const fieldKind = computed(() => {
  const f = props.fields.find((x) => x.code === props.node.field)
  return f?.kind ?? 'text'
})

/** Операции только те, что подходят виду поля: «содержит» для флага бессмыслен. */
const cmps = computed(() => props.meta.cmps.filter((c) => c.kind === fieldKind.value))

const needsValue = computed(() => {
  const c = props.meta.cmps.find((x) => x.code === props.node.cmp)
  return !!c && !c.no_value
})

const hint = computed(() => props.fields.find((f) => f.code === props.node.field)?.hint ?? '')

function addLeaf() {
  if (!props.node.nodes) props.node.nodes = []
  const f = props.fields[0]
  props.node.nodes.push({ field: f?.code ?? 'subject', cmp: 'contains', value: '' })
}

function addGroup() {
  if (!props.node.nodes) props.node.nodes = []
  // вложенная группа по умолчанию противоположна родительской: именно за этим
  // её и добавляют — «и ещё одно из…» внутри «и»
  props.node.nodes.push({ op: props.node.op === 'and' ? 'or' : 'and', nodes: [] })
}

function removeChild(i: number) {
  props.node.nodes?.splice(i, 1)
}

/** Смена поля может сделать операцию неприменимой — подставляем подходящую. */
function onFieldChange() {
  if (!cmps.value.some((c) => c.code === props.node.cmp)) {
    props.node.cmp = cmps.value[0]?.code
    props.node.value = ''
  }
}
</script>

<template>
  <!-- ГРУППА -->
  <div v-if="group" class="node group" :class="{ fail: trace && !trace.ok, pass: trace?.ok }">
    <div class="node-head">
      <select v-model="node.op" class="op">
        <option value="and">и — сошлись все</option>
        <option value="or">или — сошлось любое</option>
      </select>
      <span v-if="trace" class="verdict">{{ trace.ok ? '✓' : '✗' }}</span>
      <span class="grow" />
      <button class="mini" @click="addLeaf">+ условие</button>
      <button v-if="!tooDeep" class="mini" @click="addGroup">+ группа</button>
      <button v-if="removable" class="mini danger" @click="emit('remove')">✕</button>
    </div>

    <p v-if="!nodes.length" class="hint small">
      Условий нет — правило сработает на каждое событие источника.
    </p>

    <div class="children">
      <ConditionNode v-for="(child, i) in nodes" :key="i" :node="child" :meta="meta"
                     :fields="fields" :depth="depth + 1" :trace="trace?.nodes?.[i] ?? null"
                     removable @remove="removeChild(i)" />
    </div>
  </div>

  <!-- УСЛОВИЕ -->
  <div v-else class="node leaf" :class="{ fail: trace && !trace.ok, pass: trace?.ok }">
    <div class="leaf-row">
      <select v-model="node.field" class="field" @change="onFieldChange">
        <option v-for="f in fields" :key="f.code" :value="f.code">{{ f.title }}</option>
      </select>
      <select v-model="node.cmp" class="cmp">
        <option v-for="c in cmps" :key="c.code" :value="c.code">{{ c.title }}</option>
      </select>
      <input v-if="needsValue" v-model="node.value" class="value" placeholder="значение" />
      <span v-if="trace" class="verdict">{{ trace.ok ? '✓' : '✗' }}</span>
      <button v-if="removable" class="mini danger" @click="emit('remove')">✕</button>
    </div>
    <p v-if="hint" class="hint small">{{ hint }}</p>
    <p v-if="trace" class="hint small got">
      в событии было: <b>{{ trace.got || '(пусто)' }}</b>
    </p>
  </div>
</template>

<style scoped>
.node {
  border-radius: 10px;
  padding: 8px;
  margin-bottom: 6px;
  background: var(--card-color);
  backdrop-filter: var(--card-blur);
}

.node.group {
  border: 1px solid var(--bg-secondary);
}

.node.pass {
  box-shadow: inset 3px 0 0 #22c55e;
}

.node.fail {
  box-shadow: inset 3px 0 0 #ef4444;
}

.node-head,
.leaf-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.children {
  margin: 6px 0 0 10px;
  padding-left: 8px;
  border-left: 2px solid var(--bg-secondary);
}

.grow {
  flex: 1;
}

select,
input {
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 6px 8px;
  min-width: 0;
}

.op {
  font-weight: 600;
}

.field,
.cmp {
  flex: 1;
  min-width: 120px;
}

.value {
  flex: 2;
  min-width: 140px;
}

.mini {
  background: var(--bg-color);
  border: none;
  border-radius: 6px;
  color: var(--text-color);
  font-size: 12px;
  padding: 5px 8px;
  cursor: pointer;
}

.mini.danger {
  color: #ef4444;
}

.verdict {
  font-size: 14px;
  font-weight: 700;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
}

.hint.small {
  font-size: 11px;
  margin: 4px 0 0;
}

.got b {
  font-weight: 600;
  overflow-wrap: anywhere;
}
</style>
