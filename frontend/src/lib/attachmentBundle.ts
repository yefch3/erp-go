/**
 * 给不给「下载全部」那颗按钮。
 *
 * 会话里每一封各有各的一颗——对方发来三个文件、我们回了两个，就是三个的那颗
 * 和两个的那颗，各打各的包。所以这个判断是**按信**问的，不是按页面问的。
 *
 * 两个条件缺一不可：
 *
 * · 两个以上文件。只有一个时它和旁边那颗「下载」是同一件事，多一颗只会让人挑。
 * · 知道是哪一封信。打包那条接口按信的编号取文件（/inbound-mails/<信>/
 *   attachments/download），没有编号就取不了。会话里「我发出」而本地又没留底
 *   的那几条就是这种——那时给了按钮，点下去只会 404。
 */
export function canBundleAttachments(fileCount: number, mailID: string): boolean {
  return fileCount > 1 && Boolean(mailID)
}
