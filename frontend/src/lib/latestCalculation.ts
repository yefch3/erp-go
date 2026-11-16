// Debounce edits, discard outdated responses, and let saving flush the latest work.
export function latestCalculation<I,R>(options:{delay?:number;run:(input:I)=>Promise<R>;apply:(result:R,input:I)=>void;fail:(error:unknown)=>void;pending:(value:boolean)=>void}){
 let generation=0,disposed=false,timer:ReturnType<typeof setTimeout>|undefined
 let latest:{generation:number;input:I;promise?:Promise<boolean>}|undefined
 function start(job:NonNullable<typeof latest>){
  if(job.promise)return job.promise
  job.promise=(async()=>{
   try{const result=await options.run(job.input);if(!disposed&&generation===job.generation)options.apply(result,job.input);return true}
   catch(error){if(!disposed&&generation===job.generation)options.fail(error);return false}
   finally{if(!disposed&&generation===job.generation)options.pending(false)}
  })()
  return job.promise
 }
 return {
  schedule(input:I){
   if(disposed)return
   clearTimeout(timer);const job={generation:++generation,input};latest=job
   options.pending(true);timer=setTimeout(()=>{timer=undefined;void start(job)},options.delay??500)
  },
  async flush(){
   clearTimeout(timer);timer=undefined
   while(!disposed&&latest){const job=latest;const ok=await start(job);if(latest===job)return ok}
   return !disposed
  },
  cancel(){clearTimeout(timer);timer=undefined;generation++;latest=undefined;options.pending(false)},
  dispose(){disposed=true;clearTimeout(timer);generation++;latest=undefined},
 }
}
