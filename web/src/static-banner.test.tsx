import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import type {ServiceList} from './model'

afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.unstubAllEnvs();sessionStorage.clear()})
it('adds example drafts to an initialized demo from the banner without a backend',async()=>{
 vi.resetModules();vi.stubEnv('MODE','demo');sessionStorage.clear()
 const assign=vi.fn()
 vi.stubGlobal('location',{pathname:'/',assign})
 vi.stubGlobal('fetch',vi.fn().mockRejectedValue(new Error('No backend')))
 const {resetDemo}=await import('./demo/runtime')
 const {api}=await import('./api')
 resetDemo(true)
 await api('/setup/complete',{method:'POST',body:{settings:{domain:'demo.example.com'}}})
 const {DemoBanner}=await import('./demo/Banner')
 render(<DemoBanner/>)
 fireEvent.click(screen.getByRole('button',{name:'添加演示服务'}))
 await waitFor(()=>expect(assign).toHaveBeenCalledWith('/services'))
 const list=await api<ServiceList>('/services')
 expect(list.services).toHaveLength(6)
 expect(list.published).toEqual([])
 expect(list.services.every(s=>s.hostname.endsWith('.demo.example.com'))).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'添加演示服务'}))
 expect(await screen.findByRole('status')).toHaveTextContent('已经存在')
 expect((await api<ServiceList>('/services')).services).toHaveLength(6)
 expect(fetch).not.toHaveBeenCalled()
})
