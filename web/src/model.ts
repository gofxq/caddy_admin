export interface Service {id:string;name:string;domain_id:string;hostname:string;scheme:'http'|'https';host:string;port:number;enabled:boolean;notes:string;dial:string;updated_at:string}
export interface Session {username:string;csrf:string;must_change:boolean;expires:number}
export interface ServiceList {revision:number;services:Service[];published:Service[]}
export interface Change {kind:string;hostname:string;before?:Service;after?:Service}
export interface Deployment {id:string;version:number;revision:number;status:string;services:Service[];base_hash:string;hash:string;actor:string;created:string;finished:string;error:string;rollback_id:string;changes:Change[]}
export interface Preview {settings:ManagedSettings;active_settings:ManagedSettings;settings_changed:boolean;revision:number;services:Service[];changes:Change[];config:unknown;hash:string;runtime_hash:string;expected_hash:string;drift:boolean;validation_id:string;validation_expires_at:string;rollback_id:string;rollback_config?:unknown;rollback_hash?:string;config_redacted_fields?:string[];rollback_config_redacted_fields?:string[]}
export interface Overview {reachable:boolean;drift:boolean;runtime_hash:string;expected_hash:string;version:number;enabled:number;draft_revision:number;unpublished:boolean;message:string;recent:Deployment[];checked_at:string}
export interface CertificateStatus {mode:'bootstrap_internal'|'cloudflare';activation_status:'idle'|'applying'|'uncertain'|'failed'|'success';public_status:'unknown'|'pending'|'ready'|'error';last_error_class:string;updated_at:string}
export interface ManagedDomain {id:string;name:string;access:'trusted'|'internet'|null}
export interface ManagedSettings {metrics_enabled?:boolean;access_logs_enabled?:boolean;alerts_enabled?:boolean;upstream_checks_enabled?:boolean;origin:string;domains:ManagedDomain[];admin_domain:string;previous_admin_domain?:string;previous_origin?:string;console_lan_only:boolean;lan_cidrs:string[];upstream_cidrs:string[];allowed_names:string[];denied_ips:string[];resolvers:string[]}
export interface Settings {config:ManagedSettings&{test_tls:boolean};active_config:ManagedSettings;revision:number;manager_version:string;caddy_version:string;cloudflare_module:boolean;token_configured:boolean;certificate_status:CertificateStatus;external_caddy:boolean}
export function accessLabel(access:ManagedDomain['access']){return access==='trusted'?'仅可信网络':access==='internet'?'允许互联网访问':'待配置'}
export function exposureChanges(before:ManagedSettings,after:ManagedSettings){return before.console_lan_only&&!after.console_lan_only||after.domains.some(d=>d.access==='internet'&&before.domains.find(x=>x.id===d.id)?.access!=='internet')||(before.console_lan_only||before.domains.some(d=>d.access==='trusted'))&&after.lan_cidrs.some(c=>!before.lan_cidrs.includes(c))}
export interface Certificate {subject:string;status:string;message:string;sans:string[];not_before:string;not_after:string;days:number;checked_at:string}
export interface Audit {id:number;time:string;actor:string;action:string;object:string;result:string;revision:number;version:number;rollback_id:string;error_class:string}
export interface Page<T>{items:T[];offset:number;limit:number}
export function upstream(s:Service){return `${s.scheme}://${s.host.includes(':')?`[${s.host}]`:s.host}:${s.port}`}
export function serviceState(s:Service,published:Service[]){const p=published.find(x=>x.id===s.id);if(!p)return '待首次发布';return (['name','domain_id','hostname','scheme','host','port','enabled','notes','dial'] as const).some(k=>s[k]!==p[k])?'未发布修改':'已生效'}
export function filterServices(services:Service[],keyword:string,domainID:string,state:string){return services.filter(s=>(`${s.name} ${s.hostname} ${s.host}`.toLowerCase().includes(keyword.toLowerCase()))&&(!domainID||s.domain_id===domainID)&&(!state||s.enabled===(state==='enabled')))}
export const statusLabel:Record<string,string>={success:'发布成功',failed:'发布失败',applying:'正在发布',uncertain:'待核对',valid:'证书有效',warning:'即将到期',invalid:'证书异常',unknown:'无法检测',added:'新增',deleted:'删除',updated:'修改',enabled:'启用',disabled:'停用'}
export function date(value:string){return value?new Date(value).toLocaleString('zh-CN',{hour12:false}):'—'}

export interface RuntimeCheck {deployment_id:string;version:number;status:'matched'|'drift'|'unknown'|'pending';reachable:boolean;expected_hash:string;runtime_hash:string;checked_at:string;message:string}
export interface ServiceRelease {id:string;version:number;status:string;created:string;finished:string;rollback_id:string}
export interface ServiceDetail {id:string;revision:number;draft:Service|null;published:Service|null;draft_domain:ManagedDomain|null;published_domain:ManagedDomain|null;runtime:RuntimeCheck;recent_deployments:ServiceRelease[];external_caddy:boolean}
export interface UpstreamCheck {deployment_id:string;status:'reachable'|'unreachable'|'unknown';target:string;duration_ms:number;checked_at:string;expected_hash:string;vantage:'manager';message:string}

export interface TrafficStats {requests:number;request_bytes:number;response_bytes:number;five_xx:number;statuses:Record<string,number>;p50:number|null;p95:number|null;p99:number|null;first_byte_p95:number|null;quantile_lower_bound:boolean}
export interface TrafficPoint extends TrafficStats {time:number}
export interface TrafficData {from:number;to:number;step:number;coverage:number;complete:boolean;summary:TrafficStats;points:TrafficPoint[];services:{id:string;hostname:string}[]}
export interface ObservationStatus {state:'disabled'|'baseline'|'ready'|'unavailable';message:string;last_attempt:string;last_sample:string;logs_state:string;log_gap:boolean;dropped_logs:number;interval_seconds:number;external_caddy:boolean}
export interface AccessEvent {epoch:string;sequence:number;time:string;kind:'access'|'diagnostic';service_id?:string;hostname?:string;method?:string;status?:number;path?:string;ip?:string;duration_seconds?:number;request_bytes?:number;response_bytes?:number;class?:string}
export interface Alert {id:string;service_id:string;kind:string;status:'active'|'unknown'|'resolved'|'disabled';message:string;first_seen:string;last_seen:string;resolved_at:string;acknowledged:boolean}
export interface Diagnostics {domain_id:string;checked_at:string;vantage:'manager';checks:{name:string;status:'pass'|'fail'|'unknown';message:string;values:string[]}[];events:AccessEvent[]}

export interface PortalService {name:string;hostname:string;url:string}
