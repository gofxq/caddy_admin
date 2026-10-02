import {useState} from 'react'
import {generatePassword} from '../password'

export function NewPasswordField({label,value,onChange,onGenerate,disabled=false}:{label:string;value:string;onChange:(value:string)=>void;onGenerate?:(value:string)=>void;disabled?:boolean}){
 const [visible,setVisible]=useState(false),[error,setError]=useState('')
 function generate(){
  try{
   const password=generatePassword()
   onChange(password);onGenerate?.(password);setVisible(true);setError('')
  }catch{setError('无法生成随机密码，请手动设置密码。')}
 }
 return <div className="new-password-field">
  <label>{label}<input aria-label={label} type={visible?'text':'password'} autoComplete="new-password" required minLength={8} maxLength={256} disabled={disabled} value={value} onChange={event=>onChange(event.target.value)}/></label>
  <div className="password-actions">
   <button type="button" className="button button-outline button-sm" disabled={disabled} onClick={generate}>随机生成</button>
   <button type="button" className="button button-ghost button-sm" disabled={disabled||!value} aria-pressed={visible} onClick={()=>setVisible(!visible)}>{visible?'隐藏密码':'显示密码'}</button>
  </div>
  <p className="muted">至少 8 个字符，最多 256 字节。随机生成 20 位密码，请保存后继续。</p>
  {error&&<p className="notice notice-red" role="alert">{error}</p>}
 </div>
}
