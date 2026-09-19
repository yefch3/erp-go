// 分配角色前的两道闸（2026-09-18 事故之后加的）。
//
// 那天有人在员工页勾了四个人 → 批量分配角色 → 模式选了「替换」→ 一个角色
// 都没勾就点了保存。"替换成什么都没有"在当时的代码里就是清空：四个销售
// 的角色全没了，登录后只剩待办和邮件。逻辑上系统"照做了"，但没有人会真
// 的想要这个结果。
//
// 规则写成纯函数，页面只负责问和拦：
// - 一个角色都没勾：不许保存（追加模式下本来就是空操作，替换模式下是清空）。
// - 替换模式：会拿掉这些人原有的全部角色，先确认。
// - 单人弹窗把角色全取消：偶尔是有意的（离职前收权），所以是确认不是禁止。

export type BatchRolesMode = 'append' | 'replace'

export type BatchRolesVerdict = 'pickOne' | 'confirmReplace' | 'ok'

export function batchRolesVerdict(mode: BatchRolesMode, picked: readonly string[]): BatchRolesVerdict {
  if (picked.length === 0) return 'pickOne'
  if (mode === 'replace') return 'confirmReplace'
  return 'ok'
}

export type SingleRolesVerdict = 'confirmClear' | 'ok'

export function singleRolesVerdict(picked: readonly string[]): SingleRolesVerdict {
  return picked.length === 0 ? 'confirmClear' : 'ok'
}
