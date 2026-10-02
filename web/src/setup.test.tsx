import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {Setup} from './pages/Setup'

afterEach(()=>{cleanup();vi.unstubAllGlobals()})

const status=(external_caddy=false,resolver_suggestions:string[]=[])=>new Response(JSON.stringify({initialized:false,external_caddy,resolver_suggestions,client_ip:'10.0.0.2'}),{status:200,headers:{'Content-Type':'application/json'}})
const preflight=(warnings=false)=>new Response(JSON.stringify({normalized:{admin_domain:'caddyadmin.home.example.com',origin:'https://caddyadmin.home.example.com',admin_origin:'https://caddyadmin.home.example.com'},network_valid:true,checks:[{id:'settings_valid',status:'pass',message:'设置有效'},...(warnings?[{id:'admin_dns',status:'warning',message:'控制台域名尚未解析'}]:[])],can_complete:true,requires_acknowledgement:warnings}),{status:200,headers:{'Content-Type':'application/json'}})

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

it('initializes without a token and derives the console domain from Homelab',async()=>{
 const fetch=vi.fn().mockResolvedValueOnce(status()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(new Response(JSON.stringify({status:'restarting',admin_origin:'https://caddyadmin.home.example.com'}),{status:202,headers:{'Content-Type':'application/json'}}))
 vi.stubGlobal('fetch',fetch)
 render(<Setup pollLogin={false}/>)
 expect(await screen.findByRole('heading',{name:'初始化 Caddy Admin'})).toBeInTheDocument()
 expect(screen.getByText(/第一个成功提交者将成为管理员/)).toBeInTheDocument()
 await reachConfirmation()
 expect(screen.queryByLabelText('公网域名')).not.toBeInTheDocument()
 expect(screen.queryByLabelText('控制台域名')).not.toBeInTheDocument()
 expect(screen.getByText('caddyadmin.home.example.com')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 await waitFor(()=>expect(fetch).toHaveBeenCalledTimes(5))
 const [,options]=fetch.mock.calls[4]
 expect(options.headers).toEqual({'Content-Type':'application/json'})
 const body=JSON.parse(options.body)
 expect(body.settings).toEqual({homelab_domain:'Home.Example.com.',lan_cidrs:['10.0.0.0/8'],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:['9.9.9.9']})
 expect(body.acknowledge_warnings).toBe(false)
 expect(screen.getByText('服务正在重启')).toBeInTheDocument()
 expect(screen.getByRole('link',{name:'通过临时入口继续登录'})).toBeInTheDocument()
})

it('does not ask for a local Cloudflare token in external Caddy mode',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status(true):preflight()))
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
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status():preflight()))
 render(<Setup pollLogin={false}/>)
 await reachDomains()
 fireEvent.change(screen.getByLabelText('Homelab 域名'),{target:{value:'home.example.com'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 fireEvent.change(screen.getByLabelText('可信网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 fireEvent.change(screen.getByLabelText('上游网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled()
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 await screen.findByLabelText('DNS 解析器（逗号分隔）')
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
 vi.stubGlobal('fetch',vi.fn().mockResolvedValueOnce(status()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(conflict))
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
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status(false,['10.0.0.53']):preflight()))
 render(<Setup pollLogin={false}/>)
 await reachDomains()
 fireEvent.change(screen.getByLabelText('Homelab 域名'),{target:{value:'home.example.com'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 fireEvent.change(screen.getByLabelText('可信网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 fireEvent.change(screen.getByLabelText('上游网络 CIDR'),{target:{value:'10.0.0.0/8'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(await screen.findByLabelText('DNS 解析器（逗号分隔）')).toHaveValue('')
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
 expect(await screen.findByRole('heading',{name:'服务正在重启'})).toBeInTheDocument()
 expect(screen.getByText(/服务器已能解析域名/)).toBeInTheDocument()
 expect(screen.queryByRole('link',{name:'通过临时入口继续登录'})).not.toBeInTheDocument()
})

it('polls handoff without replaying setup when the completion response is lost',async()=>{
 const handoff={initialized:true,mode:'embedded',admin_origin:'https://caddyadmin.home.example.com',manager_status:'ready',dns_status:'pending',console_status:'pending',temporary_entry:true,checked_at:'2026-10-01T00:00:00Z'}
 const fetch=vi.fn().mockResolvedValueOnce(status()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(preflight()).mockResolvedValueOnce(preflight()).mockRejectedValueOnce(new Error('response lost')).mockResolvedValue(new Response(JSON.stringify(handoff),{status:200,headers:{'Content-Type':'application/json'}}))
 vi.stubGlobal('fetch',fetch)
 render(<Setup/>)
 await reachConfirmation()
 fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 expect(await screen.findByRole('heading',{name:'服务正在重启'})).toBeInTheDocument()
 await screen.findByText(/https:\/\/caddyadmin.home.example.com/)
 expect(fetch.mock.calls.filter(([path])=>path==='/api/v1/setup/complete')).toHaveLength(1)
})

it('keeps polling handoff when both initial endpoints are temporarily unavailable',async()=>{
 const handoff={initialized:true,mode:'external',admin_origin:'https://caddyadmin.home.example.com',manager_status:'ready',dns_status:'ready',console_status:'pending',temporary_entry:true,checked_at:'2026-10-01T00:00:00Z'}
 const fetch=vi.fn().mockRejectedValueOnce(new Error('setup closed')).mockRejectedValueOnce(new Error('handoff starting')).mockResolvedValue(new Response(JSON.stringify(handoff),{status:200,headers:{'Content-Type':'application/json'}}))
 vi.stubGlobal('fetch',fetch)
 render(<Setup/>)
 expect(await screen.findByRole('heading',{name:'服务正在重启'})).toBeInTheDocument()
 await screen.findByText(/https:\/\/caddyadmin.home.example.com/)
})


it('suggests the accessed LAN address and copies an editable DNS-only wildcard record',async()=>{
 vi.stubGlobal('location',new URL('https://192.168.2.6/setup'))
 const writeText=vi.fn().mockResolvedValue(undefined)
 vi.stubGlobal('navigator',{clipboard:{writeText}})
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status():preflight()))
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
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status():preflight()))
 const page=render(<Setup pollLogin={false}/>)
 await reachDNS()
 expect(screen.getByLabelText('Caddy 访问 IP')).toHaveValue('')
 expect(screen.getByRole('button',{name:'复制 DNS 记录'})).toBeDisabled()
 page.unmount()
 vi.stubGlobal('location',new URL('https://192.168.2.6:8082/setup'))
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status(true):preflight()))
 render(<Setup pollLogin={false}/>)
 await reachDNS()
 expect(screen.getByLabelText('Caddy 访问 IP')).toHaveValue('')
 expect(screen.getByText(/填写外部 Caddy 的地址/)).toBeInTheDocument()
})

it('rechecks DNS on demand and retains input after a failed check without submitting setup',async()=>{
 const fetch=vi.fn().mockResolvedValueOnce(status()).mockResolvedValueOnce(preflight())
   .mockRejectedValueOnce(new Error('网络中断，请重试'))
   .mockResolvedValueOnce(preflight(true))
 vi.stubGlobal('fetch',fetch)
 render(<Setup pollLogin={false}/>)
 await reachDNS()
 fireEvent.change(screen.getByLabelText('Caddy 访问 IP'),{target:{value:'192.168.2.6'}})
 fireEvent.change(screen.getByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'192.168.2.1'}})
 fireEvent.click(screen.getByRole('button',{name:'重新检查 DNS'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('网络中断，请重试')
 expect(screen.getByLabelText('Caddy 访问 IP')).toHaveValue('192.168.2.6')
 expect(screen.getByLabelText('DNS 解析器（逗号分隔）')).toHaveValue('192.168.2.1')
 fireEvent.click(screen.getByRole('button',{name:'重新检查 DNS'}))
 expect(await screen.findByText('控制台域名尚未解析')).toBeInTheDocument()
 const [,options]=fetch.mock.calls[3]
 expect(JSON.parse(options.body).settings.resolvers).toEqual(['192.168.2.1'])
 expect(JSON.parse(options.body).settings).not.toHaveProperty('dns_address')
 expect(fetch.mock.calls.filter(([path])=>path==='/api/v1/setup/complete')).toHaveLength(0)
})

it('uses AAAA for a private IPv6 target and rejects hostnames or port-bearing addresses',async()=>{
 vi.stubGlobal('location',new URL('https://[fd12:3456::6]/setup'))
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status():preflight()))
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
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async path=>path==='/api/v1/setup/status'?status():preflight()))
 render(<Setup pollLogin={false}/>)
 await reachDNS()
 fireEvent.change(screen.getByLabelText('Caddy 访问 IP'),{target:{value:'192.168.2.6'}})
 fireEvent.click(screen.getByRole('button',{name:'复制 DNS 记录'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('复制失败，请手动复制上方记录。')
 expect(screen.getByText('*.home.example.com')).toBeInTheDocument()
 expect(screen.getByText('192.168.2.6')).toBeInTheDocument()
})

it('shows DNS check progress and results while preventing duplicate checks',async()=>{
 let finishCheck!:(response:Response)=>void
 const fetch=vi.fn().mockResolvedValueOnce(status()).mockResolvedValueOnce(preflight())
  .mockImplementationOnce(()=>new Promise<Response>(resolve=>{finishCheck=resolve}))
 vi.stubGlobal('fetch',fetch)
 render(<Setup pollLogin={false}/>)
 await reachDNS()
 expect(screen.getByRole('button',{name:'重新检查 DNS'})).toBeDisabled()
 fireEvent.change(screen.getByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'192.168.2.1'}})
 fireEvent.click(screen.getByRole('button',{name:'重新检查 DNS'}))
 expect(screen.getByRole('button',{name:'正在检查 DNS…'})).toBeDisabled()
 expect(screen.getByRole('button',{name:'正在预检…'})).toBeDisabled()
 finishCheck(new Response(JSON.stringify({normalized:{},network_valid:true,checks:[
  {id:'resolver_reachable',status:'pass',message:'至少一个 DNS 解析器可响应'},
  {id:'admin_dns',status:'pass',message:'控制台域名已有解析结果'}
 ],can_complete:true,requires_acknowledgement:false}),{status:200}))
 expect(await screen.findByText('控制台域名已有解析结果')).toBeInTheDocument()
 expect(screen.getByText('至少一个 DNS 解析器可响应')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'重新检查 DNS'})).toBeEnabled()
 expect(fetch.mock.calls.filter(([path])=>path==='/api/v1/setup/preflight')).toHaveLength(2)
})
