import {afterEach,expect,it} from 'vitest'
import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {CaddyConfigPreview} from './components/CaddyConfigPreview'
afterEach(cleanup)
const props={runtimeConfig:{apps:{http:{upstream:'10.0.0.8:80'}}},config:{apps:{http:{upstream:'10.0.0.9:80'}}},runtimeHash:'before-full-hash',hash:'after-full-hash',readAt:'2026-10-15T12:00:00Z',redactedFields:['/apps/tls/api_token'],runtimeRedactedFields:[]}
it('shows git-style changed lines, numbers and changed characters',()=>{
 const {container}=render(<CaddyConfigPreview {...props}/>)
 expect(screen.getByRole('tab',{name:'发布前对比'})).toHaveAttribute('aria-selected','true')
 expect(container.querySelector('.config-line-removed')).toHaveTextContent('10.0.0.8:80')
 expect(container.querySelector('.config-line-added')).toHaveTextContent('10.0.0.9:80')
 expect([...container.querySelectorAll('mark')].map(n=>n.textContent)).toEqual(['8','9'])
 expect(screen.getByText(/before-full-hash/)).toBeInTheDocument()
 fireEvent.click(screen.getByRole('button',{name:'统一视图'}))
 expect(container.querySelector('.config-diff-unified')).toBeInTheDocument()
})
it('switches between full running and candidate JSON and lists redaction locations',()=>{
 render(<CaddyConfigPreview {...props}/>)
 fireEvent.click(screen.getByRole('tab',{name:'当前运行配置'}))
 expect(screen.getByRole('tabpanel')).toHaveTextContent('10.0.0.8:80')
 expect(screen.getByRole('tabpanel')).not.toHaveTextContent('10.0.0.9:80')
 fireEvent.click(screen.getByRole('tab',{name:'待发布配置'}))
 expect(screen.getByRole('tabpanel')).toHaveTextContent('10.0.0.9:80')
 expect(screen.getByText('/apps/tls/api_token')).toBeInTheDocument()
 expect(screen.getByText(/不能直接用于恢复/)).toBeInTheDocument()
})
it('distinguishes identical visible JSON from different complete fingerprints',()=>{
 render(<CaddyConfigPreview {...props} config={props.runtimeConfig}/>)
 expect(screen.getByText(/可见配置相同，但完整配置指纹不同/)).toBeInTheDocument()
})
it('does not fabricate a running config if none was read',()=>{
 render(<CaddyConfigPreview {...props} runtimeConfig={undefined}/>)
 expect(screen.getByText(/未获取到当前运行配置/)).toBeInTheDocument()
 expect(screen.queryByText(/没有配置差异/)).not.toBeInTheDocument()
 fireEvent.click(screen.getByRole('tab',{name:'待发布配置'}))
 expect(screen.getByRole('tabpanel')).toHaveTextContent('10.0.0.9:80')
})
it('folds unchanged context and lets users expand it',()=>{
 const runtimeConfig=Object.fromEntries(Array.from({length:30},(_,i)=>[`field${String(i).padStart(2,'0')}`,i]))
 render(<CaddyConfigPreview {...props} runtimeConfig={runtimeConfig} config={{...runtimeConfig,field15:99}}/>)
 expect(screen.queryByText(/"field00":/)).not.toBeInTheDocument()
 fireEvent.click(screen.getByRole('checkbox',{name:'显示未修改行'}))
 expect(screen.getAllByText(/"field00":/)).toHaveLength(2)
})
