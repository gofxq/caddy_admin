import type {Service} from './model'

const siteRoot=/^\/srv\/sites(?:\/[a-z0-9_-][a-z0-9._-]*)+$/i
const apiPath=/^(?:\/[a-z0-9_-][a-z0-9._-]*)+\/\*$/i

export const noPrimaryUpstream={scheme:'http',host:'',port:80,dial:''} as const

export function quickCaddyfile(service:Service):{source:string;needsReview:boolean}{
 const site=service.static_site
 const address=service.host.includes(':')?`[${service.host}]`:service.host
 const primary=!site||!!site.api_path
 const validUpstream=/^(?:[a-z0-9.-]+|\[[a-f0-9:.]+\])$/i.test(address)&&Number.isInteger(service.port)&&service.port>0&&service.port<=65535
 const proxy=primary&&validUpstream
  ?service.scheme==='grpcs'
   ?`reverse_proxy https://${address}:${service.port} {\n    transport http {\n        versions 2\n    }\n}`
   :`reverse_proxy ${service.scheme}://${address}:${service.port}`
  :''
 const validSite=!site||siteRoot.test(site.root)&&(!site.api_path||apiPath.test(site.api_path))
 const needsReview=!!service.advanced&&Object.keys(service.advanced).length>0||!validSite||primary&&!validUpstream&&!!service.host
 if(!site)return {source:proxy,needsReview}
 if(!validSite||site.api_path&&!proxy)return {source:'',needsReview:true}
 const files=`root * ${site.root}\ntry_files {path} {path}/${(site.spa_fallback??!!site.api_path)?' /index.html':''}\nfile_server`
 if(!site.api_path)return {source:files,needsReview}
 const indent=(body:string)=>body.split('\n').map(line=>'    '+line).join('\n')
 return {source:`handle ${site.api_path} {\n${indent(proxy)}\n}\nhandle {\n${indent(files)}\n}`,needsReview}
}
