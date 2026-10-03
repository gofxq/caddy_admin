import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'

beforeEach(()=>{
 vi.resetModules();vi.stubEnv('MODE','demo');sessionStorage.clear()
 vi.stubGlobal('location',new URL('http://localhost:5178/setup'))
 vi.stubGlobal('fetch',vi.fn().mockRejectedValue(new Error('No backend or DoH is available')))
})
afterEach(()=>{cleanup();vi.unstubAllEnvs();vi.unstubAllGlobals();sessionStorage.clear()})

it('runs the entire initialization wizard over HTTP without any backend, DNS request or HTTPS redirect',async()=>{
 const {Setup}=await import('./pages/Setup')
 render(<Setup/>)
 expect(await screen.findByLabelText('管理员密码')).toHaveValue('demo-password')
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(screen.getByLabelText('首个通配符域名')).toHaveValue('home.example.com')
 fireEvent.click(screen.getByRole('button',{name:'预览 DNS 变更'}))
 fireEvent.click(await screen.findByRole('checkbox',{name:/确认.*DNS/}))
 fireEvent.click(screen.getByRole('button',{name:'确认配置'}))
 await screen.findByText(/DNS 配置已确认/)
 fireEvent.click(screen.getByRole('button',{name:'检查 DNS'}))
 await waitFor(()=>expect(screen.getByRole('button',{name:'下一步'})).toBeEnabled())
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 await screen.findByRole('heading',{name:'确认配置'})
 fireEvent.click(screen.getByRole('button',{name:'完成初始化'}))
 expect(await screen.findByRole('heading',{name:'初始化已完成'})).toBeInTheDocument()
 expect(screen.getByRole('link',{name:'打开演示控制台'})).toHaveAttribute('href','/')
 expect(fetch).not.toHaveBeenCalled()
 const {api}=await import('./api')
 expect(await api('/services')).toMatchObject({services:[],published:[]})
})
