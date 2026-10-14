import {describe,expect,it} from 'vitest'
import {createMockApi} from '../mock-api'
import type {Deployment,Preview,ServiceList,Settings} from './model'

describe('static demo',()=>{
 it('includes varied example services with published, changed, unpublished and disabled states',()=>{
  const demo=createMockApi({staticDemo:true})
  const list=demo.handle('GET','/services').body as ServiceList
  expect(list.services.map(s=>s.name)).toEqual(['照片库','监控面板','智能家居','影音库','文件管理','下载中心'])
  expect(list.published.map(s=>s.id)).toEqual(['photos','grafana','media','files'])
  expect(list.services.find(s=>s.id==='downloads')?.enabled).toBe(false)
  expect(list.services.find(s=>s.id==='files')?.scheme).toBe('https')
  expect((demo.handle('GET','/draft/preview').body as Preview).changes.map(c=>c.kind)).toEqual(['updated','added','added'])
 })
 it('adds samples to a customized initialized instance without overwriting drafts or publishing',()=>{
  const demo=createMockApi({staticDemo:true,setup:true})
  demo.handle('POST','/setup/complete',{settings:{domain:'demo.example.com',resolvers:['1.1.1.1']}})
  const before=demo.handle('GET','/services').body as ServiceList
  const sample={name:'我的服务',domain_id:'home',hostname:'photos.demo.example.com',scheme:'http',host:'192.168.1.9',port:9000,enabled:true,notes:'保留用户修改'}
  demo.handle('POST','/services',{revision:before.revision,service:sample})
  const edited=demo.handle('GET','/services').body as ServiceList
  expect(demo.handle('POST','/demo/services',{revision:before.revision}).status).toBe(409)
  expect(demo.handle('POST','/demo/services',{revision:edited.revision}).body).toMatchObject({added:5})
  const after=demo.handle('GET','/services').body as ServiceList
  expect(after.services).toHaveLength(6)
  expect(after.services[0]).toMatchObject(sample)
  expect(after.services.every(s=>s.domain_id==='home'&&s.hostname.endsWith('.demo.example.com'))).toBe(true)
  expect(after.published).toEqual([])
  expect(demo.handle('GET',`/draft/revisions/${after.revision}`).status).toBe(200)
  expect(demo.handle('POST','/demo/services',{revision:after.revision}).body).toMatchObject({added:0,revision:after.revision})
 })
 it('completes initialization and DNS locally, leaving new services as drafts',()=>{
  const demo=createMockApi({staticDemo:true,setup:true})
  expect(demo.handle('GET','/setup/progress').body).toEqual({status:'manual',stage:'configuration'})
  expect(demo.handle('GET','/setup/status').body).toMatchObject({initialized:false})
  expect(demo.handle('POST','/setup/recheck',{setup_id:'static-demo'}).status).toBe(409)
  const plan=demo.handle('POST','/setup/dns/preview',{domain:'demo.example.com',address:'192.168.1.5'}).body as {fingerprint:string}
  expect(plan).toMatchObject({name:'*.demo.example.com',address:'192.168.1.5',action:'create'})
  expect(demo.handle('POST','/setup/dns/confirm',{settings:{domain:'demo.example.com'},cloudflare:{address:'192.168.1.5',fingerprint:plan.fingerprint,confirmed:true}}).status).toBe(200)
  expect(demo.handle('POST','/setup/dns/preview',{domain:'demo.example.com',address:'192.168.1.5'}).body).toMatchObject({action:'reuse'})
  expect(demo.handle('POST','/setup/preflight',{settings:{domain:'demo.example.com',resolvers:['1.1.1.1']}}).body).toMatchObject({can_complete:true,requires_acknowledgement:false})
  expect(demo.handle('POST','/setup/complete',{username:'visitor',password:'never-store-password',token:'never-store-token',settings:{domain:'demo.example.com',resolvers:['1.1.1.1']}}).status).toBe(200)
  expect(demo.handle('GET','/setup/handoff').body).toMatchObject({initialized:true,console_status:'ready',dns_status:'ready'})
  expect(demo.handle('GET','/services').body).toMatchObject({services:[],published:[]})
  expect(demo.handle('GET','/auth/session').body).toMatchObject({username:'visitor'})
  expect(demo.handle('GET','/certificates').body).toMatchObject({items:[{subject:'*.demo.example.com'}]})
  expect(demo.handle('GET','/audit').body).toMatchObject({items:[{actor:'visitor',action:'setup.complete'}]})
 })
 it('reuses default setup DNS and rejects an unconfirmed change in the simulator',()=>{
  const demo=createMockApi({staticDemo:true,setup:true})
  expect(demo.handle('POST','/setup/dns/preview',{domain:'home.example.com',address:'192.168.1.5'}).body).toMatchObject({action:'reuse'})
  const plan=demo.handle('POST','/setup/dns/preview',{domain:'home.example.com',address:'192.168.1.8'}).body as {fingerprint:string}
  expect(plan).toMatchObject({action:'update',old_address:'192.168.1.5'})
  expect(demo.handle('POST','/setup/dns/confirm',{settings:{domain:'home.example.com'},cloudflare:{address:'192.168.1.8',fingerprint:plan.fingerprint,confirmed:false}}).status).toBe(422)
  expect(demo.handle('POST','/setup/dns/preview',{domain:'home.example.com',address:'192.168.1.5'}).body).toMatchObject({action:'reuse'})
 })
 it('finishes certificate activation and exposes domain DNS operations',()=>{
  const demo=createMockApi({staticDemo:true})
  expect(demo.handle('POST','/settings/cloudflare',{token:'never-store-token',enable:true}).status).toBe(200)
  expect(demo.handle('GET','/settings').body).toMatchObject({token_configured:true,certificate_status:{activation_status:'success',public_status:'ready'}})
  expect(demo.handle('POST','/settings/dns/preview',{domain_id:'home',address:'192.168.1.5'}).status).toBe(200)
  expect(demo.handle('POST','/settings/dns/confirm',{fingerprint:'mock-domain-dns',confirm:true}).status).toBe(200)
 })
 it('publishes immediately and rolls back without replacing unpublished drafts',()=>{
  const demo=createMockApi({staticDemo:true})
  const initial=demo.handle('GET','/services').body as ServiceList
  const checked=demo.handle('POST','/draft/validate',{revision:initial.revision}).body as Preview
  const release=demo.handle('POST','/deployments',{revision:checked.revision,validation_id:checked.validation_id,expected_hash:checked.runtime_hash}).body as Deployment
  expect(release.status).toBe('success')
  expect((demo.handle('GET','/services').body as ServiceList).published).toEqual(initial.services)
  const rollback=demo.handle('POST','/draft/validate',{revision:initial.revision,rollback_id:'release-1'}).body as Preview
  expect(demo.handle('POST','/deployments',{revision:rollback.revision,validation_id:rollback.validation_id,expected_hash:rollback.runtime_hash}).status).toBe(200)
  const after=demo.handle('GET','/services').body as ServiceList
  expect(after.services).toEqual(initial.services)
  expect(after.published.map(s=>s.id)).toEqual(['photos'])
 })
 it('imports services using newly registered domains and arbitrary demo upstreams',()=>{
  const demo=createMockApi({staticDemo:true})
  const settings=demo.handle('GET','/settings').body as Settings
  const {test_tls:_,...policy}=settings.config
  policy.domains.push({id:'public',name:'demo.example.com',access:'internet'})
  expect(demo.handle('PUT','/settings',{revision:settings.revision,settings:policy,confirm_exposure:true}).status).toBe(200)
  const configuration=demo.handle('GET','/configuration/export').body as {services:unknown[]}
  configuration.services=[{name:'演示',domain_id:'public',hostname:'app.demo.example.com',scheme:'https',host:'app.internal',port:443,enabled:true,notes:''}]
  expect(demo.handle('POST','/configuration/preview',{configuration}).status).toBe(200)
  expect(demo.handle('POST','/configuration/import',{configuration,revision:settings.revision+1,confirm:true}).status).toBe(200)
 })
 it('retains historical drafts referenced by settings and console handoff audits',()=>{
  const demo=createMockApi({staticDemo:true})
  const before=demo.handle('GET','/settings').body as Settings
  const {test_tls:_,...policy}=before.config
  policy.admin_domain='console.home.example.com'
  expect(demo.handle('PUT','/settings',{revision:before.revision,settings:policy}).status).toBe(200)
  expect(demo.handle('GET',`/draft/revisions/${before.revision+1}`).status).toBe(200)
  const checked=demo.handle('POST','/draft/validate',{revision:before.revision+1}).body as Preview
  expect(demo.handle('POST','/deployments',{revision:checked.revision,validation_id:checked.validation_id,expected_hash:checked.runtime_hash}).status).toBe(200)
  expect(demo.handle('POST','/settings/console/complete',{revision:before.revision+1,confirm:true}).status).toBe(200)
  expect(demo.handle('GET',`/draft/revisions/${before.revision+2}`).status).toBe(200)
 })
 it('reports unpublished policy changes after all service drafts have been published',()=>{
  const demo=createMockApi({staticDemo:true})
  const before=demo.handle('GET','/settings').body as Settings
  const checked=demo.handle('POST','/draft/validate',{revision:before.revision}).body as Preview
  demo.handle('POST','/deployments',{revision:checked.revision,validation_id:checked.validation_id,expected_hash:checked.runtime_hash})
  const {test_tls:_,...policy}=before.config
  policy.resolvers=['1.1.1.1']
  expect(demo.handle('PUT','/settings',{revision:before.revision,settings:policy}).status).toBe(200)
  expect(demo.handle('GET','/overview').body).toMatchObject({unpublished:true})
 })
 it('attributes initialization audits to the chosen demo administrator',()=>{
  const demo=createMockApi({staticDemo:true,setup:true})
  demo.handle('POST','/setup/complete',{username:'visitor'})
  expect(demo.handle('GET','/audit').body).toMatchObject({items:[{actor:'visitor',action:'setup.complete'}]})
 })
})

it('initializes with open defaults but preserves an explicitly imported policy',()=>{
 const demo=createMockApi({staticDemo:true,setup:true})
 demo.handle('POST','/setup/complete',{settings:{domain:'demo.example.com'}})
 const s=demo.handle('GET','/settings').body as Settings
 expect(s.config).toMatchObject({domains:[{id:'home',name:'demo.example.com',access:'internet'}],console_lan_only:false,lan_cidrs:['0.0.0.0/0','::/0'],upstream_cidrs:['0.0.0.0/0','::/0'],allowed_names:['*'],denied_ips:[]})
 const {test_tls:_,...imported}=s.config
 imported.domains[0].access=null;imported.lan_cidrs=[];imported.upstream_cidrs=[];imported.allowed_names=[]
 const other=createMockApi({staticDemo:true,setup:true})
 other.handle('POST','/setup/complete',{settings:{domain:'demo.example.com'},import_settings:imported})
 expect((other.handle('GET','/settings').body as Settings).config).toMatchObject(imported)
})

it('keeps imported console restrictions and extra domains in drafts during setup',()=>{
 const demo=createMockApi({staticDemo:true,setup:true})
 const imported={origin:'https://caddyadmin.demo.example.com',admin_domain:'caddyadmin.demo.example.com',domains:[{id:'home',name:'demo.example.com',access:'trusted' as const},{id:'other',name:'other.example.net',access:null}],console_lan_only:true,lan_cidrs:['10.0.0.0/8'],upstream_cidrs:[],allowed_names:[],denied_ips:[],resolvers:['1.1.1.1']}
 demo.handle('POST','/setup/complete',{settings:{domain:'demo.example.com'},import_settings:imported})
 const data=demo.handle('GET','/settings').body as Settings
 expect(data.config).toMatchObject(imported)
 expect(data.active_config.console_lan_only).toBe(false)
 expect(data.active_config.domains).toEqual([imported.domains[0]])
})
