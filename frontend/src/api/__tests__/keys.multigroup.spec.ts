import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn()
}))

vi.mock('../client', () => ({
  apiClient: mocks
}))

describe('keys API multi-group payloads', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.post.mockResolvedValue({ data: { id: 1, name: 'multi', key: 'sk-test' } })
    mocks.put.mockResolvedValue({ data: { id: 1, name: 'multi', key: 'sk-test' } })
  })

  it('sends assigned group ids when creating an API key', async () => {
    const { create } = await import('../keys')

    await create('multi', 2, undefined, undefined, undefined, undefined, undefined, undefined, [2, 5])

    expect(mocks.post).toHaveBeenCalledWith('/keys', {
      name: 'multi',
      group_id: 2,
      group_ids: [2, 5]
    })
  })

  it('preserves assigned group ids when changing only the default group', async () => {
    const { update } = await import('../keys')

    await update(10, { group_id: 5, group_ids: [2, 5] })

    expect(mocks.put).toHaveBeenCalledWith('/keys/10', {
      group_id: 5,
      group_ids: [2, 5]
    })
  })
})
