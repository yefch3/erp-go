export type DeliveryOrder = { status:string; fulfillmentMode?:string }
export function deliveryStatusKey(order:DeliveryOrder):string {
 if(order.status==='PARTIALLY_RECEIVED')return `purchaseFulfillment.${order.fulfillmentMode==='WAREHOUSE'?'partialWarehouse':'partialDelivery'}`
 if(order.status==='RECEIVED')return `purchaseFulfillment.${order.fulfillmentMode==='WAREHOUSE'?'completeWarehouse':'completeDelivery'}`
 return `orders.statuses.${order.status}`
}
export function deliveryDestination(order:{fulfillmentMode?:string;warehouseName?:string;deliveryPortCode?:string;deliveryPortName?:string;deliveryAddress?:string}):string {
 return order.fulfillmentMode==='WAREHOUSE'?order.warehouseName||'待确认仓库':[order.deliveryPortCode,order.deliveryPortName].filter(Boolean).join(' · ')||order.deliveryAddress||'待确认交接地点'
}
