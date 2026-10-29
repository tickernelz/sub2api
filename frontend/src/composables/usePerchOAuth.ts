import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { exchangeCode as requestExchangeCode, generateAuthUrl as requestAuthUrl } from '@/api/admin/perch'
import { extractApiErrorMessage } from '@/utils/apiError'

export interface PerchTokenInfo {
  access_token: string
  refresh_token: string
  app_url: string
  expires_at: number
  user_id: string
  email: string
}

export type PerchProvider = 'google' | 'github'

export function usePerchOAuth() {
  const appStore = useAppStore()
  const { t } = useI18n()

  const authUrl = ref('')
  const sessionId = ref('')
  const provider = ref<PerchProvider>('google')
  const redirectUri = ref('')
  const loading = ref(false)
  const error = ref('')

  const resetState = () => {
    authUrl.value = ''
    sessionId.value = ''
    provider.value = 'google'
    redirectUri.value = ''
    loading.value = false
    error.value = ''
  }

  const generateAuthUrl = async (chosen: PerchProvider = 'google'): Promise<boolean> => {
    loading.value = true
    authUrl.value = ''
    sessionId.value = ''
    error.value = ''
    provider.value = chosen
    try {
      const response = await requestAuthUrl({ provider: chosen })
      authUrl.value = response.auth_url
      sessionId.value = response.session_id
      redirectUri.value = response.redirect_uri
      return true
    } catch (err) {
      error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.perch.failedToGenerateUrl'))
      appStore.showError(error.value)
      return false
    } finally {
      loading.value = false
    }
  }

  const exchangeAuthCode = async (codeOrUrl: string): Promise<PerchTokenInfo | null> => {
    if (!codeOrUrl.trim() || !sessionId.value) {
      error.value = t('admin.accounts.oauth.perch.missingCode')
      appStore.showError(error.value)
      return null
    }
    loading.value = true
    error.value = ''
    try {
      const tokenInfo = await requestExchangeCode({
        session_id: sessionId.value,
        callback_url: codeOrUrl.trim()
      })
      return tokenInfo
    } catch (err) {
      error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.perch.failedToExchange'))
      appStore.showError(error.value)
      return null
    } finally {
      loading.value = false
    }
  }

  const buildCredentials = (info: PerchTokenInfo): Record<string, unknown> => {
    const credentials: Record<string, unknown> = { access_token: info.access_token }
    if (info.refresh_token) credentials.refresh_token = info.refresh_token
    credentials.app_url = info.app_url
    if (info.expires_at > 0) credentials.expires_at = info.expires_at
    return credentials
  }

  return {
    authUrl,
    sessionId,
    provider,
    redirectUri,
    loading,
    error,
    resetState,
    generateAuthUrl,
    exchangeAuthCode,
    buildCredentials
  }
}
