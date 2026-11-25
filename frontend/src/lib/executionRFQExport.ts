interface InquiryRequirement {
  id: string
  productName: string
  spec: string
  availableQty: string
  uomCode: string
  contractItemRemark?: string
}

const materialLabel = /^(?:材质(?:\s*[/／]\s*标准)?|材料(?:标准)?|牌号|标准|材质标准|执行标准|material(?:[\s_/-]+standard)?|grade|standard)$/i
const technicalLabel = /^(?:技术要求|表面(?:处理|要求)|包装(?:要求)?|technical[\s_]+requirements?|surface(?:[\s_]+requirements?)?|packag(?:e|ing)(?:[\s_]+requirements?)?)$/i
const internalLabel = /^(?:客户(?:名称|姓名|信息|编号|联系人|电话|邮箱|地址)?|合同号|(?:外销|客户|工厂|采购|销售)(?:单价|价格|报价|金额)|报价|单价|币种|成本|利润|(?:内部|售前|销售|合同明细)?备注|customer(?:[\s_]+(?:name|id|contact|email|phone|address|price))?|contract[\s_]+(?:no|number|id)|(?:sales|supplier|factory|internal)[\s_]+(?:price|cost|remarks?)|unit[\s_]+price|price|cost|profit|currency|remarks?|notes?)$/i

export function executionRFQExportRows(requirements: InquiryRequirement[]) {
  return requirements.filter(line => Number(line.availableQty) > 0).map(line => {
    const material: string[] = []
    const specification: string[] = []
    const technical: string[] = []
    for (const fragment of (line.spec ?? '').split(/[;；\r\n]+/).map(value => value.trim()).filter(Boolean)) {
      const label = fragment.match(/^([^:：]+)[:：]\s*(.+)$/)
      if (label && internalLabel.test(label[1].trim())) continue
      if (label && materialLabel.test(label[1].trim())) {
        material.push(label[2])
      } else if (label && technicalLabel.test(label[1].trim())) {
        technical.push(fragment)
      } else {
        specification.push(fragment)
      }
    }
    return {
      line: {
        requirement_id: line.id,
        available_qty: line.availableQty,
        product_name: line.productName ?? '',
        material_standard: material.join(' / '),
        specification: specification.join('\n'),
        technical_requirements: technical.join('\n'),
      },
      unit: line.uomCode,
      sourceRemark: line.contractItemRemark?.trim() ?? '',
    }
  })
}
