import {useState,type ClipboardEvent,type ReactNode} from 'react'
import type {ManagedSettings,Service} from '../model'
import {ServiceAdvancedFields} from './ServiceAdvancedFields'
import {accessLabel,upstream} from '../model'
import {MAX_CADDYFILE_BYTES} from '../configuration'
import {noPrimaryUpstream,quickCaddyfile} from '../service-caddyfile'
import {Button} from './ui/button'

type Mode='proxy'|'files'|'static_site'|'custom'

function ByteField({label,value,limit,trim=false,multiline=false,onChange}:{label:string;value:string;limit:number;trim?:boolean;multiline?:boolean;onChange:(value:string)=>void}){
 const bytes=new TextEncoder().encode(trim?value.trim():value).length
 const error=bytes>limit?`${label}不能超过 ${limit} 字节`:''
 const props={value,maxLength:limit,'aria-label':label,onChange:(event:React.ChangeEvent<HTMLInputElement|HTMLTextAreaElement>)=>onChange(event.target.value),ref:(node:HTMLInputElement|HTMLTextAreaElement|null)=>node?.setCustomValidity(error)}
 return <label className="span-2">{label}{multiline?<textarea {...props}/>:<input {...props} required placeholder="例如：Immich 相册"/>}<small className={error?'text-danger':undefined}>{bytes} / {limit} 字节（中文通常占 3 字节）</small></label>
}

export function ServiceFields({service,settings,onChange}:{service:Service;settings:ManagedSettings;onChange:(service:Service)=>void}){
 const [portEdited,setPortEdited]=useState(service.port!==(service.scheme==='https'||service.scheme==='grpcs'?443:service.scheme==='h2c'?50051:80))
 const [pasteError,setPasteError]=useState('')
 const [savedCustom,setSavedCustom]=useState<string>()
 const [savedQuick,setSavedQuick]=useState<Service>()
 const [pendingMode,setPendingMode]=useState<Mode>()
 const site=service.static_site
 const custom=service.caddyfile!==undefined
 const filesOnly=!!site&&!site.api_path
 const currentMode:Mode=custom?'custom':filesOnly?'files':site?'static_site':'proxy'
 const source=service.caddyfile??''
 const sourceBytes=new TextEncoder().encode(source).length
 const sourceError=!source.trim()?'请输入 Caddyfile 正文':sourceBytes>MAX_CADDYFILE_BYTES?'Caddyfile 正文不能超过 16 KiB':''

 const domain=settings.domains.find(d=>d.id===service.domain_id)
 const suffix=domain?`.${domain.name}`:''
 const prefix=suffix&&service.hostname.endsWith(suffix)?service.hostname.slice(0,-suffix.length):service.hostname
 const prefixValid=/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/i.test(prefix)
 const reserved=service.hostname===settings.admin_domain||service.hostname===settings.previous_admin_domain
 const hostnameError=!prefixValid?'请输入一层子域名，只能包含字母、数字和连字符，最长 63 个字符':reserved?'此域名由控制台使用，请更换子域名':''
 function update(patch:Partial<Service>){onChange({...service,...patch})}
 function applyMode(mode:Mode){
  setPendingMode(undefined)
  if(mode==='custom'){
   setSavedQuick(service)
   update({...noPrimaryUpstream,static_site:undefined,advanced:undefined,caddyfile:savedCustom??quickCaddyfile(service).source})
   return
  }
  if(custom)setSavedCustom(source)
  const quick=custom?savedQuick:service
  const previousSite=quick?.static_site
  const root=previousSite?.root||(prefixValid?`/srv/sites/${prefix}`:'')
  const static_site=mode==='files'?{root,spa_fallback:previousSite?.spa_fallback??false}:mode==='static_site'?{root,api_path:previousSite?.api_path||'/api/*',spa_fallback:previousSite?.spa_fallback??true}:undefined
  const route=mode!=='files'&&quick?{scheme:quick.scheme,host:quick.host,port:quick.port,dial:quick.dial}:noPrimaryUpstream
  update({...route,static_site,caddyfile:undefined,advanced:mode==='files'?undefined:quick?.advanced})
 }
 function chooseMode(mode:Mode){
  if(mode===currentMode){setPendingMode(undefined);return}
  if((custom&&source.trim())||(mode==='custom'&&quickCaddyfile(service).needsReview)){setPendingMode(mode);return}
  applyMode(mode)
 }
 function compressionFor(scheme:Service['scheme']){return (scheme==='h2c'||scheme==='grpcs')&&service.advanced?{advanced:{...service.advanced,compression:undefined}}:{}}
 function pasteAddress(event:ClipboardEvent<HTMLInputElement>){
  const raw=event.clipboardData.getData('text').trim()
  if(!raw.includes('://'))return
  event.preventDefault()
  try{
   const url=new URL(raw)
   if(!['http:','https:','h2c:','grpcs:'].includes(url.protocol))throw new Error('支持 HTTP、HTTPS、h2c 或 grpcs 上游地址')
   if(url.username||url.password||url.pathname!==''&&url.pathname!=='/'||url.search||url.hash)throw new Error('不支持凭据、路径、查询参数或片段，请只粘贴协议、主机和端口')
   const scheme=url.protocol.slice(0,-1) as Service['scheme']
   update({scheme,...compressionFor(scheme),host:url.hostname.replace(/^\[|\]$/g,''),port:Number(url.port||(scheme==='https'||scheme==='grpcs'?443:scheme==='h2c'?50051:80))})
   setPortEdited(/:\d+$/.test(raw.match(/^(?:https?|h2c|grpcs):\/\/([^/?#]+)/i)?.[1]??''));setPasteError('')
  }catch(error){setPasteError(error instanceof TypeError?'上游地址格式无效，请检查协议、主机和端口':(error as Error).message)}
 }
 const domainField:ReactNode=settings.domains.length===1?<div><span>所属域名</span><p>{domain?.name} · {accessLabel(domain?.access??null)}</p></div>:<label>所属域名<select value={service.domain_id} required onChange={event=>{const next=settings.domains.find(d=>d.id===event.target.value);update({domain_id:event.target.value,hostname:next?`${prefix}.${next.name}`:''})}}>{settings.domains.map(d=><option key={d.id} value={d.id}>{d.name} · {accessLabel(d.access)}</option>)}</select></label>
 return <>
  <div className="form-grid">
   <ByteField label="服务名称" value={service.name} limit={100} trim onChange={name=>update({name})}/>
   <label className="span-2">服务类型<select value={currentMode} onChange={event=>chooseMode(event.target.value as Mode)}>
    <option value="proxy">反向代理</option><option value="files">静态文件托管</option><option value="static_site">静态站点 + API 代理</option><option value="custom">自定义 Caddyfile</option>
   </select></label>
   {pendingMode&&<div className="notice notice-amber span-2" role="status"><div>
    <p>{custom?'切换后，保存草稿将使用快捷配置。关闭编辑器前可切回此正文。':'当前快捷配置含有需要手动迁移的选项。切换后，请在保存前核对并补齐 Caddyfile。'}</p>
    <div className="snippet-examples"><Button variant="outline" size="sm" type="button" onClick={()=>applyMode(pendingMode)}>{custom?'继续使用快捷配置':'继续使用自定义配置'}</Button><Button variant="ghost" size="sm" type="button" onClick={()=>setPendingMode(undefined)}>保留当前配置</Button></div>
   </div></div>}
   {site&&<>
    <label>静态目录<input aria-label="静态目录" required maxLength={1024} value={site.root} placeholder="/srv/sites/douji_admin" onChange={event=>update({static_site:{...site,root:event.target.value}})}/><small>填写 /srv/sites/ 下的项目目录。Docker 将宿主机的 SITES_DIR（默认 ./sites）挂载到 /srv/sites。</small></label>
    {!filesOnly&&<label>API 路径<input aria-label="API 路径" required maxLength={256} value={site.api_path} placeholder="/api/*" onChange={event=>update({static_site:{...site,api_path:event.target.value}})}/><small>匹配的请求保留原路径，使用下方的上游和高级配置。</small></label>}
    <label className="checkbox"><input type="checkbox" checked={site.spa_fallback??!filesOnly} onChange={event=>update({static_site:{...site,spa_fallback:event.target.checked}})}/>前端路由回退到 index.html</label>
   </>}
   <div>{domainField}<small>待配置访问范围也可保存草稿，发布前须先在设置中配置。</small></div>
   <label>子域名<div className="hostname-input"><input aria-label="子域名" required value={prefix} maxLength={63} placeholder="例如：photos" ref={node=>node?.setCustomValidity(hostnameError)} onChange={event=>{const next=event.target.value.trim().toLowerCase();update({hostname:next?`${next}${suffix}`:'',...(site&&!site.root&&next&&/^[a-z0-9][a-z0-9-]*$/.test(next)?{static_site:{...site,root:`/srv/sites/${next}`}}:{})})}}/><span>{suffix}</span></div><small>{prefix&&hostnameError?hostnameError:'只填写前缀，域名后缀自动补全。'}</small></label>
   {custom&&<div className="span-2">
    <label className="snippet-label"><span>Caddyfile 正文</span><textarea aria-label="Caddyfile 正文" spellCheck={false} rows={10} required value={source} ref={node=>node?.setCustomValidity(sourceError)} onChange={event=>update({caddyfile:event.target.value})} placeholder={'root * /srv/sites/my-app\nfile_server'}/></label>
    <small className={sourceBytes>MAX_CADDYFILE_BYTES?'text-danger':'muted'}>{sourceBytes} / {MAX_CADDYFILE_BYTES} 字节</small>
    <p className="muted">填写站点块内的 HTTP 指令。域名、TLS 和访问范围由控制台管理；代理目标仍须符合上游许可。</p>
    {savedQuick?.advanced&&<details className="json-details"><summary>查看切换前的高级选项</summary><pre>{JSON.stringify(savedQuick.advanced,null,2)}</pre></details>}
   </div>}
   {!filesOnly&&!custom&&<>
   <label>上游协议<select value={service.scheme} onChange={event=>{const scheme=event.target.value as Service['scheme'];update({scheme,...(!portEdited?{port:scheme==='https'||scheme==='grpcs'?443:scheme==='h2c'?50051:80}:{}),...compressionFor(scheme)})}}><option value="http">HTTP</option><option value="https">HTTPS · 验证证书</option><option value="h2c">gRPC 明文 · h2c</option><option value="grpcs">gRPC TLS · 验证证书</option></select></label>
   <label>端口<input type="number" min={1} max={65535} required value={service.port} onChange={event=>{setPortEdited(true);update({port:Number(event.target.value)})}}/></label>
   <label className="span-2">上游主机<input aria-label="上游主机" required placeholder="10.0.0.8 或已许可的服务名" value={service.host} onPaste={pasteAddress} onChange={event=>{setPasteError('');update({host:event.target.value})}}/><small>可粘贴 HTTP/HTTPS、h2c:// 或 grpcs:// 地址，自动填入协议、主机和端口。</small></label>
   {pasteError&&<p className="notice notice-red span-2" role="alert">{pasteError}</p>}
   </>}
   <ByteField label="备注" value={service.notes} limit={2000} multiline onChange={notes=>update({notes})}/>
  </div>
  {!filesOnly&&!custom&&<ServiceAdvancedFields service={service} onChange={onChange}/>}
  <label className="checkbox"><input type="checkbox" checked={service.enabled} onChange={event=>update({enabled:event.target.checked})}/>发布后启用此服务</label>
  <div className="route-preview"><span aria-label="访问地址">{prefix?`https://${service.hostname}`:'填写子域名后显示访问地址'}</span><span>→</span><code>{site&&!filesOnly?`${site.api_path} → ${upstream(service)} · 其余路径 → ${site.root}`:upstream(service)}</code></div>
 </>
}
