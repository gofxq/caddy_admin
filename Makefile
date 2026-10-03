.PHONY: test build dev web static-web check integration container-test single-up validation-up validation-status validation-logs validation-down
test:
	go test -race ./cmd/... ./internal/...
	cd web && corepack pnpm test
build:
	go build -o bin/manager ./cmd/manager
	cd web && corepack pnpm build
check:
	go vet ./cmd/... ./internal/...
	go mod verify
	cd deploy/caddy && go mod verify
integration:
	cd deploy/caddy && go build -o ../../bin/caddy .
	CADDY_INTEGRATION_BINARY=$(CURDIR)/bin/caddy go test -v ./internal/adapter/caddy -run 'Test(CaddyIntegration|CloudflareValidation)' -count=1
dev:
	cd web && corepack pnpm dev
web:
	cd web && corepack pnpm dev --mode mock --port 5177 --strictPort
static-web:
	cd web && corepack pnpm install --frozen-lockfile
	cd web && corepack pnpm build:demo
	cd web && corepack pnpm preview:demo

single-up:
	docker compose up -d
container-test:
	docker build --target test-client -t caddy-admin:single-container-test .
	python3 test/container_smoke.py
	
validation-up:
	docker compose -p caddy-admin-validation -f compose.yaml -f compose.build.yaml -f compose.validation.yaml up -d --build

validation-status:
	docker compose -p caddy-admin-validation -f compose.yaml -f compose.build.yaml -f compose.validation.yaml ps
	docker compose -p caddy-admin-validation -f compose.yaml -f compose.build.yaml -f compose.validation.yaml port caddy 443 --protocol tcp

validation-logs:
	docker compose -p caddy-admin-validation -f compose.yaml -f compose.build.yaml -f compose.validation.yaml logs --tail=200

validation-down:
	docker compose -p caddy-admin-validation -f compose.yaml -f compose.build.yaml -f compose.validation.yaml down -v

reup-validation: validation-down validation-up validation-status
