<script setup lang="ts">
// Почта: письма, принятые собственным SMTP-приёмником на resager.ru.
//
// Ящиков и IMAP нет — приёмник только принимает и складывает письма в базу.
// Главная защита от ботов стоит на самом SMTP: адрес получателя проверяется по
// белому списку ещё на RCPT TO, поэтому словарный перебор сюда не доходит.
import { computed, ref } from 'vue'
import { confirmAction } from '../../shared/telegram'
import { showToast } from '../../shared/toast'
import {
  archiveMessage, createAddress, deleteAddress, deleteMessage, deleteReceipt,
  downloadAttachment, fetchGuard, fetchMail, fetchMessage, parseMessage,
  saveMailSettings, setAddressParser, setFlag, unblockIP, updateAddress,
} from './api'
import { fetchRefs } from '../finance/api'
import { flattenCategories, type FinanceRefs } from '../finance/types'
import {
  fmtFull, fmtSize, fmtWhen, PAID_WITH, SPF_LABELS,
  type MailAddress, type MailAttachment, type MailIPStat, type MailMessage,
  type MailOverview, type MailReceipt, type MailReceiptItem,
} from './types'

type Tab = 'inbox' | 'spam' | 'archive' | 'receipts' | 'addresses' | 'guard'

const tab = ref<Tab>('inbox')
const loading = ref(true)
const busy = ref(false)
const data = ref<MailOverview | null>(null)
const search = ref('')
const filterAddr = ref(0)

const domain = computed(() => data.value?.domains?.[0] ?? 'resager.ru')
// справочники Finance нужны форме адреса: куда записывать трату
const refs = ref<FinanceRefs | null>(null)
const receiptsOn = computed(() => data.value?.receipts_allowed === true)
const receipts = computed(() => data.value?.receipts ?? [])
const catList = computed(() => flattenCategories(refs.value?.categories ?? []))
const addresses = computed(() => data.value?.addresses ?? [])
const totals = computed(() => data.value?.totals)

async function load() {
  try {
    const box = tab.value === 'addresses' || tab.value === 'guard' ? 'inbox' : tab.value
    data.value = await fetchMail({
      box, q: search.value.trim(), address_id: filterAddr.value, limit: 100,
    })
  } catch {
    showToast('Не удалось загрузить почту')
  } finally {
    loading.value = false
  }
}
void load()

function switchTab(t: Tab) {
  tab.value = t
  if (t === 'guard') void loadGuard()
  else void load()
  if (t === 'addresses' && receiptsOn.value && !refs.value) void loadRefs()
}

async function loadRefs() {
  try {
    refs.value = await fetchRefs()
  } catch {
    // без справочников форма всё равно работает — просто без выбора счёта
  }
}

let searchTimer: ReturnType<typeof setTimeout> | undefined
function onSearch() {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => void load(), 350)
}

// --- письмо ---

const open = ref<{
  message: MailMessage
  attachments: MailAttachment[]
  receipt?: MailReceipt
  receipt_items?: MailReceiptItem[]
} | null>(null)
const showHTML = ref(false)
const loadImages = ref(false)
const showTech = ref(false)

async function openMessage(m: MailMessage) {
  try {
    open.value = await fetchMessage(m.id)
    showHTML.value = !open.value.message.text_body && !!open.value.message.html_body
    loadImages.value = false
    showTech.value = false
    m.is_read = true
  } catch {
    showToast('Не удалось открыть письмо')
  }
}

/**
 * HTML письма показывается в песочнице: без скриптов и — пока не нажали
 * «показать картинки» — без единого запроса наружу. Картинки в рассылках
 * работают маячками: загрузка сообщает отправителю, что письмо открыли.
 */
const htmlDoc = computed(() => {
  const body = open.value?.message.html_body ?? ''
  const csp = loadImages.value
    ? "default-src 'none'; img-src https: data:; style-src 'unsafe-inline'"
    : "default-src 'none'; style-src 'unsafe-inline'"
  return `<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${csp}">
<style>body{font:14px/1.5 system-ui,sans-serif;color:#111;background:#fff;margin:8px;
overflow-wrap:anywhere}img{max-width:100%;height:auto}</style></head><body>${body}</body></html>`
})

async function toggleStar(m: MailMessage) {
  m.starred = !m.starred
  try {
    await setFlag(m.id, 'starred', m.starred)
  } catch {
    m.starred = !m.starred
    showToast('Не удалось сохранить')
  }
}

async function markSpam(m: MailMessage, spam: boolean) {
  try {
    await setFlag(m.id, 'is_spam', spam)
    open.value = null
    await load()
  } catch {
    showToast('Не удалось сохранить')
  }
}

async function archive(m: MailMessage) {
  try {
    await archiveMessage(m.id, !m.archived_at)
    open.value = null
    await load()
  } catch {
    showToast('Не удалось сохранить')
  }
}

async function remove(m: MailMessage) {
  if (!(await confirmAction(`Удалить письмо «${m.subject || '(без темы)'}» вместе с исходником?`))) return
  try {
    await deleteMessage(m.id)
    open.value = null
    await load()
  } catch {
    showToast('Не удалось удалить')
  }
}

async function download(a: MailAttachment) {
  try {
    await downloadAttachment(a)
  } catch {
    showToast('Не удалось скачать вложение')
  }
}

// --- чеки магазинов ---

/** Разобрать письмо руками: для писем, пришедших до настройки разборщика. */
async function parseNow(m: MailMessage, refresh = false) {
  busy.value = true
  try {
    const res = await parseMessage(m.id, { refresh })
    if (open.value) {
      open.value.receipt = res.receipt
      open.value.receipt_items = res.receipt_items
    }
    await load()
    showToast(refresh ? 'Чек перечитан ✅' : 'Чек записан в траты ✅')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось разобрать')
  } finally {
    busy.value = false
  }
}

async function forgetReceipt(r: MailReceipt) {
  if (!(await confirmAction('Забыть разбор этого чека? Созданная трата останется в Finance.'))) return
  try {
    await deleteReceipt(r.id)
    if (open.value) {
      open.value.receipt = undefined
      open.value.receipt_items = undefined
    }
    await load()
  } catch {
    showToast('Не удалось удалить')
  }
}

const RECEIPT_STATUS: Record<string, string> = {
  parsed: 'разобран, трата не создана',
  imported: 'записан в траты',
  failed: 'не удалось записать',
  skipped: 'пропущен — такая трата уже есть',
}

function fmtAmount(v: number, cur: string): string {
  return `${Math.round(v).toLocaleString('ru-RU')} ${cur.toUpperCase()}`
}

/**
 * Как назвать чек. У магазина есть настоящий номер заказа, у отчёта о поездке
 * его нет — там номер собран из даты и времени, и показывать его как «заказ
 * #20260806-1429» бессмысленно: это поездка, а не заказ.
 */
function receiptRef(r: MailReceipt): string {
  if (r.parser === 'yandexgo') {
    return r.purchased_at
      ? `поездка ${new Date(r.purchased_at).toLocaleString('ru-RU', {
          day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit',
          timeZone: 'UTC',
        })}`
      : 'поездка'
  }
  return `заказ #${r.order_no}`
}

// --- адреса ---

const addrForm = ref<{
  id: number | null
  address: string
  label: string
  kind: 'address' | 'alias'
  only_from: string
  enabled: boolean
  note: string
  parser: string
  parser_category_id: number
  parser_account_id: number
} | null>(null)

function openAddr(a?: MailAddress) {
  addrForm.value = {
    id: a?.id ?? null,
    address: a ? a.address.split('@')[0] : '',
    label: a?.label ?? '',
    kind: a?.kind ?? 'address',
    only_from: a?.only_from ?? '',
    enabled: a?.enabled ?? true,
    note: a?.note ?? '',
    parser: a?.parser ?? '',
    parser_category_id: a?.parser_category_id ?? 0,
    parser_account_id: a?.parser_account_id ?? 0,
  }
  if (receiptsOn.value && !refs.value) void loadRefs()
}

async function saveAddr() {
  const f = addrForm.value
  if (!f) return
  busy.value = true
  try {
    const body = {
      address: f.address, label: f.label, kind: f.kind,
      only_from: f.only_from, enabled: f.enabled, note: f.note,
    }
    const saved = f.id
      ? (await updateAddress(f.id, body)).address
      : (await createAddress(body)).address
    // разбор чеков — отдельным запросом: он под персональным доступом, и
    // обычное сохранение адреса не должно на нём спотыкаться
    if (receiptsOn.value) {
      await setAddressParser(saved.id, {
        parser: f.parser,
        category_id: f.parser_category_id || null,
        account_id: f.parser_account_id || null,
      })
    }
    addrForm.value = null
    await load()
    showToast('Сохранено ✅')
  } catch (e) {
    showToast(e instanceof Error ? e.message : 'Не удалось сохранить')
  } finally {
    busy.value = false
  }
}

async function removeAddr(a: MailAddress) {
  if (!(await confirmAction(`Удалить адрес ${a.address}? Письма останутся, но новые приниматься не будут.`))) return
  try {
    await deleteAddress(a.id)
    addrForm.value = null
    await load()
  } catch {
    showToast('Не удалось удалить')
  }
}

async function copyAddr(a: MailAddress) {
  try {
    await navigator.clipboard.writeText(a.address)
    showToast('Адрес скопирован')
  } catch {
    showToast(a.address)
  }
}

async function toggleNotify() {
  if (!data.value) return
  data.value.notify = !data.value.notify
  try {
    await saveMailSettings({ notify: data.value.notify })
  } catch {
    data.value.notify = !data.value.notify
    showToast('Не удалось сохранить')
  }
}

// --- защита ---

const guard = ref<MailIPStat[]>([])

async function loadGuard() {
  try {
    guard.value = (await fetchGuard()).ips
  } catch {
    showToast('Не удалось загрузить статистику')
  }
}

async function unblock(ip: MailIPStat) {
  try {
    await unblockIP(ip.ip)
    await loadGuard()
    showToast('Разблокирован')
  } catch {
    showToast('Не удалось разблокировать')
  }
}

function isBlocked(ip: MailIPStat): boolean {
  return !!ip.blocked_until && new Date(ip.blocked_until) > new Date()
}
</script>

<template>
  <div class="mail">
    <div v-if="totals" class="cards">
      <div class="card">
        <span class="lbl">Писем</span>
        <b>{{ totals.messages }}</b>
        <span v-if="totals.unread" class="sub">непрочитанных: {{ totals.unread }}</span>
      </div>
      <div class="card">
        <span class="lbl">В спаме</span>
        <b>{{ totals.spam }}</b>
      </div>
      <div class="card">
        <span class="lbl">Отбито попыток</span>
        <b>{{ totals.rejected }}</b>
        <span class="sub">адресов замечено: {{ totals.ips }}</span>
      </div>
      <div class="card" :class="{ warn: totals.blocked > 0 }">
        <span class="lbl">Заблокировано</span>
        <b>{{ totals.blocked }}</b>
      </div>
    </div>

    <div class="tabs">
      <button :class="{ on: tab === 'inbox' }" @click="switchTab('inbox')">Входящие</button>
      <button :class="{ on: tab === 'spam' }" @click="switchTab('spam')">Спам</button>
      <button :class="{ on: tab === 'archive' }" @click="switchTab('archive')">Архив</button>
      <button v-if="receiptsOn" :class="{ on: tab === 'receipts' }" @click="switchTab('receipts')">
        Чеки<span v-if="receipts.length"> ({{ receipts.length }})</span>
      </button>
      <button :class="{ on: tab === 'addresses' }" @click="switchTab('addresses')">Адреса</button>
      <button :class="{ on: tab === 'guard' }" @click="switchTab('guard')">Защита</button>
    </div>

    <!-- ПИСЬМА -->
    <template v-if="tab === 'inbox' || tab === 'spam' || tab === 'archive'">
      <div class="filters">
        <input v-model="search" class="search" placeholder="Поиск по теме, отправителю и тексту"
               @input="onSearch" />
        <select v-model.number="filterAddr" @change="load">
          <option :value="0">все адреса</option>
          <option v-for="a in addresses" :key="a.id" :value="a.id">{{ a.address }}</option>
        </select>
      </div>

      <p v-if="loading" class="hint">Загрузка…</p>
      <p v-else-if="!data?.messages.length" class="hint">
        Писем нет. Адрес для получения заводится на вкладке «Адреса»; всё, что
        приходит на незаведённые адреса, отбивается ещё до приёма.
      </p>

      <div v-for="m in data?.messages ?? []" :key="m.id" class="row"
           :class="{ unread: !m.is_read }" @click="openMessage(m)">
        <div class="row-main">
          <span class="from">
            {{ m.from_name || m.from_addr || m.mail_from || 'неизвестно' }}
            <span v-if="m.spam_score >= 4" class="score" :title="m.spam_reasons">
              ⚠️ {{ m.spam_score }}
            </span>
          </span>
          <span class="subj">{{ m.subject || '(без темы)' }}</span>
          <span class="meta">{{ m.rcpt }} · {{ fmtSize(m.size_bytes) }}</span>
        </div>
        <div class="row-right">
          <span class="when">{{ fmtWhen(m.received_at) }}</span>
          <button class="star" :class="{ on: m.starred }" @click.stop="toggleStar(m)">
            {{ m.starred ? '★' : '☆' }}
          </button>
        </div>
      </div>
    </template>

    <!-- ЧЕКИ -->
    <template v-else-if="tab === 'receipts'">
      <p class="hint">
        Чеки, разобранные из писем магазинов и сервисов. Каждый записан в «Траты»
        одной суммой — итогом чека: позиции магазин показывает по каталожным
        ценам, и с итогом они не сходятся (вес, скидки), а из кошелька уходит итог.
      </p>
      <div v-for="r in receipts" :key="r.id" class="row">
        <div class="row-main">
          <span class="from">
            {{ r.merchant }} · {{ receiptRef(r) }}
            <span v-if="r.status !== 'imported'" class="tag">{{ RECEIPT_STATUS[r.status] }}</span>
          </span>
          <span class="meta">
            {{ r.purchased_on ? fmtWhen(r.purchased_on) : fmtWhen(r.created_at) }}
            <template v-if="r.paid_with"> · {{ PAID_WITH[r.paid_with] ?? r.paid_with }}</template>
            <template v-if="r.delivery_fee || r.service_fee">
              · доставка и сервис {{ fmtAmount(r.delivery_fee + r.service_fee, r.currency) }}
            </template>
          </span>
          <span v-if="r.error" class="meta warn-text">{{ r.error }}</span>
        </div>
        <div class="row-right">
          <span class="amount">{{ fmtAmount(r.total, r.currency) }}</span>
          <button class="mini" title="Забыть разбор" @click="forgetReceipt(r)">✕</button>
        </div>
      </div>
      <p v-if="!receipts.length" class="hint">
        Чеков пока нет. Заведите адрес под магазин, включите у него разбор — и
        пересылайте письма с заказами туда.
      </p>
    </template>

    <!-- АДРЕСА -->
    <template v-else-if="tab === 'addresses'">
      <p class="hint">
        Почта принимается только на заведённые здесь адреса — это и есть главный
        фильтр от ботов. Для магазина заводите отдельный алиас: если он начнёт
        течь спамом, будет видно, кто продал адрес, и его можно выключить.
      </p>
      <div class="head">
        <button class="btn primary grow" @click="openAddr()">＋ Адрес</button>
        <button class="btn" :title="data?.notify ? 'Уведомления включены' : 'Уведомления выключены'"
                @click="toggleNotify">{{ data?.notify ? '🔔' : '🔕' }}</button>
      </div>

      <div v-for="a in addresses" :key="a.id" class="row" :class="{ off: !a.enabled }">
        <div class="row-main">
          <span class="from">
            {{ a.address }}
            <span v-if="a.kind === 'alias'" class="tag">алиас</span>
            <span v-if="!a.enabled" class="tag">выключен</span>
            <span v-if="a.parser" class="tag">🧾 чеки → Finance</span>
          </span>
          <span class="meta">
            {{ a.label || '—' }}
            <template v-if="a.only_from"> · только с {{ a.only_from }}</template>
            · принято {{ a.received }}<template v-if="a.rejected">, отбито {{ a.rejected }}</template>
          </span>
        </div>
        <div class="acts">
          <button class="mini" title="Скопировать" @click="copyAddr(a)">⧉</button>
          <button class="mini" title="Изменить" @click="openAddr(a)">✎</button>
        </div>
      </div>
      <p v-if="!addresses.length" class="hint">Адресов нет.</p>
    </template>

    <!-- ЗАЩИТА -->
    <template v-else>
      <p class="hint">
        Кто подключался к порту 25. Адрес закрывается сам: за перебор получателей,
        поток ошибок или разговор до приветствия — сначала на час, при повторах
        дольше.
      </p>
      <div v-for="ip in guard" :key="ip.ip" class="row" :class="{ blocked: isBlocked(ip) }">
        <div class="row-main">
          <span class="from">
            {{ ip.ip }}
            <span v-if="isBlocked(ip)" class="tag danger">заблокирован</span>
          </span>
          <span class="meta">
            {{ ip.ptr || 'без PTR' }} · подключений {{ ip.connections }},
            принято {{ ip.accepted }}, отбито {{ ip.rejected }}
          </span>
          <span v-if="ip.last_reason" class="meta">причина: {{ ip.last_reason }}</span>
        </div>
        <div class="row-right">
          <span class="when">{{ fmtWhen(ip.last_seen) }}</span>
          <button v-if="isBlocked(ip)" class="mini" @click="unblock(ip)">Открыть</button>
        </div>
      </div>
      <p v-if="!guard.length" class="hint">Пока никто не приходил.</p>
    </template>

    <!-- ПИСЬМО -->
    <Teleport to="body">
      <div v-if="open" class="modal" @click.self="open = null">
        <div class="modal-box">
          <h3>{{ open.message.subject || '(без темы)' }}</h3>
          <p class="meta">
            От: {{ open.message.from_name }} &lt;{{ open.message.from_addr || open.message.mail_from }}&gt;<br />
            Кому: {{ open.message.rcpt }} · {{ fmtFull(open.message.received_at) }}
          </p>

          <div v-if="open.message.spam_score >= 4" class="warnbox">
            ⚠️ Подозрительность {{ open.message.spam_score }}: {{ open.message.spam_reasons }}
          </div>

          <div class="viewswitch">
            <button :class="{ on: !showHTML }" @click="showHTML = false">Текст</button>
            <button :class="{ on: showHTML }" :disabled="!open.message.html_body"
                    @click="showHTML = true">HTML</button>
            <button v-if="showHTML && !loadImages" class="mini" @click="loadImages = true">
              Показать картинки
            </button>
          </div>

          <!-- sandbox="" — самый строгий режим: ни скриптов, ни форм, ни переходов -->
          <iframe v-if="showHTML" class="html" sandbox="" :srcdoc="htmlDoc" />
          <pre v-else class="text">{{ open.message.text_body || '(пусто)' }}</pre>
          <p v-if="showHTML && !loadImages" class="hint small">
            Картинки не загружены: в рассылках они работают маячками и сообщают
            отправителю, что письмо открыли.
          </p>

          <div v-if="open.attachments.length" class="atts">
            <span class="lbl">Вложения</span>
            <button v-for="a in open.attachments" :key="a.id" class="att" @click="download(a)">
              📎 {{ a.filename || 'файл' }} · {{ fmtSize(a.size_bytes) }}
            </button>
          </div>

          <div v-if="open.receipt" class="receipt">
            <div class="receipt-head">
              <b>🧾 {{ open.receipt.merchant }} · {{ receiptRef(open.receipt) }}</b>
              <b>{{ fmtAmount(open.receipt.total, open.receipt.currency) }}</b>
            </div>
            <div class="meta">
              {{ RECEIPT_STATUS[open.receipt.status] }}
              <template v-if="open.receipt.paid_with">
                · {{ PAID_WITH[open.receipt.paid_with] ?? open.receipt.paid_with }}
              </template>
            </div>
            <div v-for="it in open.receipt_items ?? []" :key="it.id" class="ritem">
              <span>{{ it.name }}</span>
              <span class="meta">×{{ it.qty }}{{ it.unit ? ' ' + it.unit : '' }}</span>
              <span>{{ fmtAmount(it.amount, open.receipt.currency) }}</span>
            </div>
            <!-- у чека магазина смысл в позициях, у отчёта о поездке — в заметке -->
            <pre v-if="open.receipt.note" class="rnote">{{ open.receipt.note }}</pre>
            <div v-if="(open.receipt_items ?? []).length" class="ritem sums">
              <span>Товары / доставка / сервис</span>
              <span></span>
              <span>
                {{ fmtAmount(open.receipt.subtotal, open.receipt.currency) }} /
                {{ fmtAmount(open.receipt.delivery_fee, open.receipt.currency) }} /
                {{ fmtAmount(open.receipt.service_fee, open.receipt.currency) }}
              </span>
            </div>
            <div class="racts">
              <button class="link" :disabled="busy" @click="parseNow(open.message, true)">
                Перечитать письмо
              </button>
              <button class="link" @click="forgetReceipt(open.receipt)">Забыть разбор</button>
            </div>
          </div>
          <button v-else-if="receiptsOn" class="btn wide" :disabled="busy"
                  @click="parseNow(open.message)">
            🧾 Разобрать как чек и записать в траты
          </button>

          <button class="link" @click="showTech = !showTech">
            {{ showTech ? '▾' : '▸' }} Технические данные
          </button>
          <div v-if="showTech" class="tech">
            <div>IP: {{ open.message.remote_ip }}</div>
            <div>PTR: {{ open.message.ptr || '—' }}</div>
            <div>HELO: {{ open.message.helo }}</div>
            <div>{{ SPF_LABELS[open.message.spf] ?? open.message.spf ?? '—' }}</div>
            <div>Шифрование: {{ open.message.tls ? 'да' : 'нет' }}</div>
            <div>Message-ID: {{ open.message.message_id || '—' }}</div>
            <div>Размер: {{ fmtSize(open.message.size_bytes) }}</div>
          </div>

          <div class="modal-acts">
            <button class="btn" @click="open = null">Закрыть</button>
            <button v-if="!open.message.is_spam" class="btn" @click="markSpam(open.message, true)">
              В спам
            </button>
            <button v-else class="btn" @click="markSpam(open.message, false)">Не спам</button>
            <button class="btn" @click="archive(open.message)">
              {{ open.message.archived_at ? 'Из архива' : 'В архив' }}
            </button>
            <button class="btn danger" @click="remove(open.message)">Удалить</button>
          </div>
        </div>
      </div>

      <!-- АДРЕС -->
      <div v-if="addrForm" class="modal" @click.self="addrForm = null">
        <div class="modal-box">
          <h3>{{ addrForm.id ? 'Адрес' : 'Новый адрес' }}</h3>
          <label>Тип</label>
          <select v-model="addrForm.kind" :disabled="!!addrForm.id">
            <option value="address">Постоянный адрес</option>
            <option value="alias">Одноразовый алиас под магазин</option>
          </select>
          <label>Имя до «@»</label>
          <div class="addr">
            <input v-model="addrForm.address" :disabled="!!addrForm.id"
                   :placeholder="addrForm.kind === 'alias' ? 'можно оставить пустым' : 'habits'" />
            <span>@{{ domain }}</span>
          </div>
          <p v-if="addrForm.kind === 'alias' && !addrForm.id" class="hint small">
            Пустое имя — сгенерируем из названия, например ozon-a7f3@{{ domain }}.
          </p>
          <label>Для кого (название)</label>
          <input v-model="addrForm.label" placeholder="Ozon, банк, подписки" />
          <label>Принимать только с домена</label>
          <input v-model="addrForm.only_from" placeholder="ozon.ru — пусто значит от кого угодно" />
          <p class="hint small">
            Самый сильный фильтр: письма с других доменов отбиваются ещё до приёма.
          </p>
          <label class="chk">
            <input v-model="addrForm.enabled" type="checkbox" />
            <span>Принимать почту</span>
          </label>
          <label>Заметка</label>
          <input v-model="addrForm.note" />

          <template v-if="receiptsOn">
            <label>Разбирать письма как чеки</label>
            <select v-model="addrForm.parser">
              <option value="">не разбирать</option>
              <option v-for="p in data?.parsers ?? []" :key="p.code" :value="p.code">
                {{ p.title }}
              </option>
            </select>
            <template v-if="addrForm.parser">
              <label>Категория траты</label>
              <select v-model.number="addrForm.parser_category_id">
                <option :value="0">без категории</option>
                <option v-for="{ cat, depth } in catList" :key="cat.id" :value="cat.id">
                  {{ '\u3000'.repeat(depth) }}{{ cat.icon ? cat.icon + ' ' : '' }}{{ cat.name }}
                </option>
              </select>
              <label>Счёт списания</label>
              <select v-model.number="addrForm.parser_account_id">
                <option :value="0">не указан</option>
                <option v-for="a in refs?.accounts ?? []" :key="a.id" :value="a.id">
                  {{ a.name }} ({{ a.currency.toUpperCase() }})
                </option>
              </select>
              <p class="hint small">
                Каждое письмо с заказом станет одной тратой на сумму итога чека.
                Повторная пересылка того же заказа трату не удвоит.
              </p>
            </template>
          </template>

          <div class="modal-acts">
            <button class="btn" @click="addrForm = null">Отмена</button>
            <button v-if="addrForm.id" class="btn danger"
                    @click="removeAddr(addresses.find((x) => x.id === addrForm!.id)!)">Удалить</button>
            <button class="btn primary" :disabled="busy" @click="saveAddr">Сохранить</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 8px;
}

.card {
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  backdrop-filter: var(--card-blur);
}

.card.warn b {
  color: #ef4444;
}

.card b {
  font-size: 17px;
}

.lbl,
.sub {
  font-size: 12px;
  color: var(--text-secondary);
}

.sub {
  font-size: 11px;
}

.tabs {
  display: flex;
  gap: 6px;
  margin: 10px 0;
  overflow-x: auto;
}

.tabs button {
  flex: 1;
  min-width: 84px;
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

.filters {
  display: flex;
  gap: 6px;
  margin-bottom: 8px;
}

.search {
  flex: 2;
}

.filters select {
  flex: 1;
  min-width: 0;
}

.search,
.filters select {
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 8px 9px;
  backdrop-filter: var(--card-blur);
}

.head {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
}

.grow {
  flex: 1;
}

.row {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  background: var(--card-color);
  border-radius: 10px;
  padding: 10px 12px;
  margin-bottom: 6px;
  cursor: pointer;
  backdrop-filter: var(--card-blur);
}

.row.unread {
  box-shadow: inset 3px 0 0 var(--accent-color);
}

.row.blocked {
  box-shadow: inset 3px 0 0 #ef4444;
}

.row.off {
  opacity: 0.6;
}

.row-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.from {
  font-size: 14px;
  font-weight: 600;
  overflow-wrap: anywhere;
}

.subj {
  font-size: 14px;
  overflow-wrap: anywhere;
}

.meta {
  font-size: 11px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
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

.score {
  font-size: 11px;
  color: #f59e0b;
  margin-left: 6px;
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

.star {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 15px;
  cursor: pointer;
  padding: 0;
}

.star.on {
  color: #f59e0b;
}

.acts {
  display: flex;
  gap: 4px;
  align-items: flex-start;
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

.hint {
  font-size: 13px;
  color: var(--text-secondary);
  margin: 10px 0;
}

.hint.small {
  font-size: 11px;
  margin: 4px 0 0;
}

.btn {
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 14px;
  padding: 10px 14px;
  cursor: pointer;
}

.btn.primary {
  background: var(--accent-color);
  color: #fff;
}

.btn.danger {
  color: #ef4444;
}

.link {
  background: none;
  border: none;
  color: var(--accent-color);
  font-size: 13px;
  padding: 10px 0;
  cursor: pointer;
}

.modal {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 20px 12px;
  overflow-y: auto;
  z-index: 1300;
}

.modal-box {
  background: var(--bg-color);
  border-radius: 12px;
  padding: 14px;
  width: 100%;
  max-width: 640px;
}

.modal-box h3 {
  margin: 0 0 8px;
  font-size: 16px;
  overflow-wrap: anywhere;
}

.modal-box label {
  display: block;
  font-size: 12px;
  color: var(--text-secondary);
  margin: 8px 0 4px;
}

.modal-box input:not([type='checkbox']),
.modal-box select {
  width: 100%;
  background: var(--bg-secondary);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 15px;
  padding: 9px 10px;
}

.chk {
  display: flex !important;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--text-color) !important;
  margin: 10px 0 !important;
}

.addr {
  display: flex;
  align-items: center;
  gap: 6px;
}

.addr span {
  font-size: 13px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.warnbox {
  background: rgba(245, 158, 11, 0.15);
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 12px;
  margin: 8px 0;
}

.viewswitch {
  display: flex;
  gap: 6px;
  margin: 10px 0 6px;
}

.viewswitch button {
  background: var(--card-color);
  border: none;
  border-radius: 6px;
  color: var(--text-color);
  font-size: 12px;
  padding: 6px 10px;
  cursor: pointer;
}

.viewswitch button.on {
  background: var(--accent-color);
  color: #fff;
}

.viewswitch button:disabled {
  opacity: 0.4;
}

/* HTML письма — в песочнице: без скриптов и без запросов наружу */
.html {
  width: 100%;
  height: 60vh;
  border: none;
  border-radius: 8px;
  background: #fff;
}

.text {
  background: var(--card-color);
  border-radius: 8px;
  padding: 10px;
  font-size: 13px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 60vh;
  overflow-y: auto;
  margin: 0;
}

.atts {
  margin-top: 10px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.att {
  background: var(--card-color);
  border: none;
  border-radius: 8px;
  color: var(--text-color);
  font-size: 13px;
  padding: 8px 10px;
  text-align: left;
  cursor: pointer;
}

.receipt {
  background: var(--card-color);
  border-radius: 8px;
  padding: 10px;
  margin: 10px 0;
}

.receipt-head {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  font-size: 14px;
  margin-bottom: 4px;
}

.rnote {
  margin: 6px 0 0;
  font: inherit;
  font-size: 12px;
  line-height: 1.45;
  color: var(--text-secondary);
  /* адреса и марки машин длинные — переносим, а не режем полосой прокрутки */
  white-space: pre-wrap;
  word-break: break-word;
}

.ritem {
  display: grid;
  grid-template-columns: 1fr auto auto;
  gap: 8px;
  font-size: 12px;
  padding: 3px 0;
  border-top: 1px solid var(--bg-color);
  overflow-wrap: anywhere;
}

.racts {
  display: flex;
  gap: 12px;
}

.ritem.sums {
  color: var(--text-secondary);
}

.warn-text {
  color: #ef4444;
}

.tech {
  background: var(--card-color);
  border-radius: 8px;
  padding: 8px 10px;
  font-size: 12px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}

.modal-acts {
  display: flex;
  gap: 6px;
  margin-top: 14px;
  flex-wrap: wrap;
}

.modal-acts .btn {
  flex: 1;
  min-width: 90px;
}
</style>
