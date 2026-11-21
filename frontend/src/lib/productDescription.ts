interface DescriptionField {
  key: string
  label: string
  value?: string | null
}

function inlineText(value?: string | null): string {
  return (value ?? '').split(/\r\n|\r|\n/).map(line => line.trim()).filter(Boolean).join('，')
}

export function formatProductDescription(name: string | null | undefined, fields: DescriptionField[]): string {
  const productName = inlineText(name)
  const parts = productName ? [productName] : []
  const seen = new Set<string>()
  for (const field of fields) {
    const value = inlineText(field.value)
    const key = JSON.stringify([field.key, value])
    if (!value || seen.has(key) || (field.key === 'specification' && value === productName)) continue
    seen.add(key)
    parts.push(`${field.label}：${value}`)
  }
  return parts.join('，') || '—'
}
