import { describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { mount } from '@vue/test-utils'

const { createAccountMock } = vi.hoisted(() => ({
  createAccountMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
    showWarning: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isSimpleMode: true
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false })
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue({})
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, _params?: unknown, fallback?: string) => fallback || key
    })
  }
})

import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: {
      type: Boolean,
      default: false
    }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: {
    platform: {
      type: String,
      default: ''
    },
    showAccessTokenOption: {
      type: Boolean,
      default: false
    }
  },
  emits: ['import-access-token'],
  template: `
    <div data-testid="oauth-flow" :data-platform="platform" :data-show-access-token="String(showAccessTokenOption)">
      <button type="button" data-testid="import-access-token" @click="$emit('import-access-token', 'user-123::cursor-access-token')">import</button>
    </div>
  `,
  setup() {
    return {
      inputMethod: 'access_token',
      authCode: '',
      oauthState: '',
      oauthCallbackPath: '',
      oauthLoginOption: '',
      projectId: '',
      reset: vi.fn()
    }
  }
})

function mountModal() {
  return mount(CreateAccountModal, {
    props: {
      show: true,
      proxies: [],
      groups: []
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub,
        ConfirmDialog: true,
        Icon: true,
        ProxySelector: true,
        GroupSelector: true,
        ModelWhitelistSelector: true,
        QuotaLimitCard: true,
        Select: true
      }
    }
  })
}

describe('CreateAccountModal Cursor account flow', () => {
  it('shows Cursor as a first-class platform in add account', () => {
    const wrapper = mountModal()

    expect(wrapper.text()).toContain('Cursor')
  })

  it('creates a Cursor OAuth account from an imported access token', async () => {
    createAccountMock.mockReset()
    createAccountMock.mockResolvedValue({ id: 101 })

    const wrapper = mountModal()
    await wrapper.get('[data-testid="account-platform-cursor"]').trigger('click')
    await wrapper.get('input[data-tour="account-form-name"]').setValue('Cursor Account')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await nextTick()

    const oauthFlow = wrapper.get('[data-testid="oauth-flow"]')
    expect(oauthFlow.attributes('data-platform')).toBe('cursor')
    expect(oauthFlow.attributes('data-show-access-token')).toBe('true')

    await wrapper.get('[data-testid="import-access-token"]').trigger('click')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({
      name: 'Cursor Account',
      platform: 'cursor',
      type: 'oauth',
      credentials: {
        access_token: 'user-123::cursor-access-token'
      }
    })
  })
})
