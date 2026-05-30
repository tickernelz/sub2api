import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import GroupOptionItem from '../GroupOptionItem.vue'

describe('GroupOptionItem', () => {
  it('renders OpenCode rate pill with dedicated cyan styling', () => {
    const wrapper = mount(GroupOptionItem, {
      props: {
        name: 'opencode-default',
        platform: 'opencode',
        rateMultiplier: 1
      },
      global: {
        stubs: {
          GroupBadge: {
            props: ['name'],
            template: '<span>{{ name }}</span>'
          }
        }
      }
    })

    const ratePill = wrapper.find('span.bg-cyan-50')
    expect(ratePill.exists()).toBe(true)
    expect(ratePill.text()).toContain('1x')
    expect(ratePill.classes()).toContain('text-cyan-700')
  })
})
