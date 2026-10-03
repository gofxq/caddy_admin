import {expect,it,vi} from 'vitest'
import {fireEvent,screen,waitFor,within} from '@testing-library/react'
import {QueryClient} from '@tanstack/react-query'
import ReactDOM from 'react-dom/client'

it('navigates all demo pages, edits and publishes a draft, checks its upstream, and logs in without a backend',async()=>{
 vi.stubEnv('MODE','demo');sessionStorage.clear()
 vi.stubGlobal('scrollTo',vi.fn())
 vi.stubGlobal('fetch',vi.fn().mockRejectedValue(new Error('A static site has no backend')))
 const mounted=vi.spyOn(QueryClient.prototype,'mount')
 const created=vi.spyOn(ReactDOM,'createRoot')
 document.body.innerHTML='<div id="root"></div>'
 window.history.replaceState({},'', '/')
 try{
  await import('./main')
  const nav=within(await screen.findByRole('navigation'))
  expect(screen.getByRole('region',{name:'静态演示模式'})).toBeInTheDocument()
  fireEvent.click(nav.getByRole('link',{name:'服务'}))
  fireEvent.click(await screen.findByRole('button',{name:'编辑 照片库'}))
  fireEvent.change(await screen.findByLabelText('服务名称'),{target:{value:'新的照片库'}})
  fireEvent.click(screen.getByRole('button',{name:'保存草稿'}))
  await waitFor(()=>expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  fireEvent.click(nav.getByRole('link',{name:'发布'}))
  fireEvent.click(await screen.findByRole('button',{name:'校验配置'}))
  await waitFor(()=>expect(screen.getByRole('button',{name:'确认发布'})).toBeEnabled())
  fireEvent.click(screen.getByRole('button',{name:'确认发布'}))
  fireEvent.click(await screen.findByRole('button',{name:'立即发布'}))
  expect(await screen.findByText('发布 v3 已模拟成功')).toBeInTheDocument()
  fireEvent.click(nav.getByRole('link',{name:'服务'}))
  fireEvent.click(await screen.findByRole('link',{name:'新的照片库'}))
  fireEvent.click(await screen.findByRole('button',{name:'检查已发布上游'}))
  expect(await screen.findByText('TCP 可连接')).toBeInTheDocument()
  for(const label of ['证书','审计','设置','概览']){
   fireEvent.click(nav.getByRole('link',{name:label}))
   expect(await screen.findByRole('heading',{level:1,name:label})).toBeInTheDocument()
  }
  fireEvent.click(screen.getByRole('button',{name:'登出'}))
  fireEvent.click(await screen.findByRole('button',{name:'登录控制台'}))
  expect(await screen.findByRole('navigation')).toBeInTheDocument()
  expect(fetch).not.toHaveBeenCalled()
 }finally{
  created.mock.results[0]?.value.unmount()
  ;(mounted.mock.instances[0] as QueryClient|undefined)?.clear()
  vi.restoreAllMocks();vi.unstubAllGlobals();vi.unstubAllEnvs();sessionStorage.clear()
 }
})
