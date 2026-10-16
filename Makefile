.PHONY: test build dev web web-demo static-web check integration container-test up down freshup status logs
test:
	go test -race ./cmd/... ./internal/...
	cd deploy/caddy && go test -race ./observability
	cd web && corepack pnpm test
build:
	go build -o bin/manager ./cmd/manager
	cd web && corepack pnpm build
check:
	go vet ./cmd/... ./internal/...
	cd deploy/caddy && go vet ./...
	go mod verify
	cd deploy/caddy && go mod verify
integration:
	cd deploy/caddy && go build -o ../../bin/caddy .
	CADDY_INTEGRATION_BINARY=$(CURDIR)/bin/caddy go test -v ./internal/adapter/caddy -run 'Test(CaddyIntegration|CloudflareValidation|DNSPodValidation)' -count=1
dev:
	cd web && corepack pnpm dev
web:
	cd web && corepack pnpm dev --mode mock --port 5177 --strictPort
web-demo: 
	cd web && corepack pnpm install --frozen-lockfile
	cd web && corepack pnpm build:demo
	cd web && corepack pnpm preview:demo

container-test:
	docker build --target test-client -t caddy-admin:single-container-test .
	python3 test/container_smoke.py
	CGO_ENABLED=0 go build -o bin/setup-fixture ./test/setup-fixture
	python3 test/environment_setup.py
	python3 test/environment_setup.py dnspod
	
up:
	docker compose -p caddy-admin-validation -f compose.yaml -f compose.validation.yaml up -d --build
down:
	docker compose -p caddy-admin-validation -f compose.yaml -f compose.validation.yaml down -v
status:
	docker compose -p caddy-admin-validation -f compose.yaml -f compose.validation.yaml ps
	docker compose -p caddy-admin-validation -f compose.yaml -f compose.validation.yaml port caddy 443 --protocol tcp
logs:
	docker compose -p caddy-admin-validation -f compose.yaml -f compose.validation.yaml logs --tail=200 -f
freshup: down up status logs
