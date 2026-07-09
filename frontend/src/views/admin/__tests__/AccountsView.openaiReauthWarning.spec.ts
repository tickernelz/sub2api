import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountActionMenu from '@/components/admin/account/AccountActionMenu.vue'
import AccountsView from '../AccountsView.vue'
import type { Account } from '@/types'

const {
  listAccounts,
  listWithEtag,
  getBatchTodayStats,
  getAllProxies,
  getAllGroups
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchTodayStats,
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn()
    },
    proxies: {
      getAll: getAllProxies
    },
    groups: {
      getAll: getAllGroups
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    token: 'test-token',
    isSimpleMode: false
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const reauthRequiredAccount: Account = {
  id: 42,
  name: 'codex-openai-oauth',
  platform: 'openai',
  type: 'oauth',
  credentials: {},
  credentials_status: { has_access_token: true, has_refresh_token: true },
  extra: {
    openai_requires_reauth: true,
    openai_refresh_token_status: 'reused',
    openai_refresh_token_reused_at: '2026-06-06T02:30:00Z'
  },
  proxy_id: null,
  concurrency: 1,
  priority: 1,
  status: 'active',
  error_message: null,
  last_used_at: null,
  expires_at: null,
  auto_pause_on_expired: false,
  created_at: '2026-06-06T00:00:00Z',
  updated_at: '2026-06-06T00:00:00Z',
  schedulable: true,
  rate_limited_at: null,
  rate_limit_reset_at: null,
  overload_until: null,
  temp_unschedulable_until: null,
  temp_unschedulable_reason: null,
  session_window_start: null,
  session_window_end: null,
  session_window_status: null
}

const DataTableStub = {
  props: ['data'],
  template: `
    <div data-test="data-table">
      <div v-for="row in data" :key="row.id" data-test="platform-cell">
        <slot name="cell-platform_type" :row="row" />
      </div>
    </div>
  `
}

function mountAccountsView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        Pagination: true,
        ConfirmDialog: true,
        AccountTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
        AccountTableFilters: { template: '<div></div>' },
        AccountBulkActionsBar: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        HelpTooltip: true,
        Icon: true
      }
    }
  })
}

describe('admin AccountsView OpenAI OAuth reauth warning', () => {
  beforeEach(() => {
    localStorage.clear()
    listAccounts.mockReset()
    listWithEtag.mockReset()
    getBatchTodayStats.mockReset()
    getAllProxies.mockReset()
    getAllGroups.mockReset()

    listAccounts.mockResolvedValue({
      items: [reauthRequiredAccount],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
    listWithEtag.mockResolvedValue({
      notModified: true,
      etag: null,
      data: null
    })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
  })

  it('shows a compact reauth-needed badge without marking the OpenAI account unschedulable', async () => {
    const wrapper = mountAccountsView()
    await flushPromises()

    const platformCell = wrapper.find('[data-test="platform-cell"]')
    expect(platformCell.exists()).toBe(true)
    expect(platformCell.text()).toContain('admin.accounts.openai.refreshTokenReauthRequired')
    expect(platformCell.text()).toContain('admin.accounts.openai.refreshTokenStillSchedulable')
  })

  it('shows the reauth reason at the top of the account action menu', () => {
    const wrapper = mount(AccountActionMenu, {
      props: {
        show: true,
        account: reauthRequiredAccount,
        position: { top: 10, left: 10 }
      },
      global: {
        stubs: {
          Teleport: true,
          Icon: true
        }
      }
    })

    expect(wrapper.text()).toContain('admin.accounts.openai.refreshTokenReauthRequired')
    expect(wrapper.text()).toContain('admin.accounts.openai.refreshTokenReauthActionHint')
  })
})
