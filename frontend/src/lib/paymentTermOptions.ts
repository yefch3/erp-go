export const paymentTermOptions = ['T/T', '30%预付款，70%发货前付清', '信用证 L/C', '货到付款', '月结 30 天', '月结 60 天']

export function suggestPaymentTerms(query: string, done: (items: {value: string}[]) => void) {
 const search = query.trim().toLocaleLowerCase()
 done(paymentTermOptions.filter(term => term.toLocaleLowerCase().includes(search)).map(value => ({value})))
}
