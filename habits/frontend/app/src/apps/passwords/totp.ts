// TOTP (RFC 6238): 6 цифр, окно 30 секунд, HMAC-SHA1 через WebCrypto.
// Секрет хранится в записи хранилища и, как и всё остальное, не покидает устройство.

const B32_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'

/** null — строка не является base32-секретом. */
export function base32Decode(input: string): Uint8Array | null {
  const clean = input.toUpperCase().replace(/[\s-]/g, '').replace(/=+$/, '')
  if (!clean || /[^A-Z2-7]/.test(clean)) return null
  let bits = 0
  let value = 0
  const out: number[] = []
  for (const ch of clean) {
    value = (value << 5) | B32_ALPHABET.indexOf(ch)
    bits += 5
    if (bits >= 8) {
      bits -= 8
      out.push((value >> bits) & 0xff)
    }
  }
  return out.length ? new Uint8Array(out) : null
}

/** Принимает base32-секрет или целиком otpauth:// URL, возвращает секрет. */
export function normalizeTotpSecret(raw: string): string | null {
  let secret = raw.trim()
  if (secret.toLowerCase().startsWith('otpauth://')) {
    try {
      secret = new URL(secret).searchParams.get('secret') ?? ''
    } catch {
      return null
    }
  }
  secret = secret.toUpperCase().replace(/[\s-]/g, '')
  return base32Decode(secret) ? secret : null
}

export async function totpCode(secret: string, now = Date.now()): Promise<string | null> {
  const keyBytes = base32Decode(secret)
  if (!keyBytes) return null
  const counter = Math.floor(now / 1000 / 30)
  const msg = new Uint8Array(8)
  new DataView(msg.buffer).setBigUint64(0, BigInt(counter))
  const key = await crypto.subtle.importKey(
    'raw',
    keyBytes as BufferSource,
    { name: 'HMAC', hash: 'SHA-1' },
    false,
    ['sign'],
  )
  const mac = new Uint8Array(await crypto.subtle.sign('HMAC', key, msg as BufferSource))
  const offset = mac[mac.length - 1] & 0x0f
  const code =
    (((mac[offset] & 0x7f) << 24) | (mac[offset + 1] << 16) | (mac[offset + 2] << 8) | mac[offset + 3]) % 1_000_000
  return String(code).padStart(6, '0')
}

/** Сколько секунд осталось до смены кода. */
export function totpSecondsLeft(now = Date.now()): number {
  return 30 - (Math.floor(now / 1000) % 30)
}
