import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {api} from '../api'
import {type Audit as AuditEvent,type Page,type Service,date} from '../model'
import {Heading,ErrorBox,Loading,Empty,Badge,Pager} from '../components/shared'
import {Button} from '../components/ui/button'
import {Dialog} from '../components/ui/dialog'
export function Audit(){
 const [offset,setOffset]=useState(0),[revision,setRevision]=useState<number|null>(null)
 const q=useQuery({queryKey:['audit',offset],queryFn:()=>api<Page<AuditEvent>>(`/audit?offset=${offset}`),refetchInterval:15000})
 const draft=useQuery({queryKey:['draft-revision',revision],queryFn:()=>api<{revision:number;services:Service[]}>(`/draft/revisions/${revision}`),enabled:revision!==null})
 return <><Heading title="审计" description="追溯服务变更、配置校验和每一次发布操作。"/><ErrorBox error={q.error} onRetry={()=>void q.refetch()}/><section className="card">{q.isPending?<Loading/>:q.data?.items.length?<div className="table-wrap"><table><thead><tr><th>时间</th><th>操作者</th><th>操作 / 对象</th><th>结果</th><th>关联版本</th></tr></thead><tbody>{q.data.items.map(a=><tr key={a.id}><td>{date(a.time)}</td><td>{a.actor}</td><td><strong>{a.action}</strong><small className="mono">{a.object}</small>{a.rollback_id&&<small>回滚来源：{a.rollback_id}</small>}</td><td><Badge>{a.result||'完成'}</Badge>{a.error_class&&<small>错误分类：{a.error_class}</small>}</td><td>{a.action.startsWith('service.')||a.action==='validation'||a.action==='configuration.import'||a.action.startsWith('deployment.')||a.action.startsWith('rollback.')?<Button variant="ghost" onClick={()=>setRevision(a.revision)}>草稿 r{a.revision}</Button>:'—'} {a.version?`/ v${a.version}`:''}</td></tr>)}</tbody></table></div>:<Empty title="暂无审计记录"/>}<Pager offset={offset} count={q.data?.items.length??0} onChange={setOffset}/></section><Dialog open={revision!==null} onOpenChange={v=>{if(!v)setRevision(null)}} title={`历史草稿 r${revision}`} wide><ErrorBox error={draft.error} onRetry={()=>void draft.refetch()}/>{draft.isPending?<Loading/>:draft.data&&<pre>{JSON.stringify(draft.data.services,null,2)}</pre>}</Dialog></>
}
