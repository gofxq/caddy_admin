export interface Service {id:string;name:string;group:'public'|'homelab';hostname:string;scheme:'http'|'https';host:string;port:number;enabled:boolean;notes:string;dial:string;updated_at:string}
export interface Session {username:string;csrf:string;must_change:boolean;expires:number}
export interface ServiceList {revision:number;services:Service[];published:Service[]}
export interface Change {kind:string;hostname:string;before?:Service;after?:Service}
export interface Deployment {id:string;version:number;revision:number;status:string;services:Service[];base_hash:string;hash:string;actor:string;created:string;finished:string;error:string;rollback_id:string;changes:Change[]}
export interface Preview {revision:number;services:Service[];changes:Change[];config:unknown;hash:string;runtime_hash:string;expected_hash:string;drift:boolean;validation_id:string;validation_expires_at:string;rollback_id:string;rollback_config?:unknown;rollback_hash?:string;config_redacted_fields?:string[];rollback_config_redacted_fields?:string[]}
export interface Overview {reachable:boolean;drift:boolean;runtime_hash:string;expected_hash:string;version:number;enabled:number;draft_revision:number;unpublished:boolean;message:string;recent:Deployment[];checked_at:string}
export interface CertificateStatus {mode:'bootstrap_internal'|'cloudflare';activation_status:'idle'|'applying'|'uncertain'|'failed'|'success';public_status:'unknown'|'pending'|'ready'|'error';last_error_class:string;updated_at:string}
export interface Settings {config:{origin:string;public_domain:string;homelab_domain:string;admin_domain:string;lan_cidrs:string[];upstream_cidrs:string[];allowed_names:string[];denied_ips:string[];resolvers:string[];test_tls:boolean};manager_version:string;caddy_version:string;cloudflare_module:boolean;token_configured:boolean;certificate_status:CertificateStatus;external_caddy:boolean}
export interface Certificate {subject:string;status:string;message:string;sans:string[];not_before:string;not_after:string;days:number;checked_at:string}
export interface Audit {id:number;time:string;actor:string;action:string;object:string;result:string;revision:number;version:number;rollback_id:string;error_class:string}
export interface Page<T>{items:T[];offset:number;limit:number}
export function upstream(s:Service){return `${s.scheme}://${s.host.includes(':')?`[${s.host}]`:s.host}:${s.port}`}
export function serviceState(s:Service,published:Service[]){const p=published.find(x=>x.id===s.id);if(!p)return '待首次发布';return (['name','group','hostname','scheme','host','port','enabled','notes','dial'] as const).some(k=>s[k]!==p[k])?'未发布修改':'已生效'}
export function filterServices(services:Service[],keyword:string,group:string,state:string){return services.filter(s=>(`${s.name} ${s.hostname} ${s.host}`.toLowerCase().includes(keyword.toLowerCase()))&&(!group||s.group===group)&&(!state||s.enabled===(state==='enabled')))}
export const statusLabel:Record<string,string>={success:'发布成功',failed:'发布失败',applying:'正在发布',uncertain:'待核对',valid:'证书有效',warning:'即将到期',invalid:'证书异常',unknown:'无法检测',added:'新增',deleted:'删除',updated:'修改',enabled:'启用',disabled:'停用'}
export function date(value:string){return value?new Date(value).toLocaleString('zh-CN',{hour12:false}):'—'}
