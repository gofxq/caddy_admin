import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {ArrowUpRight,Globe,Search,Waypoints,RefreshCw} from 'lucide-react'
import {api,APIError} from '../api'
import type {PortalService} from '../model'
import {Button} from '../components/ui/button'
import {Empty,Loading} from '../components/shared'

export function Portal(){
 const [keyword,setKeyword]=useState('')
 const q=useQuery({queryKey:['portal'],queryFn:()=>api<{services:PortalService[]}>('/portal'),refetchInterval:30000})
 const services=q.data?.services??[]
 const query=keyword.trim().toLowerCase()
 const filtered=services.filter(s=>`${s.name} ${s.hostname}`.toLowerCase().includes(query))
 return <div className="portal-screen">
  <header className="portal-topbar"><a className="brand" href="/portal"><Waypoints size={25}/><span>Caddy<span className="brand-light"> Admin</span></span></a><a className="portal-console" href="/">管理控制台 <ArrowUpRight size={16}/></a></header>
  <main className="portal-main">
   <div className="portal-heading"><span className="portal-eyebrow">你的应用，一处直达</span><h1>服务导览</h1><p>快速打开已发布的服务。</p></div>
   <div className="portal-toolbar"><div className="search"><Search size={18}/><input type="search" aria-label="搜索服务" placeholder="搜索服务名称或域名…" value={keyword} onChange={e=>setKeyword(e.target.value)}/></div><Button variant="ghost" aria-label="刷新服务列表" disabled={q.isFetching} onClick={()=>void q.refetch()}><RefreshCw size={17} className={q.isFetching?'spin':''}/></Button></div>
   {q.isPending?<Loading/>:q.isError?<div role="alert" className="portal-error"><p>{q.error instanceof APIError&&['network_restricted','policy_pending'].includes(q.error.code)?q.error.message:'暂时无法读取服务列表，请稍后重试。'}</p><Button variant="outline" disabled={q.isFetching} onClick={()=>void q.refetch()}>重试</Button></div>:!services.length?<Empty title="暂无可访问的服务">服务成功发布后会显示在这里；部分服务仅在可信网络可见。</Empty>:!filtered.length?<Empty title="没有匹配的服务">试试其他名称或域名。</Empty>:<><div className="portal-count" aria-live="polite">{filtered.length} 个服务</div><div className="portal-grid">{filtered.map(s=><a className="portal-card" key={s.hostname} href={s.url} target="_blank" rel="noopener noreferrer"><span className="portal-icon"><Globe size={23}/></span><h2>{s.name}</h2><span className="portal-hostname">{s.hostname}</span><span className="portal-open">打开服务 <ArrowUpRight size={16}/></span></a>)}</div></>}
  </main>
  <footer className="portal-footer">访问受各服务的网络策略与登录要求限制 · 链接不代表服务健康状态</footer>
 </div>
}
