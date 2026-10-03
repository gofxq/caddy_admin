import type {Change,Service,Settings} from './model.ts'
export const MAX_CONFIGURATION_BYTES=60*1024
export type PortableService=Pick<Service,'name'|'domain_id'|'hostname'|'scheme'|'host'|'port'|'enabled'|'notes'>
export type Configuration={format:'caddy-web-admin';version:2;settings:Omit<Settings['config'],'test_tls'>;services:PortableService[]}
export type ConfigurationPreview={revision:number;services:Service[];changes:Change[]}
const invalid=()=>new Error('配置文件格式无效；请上传本项目导出的 JSON 文件。')
function record(value:unknown,keys:string[]):Record<string,unknown>{if(!value||typeof value!=='object'||Array.isArray(value))throw invalid();const v=value as Record<string,unknown>;if(Object.keys(v).length!==keys.length||keys.some(k=>!(k in v))||Object.keys(v).some(k=>!keys.includes(k)))throw invalid();return v}
export function parseConfiguration(raw:string):Configuration{
 if(new TextEncoder().encode(raw).byteLength>MAX_CONFIGURATION_BYTES)throw new Error('配置文件不得超过 60 KiB。')
 let decoded:unknown;try{decoded=JSON.parse(raw)}catch{throw invalid()}
 const value=record(decoded,['format','version','settings','services'])
 if(value.format!=='caddy-web-admin'||value.version!==2)throw new Error('不支持的配置文件格式或版本。')
 const strings=['origin','admin_domain'],arrays=['lan_cidrs','upstream_cidrs','allowed_names','denied_ips','resolvers']
 const optional=['previous_admin_domain','previous_origin'].filter(k=>value.settings&&typeof value.settings==='object'&&k in value.settings)
 const settings=record(value.settings,[...strings,...arrays,'domains','console_lan_only',...optional])
 if([...strings,...optional].some(k=>typeof settings[k]!=='string')||arrays.some(k=>!Array.isArray(settings[k])||(settings[k] as unknown[]).some(v=>typeof v!=='string')))throw invalid()
 if(typeof settings.console_lan_only!=='boolean'||!Array.isArray(settings.domains))throw invalid()
 for(const item of settings.domains){const d=record(item,['id','name','access']);if(typeof d.id!=='string'||typeof d.name!=='string'||!(d.access===null||d.access==='trusted'||d.access==='internet'))throw invalid()}
 if(!Array.isArray(value.services))throw invalid()
 for(const item of value.services){const service=record(item,['name','domain_id','hostname','scheme','host','port','enabled','notes']);if(['name','hostname','host','notes'].some(k=>typeof service[k]!=='string')||typeof service.domain_id!=='string'||!['http','https'].includes(service.scheme as string)||typeof service.enabled!=='boolean'||!Number.isInteger(service.port)||(service.port as number)<1||(service.port as number)>65535)throw invalid()}
 return value as unknown as Configuration
}
export function setupConfiguration(value:Configuration){const s=value.settings;if(s.previous_admin_domain||s.previous_origin)throw new Error('控制台交接尚未完成，不能通过初始化导入；请完成交接后重新导出。');const first=s.domains.find(d=>s.admin_domain===`caddyadmin.${d.name}`);if(!first||s.origin!==`https://${s.admin_domain}`)throw new Error('该文件的自定义控制台地址暂不支持通过初始化导入；请使用标准控制台地址。');return {domain:first.name,resolvers:s.resolvers}}
export async function readConfiguration(file:File):Promise<Configuration>{if(file.size>MAX_CONFIGURATION_BYTES)throw new Error('配置文件不得超过 60 KiB。');const raw=await new Promise<string>((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result));reader.onerror=()=>reject(new Error('无法读取配置文件，请重新选择。'));reader.readAsText(file)});return parseConfiguration(raw)}
export function downloadConfiguration(value:Configuration){const raw=JSON.stringify(value);parseConfiguration(raw);const url=URL.createObjectURL(new Blob([raw],{type:'application/json'}));try{const link=document.createElement('a');link.href=url;link.download=`caddy-admin-config-${new Date().toISOString().replace(/[:.]/g,'-')}.json`;link.click()}finally{URL.revokeObjectURL(url)}}
