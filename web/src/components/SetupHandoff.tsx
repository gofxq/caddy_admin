import {useState} from 'react'
import {Waypoints} from 'lucide-react'

export type Handoff={initialized:boolean;mode:'embedded'|'external';admin_origin:string;manager_status:'ready'|'pending'|'error';dns_status:'ready'|'pending'|'error';console_status:'ready'|'pending'|'error';temporary_entry:boolean;checked_at:string}

export function SetupHandoff({adminOrigin,handoff,offline,externalCaddy,httpsEntry,testTLS,onRetry}:{adminOrigin:string;handoff:Handoff|null;offline:boolean;externalCaddy:boolean;httpsEntry:string;testTLS:boolean;onRetry:()=>void}){
 const [copyMessage,setCopyMessage]=useState('')
 const completed=handoff?.initialized===true||!!adminOrigin
 const ready=handoff?.console_status==='ready'
 async function copy(){
  try{if(!navigator.clipboard)throw new Error('clipboard unavailable');await navigator.clipboard.writeText(adminOrigin);setCopyMessage('地址已复制')}
  catch{setCopyMessage('复制失败，请手动复制上方正式入口地址。')}
 }
 return <div className="setup-screen"><section className="setup-card">
  <Waypoints size={38}/><h1>{completed?'初始化已完成':'正在核对初始化结果'}</h1>
  <p className="muted">{completed?'管理员与设置已保存。':'提交结果暂时未知，正在核对；请勿重复提交。'}正式入口：<strong>{adminOrigin||'等待交接服务返回地址'}</strong></p>
  {offline&&<div className="notice notice-amber">{location.protocol==='http:'?'暂时无法读取初始化结果，请通过下方 HTTPS 交接页面核对。':'暂时无法读取交接状态，已保留当前输入；服务恢复后会继续核对。'}</div>}
  {location.protocol==='http:'&&<p className="notice notice-amber">HTTP 会切换到 HTTPS；请打开 <a href={httpsEntry+'/setup'}>HTTPS 交接页面</a> 核对，临时证书可能显示信任提示。不要重复提交初始化。</p>}
  <dl>
   <div><dt>Manager</dt><dd>{handoff?.manager_status==='ready'?'服务已响应':handoff?.manager_status==='error'?'暂不可用，等待重试':'等待服务响应'}</dd></div>
   <div><dt>服务端 DNS</dt><dd>{handoff?.dns_status==='ready'?'已有解析结果':'尚未确认，可能受服务器解析器或网络影响'}</dd></div>
   <div><dt>HTTPS 证书与路由</dt><dd>{ready?'服务端探测通过':'尚未确认，请等待证书签发或检查路由'}</dd></div>
  </dl>
  {testTLS&&<p className="notice notice-amber">当前为测试 TLS，使用内部 CA，不代表公网可信证书。</p>}
  {handoff?.dns_status==='ready'&&!ready&&<p className="notice notice-amber">服务器已能解析域名，但可信 TLS 或控制台路由尚未就绪。</p>}
  <p className="muted">服务端探测通过不代表当前设备可达。请从本机打开正式控制台；访问失败时检查本机 DNS、LAN/VPN 路由和防火墙，证书异常时检查签发状态。</p>
  <div className="setup-actions">
   <button className="button button-outline" disabled={!adminOrigin} onClick={()=>void copy()}>复制地址</button>
   {location.protocol==='https:'&&<button className="button button-outline" onClick={onRetry}>重新核对状态</button>}
   {adminOrigin&&!externalCaddy&&handoff?.temporary_entry!==false&&<a className="button button-outline" href={httpsEntry+'/'}>通过临时入口继续登录</a>}
   {adminOrigin&&<a className="button button-primary" href={adminOrigin+'/'}>打开正式控制台</a>}
  </div>
  {copyMessage&&<p role="status" className="muted">{copyMessage}</p>}
 </section></div>
}
