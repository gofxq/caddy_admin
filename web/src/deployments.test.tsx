import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Deployments} from './pages/Deployments'
import {api} from './api'
vi.mock('./api',async importOriginal=>({...await importOriginal<typeof import('./api')>(),api:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
const service={id:'one',name:'Name',hostname:'new.home.example.com',scheme:'http',host:'10.77.0.8',port:80,enabled:true,notes:'new notes'}
const preview={revision:1,services:[service],changes:[{kind:'updated',hostname:service.hostname,before:{...service,hostname:'old.home.example.com',notes:'old notes'},after:service}],config:{},hash:'candidate',runtime_hash:'base',expected_hash:'base',drift:false,validation_id:'valid',rollback_id:'',validation_expires_at:new Date(Date.now()+900000).toISOString()}
function setup(expires=preview.validation_expires_at){const qc=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}});vi.mocked(api).mockImplementation(async path=>path==='/draft/validate'?{...preview,validation_expires_at:expires}:path.startsWith('/draft/preview')?preview:{items:[]});render(<QueryClientProvider client={qc}><Deployments/></QueryClientProvider>);return qc}
it('disables publishing expired validation',async()=>{setup(new Date(Date.now()-1).toISOString());fireEvent.click(await screen.findByText('校验配置'));await waitFor(()=>expect(api).toHaveBeenCalledWith('/draft/validate',expect.anything()));await screen.findByText(/校验已到期/);expect(screen.getByRole('button',{name:'确认发布'})).toBeDisabled()})
it('shows old hostname and notes',async()=>{setup();await screen.findByText(/old.home.example.com/);await screen.findByText(/old notes/)})
it('disables publication when preview refresh fails',async()=>{const qc=setup();fireEvent.click(await screen.findByText('校验配置'));await waitFor(()=>expect(screen.getByRole('button',{name:'确认发布'})).toBeEnabled());vi.mocked(api).mockRejectedValue(new Error('preview offline'));await qc.invalidateQueries({queryKey:['preview']});await screen.findByText('preview offline');expect(screen.getByRole('button',{name:'确认发布'})).toBeDisabled()})
it('explains when configuration is redacted instead of presenting it as a recovery copy',async()=>{
 const qc=new QueryClient({defaultOptions:{queries:{retry:false}}})
 vi.mocked(api).mockImplementation(async path=>path.startsWith('/draft/preview')?{...preview,config_redacted_fields:['admin','storage']}:{items:[]})
 render(<QueryClientProvider client={qc}><Deployments/></QueryClientProvider>)
 await screen.findByText(/管理接口与证书存储配置已隐藏/)
 expect(screen.getByText(/不能直接用于恢复/)).toBeInTheDocument()
})

it('restores an applying task and disables validation and publication',async()=>{
 const applying={id:'deployment-running',version:2,revision:1,status:'applying',services:[],base_hash:'base',hash:'candidate',actor:'admin',created:'2026-10-01T00:00:00Z',finished:'',error:'',rollback_id:'',changes:preview.changes}
 const qc=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 vi.mocked(api).mockImplementation(async path=>path.startsWith('/deployments?')?{items:[applying]}:path==='/deployments/deployment-running'?{deployment:applying}:path.startsWith('/draft/preview')?preview:{items:[]})
 render(<QueryClientProvider client={qc}><Deployments/></QueryClientProvider>)
 expect(await screen.findByText('正在后台发布')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'校验配置'})).toBeDisabled()
 expect(screen.getByRole('button',{name:'确认发布'})).toBeDisabled()
 expect(screen.getByText(/关闭页面不会取消/)).toBeInTheDocument()
})

it('treats uncertain as high risk and offers no ordinary retry',async()=>{
 const uncertain={id:'deployment-uncertain',version:3,revision:1,status:'uncertain',services:[],base_hash:'base',hash:'candidate',actor:'admin',created:'2026-10-01T00:00:00Z',finished:'',error:'response lost',rollback_id:'',changes:preview.changes}
 const qc=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 vi.mocked(api).mockImplementation(async path=>path.startsWith('/deployments?')?{items:[uncertain]}:path==='/deployments/deployment-uncertain'?{deployment:uncertain}:path.startsWith('/draft/preview')?preview:{items:[]})
 render(<QueryClientProvider client={qc}><Deployments/></QueryClientProvider>)
 expect(await screen.findByText(/实际状态待核对/)).toBeInTheDocument()
 expect(screen.getByText(/不要重复发布/)).toBeInTheDocument()
 expect(screen.queryByText('重试发布')).not.toBeInTheDocument()
 expect(screen.getByRole('link',{name:'前往概览'})).toHaveAttribute('href','/')
})

it('explains a failed task and permits a fresh validation',async()=>{
 const failed={id:'failed-task',version:2,revision:1,status:'failed',services:[],base_hash:'base',hash:'candidate',actor:'admin',created:'2026-10-01T00:00:00Z',finished:'2026-10-01T00:00:01Z',error:'加载被拒绝',rollback_id:'',changes:preview.changes}
 const qc=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 vi.mocked(api).mockImplementation(async path=>path.startsWith('/deployments?')?{items:[failed]}:path==='/deployments/failed-task'?{deployment:failed}:preview)
 render(<QueryClientProvider client={qc}><Deployments/></QueryClientProvider>)
 expect(await screen.findByText('发布 v2 失败')).toBeInTheDocument()
 expect(screen.getByText(/候选配置未替换原运行配置/)).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'校验配置'})).toBeEnabled()
 expect(screen.getByRole('button',{name:'确认发布'})).toBeDisabled()
})

it('summarizes change kinds and affected domains before publication',async()=>{
 setup()
 fireEvent.click(await screen.findByText('校验配置'))
 await waitFor(()=>expect(screen.getByRole('button',{name:'确认发布'})).toBeEnabled())
 fireEvent.click(screen.getByRole('button',{name:'确认发布'}))
 expect(screen.getByText(/新增 0、修改 1、启用 0、停用 0、删除 0/)).toBeInTheDocument()
 expect(screen.getByText('受影响域名：old.home.example.com、new.home.example.com')).toBeInTheDocument()
})

it('switches from stale cached history to the latest unresolved task',async()=>{
 const old={id:'old-success',version:1,revision:1,status:'success',services:[],base_hash:'base',hash:'old',actor:'admin',created:'2026-09-30T00:00:00Z',finished:'2026-09-30T00:00:01Z',error:'',rollback_id:'',changes:[]}
 const applying={...old,id:'new-applying',version:2,status:'applying',finished:'',hash:'candidate'}
 let historyCalls=0
 const qc=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 vi.mocked(api).mockImplementation(async path=>path.startsWith('/deployments?')?{items:[historyCalls++?applying:old]}:path==='/deployments/old-success'?{deployment:old}:path==='/deployments/new-applying'?{deployment:applying}:path.startsWith('/draft/preview')?preview:{items:[]})
 render(<QueryClientProvider client={qc}><Deployments/></QueryClientProvider>)
 expect(await screen.findByText(/发布 v1 已成功上线/)).toBeInTheDocument()
 await qc.invalidateQueries({queryKey:['deployments']})
 expect(await screen.findByText('正在后台发布')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'校验配置'})).toBeDisabled()
})

it('recovers the unresolved task after a publication response is lost',async()=>{
 const applying={id:'committed-task',version:2,revision:1,status:'applying',services:[],base_hash:'base',hash:'candidate',actor:'admin',created:'2026-10-01T00:00:00Z',finished:'',error:'',rollback_id:'',changes:preview.changes}
 let committed=false
 const qc=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 vi.mocked(api).mockImplementation(async path=>{
  if(path==='/deployments'){committed=true;throw new Error('response lost')}
  if(path.startsWith('/deployments?'))return {items:committed?[applying]:[]}
  if(path==='/deployments/committed-task')return {deployment:applying}
  return preview
 })
 render(<QueryClientProvider client={qc}><Deployments/></QueryClientProvider>)
 fireEvent.click(await screen.findByText('校验配置'))
 await waitFor(()=>expect(screen.getByRole('button',{name:'确认发布'})).toBeEnabled())
 fireEvent.click(screen.getByRole('button',{name:'确认发布'}))
 fireEvent.click(screen.getByRole('button',{name:'立即发布'}))
 expect(await screen.findByText('正在后台发布')).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'校验配置',hidden:true})).toBeDisabled()
 expect(vi.mocked(api).mock.calls.filter(([path])=>path==='/deployments')).toHaveLength(1)
})

it('keeps querying the fixed task after its first detail request fails',async()=>{
 const applying={id:'offline-task',version:2,revision:1,status:'applying',services:[],base_hash:'base',hash:'candidate',actor:'admin',created:'2026-10-01T00:00:00Z',finished:'',error:'',rollback_id:'',changes:preview.changes}
 let queries=0
 const qc=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 vi.mocked(api).mockImplementation(async path=>{
  if(path.startsWith('/deployments?'))return {items:[applying]}
  if(path==='/deployments/offline-task'){if(queries++===0)throw new Error('offline');return {deployment:{...applying,status:'success',finished:'2026-10-01T00:00:01Z'}}}
  return preview
 })
 render(<QueryClientProvider client={qc}><Deployments/></QueryClientProvider>)
 expect(await screen.findByText(/已保留任务 ID/)).toBeInTheDocument()
 expect(screen.getByRole('button',{name:'校验配置'})).toBeDisabled()
 expect(await screen.findByText('发布 v2 已成功上线',{}, {timeout:4000})).toBeInTheDocument()
 expect(queries).toBeGreaterThan(1)
})
it('requires separate exposure confirmation when publishing an enabled internet service',async()=>{
 const settings={origin:'https://caddyadmin.example.com',admin_domain:'caddyadmin.example.com',domains:[{id:'internet',name:'example.com',access:'internet'}],console_lan_only:false,lan_cidrs:[],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:['1.1.1.1']}
 const candidate={...preview,settings,active_settings:settings,settings_changed:false,services:[{...service,domain_id:'internet',hostname:'photos.example.com'}],changes:[{kind:'added',hostname:'photos.example.com',after:{...service,domain_id:'internet',hostname:'photos.example.com'}}]}
 vi.mocked(api).mockImplementation(async path=>path.startsWith('/draft/')?candidate:{items:[]})
 render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})}><Deployments/></QueryClientProvider>)
 fireEvent.click(await screen.findByText('校验配置'));await waitFor(()=>expect(screen.getByRole('button',{name:'确认发布'})).toBeEnabled());fireEvent.click(screen.getByRole('button',{name:'确认发布'}));expect(screen.getByRole('button',{name:'立即发布'})).toBeDisabled();fireEvent.click(screen.getByLabelText('我已核对域名与策略变更，确认放开访问范围。'));expect(screen.getByRole('button',{name:'立即发布'})).toBeEnabled()
})
