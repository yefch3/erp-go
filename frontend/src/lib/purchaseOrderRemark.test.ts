import { describe, expect, it } from 'vitest'
import { createSSRApp, h } from 'vue'
import { renderToString } from 'vue/server-renderer'
import { createI18n } from 'vue-i18n'
import PurchaseOrderRemark from '../components/PurchaseOrderRemark.vue'
import zh from '../locales/zh'
import en from '../locales/en'
import es from '../locales/es'

const render = async (remark: string | undefined, locale = 'zh') => {
  const app = createSSRApp({ render: () => h(PurchaseOrderRemark, { remark }) })
  app.use(createI18n({ legacy: false, locale, messages: { zh, en, es } }))
  return renderToString(app)
}

describe('read-only purchase order remarks', () => {
  it('labels the source and preserves multiline plain text without interpreting HTML', async () => {
    const html = await render('按合同区分工厂\n<script>alert(1)</script> & 检查包装')
    expect(html).toContain('采购单备注')
    expect(html).toContain('按合同区分工厂\n&lt;script&gt;alert(1)&lt;/script&gt; &amp; 检查包装')
    expect(html).not.toContain('<script>')
    expect(html).not.toMatch(/<(input|textarea)\b/)
  })
  it('supports old responses and empty/whitespace remarks without inventing inspection requirements', async () => {
    for (const value of [undefined, '', ' \n ']) expect(await render(value)).toContain('未填写采购单备注')
  })
  it('provides source labels and empty states in English and Spanish', async () => {
    expect(await render(undefined, 'en')).toContain('No purchase order remarks')
    expect(await render(undefined, 'es')).toContain('Sin observaciones del pedido de compra')
  })
})
