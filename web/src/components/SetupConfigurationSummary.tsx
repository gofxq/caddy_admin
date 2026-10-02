type Policy={homelab_domain:string;lan_cidrs:string[];upstream_cidrs:string[];allowed_names:string[];denied_ips:string[];resolvers:string[]}

export function SetupConfigurationSummary({settings,adminDomain}:{settings:Policy;adminDomain:string}){
 const rows=[
  ['控制台',adminDomain||'待填写 Homelab 域名'],
  ['Homelab 域名',settings.homelab_domain.trim().toLowerCase().replace(/\.+$/,'')||'待填写'],
  ['Public 域名','尚未配置'],
  ['可信网络',settings.lan_cidrs.join('，')||'待填写'],
  ['上游网络',settings.upstream_cidrs.join('，')||'待填写'],
  ['允许的上游名称',settings.allowed_names.join('，')||'未额外允许'],
  ['拒绝的目标 IP',settings.denied_ips.join('，')||'无额外规则'],
  ['DNS 解析器',settings.resolvers.join('，')||'待填写'],
 ]
 return <section className="setup-summary" aria-label="配置摘要"><dl>{rows.map(([label,value])=><div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl></section>
}
