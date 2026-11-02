import { describe,expect,it } from 'vitest'
import { fillFreightTable,freightFillPatch,freightSelection,missingFreightPrices,pricedFreightRates,selectAllFreightRates,undoFreightFill,unselectAllFreightRates } from './freightRateFill'
import type { FreightRate,Product } from './inquiryWorkspace'

const product=(id:string,unit='MT',name=`产品 ${id}`):Product=>({id,product:name,specification:'',quantity:'10',unit,delivery:'',weight:'',volume:'',packaging:'',remark:'',customFields:{}})
const rate=(productId:string,price='',currency='USD',unit='MT'):FreightRate=>({productId,price,currency,unit,remark:''})
const products=[product('a'),product('b','PCS'),product('c',''),product('d')]

describe('freight rate select-all',()=>{
  it('reports none, some and all for the header checkbox',()=>{
    expect(freightSelection(products,[])).toEqual({checked:false,indeterminate:false,count:0})
    expect(freightSelection(products,[rate('a'),rate('c')])).toEqual({checked:false,indeterminate:true,count:2})
    expect(freightSelection(products,products.map(p=>rate(p.id)))).toEqual({checked:true,indeterminate:false,count:4})
    expect(freightSelection([],[])).toEqual({checked:false,indeterminate:false,count:0})
  })
  it('ignores rates for products no longer in the table',()=>{
    expect(freightSelection(products,[rate('gone')])).toEqual({checked:false,indeterminate:false,count:0})
  })
  it('adds only missing rows with the per-row defaults and leaves filled rows alone',()=>{
    const filled=rate('b','35','EUR','PCS')
    const before=[filled]
    const out=selectAllFreightRates(products,before,'USD')
    expect(out.added).toEqual(['a','c','d'])
    expect(out.rates.map(r=>r.productId)).toEqual(['b','a','c','d'])
    expect(out.rates[0]).toBe(filled)
    expect(out.rates[1]).toEqual({productId:'a',price:'',currency:'USD',unit:'MT',remark:''})
    expect(out.rates[2].unit).toBe('MT')
    expect(before).toEqual([filled])
  })
  it('uses the editor currency as the default for added rows',()=>{
    expect(selectAllFreightRates([product('a')],[],'CNY').rates[0].currency).toBe('CNY')
    expect(selectAllFreightRates([product('a')],[],'').rates[0].currency).toBe('USD')
  })
  it('unselects only table rows and tells which priced rows would be dropped',()=>{
    const rates=[rate('a','12'),rate('b',''),rate('gone','9')]
    expect(pricedFreightRates(products,rates).map(r=>r.productId)).toEqual(['a'])
    expect(pricedFreightRates(products,[rate('a','  ')])).toEqual([])
    expect(unselectAllFreightRates(products,rates).map(r=>r.productId)).toEqual(['gone'])
    expect(rates).toHaveLength(3)
  })
})

describe('freight rate bulk fill',()=>{
  it('treats empty inputs as leave-alone and normalizes what is given',()=>{
    expect(freightFillPatch({price:' 35.5 ',currency:' eur ',unit:''})).toEqual({patch:{price:'35.5',currency:'EUR'}})
    expect(freightFillPatch({price:'',currency:'',unit:' CBM '})).toEqual({patch:{unit:'CBM'}})
    expect(freightFillPatch({price:'0',currency:'',unit:''})).toEqual({patch:{price:'0'}})
  })
  it('rejects an empty bar, a non-number price and a currency that is not three letters',()=>{
    expect(freightFillPatch({price:' ',currency:'',unit:''})).toEqual({error:'empty'})
    expect(freightFillPatch({price:'1,200',currency:'',unit:''})).toEqual({error:'price'})
    expect(freightFillPatch({price:'-3',currency:'',unit:''})).toEqual({error:'price'})
    expect(freightFillPatch({price:'',currency:'US',unit:''})).toEqual({error:'currency'})
    expect(freightFillPatch({price:'',currency:'U$D',unit:''})).toEqual({error:'currency'})
  })
  it('fills only ticked rows, overwriting by default, without mutating the input',()=>{
    const rates=[rate('a','10'),rate('b','',"USD",'PCS'),rate('gone','1')]
    const out=fillFreightTable(products,rates,{price:'35',currency:'EUR'},{selectAll:false,currency:'USD'})
    expect(out.count).toBe(2)
    expect(out.rates.map(r=>[r.productId,r.price,r.currency,r.unit])).toEqual([['a','35','EUR','MT'],['b','35','EUR','PCS'],['gone','1','USD','MT']])
    expect(out.rates[2]).toBe(rates[2])
    expect(rates[0].price).toBe('10')
    expect(out.undo.added).toEqual([])
  })
  it('ticks every row before filling when asked to',()=>{
    const out=fillFreightTable(products,[rate('b','8','USD','PCS')],{price:'20'},{selectAll:true,currency:'USD'})
    expect(out.count).toBe(4)
    expect(out.undo.added).toEqual(['a','c','d'])
    expect(out.rates.map(r=>[r.productId,r.price,r.unit])).toEqual([['b','20','PCS'],['a','20','MT'],['c','20','MT'],['d','20','MT']])
  })
  it('undoes one fill: restores overwritten cells and drops rows it ticked',()=>{
    const original=[rate('b','8','USD','PCS')]
    const out=fillFreightTable(products,original,{price:'20',unit:'CBM'},{selectAll:true,currency:'USD'})
    const restored=undoFreightFill(out.rates,out.undo)
    expect(restored).toEqual(original)
  })
  it('keeps later edits to other cells and skips rows that were removed since',()=>{
    const out=fillFreightTable(products,[rate('a','5'),rate('b','6')],{price:'20'},{selectAll:false,currency:'USD'})
    const edited=out.rates.filter(r=>r.productId!=='b').map(r=>r.productId==='a'?{...r,unit:'CBM'}:r)
    expect(undoFreightFill(edited,out.undo)).toEqual([{productId:'a',price:'5',currency:'USD',unit:'CBM',remark:''}])
  })
})

describe('freight rates missing a price before save',()=>{
  it('lists ticked table rows whose price is blank, in table order',()=>{
    const rates=[rate('d',''),rate('a','12'),rate('b','  '),rate('gone','')]
    expect(missingFreightPrices(products,rates).map(p=>p.id)).toEqual(['b','d'])
    expect(missingFreightPrices(products,[rate('a','0')])).toEqual([])
  })
})
