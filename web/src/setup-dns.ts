import {isStaticDemo} from './request'
export const DOH_PRESETS=[
 {id:'cloudflare',name:'Cloudflare DoH',url:'https://cloudflare-dns.com/dns-query'},
 {id:'google',name:'Google DoH',url:'https://dns.google/resolve'},
] as const
export type DoHProvider=typeof DOH_PRESETS[number]['id']
export type DNSQuery={name:string;resolver:string;addresses:string[];status:'pass'|'block';message:string}
export type DNSReport={verified:boolean;queries:DNSQuery[]}

function normalizeIP(value:string):string|null {
 if(/^(0|[1-9]\d{0,2})(\.(0|[1-9]\d{0,2})){3}$/.test(value))return value.split('.').every(p=>Number(p)<=255)?value:null
 if(!value.includes(':')||!/^[a-f0-9:.]+$/i.test(value))return null
 try{
  const ip=new URL(`http://[${value}]/`).hostname.slice(1,-1)
  // DNS may return the IPv4-mapped IPv6 form of the same address.
  const mapped=/^::ffff:([a-f0-9]{1,4}):([a-f0-9]{1,4})$/.exec(ip)
  if(mapped){const high=parseInt(mapped[1],16),low=parseInt(mapped[2],16);return [high>>8,high&255,low>>8,low&255].join('.')}
  return ip
 }catch{return null}
}

type DNSAnswer={name:string;type:number;data:string}
function normalizeDNSName(value:string):string {
 return value.toLowerCase().replace(/\.+$/,'')
}
function readAnswer(body:unknown,name:string,type:number):{addresses:string[];error:string} {
 if(!body||typeof body!=='object')throw new Error('DoH 返回格式无效，请重试或更换查询服务')
 const value=body as {Status?:number;TC?:boolean;Question?:{name:string;type:number}[];Answer?:DNSAnswer[]}
 const question=value.Question
 if(!Array.isArray(question)||question.length!==1||!question[0]||typeof question[0].name!=='string'||normalizeDNSName(question[0].name)!==normalizeDNSName(name)||question[0].type!==type||!Number.isInteger(value.Status)||value.TC)throw new Error('DoH 返回格式无效，请重试或更换查询服务')
 if(value.Status===3)return {addresses:[],error:'域名不存在，请检查通配符记录或等待缓存更新'}
 if(value.Status!==0)return {addresses:[],error:`DNS 服务返回错误（${value.Status}），请重试或更换查询服务`}
 if(value.Answer!==undefined&&!Array.isArray(value.Answer))throw new Error('DoH 返回格式无效，请重试或更换查询服务')
 const answers=value.Answer??[],names=new Set([normalizeDNSName(name)])
 if(answers.some(answer=>!answer||typeof answer.name!=='string'||typeof answer.type!=='number'||typeof answer.data!=='string'))throw new Error('DoH 返回格式无效，请重试或更换查询服务')
 // Accept addresses for the query or its returned CNAME chain, never unrelated answers.
 for(let i=0;i<answers.length;i++)for(const answer of answers){
  if(answer.type===5&&typeof answer.name==='string'&&typeof answer.data==='string'&&names.has(normalizeDNSName(answer.name)))names.add(normalizeDNSName(answer.data))
 }
 const addresses:string[]=[]
 for(const answer of answers){
  if(answer.type!==type||typeof answer.name!=='string'||!names.has(normalizeDNSName(answer.name)))continue
  if(!normalizeIP(answer.data)||(type===28)!==answer.data.includes(':'))throw new Error('DoH 返回地址格式无效，请重试或更换查询服务')
  addresses.push(normalizeIP(answer.data)!)
 }
 return {addresses,error:''}
}

export async function checkSetupDNS(domain:string,address:string,provider:DoHProvider):Promise<DNSReport> {
 const endpoint=DOH_PRESETS.find(p=>p.id===provider)
 const target=address?normalizeIP(address.trim()):null
 if(!endpoint||address&&!target)throw new Error('请填写有效的目标 IP 并选择 DoH 查询服务')
 const nonce=new Uint8Array(12)
 crypto.getRandomValues(nonce)
 const names=[`caddyadmin.${domain}.`,`setup-check-${Array.from(nonce,b=>b.toString(16).padStart(2,'0')).join('')}.${domain}.`]
 if(isStaticDemo)return {verified:true,queries:names.map(name=>({name,resolver:'浏览器本地模拟',addresses:target?[target]:['192.168.1.5'],status:'pass',message:'演示解析成功，未执行真实 DNS 查询'}))}
 const queries=await Promise.all(names.map(async name=>{
  const controller=new AbortController(),timer=setTimeout(()=>controller.abort(),6000)
  const query:DNSQuery={name,resolver:endpoint.url,addresses:[],status:'pass',message:target?'全部解析地址与目标 IP 一致':'解析成功（未核对目标 IP）'}
  try{
   const results=await Promise.allSettled([1,28].map(async type=>{
    const url=new URL(endpoint.url)
    url.searchParams.set('name',name);url.searchParams.set('type',type===1?'A':'AAAA')
    const response=await fetch(url.toString(),{headers:{Accept:'application/dns-json'},credentials:'omit',mode:'cors',redirect:'error',referrerPolicy:'no-referrer',cache:'no-store',signal:controller.signal})
    if(!response.ok)throw new Error(`DoH 请求失败（HTTP ${response.status}），请重试或更换查询服务`)
    return readAnswer(await response.json(),name,type)
   }))
   let failure=''
   for(const result of results){
    if(result.status==='fulfilled'){query.addresses.push(...result.value.addresses);failure ||= result.value.error}
    else if(controller.signal.aborted)failure='DoH 查询超时，请重试或更换查询服务'
    else failure ||= result.reason instanceof TypeError?'无法连接 DoH，请检查网络、跨域限制或更换查询服务':result.reason instanceof SyntaxError?'DoH 返回格式无效，请重试或更换查询服务':result.reason instanceof Error?result.reason.message:'DoH 查询失败，请重试或更换查询服务'
   }
   query.addresses=[...new Set(query.addresses)]
   if(!failure&&!query.addresses.length)failure='未返回 A/AAAA 地址，记录可能尚未配置或被解析器过滤；请核对记录或更换查询服务'
   if(!failure&&target&&query.addresses.some(ip=>ip!==target))failure='解析地址不符，请核对目标 IP 与已有 A/AAAA 记录'
   if(failure){query.status='block';query.message=failure}
  }finally{clearTimeout(timer)}
  return query
 }))
 return {verified:queries.every(q=>q.status==='pass'),queries}
}
