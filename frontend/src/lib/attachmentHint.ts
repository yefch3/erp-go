// 一个附件那一行该说什么。
//
// 四种说法，不能合成三种，而且区别都在「我们凭什么这么说」上：
//
//   downloadFile     有下载地址，正常。
//   fileUnavailable  留过底，但这会儿签不出地址 —— 刷新可能就好了。
//   fileTooLarge     太大，从来没留底，**但原件还在**，有路可走。
//   fileGone         没留底，也没有别的话可说。
//
// 前两句原本是同一条消息，结果在文件其实好端端、只是某个过期服务没返回那个
// 字段的时候，告诉了人家他那份 8MB 的材料没有留底。对别人的数据这么笃定的
// 一句话，是要有依据才配说的。
//
// 第三句是同一个道理再走一遍：「没留底」和「太大了没留底」对读的人是两件
// 事——前者他无能为力，后者他能用「转发为附件」把原件取回来。以前这种附件
// 根本走不到这里：它被截成正好 25 MB 存了下来，看着像个好文件，下下来打不开。
//
// 返回的是 key 不是句子：这样这段判断可以离开 i18n 单独测，而它值得单独测——
// 说错了就是对客户的数据下了一个没有依据的断言。

/** 收信端单个附件的上限，和服务端 mimeparse.go 的 maxAttachmentBytes 一致。 */
export const INBOUND_ATTACHMENT_LIMIT = 25 * 1024 * 1024

export type AttachmentHintKey =
  | 'downloadFile'
  | 'fileUnavailable'
  | 'fileTooLarge'
  | 'fileGone'

export interface AttachmentLike {
  downloadUrl?: string
  /** 有没有留过底。服务端由 file_key 是否为空推出来。 */
  stored?: boolean
  fileSize?: number | string
}

export function attachmentHintKey(a: AttachmentLike): AttachmentHintKey {
  if (a.downloadUrl) return 'downloadFile'
  if (a.stored) return 'fileUnavailable'
  if (Number(a.fileSize) > INBOUND_ATTACHMENT_LIMIT) return 'fileTooLarge'
  return 'fileGone'
}
