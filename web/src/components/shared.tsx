import type { ReactNode } from 'react'
import {APIError} from '../api'
import { AlertTriangle, ArrowRight, RefreshCw, Inbox } from 'lucide-react'
import { Button } from './ui/button'
import { date, statusLabel, type Deployment } from '../model'
export function Badge({children,tone='neutral'}:{children:ReactNode;tone?:string}){return <span className={`badge badge-${tone}`}>{children}</span>}
export function Status({status}:{status:string}){return <Badge tone={status==='success'||status==='valid'?'green':status==='failed'||status==='invalid'?'red':'amber'}>{statusLabel[status]??status}</Badge>}
function errorGuidance(error:unknown){
 if(!(error instanceof APIError))return null
 switch(error.code){
  case 'network':case 'http_error':case 'invalid_response':case 'unavailable':case 'system_resolution':return {impact:'当前页面无法确认请求结果；已有数据不会被页面自动覆盖。',next:'检查 Caddy、数据库和网络连接，核对实际状态后再重试。'}
  case 'conflict':return {impact:'其他操作已改变当前状态，本次操作没有按旧状态继续。',next:'刷新页面并重新核对变更，再重新提交。'}
  case 'validation':return {impact:'输入未通过服务端校验，当前表单内容仍会保留。',next:'按上方原因修正输入后再次提交。'}
  case 'rate_limited':return {impact:'系统暂时拒绝新的请求。',next:'等待提示的时间后再试，不要连续提交。'}
  case 'credentials':return {impact:'凭据未通过验证，当前会话不会因此自动退出。',next:'检查当前密码或登录信息后再试。'}
  default:return null
 }
}
export function ErrorBox({error,onRetry,retryLabel='重试'}:{error:unknown;onRetry?:()=>void;retryLabel?:string}){if(!error)return null;const guidance=errorGuidance(error);return <div role="alert" className="notice notice-red error-box"><AlertTriangle size={18}/><div><span>{error instanceof Error?error.message:String(error)}</span>{guidance&&<><small>{guidance.impact}</small><small>下一步：{guidance.next}</small></>}{error instanceof APIError&&error.requestID&&<small>请求编号：{error.requestID}</small>}</div>{onRetry&&<Button variant="outline" size="sm" onClick={onRetry}>{retryLabel}</Button>}</div>}
export function Loading(){return <div className="empty"><RefreshCw className="spin" size={24}/><p>正在读取数据…</p></div>}
export function Empty({title,children,action}:{title:string;children?:ReactNode;action?:ReactNode}){return <div className="empty"><Inbox size={30}/><h3>{title}</h3>{children&&<p>{children}</p>}{action}</div>}
export function Heading({title,description,action}:{title:string;description:string;action?:ReactNode}){return <header className="page-heading"><div><h1>{title}</h1><p>{description}</p></div>{action}</header>}
export function History({items,onRollback,onDetail}:{items:Deployment[];onRollback?:(id:string)=>void;onDetail?:(id:string)=>void}){return items.length?<div className="table-wrap"><table><thead><tr><th>版本 / 时间</th><th>变更</th><th>操作者</th><th>状态</th><th/></tr></thead><tbody>{items.map(d=><tr key={d.id}><td><strong>v{d.version}</strong>{d.rollback_id&&<Badge>回滚</Badge>}<small>{date(d.created)}</small></td><td>{d.changes.length} 项变更<small className="text-danger">{d.error}</small></td><td>{d.actor}</td><td><Status status={d.status}/></td><td><div className="row-actions">{onDetail&&<Button variant="ghost" size="sm" onClick={()=>onDetail(d.id)}>详情 <ArrowRight size={14}/></Button>}{d.status==='success'&&onRollback&&<Button variant="outline" size="sm" onClick={()=>onRollback(d.id)}>回滚</Button>}</div></td></tr>)}</tbody></table></div>:<Empty title="还没有发布记录">保存服务草稿后，前往预览与校验开始首次发布。</Empty>}
export function Pager({offset,count,onChange}:{offset:number;count:number;onChange:(v:number)=>void}){return <div className="pager"><span>第 {offset/20+1} 页</span><Button variant="outline" size="sm" disabled={offset===0} onClick={()=>onChange(Math.max(0,offset-20))}>上一页</Button><Button variant="outline" size="sm" disabled={count<20} onClick={()=>onChange(offset+20)}>下一页</Button></div>}
