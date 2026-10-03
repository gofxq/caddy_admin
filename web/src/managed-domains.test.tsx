import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {Setup} from './pages/Setup'
import {parseConfiguration} from './configuration'
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
it('uses three setup steps and confirms configured credentials without password inputs',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({initialized:false,external_caddy:false,test_tls:false,setup_id:'process',admin_password_status:'ready',cloudflare_token_status:'ready'}))))
 render(<Setup pollLogin={false}/> )
 expect(await screen.findByText('管理员密码已通过部署配置')).toBeInTheDocument()
 expect(screen.getByRole('list',{name:'初始化步骤'}).children).toHaveLength(3)
 expect(screen.queryByLabelText('管理员密码')).not.toBeInTheDocument()
 fireEvent.click(screen.getByLabelText('我确认使用已配置的管理员密码'))
 fireEvent.click(screen.getByRole('button',{name:'下一步'}))
 expect(await screen.findByLabelText('首个通配符域名')).toBeInTheDocument()
 expect(screen.getByText('Cloudflare Token 已通过部署配置')).toBeInTheDocument()
 expect(screen.queryByLabelText('Cloudflare API Token')).not.toBeInTheDocument()
 expect(screen.queryByLabelText('可信网络 CIDR')).not.toBeInTheDocument()
})
it('accepts format two domain references and rejects old format',()=>{
 const config={format:'caddy-web-admin',version:2,settings:{origin:'https://caddyadmin.example.com',admin_domain:'caddyadmin.example.com',domains:[{id:'d1',name:'example.com',access:null}],console_lan_only:false,lan_cidrs:[],upstream_cidrs:[],allowed_names:[],denied_ips:[],resolvers:['1.1.1.1']},services:[]}
 expect(parseConfiguration(JSON.stringify(config)).settings.domains).toEqual(config.settings.domains)
 expect(()=>parseConfiguration(JSON.stringify({...config,version:1}))).toThrow(/版本/)
})
it.each(['missing','ready','invalid'])('never echoes deployment password for status %s',async status=>{
 vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({initialized:false,test_tls:true,admin_password_status:status,cloudflare_token_status:'invalid',admin_password_error:'部署密码格式有误',setup_id:'p'}))))
 render(<Setup pollLogin={false}/>)
 await screen.findByRole('heading',{name:'初始化 Caddy Admin'})
 if(status==='missing')expect(screen.getByLabelText('管理员密码')).toHaveValue('')
 else expect(screen.queryByLabelText('管理员密码')).not.toBeInTheDocument()
 if(status==='invalid'){expect(screen.getByRole('alert')).toHaveTextContent('部署密码格式有误');expect(screen.getByRole('button',{name:'下一步'})).toBeDisabled()}
})
it('submits only configured password consent and setup identity in isolated mode',async()=>{
 const fetch=vi.fn().mockImplementation(async(path:string)=>path.endsWith('/status')?new Response(JSON.stringify({initialized:false,test_tls:true,admin_password_status:'ready',setup_id:'process'})):path.endsWith('/preflight')?new Response(JSON.stringify({normalized:{admin_domain:'caddyadmin.example.com'},checks:[],can_complete:true,requires_acknowledgement:false})):new Response(JSON.stringify({admin_origin:'https://caddyadmin.example.com'})))
 vi.stubGlobal('fetch',fetch);render(<Setup pollLogin={false}/>)
 fireEvent.click(await screen.findByLabelText('我确认使用已配置的管理员密码'));fireEvent.click(screen.getByRole('button',{name:'下一步'}));fireEvent.change(screen.getByLabelText('首个通配符域名'),{target:{value:'*.Example.com'}});fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'});fireEvent.click(screen.getByRole('button',{name:'完成初始化'}));await screen.findByRole('heading',{name:'初始化已完成'})
 const body=JSON.parse(fetch.mock.calls.find(c=>c[0].endsWith('/complete'))![1].body)
 expect(body).toMatchObject({use_configured_password:true,setup_id:'process',settings:{domain:'example.com',resolvers:['1.1.1.1']}})
 expect(body).not.toHaveProperty('password');expect(body).not.toHaveProperty('cloudflare')
})
it('refreshes stale configured credential confirmation while retaining the domain',async()=>{
 let statusCalls=0
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(path:string)=>path.endsWith('/status')?new Response(JSON.stringify({initialized:false,test_tls:true,admin_password_status:'ready',setup_id:statusCalls++?'new':'old'})):path.endsWith('/preflight')?new Response(JSON.stringify({normalized:{admin_domain:'caddyadmin.example.com'},checks:[],can_complete:true,requires_acknowledgement:false})):new Response(JSON.stringify({error:{code:'conflict',message:'预配置密码确认已失效，请重新读取初始化状态'}}),{status:409})))
 render(<Setup pollLogin={false}/>);fireEvent.click(await screen.findByLabelText('我确认使用已配置的管理员密码'));fireEvent.click(screen.getByRole('button',{name:'下一步'}));fireEvent.change(screen.getByLabelText('首个通配符域名'),{target:{value:'example.com'}});fireEvent.click(screen.getByRole('button',{name:'下一步'}));await screen.findByRole('heading',{name:'确认配置'});fireEvent.click(screen.getByRole('button',{name:'完成初始化'}));await screen.findByRole('alert');fireEvent.click(screen.getByRole('button',{name:'重新读取凭据状态'}));expect(await screen.findByLabelText('我确认使用已配置的管理员密码')).not.toBeChecked();fireEvent.click(screen.getByLabelText('我确认使用已配置的管理员密码'));fireEvent.click(screen.getByRole('button',{name:'下一步'}));expect(screen.getByLabelText('首个通配符域名')).toHaveValue('example.com')
})
