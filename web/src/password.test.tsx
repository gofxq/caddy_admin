import {expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {PasswordForm} from './pages/Login'
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
