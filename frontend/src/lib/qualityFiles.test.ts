import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import { qualityFileRound, stageQualityFiles } from './qualityFiles'

describe('inspection attachment staging', () => {
  const file = (name: string, lastModified = 1) => ({ name, size: 10, type: 'image/png', lastModified }) as File
  it('appends multiple batches without changing existing categories or retry keys', () => {
    const photos = stageQualityFiles([], [file('a.png'), file('b.png')], 'PHOTO')
    photos[0].fileKey = 'uploaded-but-not-registered'
    const pending = stageQualityFiles(photos, [file('report.pdf'), file('details.pdf')], 'REPORT')
    expect(pending.map(v => [v.file.name, v.category])).toEqual([
      ['a.png', 'PHOTO'], ['b.png', 'PHOTO'], ['report.pdf', 'REPORT'], ['details.pdf', 'REPORT'],
    ])
    expect(pending[0].fileKey).toBe('uploaded-but-not-registered')
  })
  it('ignores the same file staged again in the same category, not new versions or categories', () => {
    const original = stageQualityFiles([], [file('a.png')], 'PHOTO')
    expect(stageQualityFiles(original, [file('a.png'), file('a.png', 2)], 'PHOTO')).toHaveLength(2)
    expect(stageQualityFiles(original, [file('a.png')], 'OTHER')).toHaveLength(2)
  })
})

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
