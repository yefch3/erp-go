type HelpText = readonly [zh: string, en: string, es: string]

function languageIndex(locale: string): 0 | 1 | 2 {
  const language = locale.toLowerCase().split('-')[0]
  return language === 'en' ? 1 : language === 'es' ? 2 : 0
}

const permissionNotes: Readonly<Partial<Record<string, HelpText>>> = {
  'approval:flow:read': ['查看审批流程及各节点的审批人配置。', 'View workflows and approver settings for each step.', 'Consulta los flujos y los responsables de cada paso.'],
  'approval:flow:write': [
    '新建或保存审批流程的新版本，设置节点顺序、审批人和金额档位，也可删除指定档位。',
    'Create workflows or save new versions, set step order, approvers and amount bands, and delete individual bands.',
    'Crea flujos o guarda nuevas versiones, configura el orden, los aprobadores y los tramos de importe, y elimina tramos.',
  ],
  'approval:instance:read': ['查看单据的审批进度、当前环节和处理历史。', 'View approval progress, current steps and processing history.', 'Consulta el avance, el paso actual y el historial de aprobación.'],
  'approval:task:act': [
    '保留在角色清单中的审批权限。当前审批按钮按任务实际分派开放，收到任务的人可按流程通过、驳回或退回。',
    'This approval permission remains in the role catalogue. The current workflow lets assigned approvers approve, reject or return their tasks.',
    'Este permiso sigue en el catálogo. El flujo actual permite aprobar, rechazar o devolver tareas a quienes las tienen asignadas.',
  ],

  'export:contract:approve': [
    '保存时会补齐「查看合同」。实际审批由流程分派，只有符合该任务审批条件的人才能处理。',
    'Saving this permission also grants View contracts. The workflow assigns approval tasks and determines who can act on them.',
    'Al guardar también se concede Ver contratos. El flujo asigna las tareas y determina quién puede tramitarlas.',
  ],
  'export:contract:read': ['打开外销合同列表和详情，查看合同条款、状态和资料。', 'Open export contracts and view their terms, status and records.', 'Abre contratos de exportación y consulta sus condiciones, estado y documentos.'],
  'export:contract:write': [
    '新建、编辑或补录外销合同，上传或移除合同附件，并按状态提交审批、发起变更、登记签署、完结或作废。',
    'Create, edit or backfill export contracts, add or remove attachments, and submit, amend, record signing, complete or cancel them as their status allows.',
    'Crea, edita o registra contratos históricos, añade o quita archivos y, según el estado, envía a aprobación, modifica, registra la firma, completa o cancela.',
  ],
  'export:ownership:transfer': ['转移报价单、外销合同等单据的负责人。更换负责人可能改变谁能看到该单。', 'Transfer ownership of documents such as quotations and export contracts. This can change visibility.', 'Transfiere la responsabilidad de cotizaciones y contratos de exportación. Esto puede cambiar su visibilidad.'],
  'export:quotation:read': ['查看客户报价单、明细及报价状态。', 'View customer quotations, line items and statuses.', 'Consulta cotizaciones de clientes, partidas y estados.'],
  'export:quotation:write': [
    '创建或编辑客户报价和报价明细，发送报价、登记客户回复或取消报价。也可从符合条件的询盘生成正式报价单。',
    'Create or edit customer quotes and line items, send quotes, record customer responses or cancel quotes. Generate formal quotes from eligible inquiries.',
    'Crea o edita cotizaciones y partidas, envía cotizaciones, registra respuestas o cancela. Genera cotizaciones formales desde consultas elegibles.',
  ],
  'export:receipt:read': ['查看应收、已登记收款及核销对账结果。', 'View receivables, recorded receipts and reconciliation results.', 'Consulta cuentas por cobrar, cobros registrados y resultados de conciliación.'],
  'export:receipt:write': [
    '新增银行账户，登记或冲销客户收款，新建或编辑手工应收，调整到期日、确认合同执行条件，以及完成或重开应收核销。',
    'Add bank accounts, record or reverse customer receipts, create or edit manual receivables, adjust due dates, release contracts for execution, and close or reopen settlement.',
    'Añade cuentas bancarias, registra o revierte cobros, crea o edita cuentas por cobrar manuales, ajusta vencimientos, habilita la ejecución del contrato y cierra o reabre la conciliación.',
  ],
  'export:shipment:read': ['查看出运单及其中的货物和发运信息。', 'View shipment records, cargo and dispatch information.', 'Consulta expediciones, mercancía y datos de despacho.'],
  'export:shipment:write': [
    '新建或编辑出运单、确认发运、登记到达或取消出运单。各操作受单据状态限制。',
    'Create or edit shipments, confirm dispatch, record arrival or cancel, subject to the shipment status.',
    'Crea o edita expediciones, confirma el despacho, registra la llegada o cancela, según su estado.',
  ],

  'fx:rate:read': [
    '查看汇率异常清单。普通汇率、同步状态和关注币种可由已登录员工查看。',
    'View the exchange-rate anomalies list. Signed-in employees can already view standard rates, sync status and watched currencies.',
    'Consulta las anomalías de tipos de cambio. Todo usuario autenticado puede consultar los tipos normales, el estado de sincronización y las divisas seguidas.',
  ],
  'fx:rate:write': [
    '在汇率页保存公司关注的币种。该设置影响关注列表。',
    'Save the currencies tracked by the company on the exchange-rates page.',
    'Guarda las divisas que sigue la empresa en la página de tipos de cambio.',
  ],
  'iam:department:read': ['查看部门、部门层级及负责人。', 'View departments, their hierarchy and department leads.', 'Consulta departamentos, su jerarquía y sus responsables.'],
  'iam:department:write': [
    '新增或编辑部门，可调整上级部门、负责人和部门状态。部门调整会影响按部门配置的单据可见范围。',
    'Create or edit departments, change their parent department, lead or status. Changes can affect department-based document visibility.',
    'Crea o edita departamentos y cambia su superior, responsable o estado. Los cambios pueden afectar la visibilidad de documentos por departamento.',
  ],
  'iam:employee:read': [
    '查看员工列表、档案、变更记录、公司邮箱配置和组织架构。查看员工实际生效的权限还需「查看角色」。',
    'View employee lists, profiles, history, mailbox configuration and the org chart. Effective access diagnostics also require View roles.',
    'Consulta listas, fichas, historial, configuración de buzones y organigrama. El diagnóstico de acceso efectivo también requiere Ver roles.',
  ],
  'iam:employee:write': [
    '新增、编辑或批量导入员工，调整所属部门和直属主管，启用或停用员工。也可开通账号、发邀请、重置密码、结束登录或分配公司邮箱。分配角色需「维护角色」。',
    'Create, edit or import employees, change departments or managers, and activate or deactivate staff. Open accounts, invite users, reset passwords, revoke sessions or assign company mailboxes. Assigning roles requires Manage roles.',
    'Crea, edita o importa empleados, cambia departamentos o supervisores y activa o desactiva empleados. Abre cuentas, envía invitaciones, restablece contraseñas, cierra sesiones o asigna buzones. Asignar roles requiere Gestionar roles.',
  ],
  'iam:role:read': ['查看角色、权限清单和已配置的数据范围。', 'View roles, permission lists and configured data scopes.', 'Consulta roles, listas de permisos y alcances configurados.'],
  'iam:role:write': [
    '新建、启用或停用角色，保存角色的权限和数据范围，并给员工分配或移除角色。授权仍受角色管理规则约束。',
    'Create, activate or deactivate roles, save permissions and data scopes, and assign or remove employee roles, subject to role administration rules.',
    'Crea, activa o desactiva roles, guarda permisos y alcances, y asigna o quita roles según las reglas de administración.',
  ],

  'inventory:stock:cost': ['查看库存成本和金额等成本信息。', 'View inventory costs and stock values.', 'Consulta los costos y valores del inventario.'],
  'inventory:stock:import': [
    '预览并校验期初库存导入数据，确认后批量写入库存，也可取消未确认的导入。',
    'Preview and validate opening stock, confirm the bulk import or cancel an unconfirmed import.',
    'Previsualiza y valida los saldos iniciales, confirma la importación masiva o cancela una importación sin confirmar.',
  ],
  'inventory:stock:read': ['查看仓库、库存数量及出入库记录。', 'View warehouses, stock quantities and movement records.', 'Consulta almacenes, existencias y registros de movimientos.'],
  'inventory:stock:write': [
    '新增或编辑仓库和仓库设置，登记库存入库、冻结或解冻库存，以及新建、确认或取消出库单。导入期初库存需单独授权。',
    'Create or edit warehouses and their settings, receive or freeze and release stock, and create, confirm or cancel stock issues. Opening-stock imports require a separate permission.',
    'Crea o edita almacenes y sus ajustes, registra entradas, bloquea o libera existencias y crea, confirma o cancela salidas. Importar saldos iniciales requiere otro permiso.',
  ],

  'mail:email:export': [
    '邮箱解锁后，可将有权访问的邮件会话导出。查看谁在何时导出使用「查看导出记录」。',
    'After unlocking the mailbox, export conversations you can access. Reviewing who exported and when uses View export records.',
    'Después de desbloquear el buzón, exporta las conversaciones accesibles. Para consultar quién exportó y cuándo, usa Ver registros de exportación.',
  ],
  'mail:email:read': [
    '查看邮箱收发记录，也可同步自己的邮箱、新增或删除文件夹、移动邮件、标记已读或删除邮件。个人邮箱默认有此权限。',
    'View sent and received mail, sync your mailbox, create or delete folders, move messages, mark them as read or delete them. Personal mailboxes have this permission by default.',
    'Consulta correos enviados y recibidos, sincroniza tu buzón, crea o elimina carpetas, mueve mensajes, márcalos como leídos o elimínalos. Los buzones personales tienen este permiso por defecto.',
  ],
  'mail:email:write': [
    '新建或编辑草稿、上传附件、发送或定时发送邮件，建立群发并管理签名和邮件模板。发信仍受邮箱与发送规则限制。',
    'Create or edit drafts, upload attachments, send or schedule messages, create campaigns and manage signatures and templates. Sending remains subject to mailbox and delivery rules.',
    'Crea o edita borradores, sube adjuntos, envía o programa mensajes, crea campañas y configura firmas y plantillas. El envío sigue las reglas del buzón.',
  ],
  'mail:export:audit': ['查看邮件导出记录，核对导出操作和时间。', 'Review email export records and when exports were performed.', 'Consulta los registros de exportación de correo y cuándo se realizaron.'],
  'mail:supervision:audit': ['查看员工邮箱监管操作的审计记录。', 'View the audit history of employee mailbox supervision.', 'Consulta el historial de auditoría de la supervisión de buzones.'],
  'mail:supervision:read': ['通过邮箱监管功能查看员工邮箱，实际访问仍受监管规则限制。', 'Read employee mailboxes through mailbox supervision, subject to its access rules.', 'Consulta buzones de empleados mediante la supervisión, según sus reglas de acceso.'],
  'mail:suppression:write': [
    '新增或移除拒收名单里的邮箱地址。名单内的地址会被排除在后续发送之外。',
    'Add or remove email addresses on the suppression list. Listed addresses are excluded from future sends.',
    'Añade o quita direcciones de la lista de exclusión. Las direcciones incluidas se omiten en envíos posteriores.',
  ],

  'masterdata:customer:read': ['打开客户列表和档案。普通员工按客户档案上的有效负责人关系查看。', 'Open customer records. Regular employees see records for which they are active owners.', 'Abre fichas de clientes. Los empleados ven las fichas de las que son responsables activos.'],
  'masterdata:customer:write': [
    '新建或批量导入客户。可编辑负责客户的档案、联系人、地址和负责人，上传资料，登记信用评级或恢复停用档案。停用客户、批量删除或批量调整负责人需超级管理员权限。',
    'Create or bulk-import customers. Edit profiles, contacts, addresses and owners on accessible records, upload documents, record credit ratings or reactivate records. Deactivation, bulk deletion and bulk owner changes require a super administrator.',
    'Crea o importa clientes, edita fichas, contactos, direcciones y responsables accesibles, sube documentos, registra calificaciones o reactiva fichas. Desactivar, eliminar en bloque o cambiar responsables en bloque requiere un superadministrador.',
  ],
  'masterdata:port:read': ['查看港口基础档案，用于查询或选择起运港、目的港。', 'View port records and look up origin and destination ports.', 'Consulta fichas de puertos y busca puertos de origen y destino.'],
  'masterdata:port:write': [
    '可在「船港管理」点击「新增港口」，也可批量导入、编辑、启用或停用港口，以及设置常用港口。',
    'Use Add port in Port Management. You can also import ports in bulk, edit, activate or deactivate them, and mark them as favorites.',
    'Usa Añadir puerto en la gestión de puertos. También puedes importar puertos en bloque, editarlos, activarlos, desactivarlos y marcarlos como favoritos.',
  ],
  'masterdata:supplier:read': ['打开供应商列表和详情。普通员工看到的是自己担任有效负责人的供应商。', 'Open the supplier list and details. Regular employees see suppliers for which they are active owners.', 'Abre la lista y las fichas de proveedores. Los empleados ven los proveedores de los que son responsables activos.'],
  'masterdata:supplier:write': [
    '新建或批量导入供应商。可编辑负责供应商的档案或联系人、上传或移除资料、登记信用评级或恢复停用档案。新建时自动把录入人设为主负责人。停用、批量删除及分配负责人需超级管理员权限。',
    'Create or bulk-import suppliers. Edit accessible profiles and contacts, add or remove documents, record credit ratings or reactivate records. The creator becomes the primary owner. Deactivation, bulk deletion and owner assignment require a super administrator.',
    'Crea o importa proveedores, edita fichas y contactos accesibles, añade o quita documentos, registra calificaciones o reactiva fichas. Quien crea la ficha queda como responsable principal. Desactivar, eliminar en bloque o asignar responsables requiere un superadministrador.',
  ],

  'procurement:daily-price:delete': ['删除修改范围内录入错误的每日基价或基差记录。', 'Delete incorrect daily base prices or spreads within your editing scope.', 'Elimina precios base diarios o diferenciales erróneos dentro del alcance de edición.'],
  'procurement:daily-price:manage': [
    '新增、编辑或删除每日基价的配置项，扩大到全公司的基价和基差记录。录入和删除记录分别需要对应权限。',
    'Add, edit or delete daily-price configuration items and extend access to company-wide price and spread records. Entering or deleting records still requires the respective permission.',
    'Añade, edita o elimina elementos de configuración y amplía el alcance a los precios y diferenciales de toda la empresa. Registrar o borrar datos requiere su permiso específico.',
  ],
  'procurement:daily-price:read': ['查看每日基价、基差及历史趋势。', 'View daily base prices, spreads and historical trends.', 'Consulta precios base diarios, diferenciales y tendencias históricas.'],
  'procurement:daily-price:write': ['录入或修改每日基价和基差，修改范围影响可处理的记录。', 'Enter or edit base prices and spreads within your editing scope.', 'Registra o edita precios base y diferenciales dentro de tu alcance.'],
  'procurement:exception:write': [
    '在采购单里新增到货异常或到货检查记录，并登记这些记录的处理结果。',
    'Add receipt exceptions or receipt inspection records to purchase orders and record their resolutions.',
    'Añade incidencias o revisiones de recepción a las órdenes de compra y registra sus resultados.',
  ],
  'procurement:invoice:read': [
    '旧版供应商发票权限。当前发票页已下线，历史发票相关核销可在「供应商对账」里按对账权限查看。',
    'Legacy supplier-invoice permission. The invoice page is retired. Review settlements related to historical invoices in Supplier reconciliation with the relevant permission.',
    'Permiso antiguo de facturas. La página de facturas se retiró. Consulta las conciliaciones históricas en Conciliación de proveedores con el permiso correspondiente.',
  ],
  'procurement:invoice:write': [
    '旧版供应商发票权限。当前版本没有独立的发票录入页。上传发票扫描件或冲销历史核销，请使用「添加和维护出账」。',
    'Legacy supplier-invoice permission. There is no standalone invoice entry page in this version. Use Add and manage outgoing payments to attach invoice scans or reverse historical settlements.',
    'Permiso antiguo de facturas. Esta versión no tiene una página independiente de registro. Para adjuntar facturas escaneadas o revertir conciliaciones históricas usa el permiso de gestión de pagos salientes.',
  ],
  'procurement:order:cancel': [
    '填写取消原因后，可取消草稿、已驳回或未到货的已下单采购单。已有到货的采购单不能用此操作取消。',
    'Enter a reason to cancel a draft, rejected order or placed order with no receipts. Orders with received quantities are ineligible.',
    'Indica un motivo para cancelar un borrador, una orden rechazada o un pedido sin recepciones. Las órdenes con cantidades recibidas quedan excluidas.',
  ],
  'procurement:order:close': [
    '将已收货或部分收货的采购单结案。未收齐时，需注明剩余数量另购或放弃，并填写原因。',
    'Close received or partially received purchase orders. For a shortfall, specify whether to buy the balance elsewhere or drop it, and enter a reason.',
    'Cierra órdenes recibidas o parcialmente recibidas. Si falta cantidad, indica si se comprará por otra vía o se descartará y registra el motivo.',
  ],
  'procurement:order:read': [
    '打开采购单列表、明细、采购单据和履约资料。按采购负责人和「采购订单可见范围」筛选。登记仓库收货或确认工厂交货另有写入权限。',
    'View purchase orders, documents and fulfillment details within the assigned-buyer scope. Warehouse receipts and factory handover confirmations require separate write access.',
    'Consulta órdenes, documentos y seguimiento según el alcance del comprador. Registrar la recepción en almacén o confirmar la entrega de fábrica exige permisos de escritura específicos.',
  ],
  'procurement:order:send': ['按流程正式将采购单发送给供应商。', 'Send purchase orders formally to suppliers according to the workflow.', 'Envía formalmente órdenes de compra a proveedores según el flujo.'],
  'procurement:order:submit': ['将符合条件的采购单草稿提交审批。', 'Submit eligible purchase-order drafts for approval.', 'Envía borradores de órdenes de compra elegibles a aprobación.'],
  'procurement:order:write': [
    '新建、编辑或导入采购草稿，补录历史采购单并更换历史单采购员。可录入、选择或删除实单报价，上传采购合同、申请付款、确认工厂交货或记录质检异常处理。提交审批、取消和正式发送分别单独授权。',
    'Create, edit or import purchase drafts, backfill and reassign historical orders. Enter, select or delete execution quotes, upload contracts, request payment, confirm factory handover or record responses to inspection issues. Submission, cancellation and formal sending require separate permissions.',
    'Crea, edita o importa borradores, registra órdenes históricas y reasígnalas. Registra, selecciona o elimina cotizaciones de ejecución, sube contratos, solicita pagos, confirma la entrega de fábrica o registra la resolución de incidencias. El envío a aprobación, la cancelación y el envío formal tienen permisos propios.',
  ],
  'procurement:payment:read': [
    '查看采购侧银行流水及变更记录。查看供应商应付和付款核销使用「查看供应商对账」。',
    'View procurement-side bank transactions and their change history. Supplier payables and settlements use View supplier reconciliation.',
    'Consulta movimientos bancarios de compras y su historial. Las cuentas por pagar y conciliaciones requieren Ver conciliación de proveedores.',
  ],
  'procurement:payment:write': [
    '旧版付款写入权限。保存时会补齐采购侧银行流水的查看权限。当前供应商付款登记、冲销和核销使用「添加和维护出账」。',
    'Legacy payment write permission. Saving it also grants procurement bank transaction viewing. Supplier payment entry, reversal and settlement now use Add and manage outgoing payments.',
    'Permiso antiguo de escritura de pagos. Al guardarlo también se concede la consulta de movimientos bancarios de compras. Para registrar, revertir o conciliar pagos a proveedores usa el permiso de gestión de pagos salientes.',
  ],
  'procurement:production:write': [
    '在采购单上新增跟进记录、登记供应商确认，或保存备货和生产进度节点。',
    'Add purchase-order follow-ups, record supplier confirmations, and save preparation or production milestones.',
    'Añade seguimientos, registra confirmaciones del proveedor y guarda hitos de preparación o producción.',
  ],
  'procurement:receipt:write': [
    '在仓库到货场景登记采购收货、实际到货数量及相关凭证。采购确认工厂交货使用「维护采购单」。',
    'Record warehouse receipts, actual quantities and supporting documents. Confirming factory handover uses Manage purchase orders.',
    'Registra recepciones en almacén, cantidades reales y comprobantes. Confirmar la entrega de fábrica requiere Gestionar órdenes de compra.',
  ],
  'procurement:recon:read': ['查看供应商应付、出账、付款及核销结果。', 'View supplier payables, payment records and reconciliation results.', 'Consulta cuentas por pagar a proveedores, pagos y resultados de conciliación.'],
  'procurement:recon:write': [
    '新增或编辑手工应付，登记或冲销付款，确认完成或重开核销，修改应付到期日并上传或移除凭证。也可核验采购和物流合同。',
    'Create or edit manual payables, record or reverse payments, close or reopen settlement, change due dates and add or remove receipts. Also verify purchase and logistics contracts.',
    'Crea o edita cuentas por pagar manuales, registra o revierte pagos, cierra o reabre la conciliación, cambia vencimientos y añade o quita comprobantes. También verifica contratos de compra y logística.',
  ],
  'procurement:reimbursement:manage': ['将已通过审批的报销标记为已付款，或撤销付款登记。', 'Mark approved reimbursements as paid or reverse the payment entry.', 'Marca reembolsos aprobados como pagados o revierte el registro de pago.'],
  'finance:reimbursement:approve': ['审批直接送交财务的报销。只有超级管理员可以审批自己的报销和调整此授权。', 'Decide on reimbursements sent directly to Finance. Only super administrators can decide their own claims or change this grant.', 'Aprueba o devuelve reembolsos enviados directamente a Finanzas. Solo los superadministradores pueden decidir sobre sus propias solicitudes o cambiar este permiso.'],
  'procurement:requirement:exception': ['手工建立例外采购需求，用于需要独立登记的采购事项。', 'Manually create exceptional purchase requirements for purchases that need a separate record.', 'Crea manualmente necesidades de compra excepcionales que requieren un registro independiente.'],
  'procurement:requirement:read': ['进入实单询价并查看采购需求。能看谁的需求由「采购需求可见范围」决定，页面还按需求状态筛选。', 'Open execution sourcing and view purchase requirements. Results follow the requirement scope and the page’s status filters.', 'Abre la cotización de ejecución y consulta necesidades de compra según su alcance y los filtros de estado.'],
  'procurement:requirement:write': [
    '关闭或重新打开符合条件的采购需求。手工新增例外采购需求使用「建立例外采购需求」。',
    'Close or reopen eligible purchase requirements. Adding a manual exceptional requirement uses Create exceptional purchase requirements.',
    'Cierra o reabre necesidades de compra elegibles. Para crear una necesidad excepcional manual usa Crear necesidades de compra excepcionales.',
  ],
  'procurement:sourcing:approve': [
    '指定主责采购员，建立统一采购方案、确认成本方案并提交销售，也可发布补充询价任务。生成正式客户报价单使用「维护报价单」。',
    'Assign the lead buyer, create a unified purchase plan, confirm a cost scenario, submit plans to sales or request follow-up sourcing. Creating the formal customer quotation uses Manage quotations.',
    'Asigna al comprador principal, crea un plan unificado, confirma un escenario de costos, lo envía a ventas o solicita cotizaciones adicionales. Crear la cotización formal requiere Gestionar cotizaciones.',
  ],
  'procurement:execution-quote:select': [
    '在实单询价中勾选工厂报价并确认后，原报价录入人负责该规格的议价和生成采购草稿。也可取消尚未生成草稿的指派。可选择的报价由「工厂报价可见范围」决定。',
    'Confirm a selected factory quote in an execution inquiry to assign negotiation and draft purchase order creation for that specification to the employee who entered the quote. Assignments can be cancelled before a draft is created. Factory quote visibility determines which quotes can be selected.',
    'Al confirmar una cotización de fábrica en una consulta de ejecución, quien registró la cotización se encarga de negociar y generar el borrador de compra de esa especificación. Se puede cancelar la asignación antes de generar el borrador. La visibilidad de cotizaciones determina cuáles se pueden seleccionar.',
  ],
  'procurement:sourcing:price': [
    '手工录入或批量导入供应商报价，建立成本方案，处理被分派的补充询价或重新议价任务。确认成本方案另需相应权限。',
    'Enter or bulk-import supplier quotes, create cost scenarios and resolve assigned re-quote or renegotiation tasks. Confirming cost scenarios requires separate access.',
    'Registra o importa cotizaciones, crea escenarios de costos y resuelve tareas asignadas de recotización o renegociación. Confirmar los costos requiere otro permiso.',
  ],
  'procurement:sourcing:read': ['查看售前采购询价、案件及可访问的询价明细。', 'View presales procurement sourcing cases and accessible inquiry details.', 'Consulta casos de abastecimiento de preventa y los detalles a los que tienes acceso.'],
  'procurement:sourcing:send': ['按询价流程向工厂或供应商发出询价。', 'Send requests for quotation to factories or suppliers through the sourcing workflow.', 'Envía solicitudes de cotización a fábricas o proveedores mediante el flujo de compras.'],
  'procurement:sourcing:write': [
    '接收或退回采购询价、加入询价或申请主责，新增或编辑工厂询价记录。发询价和录报价分别使用各自的权限。',
    'Accept or return sourcing inquiries, join or request the lead assignment, and create or edit factory requests for quotation. Sending and entering quotes use separate permissions.',
    'Acepta o devuelve consultas, participa o solicita ser responsable y crea o edita solicitudes a fábricas. Enviar y registrar cotizaciones requieren permisos propios.',
  ],

  'product:product:read': ['查看产品资料、规格及可选产品。', 'View product records, specifications and available products.', 'Consulta fichas, especificaciones y productos disponibles.'],
  'product:product:write': [
    '新增、编辑、启用或停用产品；新增规格（SKU）或停用规格；新增、编辑或停用产品分类；新增计量单位、配置属性模板及上传或移除产品附件。',
    'Create, edit, activate or deactivate products, add or deactivate SKUs, create, edit or deactivate categories, add units, set attribute templates and add or remove attachments.',
    'Crea, edita, activa o desactiva productos, añade o desactiva SKU, crea, edita o desactiva categorías, añade unidades, configura atributos y añade o quita adjuntos.',
  ],
  'quality:file:upload': ['为质检任务保存现场照片、视频或报告等质检资料。', 'Save photos, video, reports and other evidence for inspection tasks.', 'Guarda fotos, videos, informes y otras evidencias de inspección.'],
  'quality:release:decide': ['记录质检部分合格后允许先发的数量和放行决定。', 'Record release decisions and the quantity allowed to ship after a partial pass.', 'Registra la decisión de liberación y la cantidad que puede enviarse tras una aprobación parcial.'],
  'quality:task:read': [
    '打开质检工作台，查看范围内的任务、检查结果和历史资料。删除质检任务还需超级管理员权限。',
    'Open the inspections workspace and view tasks, findings and history within scope. Deleting inspection tasks requires a super administrator.',
    'Abre el espacio de inspecciones y consulta tareas, resultados e historial dentro del alcance. Eliminar tareas requiere un superadministrador.',
  ],
  'quality:task:request': [
    '从已下单的采购单发起整单出厂前质检，填写预计日期、地点和联系人。同一采购单只有一张质检任务，进入质检工作台需另有查看质检权限。',
    'Request one pre-shipment inspection per placed purchase order and enter the expected date, location and contact. The inspections workspace requires separate read access.',
    'Solicita una inspección previa al envío por orden emitida e indica fecha, lugar y contacto. El espacio de inspecciones requiere un permiso de consulta independiente.',
  ],
  'quality:task:write': [
    '在质检工作台建单，修改预计质检日期、联系人或地点，接收任务并登记检查和复检结果。保存现场资料需「保存质检资料」。',
    'Create inspection tasks, edit expected dates, contacts or locations, accept tasks and record inspection and reinspection results. Saving evidence requires Save inspection files.',
    'Crea tareas, cambia fechas previstas, contactos o lugares, acepta tareas y registra inspecciones y reinspecciones. Guardar evidencias requiere Guardar archivos de inspección.',
  ],
  'report:business:read': [
    '查看授权范围内的团队邮件活动和待回复统计。邮件明细继续受邮箱权限控制。',
    'View team mail activity and reply queues within the reporting scope. Mail content follows the existing mailbox permissions.',
    'Consulta la actividad de correo del equipo y las respuestas pendientes dentro del alcance del informe.',
  ],

  'sales:inquiry:delete': ['删除流程允许删除的客户询盘。', 'Delete customer inquiries when their status permits deletion.', 'Elimina consultas de clientes cuando su estado lo permite.'],
  'sales:inquiry:read': ['进入客户询盘列表，查看有权限访问的询盘及需求明细。', 'Open customer inquiries and view accessible inquiry details and requirements.', 'Abre consultas de clientes y ve los detalles y requisitos a los que tienes acceso.'],
  'sales:inquiry:submit': ['将整理好的客户询盘提交给采购进入询价流程。', 'Submit prepared customer inquiries to procurement to start sourcing.', 'Envía a compras las consultas preparadas para iniciar la cotización.'],
  'sales:inquiry:write': [
    '新建、编辑或导入客户询盘，编辑产品明细、撤回询盘，记录客户反馈和选择，并发布补充采购或船运询价任务。可新建、编辑、启停询盘模板。提交和删除询盘单独授权。',
    'Create, edit or import inquiries, edit line items, withdraw inquiries, record feedback and selections, request follow-up quotes, and create, edit, activate or deactivate templates. Submitting and deleting inquiries require separate permissions.',
    'Crea, edita o importa consultas, edita partidas, retira consultas, registra respuestas y selecciones, pide cotizaciones adicionales y crea, edita, activa o desactiva plantillas. Enviar y eliminar requieren permisos propios.',
  ],
  'sales:procurement-progress:read': ['查看客户询盘的采购处理进度和汇总。', 'View procurement progress and summaries for customer inquiries.', 'Consulta el avance y el resumen de compras de las consultas de clientes.'],

  'shipping:document:download': ['下载有权访问的船期单证文件。', 'Download shipping documents you can access.', 'Descarga los documentos de embarque a los que tienes acceso.'],
  'shipping:document:invalidate': ['将符合条件的船期单证标记为作废，并保留历史。', 'Invalidate eligible shipping documents while preserving their history.', 'Anula documentos de embarque elegibles y conserva su historial.'],
  'shipping:document:manage': [
    '保存时会补齐「查看船期」和「查看船期单证」。上传、下载或作废单证需要勾选各自的权限。',
    'Saving grants View shipping schedules and View shipping documents. Uploading, downloading or invalidating documents requires the corresponding permissions.',
    'Al guardar se conceden Ver embarques y Ver documentos de embarque. Subir, descargar o anular requiere el permiso correspondiente.',
  ],
  'shipping:document:upload': ['为有权维护的船期上传单证和相关附件。', 'Upload documents and attachments to schedules you can maintain.', 'Sube documentos y adjuntos a los embarques que puedes gestionar.'],
  'shipping:document:view': [
    '查看船期单证列表、状态及在线预览文件。下载、上传和作废分别单独授权。',
    'View shipping document lists and statuses and preview files online. Downloading, uploading and invalidating each require separate permissions.',
    'Consulta listas y estados y previsualiza archivos en línea. Descargar, subir y anular requieren permisos propios.',
  ],
  'shipping:progress:write': ['更新船期进度、节点和延误记录。', 'Update shipping progress, milestones and delay records.', 'Actualiza el avance, los hitos y los retrasos de embarque.'],
  'shipping:route:write': [
    '新增或移除船期路线里的港口节点，调整节点顺序，也可编辑港口相关航程资料。新建港口基础档案使用「维护港口」。',
    'Add or remove ports from a shipping route, reorder its stops and edit voyage details. Creating port master records uses Manage ports.',
    'Añade o elimina puertos de una ruta, reordena las escalas y edita datos del trayecto. Crear fichas de puertos requiere Gestionar puertos.',
  ],
  'shipping:schedule:read': [
    '查看范围内的船期及运输资料，查看和标记船期提醒，并调整自己的提醒偏好。',
    'View schedules and transport records within scope, review and mark schedule reminders and adjust your own reminder preferences.',
    'Consulta embarques y transporte dentro del alcance, revisa y marca recordatorios y ajusta tus preferencias.',
  ],
  'shipping:schedule:write': [
    '新增、编辑、取消或更新船期状态，设定提醒规则；也可新建手工订舱、关联合同，录入或选定物流报价、上传物流合同及申请付款。',
    'Create, edit or cancel schedules, change statuses and set reminder rules. Create manual bookings, link contracts, enter or choose logistics quotes, upload contracts and request payment.',
    'Crea, edita o cancela embarques, cambia estados y configura recordatorios. Crea reservas manuales, vincula contratos, registra o elige cotizaciones, sube contratos y solicita pagos.',
  ],
  'shipping:sourcing:approve': ['指定主责船运人员，建立统一船运方案并提交销售。', 'Assign a shipping lead, create a consolidated shipping plan and submit it to sales.', 'Asigna responsable de embarque, crea un plan consolidado y lo envía a ventas.'],
  'shipping:sourcing:read': ['查看售前船运询价任务及有权访问的船运报价。', 'View presales shipping inquiries and quotes you can access.', 'Consulta solicitudes y cotizaciones logísticas de preventa a las que tienes acceso.'],
  'shipping:sourcing:write': [
    '开始或加入售前船运询价、申请成为主责，录入或修改承运人、货代报价，并处理补充询价任务。',
    'Start or join presales shipping inquiries, request the lead assignment, add or edit carrier and forwarder quotes, and resolve follow-up tasks.',
    'Inicia o participa en consultas logísticas de preventa, solicita ser responsable, añade o edita cotizaciones y resuelve tareas de seguimiento.',
  ],
}

const fallback: HelpText = [
  '此权限暂未补录说明，可按权限编码核对。',
  'A description has not yet been added for this permission. Use its code to identify it.',
  'Este permiso aún no tiene descripción. Puedes identificarlo por su código.',
]

export function permissionHelp(code: string, locale: string): string {
  return (permissionNotes[code] ?? fallback)[languageIndex(locale)]
}

const scopeModules: Readonly<Record<string, HelpText>> = {
  export: [
    '按单据负责人限制报价单、外销合同等出口业务单据的可见和维护范围。部分操作另有业务规则。',
    'Limits access to quotations, export contracts and other export documents by document owner. Some actions have additional rules.',
    'Limita el acceso a cotizaciones, contratos y otros documentos de exportación por responsable. Algunas acciones tienen reglas adicionales.',
  ],
  procurement_sourcing: [
    '影响售前客户询盘、采购寻源和关联船运询价，按单据负责人及流程中的参与关系判断。',
    'Affects presales customer inquiries, procurement sourcing and related shipping inquiries based on ownership and participation.',
    'Afecta consultas de clientes, abastecimiento y cotizaciones logísticas de preventa según responsables y participantes.',
  ],
  procurement_order: [
    '采购单按单据上的采购负责人筛选。在「仅本人」下，只能看到自己负责的单。相关供应商发票按录入人判断。',
    'Orders are filtered by the assigned buyer. Own records shows only orders assigned to you. Related supplier invoices use the person who entered them.',
    'Las órdenes se filtran por comprador asignado. Solo propios muestra únicamente tus órdenes. Las facturas vinculadas se filtran por quien las registró.',
  ],
  procurement_requirement: [
    '影响「实单询价」的采购需求。合同生成的需求通常归合同销售负责人，手工需求归创建人。',
    'Applies to execution-sourcing requirements. Contract-generated requirements usually belong to the contract salesperson. Manual ones belong to their creator.',
    'Se aplica a las necesidades de ejecución. Las generadas desde un contrato suelen pertenecer a su vendedor. Las manuales pertenecen a quien las creó.',
  ],
  procurement_factory_quote: [
    '按报价录入人筛选售前询价和实单询价里的工厂报价。采购需求继续按自己的可见范围展示。选厂和代做仍需相应的业务权限。',
    'Filters factory quotes in presales and execution inquiries by who entered them. Purchase requirements have a separate scope. Selecting factories and acting for another buyer still require the relevant permissions.',
    'Filtra por autor las cotizaciones de fábrica en consultas de preventa y ejecución. Las necesidades de compra tienen su propio alcance. Seleccionar fábricas o actuar por otro comprador requiere los permisos correspondientes.',
  ],
  supplier_read: [
    '需要「查看供应商」权限。此范围控制供应商及货代档案的查看。编辑、删除及负责人管理仍按原有权限执行。',
    'Requires View suppliers. This scope controls access to view supplier and freight forwarder records. Editing, deletion and owner management follow existing permissions.',
    'Requiere Ver proveedores. Este alcance controla la consulta de fichas de proveedores y agentes de carga. La edición, la eliminación y la gestión de responsables conservan sus permisos actuales.',
  ],
  daily_base_price: [
    '按录入人限制可修改或删除的每日基价和基差。查看趋势与管理配置另有功能权限。',
    'Limits whose daily base prices and spreads can be edited or deleted. Viewing trends and managing settings have separate permissions.',
    'Limita qué precios base y diferenciales se pueden editar o borrar según quién los registró. Las tendencias y la configuración tienen permisos propios.',
  ],
  shipping: [
    '船期可以通过船期负责人进入范围，也可以通过关联客户的负责人进入范围。',
    'A schedule can fall within scope through its assigned owner or the owner of the linked customer.',
    'Un embarque puede estar dentro del alcance por su responsable o por el responsable del cliente vinculado.',
  ],
  quality: [
    '质检任务按关联采购单的采购负责人或采购需求负责人判断是否在范围内。',
    'Inspection scope follows the assigned buyer of the related order or the associated purchase requirement owner.',
    'El alcance de las inspecciones sigue al comprador asignado a la orden o al responsable de la necesidad vinculada.',
  ],
  mail: [
    '按发件人限制业务发件记录及往来的可见范围。员工邮箱监管另有权限和规则。',
    'Limits visibility of business sending records and correspondence by sender. Mailbox supervision has its own permissions and rules.',
    'Limita los registros de envíos y conversaciones por remitente. La supervisión de buzones tiene permisos y reglas propios.',
  ],
}

const scopeOptions: Readonly<Record<string, HelpText>> = {
  SELF: ['只覆盖本人。系统按上面每项说明确定记录归谁。', 'Covers your own records. Each module explains how it determines ownership.', 'Abarca tus propios registros. Cada módulo explica cómo determina el responsable.'],
  DEPT: ['覆盖本人及同部门在职员工。', 'Covers you and active employees in your department.', 'Te abarca a ti y al personal activo de tu departamento.'],
  DEPT_AND_SUB: ['覆盖本人、同部门及其下级部门的在职员工。', 'Covers you and active employees in your department and its subdepartments.', 'Te abarca a ti y al personal activo de tu departamento y de sus departamentos subordinados.'],
  ALL: ['覆盖当前公司内该模块的所有数据，仍需对应的功能权限。', 'Covers all records for this module in the current company. The corresponding feature permissions are still required.', 'Abarca todos los registros del módulo en la empresa actual. También se requieren los permisos de la función.'],
  CUSTOM: ['覆盖本人和已指定部门的在职员工。', 'Covers you and active employees in the configured departments.', 'Te abarca a ti y al personal activo de los departamentos configurados.'],
}

const scopeCopy = {
  intro: [
    '功能权限决定可以做什么。可配置的数据范围决定可以处理谁的记录，未设置时默认仅本人。',
    'Feature permissions control available actions. Configurable data scopes control whose records are covered and default to your own.',
    'Los permisos de función determinan las acciones disponibles. Los alcances configurables determinan qué registros se incluyen y, por defecto, abarcan los propios.',
  ],
  ownerBased: [
    '客户按档案上的有效负责人关系控制可见性。普通员工查看自己负责的客户，超级管理员可以查看全部。',
    'Customer visibility follows active ownership on each record. Regular employees see customers they own. Super administrators can see all customers.',
    'La visibilidad de clientes sigue a los responsables activos de cada ficha. El personal ve los clientes a su cargo. Los superadministradores pueden ver todos.',
  ],
  optionsTitle: ['查看各范围的含义', 'What each scope covers', 'Qué abarca cada alcance'],
} satisfies Record<string, HelpText>

export function scopeHelp(locale: string) {
  const index = languageIndex(locale)
  const translate = (entries: Readonly<Record<string, HelpText>>): Record<string, string> =>
    Object.fromEntries(Object.entries(entries).map(([key, text]) => [key, text[index]]))

  return {
    intro: scopeCopy.intro[index],
    ownerBased: scopeCopy.ownerBased[index],
    optionsTitle: scopeCopy.optionsTitle[index],
    modules: translate(scopeModules),
    options: translate(scopeOptions),
  }
}
