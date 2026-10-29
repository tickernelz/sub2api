export type PerchSessionFields = {
  accessToken: string
  refreshToken: string
  appUrl: string
  expiresAt: number
}

export const PERCH_DEFAULT_APP_URL = 'https://app.perchai.app'

const PERCH_TEXT_KEYS = ['accessToken', 'access_token', 'refreshToken', 'refresh_token', 'appUrl', 'app_url']

function base64UrlDecode(segment: string): string {
  const padded = segment.replace(/-/g, '+').replace(/_/g, '/')
  const padding = padded.length % 4 === 0 ? '' : '='.repeat(4 - (padded.length % 4))
  return atob(padded + padding)
}

export function perchExpirySeconds(token: string): number {
  const segments = token.trim().split('.')
  if (segments.length < 2) return 0
  try {
    const payload = JSON.parse(base64UrlDecode(segments[1])) as { exp?: unknown }
    const exp = Number(payload.exp)
    return Number.isFinite(exp) && exp > 0 ? Math.floor(exp) : 0
  } catch {
    return 0
  }
}

function pickText(source: Record<string, unknown>, camel: string, snake: string): string {
  for (const key of [camel, snake]) {
    const value = source[key]
    if (typeof value === 'string' && value.trim() !== '') return value.trim()
  }
  return ''
}

function candidateObjects(value: unknown, depth: number): Record<string, unknown>[] {
  if (depth < 0 || value === null || typeof value !== 'object' || Array.isArray(value)) return []
  const record = value as Record<string, unknown>
  const nested = Object.values(record).flatMap((entry) => candidateObjects(entry, depth - 1))
  return [record, ...nested]
}

export function parsePerchSession(raw: string): Partial<PerchSessionFields> {
  const text = raw.trim()
  if (!text) return {}
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return { accessToken: text }
  }
  for (const record of candidateObjects(parsed, 1)) {
    const accessToken = pickText(record, 'accessToken', 'access_token')
    if (!accessToken) continue
    const refreshToken = pickText(record, 'refreshToken', 'refresh_token')
    const appUrl = pickText(record, 'appUrl', 'app_url')
    const rawExpiry = record.expiresAt ?? record.expires_at
    const expiresAt = Number(rawExpiry)
    return {
      accessToken,
      refreshToken,
      appUrl,
      expiresAt: Number.isFinite(expiresAt) && expiresAt > 0 ? Math.floor(expiresAt) : perchExpirySeconds(accessToken)
    }
  }
  return {}
}

export function perchCredentials(fields: PerchSessionFields): Record<string, unknown> {
  const credentials: Record<string, unknown> = { access_token: fields.accessToken.trim() }
  const refreshToken = fields.refreshToken.trim()
  if (refreshToken) credentials.refresh_token = refreshToken
  credentials.app_url = fields.appUrl.trim() || PERCH_DEFAULT_APP_URL
  const expiresAt = fields.expiresAt > 0 ? fields.expiresAt : perchExpirySeconds(fields.accessToken)
  if (expiresAt > 0) credentials.expires_at = expiresAt
  return credentials
}

export function isPerchSessionText(value: string): boolean {
  const text = value.trim()
  if (!text || !PERCH_TEXT_KEYS.some((key) => text.includes(key))) return false
  return Object.keys(parsePerchSession(text)).length > 0
}
