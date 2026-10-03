import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {createMemoryHistory,createRootRoute,createRoute,createRouter,Outlet,RouterProvider} from '@tanstack/react-router'
import {ServiceDetail} from './pages/ServiceDetail'
import type {ServiceDetail as Detail,UpstreamCheck} from './model'

const service={id:'photos',name:'照片库',domain_id:'home',hostname:'photos.example.com',scheme:'http' as const,host:'10.42.0.8',port:2283,enabled:true,notes:'照片备份',dial:'10.42.0.8:2283',updated_at:'2026-10-03T00:00:00Z'}
const data=():Detail=>({id:'photos',revision:8,draft:{...service,hostname:'new.example.com',host:'10.42.0.9',dial:'10.42.0.9:2283'},published:service,draft_domain:{id:'home',name:'example.com',access:null},published_domain:{id:'home',name:'example.com',access:'trusted'},runtime:{deployment_id:'release-1',version:1,status:'matched',reachable:true,expected_hash:'version-1',runtime_hash:'version-1',checked_at:'2026-10-03T00:00:00Z',message:'运行配置与已发布版本一致；这不代表上游应用健康'},recent_deployments:[],external_caddy:false})
const response=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status})
const result:UpstreamCheck={deployment_id:'release-1',status:'reachable',target:service.dial,duration_ms:2,checked_at:'2026-10-03T00:01:00Z',expected_hash:'version-1',vantage:'manager',message:'Manager 可建立 TCP 连接；不代表 HTTP 应用正常或 HTTPS 证书有效'}
function view(id='photos'){
 vi.stubGlobal('scrollTo',vi.fn())
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 const root=createRootRoute({component:Outlet})
 const route=createRoute({getParentRoute:()=>root,path:'/services/$id',component:()=> <ServiceDetail id={route.useParams().id}/>})
 const services=createRoute({getParentRoute:()=>root,path:'/services',component:()=> <p>服务列表</p>})
 const releases=createRoute({getParentRoute:()=>root,path:'/deployments',component:()=> <p>发布列表</p>})
 const router=createRouter({routeTree:root.addChildren([route,services,releases]),history:createMemoryHistory({initialEntries:[`/services/${id}`]})})
 render(<QueryClientProvider client={client}><RouterProvider router={router}/></QueryClientProvider>)
 return {client,router}
}
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks()})
it('separates draft and published configuration without claiming application health',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(response(data())));view()
 const draft=await screen.findByRole('region',{name:'服务草稿'}),published=screen.getByRole('region',{name:'已发布配置'})
 expect(draft).toHaveTextContent('new.example.com');expect(draft).toHaveTextContent('待配置')
 expect(published).toHaveTextContent('photos.example.com');expect(published).toHaveTextContent('仅可信网络')
 expect(screen.getByText(/这不代表上游应用健康/)).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'检查已发布上游'})).toBeEnabled()
})
it('keeps a pending deletion visible and checks only the published version',async()=>{
 const detail=data();detail.draft=null;detail.draft_domain=null
 const fetch=vi.fn().mockImplementation(async(path:string)=>response(path.endsWith('/check-upstream')?result:detail));vi.stubGlobal('fetch',fetch);view()
 expect(await screen.findByText(/已从草稿删除/)).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'检查已发布上游'}))
 expect(await screen.findByText('TCP 可连接')).toBeInTheDocument()
 const call=fetch.mock.calls.find(([path])=>path.endsWith('/check-upstream'))
 expect(JSON.parse(call![1].body)).toEqual({expected_hash:'version-1',expected_deployment_id:'release-1'})
 expect(screen.getByRole('region',{name:'上游检查'})).toHaveTextContent('Manager')
})
it.each(['unknown','drift','pending'] as const)('preserves configuration and disables probing when runtime is %s',async status=>{
 const detail=data();detail.runtime.status=status
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(response(detail)));view()
 expect(await screen.findByRole('region',{name:'已发布配置'})).toHaveTextContent('photos.example.com')
 expect(screen.getByRole('button',{name:'检查已发布上游'})).toBeDisabled()
})
it('does not expose a check for a draft-only service',async()=>{
 const detail=data();detail.published=null;detail.published_domain=null
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(response(detail)));view()
 expect(await screen.findByText('尚未发布')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'检查已发布上游'})).toBeDisabled()
})
it('retains configuration after a failed manual check and allows retry',async()=>{
 let checks=0
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(path:string)=>path.endsWith('/check-upstream')?(++checks===1?response({error:{code:'unavailable',message:'检查暂不可用'}},503):response(result)):response(data())));view()
 fireEvent.click(await screen.findByRole('button',{name:'检查已发布上游'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('检查暂不可用')
 expect(screen.getByRole('region',{name:'已发布配置'})).toHaveTextContent('photos.example.com')
 fireEvent.click(screen.getByRole('button',{name:'重试检查'}));expect(await screen.findByText('TCP 可连接')).toBeInTheDocument()
})
it('clears previous and late results when the published version changes',async()=>{
 let detail=data(),resolveCheck:(value:Response)=>void=()=>{}
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(path:string)=>path.endsWith('/check-upstream')?new Promise<Response>(resolve=>{resolveCheck=resolve}):response(detail)))
 const {client}=view();fireEvent.click(await screen.findByRole('button',{name:'检查已发布上游'}))
 await waitFor(()=>expect(screen.getByRole('button',{name:'检查中…'})).toBeDisabled())
 detail={...detail,runtime:{...detail.runtime,expected_hash:'version-2',runtime_hash:'version-2'}}
 await client.refetchQueries({queryKey:['service-detail','photos']})
 resolveCheck(response(result))
 await waitFor(()=>expect(screen.getByRole('button',{name:'检查已发布上游'})).toBeEnabled())
 expect(screen.queryByText('TCP 可连接')).not.toBeInTheDocument()
})
it('does not show the previous service check after navigating to another service',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(path:string)=>path.endsWith('/check-upstream')?response(result):response({...data(),id:path.endsWith('/another')?'another':'photos'})))
 const {router}=view();fireEvent.click(await screen.findByRole('button',{name:'检查已发布上游'}));await screen.findByText('TCP 可连接')
 await router.navigate({to:'/services/$id',params:{id:'another'}})
 await screen.findByRole('region',{name:'已发布配置'});expect(screen.queryByText('TCP 可连接')).not.toBeInTheDocument()
})
it('provides a retry after the initial detail request fails',async()=>{
 let reads=0;vi.stubGlobal('fetch',vi.fn().mockImplementation(async()=>++reads===1?response({error:{code:'unavailable',message:'详情暂不可用'}},503):response(data())));view()
 const alert=await screen.findByRole('alert');fireEvent.click(within(alert).getByRole('button',{name:'重试'}));expect(await screen.findByRole('region',{name:'服务草稿'})).toHaveTextContent('new.example.com')
})

it.each([503,404])('clears verified state when a background detail refresh fails with %s',async status=>{
 let fail=false
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(path:string)=>path.endsWith('/check-upstream')?response(result):fail?response({error:{code:'unavailable',message:'详情状态不可确认'}},status):response(data())))
 const {client}=view();fireEvent.click(await screen.findByRole('button',{name:'检查已发布上游'}));await screen.findByText('TCP 可连接')
 fail=true;await client.refetchQueries({queryKey:['service-detail','photos']})
 await screen.findByRole('alert')
 expect(screen.getByRole('region',{name:'已发布配置'})).toHaveTextContent('photos.example.com')
 expect(screen.queryByText('TCP 可连接')).not.toBeInTheDocument()
 expect(screen.queryByText('配置一致')).not.toBeInTheDocument()
 expect(screen.getByRole('button',{name:'检查已发布上游'})).toBeDisabled()
})
it('rejects a late check response after a background refresh fails',async()=>{
 let fail=false,resolveCheck:(value:Response)=>void=()=>{}
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(path:string)=>path.endsWith('/check-upstream')?new Promise<Response>(resolve=>{resolveCheck=resolve}):fail?response({error:{code:'unavailable',message:'详情状态不可确认'}},503):response(data())))
 const {client}=view();fireEvent.click(await screen.findByRole('button',{name:'检查已发布上游'}));await screen.findByRole('button',{name:'检查中…'})
 fail=true;await client.refetchQueries({queryKey:['service-detail','photos']});await screen.findByRole('alert')
 resolveCheck(response(result));await waitFor(()=>expect(screen.queryByRole('button',{name:'检查中…'})).not.toBeInTheDocument())
 expect(screen.queryByText('TCP 可连接')).not.toBeInTheDocument()
})

it('clears a completed check after a same-hash new release',async()=>{
 let detail=data()
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(path:string)=>response(path.endsWith('/check-upstream')?result:detail)))
 const {client}=view();fireEvent.click(await screen.findByRole('button',{name:'检查已发布上游'}));await screen.findByText('TCP 可连接')
 detail={...detail,runtime:{...detail.runtime,deployment_id:'release-2',version:2}}
 await client.refetchQueries({queryKey:['service-detail','photos']})
 await waitFor(()=>expect(screen.queryByText('TCP 可连接')).not.toBeInTheDocument())
})
