# Tool versions are pinned here; nothing needs a global install.
BUF_VERSION            := v1.47.2
PROTOC_GEN_GO          := v1.36.1
PROTOC_GEN_GO_GRPC     := v1.5.1
GOLANGCI_LINT          := v1.62.2

BIN := $(CURDIR)/bin
BUF := go run github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

# ---------------------------------------------------------------- proto

$(BIN)/protoc-gen-go:
	GOBIN=$(BIN) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO)

$(BIN)/protoc-gen-go-grpc:
	GOBIN=$(BIN) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC)

.PHONY: proto
proto: $(BIN)/protoc-gen-go $(BIN)/protoc-gen-go-grpc ## Lint protos and regenerate gen/go
	$(BUF) lint
	$(BUF) generate
	cd gen && go mod tidy

.PHONY: proto-breaking
proto-breaking: ## Check protos against main for breaking changes
	$(BUF) breaking --against '.git#branch=main'

# ---------------------------------------------------------------- infra

.PHONY: up
up: ## Start local infrastructure (PostgreSQL, Kafka, Redis, MinIO)
	docker compose -f deploy/docker-compose.infra.yml up -d --wait

.PHONY: down
down: ## Stop local infrastructure (volumes preserved)
	docker compose -f deploy/docker-compose.infra.yml down

.PHONY: nuke
nuke: ## Stop local infrastructure AND delete its volumes
	docker compose -f deploy/docker-compose.infra.yml down -v

.PHONY: migrate
migrate: ## Run goose migrations for every service that has them
	@found=0; \
	for dir in services/*/db/migrations; do \
		[ -d "$$dir" ] || continue; \
		found=1; \
		svc=$$(echo "$$dir" | cut -d/ -f2); \
		echo "==> migrating $$svc"; \
		go run github.com/pressly/goose/v3/cmd/goose@v3.24.0 -dir "$$dir" postgres \
			"postgres://erp_$$svc:erp_$${svc}_pw@localhost:$${PG_PORT:-5433}/erp_$$svc?sslmode=disable" up; \
	done; \
	[ "$$found" = 1 ] || echo "no migrations yet"

# ---------------------------------------------------------------- quality

.PHONY: test
test: ## Run all Go tests
	@for mod in pkg gen $(wildcard services/*); do \
		[ -f "$$mod/go.mod" ] || continue; \
		echo "==> go test ./$$mod/..."; \
		(cd "$$mod" && go test ./...) || exit 1; \
	done

.PHONY: lint
lint: ## golangci-lint over every module
	@for mod in pkg $(wildcard services/*); do \
		[ -f "$$mod/go.mod" ] || continue; \
		echo "==> lint $$mod"; \
		(cd "$$mod" && go run github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT) run ./...) || exit 1; \
	done

.PHONY: check-tenant
check-tenant: ## Verify every migration table carries tenant_id
	sh scripts/check-tenant-id.sh

.PHONY: ci
ci: proto check-tenant test ## What CI runs; proto regeneration must be a no-op
	git diff --exit-code gen/ || (echo "gen/ is stale: run 'make proto' and commit" && exit 1)

.PHONY: sqlc
sqlc: ## Regenerate sqlc stores for every service that has one
	@for cfg in services/*/db/sqlc.yaml; do \
		[ -f "$$cfg" ] || continue; \
		echo "==> sqlc $$(dirname $$cfg)"; \
		(cd "$$(dirname $$cfg)" && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate) || exit 1; \
	done
