import {afterEach,expect,it,vi} from 'vitest'
import {cleanup,fireEvent,render,screen} from '@testing-library/react'
import {QueryClient,QueryClientProvider} from '@tanstack/react-query'
import {Audit} from './pages/Audit'
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
it('opens the immutable draft revision attached to a configuration import',async()=>{const fetch=vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({items:[{id:1,time:'2026-10-02T00:00:00Z',actor:'admin',action:'configuration.import',object:'draft',result:'success',revision:8,version:0}],offset:0,limit:20}))).mockResolvedValueOnce(new Response(JSON.stringify({revision:8,services:[]})));vi.stubGlobal('fetch',fetch);const qc=new QueryClient({defaultOptions:{queries:{retry:false}}});render(<QueryClientProvider client={qc}><Audit/></QueryClientProvider>);fireEvent.click(await screen.findByRole('button',{name:'草稿 r8'}));expect(await screen.findByRole('heading',{name:'历史草稿 r8'})).toBeInTheDocument();expect(fetch.mock.calls[1][0]).toBe('/api/v1/draft/revisions/8')})
