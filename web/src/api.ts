import {request} from './request'
let csrf=''
let sessionGeneration=0
export function setCSRF(value:string){if(csrf!==value)sessionGeneration++;csrf=value}
export class APIError extends Error {constructor(public status:number,public code:string,message:string,public requestID=''){super(message)}}
export async function api<T>(path:string,options:{method?:string;body?:unknown}={}):Promise<T>{
 const generation=sessionGeneration
 let response:Response
 try {response=await request(`/api/v1${path}`,{method:options.method??'GET',credentials:'same-origin',headers:{...(options.body!==undefined?{'Content-Type':'application/json'}:{}),...(options.method&&options.method!=='GET'?{'X-CSRF-Token':csrf}:{})},body:options.body===undefined?undefined:JSON.stringify(options.body)})}
 catch {throw new APIError(0,'network','网络连接失败，请重试')}
 let data:unknown
 try {const body=await response.text();data=body?JSON.parse(body):undefined} catch {data=undefined}
 // Neither a late success nor a late 401 may affect a newer session.
 if(generation!==sessionGeneration)throw new APIError(0,'session_changed','会话已变更，请重试')
 const error=(data&&typeof data==='object'&&'error' in data?data.error:null) as {code?:string;message?:string;request_id?:string}|null
 if(!response.ok){
  const code=typeof error?.code==='string'?error.code:'http_error'
  if(response.status===401&&code==='unauthorized'){sessionGeneration++;setCSRF('');window.dispatchEvent(new Event('session-expired'))}
  throw new APIError(response.status,code,typeof error?.message==='string'?error.message:`请求失败（HTTP ${response.status}），请重试`,error?.request_id??response.headers.get('X-Request-ID')??'')
 }
 if(data===undefined&&response.status!==204)throw new APIError(response.status,'invalid_response','服务响应无效，请重试')
 return data as T
}
