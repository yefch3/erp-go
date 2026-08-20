// Element Plus 在用户取消或关闭确认框时会拒绝 Promise。
// 这两种结果属于正常交互，不应作为未处理错误显示在控制台。
export function isDialogDismissed(action: unknown): boolean {
  return action === 'cancel' || action === 'close'
}
