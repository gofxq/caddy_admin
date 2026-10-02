import {afterEach,expect,it,vi} from 'vitest'
import {api,APIError,setCSRF} from './api'
afterEach(()=>vi.restoreAllMocks())
it('keeps the session for wrong current credentials',async()=>{vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({error:{code:'credentials',message:'当前密码错误'}}),{status:401})));const listener=vi.fn();window.addEventListener('session-expired',listener);await expect(api('/auth/password',{method:'POST',body:{}})).rejects.toMatchObject({status:401,code:'credentials'});expect(listener).not.toHaveBeenCalled();window.removeEventListener('session-expired',listener)})
it('expires an unauthorized session',async()=>{vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(JSON.stringify({error:{code:'unauthorized'}}),{status:401})));const listener=vi.fn();window.addEventListener('session-expired',listener);await expect(api('/services')).rejects.toBeInstanceOf(APIError);expect(listener).toHaveBeenCalledOnce();window.removeEventListener('session-expired',listener)})
it.each(['','<html>private proxy diagnostics</html>'])('preserves HTTP status without exposing a proxy body',async body=>{vi.stubGlobal('fetch',vi.fn().mockResolvedValue(new Response(body,{status:502})));await expect(api('/services')).rejects.toMatchObject({status:502,code:'http_error'});await expect(api('/services')).rejects.not.toThrow('private proxy')})
it('normalizes network errors',async()=>{vi.stubGlobal('fetch',vi.fn().mockRejectedValue(new Error('private network detail')));await expect(api('/services')).rejects.toMatchObject({status:0,code:'network'})})

it.each([200,401])('ignores a late response from an expired session (%s)',async status=>{
 setCSRF('old-session')
 let finish!:(response:Response)=>void
 vi.stubGlobal('fetch',vi.fn().mockImplementationOnce(()=>new Promise<Response>(resolve=>{finish=resolve})).mockResolvedValueOnce(new Response(JSON.stringify({error:{code:'unauthorized'}}),{status:401})))
 const late=api('/draft/validate',{method:'POST',body:{}})
 const rejected=expect(late).rejects.toMatchObject({code:'session_changed'})
 await expect(api('/services')).rejects.toMatchObject({code:'unauthorized'})
 setCSRF('new-session')
 const listener=vi.fn()
 window.addEventListener('session-expired',listener)
 finish(new Response(JSON.stringify(status===200?{revision:1}:{error:{code:'unauthorized'}}),{status}))
 await rejected
 expect(listener).not.toHaveBeenCalled()
 window.removeEventListener('session-expired',listener)
})
