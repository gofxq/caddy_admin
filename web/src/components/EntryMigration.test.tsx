import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,render,screen,waitFor} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {EntryMigration} from './EntryMigration'
import {api} from '../api'
vi.mock('../api',()=>({api:vi.fn()}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
it('offers migration from any management page only after trusted TLS is ready',async()=>{
 const qc=new QueryClient({defaultOptions:{queries:{retry:false}}})
 const settings={config:{origin:'https://caddyadmin.home.example.com'},external_caddy:true,certificate_status:{public_status:'pending'}}
 vi.mocked(api).mockImplementation(async path=>path==='/settings'?settings:{reachable:true})
 render(<QueryClientProvider client={qc}><EntryMigration/></QueryClientProvider>)
 await waitFor(()=>expect(qc.getQueryData(['overview'])).toEqual({reachable:true}))
 expect(screen.queryByRole('link',{name:'打开正式入口'})).not.toBeInTheDocument()
 qc.setQueryData(['settings'],{...settings,certificate_status:{public_status:'ready'}})
 expect(await screen.findByRole('link',{name:'打开正式入口'})).toHaveAttribute('href','https://caddyadmin.home.example.com')
})
