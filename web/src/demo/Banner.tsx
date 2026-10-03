import {useState} from 'react'
import {Dialog} from '../components/ui/dialog'
import {Button} from '../components/ui/button'
import {resetDemo} from './runtime'
import {api} from '../api'
import type {ServiceList} from '../model'

export function DemoBanner(){
 const [action,setAction]=useState<'reset'|'setup'|null>(null)
 const [adding,setAdding]=useState(false),[message,setMessage]=useState('')
 async function addServices(){
  setAdding(true);setMessage('')
  try{
   const list=await api<ServiceList>('/services')
   const result=await api<{added:number}>('/demo/services',{method:'POST',body:{revision:list.revision}})
   setMessage(result.added?`已添加 ${result.added} 个演示服务草稿。`:'演示服务已经存在，无需重复添加。')
   if(result.added)location.assign('/services')
  }catch(error){setMessage(error instanceof Error?error.message:'添加失败，请重试')}
  finally{setAdding(false)}
 }
 return <><div className="demo-banner" role="region" aria-label="静态演示模式"><div><strong>静态演示</strong><span>操作仅保存在当前标签页，配置均模拟成功，不会影响真实服务。</span>{message&&<span role="status">{message}</span>}</div><div className="demo-banner-actions">{location.pathname!=='/setup'&&<Button variant="outline" size="sm" disabled={adding} onClick={()=>void addServices()}>{adding?'正在添加…':'添加演示服务'}</Button>}<Button variant="outline" size="sm" disabled={adding} onClick={()=>setAction('setup')}>体验初始化</Button><Button variant="outline" size="sm" disabled={adding} onClick={()=>setAction('reset')}>重置演示</Button></div></div><Dialog open={action!==null} onOpenChange={open=>{if(!open)setAction(null)}} title={action==='setup'?'体验初始化':'重置演示'}><p>{action==='setup'?'将清除当前标签页的演示数据，从空实例体验初始化流程。':'将清除当前标签页的修改，恢复预置服务与发布记录。'}</p><div className="dialog-actions"><Button variant="outline" onClick={()=>setAction(null)}>取消</Button><Button onClick={()=>{resetDemo(action==='setup');location.assign(action==='setup'?'/setup':'/')}}>确认</Button></div></Dialog></>
}
