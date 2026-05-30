import { describe, expect, it } from 'vitest'

import { platformBadgeClass, platformLabel } from '../platformColors'

describe('platformColors', () => {
  it('recognizes OpenCode as a first-class platform', () => {
    expect(platformLabel('opencode')).toBe('OpenCode')
    expect(platformBadgeClass('opencode')).toContain('cyan')
  })
})
