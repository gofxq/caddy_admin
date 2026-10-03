import {useState} from 'react'
import {useMutation} from '@tanstack/react-query'
import {api} from '../api'
import type {ManagedDomain} from '../model'
import {dnsRecord} from './SetupDNSGuidance'
import {checkSetupDNS,DOH_PRESETS,type DNSReport,type DoHProvider} from '../setup-dns'
import {ErrorBox} from './shared'
type Plan={name:string;type:string;address:string;action:string;old_address?:string;old_proxied?:boolean;warning?:string;fingerprint:string}
export function DomainDNS({domains}:{domains:ManagedDomain[]}){
 const [domainID,setDomainID]=useState(domains[0]?.id??''),[address,setAddress]=useState(''),[plan,setPlan]=useState<Plan|null>(null),[confirmed,setConfirmed]=useState(false),[message,setMessage]=useState(''),[provider,setProvider]=useState<DoHProvider>('cloudflare'),[report,setReport]=useState<DNSReport|null>(null)
 const target=dnsRecord(address),domain=domains.find(d=>d.id===domainID)
 const preview=useMutation({mutationFn:()=>api<Plan>('/settings/dns/preview',{method:'POST',body:{domain_id:domainID,address:target!.address}}),onSuccess:p=>{setPlan(p);setConfirmed(false);setReport(null);setMessage('')}})
 const apply=useMutation({mutationFn:()=>api('/settings/dns/confirm',{method:'POST',body:{domain_id:domainID,address:target!.address,fingerprint:plan!.fingerprint,confirm:true}}),onSuccess:()=>setMessage('域名 DNS 配置已确认，请检查解析后预览和发布域名证书配置。')})
 const check=useMutation({mutationFn:()=>checkSetupDNS(domain!.name,target!.address,provider),onSuccess:setReport})
 const busy=preview.isPending||apply.isPending||check.isPending
 function invalidate(){setPlan(null);setConfirmed(false);setReport(null);setMessage('');preview.reset();apply.reset();check.reset()}
 return <section className="card settings-card"><h2>域名 DNS</h2><p className="muted">先保存新增域名草稿，再核对 Token 授权及记录。预览不写 DNS；确认立即写入 Cloudflare。先发布不含新域名业务服务的证书配置，证书就绪后再发布业务服务。</p><fieldset disabled={busy}><label>核对域名<select value={domainID} onChange={e=>{setDomainID(e.target.value);invalidate()}}>{domains.map(d=><option value={d.id} key={d.id}>{d.name}</option>)}</select></label><label>域名 DNS 目标 IP<input value={address} placeholder="公网或内网 IPv4 / IPv6" onChange={e=>{setAddress(e.target.value);invalidate()}}/></label>{address&&!target&&<p className="text-danger">请填写一个有效 IP，不含协议或端口。</p>}<button className="button button-outline" disabled={!domain||!target} onClick={()=>{invalidate();preview.mutate()}}>预览域名 DNS</button>
 {plan&&<><p>{plan.action==='create'?'新建':plan.action==='update'?'更新':'复用'}：{plan.type} {plan.name} → {plan.address}，仅 DNS（灰云）。</p>{plan.old_address&&<p className="notice notice-amber">原记录 {plan.old_address}，{plan.old_proxied?'已代理':'仅 DNS'}。</p>}{plan.warning&&<p className="notice notice-amber">{plan.warning}</p>}<label className="checkbox warning"><input type="checkbox" checked={confirmed} onChange={e=>setConfirmed(e.target.checked)}/>我确认上述域名 DNS 记录及变更影响。</label><button className="button button-primary" disabled={!confirmed||!!message} onClick={()=>apply.mutate()}>确认域名 DNS 变更</button></>}
 <div className="dns-presets">{DOH_PRESETS.map(p=><button key={p.id} className="button button-outline button-sm" aria-pressed={provider===p.id} onClick={()=>{setProvider(p.id);setReport(null)}}>{p.name}</button>)}<button className="button button-outline" disabled={!domain||!target} onClick={()=>check.mutate()}>检查域名 DNS</button></div><p className="muted">浏览器 DoH 仅在检查时查询，不发送凭据；查询通过不代表本机可达或证书就绪。</p></fieldset><ErrorBox error={preview.error??apply.error??check.error}/>{busy&&<p role="status">正在处理域名 DNS…</p>}{message&&<p role="status" className="notice notice-green">{message}</p>}{report&&<div><p role="status">{report.verified?'DNS 查询通过':'DNS 查询未通过，请核对记录后重查'}</p>{report.queries.map((q,i)=><p key={i}>{q.name} · {q.addresses.join('、')||'无地址'} · {q.message}</p>)}</div>}</section>
}
