import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import VersionBadge from '../VersionBadge.vue'

const appStore = {
  versionLoading: false,
  currentVersion: '0.1.139',
  latestVersion: '0.1.140',
  hasUpdate: true,
  releaseInfo: {
    name: 'Sub2API 0.1.140',
    body: '',
    published_at: '2026-05-28T00:00:00Z',
    html_url: 'https://github.com/tickernelz/sub2api/releases/tag/v0.1.140'
  },
  buildType: 'docker',
  fetchVersion: vi.fn(),
  clearVersionCache: vi.fn()
}

vi.mock('@/stores', () => ({
  useAuthStore: () => ({ isAdmin: true }),
  useAppStore: () => appStore
}))

vi.mock('@/api/admin/system', () => ({
  performUpdate: vi.fn(),
  restartService: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'version.currentVersion': 'Current Version',
    'version.latestVersion': 'Latest Version',
    'version.upToDate': "You're running the latest version.",
    'version.updateAvailable': 'A new version is available!',
    'version.viewRelease': 'View Release',
    'version.refresh': 'Refresh',
    'version.dockerModeHint': 'Docker deployment, pull the latest image and recreate the container to update'
  }

  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key
    })
  }
})

describe('VersionBadge', () => {
  it('routes Docker deployments to the fork release without showing the self-update button', async () => {
    const wrapper = mount(VersionBadge, {
      props: { version: '0.1.139' },
      global: {
        stubs: {
          Icon: { template: '<span />' }
        }
      }
    })

    await wrapper.get('button').trigger('click')

    expect(wrapper.text()).toContain('Docker deployment, pull the latest image and recreate the container to update')
    expect(wrapper.text()).not.toContain('version.updateNow')
    expect(wrapper.find('a[href="https://github.com/tickernelz/sub2api/releases/tag/v0.1.140"]').exists()).toBe(true)
  })
})
