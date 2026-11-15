import { describe, expect, it } from 'vitest'
import { manualCargoError, manualCargoLines, type ManualCargoLine } from './manualShippingForm'
const line=(values:Partial<ManualCargoLine>={}):ManualCargoLine=>({productCode:'',productName:'',specification:'',quantity:'',uomCode:'',remark:'',...values})
describe('manual shipping form',()=>{
 it('allows an empty draft but rejects a partially filled row or invalid quantities',()=>{
  expect(manualCargoLines([line()])).toEqual([])
  expect(manualCargoError([line()])).toBe('')
  for(const quantity of ['0','-1','1.12345','x',''])expect(manualCargoError([line({productName:'Steel',quantity,uomCode:'MT'})])).not.toBe('')
  expect(manualCargoError([line({productName:'Steel'})])).not.toBe('')
 })
 it('keeps populated goods, notes and precision when saving and editing',()=>{
  const lines=[line(),line({productName:' Steel ',quantity:' 1.2345 ',uomCode:' MT ',specification:'3mm',remark:' Keep note '})]
  expect(manualCargoError(lines)).toBe('')
  expect(manualCargoLines(lines)).toEqual([line({productName:'Steel',quantity:'1.2345',uomCode:'MT',specification:'3mm',remark:'Keep note'})])
 })
})
