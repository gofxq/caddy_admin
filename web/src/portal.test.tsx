import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Portal} from './pages/Portal'
const services=[{name:'照片库',hostname:'photos.example.com',url:'https://photos.example.com'},{name:'监控',hostname:'grafana.example.com',url:'https://grafana.example.com'}]
const response=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status})
function view(){const client=new QueryClient({defaultOptions:{queries:{retry:false}}});render(<QueryClientProvider client={client}><Portal/></QueryClientProvider>);return client}
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
it('shows service links without requesting a session and filters names and hostnames',async()=>{
 vi.stubGlobal('fetch',vi.fn(async(path:string)=>path==='/api/v1/portal'?response({services}):response({error:{code:'unauthorized',message:'请先登录'}},401)))
 view()
 const link=await screen.findByRole('link',{name:/照片库/})
 expect(link).toHaveAttribute('href','https://photos.example.com');expect(link).toHaveAttribute('target','_blank');expect(link).toHaveAttribute('rel','noopener noreferrer')
 expect(screen.queryByText('管理员登录')).not.toBeInTheDocument()
 const search=screen.getByRole('searchbox',{name:'搜索服务'})
 fireEvent.change(search,{target:{value:'GRAFANA'}})
 expect(screen.queryByRole('link',{name:/照片库/})).not.toBeInTheDocument();expect(screen.getByRole('link',{name:/监控/})).toBeInTheDocument()
 fireEvent.change(search,{target:{value:'不存在'}});expect(screen.getByText('没有匹配的服务')).toBeInTheDocument()
})
it('shows an empty guide before the first publish',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>response({services:[]})));view()
 expect(await screen.findByText('暂无可访问的服务')).toBeInTheDocument()
})
it('allows retry after a failure while preserving the search',async()=>{
 let fails=true
 vi.stubGlobal('fetch',vi.fn(async()=>fails?response({error:{code:'unavailable',message:'服务列表暂不可用'}},503):response({services})))
 view();fireEvent.change(screen.getByRole('searchbox',{name:'搜索服务'}),{target:{value:'照片'}})
 await screen.findByRole('alert');fails=false;fireEvent.click(screen.getByRole('button',{name:'重试'}))
 expect(await screen.findByRole('link',{name:/照片库/})).toBeInTheDocument();expect(screen.queryByRole('link',{name:/监控/})).not.toBeInTheDocument()
})
it('hides stale links when a refresh fails',async()=>{
 let fails=false
 vi.stubGlobal('fetch',vi.fn(async()=>fails?response({error:{code:'policy_pending',message:'正在核对'}},503):response({services})))
 const client=view();await screen.findByRole('link',{name:/照片库/});fails=true
 await client.refetchQueries({queryKey:['portal']});await screen.findByRole('alert')
 expect(screen.queryByRole('link',{name:/照片库/})).not.toBeInTheDocument()
})
