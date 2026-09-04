// Преобразования между ISO-временем (UTC, с сервера) и значением
// <input type="datetime-local"> (наивное локальное «YYYY-MM-DDTHH:MM»).

export function isoToLocalInput(iso: string): string {
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function localInputToIso(local: string): string {
  return new Date(local).toISOString()
}

/** Короткое отображение времени напоминания. */
export function fmtRemind(iso: string): string {
  return new Date(iso).toLocaleString('ru-RU', {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}
