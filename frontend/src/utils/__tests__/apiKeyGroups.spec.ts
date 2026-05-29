import { describe, expect, it } from 'vitest'
import type { ApiKey } from '@/types'
import {
  apiKeyAssignedGroupIds,
  defaultGroupForAssignedGroups,
  displayGroupForApiKey,
  groupSelectorOptionsForApiKey,
  payloadForDefaultGroupChange
} from '../apiKeyGroups'

const key = (overrides: Partial<ApiKey>): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test',
  name: 'test',
  group_id: null,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '',
  updated_at: '',
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
  ...overrides
})

describe('api key group assignment helpers', () => {
  it('uses group_ids when available and falls back to the legacy default group', () => {
    expect(apiKeyAssignedGroupIds(key({ group_id: 7 }))).toEqual([7])
    expect(apiKeyAssignedGroupIds(key({ group_id: 7, group_ids: [2, 7, 2] }))).toEqual([2, 7])
  })

  it('keeps the current default only when it is still assigned', () => {
    expect(defaultGroupForAssignedGroups(2, [5, 2])).toBe(2)
    expect(defaultGroupForAssignedGroups(9, [5, 2])).toBe(5)
    expect(defaultGroupForAssignedGroups(null, [])).toBeNull()
  })

  it('changes the default group without replacing existing assignments', () => {
    expect(payloadForDefaultGroupChange(key({ group_id: 2, group_ids: [2, 5] }), 5)).toEqual({
      group_id: 5,
      group_ids: [5, 2]
    })
    expect(payloadForDefaultGroupChange(key({ group_id: 2, group_ids: [2, 5] }), 7)).toEqual({
      group_id: 2,
      group_ids: [2, 5]
    })
  })

  it('does not re-add a stale legacy default group when assignments exist', () => {
    expect(payloadForDefaultGroupChange(key({ group_id: 9, group_ids: [5, 2] }), 5)).toEqual({
      group_id: 5,
      group_ids: [5, 2]
    })
  })

  it('keeps quick selector constrained to assigned groups', () => {
    const options = [
      { value: 2, label: 'two' },
      { value: 5, label: 'five' },
      { value: 7, label: 'seven' }
    ]

    expect(groupSelectorOptionsForApiKey(key({ group_id: null }), options)).toEqual([])
    expect(groupSelectorOptionsForApiKey(key({ group_id: 2, group_ids: [2, 5] }), options)).toEqual([
      { value: 2, label: 'two' },
      { value: 5, label: 'five' }
    ])
  })

  it('resolves display group from assigned groups when legacy group is stale', () => {
    expect(displayGroupForApiKey(key({
      group_id: 9,
      group_ids: [5, 2],
      groups: [
        { id: 5, name: 'five', platform: 'anthropic', subscription_type: 'standard' },
        { id: 2, name: 'two', platform: 'openai', subscription_type: 'standard' }
      ]
    }))).toMatchObject({ id: 5, name: 'five' })
  })
})
