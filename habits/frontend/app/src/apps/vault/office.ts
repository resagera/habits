/**
 * Разбор docx и zip прямо в браузере — тяжёлую библиотеку не тянем.
 *
 * docx — это обычный zip с `word/document.xml`; для просмотра достаточно
 * абзацев, заголовков, жирного/курсива и списков. Полная верстка здесь и не
 * нужна: сейф показывает содержимое, а не редактирует его.
 */
import { unzipSync, zipSync } from 'fflate'
import type { FileMeta } from './types'

/** Потолок разбора: zip разворачивается в памяти целиком. */
const MAX_UNZIP = 64 << 20

export interface ZipEntry {
  name: string
  size: number
}

export function isDocx(meta: FileMeta): boolean {
  return (
    meta.type === 'application/vnd.openxmlformats-officedocument.wordprocessingml.document' ||
    meta.name.toLowerCase().endsWith('.docx')
  )
}

export function isZip(meta: FileMeta): boolean {
  const t = meta.type
  return (
    t === 'application/zip' ||
    t === 'application/x-zip-compressed' ||
    meta.name.toLowerCase().endsWith('.zip')
  )
}

function esc(s: string): string {
  return s.replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' })[c] as string)
}

/**
 * docx → HTML. Строим строку сами из текстовых узлов с экранированием:
 * ничего исполняемого из документа в разметку попасть не может.
 */
export async function docxToHtml(blob: Blob): Promise<string> {
  if (blob.size > MAX_UNZIP) throw new Error('слишком большой файл')
  const zip = unzipSync(new Uint8Array(await blob.arrayBuffer()), {
    filter: (f) => f.name === 'word/document.xml',
  })
  const xml = zip['word/document.xml']
  if (!xml) throw new Error('это не docx')
  const doc = new DOMParser().parseFromString(new TextDecoder().decode(xml), 'application/xml')
  const W = 'http://schemas.openxmlformats.org/wordprocessingml/2006/main'

  const out: string[] = []
  let listOpen = false
  for (const p of Array.from(doc.getElementsByTagNameNS(W, 'p'))) {
    const style = p.getElementsByTagNameNS(W, 'pStyle')[0]?.getAttributeNS(W, 'val') ?? ''
    const bullet = p.getElementsByTagNameNS(W, 'numPr').length > 0
    const parts: string[] = []
    for (const run of Array.from(p.getElementsByTagNameNS(W, 'r'))) {
      const text = Array.from(run.getElementsByTagNameNS(W, 't'))
        .map((t) => t.textContent ?? '')
        .join('')
      if (!text) continue
      let html = esc(text)
      if (run.getElementsByTagNameNS(W, 'b').length) html = `<b>${html}</b>`
      if (run.getElementsByTagNameNS(W, 'i').length) html = `<i>${html}</i>`
      parts.push(html)
    }
    const body = parts.join('')
    if (bullet) {
      if (!listOpen) {
        out.push('<ul>')
        listOpen = true
      }
      out.push(`<li>${body || '&nbsp;'}</li>`)
      continue
    }
    if (listOpen) {
      out.push('</ul>')
      listOpen = false
    }
    const heading = /^Heading([1-6])$/.exec(style)
    if (heading) out.push(`<h${heading[1]}>${body}</h${heading[1]}>`)
    else out.push(`<p>${body || '&nbsp;'}</p>`)
  }
  if (listOpen) out.push('</ul>')
  return out.join('\n')
}

/** Список содержимого архива с распакованными размерами. */
export async function zipList(blob: Blob): Promise<ZipEntry[]> {
  if (blob.size > MAX_UNZIP) throw new Error('слишком большой архив')
  const files = unzipSync(new Uint8Array(await blob.arrayBuffer()))
  return Object.entries(files)
    .filter(([name]) => !name.endsWith('/'))
    .map(([name, data]) => ({ name, size: data.length }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

/**
 * Собрать zip из расшифрованных файлов. Без сжатия: содержимое сейфа уже
 * несжимаемо, а level > 0 на телефоне стоит заметного времени.
 */
export async function zipFiles(items: { name: string; blob: Blob }[]): Promise<Blob> {
  const entries: Record<string, Uint8Array> = {}
  const used = new Set<string>()
  for (const item of items) {
    // имена в сейфе могут повторяться — иначе файлы затирали бы друг друга
    let name = item.name.replace(/[/\\]/g, '_') || 'file'
    for (let i = 2; used.has(name); i++) {
      const dot = name.lastIndexOf('.')
      const base = dot > 0 ? name.slice(0, dot) : name
      const ext = dot > 0 ? name.slice(dot) : ''
      name = `${base} (${i})${ext}`
    }
    used.add(name)
    entries[name] = new Uint8Array(await item.blob.arrayBuffer())
  }
  return new Blob([zipSync(entries, { level: 0 }) as BlobPart], { type: 'application/zip' })
}
