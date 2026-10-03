import type { Plugin } from "vite";
import { createMockApi } from "./src/demo/mock-api.ts";
export { createMockApi } from "./src/demo/mock-api.ts";

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
