import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Settings} from './pages/Settings'

afterEach(()=>{cleanup();vi.unstubAllGlobals()})

it('shows bootstrap certificate state and submits the Cloudflare token once',async()=>{
 const fetch=vi.fn()
 fetch.mockResolvedValueOnce(new Response(JSON.stringify({config:{origin:'https://admin.home.example.com',domains:[{id:'home',name:'home.example.com',access:'trusted'}],console_lan_only:false,admin_domain:'admin.home.example.com',lan_cidrs:['10.0.0.0/8'],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:['9.9.9.9'],test_tls:false},manager_version:'test',caddy_version:'test',cloudflare_module:true,token_configured:false,certificate_status:{mode:'bootstrap_internal',activation_status:'idle',public_status:'unknown',last_error_class:'',updated_at:'now'}}),{status:200}))
 fetch.mockResolvedValueOnce(new Response(JSON.stringify({certificate_status:{mode:'cloudflare',activation_status:'applying',public_status:'pending',last_error_class:'',updated_at:'now'},restart_required:true}),{status:202}))
 fetch.mockResolvedValueOnce(new Response(JSON.stringify({config:{origin:'https://admin.home.example.com',domains:[{id:'home',name:'home.example.com',access:'trusted'}],console_lan_only:false,admin_domain:'admin.home.example.com',lan_cidrs:['10.0.0.0/8'],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:['9.9.9.9'],test_tls:false},manager_version:'test',caddy_version:'test',cloudflare_module:true,token_configured:true,certificate_status:{mode:'cloudflare',activation_status:'applying',public_status:'pending',last_error_class:'',updated_at:'now'}}),{status:200}))
 vi.stubGlobal('fetch',fetch)
 const qc=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<QueryClientProvider client={qc}><Settings/></QueryClientProvider>)
 expect(await screen.findByText(/内部引导证书/)).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'secret-token'}})
 fireEvent.click(screen.getByRole('button',{name:'保存并启用'}))
 await waitFor(()=>expect(fetch).toHaveBeenCalledTimes(3))
 expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({token:'secret-token',enable:true})
 expect(await screen.findByText(/服务正在安全重启/)).toBeInTheDocument()
 expect(await screen.findByText(/临时证书保持管理入口可访问/)).toBeInTheDocument()
})

it('shows pending domain access without forcing console source restrictions',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({config:{origin:'https://caddyadmin.home.example.com',domains:[{id:'home',name:'home.example.com',access:null}],console_lan_only:false,admin_domain:'caddyadmin.home.example.com',lan_cidrs:['10.0.0.0/8'],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:[],test_tls:false},manager_version:'test',caddy_version:'test',cloudflare_module:true,token_configured:false,certificate_status:{mode:'bootstrap_internal',activation_status:'idle',public_status:'unknown',last_error_class:'',updated_at:'now'}}),{status:200})))
 const qc=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<QueryClientProvider client={qc}><Settings/></QueryClientProvider>)
 expect(await screen.findByLabelText('仅允许 LAN/VPN 访问控制台')).not.toBeChecked();expect(screen.getByRole('option',{name:'待配置'})).toBeInTheDocument()
 expect(screen.queryByText('*.')).not.toBeInTheDocument()
})

it('requires explicit exposure confirmation and keeps settings input after 503',async()=>{
 const config={origin:'https://caddyadmin.example.com',admin_domain:'caddyadmin.example.com',domains:[{id:'first',name:'example.com',access:null}],console_lan_only:false,lan_cidrs:[],upstream_cidrs:[],allowed_names:[],denied_ips:[],resolvers:['1.1.1.1'],test_tls:true}
 const fetch=vi.fn().mockImplementation(async(_path:string,options?:RequestInit)=>options?.method==='PUT'?new Response(JSON.stringify({error:{message:'暂不可用'}}),{status:503}):new Response(JSON.stringify({revision:3,config,active_config:config,certificate_status:{mode:'bootstrap_internal',activation_status:'idle',public_status:'unknown'}})))
 vi.stubGlobal('fetch',fetch);render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})}><Settings/></QueryClientProvider>)
 fireEvent.change(await screen.findByLabelText('访问范围 first'),{target:{value:'internet'}})
 expect(screen.getByRole('button',{name:'保存设置草稿'})).toBeDisabled()
 fireEvent.click(screen.getByLabelText('我已核对影响，确认放开业务或控制台访问范围。'))
 fireEvent.click(screen.getByRole('button',{name:'保存设置草稿'}))
 await screen.findByText('暂不可用');expect(screen.getByLabelText('访问范围 first')).toHaveValue('internet')
 expect(JSON.parse(fetch.mock.calls.find(c=>c[1]?.method==='PUT')![1]!.body as string)).toMatchObject({revision:3,confirm_exposure:true})
})

it('previews a registered domain DNS without writing and requires explicit confirmation to apply',async()=>{
 const config={origin:'https://caddyadmin.example.com',admin_domain:'caddyadmin.example.com',domains:[{id:'first',name:'example.com',access:null}],console_lan_only:false,lan_cidrs:[],upstream_cidrs:[],allowed_names:[],denied_ips:[],resolvers:['1.1.1.1'],test_tls:false}
 const fetch=vi.fn().mockImplementation(async(path:string)=>path.endsWith('/dns/preview')?new Response(JSON.stringify({name:'*.example.com',type:'A',address:'8.8.8.8',action:'create',fingerprint:'plan'})):path.endsWith('/dns/confirm')?new Response(JSON.stringify({})):new Response(JSON.stringify({revision:3,config,active_config:config,external_caddy:false,token_configured:true,certificate_status:{mode:'cloudflare',activation_status:'idle',public_status:'ready'}})))
 vi.stubGlobal('fetch',fetch);render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})}><Settings/></QueryClientProvider>)
 fireEvent.change(await screen.findByLabelText('域名 DNS 目标 IP'),{target:{value:'8.8.8.8'}})
 fireEvent.click(screen.getByRole('button',{name:'预览域名 DNS'}))
 await screen.findByText(/新建：A/)
 expect(fetch.mock.calls.some(c=>c[0].endsWith('/dns/confirm'))).toBe(false)
 expect(screen.getByRole('button',{name:'确认域名 DNS 变更'})).toBeDisabled()
 fireEvent.click(screen.getByLabelText('我确认上述域名 DNS 记录及变更影响。'))
 fireEvent.click(screen.getByRole('button',{name:'确认域名 DNS 变更'}))
 await screen.findByText('域名 DNS 配置已确认，请检查解析后预览和发布域名证书配置。')
 expect(JSON.parse(fetch.mock.calls.find(c=>c[0].endsWith('/dns/confirm'))![1]!.body as string)).toEqual({domain_id:'first',address:'8.8.8.8',fingerprint:'plan',confirm:true})
})
it('keeps separator input while entering multiple upstream ranges',async()=>{
 const config={origin:'https://caddyadmin.example.com',admin_domain:'caddyadmin.example.com',domains:[],console_lan_only:false,lan_cidrs:[],upstream_cidrs:[],allowed_names:[],denied_ips:[],resolvers:['1.1.1.1'],test_tls:true}
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({revision:3,config,active_config:config,certificate_status:{mode:'bootstrap_internal',activation_status:'idle',public_status:'unknown'}}))))
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Settings/></QueryClientProvider>)
 const input=await screen.findByLabelText('允许上游网段')
 fireEvent.change(input,{target:{value:'10.0.0.0/8,'}})
 expect(input).toHaveValue('10.0.0.0/8,')
 fireEvent.change(input,{target:{value:'10.0.0.0/8,192.168.1.0/24'}})
 expect(input).toHaveValue('10.0.0.0/8,192.168.1.0/24')
})
it('compares repeated draft edits with active policy when exposure is still unpublished',async()=>{
 const base={origin:'https://caddyadmin.example.com',admin_domain:'caddyadmin.example.com',domains:[{id:'first',name:'example.com',access:'trusted'}],console_lan_only:false,lan_cidrs:['10.0.0.0/8'],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:['1.1.1.1'],test_tls:true}
 const config={...base,domains:[{id:'first',name:'example.com',access:'internet'}]}
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({revision:4,config,active_config:base,certificate_status:{mode:'bootstrap_internal',activation_status:'idle',public_status:'unknown'}}))))
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><Settings/></QueryClientProvider>)
 fireEvent.change(await screen.findByLabelText('许可服务名'),{target:{value:'photos.internal'}})
 expect(screen.getByRole('button',{name:'保存设置草稿'})).toBeDisabled()
 expect(screen.getByLabelText('我已核对影响，确认放开业务或控制台访问范围。')).not.toBeChecked()
})
