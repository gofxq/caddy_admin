import {useQuery} from '@tanstack/react-query'
import {Link} from '@tanstack/react-router'
import {Activity,GitCompareArrows,Server,Layers,ArrowUpRight,ShieldCheck,Globe} from 'lucide-react'
import {api} from '../api'
import type {Overview as OverviewData,Settings} from '../model'
import {date} from '../model'
import {Heading,Loading,ErrorBox,History,Badge} from '../components/shared'

function FirstRunTasks({overview,settings}:{overview:OverviewData;settings?:Settings}){
 const external=settings?.external_caddy===true
 const formal=!!settings?.config?.origin&&location.origin.toLowerCase()===settings.config.origin.toLowerCase()
 const certificateReady=settings?.certificate_status?.public_status==='ready'
 const tokenReady=settings?.token_configured===true
 const connectionReady=overview.reachable&&certificateReady
 const policyTasks=[{done:!!settings?.config.domains.some(d=>d.access!==null),title:'配置业务域名访问范围',detail:'明确选择仅可信网络或允许互联网访问；控制台来源限制可按需启用',to:'/settings',external:false},{done:!!(settings?.config.upstream_cidrs.length||settings?.config.allowed_names.length),title:'配置并发布上游许可',detail:'默认尚未许可任何目标，先保存设置草稿并发布',to:'/settings',external:false}]
 const tasks=external?[
  {done:formal,title:'使用正式控制台入口',detail:formal?'当前已从正式入口访问':'当前仍是临时交接入口',to:settings?.config?.origin||'/settings',external:true},
  {done:connectionReady,title:'确认外部 Caddy 与可信 TLS',detail:connectionReady?'外部 Caddy 和真实 TLS 已就绪':'等待外部 Caddy 连通与真实 TLS 探测',to:'/certificates'},
  ...policyTasks,
  {done:overview.unpublished,title:'创建服务草稿',detail:overview.unpublished?'已有未发布服务草稿':'证书等待期间也可以先保存草稿',to:'/services'},
  {done:overview.version>0,title:'完成首次成功发布',detail:'发布前仍需预览、真实校验和明确确认',to:'/deployments'},
 ]:[
  {done:formal,title:'使用正式控制台入口',detail:formal?'当前已从正式入口访问':'当前仍是临时入口',to:settings?.config?.origin||'/settings',external:true},
  {done:tokenReady&&certificateReady,title:'启用可信证书',detail:certificateReady?'Cloudflare Token 与真实 TLS 均已就绪':tokenReady?'Token 已配置，等待证书激活与 TLS 探测':'配置 Cloudflare Token；等待期间可先创建草稿',to:tokenReady?'/certificates':'/settings'},
  ...policyTasks,
  {done:overview.unpublished,title:'创建服务草稿',detail:overview.unpublished?'已有未发布服务草稿':'保存草稿不会立即影响线上',to:'/services'},
  {done:overview.version>0,title:'完成首次成功发布',detail:'发布继续受证书、校验有效期和漂移确认门禁保护',to:'/deployments'},
 ]
 return <section className="card first-run"><div><h2>首次运行任务中心</h2><p>{external?'外部 Caddy 模式':'内置 Caddy 模式'} · 可按当前状态逐项完成</p></div><ol>{tasks.map((task,index)=><li key={task.title} className={task.done?'done':'active'}><b>{task.done?'✓':index+1}</b><span><strong>{task.title}</strong><small>{task.detail}</small>{!task.done&&(('external' in task&&task.external)&&task.to.startsWith('http')?<a className="text-link" href={task.to}>打开正式入口</a>:<Link className="text-link" to={task.to as '/settings'|'/certificates'|'/services'|'/deployments'}>前往处理</Link>)}</span></li>)}</ol></section>
}

export function Overview(){
 const q=useQuery({queryKey:['overview'],queryFn:()=>api<OverviewData>('/overview'),refetchInterval:10000})
 const settings=useQuery({queryKey:['settings'],queryFn:()=>api<Settings>('/settings'),refetchInterval:10000})
 const d=q.data,s=settings.data
 return <><Heading title="概览" description="查看运行状态、服务草稿与发布记录。" action={<Link className="button button-primary" to="/services">管理服务 <ArrowUpRight size={17}/></Link>}/><ErrorBox error={q.error} onRetry={()=>void q.refetch()}/><ErrorBox error={settings.error} onRetry={()=>void settings.refetch()}/>{q.isPending?<Loading/>:d&&<>{d.version===0&&<FirstRunTasks overview={d} settings={s}/>}<div className={`notice ${!d.reachable||d.drift?'notice-amber':'notice-green'}`}><Activity size={19}/><span>{d.message}</span><small>检查于 {date(d.checked_at)}</small></div><div className="stats-grid">{[{label:'Caddy 连通性',value:d.reachable?'已连接':'不可达',detail:'Admin API · Unix Socket',icon:Activity},{label:'配置一致性',value:!d.reachable?'未知':d.drift?'存在漂移':'保持一致',detail:d.unpublished?'另有草稿等待发布':'草稿与已发布服务一致',icon:GitCompareArrows},{label:'已启用服务',value:String(d.enabled).padStart(2,'0'),detail:'当前已发布版本',icon:Server},{label:'已发布版本',value:`v${d.version}`,detail:d.version?'热加载 · 无需重启':'系统初始配置',icon:Layers}].map(x=><section className="stat" key={x.label}><div><span>{x.label}</span><x.icon size={18}/></div><strong>{x.value}</strong><small>{x.detail}</small></section>)}</div><section className="card"><div className="section-heading"><div><h2>最近发布</h2></div><Link className="text-link" to="/deployments">全部记录 <ArrowUpRight size={15}/></Link></div><History items={d.recent}/></section><div className="info-grid"><section className="card info-card"><Globe size={24}/><div><h3>先保存，再发布</h3><p>服务编辑保存在草稿中。查看变更、完成校验并确认后，配置才会生效。</p><Link className="text-link" to="/deployments">查看发布工作区 →</Link></div></section><section className="card info-card"><ShieldCheck size={24}/><div><h3>为内部服务保留边界</h3><p>每个域名明确选择业务访问范围；控制台来源限制独立且默认关闭，可在设置中按需启用。</p><Badge>LAN / VPN 访问策略</Badge></div></section></div></>}</>}
