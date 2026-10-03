import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {createMemoryHistory,createRootRoute,createRoute,createRouter,Outlet,RouterProvider} from '@tanstack/react-router'
import {Overview} from './pages/Overview'
import {Services} from './pages/Services'
import {Deployments} from './pages/Deployments'
import {api} from './api'

vi.mock('./api',async importOriginal=>({...await importOriginal<typeof import('./api')>(),api:vi.fn()}))
beforeEach(()=>vi.stubGlobal('scrollTo',vi.fn()))
afterEach(()=>{cleanup();vi.resetAllMocks();vi.unstubAllGlobals()})

function renderPage(path:string,component:()=>React.JSX.Element){
 const root=createRootRoute({component:()=> <Outlet/>})
 const route=createRoute({getParentRoute:()=>root,path,component})
 const router=createRouter({routeTree:root.addChildren([route]),history:createMemoryHistory({initialEntries:[path]})})
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<QueryClientProvider client={client}><RouterProvider router={router}/></QueryClientProvider>)
 return router
}

const overview={reachable:true,drift:false,runtime_hash:'same',expected_hash:'same',version:0,enabled:0,draft_revision:0,unpublished:false,message:'运行配置与已发布版本一致',recent:[],checked_at:'2026-09-24T00:00:00Z'}
const settings={config:{origin:location.origin,domains:[{id:'home',name:'home.example.com',access:null}],console_lan_only:false,admin_domain:'caddyadmin.home.example.com',lan_cidrs:[],upstream_cidrs:[],allowed_names:[],denied_ips:[],resolvers:[],test_tls:false},manager_version:'test',caddy_version:'test',cloudflare_module:true,token_configured:false,certificate_status:{mode:'bootstrap_internal',activation_status:'idle',public_status:'unknown',last_error_class:'',updated_at:'now'},external_caddy:false}

it('guides a new administrator from creating a service to the first publication',async()=>{
 vi.mocked(api).mockImplementation(async path=>path==='/settings'?settings:overview)
 const router=renderPage('/',Overview)
 expect(await screen.findByRole('heading',{name:'首次运行任务中心'})).toBeInTheDocument()
 expect(screen.getByText('创建服务草稿',{selector:'strong'})).toBeInTheDocument()
 fireEvent.click(screen.getAllByRole('link',{name:'前往处理'}).find(link=>link.getAttribute('href')==='/services')!)
 await expect.poll(()=>router.state.location.pathname).toBe('/services')
})

it('moves the first-run guide to preview after a draft is saved and hides it after publication',async()=>{
 vi.mocked(api).mockImplementation(async path=>path==='/settings'?settings:{...overview,unpublished:true,draft_revision:1})
 const first=renderPage('/',Overview)
 expect(await screen.findByText('完成首次成功发布')).toBeInTheDocument()
 expect(screen.getByText('已有未发布服务草稿')).toBeInTheDocument()
 cleanup()
 vi.mocked(api).mockReset().mockImplementation(async path=>path==='/settings'?settings:{...overview,version:1,enabled:1})
 renderPage('/',Overview)
 await screen.findByText('最近发布')
 expect(screen.queryByRole('heading',{name:'首次运行任务中心'})).not.toBeInTheDocument()
 void first
})

it('opens the service editor from the empty state action',async()=>{
 vi.mocked(api).mockImplementation(async path=>path==='/services'?{revision:0,services:[],published:[]}:{config:{domains:[{id:'home',name:'home.example.com',access:'trusted'}],console_lan_only:false,upstream_cidrs:[],allowed_names:[]}})
 renderPage('/services',Services)
 fireEvent.click(await screen.findByRole('button',{name:'添加第一个服务'}))
 expect(screen.getByRole('heading',{name:'新建服务'})).toBeInTheDocument()
})

it('allows pending access domains to save drafts without a forced group',async()=>{
 vi.mocked(api).mockImplementation(async path=>path==='/services'?{revision:0,services:[],published:[]}:{config:{domains:[{id:'home',name:'home.example.com',access:null}],console_lan_only:false,upstream_cidrs:[],allowed_names:[]}})
 renderPage('/services',Services)
 fireEvent.click(await screen.findByRole('button',{name:'添加第一个服务'}))
 expect(screen.getByRole('option',{name:/home.example.com · 待配置/})).toBeEnabled()
 expect(screen.getByText(/待配置访问范围也可保存草稿/)).toBeInTheDocument()
})

it('links an empty deployment preview back to service editing',async()=>{
 vi.mocked(api).mockImplementation(async path=>path.startsWith('/draft/preview')?{revision:0,services:[],changes:[],config:{},hash:'candidate',runtime_hash:'base',expected_hash:'base',drift:false,validation_id:'',rollback_id:'',validation_expires_at:''}:{items:[]})
 renderPage('/deployments',Deployments)
 expect(await screen.findByRole('link',{name:'前往服务'})).toHaveAttribute('href','/services')
})
