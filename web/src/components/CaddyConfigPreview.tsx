import {useId,useMemo,useState,type ReactNode} from 'react'
import {diffJSON,formatJSON,type DiffLine} from '../config-diff'
import {date} from '../model'
import {Button} from './ui/button'

type Props={runtimeConfig:unknown;config:unknown;runtimeHash:string;hash:string;readAt?:string;redactedFields?:string[];runtimeRedactedFields?:string[];stale?:boolean}
export function ConfigPrivacyNotice(){return <p className="notice notice-neutral">敏感值已替换为 [REDACTED]；未知管理接口、存储或模块的参数按保守规则脱敏。此 JSON 不能直接用于恢复。差异仅比较可见内容，运行指纹仍按完整配置计算。</p>}

function Highlight({text,other}:{text:string;other?:string}){
 if(other===undefined)return <>{text}</>
 let start=0,end=0
 while(start<text.length&&start<other.length&&text[start]===other[start])start++
 while(end<text.length-start&&end<other.length-start&&text[text.length-end-1]===other[other.length-end-1])end++
 return <>{text.slice(0,start)}<mark>{text.slice(start,text.length-end)}</mark>{text.slice(text.length-end)}</>
}

type Row={before?:DiffLine;after?:DiffLine;context?:boolean;hidden?:number}
function rowsFor(lines:DiffLine[],showAll:boolean):Row[]{
 const result:Row[]=[]
 for(let i=0;i<lines.length;){
  if(lines[i].kind==='context'){
   const start=i
   while(i<lines.length&&lines[i].kind==='context')i++
   const leading=start===0?0:3,trailing=i===lines.length?0:3
   if(!showAll&&i-start>leading+trailing+1){
    for(let j=start;j<start+leading;j++)result.push({before:lines[j],after:lines[j],context:true})
    result.push({hidden:i-start-leading-trailing})
    for(let j=i-trailing;j<i;j++)result.push({before:lines[j],after:lines[j],context:true})
   }else for(let j=start;j<i;j++)result.push({before:lines[j],after:lines[j],context:true})
  }else{
   const removed:DiffLine[]=[],added:DiffLine[]=[]
   while(i<lines.length&&lines[i].kind!=='context'){(lines[i].kind==='removed'?removed:added).push(lines[i]);i++}
   for(let j=0;j<Math.max(removed.length,added.length);j++)result.push({before:removed[j],after:added[j]})
  }
 }
 return result
}
function DiffCell({line,other,side}:{line?:DiffLine;other?:DiffLine;side:'before'|'after'}){
 return <div className={`config-diff-cell ${line?`config-line-${line.kind}`:'config-line-empty'}`}>
  <span className="config-line-number" aria-label={side==='before'?'原行号':'新行号'}>{line?.[side]??''}</span>
  <span className="config-line-sign" aria-label={line?.kind==='added'?'新增':line?.kind==='removed'?'删除':undefined}>{line?.kind==='added'?'+':line?.kind==='removed'?'-':' '}</span>
  <code>{line&&<Highlight text={line.text} other={line.kind!=='context'?other?.text:undefined}/>}</code>
 </div>
}
function ConfigDiff({before,after,sameHash}:{before:unknown;after:unknown;sameHash:boolean}){
 const [layout,setLayout]=useState<'split'|'unified'>('split'),[showAll,setShowAll]=useState(false)
 const diff=useMemo(()=>diffJSON(before,after),[before,after])
 const added=diff.lines.filter(l=>l.kind==='added').length,removed=diff.lines.filter(l=>l.kind==='removed').length
 const rows=useMemo(()=>rowsFor(diff.lines,showAll),[diff,showAll])
 return <>
  <div className="config-diff-toolbar"><p><span className="text-danger">−{removed} 行删除</span><span className="config-added-count">+{added} 行新增</span></p><div className="config-view-controls"><Button size="sm" variant="outline" aria-pressed={layout==='split'} onClick={()=>setLayout('split')}>并排视图</Button><Button size="sm" variant="outline" aria-pressed={layout==='unified'} onClick={()=>setLayout('unified')}>统一视图</Button><label><input type="checkbox" checked={showAll} onChange={e=>setShowAll(e.target.checked)}/>显示未修改行</label></div></div>
  {!added&&!removed?<p className="config-comparison-empty" role="status">{sameHash?'没有配置差异。': '可见配置相同，但完整配置指纹不同；请核对脱敏字段或刷新后重新校验。'}</p>:<>
   {diff.simplified&&<p className="config-comparison-note">变更范围较大，部分内容按整块删除和新增展示；两侧配置均完整保留。</p>}
   <div className={`config-diff config-diff-${layout}`} role="region" aria-label="Caddy 配置差异" tabIndex={0}>
    <div className="config-diff-head"><span>当前运行配置（− 删除）</span><span>待发布配置（+ 新增）</span></div>
    {rows.map((row,i)=>row.hidden?<button type="button" key={i} className="config-diff-fold" onClick={()=>setShowAll(true)}>展开 {row.hidden} 行未修改内容</button>:layout==='split'?<div key={i} className={`config-diff-row${row.context?' config-diff-context':''}`}><DiffCell line={row.before} other={row.after} side="before"/><DiffCell line={row.after} other={row.before} side="after"/></div>:<div key={i}>
     {row.context?<div className="config-unified-line"><span className="config-line-number" aria-label="原行号">{row.before?.before}</span><DiffCell line={row.after} side="after"/></div>:<>{row.before&&<div className="config-unified-line"><span className="config-line-number" aria-label="原行号">{row.before.before}</span><DiffCell line={row.before} other={row.after} side="after"/></div>}{row.after&&<div className="config-unified-line"><span className="config-line-number"/><DiffCell line={row.after} other={row.before} side="after"/></div>}</>}
    </div>)}
   </div>
  </>}
 </>
}
function ReadOnlyJSON({value}:{value:unknown}){const text=useMemo(()=>formatJSON(value),[value]);return <pre className="config-full-json" tabIndex={0}>{text}</pre>}
function RedactionLocations({fields,label}:{fields:string[];label:string}){return fields.length?<details className="config-redactions"><summary>{label} · {fields.length} 处脱敏</summary><ul>{fields.map(field=><li key={field}><code>{field}</code></li>)}</ul></details>:null}

export function CaddyConfigPreview({runtimeConfig,config,runtimeHash,hash,readAt,redactedFields=[],runtimeRedactedFields=[],stale=false}:Props){
 const [tab,setTab]=useState('diff'),id=useId()
 const tabs=[{id:'diff',label:'发布前对比'},{id:'runtime',label:'当前运行配置'},{id:'candidate',label:'待发布配置'}]
 const unavailable=runtimeConfig===undefined||runtimeConfig===null
 let content:ReactNode
 if(tab==='candidate')content=<ReadOnlyJSON value={config}/>
 else if(unavailable)content=<p className="config-comparison-empty" role="status">未获取到当前运行配置，请刷新预览后重试。无法进行发布前对比。</p>
 else content=tab==='runtime'?<ReadOnlyJSON value={runtimeConfig}/>:<ConfigDiff before={runtimeConfig} after={config} sameHash={runtimeHash===hash}/>
 return <section className="config-preview" aria-label="完整 Caddy 配置">
  <div className="config-preview-heading"><h3>完整 Caddy 配置 · 只读</h3><p>对比实际运行配置与本次待发布候选，数组顺序保持原样。</p></div>
  <div className="config-fingerprints"><p>运行指纹：<code>{runtimeHash||'未知'}</code>{readAt&&<> · 读取于 {date(readAt)}</>}</p><p>候选指纹：<code>{hash}</code></p></div>
  {stale&&<p className="notice notice-amber">预览刷新失败，以下是上次成功读取的配置，不能代表当前状态。请刷新预览并重新校验。</p>}
  {(redactedFields.length>0||runtimeRedactedFields.length>0)&&<><ConfigPrivacyNotice/><RedactionLocations fields={runtimeRedactedFields} label="当前运行配置"/><RedactionLocations fields={redactedFields} label="待发布配置"/></>}
  <div className="config-tabs" role="tablist" aria-label="配置预览方式">{tabs.map((item,index)=><button key={item.id} type="button" role="tab" id={`${id}-${item.id}`} aria-controls={`${id}-panel`} aria-selected={tab===item.id} tabIndex={tab===item.id?0:-1} onClick={()=>setTab(item.id)} onKeyDown={e=>{let next:number|undefined;if(e.key==='ArrowRight')next=(index+1)%tabs.length;if(e.key==='ArrowLeft')next=(index+tabs.length-1)%tabs.length;if(e.key==='Home')next=0;if(e.key==='End')next=tabs.length-1;if(next!==undefined){e.preventDefault();setTab(tabs[next].id);document.getElementById(`${id}-${tabs[next].id}`)?.focus()}}}>{item.label}</button>)}</div>
  <div role="tabpanel" id={`${id}-panel`} aria-labelledby={`${id}-${tab}`} tabIndex={0}>{content}</div>
 </section>
}
