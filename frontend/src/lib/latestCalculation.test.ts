import {afterEach,describe,expect,it,vi} from 'vitest'
import {latestCalculation} from './latestCalculation'

afterEach(()=>vi.useRealTimers())
const deferred=<T>()=>{let resolve!:(value:T)=>void, reject!:(reason:unknown)=>void;const promise=new Promise<T>((a,b)=>{resolve=a;reject=b});return{promise,resolve,reject}}
describe('automatic calculation scheduling',()=>{
 it('debounces typing and flushes immediately when saving',async()=>{
  vi.useFakeTimers();const run=vi.fn(async(input:number)=>input*2),apply=vi.fn()
  const task=latestCalculation({run,apply,fail:vi.fn(),pending:vi.fn()})
  task.schedule(1);await vi.advanceTimersByTimeAsync(300);task.schedule(2)
  await vi.advanceTimersByTimeAsync(499);expect(run).not.toHaveBeenCalled()
  expect(await task.flush()).toBe(true);expect(run).toHaveBeenCalledTimes(1);expect(run).toHaveBeenCalledWith(2);expect(apply).toHaveBeenCalledWith(4,2)
 })
 it('never applies an old response or old failure over a newer choice',async()=>{
  vi.useFakeTimers();const first=deferred<number>(),second=deferred<number>(),apply=vi.fn(),fail=vi.fn(),pending=vi.fn()
  const task=latestCalculation({run:(id:number)=>id===1?first.promise:second.promise,apply,fail,pending})
  task.schedule(1);await vi.advanceTimersByTimeAsync(500);task.schedule(2);await vi.advanceTimersByTimeAsync(500)
  second.resolve(20);expect(await task.flush()).toBe(true);first.reject(new Error('obsolete'));await Promise.resolve()
  expect(apply).toHaveBeenCalledTimes(1);expect(apply).toHaveBeenCalledWith(20,2);expect(fail).not.toHaveBeenCalled();expect(pending).toHaveBeenLastCalledWith(false)
 })
 it('saving waits for edits made while the previous calculation is running',async()=>{
  vi.useFakeTimers();const first=deferred<number>(),second=deferred<number>(),apply=vi.fn()
  const task=latestCalculation({run:(id:number)=>id===1?first.promise:second.promise,apply,fail:vi.fn(),pending:vi.fn()})
  task.schedule(1);const saving=task.flush();task.schedule(2);first.resolve(10);await Promise.resolve();second.resolve(20)
  expect(await saving).toBe(true);expect(apply).toHaveBeenCalledTimes(1);expect(apply).toHaveBeenCalledWith(20,2)
 })
 it('reports current failures, supports a new retry, and ignores disposed requests',async()=>{
  const apply=vi.fn(),fail=vi.fn(),request=deferred<number>()
  const task=latestCalculation({run:vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(2).mockReturnValueOnce(request.promise),apply,fail,pending:vi.fn()})
  task.schedule(1);expect(await task.flush()).toBe(false);expect(fail).toHaveBeenCalledOnce()
  task.schedule(2);expect(await task.flush()).toBe(true)
  task.schedule(3);const closing=task.flush();task.dispose();request.resolve(3);await closing
  expect(apply).toHaveBeenCalledTimes(1);expect(apply).toHaveBeenCalledWith(2,2)
 })
})
