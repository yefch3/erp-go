// The mail service adds this fragment to signed URLs for MIME-embedded
// pictures. Fragments never reach object storage, but let the reader map the
// rendered image back to the owner-scoped attachment accepted by the Excel
// endpoint.
export const embeddedAttachmentFragment = 'erp-mail-attachment='

export function embeddedAttachmentID(src: string): string {
  const hash = src.split('#', 2)[1] || ''
  if (!hash.startsWith(embeddedAttachmentFragment)) return ''
  const id = hash.slice(embeddedAttachmentFragment.length)
  return /^\d+$/.test(id) && id !== '0' ? id : ''
}
