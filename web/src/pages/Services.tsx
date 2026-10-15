import {useState} from 'react'
import {useMutation,useQuery,useQueryClient} from '@tanstack/react-query'
import {Link} from '@tanstack/react-router'
import {Plus,Search,Globe,MoreHorizontal,Trash2,Power,Pencil,ArrowUpRight} from 'lucide-react'
import {api,APIError} from '../api'
import {type Service,type ServiceCheck,type ServiceList,type Settings,type ManagedSettings,accessLabel,filterServices,serviceState,upstream,date} from '../model'
import {Button} from '../components/ui/button'
import {Dialog} from '../components/ui/dialog'
import {Heading,Loading,ErrorBox,Empty,Badge} from '../components/shared'
import {ServiceFields} from '../components/ServiceFields'
import {discardChangesMessage,useUnsavedChanges} from '../components/useUnsavedChanges'

const blank:Service={id:'',name:'',domain_id:'',hostname:'',scheme:'http',host:'',port:80,enabled:true,notes:'',dial:'',updated_at:''}
type Editor={service:Service;original:Service;revision:number;settings:ManagedSettings}

export function Services(){
 const qc=useQueryClient()
 const q=useQuery({queryKey:['services'],queryFn:()=>api<ServiceList>('/services'),refetchInterval:15000})
 const settings=useQuery({queryKey:['settings'],queryFn:()=>api<Settings>('/settings')})
 const [keyword,setKeyword]=useState(''),[domainID,setDomainID]=useState(''),[state,setState]=useState('')
 const [editor,setEditor]=useState<Editor|null>(null),[remove,setRemove]=useState<{service:Service;revision:number}|null>(null)
 const [message,setMessage]=useState(''),[latest,setLatest]=useState<{list:ServiceList;settings:Settings}|null>(null),[reloadError,setReloadError]=useState<unknown>(null),[reloading,setReloading]=useState(false)
 const dirty=!!editor&&JSON.stringify(editor.service)!==JSON.stringify(editor.original)
 useUnsavedChanges(dirty)
 const change=useMutation({mutationFn:({service,revision,method}:{service:Service;revision:number;method:string})=>api('/services'+(method==='POST'?'':`/${service.id}`),{method,body:{service,revision}}),onSuccess:(_,variables)=>{
  setMessage(variables.method==='DELETE'?'服务删除草稿已保存，发布后生效':editor?'服务草稿已保存，尚未上线':`已保存${variables.service.enabled?'启用':'停用'}草稿，发布后生效`)
  setEditor(null);setRemove(null);setLatest(null)
  for(const key of ['services','overview','preview'])void qc.invalidateQueries({queryKey:[key]})
 }})
 const checker=useMutation({mutationFn:async({service,revision}:{service:Service;revision:number})=>{const result=await api<ServiceCheck>('/services/check',{method:'POST',body:{service,revision}});return {...result,input:JSON.stringify(service)}}})
 const checkCurrent=!!editor&&checker.variables?.revision===editor.revision&&JSON.stringify(checker.variables.service)===JSON.stringify(editor.service)
 const checked=!!editor&&checker.data?.revision===editor.revision&&checker.data.input===JSON.stringify(editor.service)?checker.data:null
 const d=q.data,list=d?filterServices(d.services,keyword,domainID,state):[],deleted=d?.published.filter(s=>!d.services.some(x=>x.id===s.id))??[]
 const canCreate=!!d&&!!settings.data?.config.domains.length&&!settings.isError&&!change.isPending
 function openEditor(service:Service){if(!d)return;checker.reset();change.reset();setMessage('');setLatest(null);setReloadError(null);setEditor({service:{...service},original:{...service},revision:d.revision,settings:settings.data!.config})}
 function openNew(){if(canCreate)openEditor({...blank,domain_id:settings.data!.config.domains.find(domain=>domain.id===domainID)?.id??settings.data!.config.domains[0].id})}
 function closeEditor(){if(!change.isPending&&(!dirty||window.confirm(discardChangesMessage)))setEditor(null)}
 const conflict=change.error instanceof APIError&&change.error.code==='conflict'||checkCurrent&&checker.error instanceof APIError&&checker.error.code==='conflict'
 async function readLatest(){
  setReloading(true);setReloadError(null);setLatest(null)
  try{const [list,config]=await Promise.all([q.refetch({throwOnError:true}),settings.refetch({throwOnError:true})]);if(list.data&&config.data)setLatest({list:list.data,settings:config.data})}
  catch(error){setReloadError(error)}finally{setReloading(false)}
 }
 const latestService=latest?.list.services.find(s=>s.id===editor?.service.id)
 const latestDomainExists=!!latest?.settings.config.domains.some(domain=>domain.id===editor?.service.domain_id)
 return <>
  <Heading title="服务" description="为你的应用配置域名与上游，所有修改先保存为草稿。" action={<Button disabled={!canCreate} onClick={openNew}><Plus size={17}/>新建服务</Button>}/>
  <ErrorBox error={q.error} onRetry={()=>void q.refetch()}/>
  <ErrorBox error={settings.error} onRetry={()=>void settings.refetch()} retryLabel="重试读取设置"/>
  {settings.isPending&&<p className="notice notice-neutral" role="status">正在读取域名与上游设置，读取完成后即可新建服务。</p>}
  <ErrorBox error={!editor&&!remove?change.error:null}/>
  {message&&<div className="notice notice-green" role="status"><span>{message}</span><Button variant="outline" size="sm" disabled={!canCreate} onClick={openNew}>继续添加</Button><Link to="/deployments" className="text-link">预览与发布</Link></div>}
  <div className="notice notice-neutral"><MoreHorizontal size={20}/><span>草稿修订 <strong>r{d?.revision??'—'}</strong> · 保存不会立即影响线上服务。</span><Link to="/deployments" className="text-link">预览与发布 <ArrowUpRight size={14}/></Link></div>
  {settings.data&&!settings.data.config.upstream_cidrs.length&&<div className="notice notice-amber">尚未许可任何上游目标。请先到 <Link to="/settings">设置</Link> 保存上游许可草稿，再添加服务，最后统一预览与发布。</div>}
  {settings.data&&!settings.data.config.domains.length&&<div className="notice notice-amber">尚未配置基础域名，请到 <Link to="/settings">设置</Link> 添加域名并保存草稿。</div>}
  <section className="card">
   <div className="filters"><div className="search"><Search size={17}/><input aria-label="搜索服务" placeholder="搜索名称、域名或上游…" value={keyword} onChange={e=>setKeyword(e.target.value)}/></div><select aria-label="按域名筛选" value={domainID} onChange={e=>setDomainID(e.target.value)}><option value="">所有域名</option>{settings.data?.config.domains.map(d=><option key={d.id} value={d.id}>{d.name}</option>)}</select><select aria-label="按状态筛选" value={state} onChange={e=>setState(e.target.value)}><option value="">所有状态</option><option value="enabled">已启用</option><option value="disabled">已停用</option></select></div>
   {q.isPending?<Loading/>:!d?<p className="empty">服务列表读取失败，请重试。</p>:!list.length?<Empty title={d.services.length?'没有匹配的服务':'添加你的第一个服务'} action={!d.services.length?<Button disabled={!canCreate} onClick={openNew}>添加第一个服务</Button>:undefined}>从一个域名和一个上游地址开始。</Empty>:<div className="table-wrap"><table><thead><tr><th>服务 / 域名</th><th>域名 / 访问范围</th><th>上游地址</th><th>草稿状态</th><th>线上状态</th><th>操作</th></tr></thead><tbody>{list.map(s=><tr key={s.id}><td><div className="service-name"><span className="service-icon"><Globe size={19}/></span><div><Link className="text-link" to="/services/$id" params={{id:s.id}}>{s.name}</Link><small className="mono">{s.hostname}</small></div></div><small>更新于 {date(s.updated_at)}</small></td><td><span>{settings.data?.config.domains.find(d=>d.id===s.domain_id)?.name??'未知域名'}</span><small>{accessLabel(settings.data?.config.domains.find(d=>d.id===s.domain_id)?.access??null)}</small></td><td><code>{upstream(s)}</code><small>生效目标：{s.dial||'发布前解析'}</small></td><td><Badge tone={s.enabled?'green':'neutral'}>{s.enabled?'已启用':'已停用'}</Badge><small>{serviceState(s,d.published)}</small></td><td>{d.published.find(x=>x.id===s.id)?.enabled?<Badge tone="green">路由已发布</Badge>:<Badge>未暴露</Badge>}<small>上游健康：未检测</small></td><td><div className="row-actions"><Button variant="ghost" size="sm" aria-label={`编辑 ${s.name}`} disabled={!settings.data||settings.isError||change.isPending} onClick={()=>openEditor(s)}><Pencil size={16}/></Button><Button variant="ghost" size="sm" aria-label={`${s.enabled?'停用':'启用'} ${s.name}`} disabled={change.isPending} onClick={()=>{change.reset();setMessage('');change.mutate({service:{...s,enabled:!s.enabled},revision:d.revision,method:'PUT'})}}><Power size={16}/></Button><Button variant="ghost" size="sm" aria-label={`删除 ${s.name}`} disabled={change.isPending} onClick={()=>{change.reset();setMessage('');setRemove({service:s,revision:d.revision})}}><Trash2 size={16}/></Button></div></td></tr>)}</tbody></table></div>}
   <div className="table-footer">{d?`${list.length} 个服务`:'服务数量未知'} · 访问范围由所选域名的属性决定；待配置域名不能发布启用服务</div>
  </section>
  {deleted.length>0&&<div className="notice notice-amber">待发布删除：{deleted.map((s,index)=><span key={s.id}>{index>0?'、':''}<Link className="text-link" to="/services/$id" params={{id:s.id}}>{s.hostname}</Link></span>)}。这些路由目前仍在线上生效。</div>}
  <Dialog open={!!editor} onOpenChange={v=>{if(!v)closeEditor()}} wide title={editor?.service.id?'编辑服务':'新建服务'} description="保存为草稿后，在发布页面完成校验和确认。">{editor&&settings.data&&<form onSubmit={event=>{event.preventDefault();if(event.currentTarget.reportValidity())change.mutate({...editor,method:editor.service.id?'PUT':'POST'})}}>
   <ErrorBox error={change.error||(checkCurrent?checker.error:null)}/>
   {conflict&&<div className="notice notice-amber"><div><p>输入已保留。读取最新草稿并核对差异后，再决定是否继续保存。</p><Button type="button" variant="outline" disabled={reloading} onClick={()=>void readLatest()}>{reloading?'读取中…':'读取最新草稿'}</Button><ErrorBox error={reloadError}/>{latest&&<>
    <p>最新草稿 r{latest.list.revision}</p><pre className="conflict-preview">{JSON.stringify(editor.service.id?latestService??'此服务已从最新草稿删除':latest.list.services,null,2)}</pre><details><summary>最新域名与访问策略（继续编辑后采用）</summary><pre className="conflict-preview">{JSON.stringify(latest.settings.config,null,2)}</pre></details>
    {!latestDomainExists&&<p>原所属域名已被移除，请保留所需内容，关闭编辑器后从最新列表处理。</p>}
    {(!editor.service.id||latestService)&&latestDomainExists?<Button type="button" variant="outline" onClick={()=>{const oldDomain=editor.settings.domains.find(domain=>domain.id===editor.service.domain_id);const newDomain=latest.settings.config.domains.find(domain=>domain.id===editor.service.domain_id)!;const prefix=oldDomain&&editor.service.hostname.endsWith(`.${oldDomain.name}`)?editor.service.hostname.slice(0,-oldDomain.name.length-1):editor.service.hostname;setEditor({...editor,service:{...editor.service,hostname:prefix?`${prefix}.${newDomain.name}`:''},revision:latest.list.revision,settings:latest.settings.config});setLatest(null);change.reset();checker.reset()}}>已核对，保留输入继续编辑</Button>:<p>请保留需要的内容，关闭编辑器后从最新列表重新处理。</p>}
   </>}</div></div>}
   <fieldset disabled={change.isPending} className="service-fields"><ServiceFields service={editor.service} settings={editor.settings} onChange={service=>setEditor({...editor,service})}/></fieldset>
   {checked&&<div className="service-check-feedback" role="status"><p className="notice notice-green">{checked.simulated?'演示配置检查通过，仅展示输入，未解析片段或调用真实 Caddy。':'配置检查通过，尚未保存或上线。'}发布前仍需在发布页校验。</p><details className="json-details"><summary>查看有效选项与只读 Caddy JSON</summary><h4>{checked.simulated?'高级输入（演示未解析）':'合并后的高级选项'}</h4><pre>{JSON.stringify(checked.effective,null,2)}</pre><h4>完整配置候选</h4><pre>{JSON.stringify(checked.config,null,2)}</pre></details></div>}
   {checker.data&&!checked&&<p className="muted" role="status">输入已变化，请重新检查配置。</p>}
   <div className="dialog-actions service-editor-actions"><Button variant="outline" type="button" disabled={change.isPending||checker.isPending||conflict} onClick={event=>{if(event.currentTarget.form?.reportValidity())checker.mutate({service:editor.service,revision:editor.revision})}}>{checker.isPending?'正在检查配置…':'检查配置'}</Button><span className="service-action-spacer"/><Button variant="outline" type="button" disabled={change.isPending} onClick={closeEditor}>取消</Button><Button type="submit" disabled={change.isPending||checker.isPending||conflict}>{change.isPending?'保存中…':'保存草稿'}</Button></div>
  </form>}</Dialog>
  <Dialog open={!!remove} onOpenChange={v=>{if(!v&&!change.isPending)setRemove(null)}} title="删除服务" description={`${remove?.service.hostname??''} 将从草稿移除。线上路由在再次发布前仍然保留。`}><ErrorBox error={change.error}/><div className="dialog-actions"><Button variant="outline" disabled={change.isPending} onClick={()=>setRemove(null)}>取消</Button><Button variant="destructive" disabled={change.isPending} onClick={()=>remove&&change.mutate({...remove,method:'DELETE'})}>确认删除</Button></div></Dialog>
 </>
}
