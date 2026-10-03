import {createMockApi,type DemoState} from './mock-api'

const storageKey='caddy-admin-static-demo-v1'
function restore(){
 try{
  const saved=sessionStorage.getItem(storageKey)
  if(saved)return createMockApi({staticDemo:true,state:JSON.parse(saved) as DemoState})
 }catch{/* Corrupt or unavailable browser storage starts a fresh demo. */}
 return createMockApi({staticDemo:true,setup:location.pathname==='/setup'})
}
let demo=restore()
function save(){try{sessionStorage.setItem(storageKey,JSON.stringify(demo.getState()))}catch{/* Keep operating in memory when storage is unavailable. */}}
export function resetDemo(setup=false){demo=createMockApi({staticDemo:true,setup});save()}

export function demoRequest(path:string,options:RequestInit={}):Response{
 let result:{status:number;body?:unknown}
 try{
  if(!path.startsWith('/api/v1/'))return new Response(JSON.stringify({error:{code:'not_found',message:'演示模式不执行外部请求'}}),{status:404,headers:{'Content-Type':'application/json'}})
  const body=typeof options.body==='string'?JSON.parse(options.body):undefined
  result=demo.handle(options.method??'GET',path.slice('/api/v1'.length),body)
  save()
 }catch{result={status:400,body:{error:{code:'bad_request',message:'演示请求格式无效'}}}}
 return new Response(result.status===204?null:JSON.stringify(result.body),{status:result.status,headers:{'Content-Type':'application/json; charset=utf-8'}})
}
