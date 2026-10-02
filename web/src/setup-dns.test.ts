import {afterEach,expect,it,vi} from 'vitest'
import {checkSetupDNS} from './setup-dns'

afterEach(()=>{vi.unstubAllGlobals();vi.useRealTimers()})

function dnsReply(url:string,address='192.168.2.6',status=0){
 const q=new URL(url),type=q.searchParams.get('type')==='AAAA'?28:1
 return new Response(JSON.stringify({Status:status,TC:false,Question:[{name:q.searchParams.get('name'),type}],Answer:type===1&&status===0&&address?[{name:q.searchParams.get('name'),type:1,TTL:300,data:address}]:[]}))
}

it('queries both families and wildcard through browser DoH without sending secrets',async()=>{
 const fetch=vi.fn().mockImplementation(async(url:string)=>dnsReply(url))
 vi.stubGlobal('fetch',fetch)
 const result=await checkSetupDNS('h.cmx.ee','192.168.2.6','cloudflare')
 expect(result.verified).toBe(true)
 expect(result.queries).toHaveLength(2)
 expect(result.queries[0]).toMatchObject({name:'caddyadmin.h.cmx.ee.',addresses:['192.168.2.6'],status:'pass'})
 expect(result.queries[1].name).toMatch(/^setup-check-[a-f0-9]+\.h\.cmx\.ee\.$/)
 expect(fetch).toHaveBeenCalledTimes(4)
 for(const [url,options] of fetch.mock.calls){
  expect(new URL(url).origin).toBe('https://cloudflare-dns.com')
  expect(new URL(url).searchParams.has('name')).toBe(true)
  expect(options).toMatchObject({credentials:'omit',referrerPolicy:'no-referrer',redirect:'error'})
  expect(options.body).toBeUndefined()
 }
})

it('uses the chosen Google endpoint and preserves mismatched wildcard addresses',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(url:string)=>dnsReply(url,new URL(url).searchParams.get('name')!.startsWith('caddyadmin.')?'192.168.2.6':'192.168.2.8')))
 const result=await checkSetupDNS('h.cmx.ee','192.168.2.6','google')
 expect(result.verified).toBe(false)
 expect(result.queries[1]).toMatchObject({resolver:'https://dns.google/resolve',addresses:['192.168.2.8'],status:'block'})
})

it('compares equivalent IPv6 representations and checks every returned address',async()=>{
 let extra=false
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(url:string)=>{
  const q=new URL(url),type=q.searchParams.get('type')==='AAAA'?28:1
  return new Response(JSON.stringify({Status:0,Question:[{name:q.searchParams.get('name'),type}],Answer:type===28?[{name:q.searchParams.get('name'),type:28,data:'fd12:0:0:0:0:0:0:6'},...(extra?[{name:q.searchParams.get('name'),type:28,data:'fd12::8'}]:[])]:[]}))
 }))
 expect((await checkSetupDNS('home.example.com','fd12::6','google')).verified).toBe(true)
 extra=true
 expect((await checkSetupDNS('home.example.com','fd12::6','google')).verified).toBe(false)
})

it.each([['NXDOMAIN',3,'域名不存在'],['empty',0,'未返回 A/AAAA 地址'],['SERVFAIL',2,'DNS 服务返回错误']])('blocks %s with an actionable result',async(_label,status,message)=>{
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(url:string)=>dnsReply(url,'',status)))
 const result=await checkSetupDNS('home.example.com','192.168.2.6','google')
 expect(result.verified).toBe(false)
 expect(result.queries.every(q=>q.message.includes(message))).toBe(true)
})

it.each([
 ['HTTP',()=>new Response('',{status:503})],
 ['malformed',()=>new Response('{}')],
 ['network',()=>Promise.reject(new TypeError('Failed to fetch'))],
])('keeps all query results after a %s failure',async(_label,reply)=>{
 vi.stubGlobal('fetch',vi.fn().mockImplementation(reply))
 const result=await checkSetupDNS('home.example.com','192.168.2.6','google')
 expect(result.verified).toBe(false)
 expect(result.queries).toHaveLength(2)
 expect(result.queries.every(q=>q.status==='block')).toBe(true)
})

it('never accepts a successful A response when the AAAA lookup fails',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(url:string)=>new URL(url).searchParams.get('type')==='AAAA'?new Response('',{status:503}):dnsReply(url)))
 const result=await checkSetupDNS('home.example.com','192.168.2.6','google')
 expect(result.verified).toBe(false)
 expect(result.queries[0].addresses).toEqual(['192.168.2.6'])
})

it('rejects a reply with the wrong address family',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async(url:string)=>dnsReply(url,'fd12::6')))
 const result=await checkSetupDNS('home.example.com','fd12::6','google')
 expect(result.verified).toBe(false)
 expect(result.queries[0].message).toContain('格式无效')
})

it('rejects malformed question and answer records without exposing raw exceptions',async()=>{
 vi.stubGlobal('fetch',vi.fn().mockImplementation(async()=>new Response(JSON.stringify({Status:0,Question:[null],Answer:[null]}))))
 const result=await checkSetupDNS('home.example.com','192.168.2.6','google')
 expect(result.verified).toBe(false)
 expect(result.queries[0].message).toContain('格式无效')
 expect(result.queries[0].message).not.toContain('properties')
})

it('times out pending browser requests',async()=>{
 vi.useFakeTimers()
 vi.stubGlobal('fetch',vi.fn().mockImplementation((_url:string,options:RequestInit)=>new Promise((_resolve,reject)=>options.signal!.addEventListener('abort',()=>reject(new DOMException('Aborted','AbortError'))))))
 const pending=checkSetupDNS('home.example.com','192.168.2.6','google')
 await vi.advanceTimersByTimeAsync(6000)
 const result=await pending
 expect(result.verified).toBe(false)
 expect(result.queries[0].message).toContain('超时')
})
