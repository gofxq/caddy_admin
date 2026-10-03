import {isStaticDemo} from '../request'
import {useEffect,useState} from 'react'
import {useMutation,useQuery,useQueryClient} from '@tanstack/react-query'
import {Link} from '@tanstack/react-router'
import {api} from '../api'
import {type Preview,type Deployment,type Page,type Service,exposureChanges,upstream,statusLabel,date} from '../model'
import {Button} from '../components/ui/button'
import {Dialog} from '../components/ui/dialog'
import {Heading,Loading,ErrorBox,Empty,Badge,History,Pager} from '../components/shared'

function ConfigPrivacyNotice(){return <p className="notice notice-neutral">为保护部署凭据，管理接口与证书存储配置已隐藏；这里的 JSON 不能直接用于恢复，运行指纹仍按完整配置计算。</p>}
function serviceDescription(s?:Service){return s?`${s.hostname} · ${upstream(s)} · ${s.enabled?'启用':'停用'} · 名称：${s.name} · 备注：${s.notes||'无'}`:'未配置'}
function DeploymentTask({deployment,error,onRefresh,onDetail}:{deployment?:Deployment;error:unknown;onRefresh:()=>void;onDetail:(id:string)=>void}){
 if(!deployment)return null
 const content={
  applying:{tone:'notice-amber',title:'正在后台发布',body:'关闭页面不会取消。本任务完成前不能再次校验或发布。'},
  success:{tone:'notice-green',title:`发布 v${deployment.version} ${isStaticDemo?'已模拟成功':'已成功上线'}`,body:`完成时间：${date(deployment.finished)}`},
  failed:{tone:'notice-red',title:`发布 v${deployment.version} 失败`,body:'候选配置未替换原运行配置。请刷新预览并重新校验。'},
  uncertain:{tone:'notice-red',title:`发布 v${deployment.version} 的实际状态待核对`,body:'不要重复发布。系统正在核对实际运行状态；在确认前校验和发布保持禁用。'},
 }[deployment.status]??{tone:'notice-amber',title:`发布 v${deployment.version}`,body:deployment.status}
 return <section className="card"><div className="section-heading"><div><h2>当前 / 最近一次发布</h2><p>任务 ID：{deployment.id}</p></div><Badge tone={deployment.status==='success'?'green':deployment.status==='failed'||deployment.status==='uncertain'?'red':'amber'}>{statusLabel[deployment.status]??deployment.status}</Badge></div><div className={`notice ${content.tone} inset`}><div><strong>{content.title}</strong><p>{content.body}</p>{deployment.error&&<small>{deployment.error}</small>}</div></div>{!!error&&<div className="notice notice-amber inset">任务状态查询暂时失败，已保留任务 ID 和最后状态；恢复后会继续查询。</div>}<div className="release-actions"><Button variant="outline" onClick={onRefresh}>刷新状态</Button><Button variant="outline" onClick={()=>onDetail(deployment.id)}>查看详情</Button>{deployment.status==='uncertain'&&<a className="button button-outline" href="/">前往概览</a>}</div></section>
}
function changeSummary(changes:Preview['changes']){const counts={added:0,updated:0,enabled:0,disabled:0,deleted:0};for(const change of changes){if(change.kind in counts)counts[change.kind as keyof typeof counts]++}return counts}
export function Deployments(){
 const qc=useQueryClient()
 const [rollback,setRollback]=useState(''),[offset,setOffset]=useState(0),[validated,setValidated]=useState<Preview|null>(null)
 const [confirm,setConfirm]=useState(false),[confirmDrift,setConfirmDrift]=useState(false),[confirmExposure,setConfirmExposure]=useState(false),[detail,setDetail]=useState(''),[idempotency,setIdempotency]=useState(''),[activeID,setActiveID]=useState(''),[pinned,setPinned]=useState(false)
 const [clock,setClock]=useState(Date.now())
 useEffect(()=>{const timer=window.setInterval(()=>setClock(Date.now()),1000);return()=>window.clearInterval(timer)},[])
 const preview=useQuery({queryKey:['preview',rollback],queryFn:()=>api<Preview>(`/draft/preview${rollback?`?rollback=${rollback}`:''}`),refetchInterval:15000})
 const history=useQuery({queryKey:['deployments',offset],queryFn:()=>api<Page<Deployment>>(`/deployments?offset=${offset}`),refetchInterval:3000})
 const latest=useQuery({queryKey:['deployments',0],queryFn:()=>api<Page<Deployment>>('/deployments?offset=0'),refetchInterval:3000,enabled:offset!==0})
 const latestItems=(offset===0?history.data:latest.data)?.items
 useEffect(()=>{if(!pinned&&latestItems?.[0])setActiveID(latestItems[0].id)},[latestItems,pinned])
 const task=useQuery({queryKey:['deployment-task',activeID],queryFn:()=>api<{deployment:Deployment}>(`/deployments/${activeID}`),enabled:!!activeID,refetchInterval:query=>['applying','uncertain'].includes((query.state.data?.deployment??latestItems?.find(item=>item.id===activeID))?.status??'')?2000:false})
 const selected=useQuery({queryKey:['deployment',detail],queryFn:()=>api<{deployment:Deployment;config:unknown;config_redacted_fields?:string[]}>(`/deployments/${detail}`),enabled:!!detail})
 const currentTask=task.data?.deployment??history.data?.items.find(item=>item.id===activeID)
 const locked=currentTask?.status==='applying'||currentTask?.status==='uncertain'||latestItems?.some(item=>item.status==='applying'||item.status==='uncertain')===true
 useEffect(()=>{if(currentTask&&['success','failed','uncertain'].includes(currentTask.status)){for(const key of ['deployments','overview','services','settings'])void qc.invalidateQueries({queryKey:[key]})}},[currentTask?.id,currentTask?.status,qc])
 const validate=useMutation({mutationFn:()=>api<Preview>('/draft/validate',{method:'POST',body:{revision:preview.data!.revision,rollback_id:rollback}}),onMutate:()=>{setValidated(null)},onSuccess:p=>{setValidated(p);setClock(Date.now());setIdempotency(crypto.randomUUID());qc.setQueryData(['preview',rollback],p)}})
 const p=preview.data
 const expiry=Date.parse(validated?.validation_expires_at??'')
 const reason=preview.isError?'预览连接失败，请刷新并重新校验':!validated?'请先校验配置':!Number.isFinite(expiry)||clock>=expiry?'校验已到期，请重新校验':!p||validated.hash!==p.hash||validated.revision!==p.revision||validated.runtime_hash!==p.runtime_hash||validated.rollback_id!==rollback?'候选或运行配置已变化，请重新校验':''
 const publish=useMutation({mutationFn:()=>{
  if(reason||Date.now()>=expiry)throw new Error(reason||'校验已到期，请重新校验')
  return api<Deployment>('/deployments',{method:'POST',body:{validation_id:validated!.validation_id,revision:validated!.revision,expected_hash:validated!.runtime_hash,idempotency_key:idempotency,confirm_drift:confirmDrift,confirm_exposure:confirmExposure}})
 },onSuccess:d=>{setConfirm(false);setActiveID(d.id);setPinned(true);qc.setQueryData(['deployment-task',d.id],{deployment:d})},onError:()=>{setPinned(false);void qc.invalidateQueries({queryKey:['deployments']})}})
 const canPublish=!reason&&!publish.isPending&&!validate.isPending&&!locked
 function rollbackTo(id:string){setRollback(id);setValidated(null);setConfirm(false);validate.reset();publish.reset()}
 const exposure=!!(validated?.settings&&validated.active_settings&&(exposureChanges(validated.active_settings,validated.settings)||validated.changes.some(c=>c.after?.enabled&&validated.settings.domains.find(d=>d.id===c.after?.domain_id)?.access==='internet'&&(c.kind==='added'||!c.before?.enabled||c.before.hostname!==c.after.hostname))))
 const summary=changeSummary(validated?.changes??[])
 const affectedDomains=[...new Set(validated?.changes.flatMap(change=>[change.before?.hostname,change.after?.hostname,change.hostname].filter((name):name is string=>!!name))??[])].join('、')||'无'
 return <>
  <Heading title="发布" description="预览变更、校验配置并确认发布。" action={<Button variant="outline" disabled={locked} onClick={()=>{setValidated(null);void preview.refetch()}}>刷新预览</Button>}/>
  <DeploymentTask deployment={currentTask} error={task.error} onRefresh={()=>void task.refetch()} onDetail={setDetail}/>
  {rollback&&<div className="notice notice-amber"><span>按当前策略重新发布历史服务，会重新检查 DNS 与安全规则，并保留当前草稿。当前不提供离线快照恢复。</span><Button variant="ghost" onClick={()=>rollbackTo('')}>退出回滚</Button></div>}
  <ErrorBox error={preview.error} onRetry={()=>void preview.refetch()}/><ErrorBox error={validate.error}/><ErrorBox error={publish.error}/>
  {preview.isPending?<Loading/>:p&&<section className="card">
   <div className="section-heading"><div><h2>变更预览 <Badge>{p.changes.length} 项变更</Badge></h2><p>草稿 r{p.revision} · {rollback?'按当前策略重新发布历史服务':'与当前已发布业务模型比较'}</p></div><Badge tone={canPublish?'green':'amber'}>{canPublish?(isStaticDemo?'模拟配置校验通过':'真实配置校验通过'):'等待校验'}</Badge></div>
   {p.drift&&<div className="notice notice-amber inset">检测到外部配置漂移。当前指纹 {p.runtime_hash.slice(0,12)}，期望 {p.expected_hash.slice(0,12)}。发布将完整覆盖运行配置。</div>}
   {p.changes.length?<div className="changes">{p.changes.map((c,i)=><div className="change-row" key={i}><Badge>{statusLabel[c.kind]??c.kind}</Badge><div><strong>{c.hostname}</strong><small>之前：{serviceDescription(c.before)}</small><small>之后：{serviceDescription(c.after)}</small></div></div>)}</div>:<Empty title="没有服务变更" action={<Link className="button button-outline" to="/services">前往服务</Link>}>可以校验当前配置，或先编辑草稿。</Empty>}
   {p.settings_changed&&<div className="changes"><h3>域名与访问策略变更</h3>{(['domains','admin_domain','console_lan_only','lan_cidrs','upstream_cidrs','allowed_names','denied_ips','resolvers'] as const).filter(k=>JSON.stringify(p.settings[k])!==JSON.stringify(p.active_settings[k])).map(k=><div className="change-row" key={k}><strong>{({domains:'域名与访问范围',admin_domain:'控制台',console_lan_only:'控制台来源限制',lan_cidrs:'可信网络',upstream_cidrs:'上游许可',allowed_names:'许可服务名',denied_ips:'禁止目标',resolvers:'服务器 DNS'})[k]}</strong><div><small>之前：{JSON.stringify(p.active_settings[k])}</small><small>之后：{JSON.stringify(p.settings[k])}</small></div></div>)}</div>}
   {!!(p.config_redacted_fields?.length||p.rollback_config_redacted_fields?.length)&&<ConfigPrivacyNotice/>}
   {p.rollback_config!==undefined&&<details className="json-details" open><summary>历史快照与当前策略候选{p.rollback_hash!==p.hash?'存在差异':'相同'}</summary><p>历史指纹：{p.rollback_hash} · 候选指纹：{p.hash}</p><h3>原始历史快照</h3><pre>{JSON.stringify(p.rollback_config,null,2)}</pre><h3>按当前策略生成的候选</h3><pre>{JSON.stringify(p.config,null,2)}</pre></details>}
   <details className="json-details"><summary>生成的 Caddy JSON · 只读</summary><pre>{JSON.stringify(p.config,null,2)}</pre></details>
   {validated&&<p>校验有效期至 {date(validated.validation_expires_at)}</p>}
   {reason&&<p role="status" className="notice notice-amber">{reason}</p>}
   <div className="release-actions"><span className="muted">热加载完整配置 · 无需重启 Caddy</span><Button variant="outline" disabled={preview.isError||validate.isPending||publish.isPending||locked} onClick={()=>validate.mutate()}>{validate.isPending?'Caddy 校验中…':'校验配置'}</Button><Button disabled={!canPublish} onClick={()=>{setConfirmDrift(false);setConfirmExposure(false);setConfirm(true)}}>确认发布</Button></div>
  </section>}
  <section className="card"><div className="section-heading"><h2>发布历史</h2></div><ErrorBox error={history.error} onRetry={()=>void history.refetch()}/>{history.isPending?<Loading/>:<History items={history.data?.items??[]} onRollback={rollbackTo} onDetail={setDetail}/>}<Pager offset={offset} count={history.data?.items.length??0} onChange={setOffset}/></section>
  <Dialog open={confirm} onOpenChange={v=>{if(!publish.isPending)setConfirm(v)}} title={rollback?'确认回滚发布':'确认发布配置'} description="发布在后台执行，关闭页面不会取消。"><p>影响摘要：新增 {summary.added}、修改 {summary.updated}、启用 {summary.enabled}、停用 {summary.disabled}、删除 {summary.deleted}。</p><p>受影响域名：{affectedDomains}</p>{rollback&&<p className="notice notice-amber">这是在线回滚：历史业务模型会按当前策略和 DNS 重新生成。</p>}{exposure&&<label className="checkbox warning"><input type="checkbox" checked={confirmExposure} onChange={e=>setConfirmExposure(e.target.checked)}/>我已核对域名与策略变更，确认放开访问范围。</label>}{validated?.settings_changed&&<p className="notice notice-amber">本次发布同时应用域名与访问策略草稿。</p>}{reason&&<p role="alert">{reason}</p>}{validated?.drift&&<label className="checkbox warning"><input type="checkbox" checked={confirmDrift} onChange={e=>setConfirmDrift(e.target.checked)}/>我已核对运行指纹，确认覆盖外部配置漂移。</label>}<ErrorBox error={publish.error}/><div className="dialog-actions"><Button variant="outline" disabled={publish.isPending} onClick={()=>setConfirm(false)}>取消</Button><Button disabled={!canPublish||(!!validated?.drift&&!confirmDrift)||exposure&&!confirmExposure} onClick={()=>publish.mutate()}>{publish.isPending?'提交中…':'立即发布'}</Button></div></Dialog>
  <Dialog open={!!detail} onOpenChange={v=>{if(!v)setDetail('')}} title="发布详情" wide><ErrorBox error={selected.error} onRetry={()=>void selected.refetch()}/>{selected.isPending?<Loading/>:selected.data&&<><p>操作者：{selected.data.deployment.actor} · 草稿 r{selected.data.deployment.revision}</p><p>{selected.data.deployment.error}</p>{!!selected.data.config_redacted_fields?.length&&<ConfigPrivacyNotice/>}<pre>{JSON.stringify(selected.data.config,null,2)}</pre></>}</Dialog>
 </>
}
