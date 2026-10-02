import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {APIError} from '../api'
import {ErrorBox} from './shared'

afterEach(cleanup)

it('explains the impact of an unavailable request and offers retry',()=>{
 const retry=vi.fn()
 render(<ErrorBox error={new APIError(503,'unavailable','操作暂不可用','req-1')} onRetry={retry}/>)
 expect(screen.getByText('操作暂不可用')).toBeInTheDocument()
 expect(screen.getByText(/当前页面无法确认请求结果/)).toBeInTheDocument()
 expect(screen.getByText(/检查 Caddy、数据库和网络连接/)).toBeInTheDocument()
 expect(screen.getByText(/请求编号：req-1/)).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'重试'}))
 expect(retry).toHaveBeenCalledOnce()
})

it('tells a conflicted editor to refresh without suggesting a blind retry',()=>{
 render(<ErrorBox error={new APIError(409,'conflict','草稿已被修改，请刷新')}/>)
 expect(screen.getByText(/其他操作已改变当前状态/)).toBeInTheDocument()
 expect(screen.getByText(/刷新页面并重新核对/)).toBeInTheDocument()
 expect(screen.queryByRole('button',{name:'重试'})).not.toBeInTheDocument()
})
