// Protobuf int64 accepts decimal strings, but not an empty UI selection.
export function contractItemPayload<T extends {productId:string}>(line:T):T {
  return {...line,productId:line.productId.trim()||'0'}
}
