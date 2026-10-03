import {useState} from 'react'
import {HelpTooltip} from './HelpTooltip'

export function dnsRecord(value:string):{type:'A'|'AAAA';address:string}|null {
 const address=value.trim()
 if (/^(0|[1-9]\d{0,2})(\.(0|[1-9]\d{0,2})){3}$/.test(address)) {
  const parts=address.split('.').map(Number)
  if(parts.some(part=>part>255)||parts[0]===0||parts[0]===127||parts[0]>=224||parts[0]===169&&parts[1]===254)return null
  return {type:'A',address}
 }
 if(!address.includes(':')||!/^(\[[0-9a-f:.]+\]|[0-9a-f:.]+)$/i.test(address))return null
 try {
  const parsed=new URL(`http://[${address.replace(/^\[|\]$/g,'')}]/`).hostname.slice(1,-1)
  if(parsed==='::'||parsed==='::1'||/^(fe[89ab]|ff|::ffff:)/.test(parsed))return null
  return {type:'AAAA',address:parsed}
 } catch {return null}
}

export function suggestedDNSAddress(hostname:string):string {
 return dnsRecord(hostname)?.address??''
}

export function SetupDNSGuidance({domain,address,onAddressChange,externalCaddy,automatic=false}:{automatic?:boolean;domain:string;address:string;onAddressChange:(value:string)=>void;externalCaddy:boolean}) {
 const [copyStatus,setCopyStatus]=useState<'idle'|'copied'|'failed'>('idle')
 const record=dnsRecord(address)
 const name=`*.${domain}`
 async function copy(){
  if(!record||!domain)return
  try {
   if(!navigator.clipboard)throw new Error('clipboard unavailable')
   await navigator.clipboard.writeText(`${record.type}\t${name}\t${record.address}\t仅 DNS（灰云）`)
   setCopyStatus('copied')
  } catch {setCopyStatus('failed')}
 }
 return <section className="setup-dns-guidance" aria-label="Cloudflare DNS 配置指引">
  <div className="field-heading"><label htmlFor="setup-address">Caddy 访问 IP</label><HelpTooltip label="DNS 记录说明">通配符覆盖 {domain||'基础域名'} 下的一层子域，不包含根域名；已有精确记录请核对。{automatic?'预览不修改 DNS，确认配置时立即执行变更。':'请自行在 Cloudflare 保存仅 DNS（灰云）记录。'}</HelpTooltip></div>
  <input id="setup-address" placeholder="例如：192.168.2.6" value={address} onChange={event=>{onAddressChange(event.target.value);setCopyStatus('idle')}}/>
  <small className="muted">{externalCaddy?'填写外部 Caddy 的地址，不是 Manager 的地址。':'请填写访问设备能到达的 Caddy 地址。'}</small>
  {address.trim()&&!record&&<p className="text-danger">请填写可供其他设备访问的 IPv4 或 IPv6 地址，不含协议或端口。</p>}
  <div className="dns-record"><span>{record?.type??'A / AAAA'}</span><code>{domain?name:'*.基础域名'}</code><code>{record?.address??'待填写 IP'}</code><span>仅 DNS（灰云）</span><button type="button" className="button button-ghost button-sm" disabled={!record||!domain} onClick={()=>void copy()}>复制 DNS 记录</button></div>
  {copyStatus==='copied'&&<p className="muted" role="status">DNS 记录已复制</p>}
  {copyStatus==='failed'&&<p className="text-danger" role="alert">复制失败，请手动复制上方记录。</p>}
 </section>
}
