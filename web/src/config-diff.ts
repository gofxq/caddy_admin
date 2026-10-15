export type DiffLine={kind:'context'|'removed'|'added';text:string;before?:number;after?:number}
type Edit=Pick<DiffLine,'kind'|'text'>

// Sort object keys only: array order controls Caddy route precedence.
export function formatJSON(value:unknown):string {
 return JSON.stringify(value,(_key,item)=>item&&typeof item==='object'&&!Array.isArray(item)?Object.fromEntries(Object.entries(item).sort(([a],[b])=>a<b?-1:a>b?1:0)):item,2)??'null'
}

// Myers shortest edit script with a bounded trace. A very large replacement
// falls back to whole-block removals/additions, never a truncated comparison.
function edits(before:string[],after:string[]):{edits:Edit[];simplified:boolean}{
 const trace:Map<number,number>[]=[]
 const frontier=new Map<number,number>([[1,0]])
 let work=0
 for(let depth=0;depth<=before.length+after.length;depth++){
  work+=2*depth+1
  if(work>250000)break
  trace.push(new Map(frontier))
  for(let diagonal=-depth;diagonal<=depth;diagonal+=2){
   let x=diagonal===-depth||(diagonal!==depth&&(frontier.get(diagonal-1)??-Infinity)<(frontier.get(diagonal+1)??-Infinity))?(frontier.get(diagonal+1)??0):(frontier.get(diagonal-1)??0)+1
   let y=x-diagonal
   while(x<before.length&&y<after.length&&before[x]===after[y]){x++;y++}
   frontier.set(diagonal,x)
   if(x>=before.length&&y>=after.length){
    const result:Edit[]=[]
    for(let d=depth;d>=0;d--){
     const previous=trace[d],k=x-y
     const previousK=k===-d||(k!==d&&(previous.get(k-1)??-Infinity)<(previous.get(k+1)??-Infinity))?k+1:k-1
     const previousX=previous.get(previousK)??0,previousY=previousX-previousK
     while(x>previousX&&y>previousY){result.push({kind:'context',text:before[--x]});y--}
     if(d>0){if(x===previousX)result.push({kind:'added',text:after[--y]});else result.push({kind:'removed',text:before[--x]})}
    }
    return {edits:result.reverse(),simplified:false}
   }
  }
 }
 return {edits:[...before.map(text=>({kind:'removed' as const,text})),...after.map(text=>({kind:'added' as const,text}))],simplified:true}
}

export function diffJSON(before:unknown,after:unknown):{lines:DiffLine[];simplified:boolean}{
 const a=formatJSON(before).split('\n'),b=formatJSON(after).split('\n')
 let start=0,end=0
 while(start<a.length&&start<b.length&&a[start]===b[start])start++
 while(end<a.length-start&&end<b.length-start&&a[a.length-1-end]===b[b.length-1-end])end++
 const core=edits(a.slice(start,a.length-end),b.slice(start,b.length-end))
 const all:Edit[]=[...a.slice(0,start).map(text=>({kind:'context' as const,text})),...core.edits,...a.slice(a.length-end).map(text=>({kind:'context' as const,text}))]
 let oldLine=0,newLine=0
 const lines:DiffLine[]=all.map(line=>({...line,...(line.kind!=='added'?{before:++oldLine}:{}),...(line.kind!=='removed'?{after:++newLine}:{})}))
 return {lines,simplified:core.simplified}
}
