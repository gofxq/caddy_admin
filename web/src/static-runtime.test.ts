import {afterEach,beforeEach,expect,it,vi} from 'vitest'
import type {ServiceList,Settings} from './model'

beforeEach(()=>{vi.resetModules();vi.stubEnv('MODE','demo');sessionStorage.clear();vi.stubGlobal('fetch',vi.fn().mockRejectedValue(new Error('A static demo must not contact a backend')))})
afterEach(()=>{vi.unstubAllEnvs();vi.unstubAllGlobals();sessionStorage.clear()})

it('serves every console page and rejects missing APIs without falling back to the network',async()=>{
 const {api}=await import('./api')
 for(const path of ['/auth/session','/overview','/services','/services/photos','/draft/preview','/deployments','/certificates','/audit','/settings','/configuration/export'])expect(await api(path)).toBeTruthy()
 await expect(api('/missing')).rejects.toMatchObject({status:404,code:'not_found'})
 expect(fetch).not.toHaveBeenCalled()
})
it('retains drafts and published versions across a reload and resets explicitly',async()=>{
 let {api}=await import('./api')
 const before=await api<ServiceList>('/services')
 await api('/services/photos',{method:'PUT',body:{revision:before.revision,service:{...before.services[0],name:'刷新后保留'}}})
 vi.resetModules();({api}=await import('./api'))
 const after=await api<ServiceList>('/services')
 expect(after.services[0].name).toBe('刷新后保留')
 expect(after.published[0].name).toBe('照片库')
 const runtimePath='./demo/runtime'
 const {resetDemo}=await import(/* @vite-ignore */runtimePath)
 resetDemo()
 expect((await api<ServiceList>('/services')).services[0].name).toBe('照片库')
 expect(fetch).not.toHaveBeenCalled()
})
it('stores credential status without storing entered passwords or tokens',async()=>{
 const {api}=await import('./api')
 await api('/settings/cloudflare',{method:'POST',body:{token:'sensitive-demo-token',enable:true}})
 await api('/auth/password',{method:'POST',body:{current:'sensitive-current',password:'sensitive-new-password'}})
 const saved=Array.from({length:sessionStorage.length},(_,i)=>sessionStorage.getItem(sessionStorage.key(i)!)).join('')
 expect(saved).not.toMatch(/sensitive-demo-token|sensitive-current|sensitive-new-password/)
 expect(fetch).not.toHaveBeenCalled()
})
it('simulates DNS queries locally for both providers and IPv6',async()=>{
 const {checkSetupDNS}=await import('./setup-dns')
 for(const provider of ['cloudflare','google'] as const){
  const report=await checkSetupDNS('demo.example.com','2001:db8::5',provider)
  expect(report.verified).toBe(true)
  expect(report.queries).toHaveLength(2)
  expect(report.queries[0]).toMatchObject({addresses:['2001:db8::5'],status:'pass'})
 }
 expect(fetch).not.toHaveBeenCalled()
})
it('keeps the demo usable when browser storage is unavailable',async()=>{
 vi.spyOn(Storage.prototype,'getItem').mockImplementation(()=>{throw new Error('storage denied')})
 vi.spyOn(Storage.prototype,'setItem').mockImplementation(()=>{throw new Error('storage denied')})
 try{
  const {api}=await import('./api')
  await api('/settings/cloudflare',{method:'POST',body:{token:'demo-token'}})
  expect((await api<Settings>('/settings')).token_configured).toBe(true)
 }finally{vi.restoreAllMocks()}
})
