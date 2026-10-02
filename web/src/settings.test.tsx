import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Settings} from './pages/Settings'

afterEach(()=>{cleanup();vi.unstubAllGlobals()})

it('shows bootstrap certificate state and submits the Cloudflare token once',async()=>{
 const fetch=vi.fn()
 fetch.mockResolvedValueOnce(new Response(JSON.stringify({config:{origin:'https://admin.home.example.com',public_domain:'example.com',homelab_domain:'home.example.com',admin_domain:'admin.home.example.com',lan_cidrs:['10.0.0.0/8'],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:['9.9.9.9'],test_tls:false},manager_version:'test',caddy_version:'test',cloudflare_module:true,token_configured:false,certificate_status:{mode:'bootstrap_internal',activation_status:'idle',public_status:'unknown',last_error_class:'',updated_at:'now'}}),{status:200}))
 fetch.mockResolvedValueOnce(new Response(JSON.stringify({certificate_status:{mode:'cloudflare',activation_status:'applying',public_status:'pending',last_error_class:'',updated_at:'now'},restart_required:true}),{status:202}))
 fetch.mockResolvedValueOnce(new Response(JSON.stringify({config:{origin:'https://admin.home.example.com',public_domain:'example.com',homelab_domain:'home.example.com',admin_domain:'admin.home.example.com',lan_cidrs:['10.0.0.0/8'],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:['9.9.9.9'],test_tls:false},manager_version:'test',caddy_version:'test',cloudflare_module:true,token_configured:true,certificate_status:{mode:'cloudflare',activation_status:'applying',public_status:'pending',last_error_class:'',updated_at:'now'}}),{status:200}))
 vi.stubGlobal('fetch',fetch)
 const qc=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<QueryClientProvider client={qc}><Settings/></QueryClientProvider>)
 expect(await screen.findByText(/内部引导证书/)).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText('Cloudflare API Token'),{target:{value:'secret-token'}})
 fireEvent.click(screen.getByRole('button',{name:'保存并启用'}))
 await waitFor(()=>expect(fetch).toHaveBeenCalledTimes(3))
 expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual({token:'secret-token',enable:true})
 expect(await screen.findByText(/服务正在安全重启/)).toBeInTheDocument()
 expect(await screen.findByText(/临时证书保持管理入口可访问/)).toBeInTheDocument()
})

it('shows that the optional Public domain is not configured',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({config:{origin:'https://caddyadmin.home.example.com',public_domain:'',homelab_domain:'home.example.com',admin_domain:'caddyadmin.home.example.com',lan_cidrs:['10.0.0.0/8'],upstream_cidrs:['10.0.0.0/8'],allowed_names:[],denied_ips:[],resolvers:[],test_tls:false},manager_version:'test',caddy_version:'test',cloudflare_module:true,token_configured:false,certificate_status:{mode:'bootstrap_internal',activation_status:'idle',public_status:'unknown',last_error_class:'',updated_at:'now'}}),{status:200})))
 const qc=new QueryClient({defaultOptions:{queries:{retry:false},mutations:{retry:false}}})
 render(<QueryClientProvider client={qc}><Settings/></QueryClientProvider>)
 expect(await screen.findByText('尚未配置')).toBeInTheDocument()
 expect(screen.queryByText('*.')).not.toBeInTheDocument()
})
