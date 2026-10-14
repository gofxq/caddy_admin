import {useEffect} from 'react'
import {useRouter} from '@tanstack/react-router'

export const discardChangesMessage='有尚未保存的修改，确认放弃并离开？'

export function useUnsavedChanges(dirty:boolean){
 const router=useRouter({warn:false})
 useEffect(()=>{
  if(!dirty)return
  const beforeUnload=(event:BeforeUnloadEvent)=>{event.preventDefault();event.returnValue=''}
  window.addEventListener('beforeunload',beforeUnload)
  const unblock=router?.history.block({blockerFn:()=>!window.confirm(discardChangesMessage),enableBeforeUnload:false})
  return()=>{window.removeEventListener('beforeunload',beforeUnload);unblock?.()}
 },[dirty,router])
}
