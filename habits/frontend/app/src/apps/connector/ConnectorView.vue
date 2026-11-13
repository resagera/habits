<script setup lang="ts">
// Коннектор: правила, связывающие страницы между собой.
//
// Правило = источники + дерево условий (И/ИЛИ с любой вложенностью) +
// результаты. Источники, поля, операции и виды результатов приходят с сервера
// (meta): страница ничего про них не знает заранее, поэтому новое поле на
// бэкенде появляется в формах само.
//
// Проверка на письме — главный инструмент настройки: она НЕ выполняет
// результаты, а показывает по каждому условию, сошлось оно или нет, и что в
// событии лежало. Без неё правило настраивалось бы отметками в трекере.
import { computed, ref } from 'vue'
import { confirmAction } from '../../shared/telegram'
import { showToast } from '../../shared/toast'
import { colorCss, PRESET_COLORS, PRESET_EMOJI } from '../tracker/display'
import type { Category } from '../tracker/types'
import ConditionNode from './components/ConditionNode.vue'
import {
  applyRule, createRule, deleteRule, fetchConnector, setRuleEnabled, testRule, updateRule,
  type RuleBody,
} from './api'
import {
  emptyGroup, emptyRule, fmtWhen, isGroup,
  type ConnectorAction, type ConnectorNode, type ConnectorOverview, type ConnectorRule,
  type Trace,
} from './types'

type Tab = 'rules' | 'runs'

const tab = ref<Tab>('rules')
const loading = ref(true)
const busy = ref(false)
const data = ref<ConnectorOverview | null>(null)

const rules = computed(() => data.value?.rules ?? [])
const runs = computed(() => data.value?.runs ?? [])
const meta = computed(() => data.value?.meta ?? null)
const trackers = computed(() => data.value?.trackers ?? [])
const messages = computed(() => data.value?.messages ?? [])

async function load() {
  try {
    data.value = await fetchConnector()
  } catch {
    showToast('Не удалось загрузить правила')
  } finally {
    loading.value = false
  }
}
void load()

// --- справочники ---

/** Поля всех выбранных источников: условия строятся по ним. */
const draftFields = computed(() => {
  if (!meta.value || !draft.value) return []
  const picked = new Set(draft.value.sources)
  return meta.value.sources.filter((s) => picked.has(s.code)).flatMap((s) => s.fields)
})

function fieldTitle(code?: string): string {
  for (const s of meta.value?.sources ?? []) {
    const f = s.fields.find((x) => x.code === code)
    if (f) return f.title
  }
  return code ?? ''
}

function cmpTitle(code?: string): string {
  return meta.value?.cmps.find((c) => c.code === code)?.title ?? code ?? ''
}

function trackerByID(id?: number): Category | undefined {
  return trackers.value.find((t) => t.id === id)
}

/** Человеческое описание дерева: его видно в списке правил. */
function describeNode(n: ConnectorNode): string {
  if (isGroup(n)) {
    const parts = (n.nodes ?? []).map(describeNode).filter(Boolean)
    if (!parts.length) return 'любое событие'
    const sep = n.op === 'or' ? ' ИЛИ ' : ' И '
    return parts.length > 1 ? `(${parts.join(sep)})` : parts[0]
  }
  const cmp = meta.value?.cmps.find((c) => c.code === n.cmp)
  const value = cmp?.no_value ? '' : ` «${n.value ?? ''}»`
  return `${fieldTitle(n.field)} ${cmpTitle(n.cmp)}${value}`
}

function describeAction(a: ConnectorAction): string {
  if (a.type === 'tracker_mark') {
    const t = trackerByID(a.category_id)
    const when = a.day === 'today' ? 'сегодня' : 'в день события'
    if (t?.kind === 'counter') return `«${t.name}» +${a.count || 1} ${when}`
    return `отметка в «${t?.name ?? 'удалённый трекер'}» ${when}`
  }
  if (a.type === 'notify') return `сообщение: ${a.text}`
  if (a.type === 'mail_flag') {
    const f = meta.value?.mail_flags.find((x) => x.code === a.flag)
    return `письмо: ${a.value === false ? 'снять ' : ''}${f?.title ?? a.flag}`
  }
  return a.type
}

function sourceTitle(code: string): string {
  const s = meta.value?.sources.find((x) => x.code === code)
  return s ? `${s.icon} ${s.title}` : code
}

// --- редактор ---

const draft = ref<(RuleBody & { id?: number }) | null>(null)
const trace = ref<Trace | null>(null)
const testMessageID = ref(0)
const testLabel = ref('')

function openNew() {
  draft.value = { ...emptyRule() }
  trace.value = null
  testLabel.value = ''
}

/** Правило из списка правится копией: отмена не должна менять список. */
function openEdit(r: ConnectorRule) {
  draft.value = JSON.parse(JSON.stringify({
    id: r.id, name: r.name, enabled: r.enabled, sources: r.sources,
    match: r.match ?? emptyGroup(), actions: r.actions ?? [],
    // пояс берём текущий с устройства: правило считает «сегодня» по нему
    tz_offset_minutes: -new Date().getTimezoneOffset(),
  }))
  trace.value = null
  testLabel.value = ''
}

function closeEdit() {
  draft.value = null
  trace.value = null
}

function toggleSource(code: string) {
  if (!draft.value) return
  const i = draft.value.sources.indexOf(code)
  if (i >= 0) draft.value.sources.splice(i, 1)
  else draft.value.sources.push(code)
}

function addAction(type: string) {
  if (!draft.value || !meta.value) return
  if (draft.value.actions.length >= meta.value.limits.actions) {
    showToast(`Результатов не больше ${meta.value.limits.actions}`)
    return
  }
  if (type === 'tracker_mark') {
    const t = trackers.value[0]
    draft.value.actions.push({
      type, category_id: t?.id ?? 0, day: 'event',
      count: t?.kind === 'counter' ? 1 : undefined,
      emoji: t?.style === 'emoji' ? t.emoji : undefined,
    })
  } else if (type === 'notify') {
    draft.value.actions.push({ type, text: '{subject}' })
  } else {
    draft.value.actions.push({ type, flag: 'is_read', value: true })
  }
}

function removeAction(i: number) {
  draft.value?.actions.splice(i, 1)
}

/** Сменили трекер — цвет и эмодзи из прежнего набора уже не годятся. */
function onTrackerChange(a: ConnectorAction) {
  const t = trackerByID(a.category_id)
  a.color = undefined
  a.emoji = t?.style === 'emoji' ? t.emoji : undefined
  a.count = t?.kind === 'counter' ? (a.count || 1) : undefined
}

function pickColor(a: ConnectorAction, color: string) {
  a.color = a.color === color ? undefined : color
}

function pickEmoji(a: ConnectorAction, emoji: string) {
  a.emoji = a.emoji === emoji ? undefined : emoji
}

function body(): RuleBody {
  const d = draft.value!
  return {
    name: d.name, enabled: d.enabled, sources: d.sources,
    match: d.match, actions: d.actions, tz_offset_minutes: d.tz_offset_minutes,
  }
}

async function save() {
  if (!draft.value) return
  busy.value = true
  try {
    if (draft.value.id) await updateRule(draft.value.id, body())
    else await createRule(body())
    closeEdit()
    await load()
    showToast('Сохранено')
  } catch (e) {
    showToast(errText(e, 'Не удалось сохранить'))
  } finally {
    busy.value = false
  }
}

async function runTest() {
  if (!draft.value) return
  busy.value = true
  try {
    const res = await testRule(body(), testMessageID.value || undefined)
    trace.value = res.trace
    testMessageID.value = res.message_id
    testLabel.value = res.event_label
    showToast(res.matched ? 'Правило подходит' : 'Правило не подходит')
  } catch (e) {
    showToast(errText(e, 'Не удалось проверить'))
  } finally {
    busy.value = false
  }
}

/** Применить сохранённое правило к выбранному письму — по-настоящему. */
async function runApply() {
  if (!draft.value?.id || !testMessageID.value) return
  if (!(await confirmAction('Применить правило к этому письму? Результаты выполнятся по-настоящему.'))) return
  busy.value = true
  try {
    const { result } = await applyRule(draft.value.id, testMessageID.value)
    trace.value = result.trace
    if (!result.matched) showToast('Правило не подошло — ничего не сделано')
    else if (result.error) showToast(`С ошибкой: ${result.error}`)
    else if (result.skipped) showToast('Это письмо правило уже обрабатывало')
    else showToast(result.done.join('; ') || 'Готово')
    await load()
  } catch (e) {
    showToast(errText(e, 'Не удалось применить'))
  } finally {
    busy.value = false
  }
}

async function toggleEnabled(r: ConnectorRule) {
  try {
    await setRuleEnabled(r.id, !r.enabled)
    r.enabled = !r.enabled
  } catch {
    showToast('Не удалось переключить')
  }
}

async function remove(r: ConnectorRule) {
  if (!(await confirmAction(`Удалить правило «${r.name}»? Журнал его срабатываний тоже уйдёт.`))) return
  try {
    await deleteRule(r.id)
    await load()
    showToast('Удалено')
  } catch {
    showToast('Не удалось удалить')
  }
}

function errText(e: unknown, fallback: string): string {
  const msg = (e as { message?: string })?.message
  return msg && msg.length < 200 ? msg : fallback
}

function msgLabel(m: { subject: string; from_addr: string; mail_from: string; received_at: string }): string {
  const from = m.from_addr || m.mail_from || 'неизвестно'
  return `${fmtWhen(m.received_at)} · ${m.subject || '(без темы)'} — ${from}`
}

const ruleTracker = computed(() => (a: ConnectorAction) => trackerByID(a.category_id))
</script>

<template>
  <div class="connector">
    <p v-if="loading" class="hint">Загрузка…</p>

    <template v-else>
      <div class="cards">
        <div class="card">
          <span class="lbl">Правил</span>
          <b>{{ rules.length }}</b>
          <span class="sub">включено: {{ rules.filter((r) => r.enabled).length }}</span>
        </div>
        <div class="card">
          <span class="lbl">Срабатываний</span>
          <b>{{ rules.reduce((n, r) => n + r.runs, 0) }}</b>
          <span class="sub">в журнале: {{ runs.length }}</span>
        </div>
      </div>

      <div class="tabs">
        <button :class="{ on: tab === 'rules' }" @click="tab = 'rules'">Правила</button>
        <button :class="{ on: tab === 'runs' }" @click="tab = 'runs'">Журнал</button>
      </div>

      <!-- ПРАВИЛА -->
      <template v-if="tab === 'rules'">
        <div class="head">
          <button class="primary" @click="openNew">+ Правило</button>
        </div>

        <p v-if="!rules.length" class="hint">
          Правил пока нет. Правило связывает страницы: событие на одной (пришло
          письмо) превращается в действие на другой (отметка в трекере).
          Условий может быть сколько угодно, они соединяются «и» и «или» с любой
          вложенностью.
        </p>

        <div v-for="r in rules" :key="r.id" class="row" :class="{ off: !r.enabled }">
          <div class="row-main">
            <span class="name">
              {{ r.name }}
              <span v-if="!r.enabled" class="tag">выключено</span>
            </span>
            <span class="meta">
              {{ r.sources.map(sourceTitle).join(', ') }} → {{ describeNode(r.match) }}
            </span>
            <span class="meta act">
              ⇒ {{ r.actions.map(describeAction).join(' + ') }}
            </span>
            <span class="meta">
              срабатываний: {{ r.runs }}<template v-if="r.last_run_at">,
                последнее {{ fmtWhen(r.last_run_at) }}</template>
            </span>
          </div>
          <div class="row-right">
            <button class="mini" @click="toggleEnabled(r)">
              {{ r.enabled ? 'Выключить' : 'Включить' }}
            </button>
            <button class="mini" @click="openEdit(r)">Правка</button>
            <button class="mini danger" @click="remove(r)">Удалить</button>
          </div>
        </div>
      </template>

      <!-- ЖУРНАЛ -->
      <template v-else>
        <p class="hint">
          Сработавшие правила и ошибки. «Не подошло» здесь не пишется — иначе
          журнал состоял бы из него; чтобы понять, почему правило не сработало,
          откройте его и нажмите «Проверить».
        </p>
        <p v-if="!runs.length" class="hint">Пока ничего не срабатывало.</p>
        <div v-for="run in runs" :key="run.id" class="row plain"
             :class="{ bad: run.status === 'error' }">
          <div class="row-main">
            <span class="name">
              {{ rules.find((r) => r.id === run.rule_id)?.name ?? 'удалённое правило' }}
              <span v-if="run.status === 'error'" class="tag danger">ошибка</span>
            </span>
            <span class="meta">{{ run.event_label }}</span>
            <span v-if="run.detail" class="meta act">{{ run.detail }}</span>
          </div>
          <div class="row-right">
            <span class="when">{{ fmtWhen(run.at) }}</span>
          </div>
        </div>
      </template>
    </template>

    <!-- РЕДАКТОР -->
    <Teleport to="body">
      <div v-if="draft && meta" class="modal" @click.self="closeEdit">
        <div class="modal-box">
          <h3>{{ draft.id ? 'Правило' : 'Новое правило' }}</h3>

          <label class="lbl">Название</label>
          <input v-model="draft.name" class="inp" placeholder="Например: Поездки из дома" />

          <label class="lbl">Источники</label>
          <div class="chips">
            <button v-for="s in meta.sources" :key="s.code" class="chip"
                    :class="{ on: draft.sources.includes(s.code) }" @click="toggleSource(s.code)">
              {{ s.icon }} {{ s.title }} <span class="sub">· {{ s.event }}</span>
            </button>
          </div>

          <label class="lbl">Условия</label>
          <p class="hint small">
            Группа «и» требует, чтобы сошлось всё; «или» — чтобы сошлось любое.
            Внутрь группы можно вложить другую группу (до {{ meta.limits.depth }}
            уровней). Текстовые сравнения регистр не различают.
          </p>
          <ConditionNode :node="draft.match" :meta="meta" :fields="draftFields" :depth="1"
                         :trace="trace" />

          <label class="lbl">Результаты</label>
          <p v-if="!draft.actions.length" class="hint small">
            Пока ничего не произойдёт — добавьте хотя бы один результат.
          </p>

          <div v-for="(a, i) in draft.actions" :key="i" class="act-box">
            <div class="act-head">
              <b>{{ meta.actions.find((t) => t.code === a.type)?.title ?? a.type }}</b>
              <span class="grow" />
              <button class="mini danger" @click="removeAction(i)">✕</button>
            </div>

            <!-- отметка в трекере -->
            <template v-if="a.type === 'tracker_mark'">
              <select v-model.number="a.category_id" class="inp" @change="onTrackerChange(a)">
                <option v-for="t in trackers" :key="t.id" :value="t.id">
                  {{ t.name }}{{ t.kind === 'counter' ? ' (счётчик)' : '' }}
                </option>
              </select>
              <select v-model="a.day" class="inp">
                <option value="event">в день события (дата письма)</option>
                <option value="today">в день срабатывания</option>
              </select>
              <template v-if="ruleTracker(a)?.kind === 'counter'">
                <label class="lbl">Прибавить</label>
                <input v-model.number="a.count" class="inp" type="number" min="1" max="50" />
              </template>
              <template v-else-if="ruleTracker(a)?.style === 'emoji'">
                <label class="lbl">Эмодзи отметки</label>
                <div class="chips">
                  <button v-for="e in (ruleTracker(a)?.emoji ? [ruleTracker(a)!.emoji] : []).concat(PRESET_EMOJI)"
                          :key="e" class="chip emoji" :class="{ on: a.emoji === e }"
                          @click="pickEmoji(a, e)">{{ e }}</button>
                </div>
              </template>
              <template v-else>
                <label class="lbl">
                  Цвет отметки
                  <span class="sub">не выбран — цвет трекера</span>
                </label>
                <div class="chips">
                  <button v-for="c in (ruleTracker(a)?.palette ?? []).map((p) => p.color2 ? `${p.color},${p.color2}` : p.color).concat(PRESET_COLORS)"
                          :key="c" class="swatch" :class="{ on: a.color === c }"
                          :style="{ background: colorCss(c) }" @click="pickColor(a, c)" />
                </div>
              </template>
            </template>

            <!-- сообщение -->
            <template v-else-if="a.type === 'notify'">
              <input v-model="a.text" class="inp" placeholder="Текст сообщения" />
              <p class="hint small">
                Подставляются {subject}, {from}, {rcpt}, {date}.
              </p>
            </template>

            <!-- пометка письма -->
            <template v-else-if="a.type === 'mail_flag'">
              <select v-model="a.flag" class="inp">
                <option v-for="f in meta.mail_flags" :key="f.code" :value="f.code">
                  {{ f.title }}
                </option>
              </select>
              <label class="check">
                <input v-model="a.value" type="checkbox" />
                ставить (снято — снимать пометку)
              </label>
            </template>
          </div>

          <div class="chips">
            <button v-for="t in meta.actions" :key="t.code" class="chip" @click="addAction(t.code)">
              + {{ t.title }}
            </button>
          </div>

          <label class="lbl">Проверка на письме</label>
          <p class="hint small">
            Проверка ничего не делает — только показывает, сошлись условия или
            нет и что лежало в полях письма.
          </p>
          <select v-model.number="testMessageID" class="inp">
            <option :value="0">последнее письмо</option>
            <option v-for="m in messages" :key="m.id" :value="m.id">{{ msgLabel(m) }}</option>
          </select>
          <p v-if="testLabel" class="hint small">Проверено на: {{ testLabel }}</p>

          <div class="modal-acts">
            <button class="mini" :disabled="busy" @click="runTest">Проверить</button>
            <button v-if="draft.id" class="mini" :disabled="busy || !testMessageID"
                    @click="runApply">
              Применить к письму
            </button>
            <span class="grow" />
            <button class="mini" @click="closeEdit">Отмена</button>
            <button class="primary" :disabled="busy" @click="save">Сохранить</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.connector {
  padding: 10px 12px 24px;
}

.cards {
  display: flex;
  gap: 6px;
}

.card {
  flex: 1;
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  backdrop-filter: var(--card-blur);
}

.card b {
  font-size: 17px;
}

.lbl,
.sub {
  font-size: 12px;
  color: var(--text-secondary);
  display: block;
  margin-top: 8px;
}

.sub {
  font-size: 11px;
  display: inline;
  margin: 0;
}

.tabs {
  display: flex;
  gap: 6px;
  margin: 10px 0;
}

.tabs button {
  flex: 1;
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 8px;
  cursor: pointer;
}

.tabs button.on {
  background: var(--accent-color);
  color: #fff;
}

.head {
  display: flex;
  justify-content: flex-end;
  margin-bottom: 8px;
}

.row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 6px;
  backdrop-filter: var(--card-blur);
}

.row.off {
  opacity: 0.6;
}

.row.bad {
  box-shadow: inset 3px 0 0 #ef4444;
}

.row-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.name {
  font-size: 14px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.meta {
  font-size: 11px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}

.meta.act {
  color: var(--text-color);
  opacity: 0.8;
}

.row-right {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
  white-space: nowrap;
}

.when {
  font-size: 11px;
  color: var(--text-secondary);
}

.tag {
  font-size: 10px;
  color: var(--text-secondary);
  border: 1px solid var(--text-secondary);
  border-radius: 4px;
  padding: 0 4px;
  margin-left: 6px;
  font-weight: 400;
}

.tag.danger {
  color: #ef4444;
  border-color: #ef4444;
}

.hint {
  font-size: 13px;
  color: var(--text-secondary);
}

.hint.small {
  font-size: 11px;
  margin: 4px 0;
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

.primary {
  background: var(--accent-color);
  border: none;
  border-radius: 8px;
  color: #fff;
  font-size: 13px;
  padding: 8px 12px;
  cursor: pointer;
}

.inp {
  width: 100%;
  box-sizing: border-box;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 8px 9px;
  margin-top: 4px;
}

.check {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  margin-top: 6px;
}

.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 4px;
}

.chip {
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 12px;
  padding: 6px 8px;
  cursor: pointer;
  text-align: left;
}

.chip.on {
  background: var(--accent-color);
  color: #fff;
}

.chip.on .sub {
  color: inherit;
}

.chip.emoji {
  font-size: 16px;
  padding: 4px 6px;
}

.swatch {
  width: 26px;
  height: 26px;
  border: 2px solid transparent;
  border-radius: 50%;
  cursor: pointer;
  padding: 0;
}

.swatch.on {
  border-color: var(--text-color);
}

.act-box {
  background: var(--card-color);
  border-radius: 10px;
  padding: 8px;
  margin-top: 6px;
  backdrop-filter: var(--card-blur);
}

.act-head {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
}

.grow {
  flex: 1;
}

.modal {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 16px;
  overflow-y: auto;
  z-index: 50;
}

.modal-box {
  background: var(--bg-color);
  border-radius: 14px;
  padding: 14px;
  width: 100%;
  max-width: 560px;
}

.modal-box h3 {
  margin: 0 0 8px;
  font-size: 16px;
}

.modal-acts {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  margin-top: 12px;
}
</style>
