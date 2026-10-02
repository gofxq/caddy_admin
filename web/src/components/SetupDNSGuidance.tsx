import {useState} from 'react'

function dnsAddress(value:string):{type:'A'|'AAAA';address:string}|null {
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
 const record=dnsAddress(hostname)
 if(!record)return ''
 if(record.type==='AAAA')return /^(fc|fd)[0-9a-f]{2}:/.test(record.address)?record.address:''
 const [first,second]=record.address.split('.').map(Number)
 return first===10||first===192&&second===168||first===172&&second>=16&&second<=31?record.address:''
}

export function SetupDNSGuidance({domain,address,onAddressChange,externalCaddy}:{domain:string;address:string;onAddressChange:(value:string)=>void;externalCaddy:boolean}) {
 const [copyStatus,setCopyStatus]=useState<'idle'|'copied'|'failed'>('idle')
 const record=dnsAddress(address)
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
  <h3>先配置域名解析</h3>
  <p className="muted">在 Cloudflare 添加下面的记录，选择「仅 DNS（灰云）」。内网地址只供能到达该网络的设备访问。</p>
  <label>Caddy 访问 IP<input aria-label="Caddy 访问 IP" placeholder="例如：192.168.2.6" value={address} onChange={event=>{onAddressChange(event.target.value);setCopyStatus('idle')}}/><small>{externalCaddy?'填写外部 Caddy 的地址，不是当前 Manager 的地址。':'使用当前访问的内网 IP 作为建议；通过 localhost 访问时请手动填写服务器地址。'}请确认该地址能从访问设备到达。</small></label>
  {address.trim()&&!record&&<p className="text-danger">请填写可供其他设备访问的 IPv4 或 IPv6 地址，不含协议或端口。</p>}
  <dl className="dns-record">
   <div><dt>类型</dt><dd>{record?.type??'A / AAAA'}</dd></div>
   <div><dt>名称</dt><dd><code>{name}</code></dd></div>
   <div><dt>地址</dt><dd><code>{record?.address??'填写 Caddy 访问 IP 后显示'}</code></dd></div>
   <div><dt>代理状态</dt><dd>仅 DNS（灰云）</dd></div>
  </dl>
  <button className="button button-outline" disabled={!record||!domain} onClick={()=>void copy()}>复制 DNS 记录</button>
  {copyStatus==='copied'&&<p className="muted" role="status">DNS 记录已复制</p>}
  {copyStatus==='failed'&&<p className="text-danger" role="alert">复制失败，请手动复制上方记录。</p>}
  <p className="muted">通配符覆盖 {domain} 下的控制台和服务子域，不包含 {domain} 本身；已有单独子域记录时请核对其地址。此处仅生成配置指引，请自行在 Cloudflare 保存记录。</p>
 </section>
}
