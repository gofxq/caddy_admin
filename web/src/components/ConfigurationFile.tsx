import {useRef,useState} from 'react'
import {readConfiguration,type Configuration} from '../configuration'
export function ConfigurationFile({onLoad,onSelect,disabled=false}:{onLoad:(value:Configuration,name:string)=>void;onSelect?:()=>void;disabled?:boolean}){
 const [error,setError]=useState(''),[reading,setReading]=useState(false);const generation=useRef(0)
 return <><label>上传配置文件<input aria-label="上传配置文件" type="file" accept=".json,application/json" disabled={disabled||reading} onChange={event=>{const file=event.target.files?.[0];event.target.value='';if(!file)return;onSelect?.();const current=++generation.current;setError('');setReading(true);void readConfiguration(file).then(value=>{if(current===generation.current)onLoad(value,file.name)}).catch(e=>{if(current===generation.current)setError(e instanceof Error?e.message:'读取失败，请重试')}).finally(()=>{if(current===generation.current)setReading(false)})}}/></label>{reading&&<p role="status">正在读取配置…</p>}{error&&<p className="notice notice-red" role="alert">{error}</p>}</>
}
