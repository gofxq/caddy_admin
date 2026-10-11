import {TrafficWorkspace} from '../components/TrafficWorkspace'
import {useEffect,useRef,useState} from 'react'
import {useMutation,useQuery} from '@tanstack/react-query'
import {Link} from '@tanstack/react-router'
import {api} from '../api'
import {accessLabel,date,upstream,type ManagedDomain,type Service,type ServiceDetail as Detail,type UpstreamCheck} from '../model'
import {Badge,Empty,ErrorBox,Heading,Loading,Status} from '../components/shared'
import {Button} from '../components/ui/button'

function ConfigurationCard({title,service,domain}:{title:string;service:Service|null;domain:ManagedDomain|null}){
 return <section className="card service-detail-card" aria-label={title}>
  <h2>{title}</h2>
  {service?<dl>
   <div><dt>服务名称</dt><dd>{service.name}</dd></div>
   <div><dt>域名</dt><dd><code>{service.hostname}</code></dd></div>
   <div><dt>访问范围</dt><dd>{accessLabel(domain?.access??null)}</dd></div>
   <div><dt>上游</dt><dd><code>{upstream(service)}</code></dd></div>
   <div><dt>已校验地址快照</dt><dd><code>{service.dial||'发布前解析'}</code></dd></div>
   <div><dt>配置状态</dt><dd><Badge tone={service.enabled?'green':'neutral'}>{service.enabled?'启用':'停用'}</Badge></dd></div>
   {service.notes&&<div><dt>备注</dt><dd>{service.notes}</dd></div>}
  </dl>:<Empty title={title==='服务草稿'?'草稿中没有此服务':'尚未发布'}>{title==='服务草稿'?'已发布路由在确认发布删除前仍保留。':'保存草稿后，到发布页面预览、校验并确认。'}</Empty>}
 </section>
}
const runtimeLabels:Record<Detail['runtime']['status'],string>={matched:'配置一致',drift:'配置漂移',unknown:'运行配置未知',pending:'发布待核对'}
const checkLabels:Record<UpstreamCheck['status'],string>={reachable:'TCP 可连接',unreachable:'TCP 无法连接',unknown:'检查结果未知'}

export function ServiceDetail({id}:{id:string}){
 const query=useQuery({queryKey:['service-detail',id],queryFn:()=>api<Detail>(`/services/${encodeURIComponent(id)}`),refetchInterval:15000})
 const detail=query.data
 const [showObservation,setShowObservation]=useState(false)
 const [checkResult,setCheckResult]=useState<{id:string;result:UpstreamCheck}|null>(null)
 const generation=useRef(0)
 const check=useMutation({
  mutationFn:({serviceID,expectedHash,deploymentID}:{serviceID:string;expectedHash:string;deploymentID:string;generation:number})=>api<UpstreamCheck>(`/services/${encodeURIComponent(serviceID)}/check-upstream`,{method:'POST',body:{expected_hash:expectedHash,expected_deployment_id:deploymentID}}),
  onSuccess:(result,variables)=>{if(variables.generation===generation.current)setCheckResult({id:variables.serviceID,result})},
 })
 useEffect(()=>{generation.current++;setCheckResult(null);check.reset()},[id,detail?.runtime.expected_hash,detail?.runtime.deployment_id,detail?.runtime.status,!!query.error])
 const runtimeStatus=query.error?'unknown':detail?.runtime.status
 const runtimeMessage=query.error?'无法重新核对运行配置；以下为上次读取的配置，请重试。':detail?.runtime.message
 const result=!query.error&&checkResult?.id===id&&checkResult.result.expected_hash===detail?.runtime.expected_hash&&checkResult.result.deployment_id===detail?.runtime.deployment_id&&detail.runtime.status==='matched'?checkResult.result:null
 const canCheck=!!detail?.published?.enabled&&detail.runtime.status==='matched'&&!query.isFetching&&!query.error
 function runCheck(){if(canCheck&&detail){setCheckResult(null);check.mutate({serviceID:id,expectedHash:detail.runtime.expected_hash,deploymentID:detail.runtime.deployment_id,generation:generation.current})}}
 function refresh(){generation.current++;setCheckResult(null);check.reset();void query.refetch()}
 return <>
  <Heading title={detail?.draft?.name??detail?.published?.name??'服务详情'} description="分别核对草稿、已发布配置和运行状态；保存草稿不会立即影响线上。" action={<Link className="text-link" to="/services">返回服务</Link>}/>
  <ErrorBox error={query.error} onRetry={refresh}/>
  {query.isPending?<Loading/>:detail&&<>
   <div className="notice notice-neutral"><span>草稿修订 r{detail.revision}</span><Link className="text-link" to="/deployments">预览与发布</Link><Button variant="outline" size="sm" disabled={query.isFetching} onClick={refresh}>{query.isFetching?'核对中…':'刷新详情'}</Button></div>
   {!detail.draft&&detail.published&&<div className="notice notice-amber">此服务已从草稿删除；已发布路由在确认发布删除前仍保留。</div>}
   <div className="service-detail-grid"><ConfigurationCard title="服务草稿" service={detail.draft} domain={detail.draft_domain}/><ConfigurationCard title="已发布配置" service={detail.published} domain={detail.published_domain}/></div>
   <section className="card service-detail-card" aria-label="运行配置核对">
    <div className="service-detail-heading"><h2>运行配置核对</h2><Badge tone={runtimeStatus==='matched'?'green':'amber'}>{runtimeLabels[runtimeStatus??'unknown']}</Badge></div>
    <p>{runtimeMessage}</p><small className="muted">上次核对：{date(detail.runtime.checked_at)}{detail.runtime.version>0?` · 已发布版本 v${detail.runtime.version}`:''}</small>
   </section>
   <section className="card service-detail-card" aria-label="上游检查">
    <div className="service-detail-heading"><h2>上游检查</h2><Button disabled={!canCheck||check.isPending} onClick={runCheck}>{check.isPending?'检查中…':'检查已发布上游'}</Button></div>
    <p>仅在点击后从 Manager 检查已发布地址的 TCP 连接，不检测草稿目标，不修改配置。</p>
    <p className="muted">{detail.external_caddy?'外部 Caddy 与 Manager 的网络可能不同，Manager 可连接不代表外部 Caddy 可连接。':'TCP 可连接不代表应用正常，也不验证 HTTPS 证书。'}</p>
    {!canCheck&&<p className="muted">需有已发布且启用的服务，并成功核对运行配置；状态变化后请刷新详情。</p>}
    <ErrorBox error={check.error} onRetry={runCheck} retryLabel="重试检查"/>
    {result?<div className="service-check-result" role="status"><Badge tone={result.status==='reachable'?'green':result.status==='unreachable'?'red':'amber'}>{checkLabels[result.status]}</Badge><p>{result.message}</p><small>目标：<code>{result.target}</code> · {result.duration_ms} ms · {date(result.checked_at)}</small></div>:<p className="muted">尚无当前版本的检查结果。</p>}
   </section>
   <details className="card" onToggle={e=>setShowObservation(e.currentTarget.open)}><summary>运行统计与日志</summary>{showObservation&&<TrafficWorkspace serviceID={id} domainID={detail.published_domain?.id} allowServiceFilter={false}/>}</details>
   <section className="card" aria-label="最近相关发布">
    <div className="service-detail-card"><h2>最近相关发布</h2><p className="muted">最近 20 次发布中涉及此服务的记录，最多展示 5 条。</p></div>
    {detail.recent_deployments.length?<div className="table-wrap"><table><thead><tr><th>版本 / 时间</th><th>状态</th><th>操作</th></tr></thead><tbody>{detail.recent_deployments.map(release=><tr key={release.id}><td><strong>v{release.version}</strong>{release.rollback_id&&<Badge>回滚</Badge>}<small>{date(release.created)}</small></td><td><Status status={release.status}/></td><td><Link className="text-link" to="/deployments">查看发布记录</Link></td></tr>)}</tbody></table></div>:<Empty title="没有最近相关发布">新建草稿尚未发布，或此服务未在最近 20 次发布中变更。</Empty>}
   </section>
  </>}
 </>
}
