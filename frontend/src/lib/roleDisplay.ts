type Locale = 'zh' | 'en' | 'es'

function normalizedLocale(locale: string): Locale {
  if (locale.toLowerCase().startsWith('es')) return 'es'
  if (locale.toLowerCase().startsWith('en')) return 'en'
  return 'zh'
}

const roleNames: Record<string, Record<Locale, string>> = {
  LOGISTICS: { zh: '物流管理', en: 'Logistics', es: 'Logística' },
  FINANCE: { zh: '财务', en: 'Finance', es: 'Finanzas' },
  SUPER_ADMIN: { zh: '超级管理员', en: 'Super administrator', es: 'Superadministrador' },
  BUYER: { zh: '采购专员', en: 'Buyer', es: 'Comprador' },
  PROCUREMENT_MANAGER: { zh: '采购经理', en: 'Procurement manager', es: 'Responsable de compras' },
  SALES: { zh: '销售专员', en: 'Sales representative', es: 'Representante de ventas' },
  SALES_MANAGER: { zh: '销售经理', en: 'Sales manager', es: 'Responsable de ventas' },
  SHIPPING_MANAGER: { zh: '船运经理', en: 'Shipping manager', es: 'Responsable de embarques' },
	QUALITY_INSPECTOR: { zh: '质检专员', en: 'Quality inspector', es: 'Inspector de calidad' },
}

const roleDescriptions: Record<string, Record<Locale, string>> = {
  LOGISTICS: {
    zh: '仓储与发运：入库、出库、采购收货、库存查询',
    en: 'Warehousing and dispatch: receipts, issues, purchase receipts and inventory lookup',
    es: 'Almacén y expedición: entradas, salidas, recepciones de compra y consulta de inventario',
  },
  FINANCE: {
    zh: '收款登记与合同核销、银行流水对账',
    en: 'Receipt entry, contract settlement and bank-statement reconciliation',
    es: 'Registro de cobros, liquidación de contratos y conciliación bancaria',
  },
  SUPER_ADMIN: {
    zh: '系统引导创建，持有全部权限',
    en: 'System-created role with every permission',
    es: 'Rol creado por el sistema con todos los permisos',
  },
  BUYER: {
    zh: '询价、匹配采购需求、维护采购单草稿并提交审批',
    en: 'Request quotes, match purchase requirements, maintain draft orders and submit them for approval',
    es: 'Solicitar cotizaciones, vincular necesidades, mantener borradores y enviarlos a aprobación',
  },
  PROCUREMENT_MANAGER: {
    zh: '管理采购询价、例外需求、采购单审批与取消',
    en: 'Manage sourcing, exceptional requirements, purchase-order approval and cancellation',
    es: 'Gestionar abastecimiento, necesidades excepcionales, aprobación y cancelación de órdenes',
  },
  SALES: {
    zh: '维护本人客户询盘、客户报价和外销合同',
    en: 'Maintain own customer inquiries, quotations and export contracts',
    es: 'Mantener sus consultas, cotizaciones y contratos de exportación',
  },
  SALES_MANAGER: {
    zh: '管理团队客户询盘、客户报价、合同审批和负责人转移',
    en: 'Manage team inquiries, quotations, contract approvals and ownership transfers',
    es: 'Gestionar consultas y cotizaciones del equipo, aprobaciones y cambios de responsable',
  },
  SHIPPING_MANAGER: {
    zh: '管理售前船运询价、主责人员和统一船运方案',
    en: 'Manage presales shipping inquiries, owners and consolidated shipping plans',
    es: 'Gestionar consultas de transporte, responsables y planes de envío consolidados',
  },
	QUALITY_INSPECTOR: {
		zh: '处理工厂出货前质检并保存每轮资料',
		en: 'Perform pre-shipment factory inspections and retain every round of evidence',
		es: 'Realizar inspecciones previas al envío y conservar la evidencia de cada ronda',
	},
}

const resources: Record<string, Record<'en' | 'es', string>> = {
  'approval:flow': { en: 'approval workflows', es: 'flujos de aprobación' },
  'approval:instance': { en: 'approval progress', es: 'progreso de aprobaciones' },
  'approval:task': { en: 'approval tasks', es: 'tareas de aprobación' },
  'export:contract': { en: 'contracts', es: 'contratos' },
  'export:ownership': { en: 'document ownership', es: 'responsables de documentos' },
  'export:quotation': { en: 'quotations', es: 'cotizaciones' },
  'export:receipt': { en: 'receipt reconciliation', es: 'conciliación de cobros' },
  'export:shipment': { en: 'shipments', es: 'expediciones' },
  'fx:rate': { en: 'exchange rates', es: 'tipos de cambio' },
  'iam:department': { en: 'departments', es: 'departamentos' },
  'iam:employee': { en: 'employees', es: 'empleados' },
  'iam:role': { en: 'roles', es: 'roles' },
  'inventory:stock': { en: 'inventory', es: 'inventario' },
  'mail:email': { en: 'email conversations', es: 'conversaciones de correo' },
  'mail:export': { en: 'export records', es: 'registros de exportación' },
  'mail:suppression': { en: 'suppression list', es: 'lista de exclusión' },
  'masterdata:customer': { en: 'customers', es: 'clientes' },
  'masterdata:factory': { en: 'factories', es: 'fábricas' },
  'masterdata:port': { en: 'ports', es: 'puertos' },
  'masterdata:supplier': { en: 'suppliers', es: 'proveedores' },
  'procurement:exception': { en: 'purchase receipt exceptions', es: 'incidencias de recepción' },
  'procurement:invoice': { en: 'supplier invoices', es: 'facturas de proveedores' },
  'procurement:order': { en: 'purchase orders', es: 'órdenes de compra' },
  'procurement:payment': { en: 'supplier payments', es: 'pagos a proveedores' },
  'procurement:production': { en: 'production progress', es: 'progreso de producción' },
  'procurement:receipt': { en: 'purchase receipts', es: 'recepciones de compra' },
  'procurement:recon': { en: 'supplier reconciliation', es: 'conciliación con proveedores' },
  'procurement:requirement': { en: 'purchase requirements', es: 'necesidades de compra' },
  'procurement:sourcing': { en: 'supplier sourcing', es: 'abastecimiento de proveedores' },
  'product:product': { en: 'products', es: 'productos' },
	'quality:task': { en: 'quality inspection tasks', es: 'tareas de inspección de calidad' },
	'quality:file': { en: 'quality inspection files', es: 'archivos de inspección de calidad' },
	'quality:release': { en: 'partial qualified release', es: 'liberación parcial conforme' },
  'sales:inquiry': { en: 'customer inquiries', es: 'consultas de clientes' },
  'sales:procurement-progress': { en: 'procurement progress', es: 'progreso de compras' },
  'shipping:document': { en: 'shipping documents', es: 'documentos de embarque' },
  'shipping:progress': { en: 'shipping progress', es: 'progreso de embarques' },
  'shipping:route': { en: 'shipping routes', es: 'rutas de embarque' },
  'shipping:schedule': { en: 'shipping schedules', es: 'programación de embarques' },
  'shipping:sourcing': { en: 'presales shipping sourcing', es: 'cotización logística de preventa' },
}

const actions: Record<string, Record<'en' | 'es', (resource: string) => string>> = {
  read: { en: (r) => `View ${r}`, es: (r) => `Ver ${r}` },
  write: { en: (r) => `Manage ${r}`, es: (r) => `Gestionar ${r}` },
  approve: { en: (r) => `Review and approve ${r}`, es: (r) => `Revisar y aprobar ${r}` },
  submit: { en: (r) => `Submit ${r}`, es: (r) => `Enviar ${r}` },
  cancel: { en: (r) => `Cancel ${r}`, es: (r) => `Cancelar ${r}` },
  close: { en: (r) => `Close ${r}`, es: (r) => `Cerrar ${r}` },
  send: { en: (r) => `Send ${r}`, es: (r) => `Enviar ${r}` },
  price: { en: (r) => `Manage pricing for ${r}`, es: (r) => `Gestionar precios de ${r}` },
  cost: { en: (r) => `View cost of ${r}`, es: (r) => `Ver el coste de ${r}` },
  import: { en: (r) => `Import ${r}`, es: (r) => `Importar ${r}` },
  export: { en: (r) => `Export ${r}`, es: (r) => `Exportar ${r}` },
  audit: { en: (r) => `View audit trail for ${r}`, es: (r) => `Ver auditoría de ${r}` },
  manage: { en: (r) => `Manage all ${r}`, es: (r) => `Gestionar todos los ${r}` },
  download: { en: (r) => `Download ${r}`, es: (r) => `Descargar ${r}` },
  invalidate: { en: (r) => `Invalidate ${r}`, es: (r) => `Invalidar ${r}` },
  upload: { en: (r) => `Upload ${r}`, es: (r) => `Subir ${r}` },
  view: { en: (r) => `View ${r}`, es: (r) => `Ver ${r}` },
  act: { en: (r) => `Process ${r}`, es: (r) => `Procesar ${r}` },
  exception: { en: (r) => `Create exceptional ${r}`, es: (r) => `Crear ${r} excepcionales` },
  transfer: { en: (r) => `Transfer ${r}`, es: (r) => `Transferir ${r}` },
	request: { en: (r) => `Request ${r}`, es: (r) => `Solicitar ${r}` },
	decide: { en: (r) => `Decide ${r}`, es: (r) => `Decidir ${r}` },
}

export function roleDisplayName(code: string, fallback: string, locale: string): string {
  return roleNames[code]?.[normalizedLocale(locale)] || fallback
}

export function roleDisplayDescription(code: string, fallback: string, locale: string): string {
  return roleDescriptions[code]?.[normalizedLocale(locale)] || fallback
}

export function permissionDisplayName(code: string, fallback: string, locale: string): string {
  const lang = normalizedLocale(locale)
  if (lang === 'zh') return fallback
  const parts = code.split(':')
  if (parts.length < 3) return fallback
  const action = parts.at(-1) || ''
  const resourceKey = `${parts[0]}:${parts.slice(1, -1).join('-')}`
  const resource = resources[resourceKey]?.[lang] || parts.slice(1, -1).join(' ').replaceAll('-', ' ')
  return actions[action]?.[lang](resource) || `${resource} (${action})`
}
