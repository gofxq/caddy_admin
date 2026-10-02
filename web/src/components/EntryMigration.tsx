import {useQuery} from '@tanstack/react-query'
import {api} from '../api'
import type {Settings,Overview} from '../model'

export function EntryMigration(){
 const settings=useQuery({queryKey:['settings'],queryFn:()=>api<Settings>('/settings'),refetchInterval:10000})
 const overview=useQuery({queryKey:['overview'],queryFn:()=>api<Overview>('/overview'),refetchInterval:10000})
 const s=settings.data
 const ready=s?.certificate_status?.public_status==='ready'&&overview.data?.reachable
 if(!ready||!s?.config?.origin||location.origin.toLowerCase()===s.config.origin.toLowerCase())return null
 return <div className="notice notice-amber"><span>正式控制台已经就绪；请迁移到受信任入口继续使用。</span><a className="button button-primary" href={s.config.origin}>打开正式入口</a></div>
}
