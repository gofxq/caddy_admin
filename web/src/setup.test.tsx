import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {Setup} from './pages/Setup'

beforeEach(()=>{vi.stubGlobal('location',new URL('https://localhost/setup'))})
afterEach(()=>{cleanup();vi.unstubAllGlobals()})

const status=(external_caddy=false,resolver_suggestions:string[]=[])=>new Response(JSON.stringify({initialized:false,test_tls:true,external_caddy,resolver_suggestions,client_ip:'10.0.0.2'}),{status:200,headers:{'Content-Type':'application/json'}})
const preflight=(warnings=false)=>new Response(JSON.stringify({normalized:{admin_domain:'caddyadmin.home.example.com',origin:'https://caddyadmin.home.example.com',admin_origin:'https://caddyadmin.home.example.com'},network_valid:true,checks:[{id:'settings_valid',status:'pass',message:'设置有效'},...(warnings?[{id:'admin_dns',status:'warning',message:'控制台域名尚未解析'}]:[])],can_complete:true,requires_acknowledgement:warnings,warning_fingerprint:warnings?'warnings-current':''}),{status:200,headers:{'Content-Type':'application/json'}})

function dohReply(path:string,address='192.168.1.6'){
 const url=new URL(path),type=url.searchParams.get('type')==='AAAA'?28:1,name=url.searchParams.get('name')
 return new Response(JSON.stringify({Status:0,Question:[{name,type}],Answer:type===1&&address?[{name,type,data:address}]:[]}))
}
async function confirmAndCheck(){
 fireEvent.click(screen.getByRole('button',{name:'确认配置'}))
 await screen.findByText(/DNS 配置已确认/)
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
 await waitFor(()=>expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled())
}

async function reachDomains(){
 fireEvent.change(await screen.findByLabelText('管理员密码'),{target:{value:'a-secure-password'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
}

async function reachDNS(){
 await reachDomains()
 fireEvent.change(screen.getByLabelText('Homelab 域名'),{target:{value:'Home.Example.com.'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 fireEvent.change(screen.getByLabelText('可信网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 fireEvent.change(screen.getByLabelText('上游网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 await screen.findByLabelText('DNS 解析器（逗号分隔）')
}

async function reachConfirmation(){
 await reachDNS()
 fireEvent.change(screen.getByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'9.9.9.9'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 await screen.findByRole('heading',{name:'确认配置'})
}

it.each([
 ['192.168.2.36','192.168.2.0/24'],
 ['10.7.8.9','10.7.8.0/24'],
 ['fd12:3456:789a:bcde::9','fd12:3456:789a:bcde::/64'],
 ['fd12::9','fd12::/64'],
 ['127.0.0.1',''],
 ['::1',''],
 ['8.8.8.8',''],
 ['169.254.2.3',''],
 ['invalid',''],
])('suggests editable network ranges for source %s',async(clientIP,cidr)=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({initialized:false,test_tls:true,client_ip:clientIP}),{status:200})))
 render(<Setup pollLogin={false}/>);await reachDomains()
 fireEvent.change(screen.getByLabelText('Homelab 域名'),{target:{value:'home.example.com'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(screen.getByLabelText('可信网络 CIDR')).toHaveValue(cidr)
 expect(screen.getByLabelText('上游网络 CIDR')).toHaveValue(cidr)
 fireEvent.change(screen.getByLabelText('可信网络 CIDR'),{target:{value:'10.10.0.0/16'}})
 fireEvent.click(screen.getByRole('button',{name:'上一步'}));fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(screen.getByLabelText('可信网络 CIDR')).toHaveValue('10.10.0.0/16')
})

it('chooses browser DoH independently of the server resolver policy',async()=>{
 const fetch=vi.fn().mockImplementation(async(path:string)=>path.endsWith('/status')?status():path.startsWith('https://')?dohReply(path):preflight())
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>);await reachDNS()
 expect(screen.getByRole('button',{name:'Cloudflare DoH'})).toHaveAttribute('aria-pressed','true')
 fireEvent.click(screen.getByRole('button',{name:'Google DoH'}))
 expect(screen.getByRole('button',{name:'Google DoH'})).toHaveAttribute('aria-pressed','true')
 expect(screen.getByLabelText('DNS 解析器（逗号分隔）')).toHaveValue('1.1.1.1')
 fireEvent.change(screen.getByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'192.168.2.1'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'})
 expect(screen.getByRole('region',{name:'配置摘要'})).toHaveTextContent('192.168.2.1')
 const calls=fetch.mock.calls.filter(([path])=>path.endsWith('/preflight'))
 expect(JSON.parse(calls[calls.length-1][1].body).settings.resolvers).toEqual(['192.168.2.1'])
})

it('accepts eight characters but rejects seven characters in setup',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(status()))
 render(<Setup pollLogin={false}/>)
 const password=await screen.findByLabelText('管理员密码')
 for(const value of ['1234567','一二三四五六七']){
  fireEvent.change(password,{target:{value}})
  expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 }
 for(const value of ['12345678','一二三四五六七八']){
  fireEvent.change(password,{target:{value}})
  expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled()
 }
})

it('generates a visible editable password without submitting setup',async()=>{
 const fetch=vi.fn().mockResolvedValue(status());vi.stubGlobal('fetch',fetch)
 render(<Setup pollLogin={false}/>);const password=await screen.findByLabelText('管理员密码')
 fireEvent.click(screen.getByRole('button',{name:'随机生成'}))
 expect((password as HTMLInputElement).value).toMatch(/^[A-Za-z0-9_-]{20}$/)
 expect(password).toHaveAttribute('type','text')
 expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled()
 fireEvent.click(screen.getByRole('button',{name:'隐藏密码'}));expect(password).toHaveAttribute('type','password')
 fireEvent.change(password,{target:{value:'12345678'}});expect(password).toHaveValue('12345678')
 expect(fetch.mock.calls.some(([path])=>path==='/api/v1/setup/complete')).toBe(false)
})

it('initializes isolated TLS mode without a token and derives the console domain from Homelab',async()=>{
 const fetch=vi.fn().mockResolvedValueOnce(status()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(new Response(JSON.stringify({status:'restarting',admin_origin:'https://caddyadmin.home.example.com'}),{status:202,headers:{'Content-Type':'application/json'}}))
 vi.stubGlobal('fetch',fetch)
 render(<Setup pollLogin={false}/>)
 expect(await screen.findByRole('heading',{name:'初始化 Caddy Admin'})).toBeInTheDocument()
 expect(screen.getByText(/第一个成功提交者将成为管理员/)).toBeInTheDocument()
 await reachConfirmation()
 expect(screen.queryByLabelText('公网域名')).not.toBeInTheDocument()
 expect(screen.queryByLabelText('控制台域名')).not.toBeInTheDocument()
 expect(screen.getByText('caddyadmin.home.example.com')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 await waitFor(()=>expect(fetch).toHaveBeenCalledTimes(4))
 const [,options]=fetch.mock.calls[3]
 expect(options.headers).toEqual({'Content-Type':'application/json'})
 const body=JSON.parse(options.body)
 expect(body.settings).toEqual({homelab_domain:'Home.Example.com.',lan_cidrs:['10.0.0.0/8'],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:['9.9.9.9']})
 expect(body.acknowledge_warnings).toBe(false)
 expect(screen.getByText('初始化已完成')).toBeInTheDocument()
 expect(screen.getByRole('link',{name:'通过临时入口继续登录'})).toBeInTheDocument()
})

it('does not ask for a local Cloudflare token in external Caddy mode',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status(true):path.startsWith('https://')?dohReply(path):preflight()))
 render(<Setup pollLogin={false}/>)
 await reachDomains()
 expect(screen.getByLabelText('Homelab 域名')).toBeInTheDocument()
 expect(screen.queryByLabelText('公网域名')).not.toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('Homelab 域名'),{target:{value:'home.example.com'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 fireEvent.change(screen.getByLabelText('可信网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 fireEvent.change(screen.getByLabelText('上游网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 fireEvent.change(await screen.findByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'9.9.9.9'}})
 expect(await screen.findByText(/专用外部 Caddy 实例安全配置/)).toBeInTheDocument()
 expect(screen.queryByLabelText('Cloudflare Token')).not.toBeInTheDocument()
})

it('requires explicit network and resolver policy before confirmation',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status():path.startsWith('https://')?dohReply(path):preflight()))
 render(<Setup pollLogin={false}/>)
 await reachDomains()
 fireEvent.change(screen.getByLabelText('Homelab 域名'),{target:{value:'home.example.com'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 fireEvent.change(screen.getByLabelText('可信网络 CIDR'),{target:{value:''}})
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 fireEvent.change(screen.getByLabelText('可信网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 fireEvent.change(screen.getByLabelText('上游网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled()
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 await screen.findByLabelText('DNS 解析器（逗号分隔）')
 fireEvent.change(screen.getByLabelText('DNS 解析器（逗号分隔）'),{target:{value:''}})
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 fireEvent.change(await screen.findByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'9.9.9.9'}})
 expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled()
})

it('blocks leaving the network step when server network validation fails',async()=>{
 const blocked=()=>new Response(JSON.stringify({normalized:{},network_valid:false,network_error:'网络格式无效',checks:[{id:'settings_valid',status:'block',message:'网络格式无效'}],can_complete:false,requires_acknowledgement:false}),{status:200})
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status():blocked()))
 render(<Setup pollLogin={false}/>)
 await reachDomains()
 fireEvent.change(screen.getByLabelText('Homelab 域名'),{target:{value:'home.example.com'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 fireEvent.change(screen.getByLabelText('可信网络 CIDR'),{target:{value:'invalid'}})
 fireEvent.change(screen.getByLabelText('上游网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('网络格式无效')
 expect(screen.getByLabelText('可信网络 CIDR')).toHaveValue('invalid')
 expect(screen.queryByLabelText('DNS 解析器（逗号分隔）')).not.toBeInTheDocument()
})

it('keeps form data when another request wins initialization',async()=>{
 const conflict=new Response(JSON.stringify({error:{message:'系统已被其他请求初始化，请前往登录'}}),{status:409,headers:{'Content-Type':'application/json'}})
 vi.stubGlobal('fetch',vi.fn().mockResolvedValueOnce(status()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(conflict))
 render(<Setup pollLogin={false}/>)
 await reachConfirmation()
 fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('系统已被其他请求初始化，请前往登录')
 fireEvent.click(screen.getByRole('button',{name:'上一步'}))
 fireEvent.click(screen.getByRole('button',{name:'上一步'}))
 fireEvent.click(screen.getByRole('button',{name:'上一步'}))
 expect(screen.getByLabelText('Homelab 域名')).toHaveValue('Home.Example.com.')
})

it('offers resolver suggestions without applying them automatically',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status(false,['10.0.0.53']):path.startsWith('https://')?dohReply(path):preflight()))
 render(<Setup pollLogin={false}/>)
 await reachDomains()
 fireEvent.change(screen.getByLabelText('Homelab 域名'),{target:{value:'home.example.com'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 fireEvent.change(screen.getByLabelText('可信网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 fireEvent.change(screen.getByLabelText('上游网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(await screen.findByLabelText('DNS 解析器（逗号分隔）')).toHaveValue('1.1.1.1')
 fireEvent.click(screen.getByRole('button',{name:'采用 10.0.0.53'}))
 expect(await screen.findByLabelText('DNS 解析器（逗号分隔）')).toHaveValue('10.0.0.53')
})

it('requires explicit acknowledgement for preflight warnings',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValueOnce(status()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(preflight(true)))
 render(<Setup pollLogin={false}/>)
 await reachConfirmation()
 expect(screen.getByText('控制台域名尚未解析')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'完成初始化'})).toBeDisabled()
 fireEvent.click(screen.getByRole('checkbox'))
 expect(screen.getByRole('button',{name:'完成初始化'})).toBeEnabled()
})

it('recovers a refreshed external handoff page after setup status closes',async()=>{
 const handoff={initialized:true,mode:'external',admin_origin:'https://caddyadmin.home.example.com',manager_status:'ready',dns_status:'ready',console_status:'pending',temporary_entry:true,checked_at:'2026-10-01T00:00:00Z'}
 vi.stubGlobal('fetch',vi.fn().mockRejectedValueOnce(new Error('restarting')).mockResolvedValueOnce(new Response(JSON.stringify(handoff),{status:200,headers:{'Content-Type':'application/json'}})))
 render(<Setup pollLogin={false}/>)
 expect(await screen.findByRole('heading',{name:'初始化已完成'})).toBeInTheDocument()
 expect(screen.getByText(/服务器已能解析域名/)).toBeInTheDocument()
 expect(screen.queryByRole('link',{name:'通过临时入口继续登录'})).not.toBeInTheDocument()
})

it('polls handoff without replaying setup when the completion response is lost',async()=>{
 const handoff={initialized:true,mode:'embedded',admin_origin:'https://caddyadmin.home.example.com',manager_status:'ready',dns_status:'pending',console_status:'pending',temporary_entry:true,checked_at:'2026-10-01T00:00:00Z'}
 const fetch=vi.fn().mockResolvedValueOnce(status()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(preflight()).mockRejectedValueOnce(new Error('response lost')).mockResolvedValue(new Response(JSON.stringify(handoff),{status:200,headers:{'Content-Type':'application/json'}}))
 vi.stubGlobal('fetch',fetch)
 render(<Setup/>)
 await reachConfirmation()
 fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 expect(await screen.findByRole('heading',{name:'初始化已完成'})).toBeInTheDocument()
 await screen.findByText(/https:\/\/caddyadmin.home.example.com/)
 expect(fetch.mock.calls.filter(([path])=>path==='/api/v1/setup/complete')).toHaveLength(1)
})

it('offers a connection retry when both initial endpoints are temporarily unavailable',async()=>{
 const handoff={initialized:true,mode:'external',admin_origin:'https://caddyadmin.home.example.com',manager_status:'ready',dns_status:'ready',console_status:'pending',temporary_entry:true,checked_at:'2026-10-01T00:00:00Z'}
 const fetch=vi.fn().mockRejectedValueOnce(new Error('setup closed')).mockRejectedValueOnce(new Error('handoff starting')).mockImplementation(async()=>new Response(JSON.stringify(handoff)))
 vi.stubGlobal('fetch',fetch);render(<Setup/>)
 expect(await screen.findByRole('heading',{name:'无法连接初始化服务'})).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'重试连接'}))
 await screen.findByRole('heading',{name:'初始化已完成'})
 await screen.findByText(/https:\/\/caddyadmin.home.example.com/)
})

it('suggests the accessed LAN address and copies an editable DNS-only wildcard record',async()=>{
 vi.stubGlobal('location',new URL('https://192.168.2.6/setup'))
 const writeText=vi.fn().mockResolvedValue(undefined)
 vi.stubGlobal('navigator',{clipboard:{writeText}})
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status():path.startsWith('https://')?dohReply(path):preflight()))
 render(<Setup pollLogin={false}/>)
 await reachDNS()
 const address=screen.getByLabelText('Caddy 访问 IP')
 expect(address).toHaveValue('192.168.2.6')
 expect(screen.getByText('*.home.example.com')).toBeInTheDocument()
 expect(screen.getByText('仅 DNS（灰云）')).toBeInTheDocument()
 fireEvent.change(address,{target:{value:'192.168.2.8'}})
 fireEvent.click(screen.getByRole('button',{name:'复制 DNS 记录'}))
 await screen.findByText('DNS 记录已复制')
 expect(writeText).toHaveBeenCalledWith('A\t*.home.example.com\t192.168.2.8\t仅 DNS（灰云）')
 fireEvent.click(screen.getByRole('button',{name:'上一步'}))
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(await screen.findByLabelText('Caddy 访问 IP')).toHaveValue('192.168.2.8')
})

it('leaves the DNS target empty for localhost and external Caddy instead of using the client IP',async()=>{
 vi.stubGlobal('location',new URL('https://localhost/setup'))
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status():path.startsWith('https://')?dohReply(path):preflight()))
 const page=render(<Setup pollLogin={false}/>)
 await reachDNS()
 expect(screen.getByLabelText('Caddy 访问 IP')).toHaveValue('')
 expect(screen.getByRole('button',{name:'复制 DNS 记录'})).toBeDisabled()
 page.unmount()
 vi.stubGlobal('location',new URL('https://192.168.2.6:8082/setup'))
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status(true):path.startsWith('https://')?dohReply(path):preflight()))
 render(<Setup pollLogin={false}/>)
 await reachDNS()
 expect(screen.getByLabelText('Caddy 访问 IP')).toHaveValue('')
 expect(screen.getByText(/填写外部 Caddy 的地址/)).toBeInTheDocument()
})

it('rechecks browser DoH on demand and retains input after a failed check',async()=>{
 let failed=true
 const fetch=vi.fn().mockImplementation(async(path:string)=>{
  if(path.endsWith('/status'))return status()
  if(path.startsWith('https://')){if(failed)throw new TypeError('Failed to fetch');return dohReply(path,'')}
  return preflight()
 })
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>);await reachDNS()
 fireEvent.change(screen.getByLabelText('Caddy 访问 IP'),{target:{value:'192.168.2.6'}})
 fireEvent.change(screen.getByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'192.168.2.1'}})
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
 await waitFor(()=>expect(screen.getByRole('table',{name:'DNS 查询结果'})).toHaveTextContent('无法连接 DoH'))
 expect(screen.getByLabelText('Caddy 访问 IP')).toHaveValue('192.168.2.6')
 expect(screen.getByLabelText('DNS 解析器（逗号分隔）')).toHaveValue('192.168.2.1')
 failed=false;fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
 await waitFor(()=>expect(screen.getByRole('table',{name:'DNS 查询结果'})).toHaveTextContent('未返回 A/AAAA 地址'))
 expect(fetch.mock.calls.some(([path])=>path.endsWith('/dns/check')||path.endsWith('/complete'))).toBe(false)
})

it('uses AAAA for a private IPv6 target and rejects hostnames or port-bearing addresses',async()=>{
 vi.stubGlobal('location',new URL('https://[fd12:3456::6]/setup'))
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status():path.startsWith('https://')?dohReply(path):preflight()))
 render(<Setup pollLogin={false}/>)
 await reachDNS()
 expect(screen.getByLabelText('Caddy 访问 IP')).toHaveValue('fd12:3456::6')
 expect(screen.getByText('AAAA')).toBeInTheDocument()
 for(const value of ['caddy.internal','192.168.2.6:443','999.168.2.6','127.0.0.1','fd12:3456::6]/path']){
  fireEvent.change(screen.getByLabelText('Caddy 访问 IP'),{target:{value}})
  expect(screen.getByRole('button',{name:'复制 DNS 记录'})).toBeDisabled()
  expect(screen.getByText('请填写可供其他设备访问的 IPv4 或 IPv6 地址，不含协议或端口。')).toBeInTheDocument()
 }
})

it('shows a manual copy fallback when the clipboard is unavailable',async()=>{
 vi.stubGlobal('navigator',{clipboard:{writeText:vi.fn().mockRejectedValue(new Error('denied'))}})
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status():path.startsWith('https://')?dohReply(path):preflight()))
 render(<Setup pollLogin={false}/>)
 await reachDNS()
 fireEvent.change(screen.getByLabelText('Caddy 访问 IP'),{target:{value:'192.168.2.6'}})
 fireEvent.click(screen.getByRole('button',{name:'复制 DNS 记录'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('复制失败，请手动复制上方记录。')
 expect(screen.getByText('*.home.example.com')).toBeInTheDocument()
 expect(screen.getByText('192.168.2.6')).toBeInTheDocument()
})

it('shows browser DNS check progress and results while preventing duplicate checks',async()=>{
 let finishCheck!:()=>void
 const fetch=vi.fn().mockImplementation(async(path:string)=>{
  if(path.endsWith('/status'))return status()
  if(path.startsWith('https://')){
   const url=new URL(path)
   if(url.searchParams.get('name')!.startsWith('caddyadmin.')&&url.searchParams.get('type')==='A')await new Promise<void>(resolve=>{finishCheck=resolve})
   return dohReply(path,'192.168.2.6')
  }
  return preflight()
 })
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>);await reachDNS()
 fireEvent.change(screen.getByLabelText('Caddy 访问 IP'),{target:{value:'192.168.2.6'}})
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
 expect(screen.getByRole('button',{name:'正在检查 DNS…'})).toBeDisabled()
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 finishCheck()
 await waitFor(()=>expect(screen.getByRole('table',{name:'DNS 查询结果'})).toHaveTextContent('192.168.2.6'))
 expect(screen.getByRole('button',{name:'检查 DNS'})).toBeEnabled()
 expect(fetch.mock.calls.filter(([path])=>path.endsWith('/preflight'))).toHaveLength(1)
 expect(fetch.mock.calls.filter(([path])=>path.startsWith('https://'))).toHaveLength(4)
})

it('previews Cloudflare DNS and binds explicit confirmation to initialization',async()=>{
 vi.stubGlobal('location',new URL('http://192.168.1.6/setup'))
 const plan={zone_id:'zone',name:'*.home.example.com',type:'A',address:'192.168.1.6',action:'update',old_address:'192.168.1.9',old_proxied:true,fingerprint:'dns-preview'}
 const fetch=vi.fn().mockImplementation(async(path:string)=>path.endsWith('/status')?new Response(JSON.stringify({initialized:false,external_caddy:false,test_tls:false}),{status:200}):path.startsWith('https://')?dohReply(path):(path.endsWith('/dns/preview')||path.endsWith('/dns/confirm'))?new Response(JSON.stringify(plan),{status:200}):path.endsWith('/complete')?new Response(JSON.stringify({status:'restarting',admin_origin:'https://caddyadmin.home.example.com'}),{status:202}):preflight())
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>);await reachDNS()
 fireEvent.click(screen.getByText('获取 Token 与权限'))
 expect(screen.getByRole('link',{name:'创建 Cloudflare API Token'})).toHaveAttribute('href','https://dash.cloudflare.com/profile/api-tokens')
 expect(screen.getByText(/HTTP 会明文传输管理员密码和 Token/)).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'192.168.1.1'}})
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'}})
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 fireEvent.click(screen.getByRole('button',{name:'预览 DNS 变更'}));await screen.findByText(/192.168.1.9/)
 fireEvent.click(screen.getByRole('checkbox',{name:/确认.*DNS/}));expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled();await confirmAndCheck()
 fireEvent.click(screen.getByRole('button',{name:'Google DoH'}))
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 expect(screen.getByRole('checkbox',{name:/确认.*DNS/})).toBeChecked()
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}));await waitFor(()=>expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled())
 fireEvent.change(screen.getByLabelText('Caddy 访问 IP'),{target:{value:'192.168.1.8'}});expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 fireEvent.change(screen.getByLabelText('Caddy 访问 IP'),{target:{value:'192.168.1.6'}});expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 fireEvent.click(screen.getByRole('button',{name:'预览 DNS 变更'}));await screen.findByText(/192.168.1.9/)
 fireEvent.click(screen.getByRole('checkbox',{name:/确认.*DNS/}));await confirmAndCheck();fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'});fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 await waitFor(()=>expect(fetch.mock.calls.some(c=>c[0].endsWith('/complete'))).toBe(true))
 const options=fetch.mock.calls.find(c=>c[0].endsWith('/complete'))![1] as RequestInit
 expect(JSON.parse(options.body as string).cloudflare).toEqual({token:'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',address:'192.168.1.6',fingerprint:'dns-preview',confirmed:true})
 expect(screen.getByRole('link',{name:'通过临时入口继续登录'})).toHaveAttribute('href','https://192.168.1.6/')
})

it('moves an accepted HTTP initialization to the HTTPS handoff without replaying credentials',async()=>{
 const assign=vi.fn();vi.stubGlobal('location',{protocol:'http:',hostname:'192.168.1.6',origin:'http://192.168.1.6',assign})
 const plan={name:'*.home.example.com',type:'A',address:'192.168.1.6',action:'create',fingerprint:'preview'}
 const fetch=vi.fn().mockImplementation(async(path:string)=>path.endsWith('/status')?new Response(JSON.stringify({initialized:false,external_caddy:false,test_tls:false}),{status:200}):path.startsWith('https://')?dohReply(path):(path.endsWith('/dns/preview')||path.endsWith('/dns/confirm'))?new Response(JSON.stringify(plan),{status:200}):path.endsWith('/complete')?new Response(JSON.stringify({status:'restarting',admin_origin:'https://caddyadmin.home.example.com'}),{status:202}):preflight())
 vi.stubGlobal('fetch',fetch);render(<Setup/>);await reachDNS()
 fireEvent.change(screen.getByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'192.168.1.1'}});fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'}})
 fireEvent.click(screen.getByRole('button',{name:'预览 DNS 变更'}));await screen.findByRole('checkbox',{name:/确认.*DNS/});fireEvent.click(screen.getByRole('checkbox',{name:/确认.*DNS/}));await confirmAndCheck();fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'});fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 await waitFor(()=>expect(assign).toHaveBeenCalledWith('https://192.168.1.6/setup'))
 expect(fetch.mock.calls.filter(c=>c[0].endsWith('/complete'))).toHaveLength(1)
 expect(fetch.mock.calls.some(c=>c[0].endsWith('/handoff'))).toBe(false)
})

it('retains DNS inputs and blocks progress until confirmation retry succeeds',async()=>{
 vi.stubGlobal('location',new URL('http://192.168.1.6/setup'))
 let attempts=0
 const fetch=vi.fn().mockImplementation(async(path:string)=>{
  if(path.endsWith('/status'))return new Response(JSON.stringify({initialized:false,external_caddy:false,test_tls:false}))
  if(path.endsWith('/dns/preview'))return new Response(JSON.stringify({name:'*.home.example.com',type:'A',address:'192.168.1.6',action:'create',fingerprint:'preview'}))
  if(path.startsWith('https://'))return dohReply(path,attempts<4?(attempts++,'192.168.1.8'):'192.168.1.6')
  return preflight()
 })
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>);await reachDNS()
 fireEvent.change(screen.getByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'192.168.1.1'}})
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'}})
 fireEvent.click(screen.getByRole('button',{name:'预览 DNS 变更'}));await screen.findByRole('checkbox',{name:/确认.*DNS/})
 fireEvent.click(screen.getByRole('checkbox',{name:/确认.*DNS/}))
 fireEvent.click(screen.getByRole('button',{name:'确认配置'}));await screen.findByText(/DNS 配置已确认/)
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}));await waitFor(()=>expect(screen.getByRole('table',{name:'DNS 查询结果'})).toHaveTextContent('解析地址不符'))
 expect(screen.getByLabelText('Cloudflare API Token')).toHaveValue('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}));await waitFor(()=>expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled())
 fireEvent.click(screen.getByRole('button',{name:'Google DoH'}))
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 expect(fetch.mock.calls.some(c=>c[0].endsWith('/complete'))).toBe(false)
})

 it('offers optional field guidance by click and keyboard focus',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(status()))
 render(<Setup pollLogin={false}/>);await reachDomains()
 const help=screen.getByRole('button',{name:'Homelab 域名说明'})
 expect(screen.queryByRole('tooltip')).not.toBeInTheDocument()
 fireEvent.mouseEnter(help)
 fireEvent.mouseMove(help)
 expect(await screen.findByRole('tooltip')).toHaveTextContent('仅配置 Homelab 域名')
 fireEvent.mouseLeave(help)
 await waitFor(()=>expect(screen.queryByRole('tooltip')).not.toBeInTheDocument())
 fireEvent.click(help)
 expect(await screen.findByRole('tooltip')).toHaveTextContent('仅配置 Homelab 域名')
 fireEvent.keyDown(help,{key:'Escape'})
 await waitFor(()=>expect(screen.queryByRole('tooltip')).not.toBeInTheDocument())
 fireEvent.blur(help)
 act(()=>help.focus())
 expect(await screen.findByRole('tooltip')).toHaveTextContent('仅配置 Homelab 域名')
 })

it('separates DNS configuration from a read-only check and shows real query results',async()=>{
 vi.stubGlobal('location',new URL('https://192.168.1.6/setup'))
 let matches=false
 const fetch=vi.fn().mockImplementation(async(path:string)=>path.endsWith('/status')?new Response(JSON.stringify({initialized:false,test_tls:false})):path.endsWith('/dns/preview')?new Response(JSON.stringify({name:'*.home.example.com',type:'A',address:'192.168.1.6',action:'create',fingerprint:'preview'})):path.startsWith('https://')?dohReply(path,matches||new URL(path).searchParams.get('name')!.startsWith('caddyadmin.')?'192.168.1.6':'192.168.1.8'):preflight())
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>);await reachDNS()
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'token'}})
 expect(screen.getByRole('button',{name:'检查 DNS'})).toBeEnabled()
 matches=true;fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}));await screen.findByRole('table',{name:'DNS 查询结果'})
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 matches=false
 fireEvent.click(screen.getByRole('button',{name:'预览 DNS 变更'}));fireEvent.click(await screen.findByRole('checkbox',{name:/确认.*DNS/}))
 fireEvent.click(screen.getByRole('button',{name:'确认配置'}));await screen.findByText(/DNS 配置已确认/)
 expect(fetch.mock.calls.filter(([path])=>path.startsWith('https://'))).toHaveLength(4)
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}));await screen.findByRole('table',{name:'DNS 查询结果'})
 expect(screen.getByRole('table',{name:'DNS 查询结果'})).toHaveTextContent('192.168.1.8')
 expect(screen.getByRole('table',{name:'DNS 查询结果'})).toHaveTextContent('解析地址不符')
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 matches=true;fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}));await waitFor(()=>expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled())
 fireEvent.click(screen.getByRole('button',{name:'Google DoH'}))
 expect(screen.queryByRole('table',{name:'DNS 查询结果'})).not.toBeInTheDocument()
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
})
