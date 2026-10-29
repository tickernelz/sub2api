import { apiClient } from '../client'

export interface PerchAuthUrlRequest {
  provider?: 'google' | 'github'
}

export interface PerchAuthUrlResponse {
  auth_url: string
  session_id: string
  provider: string
  providers: string[]
  app_url: string
  redirect_uri: string
  expires_in: number
}

export interface PerchExchangeCodeRequest {
  session_id: string
  callback_url?: string
  code?: string
}

export interface PerchExchangeCodeResponse {
  access_token: string
  refresh_token: string
  app_url: string
  expires_at: number
  user_id: string
  email: string
}

export async function generateAuthUrl(payload: PerchAuthUrlRequest): Promise<PerchAuthUrlResponse> {
  const { data } = await apiClient.post<PerchAuthUrlResponse>('/admin/perch/auth-url', payload)
  return data
}

export async function exchangeCode(
  payload: PerchExchangeCodeRequest
): Promise<PerchExchangeCodeResponse> {
  const { data } = await apiClient.post<PerchExchangeCodeResponse>(
    '/admin/perch/exchange-code',
    payload
  )
  return data
}

export default { generateAuthUrl, exchangeCode }
