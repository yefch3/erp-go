export interface CustomerDocument {
 id:string;customerId:string;documentId:string;version:number;title:string;remark:string;fileName:string;contentType:string;sizeBytes:string;expiresOn:string;remindDays:number;reminderEnabled:boolean;uploadedByName:string;createdAt:string;current:boolean
}
// Date-only validity uses UTC calendar days, matching PostgreSQL CURRENT_DATE in this deployment.
export function documentExpiry(date:string,remindDays:number,now=new Date()):{label:string;type:'info'|'danger'|'warning'|'success'} {
 if(!date)return {label:'长期有效',type:'info'}
 const today=Date.UTC(now.getUTCFullYear(),now.getUTCMonth(),now.getUTCDate())
 const remaining=Math.round((Date.parse(date+'T00:00:00Z')-today)/86400000)
 if(remaining<0)return {label:`已过期 ${-remaining} 天`,type:'danger'}
 if(remaining===0)return {label:'今天到期',type:'warning'}
 return {label:`剩余 ${remaining} 天`,type:remaining<=remindDays?'warning':'success'}
}
