FROM golang:1.26-alpine AS caddy-build
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY deploy/caddy/go.mod deploy/caddy/go.sum ./
RUN go mod download
COPY deploy/caddy/main.go ./
COPY deploy/caddy/observability ./observability
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /usr/bin/caddy .

FROM golang:1.26-alpine AS manager-build
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /manager ./cmd/manager

FROM node:24-alpine AS web-build
WORKDIR /src
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web ./
RUN pnpm build

FROM alpine:3.23 AS runtime
RUN apk add --no-cache ca-certificates tzdata su-exec && addgroup -g 10001 app && adduser -D -H -u 10001 -G app app && mkdir -p /data/caddy /config /run/caddy /srv/snapshots /var/lib/manager /srv/web && chown -R app:app /data /config /run/caddy /srv/snapshots /var/lib/manager /srv/web
COPY --from=caddy-build /usr/bin/caddy /usr/bin/caddy
COPY --from=manager-build /manager /usr/bin/manager
COPY deploy/entrypoint.sh /usr/bin/entrypoint
ENV XDG_DATA_HOME=/data XDG_CONFIG_HOME=/config
ENTRYPOINT ["/usr/bin/entrypoint"]
USER root

FROM runtime AS app
COPY --from=web-build --chown=10001:10001 /src/dist /srv/web
CMD ["manager", "run"]

# Contains curl only for the isolated container-level acceptance harness.
FROM app AS test-client
RUN apk add --no-cache curl
