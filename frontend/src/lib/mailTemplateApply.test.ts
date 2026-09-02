import { describe, expect, it } from 'vitest'
import { applyTemplateToBody, joinBody, type AppliedTemplate } from './mailTemplateApply'

const tplA = { templateId: 'a', content: '<p>Dear Hiring Manager,</p><p>Here is my resume.</p>' }
const tplB = { templateId: 'b', content: '<p>Looking forward to hearing from you.</p>' }

function apply(body: string, last: AppliedTemplate | null, tpl: typeof tplA, format = 'HTML') {
  return applyTemplateToBody({ body, format, isBlank: body.trim() === '', last, ...tpl })
}

describe('套用模板', () => {
  it('空正文：直接放进去', () => {
    const r = apply('', null, tplA)
    expect(r.kind).toBe('filled')
    expect(r.body).toBe(tplA.content)
  })

  it('同一个模板点两次，正文只有一份——这条钉的是用户撞上的那个 bug', () => {
    const first = apply('', null, tplA)
    const second = apply(first.body, first.last, tplA)
    expect(second.kind).toBe('unchanged')
    expect(second.body).toBe(first.body)
    // 点十次也一样。
    let r = second
    for (let i = 0; i < 10; i++) r = apply(r.body, r.last, tplA)
    expect(r.body).toBe(first.body)
  })

  it('换一个模板：换掉上一个，不是叠上去', () => {
    const first = apply('', null, tplA)
    const r = apply(first.body, first.last, tplB)
    expect(r.kind).toBe('replaced')
    expect(r.body).toBe(tplB.content)
    expect(r.body).not.toContain('resume')
  })

  it('自己先写了东西，再套模板：接在后面，不覆盖', () => {
    const r = apply('<p>Hi Tom,</p>', null, tplA)
    expect(r.kind).toBe('appended')
    expect(r.body).toBe(`<p>Hi Tom,</p><br>${tplA.content}`)
    // 再点一次同一个模板：还是只有一份。
    const again = apply(r.body, r.last, tplA)
    expect(again.kind).toBe('unchanged')
    expect(again.body).toBe(r.body)
  })

  it('先写、套 A、再换 B：自己写的留着，A 换成 B', () => {
    const a = apply('<p>Hi Tom,</p>', null, tplA)
    const b = apply(a.body, a.last, tplB)
    expect(b.body).toBe(`<p>Hi Tom,</p><br>${tplB.content}`)
  })

  it('套完模板又改了几个字：模板部分已经是人的了，再套同一个就追加', () => {
    const first = apply('', null, tplA)
    const edited = first.body.replace('Hiring Manager', 'Ms. Chen')
    const r = apply(edited, first.last, tplA)
    expect(r.kind).toBe('appended')
    expect(r.body).toBe(`${edited}<br>${tplA.content}`)
  })

  it('纯文本用空行接，富文本用换行接', () => {
    expect(joinBody('TEXT', 'a', 'b')).toBe('a\n\nb')
    expect(joinBody('HTML', 'a', 'b')).toBe('a<br>b')
    expect(joinBody('HTML', '', 'b')).toBe('b')
  })

  it('上一次的记录是别的正文留下的（新写一封）：不认它，按空正文填', () => {
    const stale: AppliedTemplate = { id: 'a', inserted: 'old', before: 'x' }
    const r = apply('', stale, tplA)
    expect(r.kind).toBe('filled')
    expect(r.body).toBe(tplA.content)
  })
})
