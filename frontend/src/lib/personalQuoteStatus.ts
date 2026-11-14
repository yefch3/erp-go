import type {Inquiry, Quote} from './inquiryWorkspace'

export function personalQuotePresentation(inquiry:Pick<Inquiry,'personalQuoteStatus'>):{key:string;type:'success'|'warning'|'primary'} {
 switch(inquiry.personalQuoteStatus){
  case 'MY_QUOTED':return{key:'myQuoted',type:'success'}
  case 'UPDATED':return{key:'quoteUpdated',type:'primary'}
  default:return{key:'waitingForMe',type:'warning'}
 }
}

// A reference quote supplies only its content, never its record identity,
// author, submission state or permissions. Saving creates a separate record.
export function copyInquiryQuote(quote:Quote):Quote {
 return{id:'',version:0,kind:quote.kind,body:JSON.parse(JSON.stringify(quote.body)),authorId:'',author:'',submittedAt:'',updatedBy:'',updatedAt:'',canEdit:true,canDelete:false,historical:false}
}
