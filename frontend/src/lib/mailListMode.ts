// 收件箱列表怎么排一行：一条会话一行，还是一封信一行。
//
// 我们从一开始是前者（Gmail 那一派）。263 和 Foxmail 默认是后者，用惯那边
// 的人看我们的列表会觉得信少了——「客户明明回了三次，怎么只有一行」。两种
// 都不是错的，所以这是个开关，跟人走。
//
// **这一档决定了一行代表什么，而一行代表什么决定了在它上面点删除会删掉多少
// 东西**：一条会话，还是一封信。所以它不能是前端自己记着的状态——服务端每
// 次回列表都把当前档位一起带回来（ListInboundResponse.listMode），前端只认
// 回包里的那个。同一个人开着两个标签页、在另一个标签页里改了档，这边下一次
// 拉列表就跟上了，不会出现「以为是合并档、其实服务端给的是单封档」然后一点
// 删除删掉整串。
//
// 和服务端 app.ListMode 是同一套词。

export type MailListMode = 'THREAD' | 'MESSAGE'

// 没设置过的人、以及换版本那几秒里还没带这个字段的旧服务端，都是这一档。
export const DEFAULT_LIST_MODE: MailListMode = 'THREAD'

// 认不出来的值退回默认档，不报错：这是个显示偏好，最坏的后果是看到一直以来
// 的样子。空串在这里是有意义的一档——旧服务端的回包里没有 listMode。
export function normalizeListMode(v: unknown): MailListMode {
  return v === 'MESSAGE' ? 'MESSAGE' : DEFAULT_LIST_MODE
}

// 这一档要不要把会话合起来。
//
// 三处用它，而且必须是同一个答案：列表上那个「共 N 封」的角标、点删除/归档
// 时传不传 wholeThread、点开一封之后底下那段往来显示不显示。三处各写一次
// `mode === 'THREAD'` 的话，漏掉任何一处都不报错——漏在第二处上是删多了信。
export function mergesThreads(mode: MailListMode): boolean {
  return mode !== 'MESSAGE'
}

// 菜单里点了一项，该不该真的去改——返回要切到的档位，null = 什么都不做。
//
// **这个函数存在的理由是一次真实的误会。** 原来菜单里只有一项，是个开关：
// 选中时画对钩，再点一下取消。2026-09-22 一位同事反映「勾上了还是显示会话」，
// 查日志发现他 01:34 开、01:36 又关了——他打开菜单看见对钩，为了"确认一下"
// 又点了一次，那一下正好把它关掉。
//
// 改成两项并列的单选之后，「点已经选中的那一项」必须是**什么也不发生**，
// 而不是取消。少了这一条，两项并列也救不了他：点中间那一项照样会翻。
//
// 认不出来的命令同样回 null：菜单里的字符串是写死的，能走到这里的别的值
// 只可能是有人手改了 DOM，那时按兵不动比猜一个档位安全。
export function listModeChange(current: MailListMode, command: string): MailListMode | null {
  if (command !== 'THREAD' && command !== 'MESSAGE') return null
  return command === current ? null : command
}
