import { describe, expect, it } from 'vitest'

import { embeddedAttachmentID } from './mailExcel'

describe('embeddedAttachmentID', () => {
  it('recovers the attachment id from a signed image URL', () => {
    expect(embeddedAttachmentID(
      'http://localhost:19000/erp-files/mail/image.png?X-Amz-Signature=abc#erp-mail-attachment=2020',
    )).toBe('2020')
  })

  it('does not mistake an ordinary image or malformed marker for an attachment', () => {
    expect(embeddedAttachmentID('https://example.com/image.png')).toBe('')
    expect(embeddedAttachmentID('https://example.com/image.png#erp-mail-attachment=0')).toBe('')
    expect(embeddedAttachmentID('https://example.com/image.png#erp-mail-attachment=1x')).toBe('')
  })
})
