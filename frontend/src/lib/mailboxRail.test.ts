import { describe, expect, it } from 'vitest'
import { mailboxRail, type CustomFolder, type FolderDef } from './mailFolders'

const fixed: FolderDef[] = ['inbox', 'starred', 'drafts', 'scheduled', 'sent', 'attention', 'archive', 'junk', 'trash', 'suppressions']
  .map((key) => ({ key, icon: key }))

function hf(name: string, role: string, id = 1): CustomFolder {
  return { id, accountId: 23, name, viewKey: role === 'CUSTOM' ? `F:${name}` : '', role }
}

describe('mailboxRail：一个信箱下面同一级别摆哪些行', () => {
  const host263 = [hf('INBOX', 'INBOX'), hf('已发送', 'SENT'), hf('已删除', 'TRASH'), hf('垃圾邮件', 'JUNK'),
    hf('已归档', 'ARCHIVE'), hf('草稿箱', 'DRAFTS'), hf('重要客户', 'CUSTOM', 7), hf('项目A', 'CUSTOM', 8)]

  it('固定视图在前、自建在后；拒收名单不跟信箱走', () => {
    const keys = mailboxRail(fixed, host263).map((r) => r.key)
    expect(keys).toEqual(['inbox', 'starred', 'drafts', 'scheduled', 'sent', 'attention', 'archive', 'junk', 'trash', 'F:项目A', 'F:重要客户'])
  })
  it('服务器没有归档文件夹（163 / 126 / QQ）就不显示「归档」', () => {
    const host163 = host263.filter((f) => f.role !== 'ARCHIVE')
    expect(mailboxRail(fixed, host163).map((r) => r.key)).not.toContain('archive')
  })
  it('服务器自带而 ERP 不认得的（病毒文件夹）和服务器草稿箱这一期不显示', () => {
    const keys = mailboxRail(fixed, [...host263, hf('病毒文件夹', 'SYSTEM')]).map((r) => r.key)
    expect(keys).not.toContain('F:病毒文件夹')
    expect(keys.filter((k) => k === 'drafts')).toHaveLength(1)
  })
  it('自建的能改名删除，固定的不能；固定行带服务器上的实际名字', () => {
    const rail = mailboxRail(fixed, host263)
    expect(rail.find((r) => r.key === 'trash')).toMatchObject({ custom: false, hostName: '已删除' })
    expect(rail.find((r) => r.key === 'F:项目A')).toMatchObject({ custom: true, name: '项目A' })
  })
  it('还没拉到服务器清单（空）时，固定视图照常、归档不显示', () => {
    expect(mailboxRail(fixed, []).map((r) => r.key)).toEqual(['inbox', 'starred', 'drafts', 'scheduled', 'sent', 'attention', 'junk', 'trash'])
  })
})
