// 右键「转 Excel」要发给服务器的「哪封信、哪个附件」。

export interface AttachmentExcelSource {
  kind: 'attachment'
  mailId: string
  attachmentId: string
}

/**
 * mailId 必须是**附件自己所属的那封信**，不是当前打开的那封。
 *
 * 会话视图里一屏列着一个往来的好几封，每封都带附件。2026-09-15 之前这里用的
 * 是打开的那封的号，于是对下面任何一封回信里的附件点转 Excel，服务器都去最早
 * 那封里找、找不到，回「附件不存在或未保存」——附件明明好好的，是页面报错了
 * 它属于哪封信。
 *
 * 发出去的信的附件不支持转换（它们没有 mailId），返回 null，调用方不弹菜单。
 */
export function attachmentExcelSource(mailId: string, attachmentId: string): AttachmentExcelSource | null {
  if (!mailId) return null
  return { kind: 'attachment', mailId, attachmentId }
}
