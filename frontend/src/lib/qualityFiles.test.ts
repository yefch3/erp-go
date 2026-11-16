import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import { qualityFileRound } from './qualityFiles'

const rounds = [{ id: '203', roundNo: 2 }, { id: '99', roundNo: 1 }, { id: '600', roundNo: 3 }]
const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh } })
const label = (id?: string | number) => {
  const result = qualityFileRound(id, rounds)
  return i18n.global.t(result.key, result.params || {})
}

describe('inspection attachment round identity', () => {
  it('keeps attachments uploaded before a saved round at task level', () => {
    expect(label('0')).toBe('整单资料')
    expect(label(undefined)).toBe('整单资料')
  })
  it('identifies the initial inspection from the persisted round ID', () => {
    expect(label('99')).toBe('首检')
    expect(label(99)).toBe('首检')
  })
  it('numbers reinspections independently of round IDs and array order', () => {
    expect(label('203')).toBe('第 1 次复检')
    expect(label('600')).toBe('第 2 次复检')
  })
  it('does not assign an orphaned attachment to the latest inspection', () => {
    expect(label('404')).toBe('未找到对应轮次')
  })
  it('does not invent an inspection number from invalid metadata', () => {
    expect(qualityFileRound('99', [{ id: '99', roundNo: 0 }]).key).toBe('quality.fileRoundUnknown')
  })
})
