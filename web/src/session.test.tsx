import {expect,it,vi} from 'vitest'
import {fireEvent,screen,waitFor} from '@testing-library/react'
import {focusManager,QueryClient} from '@tanstack/react-query'
import {api} from './api'

it('keeps console input on a background 503, then exits on unauthorized',async()=>{
 vi.stubGlobal('scrollTo',vi.fn())
 const mounted=vi.spyOn(QueryClient.prototype,'mount')
 let finishQuery!:(response:Response)=>void
 let finishMutation!:(response:Response)=>void
 const removed=vi.spyOn(QueryClient.prototype,'removeQueries')
 let sessionStatus=200
 let logoutStatus=503
 vi.stubGlobal('fetch',vi.fn(async(input:string)=>{
  if(input.endsWith('/late-query'))return new Promise<Response>(resolve=>{finishQuery=resolve})
  if(input.endsWith('/late-mutation'))return new Promise<Response>(resolve=>{finishMutation=resolve})
  if(input.endsWith('/auth/login')){sessionStatus=200;return new Response(JSON.stringify({username:'admin',csrf:'new-session',must_change:false}))}
  if(input.endsWith('/auth/logout'))return new Response(logoutStatus===204?'':JSON.stringify({error:{code:'unavailable',message:'登出暂不可用'}}),{status:logoutStatus})
  const data=input.endsWith('/auth/session')?(sessionStatus===200?{username:'admin',csrf:'test',must_change:false}:{error:{code:sessionStatus===401?'unauthorized':'unavailable',message:'暂不可用'}}):input.endsWith('/services')?{revision:0,services:[],published:[]}:{config:{domains:[],upstream_cidrs:[],allowed_names:[]}}
  return new Response(JSON.stringify(data),{status:input.endsWith('/auth/session')?sessionStatus:200})
 }))
 document.body.innerHTML='<div id="root"></div>'
 window.history.replaceState({},'', '/services')
 await import('./main')
 const input=await screen.findByLabelText('搜索服务')
 fireEvent.change(input,{target:{value:'unsaved search'}})
 fireEvent.click(screen.getByRole('button',{name:'登出'}))
 await screen.findByText('登出暂不可用')
 expect(screen.getByLabelText('搜索服务')).toHaveValue('unsaved search')
 const client=mounted.mock.instances[0] as QueryClient
 const lateQuery=client.fetchQuery({queryKey:['late-query'],queryFn:()=>api('/late-query')}).catch(()=>undefined)
 const lateMutation=client.getMutationCache().build(client,{mutationFn:()=>api('/late-mutation',{method:'POST',body:{}}),onSuccess:data=>{client.setQueryData(['preview'],data)}}).execute(undefined).catch(()=>undefined)
 await waitFor(()=>expect(finishMutation).toBeTypeOf('function'))
 client.setQueryData(['audit'],{entries:['protected']})
 sessionStatus=503
 focusManager.setFocused(false);focusManager.setFocused(true)
 await screen.findByText('暂不可用')
 expect(screen.getByLabelText('搜索服务')).toHaveValue('unsaved search')
 sessionStatus=401
 focusManager.setFocused(false);focusManager.setFocused(true)
 await waitFor(()=>expect(screen.queryByLabelText('搜索服务')).not.toBeInTheDocument())
 await screen.findByText('登录控制台')
 expect(client.getQueryData(['services'])).toBeUndefined()
 expect(client.getQueryData(['settings'])).toBeUndefined()
 expect(client.getMutationCache().getAll()).toHaveLength(0)
 expect(client.getQueryData(['audit'])).toBeUndefined()
 fireEvent.change(screen.getByLabelText('密码'),{target:{value:'new-password'}})
 fireEvent.click(screen.getByText('登录控制台'))
 await screen.findByLabelText('搜索服务')
 finishQuery(new Response(JSON.stringify({secret:'old query'})))
 finishMutation(new Response(JSON.stringify({secret:'old mutation'})))
 await Promise.all([lateQuery,lateMutation])
 expect(client.getQueryData(['late-query'])).toBeUndefined()
 expect(client.getQueryData(['preview'])).toBeUndefined()
 expect(client.getQueryData(['session'])).toMatchObject({csrf:'new-session'})
 mounted.mockRestore()
 removed.mockRestore()
 vi.unstubAllGlobals()
})
