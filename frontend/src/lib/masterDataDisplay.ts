type Translate = (key: string, params?: Record<string, unknown>) => string

const BUSINESS_ROLE_CODES = new Set([
  'GENERAL', 'CARRIER', 'FORWARDER', 'CUSTOMS_BROKER', 'WAREHOUSE', 'SERVICE',
])

const CERTIFICATE_STATUS_CODES = new Set(['VALID', 'EXPIRING', 'EXPIRED', 'SUSPENDED'])
const CHANGE_SECTION_CODES = new Set(['PROFILE', 'CONTACT', 'OWNER', 'CAPABILITY', 'CERTIFICATE'])

const CHANGE_MESSAGE_KEYS: Record<string, string> = {
  'supplier:CREATE:PROFILE': 'supplierCreated',
  'factory:CREATE:PROFILE': 'factoryCreated',
  'supplier:IMPORT:PROFILE': 'supplierImported',
  'factory:IMPORT:PROFILE': 'factoryImported',
  'supplier:UPDATE:PROFILE': 'supplierProfileUpdated',
  'factory:UPDATE:PROFILE': 'factoryProfileUpdated',
  'supplier:DEACTIVATE:PROFILE': 'supplierDeactivated',
  'factory:CASCADE:PROFILE': 'factorySuspendedBySupplier',
  'supplier:CREATE:CONTACT': 'contactAdded',
  'factory:CREATE:CONTACT': 'contactAdded',
  'supplier:UPDATE:CONTACT': 'contactUpdated',
  'factory:UPDATE:CONTACT': 'contactUpdated',
  'supplier:DEACTIVATE:CONTACT': 'contactRemoved',
  'factory:DEACTIVATE:CONTACT': 'contactRemoved',
  'supplier:CREATE:OWNER': 'ownerAdded',
  'factory:CREATE:OWNER': 'ownerAdded',
  'supplier:UPDATE:OWNER': 'ownerUpdated',
  'factory:UPDATE:OWNER': 'ownerUpdated',
  'supplier:DEACTIVATE:OWNER': 'ownerRemoved',
  'factory:DEACTIVATE:OWNER': 'ownerRemoved',
  'factory:CREATE:CAPABILITY': 'capabilityAdded',
  'factory:DELETE:CAPABILITY': 'capabilityRemoved',
  'factory:CREATE:CERTIFICATE': 'certificateAdded',
  'factory:DELETE:CERTIFICATE': 'certificateRemoved',
}

export function businessRoleLabel(code: string, t: Translate, fallback = ''): string {
  return BUSINESS_ROLE_CODES.has(code) ? t(`suppliers.businessRoles.${code}`) : fallback || code
}

export function certificateStatusLabel(code: string, t: Translate): string {
  return CERTIFICATE_STATUS_CODES.has(code) ? t(`suppliers.certificateStatus.${code}`) : code
}

export function changeSectionLabel(code: string, t: Translate): string {
  return CHANGE_SECTION_CODES.has(code) ? t(`suppliers.changeSections.${code}`) : code
}

export function changeSummaryLabel(
  change: { action?: string; section?: string; summary?: string },
  entity: 'supplier' | 'factory',
  t: Translate,
): string {
  const key = CHANGE_MESSAGE_KEYS[`${entity}:${change.action || ''}:${change.section || ''}`]
  if (!key) return change.summary || t('suppliers.changeMessages.changed')

  // 后端摘要中的冒号后面可能是员工、联系人或产品类别名称；它属于业务数据，应原样保留。
  const detail = (change.summary || '').match(/[：:](.+)$/)?.[1]?.trim()
  const message = t(`suppliers.changeMessages.${key}`)
  return detail ? `${message}: ${detail}` : message
}

export function operatorDisplayName(name: string, t: Translate): string {
  return ['系统管理员', 'System Administrator'].includes((name || '').trim())
    ? t('suppliers.systemAdministrator')
    : name
}
