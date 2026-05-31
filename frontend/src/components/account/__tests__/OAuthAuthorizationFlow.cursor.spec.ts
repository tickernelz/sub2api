import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, _params?: unknown, fallback?: string) => fallback || key
    })
  }
})

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copied: { value: false }, copyToClipboard: vi.fn() })
}))

import OAuthAuthorizationFlow from '../OAuthAuthorizationFlow.vue'

describe('OAuthAuthorizationFlow access token import', () => {
  it('emits imported access token when access token method is selected', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'cursor',
        showCookieOption: false,
        showAccessTokenOption: true
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    await wrapper.get('input[value="access_token"]').setValue()
    const input = wrapper.get('[data-testid="access-token-input"]')
    await input.setValue('user-123::cursor-access-token')
    await wrapper.get('[data-testid="import-access-token-button"]').trigger('click')

    expect(wrapper.emitted('import-access-token')?.[0]).toEqual(['user-123::cursor-access-token'])
  })

  it('keeps Cursor on access-token import after reset instead of falling back to manual OAuth code flow', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'cursor',
        showCookieOption: false,
        showAccessTokenOption: true
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.find('[data-testid="access-token-input"]').exists()).toBe(true)

    ;(wrapper.vm as unknown as { reset: () => void }).reset()
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="access-token-input"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('admin.accounts.oauth.cursor.step1GenerateUrl')
    expect(wrapper.find('input[value="manual"]').exists()).toBe(false)
  })
})
