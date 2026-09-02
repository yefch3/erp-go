// 「套用模板」怎么改正文。
//
// 纯逻辑放这里，是为了把三条规矩钉在测试里：**同一个模板点几次结果都一样**
// （用的人点了两次，正文里出现了两遍）；换一个模板是换掉上一个而不是叠上去；
// 自己写过的东西永远不被模板覆盖。
//
// 判断「上一个模板还在不在」靠的是一次字符串比对：正文是否仍等于「套模板之前
// 的正文 + 模板内容」。人只要在中间改过一个字，比对就不成立，那时按追加处理
// ——这正是想要的：改过的东西是人的，不是模板的。

export interface AppliedTemplate {
  id: string
  // 套进去的那段（已经换成正文当时的格式）。
  inserted: string
  // 套之前正文是什么。
  before: string
}

export type ApplyKind = 'filled' | 'appended' | 'replaced' | 'unchanged'

export interface ApplyResult {
  body: string
  last: AppliedTemplate
  kind: ApplyKind
}

// 正文和模板怎么接：空的直接放；富文本隔一个换行，纯文本隔一个空行。
export function joinBody(format: string, before: string, content: string): string {
  if (!before) return content
  return format === 'HTML' ? `${before}<br>${content}` : `${before}\n\n${content}`
}

export function applyTemplateToBody(input: {
  body: string
  format: string
  isBlank: boolean
  templateId: string
  content: string
  last: AppliedTemplate | null
}): ApplyResult {
  const { body, format, isBlank, templateId, content, last } = input
  // 上一次套的还原封不动地在正文里。
  if (last && body === joinBody(format, last.before, last.inserted)) {
    if (last.id === templateId && last.inserted === content) {
      return { body, last, kind: 'unchanged' }
    }
    const next = { id: templateId, inserted: content, before: last.before }
    return { body: joinBody(format, last.before, content), last: next, kind: 'replaced' }
  }
  if (isBlank) {
    return { body: content, last: { id: templateId, inserted: content, before: '' }, kind: 'filled' }
  }
  // 追加，不覆盖：写了一半的话比任何模板都要紧。
  return {
    body: joinBody(format, body, content),
    last: { id: templateId, inserted: content, before: body },
    kind: 'appended',
  }
}
