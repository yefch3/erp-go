import { ElMessageBox } from 'element-plus'
import { get } from '../api'

interface ImpactItem { code:string; label:string; count:string|number }
interface ImpactResponse { items:ImpactItem[]; total:string|number }
type Translate=(key:string,params?:Record<string,unknown>)=>string

// 停用前先汇总业务影响，并强制填写原因；确认只改变主数据状态，不删除历史记录。
export async function confirmDeactivation(impactEndpoint:string,name:string,t:Translate):Promise<string>{
  const impact=await get<ImpactResponse>(impactEndpoint)
  const lines=(impact.items??[]).map(item=>`${item.label}: ${item.count}`).join('\n')
  const {value}=await ElMessageBox.prompt(
    `${t('common.deactivationImpact',{name,total:Number(impact.total||0)})}${lines?`\n${lines}`:''}\n${t('common.historyRetained')}`,
    t('common.confirmDeactivate'),
    {type:'warning',inputPlaceholder:t('common.lifecycleReasonPlaceholder'),inputValidator:value=>String(value||'').trim()?true:t('common.lifecycleReasonRequired'),confirmButtonText:t('common.confirmDeactivate'),cancelButtonText:t('common.cancel')},
  )
  return String(value).trim()
}

export async function promptActivationReason(name:string,t:Translate):Promise<string>{
  const {value}=await ElMessageBox.prompt(t('common.activationReason',{name}),t('common.confirmActivate'),{inputPlaceholder:t('common.lifecycleReasonPlaceholder'),inputValidator:value=>String(value||'').trim()?true:t('common.lifecycleReasonRequired'),confirmButtonText:t('common.confirmActivate'),cancelButtonText:t('common.cancel')})
  return String(value).trim()
}
