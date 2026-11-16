import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import { qualityStatusKey } from './qualityStatus'

const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh } })

describe('inspection task status presentation', () => {
  it.each([
    ['WAITING', '待质检'],
    ['IN_PROGRESS', '质检中'],
    ['REINSPECTION', '待复检'],
    ['COMPLETED', '已完成'],
  ])('displays the actual %s task state', (status, label) => {
    expect(i18n.global.t(qualityStatusKey(status)!)).toBe(label)
  })

  it('does not mislabel missing or unknown statuses as waiting', () => {
    expect(qualityStatusKey(undefined)).toBeUndefined()
    expect(qualityStatusKey('')).toBeUndefined()
    expect(qualityStatusKey('UNKNOWN')).toBeUndefined()
  })
})
