import { describe, expect, it } from 'vitest'
import { folderNameProblem, isCustomFolderKey, viewForFolderKey } from './mailFolders'

const fixed = { inbox: 'INBOX', starred: 'STARRED', archive: 'ARCHIVE', junk: 'JUNK', trash: 'TRASH' }

describe('自建文件夹的 key', () => {
  it('F: 开头且有名字才算', () => {
    expect(isCustomFolderKey('F:供应商')).toBe(true)
    expect(isCustomFolderKey('F:')).toBe(false)
    expect(isCustomFolderKey('inbox')).toBe(false)
    expect(isCustomFolderKey('')).toBe(false)
  })

  it('固定文件夹走映射，自建的原样传，认不得的回收件箱（和后端一致）', () => {
    expect(viewForFolderKey('starred', fixed)).toBe('STARRED')
    expect(viewForFolderKey('F:供应商', fixed)).toBe('F:供应商')
    expect(viewForFolderKey('nonsense', fixed)).toBe('INBOX')
    expect(viewForFolderKey('F:', fixed)).toBe('INBOX')
  })
})

describe('文件夹名的校验（和后端同一套规矩）', () => {
  it('合法的', () => {
    for (const n of ['供应商', '  客户 2026  ', 'Project-A', '订单.待付款']) {
      expect(folderNameProblem(n)).toBe('')
    }
  })
  it('不合法的各有各的原因', () => {
    expect(folderNameProblem('')).toBe('empty')
    expect(folderNameProblem('   ')).toBe('empty')
    expect(folderNameProblem('客户/ACME')).toBe('badChars')
    expect(folderNameProblem('a*b')).toBe('badChars')
    expect(folderNameProblem('a\tb')).toBe('badChars')
    expect(folderNameProblem('inbox')).toBe('reserved')
    // 斜杠先被拦（和后端 validFolderName 的顺序一致），保留名单独试
    expect(folderNameProblem('[Gmail]/x')).toBe('badChars')
    expect(folderNameProblem('[Gmail]All Mail')).toBe('reserved')
    expect(folderNameProblem('长'.repeat(121))).toBe('tooLong')
  })
})
