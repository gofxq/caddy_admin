import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'

beforeEach(()=>{vi.resetModules();vi.stubEnv('MODE','demo');sessionStorage.clear();vi.stubGlobal('fetch',vi.fn().mockRejectedValue(new Error('No backend available')))})
afterEach(()=>{cleanup();vi.unstubAllEnvs();vi.unstubAllGlobals();sessionStorage.clear()})

it('activates certificates and completes domain DNS entirely in the browser',async()=>{
 const {Settings}=await import('./pages/Settings')
 const client=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<QueryClientProvider client={client}><Settings/></QueryClientProvider>)
 fireEvent.change(await screen.findByLabelText('Cloudflare API Token'),{target:{value:'demo-token'}})
 fireEvent.click(screen.getByRole('button',{name:'保存并启用'}))
 expect(await screen.findByRole('heading',{name:'域名 DNS'})).toBeInTheDocument()
 expect(screen.getByRole('status')).toHaveTextContent('模拟配置成功')
 fireEvent.change(screen.getByLabelText('域名 DNS 目标 IP'),{target:{value:'192.168.1.5'}})
 fireEvent.click(screen.getByRole('button',{name:'预览域名 DNS'}))
 fireEvent.click(await screen.findByRole('checkbox',{name:/确认上述域名 DNS/}))
 fireEvent.click(screen.getByRole('button',{name:'确认域名 DNS 变更'}))
 await screen.findByText(/域名 DNS 配置已确认/)
 fireEvent.click(screen.getByRole('button',{name:'检查域名 DNS'}))
 expect(await screen.findByText('DNS 查询通过')).toBeInTheDocument()
 expect(fetch).not.toHaveBeenCalled()
 client.clear()
})
