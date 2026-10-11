import {expect,it,vi} from 'vitest'
import {screen} from '@testing-library/react'
import {QueryClient} from '@tanstack/react-query'
import ReactDOM from 'react-dom/client'

it('opens /portal anonymously outside the management shell without a session request',async()=>{
 vi.stubGlobal('scrollTo',vi.fn())
 const fetch=vi.fn(async(path:string)=>new Response(JSON.stringify(path==='/api/v1/portal'?{services:[{name:'照片库',hostname:'photos.example.com',url:'https://photos.example.com'}]}:{error:{code:'unauthorized',message:'请先登录'}}),{status:path==='/api/v1/portal'?200:401}))
 vi.stubGlobal('fetch',fetch)
 const mounted=vi.spyOn(QueryClient.prototype,'mount'),created=vi.spyOn(ReactDOM,'createRoot')
 document.body.innerHTML='<div id="root"></div>';window.history.replaceState({},'', '/portal')
 try{
  await import('./main')
  expect(await screen.findByRole('heading',{level:1,name:'服务导览'})).toBeInTheDocument()
  expect(await screen.findByRole('link',{name:/照片库/})).toHaveAttribute('href','https://photos.example.com')
  expect(screen.getByRole('link',{name:/管理控制台/})).toHaveAttribute('href','/')
  expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
  expect(fetch.mock.calls.every(([path])=>path==='/api/v1/portal')).toBe(true)
 }finally{
  created.mock.results[0]?.value.unmount();(mounted.mock.instances[0] as QueryClient|undefined)?.clear()
  vi.restoreAllMocks();vi.unstubAllGlobals();window.history.replaceState({},'', '/')
 }
})
