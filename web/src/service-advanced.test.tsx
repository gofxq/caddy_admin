import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {createMemoryHistory,createRootRoute,createRoute,createRouter,Outlet,RouterProvider} from '@tanstack/react-router'
import {Services} from './pages/Services'
import {parseConfiguration} from './configuration'
import {configurationFixture} from './configuration.test'
const service={id:'photos',name:'照片库',domain_id:'home',hostname:'photos.example.com',scheme:'http',host:'10.0.0.8',port:2283,enabled:true,notes:'',dial:'10.0.0.8:2283',updated_at:'now'}
const config={origin:'https://admin.example.com',admin_domain:'admin.example.com',domains:[{id:'home',name:'example.com',access:'internet'}],console_lan_only:false,lan_cidrs:[],upstream_cidrs:['0.0.0.0/0','::/0'],allowed_names:['*'],denied_ips:[],resolvers:['1.1.1.1']}
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
function view(check?:()=>Promise<Response>){
 const fetch=vi.fn().mockImplementation(async(path:string,options?:RequestInit)=>{
  if(path==='/api/v1/services/check')return check?check():new Response(JSON.stringify({revision:3,effective:{compression:'gzip'},config:{apps:{}}}))
  if(options?.method==='PUT')return new Response(JSON.stringify({revision:4}))
  return new Response(JSON.stringify(path.endsWith('/settings')?{revision:3,config}:path.endsWith('/services')?{revision:3,services:[service],published:[service]}:{items:[]}))
 });vi.stubGlobal('fetch',fetch);vi.stubGlobal('scrollTo',vi.fn())
 const root=createRootRoute({component:Outlet}),page=createRoute({getParentRoute:()=>root,path:'/services',component:Services})
 const router=createRouter({routeTree:root.addChildren([page]),history:createMemoryHistory({initialEntries:['/services']})})
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><RouterProvider router={router}/></QueryClientProvider>)
 return fetch
}
it('saves the static directory and API matcher with the service',async()=>{
 const fetch=view()
 fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}))
 fireEvent.change(screen.getByLabelText('服务类型'),{target:{value:'static_site'}})
 expect(screen.getByLabelText('API 路径')).toHaveValue('/api/*')
 fireEvent.change(screen.getByLabelText('静态目录'),{target:{value:'/srv/sites/photos'}})
 fireEvent.change(screen.getByLabelText('API 路径'),{target:{value:'/backend/*'}})
 fireEvent.click(screen.getByRole('button',{name:'保存草稿'}))
 await waitFor(()=>expect(fetch.mock.calls.some(([,o])=>o?.method==='PUT')).toBe(true))
 const body=JSON.parse(fetch.mock.calls.find(([,o])=>o?.method==='PUT')![1]!.body as string)
 expect(body.service.static_site).toMatchObject({root:'/srv/sites/photos',api_path:'/backend/*'})
 const file={...configurationFixture,services:[{...configurationFixture.services[0],static_site:body.service.static_site}]}
 expect(parseConfiguration(JSON.stringify(file)).services[0]).toMatchObject({static_site:body.service.static_site})
})
it('offers grpc and preserves custom ports, edits headers/compression/timeouts and saves a draft',async()=>{
 const fetch=view();fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}))
 fireEvent.change(screen.getByLabelText('上游协议'),{target:{value:'h2c'}})
 expect(screen.getByLabelText('端口')).toHaveValue(2283)
 expect(screen.getByText(/gRPC 消息压缩/)).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('上游协议'),{target:{value:'https'}})
 fireEvent.click(screen.getByText('高级配置'))
 fireEvent.change(screen.getByLabelText('响应压缩'),{target:{value:'zstd_gzip'}})
 fireEvent.change(screen.getByLabelText('连接超时'),{target:{value:'5s'}})
 fireEvent.click(screen.getByRole('button',{name:'添加请求头'}))
 fireEvent.change(screen.getByLabelText('请求头 1 名称'),{target:{value:'X-App'}})
 fireEvent.change(screen.getByLabelText('请求头 1 值'),{target:{value:'photos'}})
 fireEvent.click(screen.getByRole('button',{name:'保存草稿'}))
 await waitFor(()=>expect(fetch.mock.calls.some(([,o])=>o?.method==='PUT')).toBe(true))
 const body=JSON.parse(fetch.mock.calls.find(([,o])=>o?.method==='PUT')![1]!.body as string)
 expect(body.service).toMatchObject({scheme:'https',advanced:{compression:'zstd_gzip',dial_timeout:'5s',request_headers:[{name:'X-App',operation:'set',value:'photos'}]}})
})
it('checks unsaved input, shows effective JSON and invalidates the result after changes',async()=>{
 const fetch=view();fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}))
 fireEvent.click(screen.getByText('高级配置'));fireEvent.click(screen.getByText('服务级配置片段'))
 fireEvent.click(screen.getByRole('button',{name:'插入压缩示例'}))
 expect(screen.getByLabelText('Caddyfile 片段')).toHaveValue('encode zstd gzip')
 fireEvent.click(screen.getByRole('button',{name:'检查配置'}))
 expect(await screen.findByText(/配置检查通过/)).toBeInTheDocument()
 expect(fetch.mock.calls.some(([,o])=>o?.method==='PUT')).toBe(false)
 fireEvent.change(screen.getByLabelText('Caddyfile 片段'),{target:{value:'header X-App photos'}})
 expect(screen.queryByText(/配置检查通过/)).not.toBeInTheDocument()
})
it('preserves input on a failed check and ignores late success for older input',async()=>{
 let finish!:(r:Response)=>void
 const fetch=view(()=>new Promise(r=>{finish=r}))
 fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}));fireEvent.click(screen.getByText('高级配置'))
 fireEvent.change(screen.getByLabelText('连接超时'),{target:{value:'5s'}})
 fireEvent.click(screen.getByRole('button',{name:'检查配置'}))
 await waitFor(()=>expect(fetch.mock.calls.some(([p])=>p.endsWith('/check'))).toBe(true))
 fireEvent.change(screen.getByLabelText('连接超时'),{target:{value:'10s'}})
 finish(new Response(JSON.stringify({revision:3,effective:{},config:{}})))
 await waitFor(()=>expect(screen.getByRole('button',{name:'检查配置'})).toBeEnabled())
 expect(screen.queryByText(/配置检查通过/)).not.toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'检查配置'}));await waitFor(()=>expect(fetch.mock.calls.filter(([p])=>p.endsWith('/check'))).toHaveLength(2))
 finish(new Response(JSON.stringify({error:{code:'unavailable',message:'检查暂不可用'}}),{status:503}))
 expect(await screen.findByRole('alert')).toHaveTextContent('检查暂不可用')
 expect(screen.getByLabelText('连接超时')).toHaveValue('10s')
})
it('round trips advanced options in format two and refuses unknown option fields',()=>{
 const advanced={compression:'gzip',request_headers:[{name:'X-App',operation:'set',value:'photos'}],snippet:'header -Server',dial_timeout:'5s'}
 const file={...configurationFixture,services:[{...configurationFixture.services[0],scheme:'grpcs',advanced:{...advanced,compression:'off'}}]}
 expect(parseConfiguration(JSON.stringify(file)).services[0].advanced).toEqual(file.services[0].advanced)
 expect(()=>parseConfiguration(JSON.stringify({...file,services:[{...file.services[0],advanced:{...advanced,tls_insecure_skip_verify:true}}]}))).toThrow()
})
it('clears default compression so snippets can supply it and normalizes pasted grpc URLs',async()=>{
 const fetch=view();fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}))
 fireEvent.click(screen.getByText('高级配置'))
 fireEvent.change(screen.getByLabelText('响应压缩'),{target:{value:'gzip'}})
 fireEvent.change(screen.getByLabelText('响应压缩'),{target:{value:'off'}})
 fireEvent.click(screen.getByRole('button',{name:'检查配置'}))
 await screen.findByText(/配置检查通过/)
 const checked=JSON.parse(fetch.mock.calls.find(([p])=>p==='/api/v1/services/check')![1]!.body as string)
 expect(checked.service.advanced?.compression).toBeUndefined()
 for(const scheme of ['h2c','grpcs']){
  fireEvent.change(screen.getByLabelText('上游协议'),{target:{value:'http'}})
  fireEvent.change(screen.getByLabelText('响应压缩'),{target:{value:'gzip'}})
  fireEvent.paste(screen.getByLabelText('上游主机'),{clipboardData:{getData:()=>`${scheme}://backend.internal:50051`}})
  expect(screen.getByLabelText('上游协议')).toHaveValue(scheme)
  expect(screen.getByLabelText('响应压缩')).toHaveValue('off')
 }
})
it('checks and saves a custom site body with no primary upstream field',async()=>{
 const fetch=view()
 fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}))
 fireEvent.change(screen.getByLabelText('服务类型'),{target:{value:'custom'}})
 const source=screen.getByLabelText('Caddyfile 正文')
 expect(source).toHaveValue('reverse_proxy http://10.0.0.8:2283')
 expect(screen.queryByLabelText('上游主机')).not.toBeInTheDocument()
 expect(screen.queryByText('高级配置')).not.toBeInTheDocument()
 const caddyfile='respond "hello"'
 fireEvent.change(source,{target:{value:caddyfile}})
 fireEvent.click(screen.getByRole('button',{name:'检查配置'}))
 expect(await screen.findByText(/配置检查通过/)).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'保存草稿'}))
 await waitFor(()=>expect(fetch.mock.calls.some(([,o])=>o?.method==='PUT')).toBe(true))
 const inputs=fetch.mock.calls.filter(([p,o])=>p.endsWith('/check')||o?.method==='PUT')
 for(const [, options] of inputs){
  const {service}=JSON.parse(options!.body as string)
  expect(service).toMatchObject({scheme:'http',host:'',port:80,caddyfile})
  expect(service).not.toHaveProperty('static_site')
  expect(service).not.toHaveProperty('advanced')
 }
 const file={...configurationFixture,services:[{...configurationFixture.services[0],scheme:'http',host:'',port:80,caddyfile}]}
 expect(parseConfiguration(JSON.stringify(file)).services[0]).toMatchObject({caddyfile})
})
it('requires explicit confirmation for replacing advanced options or custom source and retains the custom draft while switching',async()=>{
 view()
 fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}))
 fireEvent.click(screen.getByText('高级配置'))
 fireEvent.change(screen.getByLabelText('连接超时'),{target:{value:'5s'}})
 fireEvent.change(screen.getByLabelText('服务类型'),{target:{value:'custom'}})
 expect(screen.getByLabelText('连接超时')).toHaveValue('5s')
 fireEvent.click(screen.getByRole('button',{name:'继续使用自定义配置'}))
 fireEvent.change(screen.getByLabelText('Caddyfile 正文'),{target:{value:'respond "saved"'}})
 fireEvent.change(screen.getByLabelText('服务类型'),{target:{value:'proxy'}})
 expect(screen.getByLabelText('Caddyfile 正文')).toHaveValue('respond "saved"')
 fireEvent.click(screen.getByRole('button',{name:'继续使用快捷配置'}))
 expect(screen.getByLabelText('连接超时')).toHaveValue('5s')
 fireEvent.change(screen.getByLabelText('服务类型'),{target:{value:'custom'}})
 fireEvent.click(screen.getByRole('button',{name:'继续使用自定义配置'}))
 expect(screen.getByLabelText('Caddyfile 正文')).toHaveValue('respond "saved"')
})
it('defaults a new pure static service to the project mount without requiring an upstream',async()=>{
 const fetch=view()
 const create=await screen.findByRole('button',{name:'新建服务'})
 await waitFor(()=>expect(create).toBeEnabled())
 fireEvent.click(create)
 fireEvent.change(screen.getByLabelText('服务类型'),{target:{value:'files'}})
 fireEvent.change(screen.getByLabelText('服务名称'),{target:{value:'文档'}})
 fireEvent.change(screen.getByLabelText('子域名'),{target:{value:'docs'}})
 expect(screen.getByLabelText('静态目录')).toHaveValue('/srv/sites/docs')
 expect(screen.queryByLabelText('上游主机')).not.toBeInTheDocument()
 expect(screen.getByText(/SITES_DIR/)).toBeVisible()
 fireEvent.click(screen.getByRole('button',{name:'检查配置'}))
 await waitFor(()=>expect(fetch.mock.calls.some(([p])=>p.endsWith('/check'))).toBe(true))
 const [,options]=fetch.mock.calls.find(([p])=>p.endsWith('/check'))!
 expect(JSON.parse(options!.body as string).service).toMatchObject({scheme:'http',host:'',port:80,static_site:{root:'/srv/sites/docs',spa_fallback:false}})
})
