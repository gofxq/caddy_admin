import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {ConfigurationTransfer} from './components/ConfigurationTransfer'
import {Setup} from './pages/Setup'
const config={format:'caddy-web-admin',version:2,settings:{origin:'https://caddyadmin.home.example.com',domains:[{id:'home',name:'home.example.com',access:null}],console_lan_only:false,admin_domain:'caddyadmin.home.example.com',lan_cidrs:['10.0.0.0/8'],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:['10.0.0.53']},services:[{name:'照片库',domain_id:'home',hostname:'photos.home.example.com',scheme:'http',host:'10.0.0.8',port:2283,enabled:true,notes:''}]}
const response=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status})
const client=()=>new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks()})
async function upload(value:unknown=config){fireEvent.change(screen.getByLabelText('上传配置文件'),{target:{files:[new File([JSON.stringify(value)],'config.json',{type:'application/json'})]}})}
it('previews a replacement, requires confirmation and preserves input after a conflict',async()=>{
 const fetch=vi.fn().mockResolvedValueOnce(response({revision:7,services:config.services,changes:[{kind:'added',hostname:config.services[0].hostname}]})).mockResolvedValueOnce(response({error:{code:'conflict',message:'草稿已被修改，请重新预览导入'}},409)).mockResolvedValueOnce(response({revision:8,services:config.services,changes:[]})).mockResolvedValueOnce(response({revision:9}))
 vi.stubGlobal('fetch',fetch);render(<QueryClientProvider client={client()}><ConfigurationTransfer/></QueryClientProvider>)
 await upload();await screen.findByText('config.json：1 个服务');expect(fetch).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'预览导入'}));await screen.findByText('新增：photos.home.example.com')
 expect(screen.getByRole('button',{name:'确认导入草稿'})).toBeDisabled();fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByRole('button',{name:'确认导入草稿'}));await screen.findByText('草稿已被修改，请重新预览导入')
 expect(screen.getByText('config.json：1 个服务')).toBeInTheDocument();expect(screen.queryByRole('button',{name:'确认导入草稿'})).not.toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'预览导入'}));await screen.findByText('服务内容无变化。');fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByRole('button',{name:'确认导入草稿'}));await screen.findByText(/服务草稿已替换/)
 expect(JSON.parse(fetch.mock.calls[3][1].body)).toEqual({configuration:config,revision:8,confirm:true});expect(fetch.mock.calls.map(c=>c[0])).not.toContain('/api/v1/deployments')
})
it('exports the portable file and rejects invalid uploads without requesting the API',async()=>{
 const fetch=vi.fn().mockResolvedValue(response(config));vi.stubGlobal('fetch',fetch)
 const createObjectURL=vi.fn().mockReturnValue('blob:config'),revokeObjectURL=vi.fn();vi.stubGlobal('URL',class extends URL {static createObjectURL=createObjectURL;static revokeObjectURL=revokeObjectURL})
 vi.spyOn(Date.prototype,'toISOString').mockReturnValue('2026-10-02T12:34:56.789Z')
 let filename=''
 const click=vi.spyOn(HTMLAnchorElement.prototype,'click').mockImplementation(function(this:HTMLAnchorElement){filename=this.download})
 render(<QueryClientProvider client={client()}><ConfigurationTransfer/></QueryClientProvider>)
 await upload({...config,password:'secret'});await screen.findByRole('alert');expect(fetch).not.toHaveBeenCalled();expect(screen.queryByRole('button',{name:'预览导入'})).not.toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'导出配置'}));await screen.findByText('配置已导出。');expect(fetch.mock.calls[0][0]).toBe('/api/v1/configuration/export');expect(createObjectURL).toHaveBeenCalledOnce();expect(click).toHaveBeenCalledOnce();expect(filename).toBe('caddy-admin-config-2026-10-02T12-34-56-789Z.json');expect(revokeObjectURL).toHaveBeenCalledWith('blob:config')
})
it('initializes with imported policy and drafts while requiring a new password and explicit confirmation',async()=>{
 const fetch=vi.fn().mockImplementation(async(path:string)=>path.endsWith('/status')?response({initialized:false,test_tls:true,external_caddy:false,resolver_suggestions:[]}):path.endsWith('/preflight')?response({normalized:{admin_domain:config.settings.admin_domain},network_valid:true,checks:[],can_complete:true,requires_acknowledgement:false}):response({status:'restarting',admin_origin:config.settings.origin},202))
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>);await screen.findByLabelText('管理员密码');await upload();await screen.findByRole('button',{name:'采用导入配置'});fireEvent.click(screen.getByRole('button',{name:'采用导入配置'}))
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled();fireEvent.change(screen.getByLabelText('管理员密码'),{target:{value:'new-admin-password'}});fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(screen.getByRole('heading',{name:'首个域名与 DNS'})).toBeInTheDocument()
 expect(screen.getByRole('region',{name:'配置摘要'})).toHaveTextContent('home.example.com')
 expect(screen.getByRole('region',{name:'配置摘要'})).toHaveTextContent('10.0.0.53')
 expect(screen.getByText(/照片库 · photos/)).toBeInTheDocument()
 await screen.findByLabelText('DNS 解析器（逗号分隔）');expect(screen.getByLabelText('DNS 解析器（逗号分隔）')).toHaveValue('10.0.0.53');fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'})
 expect(screen.getByText(/照片库 · photos/)).toBeInTheDocument();expect(screen.getByRole('button',{name:'完成初始化'})).toBeDisabled();fireEvent.click(screen.getByRole('checkbox'));fireEvent.click(screen.getByRole('button',{name:'完成初始化'}));await screen.findByText('初始化已完成')
 const call=fetch.mock.calls.find(c=>c[0].endsWith('/complete'));expect(call).toBeDefined();const body=JSON.parse(call![1].body);expect(body.services).toEqual(config.services);expect(body.confirm_import).toBe(true);expect(body.password).toBe('new-admin-password');expect(body.settings.resolvers).toEqual(['10.0.0.53'])
})

it('retains imported policy after a blocked preflight without showing network editors',async()=>{
 const policy={...config,settings:{...config.settings,allowed_names:['photos.internal'],denied_ips:['10.0.0.1']}};let blocked=true
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(path:string)=>path.endsWith('/status')?response({initialized:false,test_tls:true}):response({normalized:{admin_domain:config.settings.admin_domain},checks:blocked?[{id:'target',status:'block',message:'上游策略无效'}]:[],can_complete:!blocked,requires_acknowledgement:false})))
 render(<Setup pollLogin={false}/>);await screen.findByLabelText('管理员密码');await upload(policy);fireEvent.click(await screen.findByRole('button',{name:'采用导入配置'}));fireEvent.change(screen.getByLabelText('管理员密码'),{target:{value:'new-admin-password'}});fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(screen.getByRole('region',{name:'配置摘要'})).toHaveTextContent('photos.internal');expect(screen.queryByLabelText('可信网络 CIDR')).not.toBeInTheDocument();fireEvent.change(screen.getByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'192.168.2.1'}});fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByText('上游策略无效');expect(screen.getByLabelText('DNS 解析器（逗号分隔）')).toHaveValue('192.168.2.1');blocked=false;fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'});fireEvent.click(screen.getByRole('button',{name:'上一步'}));expect(screen.getByLabelText('DNS 解析器（逗号分隔）')).toHaveValue('192.168.2.1')
})

it('requires new credentials and DNS confirmation after importing in embedded mode',async()=>{
 vi.stubGlobal('location',new URL('https://192.168.2.6/setup'))
 const fetch=vi.fn().mockImplementation(async(path:string)=>path.endsWith('/status')?response({initialized:false,test_tls:false,external_caddy:false}):path.endsWith('/dns/check')?response({verified:true,queries:[{name:'caddyadmin.home.example.com.',resolver:'10.0.0.53',addresses:['192.168.2.6'],status:'pass',message:'目标 IP 一致'}]}):path.startsWith('https://')?(()=>{const q=new URL(path),name=q.searchParams.get('name'),type=q.searchParams.get('type')==='AAAA'?28:1;return response({Status:0,Question:[{name,type}],Answer:type===1?[{name,type,data:'192.168.2.6'}]:[]})})():path.endsWith('/dns/preview')?response({name:'*.home.example.com',type:'A',address:'192.168.2.6',action:'create',fingerprint:'import-dns'}):path.endsWith('/preflight')?response({normalized:{admin_domain:config.settings.admin_domain},network_valid:true,checks:[],can_complete:true,requires_acknowledgement:false}):response({status:'restarting',admin_origin:config.settings.origin},202))
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>);await screen.findByLabelText('管理员密码');await upload();fireEvent.click(await screen.findByRole('button',{name:'采用导入配置'}))
 fireEvent.change(screen.getByLabelText('管理员密码'),{target:{value:'new-admin-password'}});fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'首个域名与 DNS'})
 expect(screen.getByLabelText('Cloudflare API Token')).toHaveValue('')
 expect(screen.getByLabelText('Caddy 访问 IP')).toHaveValue('192.168.2.6')
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'}})
 fireEvent.click(screen.getByRole('button',{name:'预览 DNS 变更'}));fireEvent.click(await screen.findByRole('checkbox',{name:/确认.*DNS/}));fireEvent.click(screen.getByRole('button',{name:'确认配置'}));await screen.findByText(/DNS 配置已确认/);fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}));await screen.findByText('DNS 已配置，控制台域名与通配符一层子域均解析到目标 IP。')
 fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'})
 expect(screen.getByRole('button',{name:'完成初始化'})).toBeDisabled()
 fireEvent.click(screen.getByRole('checkbox',{name:/确认导入配置/}));fireEvent.click(screen.getByRole('button',{name:'完成初始化'}));await screen.findByText('初始化已完成')
 const body=JSON.parse(fetch.mock.calls.find(([path])=>path.endsWith('/complete'))![1].body)
 expect(body.cloudflare).toMatchObject({token:'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',address:'192.168.2.6',fingerprint:'import-dns',confirmed:true})
 expect(body.services).toEqual(config.services)
})

it('requires missing imported resolvers to be filled and confirms an empty draft list in external mode',async()=>{
 const fetch=vi.fn().mockImplementation(async(path:string)=>path.endsWith('/status')?response({initialized:false,test_tls:false,external_caddy:true}):path.endsWith('/preflight')?response({normalized:{admin_domain:config.settings.admin_domain},network_valid:true,checks:[],can_complete:true,requires_acknowledgement:false}):response({status:'restarting',admin_origin:config.settings.origin},202))
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>);await screen.findByLabelText('管理员密码')
 await upload({...config,settings:{...config.settings,resolvers:[]},services:[]});fireEvent.click(await screen.findByRole('button',{name:'采用导入配置'}))
 fireEvent.change(screen.getByLabelText('管理员密码'),{target:{value:'new-admin-password'}});fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(screen.getByRole('region',{name:'配置摘要'})).toHaveTextContent('待填写')
 expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()
 expect(screen.getByRole('button',{name:'取消导入'})).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('DNS 解析器（逗号分隔）'),{target:{value:'192.168.2.1'}})
 expect(screen.queryByLabelText('Cloudflare API Token')).not.toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'})
 expect(screen.getByRole('button',{name:'完成初始化'})).toBeDisabled()
 fireEvent.click(screen.getByRole('checkbox',{name:/确认导入配置/}));fireEvent.click(screen.getByRole('button',{name:'完成初始化'}));await screen.findByText('初始化已完成')
 const body=JSON.parse(fetch.mock.calls.find(([path])=>path.endsWith('/complete'))![1].body)
 expect(body.services).toEqual([]);expect(body.confirm_import).toBe(true)
 expect(body.settings.resolvers).toEqual(['192.168.2.1']);expect(body).not.toHaveProperty('cloudflare')
})
it('does not silently adopt an unsupported legacy setup configuration',async()=>{vi.stubGlobal('fetch',vi.fn().mockResolvedValue(response({initialized:false})));render(<Setup pollLogin={false}/>);await screen.findByLabelText('管理员密码');await upload({...config,settings:{...config.settings,admin_domain:'custom.home.example.com'}});fireEvent.click(await screen.findByRole('button',{name:'采用导入配置'}));expect(await screen.findByRole('alert')).toHaveTextContent(/暂不支持通过初始化导入/);expect(screen.queryByText(/已采用配置/)).not.toBeInTheDocument()})

it('clears an approved preview when a different invalid file is selected',async()=>{
 const fetch=vi.fn().mockResolvedValue(response({revision:7,services:config.services,changes:[]}));vi.stubGlobal('fetch',fetch)
 render(<QueryClientProvider client={client()}><ConfigurationTransfer/></QueryClientProvider>);await upload();await screen.findByText('config.json：1 个服务');fireEvent.click(screen.getByRole('button',{name:'预览导入'}));await screen.findByText('服务内容无变化。');fireEvent.click(screen.getByRole('checkbox'));expect(screen.getByRole('button',{name:'确认导入草稿'})).toBeEnabled()
 await upload({...config,token:'secret'});await screen.findByRole('alert');expect(screen.queryByRole('button',{name:'确认导入草稿'})).not.toBeInTheDocument();expect(screen.queryByRole('button',{name:'预览导入'})).not.toBeInTheDocument();expect(fetch).toHaveBeenCalledTimes(1)
})
it.each(['input','dropdown'])('keeps imported domain IDs and names when choosing the second console domain using %s',async method=>{
 const second={...config,settings:{...config.settings,domains:[{id:'home',name:'home.example.com',access:null},{id:'second',name:'second.example.com',access:'internet'}]}}
 const fetch=vi.fn().mockImplementation(async(path:string)=>path.endsWith('/status')?response({initialized:false,test_tls:true}):path.endsWith('/preflight')?response({normalized:{admin_domain:'caddyadmin.second.example.com'},checks:[],can_complete:true,requires_acknowledgement:false}):response({admin_origin:'https://caddyadmin.second.example.com'}))
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>);await screen.findByLabelText('管理员密码');await upload(second);fireEvent.click(await screen.findByRole('button',{name:'采用导入配置'}));fireEvent.change(screen.getByLabelText('管理员密码'),{target:{value:'new-admin-password'}});fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 // Selecting an already imported domain must never rename the first domain.
 fireEvent.change(screen.getByLabelText(method==='input'?'首个通配符域名':'首个控制台域名'),{target:{value:'second.example.com'}})
 fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'});fireEvent.click(screen.getByLabelText('我已确认导入配置，服务仅保存为草稿。'));fireEvent.click(screen.getByRole('button',{name:'完成初始化'}));await screen.findByRole('heading',{name:'初始化已完成'})
 for(const endpoint of ['/preflight','/complete']){const body=JSON.parse(fetch.mock.calls.find(c=>c[0].endsWith(endpoint))![1].body);expect(body.settings.domain).toBe('second.example.com');expect(body.import_settings.domains).toEqual(second.settings.domains);expect(body.import_settings.admin_domain).toBe('caddyadmin.second.example.com');expect(body.import_settings.origin).toBe('https://caddyadmin.second.example.com')}
})
