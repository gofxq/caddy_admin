import {useState} from 'react'
import {Code2,Plus,SlidersHorizontal,Trash2} from 'lucide-react'
import type {HeaderRule,Service,ServiceAdvanced} from '../model'
import {serviceAdvancedSummary} from '../model'
import {Button} from './ui/button'

function HeaderEditor({title,rules,onChange}:{title:'请求头'|'响应头';rules:HeaderRule[];onChange:(rules:HeaderRule[])=>void}){
 function edit(index:number,patch:Partial<HeaderRule>){onChange(rules.map((r,i)=>i===index?{...r,...patch}:r))}
 return <section className="service-option-section"><div className="service-option-heading"><div><h4>{title}</h4><p>按名称设置、追加或删除。请勿填写密码、Token 或 Cookie。</p></div><Button variant="outline" size="sm" type="button" disabled={rules.length>=32} onClick={()=>onChange([...rules,{name:'',operation:'set',value:''}])}><Plus size={14}/>添加{title}</Button></div>
  {!rules.length?<p className="service-option-empty">沿用 Caddy 默认转发行为</p>:<div className="header-rule-list">{rules.map((r,i)=><div className="header-rule" key={i}>
   <label><span>操作</span><select aria-label={`${title} ${i+1} 操作`} value={r.operation} onChange={e=>edit(i,{operation:e.target.value as HeaderRule['operation'],...(e.target.value==='delete'?{value:''}:{})})}><option value="set">设置</option><option value="add">追加</option><option value="delete">删除</option></select></label>
   <label><span>名称</span><input aria-label={`${title} ${i+1} 名称`} required maxLength={128} placeholder={title==='请求头'?'X-App':'X-Frame-Options'} value={r.name} onChange={e=>edit(i,{name:e.target.value})}/></label>
   <label><span>值</span><input aria-label={`${title} ${i+1} 值`} disabled={r.operation==='delete'} maxLength={2048} placeholder={r.operation==='delete'?'删除时无需填写':'静态文本'} value={r.value??''} onChange={e=>edit(i,{value:e.target.value})}/></label>
   <Button variant="ghost" size="sm" type="button" aria-label={`移除${title} ${i+1}`} onClick={()=>onChange(rules.filter((_,index)=>index!==i))}><Trash2 size={15}/></Button>
  </div>)}</div>}
 </section>
}

export function ServiceAdvancedFields({service,onChange}:{service:Service;onChange:(s:Service)=>void}){
 const [expanded,setExpanded]=useState(!!service.advanced),[snippetExpanded,setSnippetExpanded]=useState(!!service.advanced?.snippet)
 const a=service.advanced??{},grpc=service.scheme==='h2c'||service.scheme==='grpcs'
 function edit(patch:Partial<ServiceAdvanced>){const next={...a,...patch};for(const key of Object.keys(next) as (keyof ServiceAdvanced)[]){if(next[key]===undefined||next[key]===''||Array.isArray(next[key])&&!next[key]!.length)delete next[key]};onChange({...service,advanced:Object.keys(next).length?next:undefined})}
 function insert(example:string){edit({snippet:a.snippet?`${a.snippet.trimEnd()}\n${example}`:example})}
 return <details className="service-advanced" open={expanded} onToggle={event=>setExpanded(event.currentTarget.open)}><summary><SlidersHorizontal size={17}/><span>高级配置</span><small>{serviceAdvancedSummary(service)}</small></summary><div className="service-advanced-body">
  <section className="service-option-section"><div className="service-option-heading"><div><h4>传输与性能</h4><p>留空使用 Caddy 默认值，仅影响当前服务。</p></div></div><div className="form-grid">
   <label>响应压缩<select aria-label="响应压缩" disabled={grpc} value={a.compression??'off'} onChange={e=>edit({compression:e.target.value==='off'?undefined:e.target.value as ServiceAdvanced['compression']})}><option value="off">默认关闭 / 由片段配置</option><option value="gzip">gzip · 广泛兼容</option><option value="zstd_gzip">zstd + gzip · 自动协商</option></select><small>{grpc?'gRPC 消息压缩由客户端与服务端协商；请勿添加 encode 片段。':'按浏览器支持情况压缩响应，不重复压缩已编码内容。'}</small></label>
   <div className="service-protocol-note">{grpc?'HTTP/2 上游；TLS 模式始终验证证书。':'WebSocket 和 SSE 由 Caddy 自动支持，无需额外开关。'}</div>
   <label>连接超时<input aria-label="连接超时" value={a.dial_timeout??''} placeholder="默认 3s" onChange={e=>edit({dial_timeout:e.target.value})}/><small>例如 5s、1m，最长 24h。</small></label>
   <label>响应头超时<input aria-label="响应头超时" value={a.response_header_timeout??''} placeholder="默认不限制" onChange={e=>edit({response_header_timeout:e.target.value})}/><small>等待上游开始响应，非整个请求时长。</small></label>
  </div></section>
  <HeaderEditor title="请求头" rules={a.request_headers??[]} onChange={request_headers=>edit({request_headers})}/>
  <HeaderEditor title="响应头" rules={a.response_headers??[]} onChange={response_headers=>edit({response_headers})}/>
  <details className="service-snippet" open={snippetExpanded} onToggle={event=>setSnippetExpanded(event.currentTarget.open)}><summary><Code2 size={16}/><span>服务级配置片段</span><small>熟悉 Caddyfile 时使用</small></summary><div className="service-snippet-body"><p className="muted">补充当前服务的配置，无需填写站点域名或上游地址。同一选项只能在表单或片段中设置一处。</p><div className="snippet-examples"><Button type="button" size="sm" variant="outline" disabled={grpc} onClick={()=>insert('encode zstd gzip')}>插入压缩示例</Button><Button type="button" size="sm" variant="outline" onClick={()=>insert('header X-Frame-Options SAMEORIGIN\nheader -Server')}>插入头部示例</Button><Button type="button" size="sm" variant="outline" onClick={()=>insert('reverse_proxy {\n    transport http {\n        dial_timeout 5s\n        response_header_timeout 60s\n    }\n}')}>插入超时示例</Button></div>
   <label className="snippet-label"><span>Caddyfile 片段</span><textarea aria-label="Caddyfile 片段" spellCheck={false} rows={7} value={a.snippet??''} onChange={e=>edit({snippet:e.target.value})} placeholder={'# 只作用于当前服务\nheader X-App photos'}/></label><small className={new TextEncoder().encode(a.snippet??'').byteLength>4096?'text-danger':'muted'}>{new TextEncoder().encode(a.snippet??'').byteLength} / 4096 字节</small>
   <details className="snippet-help"><summary>支持的指令与边界</summary><p>encode gzip / encode zstd gzip；header、request_header；reverse_proxy 块中的 header_up、header_down，以及 transport http 内的 dial_timeout、response_header_timeout。头规则支持设置、+追加、-删除，值可用双引号包裹，支持 # 注释。</p><p>上游仍由表单管理。不支持额外站点、全局设置、import、动态变量、凭据头、路径路由或跳过证书验证。检查失败时会显示原因和片段行号。</p></details>
  </div></details>
 </div></details>
}
