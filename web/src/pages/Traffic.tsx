import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {api} from '../api'
import type {Settings} from '../model'
import {Heading,ErrorBox} from '../components/shared'
import {DiagnosticPanel,TrafficWorkspace} from '../components/TrafficWorkspace'
export function Traffic(){
 const [domain,setDomain]=useState('')
 const settings=useQuery({queryKey:['settings'],queryFn:()=>api<Settings>('/settings')})
 return <><Heading title="流量" description="查看业务请求、流量趋势、脱敏日志和站内告警；观测异常不会阻断配置管理。"/><TrafficWorkspace/><section className="card"><h2>选择域名诊断</h2><ErrorBox error={settings.error} onRetry={()=>void settings.refetch()}/><label>已生效域名<select value={domain} onChange={e=>setDomain(e.target.value)}><option value="">请选择</option>{settings.data?.active_config?.domains.map(d=><option key={d.id} value={d.id}>{d.name}</option>)}</select></label></section>{domain&&<DiagnosticPanel key={domain} domainID={domain}/>}</>
}
