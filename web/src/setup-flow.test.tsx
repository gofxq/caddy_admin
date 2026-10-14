import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {Setup} from './pages/Setup'

beforeEach(()=>vi.stubGlobal('location',new URL('https://192.168.2.6/setup')))
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
const reply=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status})
const preflight=(warning='')=>({normalized:{admin_domain:'caddyadmin.h.cmx.ee',admin_origin:'https://caddyadmin.h.cmx.ee'},network_valid:true,can_complete:true,requires_acknowledgement:!!warning,warning_fingerprint:warning,checks:warning?[{id:warning,status:'warning',message:warning}]:[]})
function wireDNS(path:string,address='192.168.2.6'){
 const url=new URL(path),type=url.searchParams.get('type')==='AAAA'?28:1,name=url.searchParams.get('name')
 return reply({Status:0,Question:[{name,type}],Answer:type===1?[{name,type,data:address}]:[]})
}
function installAPI({automatic=false,action='create',complete=()=>reply({admin_origin:'https://caddyadmin.h.cmx.ee'},202),warning=()=>''}:{automatic?:boolean;action?:'create'|'update'|'reuse';complete?:()=>Response;warning?:()=>string}={}){
 const fetch=vi.fn().mockImplementation(async(path:string)=>{
  if(path.startsWith('https://'))return wireDNS(path)
  if(path.endsWith('/status'))return reply({initialized:false,test_tls:!automatic,client_ip:'192.168.2.9'})
  if(path.endsWith('/preflight'))return reply(preflight(warning()))
  if(path.endsWith('/dns/preview')||path.endsWith('/dns/confirm'))return reply({name:'*.h.cmx.ee',type:'A',address:'192.168.2.6',action,fingerprint:'plan'})
  if(path.endsWith('/complete'))return complete()
  throw new Error(`Unexpected request ${path}`)
 })
 vi.stubGlobal('fetch',fetch)
 return fetch
}
async function reachDNS(){fireEvent.change(await screen.findByLabelText('管理员密码'),{target:{value:'a-secure-password'}});fireEvent.click(screen.getByRole('button',{name:'下一步'}));fireEvent.change(screen.getByLabelText('首个通配符域名'),{target:{value:'h.cmx.ee'}});await screen.findByRole('heading',{name:'首个域名与 DNS'})}

it('continues with existing DNS after checking without showing change confirmation or writing DNS',async()=>{
 const fetch=installAPI({automatic:true,action:'reuse'});render(<Setup pollLogin={false}/>);await reachDNS()
 expect(screen.queryByRole('group',{name:'DNS 演示场景'})).not.toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'预览 DNS 变更'})).not.toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'确认配置'})).not.toBeInTheDocument()
 expect(screen.getByRole('button',{name:'检查 DNS'})).toBeDisabled()
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'secret-token'}})
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
 await waitFor(()=>expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled())
 expect(screen.queryByRole('checkbox',{name:/确认.*DNS/})).not.toBeInTheDocument()
 expect(fetch.mock.calls.some(([p])=>p.endsWith('/dns/confirm'))).toBe(false)
 fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'})
 fireEvent.click(screen.getByRole('button',{name:'完成初始化'}));await screen.findByRole('heading',{name:'初始化已完成'})
 const request=fetch.mock.calls.find(([p])=>p.endsWith('/complete'))!
 expect(JSON.parse(request[1].body).cloudflare).toMatchObject({fingerprint:'plan',confirmed:false})
})

it('blocks reuse when Token validation fails and keeps the inputs for retry',async()=>{
 const fetch=installAPI({automatic:true,action:'reuse'}),original=fetch.getMockImplementation()!
 fetch.mockImplementation(async(path:string)=>path.endsWith('/dns/preview')?reply({error:{message:'Token 无法访问对应 Cloudflare Zone'}},422):original(path))
 render(<Setup pollLogin={false}/>);await reachDNS()
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'secret-token'}})
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('Token 无法访问')
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 expect(screen.getByLabelText('Cloudflare API Token')).toHaveValue('secret-token')
 expect(fetch.mock.calls.some(([p])=>p.startsWith('https://'))).toBe(true)
 expect(fetch.mock.calls.some(([p])=>p.endsWith('/dns/confirm'))).toBe(false)
})

it('invalidates checked reuse when the domain, IP or Token changes',async()=>{
 installAPI({automatic:true,action:'reuse'});render(<Setup pollLogin={false}/>);await reachDNS()
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'secret-token'}})
 for(const [label,value] of [['Caddy 访问 IP','192.168.2.8'],['首个通配符域名','new.example.com'],['Cloudflare API Token','new-token']]){
  fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
  await waitFor(()=>expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled())
  fireEvent.change(screen.getByLabelText(label),{target:{value}})
  expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
  expect(screen.queryByRole('table',{name:'DNS 查询结果'})).not.toBeInTheDocument()
  if(label==='Caddy 访问 IP')fireEvent.change(screen.getByLabelText(label),{target:{value:'192.168.2.6'}})
 }
})

it('retries an initial connection failure without claiming setup is restarting',async()=>{
 const fetch=vi.fn().mockRejectedValueOnce(new Error('offline')).mockRejectedValueOnce(new Error('offline')).mockResolvedValue(reply({initialized:false,test_tls:true,client_ip:'192.168.2.9'}))
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>)
 expect(await screen.findByRole('heading',{name:'无法连接初始化服务'})).toBeInTheDocument()
 expect(screen.queryByText('服务正在重启')).not.toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'重试连接'}))
 expect(await screen.findByLabelText('管理员密码')).toBeInTheDocument()
})

it('explains invalid domain and rejects an oversized username before continuing',async()=>{
 installAPI();render(<Setup pollLogin={false}/>)
 fireEvent.change(await screen.findByLabelText('管理员密码'),{target:{value:'a-secure-password'}})
 fireEvent.change(screen.getByLabelText('管理员用户名'),{target:{value:'用'.repeat(22)}})
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 expect(screen.getByText(/用户名必须为 1–64 字节/)).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('管理员用户名'),{target:{value:'admin'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 fireEvent.change(screen.getByLabelText('首个通配符域名'),{target:{value:'https://h.cmx.ee'}})
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 expect(screen.getByText(/域名不能包含协议、端口或路径/)).toBeInTheDocument()
})

it('switches DoH without clearing the configured DNS or changing server resolvers',async()=>{
 const fetch=installAPI({automatic:true});render(<Setup pollLogin={false}/>);await reachDNS()
 expect(screen.getByText(/请填写或确认使用 Cloudflare Token/)).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'secret-token'}})
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
 fireEvent.click(await screen.findByRole('checkbox',{name:/确认.*DNS/}))
 fireEvent.click(screen.getByRole('button',{name:'确认配置'}))
 await screen.findByText(/DNS 配置已确认/)
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
 await waitFor(()=>expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled())
 fireEvent.click(screen.getByText('查询选项'))
 fireEvent.click(screen.getByRole('button',{name:'Google DoH'}))
 expect(screen.getByText(/DNS 配置已确认/)).toBeInTheDocument()
 expect(screen.getByRole('checkbox',{name:/确认.*DNS/})).toBeChecked()
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
 await waitFor(()=>expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled())
 expect(fetch.mock.calls.filter(([p])=>p.endsWith('/dns/confirm'))).toHaveLength(1)
 expect(fetch.mock.calls.some(([p])=>p.endsWith('/dns/check'))).toBe(false)
 expect(screen.getByLabelText('DNS 解析器（逗号分隔）')).toHaveValue('1.1.1.1')
 for(const [path,options] of fetch.mock.calls.filter(([p])=>p.startsWith('https://'))){expect(path).not.toContain('secret-token');expect(options?.body).toBeUndefined()}
})

it('keeps query options folded while the default DNS check remains available',async()=>{
 installAPI();render(<Setup pollLogin={false}/>);await reachDNS()
 const options=screen.getByText('查询选项').closest('details')!
 expect(options).not.toHaveAttribute('open')
 expect(within(options).getByRole('button',{name:'Google DoH'})).not.toBeVisible()
 expect(screen.getByRole('button',{name:'检查 DNS'})).toBeEnabled()
 fireEvent.click(screen.getByText('查询选项'))
 expect(within(options).getByRole('button',{name:'Cloudflare DoH'})).toHaveAttribute('aria-pressed','true')
 fireEvent.click(within(options).getByRole('button',{name:'Google DoH'}))
 expect(screen.getByLabelText('首个通配符域名')).toHaveValue('h.cmx.ee')
 expect(screen.getByLabelText('Caddy 访问 IP')).toHaveValue('192.168.2.6')
})

it('submits without a duplicate browser preflight and requires fresh consent after a warning changes',async()=>{
 let warning='old-warning',attempt=0
 const fetch=installAPI({warning:()=>warning,complete:()=>++attempt===1?reply({error:{code:'setup_warning_confirmation_required',message:'预检警告已变化'}},409):reply({admin_origin:'https://caddyadmin.h.cmx.ee'},202)})
 render(<Setup pollLogin={false}/>);await reachDNS()
 fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'})
 fireEvent.click(screen.getByRole('checkbox',{name:/理解上述警告/}))
 warning='new-warning'
 fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 expect(await screen.findByText('new-warning')).toBeInTheDocument()
 expect(screen.getByRole('checkbox',{name:/理解上述警告/})).not.toBeChecked()
 expect(screen.getByRole('button',{name:'完成初始化'})).toBeDisabled()
 const requests=fetch.mock.calls.filter(([p])=>p.endsWith('/complete'))
 expect(JSON.parse(requests[0][1].body)).toMatchObject({warning_fingerprint:'old-warning',acknowledge_warnings:true})
 expect(fetch.mock.calls.filter(([p])=>p.endsWith('/preflight'))).toHaveLength(2)
 fireEvent.click(screen.getByRole('checkbox',{name:/理解上述警告/}))
 fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 expect(await screen.findByRole('heading',{name:'初始化已完成'})).toBeInTheDocument()
 expect(fetch.mock.calls.filter(([p])=>p.endsWith('/preflight'))).toHaveLength(2)
})

it('keeps an initialized handoff on screen for the user to open the formal entry',async()=>{
 const assign=vi.fn();vi.stubGlobal('location',{protocol:'https:',hostname:'192.168.2.6',origin:'https://192.168.2.6',assign})
 vi.stubGlobal('fetch',vi.fn().mockRejectedValueOnce(new Error('setup closed')).mockResolvedValue(reply({initialized:true,mode:'embedded',admin_origin:'https://caddyadmin.h.cmx.ee',manager_status:'ready',dns_status:'pending',console_status:'ready',temporary_entry:true})))
 render(<Setup pollLogin={false}/>)
 expect(await screen.findByRole('heading',{name:'初始化已完成'})).toBeInTheDocument()
 expect(screen.getByText(/服务端探测通过不代表当前设备可达/)).toBeInTheDocument()
 expect(screen.getByRole('link',{name:'打开正式控制台'})).toHaveAttribute('href','https://caddyadmin.h.cmx.ee/')
 expect(assign).not.toHaveBeenCalled()
})

it('opens the ready formal console even when server console DNS is pending',async()=>{
 const assign=vi.fn();vi.stubGlobal('location',{protocol:'https:',hostname:'192.168.2.6',origin:'https://192.168.2.6',assign})
 const fetch=vi.fn().mockImplementation(async(path:string)=>reply(path.endsWith('/handoff')?{initialized:true,mode:'embedded',admin_origin:'https://caddyadmin.home.example.com',manager_status:'ready',dns_status:'pending',console_status:'ready',temporary_entry:true}:{initialized:true}))
 vi.stubGlobal('fetch',fetch)
 render(<Setup/>)
 await waitFor(()=>expect(assign).toHaveBeenCalledWith('https://caddyadmin.home.example.com/'))
 expect(fetch.mock.calls.every(([,options])=>!options?.method||options.method==='GET')).toBe(true)
})

it.each(['pending','error'])('keeps the handoff while the formal console is %s',async consoleStatus=>{
 const assign=vi.fn();vi.stubGlobal('location',{protocol:'https:',hostname:'192.168.2.6',origin:'https://192.168.2.6',assign})
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(path:string)=>reply(path.endsWith('/handoff')?{initialized:true,mode:'embedded',admin_origin:'https://caddyadmin.home.example.com',manager_status:'ready',dns_status:'ready',console_status:consoleStatus,temporary_entry:true}:{initialized:true})))
 render(<Setup/>)
 await screen.findByRole('link',{name:'打开正式控制台'})
 expect(assign).not.toHaveBeenCalled()
})

it('clears old consent when returning to confirmation reveals different warnings',async()=>{
 let warning='old-warning'
 installAPI({warning:()=>warning});render(<Setup pollLogin={false}/>);await reachDNS()
 fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'})
 fireEvent.click(screen.getByRole('checkbox',{name:/理解上述警告/}))
 fireEvent.click(screen.getByRole('button',{name:'上一步'}))
 warning='new-warning'
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 await screen.findByText('new-warning')
 expect(screen.getByRole('checkbox',{name:/理解上述警告/})).not.toBeChecked()
 expect(screen.getByRole('button',{name:'完成初始化'})).toBeDisabled()
})

it('keeps an unknown completion pending when handoff cannot read initialization state',async()=>{
 const fetch=installAPI({complete:()=>{throw new TypeError('response lost')}})
 const original=fetch.getMockImplementation()!
 fetch.mockImplementation(async(path:string)=>path.endsWith('/handoff')?reply({initialized:false,mode:'embedded',admin_origin:'',manager_status:'error',dns_status:'pending',console_status:'pending',temporary_entry:true}):original(path))
 render(<Setup/>);await reachDNS()
 fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'})
 fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 await screen.findByRole('heading',{name:'正在核对初始化结果'})
 await waitFor(()=>expect(fetch.mock.calls.some(([path])=>path.endsWith('/handoff'))).toBe(true))
 expect(screen.queryByRole('button',{name:'完成初始化'})).not.toBeInTheDocument()
 expect(screen.queryByText(/初始化尚未完成/)).not.toBeInTheDocument()
 expect(fetch.mock.calls.filter(([path])=>path.endsWith('/complete'))).toHaveLength(1)
})

it('restores preserved input only when handoff confirms setup has not completed',async()=>{
 const fetch=installAPI({complete:()=>{throw new TypeError('response lost')}})
 const original=fetch.getMockImplementation()!
 fetch.mockImplementation(async(path:string)=>path.endsWith('/handoff')?reply({initialized:false,mode:'embedded',admin_origin:'',manager_status:'ready',dns_status:'pending',console_status:'pending',temporary_entry:true}):original(path))
 render(<Setup/>);await reachDNS()
 fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'})
 fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('初始化尚未完成，已保留输入')
 expect(screen.getByRole('button',{name:'完成初始化'})).toBeEnabled()
 fireEvent.click(screen.getByRole('button',{name:'上一步'}))
 expect(screen.getByLabelText('Caddy 访问 IP')).toHaveValue('192.168.2.6')
 expect(fetch.mock.calls.filter(([path])=>path.endsWith('/complete'))).toHaveLength(1)
})

it('requires explicit consent for warnings on reused DNS without writing records',async()=>{
 const fetch=installAPI({automatic:true,action:'reuse'}),original=fetch.getMockImplementation()!
 fetch.mockImplementation(async(path:string)=>path.endsWith('/dns/preview')?reply({name:'*.h.cmx.ee',type:'A',address:'192.168.2.6',action:'reuse',fingerprint:'plan',warning:'另一个地址类型需要核对'}):original(path))
 render(<Setup pollLogin={false}/>);await reachDNS()
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'secret-token'}})
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
 await screen.findByText('另一个地址类型需要核对')
 await screen.findByRole('table',{name:'DNS 查询结果'})
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 fireEvent.click(screen.getByLabelText('我已理解现有 DNS 记录警告，并确认该地址可达。'))
 await waitFor(()=>expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled())
 expect(fetch.mock.calls.some(([p])=>p.endsWith('/dns/confirm'))).toBe(false)
 fireEvent.change(screen.getByLabelText('Caddy 访问 IP'),{target:{value:'192.168.2.8'}})
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 expect(screen.queryByLabelText('我已理解现有 DNS 记录警告，并确认该地址可达。')).not.toBeInTheDocument()
})

it('queries DoH before reading the Cloudflare plan',async()=>{
 const fetch=installAPI({automatic:true,action:'reuse'});render(<Setup pollLogin={false}/>);await reachDNS()
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'secret-token'}})
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}));await waitFor(()=>expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled())
 const doh=fetch.mock.calls.findIndex(([p])=>p.startsWith('https://'))
 const cf=fetch.mock.calls.findIndex(([p])=>p.endsWith('/dns/preview'))
 expect(doh).toBeLessThan(cf)
 expect(fetch.mock.calls.some(([p])=>p.endsWith('/dns/confirm'))).toBe(false)
})
