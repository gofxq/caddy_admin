import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {PasswordForm} from './pages/Login'
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks()})

it('saves an eight-character password and rejects seven characters',async()=>{
 const fetch=vi.fn().mockResolvedValue(new Response('{}',{status:200}));vi.stubGlobal('fetch',fetch)
 const qc=new QueryClient({defaultOptions:{mutations:{retry:false}}}),onDone=vi.fn()
 render(<QueryClientProvider client={qc}><PasswordForm onDone={onDone}/></QueryClientProvider>)
 fireEvent.change(screen.getByLabelText('当前密码'),{target:{value:'current-password'}})
 fireEvent.change(screen.getByLabelText('新密码'),{target:{value:'1234567'}})
 fireEvent.change(screen.getByLabelText('确认新密码'),{target:{value:'1234567'}})
 fireEvent.click(screen.getByRole('button',{name:'保存新密码'}));expect(fetch).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText('新密码'),{target:{value:'12345678'}})
 fireEvent.change(screen.getByLabelText('确认新密码'),{target:{value:'12345678'}})
 fireEvent.click(screen.getByRole('button',{name:'保存新密码'}))
 await waitFor(()=>expect(onDone).toHaveBeenCalledOnce())
 expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({current:'current-password',password:'12345678'})
})

it('fills both new password fields when generating and never submits automatically',()=>{
 const fetch=vi.fn();vi.stubGlobal('fetch',fetch)
 const qc=new QueryClient({defaultOptions:{mutations:{retry:false}}})
 render(<QueryClientProvider client={qc}><PasswordForm onDone={()=>{}}/></QueryClientProvider>)
 fireEvent.click(screen.getByRole('button',{name:'随机生成'}))
 const password=(screen.getByLabelText('新密码') as HTMLInputElement).value
 expect(password).toMatch(/^[A-Za-z0-9_-]{20}$/)
 expect(screen.getByLabelText('确认新密码')).toHaveValue(password)
 expect(screen.getByLabelText('当前密码')).toHaveValue('')
 expect(fetch).not.toHaveBeenCalled()
})

it('preserves the existing input when secure random generation is unavailable',async()=>{
 const random=vi.spyOn(crypto,'getRandomValues').mockImplementation(()=>{throw new Error('unavailable')})
 const qc=new QueryClient({defaultOptions:{mutations:{retry:false}}})
 render(<QueryClientProvider client={qc}><PasswordForm onDone={()=>{}}/></QueryClientProvider>)
 fireEvent.change(screen.getByLabelText('新密码'),{target:{value:'manual-password'}})
 fireEvent.click(screen.getByRole('button',{name:'随机生成'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('无法生成随机密码')
 expect(screen.getByLabelText('新密码')).toHaveValue('manual-password')
 expect(screen.getByLabelText('确认新密码')).toHaveValue('')
 expect(random).toHaveBeenCalledOnce()
})
it('retains password form values after incorrect current password',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify({error:{code:'credentials',message:'当前密码错误'}}),{status:401})))
 const onDone=vi.fn(),expired=vi.fn();window.addEventListener('session-expired',expired)
 const qc=new QueryClient({defaultOptions:{mutations:{retry:false}}})
 render(<QueryClientProvider client={qc}><PasswordForm onDone={onDone}/></QueryClientProvider>)
 fireEvent.change(screen.getByLabelText('当前密码'),{target:{value:'wrong'}})
 fireEvent.change(screen.getByLabelText('新密码'),{target:{value:'new-long-password'}})
 fireEvent.change(screen.getByLabelText('确认新密码'),{target:{value:'new-long-password'}})
 fireEvent.click(screen.getByText('保存新密码'))
 await screen.findByText('当前密码错误')
 expect(screen.getByLabelText('新密码')).toHaveValue('new-long-password')
 expect(onDone).not.toHaveBeenCalled();expect(expired).not.toHaveBeenCalled()
 window.removeEventListener('session-expired',expired);cleanup();vi.unstubAllGlobals()
})
