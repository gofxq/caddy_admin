import {useState,type ClipboardEvent,type ReactNode} from 'react'
import type {ManagedSettings,Service} from '../model'
import {accessLabel,upstream} from '../model'

function ByteField({label,value,limit,trim=false,multiline=false,onChange}:{label:string;value:string;limit:number;trim?:boolean;multiline?:boolean;onChange:(value:string)=>void}){
 const bytes=new TextEncoder().encode(trim?value.trim():value).length
 const error=bytes>limit?`${label}不能超过 ${limit} 字节`:''
 const props={value,maxLength:limit,'aria-label':label,onChange:(event:React.ChangeEvent<HTMLInputElement|HTMLTextAreaElement>)=>onChange(event.target.value),ref:(node:HTMLInputElement|HTMLTextAreaElement|null)=>node?.setCustomValidity(error)}
 return <label className="span-2">{label}{multiline?<textarea {...props}/>:<input {...props} required placeholder="例如：Immich 相册"/>}<small className={error?'text-danger':undefined}>{bytes} / {limit} 字节（中文通常占 3 字节）</small></label>
}

export function ServiceFields({service,settings,onChange}:{service:Service;settings:ManagedSettings;onChange:(service:Service)=>void}){
 const [portEdited,setPortEdited]=useState(service.port!==(service.scheme==='https'?443:80))
 const [pasteError,setPasteError]=useState('')
 const domain=settings.domains.find(d=>d.id===service.domain_id)
 const suffix=domain?`.${domain.name}`:''
 const prefix=suffix&&service.hostname.endsWith(suffix)?service.hostname.slice(0,-suffix.length):service.hostname
 const prefixValid=/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(prefix)
 const reserved=service.hostname===settings.admin_domain||service.hostname===settings.previous_admin_domain
 const hostnameError=!prefixValid?'请输入一层子域名，只能包含字母、数字和连字符，最长 63 个字符':reserved?'此域名由控制台使用，请更换子域名':''
 function update(patch:Partial<Service>){onChange({...service,...patch})}
 function pasteAddress(event:ClipboardEvent<HTMLInputElement>){
  const raw=event.clipboardData.getData('text').trim()
  if(!raw.includes('://'))return
  event.preventDefault()
  try{
   const url=new URL(raw)
   if(!['http:','https:'].includes(url.protocol))throw new Error('仅支持 HTTP 或 HTTPS 上游地址')
   if(url.username||url.password||url.pathname!=='/'||url.search||url.hash)throw new Error('不支持凭据、路径、查询参数或片段，请只粘贴协议、主机和端口')
   const scheme=url.protocol==='https:'?'https':'http'
   update({scheme,host:url.hostname.replace(/^\[|\]$/g,''),port:Number(url.port||(scheme==='https'?443:80))})
   setPortEdited(/:\d+$/.test(raw.match(/^https?:\/\/([^/?#]+)/i)?.[1]??''));setPasteError('')
  }catch(error){setPasteError(error instanceof TypeError?'上游地址格式无效，请检查协议、主机和端口':(error as Error).message)}
 }
 const domainField:ReactNode=settings.domains.length===1?<div><span>所属域名</span><p>{domain?.name} · {accessLabel(domain?.access??null)}</p></div>:<label>所属域名<select value={service.domain_id} required onChange={event=>{const next=settings.domains.find(d=>d.id===event.target.value);update({domain_id:event.target.value,hostname:next?`${prefix}.${next.name}`:''})}}>{settings.domains.map(d=><option key={d.id} value={d.id}>{d.name} · {accessLabel(d.access)}</option>)}</select></label>
 return <>
  <div className="form-grid">
   <ByteField label="服务名称" value={service.name} limit={100} trim onChange={name=>update({name})}/>
   <div>{domainField}<small>待配置访问范围也可保存草稿，发布前须先在设置中配置。</small></div>
   <label>子域名<div className="hostname-input"><input aria-label="子域名" required value={prefix} maxLength={63} placeholder="例如：photos" ref={node=>node?.setCustomValidity(hostnameError)} onChange={event=>{const next=event.target.value.trim().toLowerCase();update({hostname:next?`${next}${suffix}`:''})}}/><span>{suffix}</span></div><small>{prefix&&hostnameError?hostnameError:'只填写前缀，域名后缀自动补全。'}</small></label>
   <label>上游协议<select value={service.scheme} onChange={event=>{const scheme=event.target.value as Service['scheme'];update({scheme,...(!portEdited?{port:scheme==='https'?443:80}:{})})}}><option value="http">HTTP</option><option value="https">HTTPS · 验证证书</option></select></label>
   <label>端口<input type="number" min={1} max={65535} required value={service.port} onChange={event=>{setPortEdited(true);update({port:Number(event.target.value)})}}/></label>
   <label className="span-2">上游主机<input aria-label="上游主机" required placeholder="10.0.0.8 或已许可的服务名" value={service.host} onPaste={pasteAddress} onChange={event=>{setPasteError('');update({host:event.target.value})}}/><small>可粘贴 http://10.0.0.8:2283，自动填入协议、主机和端口。</small></label>
   {pasteError&&<p className="notice notice-red span-2" role="alert">{pasteError}</p>}
   <ByteField label="备注" value={service.notes} limit={2000} multiline onChange={notes=>update({notes})}/>
  </div>
  <label className="checkbox"><input type="checkbox" checked={service.enabled} onChange={event=>update({enabled:event.target.checked})}/>发布后启用此服务</label>
  <div className="route-preview"><span aria-label="访问地址">{prefix?`https://${service.hostname}`:'填写子域名后显示访问地址'}</span><span>→</span><code>{upstream(service)}</code></div>
 </>
}
