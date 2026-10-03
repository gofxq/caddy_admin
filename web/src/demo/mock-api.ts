import {parseConfiguration} from "../configuration.ts";
import {validNewPassword} from "../password.ts";
import {exposureChanges,type ManagedSettings} from "../model.ts";
import type {
  Audit,
  Change,
  Deployment,
  Preview,
  Service,
  CertificateStatus,
} from "../model.ts";

type Result = { status: number; body?: unknown };
const ok = (body: unknown): Result => ({ status: 200, body });
const fail = (status: number, code: string, message: string): Result => ({
  status,
  body: { error: { code, message } },
});
const now = () => new Date().toISOString();
const day = (offset: number) =>
  new Date(Date.now() + offset * 86400000).toISOString();
// A deterministic demo fingerprint, never used for production validation.
const hash = (value: unknown) => {
  let result = 2166136261;
  for (const char of JSON.stringify(value)) result = Math.imul(result ^ char.charCodeAt(0), 16777619);
  return "demo-" + (result >>> 0).toString(16).padStart(8, "0");
};
const randomUUID = () => globalThis.crypto.randomUUID();
const clone = <T>(value: T): T => structuredClone(value);
const page = <T>(items: T[], url: URL) => {
  const offset = Math.max(0, Number(url.searchParams.get("offset")) || 0);
  return { items: items.slice(offset, offset + 20), offset, limit: 20 };
};

const photos: Service = {
  id: "photos",
  name: "照片库",
  domain_id: "home",
  hostname: "photos.home.example.com",
  scheme: "http",
  host: "10.77.0.8",
  port: 2283,
  enabled: true,
  notes: "家庭照片备份",
  dial: "10.77.0.8:2283",
  updated_at: day(-8),
};
const grafana: Service = {
  id: "grafana",
  name: "监控面板",
  domain_id: "home",
  hostname: "grafana.home.example.com",
  scheme: "http",
  host: "10.77.0.9",
  port: 3000,
  enabled: true,
  notes: "指标与日志",
  dial: "10.77.0.9:3000",
  updated_at: day(-5),
};
const home: Service = {
  id: "home",
  name: "智能家居",
  domain_id: "home",
  hostname: "home.home.example.com",
  scheme: "http",
  host: "10.77.0.10",
  port: 8123,
  enabled: true,
  notes: "待首次发布",
  dial: "",
  updated_at: day(-1),
};
const media:Service={id:'media',name:'影音库',domain_id:'home',hostname:'media.home.example.com',scheme:'http',host:'10.77.0.11',port:8096,enabled:true,notes:'家庭电影与音乐，演示已发布服务',dial:'10.77.0.11:8096',updated_at:day(-5)};
const files:Service={id:'files',name:'文件管理',domain_id:'home',hostname:'files.home.example.com',scheme:'https',host:'10.77.0.12',port:443,enabled:true,notes:'文件浏览与共享，演示 HTTPS 上游',dial:'10.77.0.12:443',updated_at:day(-5)};
const downloads:Service={id:'downloads',name:'下载中心',domain_id:'home',hostname:'downloads.home.example.com',scheme:'http',host:'10.77.0.13',port:8080,enabled:false,notes:'演示停用草稿，可启用后发布',dial:'',updated_at:day(-1)};
const samples=[photos,grafana,home,media,files,downloads];

function changes(before: Service[], after: Service[]): Change[] {
  const result: Change[] = [];
  for (const previous of before) {
    const current = after.find((service) => service.id === previous.id);
    if (!current)
      result.push({
        kind: "deleted",
        hostname: previous.hostname,
        before: previous,
      });
    else if (
      [
        "name",
        "domain_id",
        "hostname",
        "scheme",
        "host",
        "port",
        "enabled",
        "notes",
      ].some(
        (key) =>
          previous[key as keyof Service] !== current[key as keyof Service],
      )
    ) {
      const kind =
        previous.enabled !== current.enabled
          ? current.enabled
            ? "enabled"
            : "disabled"
          : "updated";
      result.push({
        kind,
        hostname: current.hostname,
        before: previous,
        after: current,
      });
    }
  }
  for (const current of after)
    if (!before.some((service) => service.id === current.id))
      result.push({
        kind: "added",
        hostname: current.hostname,
        after: current,
      });
  return result;
}

function config(services: Service[]) {
  return {
    apps: {
      http: {
        servers: {
          srv0: {
            listen: [":443"],
            routes: services
              .filter((service) => service.enabled)
              .map((service) => ({
                match: [{ host: [service.hostname] }],
                handle: [
                  {
                    handler: "reverse_proxy",
                    upstreams: [{ dial: service.host + ":" + service.port }],
                  },
                ],
              })),
          },
        },
      },
    },
  };
}

type Validation = {id:string;revision:number;hash:string;runtimeHash:string;rollbackId:string;expires:number};
export type DemoState = {
  version:1;initialized:boolean;username:string;authenticated:boolean;tokenConfigured:boolean;
  portableSettings:ManagedSettings;activeSettings:ManagedSettings;revision:number;
  published:Service[];draft:Service[];revisions:[number,Service[]][];
  deployments:Deployment[];audits:Audit[];validation:Validation|null;
};

export function createMockApi(options:{staticDemo?:boolean;setup?:boolean;state?:DemoState} = {}) {
  const staticDemo=options.staticDemo===true;
  let initialized=!options.setup,username="admin",tokenConfigured=false;
  let portableSettings: ManagedSettings={origin:"https://caddyadmin.home.example.com",domains:[{id:"home",name:"home.example.com",access:"trusted"}],console_lan_only:false,admin_domain:"caddyadmin.home.example.com",lan_cidrs:["10.0.0.0/8"],upstream_cidrs:["10.0.0.0/8"],allowed_names:[],denied_ips:["10.0.0.2"],resolvers:["10.77.0.1"]};
  let activeSettings=clone(portableSettings);
  const deploymentSettings=new Map<string,ManagedSettings>();
  let authenticated = true;
  let revision = 7;
  let published = clone([photos, grafana,...(staticDemo?[media,files]:[])]);
  let draft = clone([
    photos,
    { ...grafana, port: 3001, dial: "", updated_at: day(-1) },
    home,
    ...(staticDemo?[media,files,downloads]:[]),
  ]);
  const revisions = new Map<number, Service[]>([
    [6, clone(published)],
    [7, clone(draft)],
    [3, clone([photos])],
  ]);
  const first: Deployment = {
    id: "release-1",
    version: 1,
    revision: 3,
    status: "success",
    services: clone([photos]),
    base_hash: hash([]),
    hash: hash([photos]),
    actor: "admin",
    created: day(-14),
    finished: day(-14),
    error: "",
    rollback_id: "",
    changes: changes([], [photos]),
  };
  const second: Deployment = {
    id: "release-2",
    version: 2,
    revision: 6,
    status: "success",
    services: clone(published),
    base_hash: first.hash,
    hash: hash(published),
    actor: "admin",
    created: day(-5),
    finished: day(-5),
    error: "",
    rollback_id: "",
    changes: changes([photos], published),
  };
  const deployments = [second, first];
  const deploymentOutcomes = new Map<string,"success"|"failed"|"uncertain">();
  const deploymentPolls = new Map<string,number>();
  const audits: Audit[] = [
    {
      id: 3,
      time: day(-1),
      actor: "admin",
      action: "service.update",
      object: grafana.hostname,
      result: "完成",
      revision: 7,
      version: 0,
      rollback_id: "",
      error_class: "",
    },
    {
      id: 2,
      time: second.created,
      actor: "admin",
      action: "deployment.success",
      object: second.id,
      result: "完成",
      revision: 6,
      version: 2,
      rollback_id: "",
      error_class: "",
    },
    {
      id: 1,
      time: first.created,
      actor: "admin",
      action: "deployment.success",
      object: first.id,
      result: "完成",
      revision: 3,
      version: 1,
      rollback_id: "",
      error_class: "",
    },
  ];
  let validation: Validation | null = null;
  if(options.setup){draft=[];published=[];deployments.length=0;audits.length=0;revision=0;revisions.clear();revisions.set(0,[]);}
  if(options.state){
    const state=clone(options.state);
    if(state.version!==1||!Array.isArray(state.draft)||!Array.isArray(state.deployments)||!Array.isArray(state.portableSettings?.domains))throw new Error("无效的演示数据");
    initialized=state.initialized;username=state.username;authenticated=state.authenticated;tokenConfigured=state.tokenConfigured;
    portableSettings=state.portableSettings;activeSettings=state.activeSettings;revision=state.revision;published=state.published;draft=state.draft;validation=state.validation;
    revisions.clear();for(const [key,value] of state.revisions)revisions.set(key,value);
    deployments.splice(0,deployments.length,...state.deployments);audits.splice(0,audits.length,...state.audits);
  }
  const certificateStatus=():CertificateStatus=>({mode:tokenConfigured?'cloudflare':'bootstrap_internal',activation_status:tokenConfigured?'success':'idle',public_status:tokenConfigured?'ready':'unknown',last_error_class:'',updated_at:now()});
  const record = (
    action: string,
    object: string,
    version = 0,
    rollbackId = "",
  ) => {
    audits.unshift({
      id: (audits[0]?.id ?? 0) + 1,
      time: now(),
      actor: username,
      action,
      object,
      result: "完成",
      revision,
      version,
      rollback_id: rollbackId,
      error_class: "",
    });
  };
  const preview = (rollbackId: string): Preview | null => {
    const original = rollbackId
      ? deployments.find(
          (item) => item.id === rollbackId && item.status === "success",
        )
      : undefined;
    if (rollbackId && !original) return null;
    const target = original?.services ?? draft;
    const result: Preview = {
      revision,
      settings:clone(rollbackId?activeSettings:portableSettings),active_settings:clone(activeSettings),settings_changed:JSON.stringify(activeSettings)!==JSON.stringify(portableSettings),
      services: clone(target),
      changes: changes(published, target),
      config: config(target),
      hash: hash(target),
      runtime_hash: hash(published),
      expected_hash: hash(published),
      drift: false,
      validation_id: "",
      validation_expires_at: "",
      rollback_id: rollbackId,
    };
    if (original) {
      result.rollback_config = config(original.services);
      result.rollback_hash = original.hash;
    }
    return result;
  };

  function handle(method: string, rawUrl: string, body?: unknown): Result {
    const url = new URL(rawUrl, "http://localhost");
    const path = url.pathname;
    const input =
      body && typeof body === "object" ? (body as Record<string, unknown>) : {};
    if(staticDemo&&path.startsWith('/setup/')){
      const settings=input.settings as {domain?:string;resolvers?:string[]}|undefined;
      const domain=settings?.domain??'home.example.com';
      if(method==='GET'&&path==='/setup/status')return ok({initialized,external_caddy:false,test_tls:false,resolver_suggestions:['1.1.1.1'],admin_password_status:'missing',cloudflare_token_status:'missing',setup_id:'static-demo'});
      if(method==='GET'&&path==='/setup/handoff')return ok({initialized,mode:'embedded',admin_origin:portableSettings.origin,manager_status:'ready',dns_status:'ready',console_status:'ready',temporary_entry:false,checked_at:now()});
      if(method==='POST'&&path==='/setup/preflight')return ok({normalized:{admin_domain:`caddyadmin.${domain}`,origin:`https://caddyadmin.${domain}`,admin_origin:`https://caddyadmin.${domain}`},network_valid:true,checks:[{id:'demo',status:'pass',message:'演示预检通过，未进行真实网络检查'}],can_complete:true,requires_acknowledgement:false,warning_fingerprint:''});
      if(method==='POST'&&path==='/setup/dns/preview')return ok({name:`*.${input.domain}`,type:String(input.address).includes(':')?'AAAA':'A',address:input.address,action:'create',fingerprint:'static-demo-dns'});
      if(method==='POST'&&path==='/setup/dns/confirm')return ok({confirmed:true});
      if(method==='POST'&&path==='/setup/complete'){
        portableSettings=clone(input.import_settings as ManagedSettings??{origin:`https://caddyadmin.${domain}`,admin_domain:`caddyadmin.${domain}`,domains:[{id:'home',name:domain,access:null}],console_lan_only:false,lan_cidrs:[],upstream_cidrs:[],allowed_names:[],denied_ips:[],resolvers:settings?.resolvers??['1.1.1.1']});
        activeSettings=clone(portableSettings);draft=((input.services??[]) as Service[]).map(s=>({...s,id:randomUUID(),dial:'',updated_at:now()}));published=[];revision++;revisions.set(revision,clone(draft));
        initialized=true;authenticated=true;username=String(input.username??'admin');tokenConfigured=true;
        record('setup.complete','demo');return ok({admin_origin:portableSettings.origin});
      }
      return fail(404,'not_found','演示 API 不支持该请求');
    }
    if(method==="GET"&&path==="/setup/status")return ok({initialized:true,external_caddy:false,test_tls:false,resolver_suggestions:[]});
    if(method==="POST"&&path.startsWith("/setup/"))return fail(409,"conflict","演示实例已经初始化，不会执行 Cloudflare DNS 操作");
    if (method === "GET" && path === "/auth/session")
      return authenticated
        ? ok({
            username,
            csrf: "mock-csrf",
            must_change: false,
            expires: Date.now() + 43200000,
          })
        : fail(401, "unauthorized", "演示会话已退出");
    if (method === "POST" && path === "/auth/login") {
      if (input.username !== username || !input.password)
        return fail(401, "credentials", "演示账号为 admin，密码可填写任意内容");
      authenticated = true;
      return ok({
        username,
        csrf: "mock-csrf",
        must_change: false,
        expires: Date.now() + 43200000,
      });
    }
    if (!authenticated) return fail(401, "unauthorized", "请先登录演示会话");
    if(staticDemo&&method==='POST'&&path==='/demo/services'){
      if(input.revision!==revision)return fail(409,'conflict','草稿已变化，请重新添加');
      const domain=portableSettings.domains[0];
      if(!domain)return fail(422,'invalid','请先登记域名，再添加演示服务');
      const additions=samples.map(s=>({...s,id:randomUUID(),domain_id:domain.id,hostname:`${s.hostname.split('.')[0]}.${domain.name}`,dial:'',updated_at:now()})).filter(s=>!draft.some(existing=>existing.hostname===s.hostname));
      if(additions.length){draft.push(...additions);revision++;revisions.set(revision,clone(draft));validation=null;record('service.demo.add','draft');}
      return ok({added:additions.length,revision});
    }
    if (method === "POST" && path === "/auth/password" &&
      (typeof input.password !== "string" || !validNewPassword(input.password)))
      return fail(422, "invalid", "密码至少 8 个字符，最多 256 字节");
    if (
      method === "POST" &&
      (path === "/auth/logout" || path === "/auth/password")
    ) {
      authenticated = false;
      return { status: 204 };
    }
    if(method==="GET" && path==="/configuration/export")return ok({format:"caddy-web-admin",version:2,settings:clone(portableSettings),services:draft.map(({name,domain_id,hostname,scheme,host,port,enabled,notes})=>({name,domain_id,hostname,scheme,host,port,enabled,notes}))});
    if(method==="POST" && ["/configuration/preview","/configuration/import"].includes(path)){
      try {
        const value=parseConfiguration(JSON.stringify(input.configuration));
        const seen=new Set<string>();
        const services:Service[]=value.services.map(item=>{
          const hostname=item.hostname.trim().toLowerCase().replace(/\.+$/,"");
          const domain=portableSettings.domains.find(d=>d.id===item.domain_id);
          const label=domain?hostname.slice(0,-("."+domain.name).length):'';
          if(!domain||!hostname.endsWith('.'+domain.name)||!label||label.includes('.')||hostname===portableSettings.admin_domain||seen.has(hostname)||!item.name.trim()||!item.host.trim())throw new Error("服务不符合演示实例的域名或网络策略");
          if(!staticDemo&&(item.domain_id!=="home"||!/^10\.(\d{1,3}\.){2}\d{1,3}$/.test(item.host)||item.host.split(".").some(v=>Number(v)>255)||portableSettings.denied_ips.includes(item.host)))throw new Error("服务不符合演示实例的网络策略");
          seen.add(hostname);
          return {...item,hostname,id:draft.find(old=>old.hostname===hostname)?.id??randomUUID(),dial:`${item.host}:${item.port}`,updated_at:now()};
        });
        if(path==="/configuration/preview")return ok({revision,services,changes:changes(draft,services)});
        if(input.confirm!==true)return fail(422,"invalid","请明确确认替换当前服务草稿");
        if(input.revision!==revision)return fail(409,"conflict","草稿已被修改，请重新预览导入");
        draft=services;revision++;revisions.set(revision,clone(draft));validation=null;record("configuration.import","draft");return ok({revision,services:clone(draft)});
      }catch{return fail(422,"invalid","配置文件无效或服务不符合当前策略");}
    }
    const detailMatch=path.match(/^\/services\/([^/]+)(\/check-upstream)?$/);
    if(detailMatch&&(method==="GET"&&!detailMatch[2]||method==="POST"&&detailMatch[2])){
      const id=decodeURIComponent(detailMatch[1]);
      const draftService=draft.find(s=>s.id===id)??null;
      const publishedService=published.find(s=>s.id===id)??null;
      if(!draftService&&!publishedService)return fail(404,"not_found","服务不存在");
      const pending=deployments.some(d=>d.status==="applying"||d.status==="uncertain");
      const runtime={deployment_id:deployments.find(d=>d.status==="success")?.id??"",version:deployments.find(d=>d.status==="success")?.version??0,status:pending?"pending":"matched",reachable:true,expected_hash:hash(published),runtime_hash:hash(published),checked_at:now(),message:pending?"发布正在执行或结果待核对，请到发布页面查看进度":"演示运行配置与已发布版本一致；这不代表上游应用健康"};
      if(method==="POST"){
        if(Object.keys(input).some(k=>k!=="expected_hash"&&k!=="expected_deployment_id"))return fail(422,"validation","请求包含未知字段");
        if(!publishedService?.enabled||pending||input.expected_hash!==runtime.expected_hash||input.expected_deployment_id!==runtime.deployment_id)return fail(409,"conflict","请核对已发布且启用的服务和运行配置");
        return ok({deployment_id:runtime.deployment_id,status:"reachable",target:publishedService.dial||`${publishedService.host}:${publishedService.port}`,duration_ms:2,checked_at:now(),expected_hash:runtime.expected_hash,vantage:"manager",message:"演示结果：TCP 可连接；未进行真实网络检查"});
      }
      const recent=deployments.slice(0,20).filter(d=>d.changes.some(c=>c.before?.id===id||c.after?.id===id)).slice(0,5).map(({id,version,status,created,finished,rollback_id})=>({id,version,status,created,finished,rollback_id}));
      return ok(clone({id,revision,draft:draftService,published:publishedService,draft_domain:portableSettings.domains.find(d=>d.id===draftService?.domain_id)??null,published_domain:activeSettings.domains.find(d=>d.id===publishedService?.domain_id)??null,runtime,recent_deployments:recent,external_caddy:false}));
    }
    if (method === "GET" && path === "/services")
      return ok({
        revision,
        services: clone(draft),
        published: clone(published),
      });
    if (
      ["POST", "PUT", "DELETE"].includes(method) &&
      (path === "/services" || path.startsWith("/services/"))
    ) {
      if (input.revision !== revision)
        return fail(409, "conflict", "草稿已变化，请刷新");
      const id = path.split("/")[2];
      if (method === "DELETE") {
        if (!draft.some((service) => service.id === id))
          return fail(404, "not_found", "服务不存在");
        draft = draft.filter((service) => service.id !== id);
      } else {
        const service = input.service as Service | undefined;
        if (
          !service?.name ||
          !service.hostname ||
          !service.host ||
          !service.port
        )
          return fail(422, "invalid_service", "请填写完整的服务信息");
        if (method === "PUT" && !draft.some((item) => item.id === id))
          return fail(404, "not_found", "服务不存在");
        const updated = {
          ...service,
          id: method === "POST" ? randomUUID() : id,
          updated_at: now(),
          dial: "",
        };
        if (method === "POST") draft.push(updated);
        else draft = draft.map((item) => (item.id === id ? updated : item));
      }
      revision++;
      revisions.set(revision, clone(draft));
      validation = null;
      record(
        "service." + method.toLowerCase(),
        id || String(input.service && (input.service as Service).hostname),
      );
      return ok({ revision });
    }
    if (method === "GET" && path === "/overview")
      return ok({
        reachable: true,
        drift: false,
        runtime_hash: hash(published),
        expected_hash: hash(published),
        version: deployments[0]?.version??0,
        enabled: published.filter((service) => service.enabled).length,
        draft_revision: revision,
        unpublished: changes(published, draft).length > 0||JSON.stringify(activeSettings)!==JSON.stringify(portableSettings),
        message: "演示环境运行正常，当前数据为本地样例。",
        recent: clone(deployments.slice(0, 3)),
        checked_at: now(),
      });
    if(method==="POST"&&path==="/settings/dns/preview"){const domain=portableSettings.domains.find(d=>d.id===input.domain_id);if(!domain)return fail(422,"invalid","请选择已登记域名");return ok({name:`*.${domain.name}`,type:String(input.address).includes(":")?"AAAA":"A",address:input.address,action:"create",fingerprint:"mock-domain-dns"});}
    if(method==="POST"&&path==="/settings/dns/confirm"){if(input.confirm!==true||input.fingerprint!=="mock-domain-dns")return fail(409,"conflict","请重新预览并确认 DNS 变更");return ok({confirmed:true});}
    if(method==="PUT"&&path==="/settings"){
      if(input.revision!==revision)return fail(409,"conflict","草稿已变化，请刷新后重新保存");
      const candidate=input.settings as ManagedSettings;
      try{parseConfiguration(JSON.stringify({format:"caddy-web-admin",version:2,settings:candidate,services:[]}));}catch{return fail(422,"invalid","域名与策略格式无效");}
      if(candidate.console_lan_only&&!candidate.lan_cidrs.length)return fail(422,"invalid","启用控制台限制须填写可信网络");
      if(exposureChanges(portableSettings,candidate)&&input.confirm_exposure!==true)return fail(422,"confirmation","请明确确认放开访问范围");
      if(candidate.domains.some(d=>!d.name)||new Set(candidate.domains.map(d=>d.name)).size!==candidate.domains.length)return fail(422,"invalid","域名不能为空或重复");
      if([...draft,...published].some(s=>!candidate.domains.some(d=>d.id===s.domain_id)))return fail(422,"invalid","域名仍有服务引用");
      if(candidate.admin_domain!==portableSettings.admin_domain){candidate.previous_admin_domain=portableSettings.admin_domain;candidate.previous_origin=portableSettings.origin;candidate.origin=`https://${candidate.admin_domain}`;}
      portableSettings=clone(candidate);revision++;revisions.set(revision,clone(draft));validation=null;record("settings.save","draft");return ok({revision,settings:clone(portableSettings)});
    }
    if(method==="POST"&&path==="/settings/console/complete"){
      if(input.revision!==revision||input.confirm!==true||!activeSettings.previous_admin_domain)return fail(409,"conflict","请从新控制台确认交接");
      delete portableSettings.previous_admin_domain;delete portableSettings.previous_origin;revision++;revisions.set(revision,clone(draft));validation=null;record('settings.console.complete','draft');return ok({revision});
    }
    if (method === "GET" && path === "/settings")
      return ok({
        config: {...portableSettings, test_tls:false},active_config:clone(activeSettings),revision,
        manager_version: "演示模式",
        caddy_version: "演示模式",
        cloudflare_module: true,
        token_configured: tokenConfigured,
        certificate_status: certificateStatus(),
        external_caddy: false,
      });
    if (method === "POST" && path === "/settings/cloudflare"){
      if(staticDemo){tokenConfigured=true;record('settings.cloudflare','demo');return ok({certificate_status:certificateStatus(),restart_required:false});}
      return {status:202,body:{certificate_status:{mode:'cloudflare',activation_status:'applying',public_status:'pending',last_error_class:'',updated_at:now()},restart_required:false}};
    }
    if(staticDemo&&method==='GET'&&path==='/certificates')return ok({items:activeSettings.domains.map(domain=>({subject:`*.${domain.name}`,status:'valid',message:'演示证书状态，未进行真实 TLS 探测。',sans:[`*.${domain.name}`],not_before:day(-20),not_after:day(70),days:70,checked_at:now()})),offset:0,limit:20});
    if (method === "GET" && path === "/certificates")
      return ok({
        items: [
          {
            subject: "*.home.example.com",
            status: "valid",
            message: "演示证书状态，未进行真实 TLS 探测。",
            sans: ["*.home.example.com"],
            not_before: day(-20),
            not_after: day(70),
            days: 70,
            checked_at: now(),
          },
          {
            subject: "*.example.com",
            status: "warning",
            message: "演示证书状态，未进行真实 TLS 探测。",
            sans: ["*.example.com"],
            not_before: day(-72),
            not_after: day(18),
            days: 18,
            checked_at: now(),
          },
        ],
        offset: 0,
        limit: 20,
      });
    if (method === "GET" && path === "/audit") return ok(page(audits, url));
    if (method === "GET" && path.startsWith("/draft/revisions/")) {
      const number = Number(path.split("/").at(-1));
      const services = revisions.get(number);
      return services
        ? ok({ revision: number, services: clone(services) })
        : fail(404, "not_found", "草稿版本不存在");
    }
    if (method === "GET" && path === "/draft/preview") {
      const result = preview(url.searchParams.get("rollback") ?? "");
      return result ? ok(result) : fail(404, "not_found", "发布记录不存在");
    }
    if (method === "POST" && path === "/draft/validate") {
      const rollbackId = String(input.rollback_id ?? "");
      const result = preview(rollbackId);
      if (!result) return fail(404, "not_found", "发布记录不存在");
      if (input.revision !== revision)
        return fail(409, "conflict", "草稿已变化，请刷新");
      validation = {
        id: randomUUID(),
        revision,
        hash: result.hash,
        runtimeHash: result.runtime_hash,
        rollbackId,
        expires: Date.now() + 900000,
      };
      record("validation", rollbackId || "r" + revision);
      return ok({
        ...result,
        validation_id: validation.id,
        validation_expires_at: new Date(validation.expires).toISOString(),
      });
    }
    if (method === "GET" && path === "/deployments")
      return ok(page(deployments, url));
    if (method === "GET" && path.startsWith("/deployments/")) {
      const item = deployments.find(
        (deployment) => deployment.id === path.split("/")[2],
      );
      if(item?.status==="applying"){
        const polls=(deploymentPolls.get(item.id)??0)+1;
        deploymentPolls.set(item.id,polls);
        if(polls>=2){
          item.status=deploymentOutcomes.get(item.id)??"success";
          if(item.status==="success"){published=clone(item.services);activeSettings=clone(deploymentSettings.get(item.id)??activeSettings);}
          if(item.status==="failed")item.error="演示：Caddy 拒绝候选配置，原运行配置保持不变";
          if(item.status==="uncertain")item.error="演示：响应丢失，实际运行状态待核对";
          if(item.status!=="uncertain")item.finished=now();
          record(`deployment.${item.status}`,item.id,item.version,item.rollback_id);
        }
      }
      return item
        ? ok({ deployment: clone(item), config: config(item.services) })
        : fail(404, "not_found", "发布记录不存在");
    }
    if (method === "POST" && path === "/deployments") {
      if (
        !validation ||
        input.validation_id !== validation.id ||
        input.revision !== revision ||
        input.expected_hash !== hash(published) ||
        validation.expires <= Date.now()
      )
        return fail(409, "conflict", "校验已失效，请重新校验");
      const result = preview(validation.rollbackId)!;
      if (
        result.hash !== validation.hash ||
        result.runtime_hash !== validation.runtimeHash
      )
        return fail(409, "conflict", "配置已变化，请重新校验");
      const deployment: Deployment = {
        id: randomUUID(),
        version: (deployments[0]?.version??0) + 1,
        revision,
        status: "applying",
        services: clone(result.services),
        base_hash: hash(published),
        hash: result.hash,
        actor: username,
        created: now(),
        finished: "",
        error: "",
        rollback_id: validation.rollbackId,
        changes: clone(result.changes),
      };
      deploymentSettings.set(deployment.id,clone(result.settings));
      deployments.unshift(deployment);
      const idempotency=String(input.idempotency_key??"");
      deploymentOutcomes.set(deployment.id,idempotency.startsWith("mock-failed")?"failed":idempotency.startsWith("mock-uncertain")?"uncertain":"success");
      deploymentPolls.set(deployment.id,0);
      validation = null;
      record("deployment.begin",deployment.id,deployment.version,deployment.rollback_id);
      if(staticDemo){
        deployment.status='success';deployment.finished=now();published=clone(deployment.services);activeSettings=clone(result.settings);
        record('deployment.success',deployment.id,deployment.version,deployment.rollback_id);
        return ok(clone(deployment));
      }
      return { status: 202, body: clone(deployment) };
    }
    return fail(404, "not_found", "演示 API 不支持该请求");
  }

  const getState=():DemoState=>clone({version:1,initialized,username,authenticated,tokenConfigured,portableSettings,activeSettings,revision,published,draft,revisions:[...revisions],deployments,audits,validation});
  return { handle, getState };
}
