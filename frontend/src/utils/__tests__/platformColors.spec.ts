import { describe, expect, it } from 'vitest'

import { platformBadgeClass, platformLabel } from '../platformColors'

describe('platformColors', () => {
  it('recognizes OpenCode as a first-class platform', () => {
    expect(platformLabel('opencode')).toBe('OpenCode')
    expect(platformBadgeClass('opencode')).toContain('cyan')
  })

  it('recognizes Cursor as a first-class platform', () => {
    expect(platformLabel('cursor')).toBe('Cursor')
    expect(platformBadgeClass('cursor')).toContain('indigo')
  })
})
