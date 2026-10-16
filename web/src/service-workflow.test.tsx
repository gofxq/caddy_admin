import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {createMemoryHistory,createRootRoute,createRoute,createRouter,Outlet,RouterProvider} from '@tanstack/react-router'
import {Services} from './pages/Services'
import {Settings} from './pages/Settings'
import {Overview} from './pages/Overview'
import type {Service,ServiceList,Settings as SettingsData} from './model'

const service:Service={id:'photos',name:'照片库',domain_id:'home',hostname:'photos.example.com',scheme:'http',host:'10.0.0.8',port:2283,enabled:true,notes:'',dial:'10.0.0.8:2283',updated_at:'now'}
const settings:SettingsData={revision:3,config:{origin:'https://caddyadmin.example.com',admin_domain:'caddyadmin.example.com',domains:[{id:'home',name:'example.com',access:'trusted'}],console_lan_only:false,lan_cidrs:['10.0.0.0/8'],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:['1.1.1.1'],test_tls:true},active_config:null!,manager_version:'test',caddy_version:'test',dns_provider:'cloudflare',dns_module:true,token_configured:false,certificate_status:{mode:'bootstrap_internal',activation_status:'idle',public_status:'unknown',last_error_class:'',updated_at:''},external_caddy:false}
settings.active_config=settings.config
const response=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status})
const clients:QueryClient[]=[]
function view(component:()=>React.JSX.Element,path='/services'){
 vi.stubGlobal('scrollTo',vi.fn())
 const root=createRootRoute({component:Outlet})
 const page=createRoute({getParentRoute:()=>root,path,component})
 const other=createRoute({getParentRoute:()=>root,path:'/certificates',component:()=> <p>其他页面</p>})
 const router=createRouter({routeTree:root.addChildren([page,other]),history:createMemoryHistory({initialEntries:[path]})})
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}});clients.push(client)
 render(<QueryClientProvider client={client}><RouterProvider router={router}/></QueryClientProvider>)
 return {router,client}
}
function server(list:ServiceList={revision:3,services:[],published:[]},config=settings){
 const fetch=vi.fn().mockImplementation(async(path:string,options?:RequestInit)=>{
  if(options?.method&&options.method!=='GET')return response({})
  if(path==='/api/v1/services')return response(list)
  if(path==='/api/v1/settings')return response(config)
  return response({items:[],recent:[],enabled:0,version:0,unpublished:false,reachable:true,checked_at:'',message:'已连接'})
 });vi.stubGlobal('fetch',fetch);return fetch
}
async function open(){await waitFor(()=>expect(screen.getByRole('button',{name:'新建服务'})).toBeEnabled());fireEvent.click(screen.getByRole('button',{name:'新建服务'}));return screen.findByRole('dialog')}
function fill(){fireEvent.change(screen.getByLabelText('服务名称'),{target:{value:'新相册'}});fireEvent.change(screen.getByLabelText('子域名'),{target:{value:'gallery'}});fireEvent.change(screen.getByLabelText('上游主机'),{target:{value:'10.0.0.9'}})}
function writes(fetch:ReturnType<typeof server>){return fetch.mock.calls.filter(([,options])=>options?.method&&options.method!=='GET')}
afterEach(()=>{cleanup();clients.splice(0).forEach(c=>c.clear());vi.restoreAllMocks();vi.unstubAllGlobals()})

it('builds a full hostname from a prefix and fixed single-domain suffix',async()=>{
 const fetch=server();view(Services);await open();fill()
 expect(screen.queryByLabelText('完整域名')).not.toBeInTheDocument()
 expect(screen.queryByRole('combobox',{name:'所属域名'})).not.toBeInTheDocument()
 expect(screen.getByLabelText('访问地址')).toHaveTextContent('https://gallery.example.com')
 fireEvent.click(screen.getByRole('button',{name:'保存草稿'}))
 await waitFor(()=>expect(writes(fetch)).toHaveLength(1))
 expect(JSON.parse(writes(fetch)[0][1]!.body as string)).toMatchObject({revision:3,service:{domain_id:'home',hostname:'gallery.example.com'}})
 expect(await screen.findByRole('status')).toHaveTextContent('服务草稿已保存，尚未上线')
 expect(within(screen.getByRole('status')).getByRole('link',{name:'预览与发布'})).toHaveAttribute('href','/deployments')
 fireEvent.click(screen.getByRole('button',{name:'继续添加'}));expect(await screen.findByLabelText('子域名')).toHaveValue('')
})
it('keeps the prefix when the selected domain changes, including editing an existing service',async()=>{
 const fetch=server({revision:3,services:[service],published:[service]},{...settings,config:{...settings.config,domains:[...settings.config.domains,{id:'second',name:'other.example',access:'internet'}]}})
 view(Services);fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}))
 expect(screen.getByLabelText('子域名')).toHaveValue('photos')
 fireEvent.change(screen.getByLabelText('所属域名'),{target:{value:'second'}})
 expect(screen.getByLabelText('子域名')).toHaveValue('photos')
 expect(screen.getByLabelText('访问地址')).toHaveTextContent('https://photos.other.example')
 fireEvent.click(screen.getByRole('button',{name:'保存草稿'}))
 await waitFor(()=>expect(writes(fetch)).toHaveLength(1));expect(JSON.parse(writes(fetch)[0][1]!.body as string).service.hostname).toBe('photos.other.example')
})
it.each(['nested.photos','caddyadmin','bad_name'])('rejects invalid or reserved subdomain %s before submitting',async prefix=>{
 const fetch=server();view(Services);await open();fill();fireEvent.change(screen.getByLabelText('子域名'),{target:{value:prefix}})
 expect(screen.getByLabelText('子域名')).toBeInvalid();fireEvent.click(screen.getByRole('button',{name:'保存草稿'}));expect(writes(fetch)).toHaveLength(0)
})
it('parses a pasted upstream URL, handles IPv6 and preserves custom ports across protocol changes',async()=>{
 const fetch=server();view(Services);await open();fill()
 fireEvent.paste(screen.getByLabelText('上游主机'),{clipboardData:{getData:()=> 'https://[fd00::8]:8443'}})
 expect(screen.getByLabelText('上游主机')).toHaveValue('fd00::8');expect(screen.getByLabelText('端口')).toHaveValue(8443);expect(screen.getByLabelText('上游协议')).toHaveValue('https')
 fireEvent.change(screen.getByLabelText('上游协议'),{target:{value:'http'}});expect(screen.getByLabelText('端口')).toHaveValue(8443)
 fireEvent.click(screen.getByRole('button',{name:'保存草稿'}));await waitFor(()=>expect(writes(fetch)).toHaveLength(1));expect(JSON.parse(writes(fetch)[0][1]!.body as string).service).toMatchObject({host:'fd00::8',port:8443,scheme:'http'})
})
it('only updates untouched default ports and rejects credentials or paths in pasted URLs',async()=>{
 server();view(Services);await open();fill()
 fireEvent.change(screen.getByLabelText('上游协议'),{target:{value:'https'}});expect(screen.getByLabelText('端口')).toHaveValue(443)
 fireEvent.change(screen.getByLabelText('端口'),{target:{value:'80'}})
 fireEvent.change(screen.getByLabelText('上游协议'),{target:{value:'http'}});fireEvent.change(screen.getByLabelText('上游协议'),{target:{value:'https'}});expect(screen.getByLabelText('端口')).toHaveValue(80)
 fireEvent.paste(screen.getByLabelText('上游主机'),{clipboardData:{getData:()=> 'https://user:secret@10.0.0.8/private?q=secret'}})
 expect(screen.getByRole('alert')).toHaveTextContent('不支持凭据、路径、查询参数或片段');expect(screen.getByLabelText('上游主机')).toHaveValue('10.0.0.9');expect(screen.queryByDisplayValue(/secret/)).not.toBeInTheDocument()
})
it('enforces the backend UTF-8 byte limits for Chinese names and notes',async()=>{
 const fetch=server();view(Services);await open();fill()
 const name=screen.getByLabelText('服务名称'),notes=screen.getByLabelText('备注')
 fireEvent.change(name,{target:{value:'中'.repeat(33)+'a'}});expect(name).toBeValid()
 fireEvent.change(name,{target:{value:'中'.repeat(34)}});expect(name).toBeInvalid();fireEvent.click(screen.getByRole('button',{name:'保存草稿'}));expect(writes(fetch)).toHaveLength(0)
 fireEvent.change(name,{target:{value:'照片'}});fireEvent.change(notes,{target:{value:'中'.repeat(667)}});expect(notes).toBeInvalid()
 fireEvent.change(notes,{target:{value:'中'.repeat(666)+'ab'}});expect(notes).toBeValid()
})
it('does not present a failed initial service read as an empty list and can retry',async()=>{
 let failed=true;server().mockImplementation(async(path:string)=>path.endsWith('/services')?(failed?response({error:{message:'服务读取失败'}},503):response({revision:3,services:[],published:[]})):response(settings))
 view(Services);await screen.findByRole('alert');expect(screen.queryByRole('button',{name:'添加第一个服务'})).not.toBeInTheDocument();expect(screen.getByRole('button',{name:'新建服务'})).toBeDisabled()
 failed=false;fireEvent.click(screen.getByRole('button',{name:'重试'}));expect(await screen.findByRole('button',{name:'添加第一个服务'})).toBeEnabled()
})
it('blocks creation while settings are loading or unavailable and recovers on retry',async()=>{
 let failed=true;server().mockImplementation(async(path:string)=>path.endsWith('/settings')?(failed?response({error:{message:'设置读取失败'}},503):response(settings)):response({revision:3,services:[],published:[]}))
 view(Services);await screen.findByText('设置读取失败');expect(screen.getByRole('button',{name:'新建服务'})).toBeDisabled();expect(screen.getByRole('button',{name:'添加第一个服务'})).toBeDisabled()
 failed=false;fireEvent.click(screen.getByRole('button',{name:'重试读取设置'}));await waitFor(()=>expect(screen.getByRole('button',{name:'新建服务'})).toBeEnabled())
})
it('confirms abandoning a dirty service form on close, navigation, and browser unload',async()=>{
 server();const {router}=view(Services);await open();fill();const confirm=vi.spyOn(window,'confirm').mockReturnValue(false)
 fireEvent.click(screen.getByRole('button',{name:'关闭'}));expect(screen.getByLabelText('子域名')).toHaveValue('gallery')
 const unload=new Event('beforeunload',{cancelable:true});window.dispatchEvent(unload);expect(unload.defaultPrevented).toBe(true)
 void router.navigate({to:'/certificates'});await waitFor(()=>expect(window.confirm).toHaveBeenCalled());expect(router.state.location.pathname).toBe('/services');expect(confirm).toHaveBeenCalled()
 confirm.mockReturnValue(true);fireEvent.click(screen.getByRole('button',{name:'取消'}));await waitFor(()=>expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
})
it('retains service input on 503 and recovers a revision conflict only after reviewing latest state',async()=>{
 let revision=3,fail=503
 const fetch=server({revision,services:[service],published:[service]}).mockImplementation(async(path:string,options?:RequestInit)=>{
  if(options?.method==='PUT')return fail?response({error:{code:fail===409?'conflict':'unavailable',message:fail===409?'草稿已变化':'暂不可用'}},fail):response({})
  if(path.endsWith('/services'))return response({revision,services:[{...service,name:revision===3?service.name:'其他人修改'}],published:[service]})
  return response(settings)
 })
 view(Services);fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}));fireEvent.change(screen.getByLabelText('服务名称'),{target:{value:'我的修改'}});fireEvent.click(screen.getByRole('button',{name:'保存草稿'}));await screen.findByText('暂不可用');expect(screen.getByLabelText('服务名称')).toHaveValue('我的修改')
 fail=409;revision=4;fireEvent.click(screen.getByRole('button',{name:'保存草稿'}));await screen.findByText('草稿已变化');fireEvent.click(screen.getByRole('button',{name:'读取最新草稿'}));await screen.findByText(/其他人修改/,{selector:'pre'})
 expect(screen.getByLabelText('服务名称')).toHaveValue('我的修改');expect(writes(fetch)).toHaveLength(2)
 fireEvent.click(screen.getByRole('button',{name:'已核对，保留输入继续编辑'}));fail=0;fireEvent.click(screen.getByRole('button',{name:'保存草稿'}));await waitFor(()=>expect(writes(fetch)).toHaveLength(3));expect(JSON.parse(writes(fetch)[2][1]!.body as string)).toMatchObject({revision:4,service:{name:'我的修改'}})
})
it('reports that an enable or disable operation only changed the draft',async()=>{
 server({revision:3,services:[service],published:[service]});view(Services);fireEvent.click(await screen.findByRole('button',{name:'停用 照片库'}))
 expect(await screen.findByRole('status')).toHaveTextContent('已保存停用草稿，发布后生效')
})
it('keeps settings input through conflict recovery and warns before leaving',async()=>{
 let revision=3,fail=true
 const fetch=server().mockImplementation(async(_path:string,options?:RequestInit)=>options?.method==='PUT'?(fail?response({error:{code:'conflict',message:'设置已变化'}},409):response({})):response({...settings,revision,config:{...settings.config,allowed_names:revision===3?[]:['other.internal']}}))
 const {router}=view(Settings,'/settings');fireEvent.change(await screen.findByLabelText('许可服务名'),{target:{value:'mine.internal'}})
 vi.spyOn(window,'confirm').mockReturnValue(false);void router.navigate({to:'/certificates'});await waitFor(()=>expect(window.confirm).toHaveBeenCalled());expect(router.state.location.pathname).toBe('/settings')
 const unload=new Event('beforeunload',{cancelable:true});window.dispatchEvent(unload);expect(unload.defaultPrevented).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'保存设置草稿'}));await screen.findByText('设置已变化');revision=4;fireEvent.click(screen.getByRole('button',{name:'读取最新设置'}));await screen.findByText(/other.internal/)
 expect(screen.getByLabelText('许可服务名')).toHaveValue('mine.internal');expect(writes(fetch)).toHaveLength(1)
 fireEvent.click(screen.getByRole('button',{name:'已核对，保留输入继续编辑'}));fail=false;fireEvent.click(screen.getByRole('button',{name:'保存设置草稿'}));await waitFor(()=>expect(writes(fetch)).toHaveLength(2));expect(JSON.parse(writes(fetch)[1][1]!.body as string)).toMatchObject({revision:4,settings:{allowed_names:['mine.internal']}})
 await screen.findByText(/设置草稿已保存/);const cleanUnload=new Event('beforeunload',{cancelable:true});window.dispatchEvent(cleanUnload);expect(cleanUnload.defaultPrevented).toBe(false)
})
it('keeps first-run guidance after a settings-only publication and uses actual services for task completion',async()=>{
 server().mockImplementation(async(path:string)=>path.endsWith('/settings')?response(settings):path.endsWith('/services')?response({revision:3,services:[],published:[]}):path.endsWith('/overview')?response({recent:[],version:1,enabled:0,unpublished:true,reachable:true,checked_at:'',message:'已连接'}):response({items:[]}))
 view(Overview,'/');const guide=await screen.findByRole('heading',{name:'首次运行任务中心'});const tasks=within(guide.closest('section')!)
 await waitFor(()=>expect(tasks.getByText('创建服务草稿').closest('li')).not.toHaveClass('done'))
 expect(tasks.getByText('完成首个服务上线').closest('li')).not.toHaveClass('done')
 expect(tasks.getByText('上游许可草稿已配置').closest('li')).toHaveClass('done')
})
it.each([{scheme:'http' as const,port:80,next:'https',want:443},{scheme:'https' as const,port:443,next:'http',want:80},{scheme:'http' as const,port:8080,next:'https',want:8080}])('updates default ports but preserves custom ports when editing $scheme:$port',async({scheme,port,next,want})=>{
 server({revision:3,services:[{...service,scheme,port}],published:[]});view(Services);fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}));fireEvent.change(screen.getByLabelText('上游协议'),{target:{value:next}});expect(screen.getByLabelText('端口')).toHaveValue(want)
})
it('blocks creation until a pending settings request resolves',async()=>{
 let finish!:(value:Response)=>void
 server().mockImplementation(async(path:string)=>path.endsWith('/settings')?new Promise<Response>(resolve=>{finish=resolve}):response({revision:3,services:[],published:[]}))
 view(Services);expect(await screen.findByRole('button',{name:'添加第一个服务'})).toBeDisabled();expect(screen.getByRole('button',{name:'新建服务'})).toBeDisabled()
 finish(response(settings));await waitFor(()=>expect(screen.getByRole('button',{name:'新建服务'})).toBeEnabled())
})
it('does not recreate a service removed by a concurrent edit',async()=>{
 let removed=false
 const fetch=server().mockImplementation(async(path:string,options?:RequestInit)=>options?.method==='PUT'?response({error:{code:'conflict',message:'草稿已变化'}},409):path.endsWith('/services')?response({revision:removed?4:3,services:removed?[]:[service],published:[service]}):response(settings))
 view(Services);fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}));fireEvent.change(screen.getByLabelText('服务名称'),{target:{value:'保留我的输入'}});fireEvent.click(screen.getByRole('button',{name:'保存草稿'}));await screen.findByText('草稿已变化');removed=true
 fireEvent.click(screen.getByRole('button',{name:'读取最新草稿'}));await screen.findByText(/此服务已从最新草稿删除/)
 expect(screen.getByLabelText('服务名称')).toHaveValue('保留我的输入');expect(screen.queryByRole('button',{name:'已核对，保留输入继续编辑'})).not.toBeInTheDocument();expect(writes(fetch)).toHaveLength(1)
})
it('keeps the original domain while reviewing a concurrent domain rename',async()=>{
 let renamed=false
 server().mockImplementation(async(path:string,options?:RequestInit)=>options?.method==='PUT'?response({error:{code:'conflict',message:'草稿已变化'}},409):path.endsWith('/services')?response({revision:renamed?4:3,services:[service],published:[service]}):response({...settings,revision:renamed?4:3,config:{...settings.config,domains:[{id:'home',name:renamed?'renamed.example':'example.com',access:'trusted'}]}}))
 view(Services);fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}));fireEvent.change(screen.getByLabelText('子域名'),{target:{value:'gallery'}});fireEvent.click(screen.getByRole('button',{name:'保存草稿'}));await screen.findByText('草稿已变化');renamed=true;fireEvent.click(screen.getByRole('button',{name:'读取最新草稿'}));await screen.findByText(/最新草稿 r4/)
 expect(screen.getByLabelText('子域名')).toHaveValue('gallery');expect(screen.getByLabelText('访问地址')).toHaveTextContent('https://gallery.example.com')
 fireEvent.click(screen.getByRole('button',{name:'已核对，保留输入继续编辑'}));expect(screen.getByLabelText('子域名')).toHaveValue('gallery');expect(screen.getByLabelText('访问地址')).toHaveTextContent('https://gallery.renamed.example')
})
it.each(['https://10.0.0.8:443','https://[fd00::8]:443'])('preserves an explicitly pasted default port from %s',async address=>{
 server();view(Services);await open();fireEvent.paste(screen.getByLabelText('上游主机'),{clipboardData:{getData:()=>address}});fireEvent.change(screen.getByLabelText('上游协议'),{target:{value:'http'}});expect(screen.getByLabelText('端口')).toHaveValue(443)
})
