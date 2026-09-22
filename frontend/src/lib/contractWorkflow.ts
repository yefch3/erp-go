export const contractOperations = [
 {action:'delete',label:'删除草稿',description:'用于尚未提交的手工草稿。删除后无法从业务列表恢复；已提交或报价生成的合同请使用作废。'},
 {action:'withdraw',label:'撤回提交',description:'用于上级尚未确认完成的合同。撤回后可修改再提交，审批记录保留。'},
 {action:'void',label:'作废合同',description:'用于尚未签署且决定不再继续的合同。停止办理，保留合同及作废原因。'},
 {action:'pause',label:'暂停执行',description:'临时暂停新增采购、订舱和发运。已发生业务仍需登记、收付款及结算。'},
 {action:'resume',label:'恢复执行',description:'暂停原因已解决时使用。核对合同执行条件后继续剩余业务。'},
 {action:'change',label:'合同变更',description:'调整产品、数量、价格、交期或付款条件。保留原版本，重新确认、签署及财务放行。'},
 {action:'terminate',label:'申请终止',description:'用于已签署并开始执行的合同。上级确认后停止剩余履约，采购、物流和账款仍需分别善后。'},
 {action:'complete',label:'合同结案',description:'履约或终止善后完成后使用。核对采购、物流、应收应付及退款事项，再归档。'},
] as const
export type ContractAction=typeof contractOperations[number]['action']
export function contractActionUnavailable(action:ContractAction,c:{status:string;entrySource?:string;quotationId?:string;quoteNo?:string;currentVersionId?:string;versionStatus?:string;versionNo?:number}):string{
 const signed=!!c.currentVersionId&&c.currentVersionId!=='0'
 const state=c.status
 switch(action){
 case 'delete':return state==='DRAFT'&&!signed&&!c.quoteNo&&(!c.quotationId||c.quotationId==='0')?'':'仅限未提交的手工草稿'
 case 'withdraw':return state==='PENDING_APPROVAL'?'':'当前不在上级确认中'
 case 'void':return !signed&&['DRAFT','PENDING_SIGN','REJECTED'].includes(state)?'':'仅限尚未签署且不在审批中的合同'
 case 'pause':return ['EXECUTING','EFFECTIVE'].includes(state)?'':'仅限执行中的合同'
 case 'resume':return state==='PAUSED'?'':'仅限暂停中的合同'
 case 'change':return ['EXECUTING','EFFECTIVE'].includes(state)&&c.versionStatus==='APPROVED'?'':'需要当前合同正在执行且没有待处理变更'
 case 'terminate':return state==='TERMINATING'?'':['EXECUTING','EFFECTIVE','PAUSED'].includes(state)&&c.versionStatus==='APPROVED'?'':'需要执行中或暂停中的合同，且没有待处理变更'
 case 'complete':return ['EXECUTING','EFFECTIVE','TERMINATED'].includes(state)&&c.versionStatus==='APPROVED'?'':'需要履约或终止善后完成，且没有待处理变更'
 }
}
