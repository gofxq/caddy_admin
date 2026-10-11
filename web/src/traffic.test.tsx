import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {TrafficWorkspace} from './components/TrafficWorkspace'
const response=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'content-type':'application/json'}})
function view(){render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})}><TrafficWorkspace serviceID="photos" domainID="home"/></QueryClientProvider>)}
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
it('shows incomplete traffic without substituting a healthy status and filters redacted logs',async()=>{
 const fetch=vi.fn().mockImplementation(async(path:string)=>response(path.includes('/traffic?')?{from:1,to:2,step:60,coverage:.4,complete:false,summary:{requests:7,request_bytes:10,response_bytes:100,five_xx:2,p50:.1,p95:.2,p99:.3,first_byte_p95:.05,statuses:{'2xx':5,'5xx':2}},points:[],services:[]}:path.includes('/logs?')?{items:[]}:path.endsWith('/alerts')?{items:[]}:{state:'baseline',message:'计数基线建立中',logs_state:'ready',log_gap:true,dropped_logs:5}));vi.stubGlobal('fetch',fetch);view()
 expect(await screen.findByText(/统计覆盖率 40%/)).toBeInTheDocument();expect(screen.getByText(/日志存在缺口/)).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('日志状态码'),{target:{value:'503'}})
 await waitFor(()=>expect(fetch.mock.calls.some(([path])=>path.includes('status=503')&&path.includes('service_id=photos'))).toBe(true))
 expect(await screen.findByText('没有匹配的脱敏日志')).toBeInTheDocument()
})
it('keeps manual diagnosis targeted to a domain ID and displays classified events',async()=>{
 const fetch=vi.fn().mockImplementation(async(path:string)=>response(path.endsWith('/diagnostics')?{domain_id:'home',checked_at:'2026-10-03T00:00:00Z',vantage:'manager',checks:[{name:'CAA',status:'fail',message:'CAA 未许可',values:[]}],events:[{epoch:'e',sequence:1,time:'2026-10-03T00:00:00Z',kind:'diagnostic',class:'acme_rate_limit'}]}:path.includes('/traffic?')?{coverage:0,summary:{},points:[],services:[]}:path.endsWith('/alerts')||path.includes('/logs?')?{items:[]}:{state:'disabled',message:'尚未启用'}));vi.stubGlobal('fetch',fetch);view()
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS / CAA / TLS'}));expect(await screen.findByText('CAA 未许可')).toBeInTheDocument();expect(screen.getByText(/ACME 限速/)).toBeInTheDocument()
 const call=fetch.mock.calls.find(([path])=>path.endsWith('/diagnostics'));expect(JSON.parse(call![1].body)).toEqual({domain_id:'home'})
})
it('retains all service options after selecting one and supports a global query limit',async()=>{
 const traffic={from:1,to:2,step:60,coverage:1,complete:true,summary:{},points:[],services:[{id:'a',hostname:'a.test'},{id:'b',hostname:'b.test'}]}
 const fetch=vi.fn().mockImplementation(async(path:string)=>response(path.endsWith('/services')?{published:[{id:'fallback',hostname:'fallback.test'}]}:path.includes('/traffic?')?(path.includes('service_id=')?{...traffic,services:[traffic.services[0]]}:traffic):path.endsWith('/alerts')||path.includes('/logs?')?{items:[]}:{state:'ready'}));vi.stubGlobal('fetch',fetch)
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><TrafficWorkspace/></QueryClientProvider>)
 const select=screen.getByLabelText('统计服务');expect(await screen.findByRole('option',{name:/b.test/})).toBeInTheDocument()
 fireEvent.change(select,{target:{value:'a'}});await waitFor(()=>expect(fetch.mock.calls.some(([path])=>path.includes('service_id=a'))).toBe(true))
 expect(screen.getByRole('option',{name:/b.test/})).toBeInTheDocument();expect(screen.getByRole('option',{name:/fallback.test/})).toBeInTheDocument()
})
it('offers published services when the global statistics query exceeds its limit',async()=>{
 const fetch=vi.fn().mockImplementation(async(path:string)=>path.includes('/traffic?')&&!path.includes('service_id=')?response({error:{code:'validation_failed',message:'查询数据过多，请选择单个服务'}},422):response(path.endsWith('/services')?{published:[{id:'fallback',hostname:'fallback.test'}]}:path.includes('/traffic?')?{coverage:1,summary:{},points:[],services:[]}:path.endsWith('/alerts')||path.includes('/logs?')?{items:[]}:{state:'ready'}));vi.stubGlobal('fetch',fetch)
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><TrafficWorkspace/></QueryClientProvider>)
 expect(await screen.findByText('查询数据过多，请选择单个服务')).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('统计服务'),{target:{value:'fallback'}})
 await waitFor(()=>expect(fetch.mock.calls.some(([path])=>path.includes('/traffic?')&&path.includes('service_id=fallback'))).toBe(true))
})
it('keeps the seven day log query within server retention despite request delay',async()=>{
 const fetch=vi.fn().mockImplementation(async(path:string)=>response(path.includes('/traffic?')?{coverage:0,summary:{},points:[],services:[]}:path.endsWith('/alerts')||path.includes('/logs?')?{items:[]}:{state:'ready'}));vi.stubGlobal('fetch',fetch);view()
 fireEvent.change(screen.getByLabelText('统计时间'),{target:{value:'168'}})
 await waitFor(()=>expect(fetch.mock.calls.some(([path])=>{if(!path.includes('/logs?'))return false;const p=new URL(path,'https://test.invalid').searchParams;return Number(p.get('to'))-Number(p.get('from'))===7*24*3600-60})).toBe(true))
})
it('preserves an alert after failed acknowledgement and allows retry',async()=>{
 let acknowledged=false,attempts=0
 const alert={id:'five_xx:photos',service_id:'photos',kind:'five_xx',status:'active',message:'5xx 需要处理',first_seen:'2026-10-03T00:00:00Z',last_seen:'2026-10-03T00:00:00Z'}
 const fetch=vi.fn().mockImplementation(async(path:string)=>{if(path.includes('/acknowledge')){if(++attempts===1)return response({error:{code:'unavailable',message:'无法确认，请重试'}},503);acknowledged=true;return response({acknowledged:true})};return response(path.endsWith('/alerts')?{items:[{...alert,acknowledged}]}:path.includes('/traffic?')?{coverage:0,summary:{},points:[],services:[]}:path.includes('/logs?')?{items:[]}:{state:'ready'})});vi.stubGlobal('fetch',fetch);view()
 fireEvent.click(await screen.findByRole('button',{name:'确认已知晓'}));expect(await screen.findByText('无法确认，请重试')).toBeInTheDocument();expect(screen.getByText('5xx 需要处理')).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'确认已知晓'}));expect(await screen.findByText('已确认')).toBeInTheDocument();expect(attempts).toBe(2)
})
