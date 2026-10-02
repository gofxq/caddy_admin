import {parseConfiguration} from "./src/configuration.ts";
import { createHash, randomUUID } from "node:crypto";
import type { Plugin } from "vite";
import type {
  Audit,
  Change,
  Deployment,
  Preview,
  Service,
} from "./src/model.ts";

type Result = { status: number; body?: unknown };
const ok = (body: unknown): Result => ({ status: 200, body });
const fail = (status: number, code: string, message: string): Result => ({
  status,
  body: { error: { code, message } },
});
const now = () => new Date().toISOString();
const day = (offset: number) =>
  new Date(Date.now() + offset * 86400000).toISOString();
const hash = (value: unknown) =>
  createHash("sha256").update(JSON.stringify(value)).digest("hex");
const clone = <T>(value: T): T => structuredClone(value);
const page = <T>(items: T[], url: URL) => {
  const offset = Math.max(0, Number(url.searchParams.get("offset")) || 0);
  return { items: items.slice(offset, offset + 20), offset, limit: 20 };
};

const photos: Service = {
  id: "photos",
  name: "照片库",
  group: "homelab",
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
  group: "homelab",
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
  group: "homelab",
  hostname: "home.home.example.com",
  scheme: "http",
  host: "10.77.0.10",
  port: 8123,
  enabled: true,
  notes: "待首次发布",
  dial: "",
  updated_at: day(-1),
};

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
        "group",
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

export function createMockApi() {
  const portableSettings={origin:"https://caddyadmin.home.example.com",public_domain:"",homelab_domain:"home.example.com",admin_domain:"caddyadmin.home.example.com",lan_cidrs:["10.0.0.0/8"],upstream_cidrs:["10.0.0.0/8"],allowed_names:[],denied_ips:["10.0.0.2"],resolvers:["10.77.0.1"]};
  let authenticated = true;
  let revision = 7;
  let published = clone([photos, grafana]);
  let draft = clone([
    photos,
    { ...grafana, port: 3001, dial: "", updated_at: day(-1) },
    home,
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
  let validation: {
    id: string;
    revision: number;
    hash: string;
    runtimeHash: string;
    rollbackId: string;
    expires: number;
  } | null = null;
  const record = (
    action: string,
    object: string,
    version = 0,
    rollbackId = "",
  ) => {
    audits.unshift({
      id: (audits[0]?.id ?? 0) + 1,
      time: now(),
      actor: "admin",
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
    if (method === "GET" && path === "/auth/session")
      return authenticated
        ? ok({
            username: "admin",
            csrf: "mock-csrf",
            must_change: false,
            expires: Date.now() + 43200000,
          })
        : fail(401, "unauthorized", "演示会话已退出");
    if (method === "POST" && path === "/auth/login") {
      if (input.username !== "admin" || !input.password)
        return fail(401, "credentials", "演示账号为 admin，密码可填写任意内容");
      authenticated = true;
      return ok({
        username: "admin",
        csrf: "mock-csrf",
        must_change: false,
        expires: Date.now() + 43200000,
      });
    }
    if (!authenticated) return fail(401, "unauthorized", "请先登录演示会话");
    if (
      method === "POST" &&
      (path === "/auth/logout" || path === "/auth/password")
    ) {
      authenticated = false;
      return { status: 204 };
    }
    if(method==="GET" && path==="/configuration/export")return ok({format:"caddy-web-admin",version:1,settings:clone(portableSettings),services:draft.map(({name,group,hostname,scheme,host,port,enabled,notes})=>({name,group,hostname,scheme,host,port,enabled,notes}))});
    if(method==="POST" && ["/configuration/preview","/configuration/import"].includes(path)){
      try {
        const value=parseConfiguration(JSON.stringify(input.configuration));
        const seen=new Set<string>();
        const services:Service[]=value.services.map(item=>{
          const hostname=item.hostname.trim().toLowerCase().replace(/\.+$/,"");
          const label=hostname.slice(0,-("."+portableSettings.homelab_domain).length);
          if(item.group!=="homelab"||!hostname.endsWith("."+portableSettings.homelab_domain)||!label||label.includes(".")||hostname===portableSettings.admin_domain||seen.has(hostname)||!item.name.trim()||!/^10\.(\d{1,3}\.){2}\d{1,3}$/.test(item.host)||item.host.split(".").some(v=>Number(v)>255)||portableSettings.denied_ips.includes(item.host))throw new Error("服务不符合演示实例的域名或网络策略");
          seen.add(hostname);
          return {...item,hostname,id:draft.find(old=>old.hostname===hostname)?.id??randomUUID(),dial:`${item.host}:${item.port}`,updated_at:now()};
        });
        if(path==="/configuration/preview")return ok({revision,services,changes:changes(draft,services)});
        if(input.confirm!==true)return fail(422,"invalid","请明确确认替换当前服务草稿");
        if(input.revision!==revision)return fail(409,"conflict","草稿已被修改，请重新预览导入");
        draft=services;revision++;revisions.set(revision,clone(draft));validation=null;record("configuration.import","draft");return ok({revision,services:clone(draft)});
      }catch{return fail(422,"invalid","配置文件无效或服务不符合当前策略");}
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
        version: deployments[0].version,
        enabled: published.filter((service) => service.enabled).length,
        draft_revision: revision,
        unpublished: changes(published, draft).length > 0,
        message: "演示环境运行正常，当前数据为本地样例。",
        recent: clone(deployments.slice(0, 3)),
        checked_at: now(),
      });
    if (method === "GET" && path === "/settings")
      return ok({
        config: {...portableSettings, test_tls:false},
        manager_version: "演示模式",
        caddy_version: "演示模式",
        cloudflare_module: true,
        token_configured: false,
        certificate_status: {mode:'bootstrap_internal',activation_status:'idle',public_status:'unknown',last_error_class:'',updated_at:now()},
        external_caddy: false,
      });
    if (method === "POST" && path === "/settings/cloudflare")
      return {status:202,body:{certificate_status:{mode:'cloudflare',activation_status:'applying',public_status:'pending',last_error_class:'',updated_at:now()},restart_required:false}};
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
          if(item.status==="success")published=clone(item.services);
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
        version: deployments[0].version + 1,
        revision,
        status: "applying",
        services: clone(result.services),
        base_hash: hash(published),
        hash: result.hash,
        actor: "admin",
        created: now(),
        finished: "",
        error: "",
        rollback_id: validation.rollbackId,
        changes: clone(result.changes),
      };
      deployments.unshift(deployment);
      const idempotency=String(input.idempotency_key??"");
      deploymentOutcomes.set(deployment.id,idempotency.startsWith("mock-failed")?"failed":idempotency.startsWith("mock-uncertain")?"uncertain":"success");
      deploymentPolls.set(deployment.id,0);
      validation = null;
      record("deployment.begin",deployment.id,deployment.version,deployment.rollback_id);
      return { status: 202, body: clone(deployment) };
    }
    return fail(404, "not_found", "演示 API 不支持该请求");
  }

  return { handle };
}

export function mockApiPlugin(): Plugin {
  const mock = createMockApi();
  return {
    name: "local-mock-api",
    configureServer(server) {
      server.middlewares.use("/api/v1", async (req, res) => {
        try {
          let raw = "";
          for await (const chunk of req) raw += String(chunk);
          const result = mock.handle(
            req.method ?? "GET",
            req.url ?? "/",
            raw ? JSON.parse(raw) : undefined,
          );
          res.statusCode = result.status;
          if (result.status === 204) return res.end();
          res.setHeader("Content-Type", "application/json; charset=utf-8");
          res.end(JSON.stringify(result.body));
        } catch {
          res.statusCode = 400;
          res.setHeader("Content-Type", "application/json; charset=utf-8");
          res.end(
            JSON.stringify({
              error: { code: "bad_request", message: "演示请求格式无效" },
            }),
          );
        }
      });
    },
  };
}
