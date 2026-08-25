# Tool versions are pinned here; nothing needs a global install.
BUF_VERSION            := v1.47.2
PROTOC_GEN_GO          := v1.36.1
PROTOC_GEN_GO_GRPC     := v1.5.1
GOLANGCI_LINT          := v1.62.2

BIN := $(CURDIR)/bin
BUF := go run github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)

# The local compose stack shifts its ports (5433/6380) so it can run next to
# anything already using 5432/6379; CI uses the standard ports. Every target
# below reads these two variables, so the same make target does the same
# thing in both places. ?= yields to values set in the environment.
PG_PORT    ?= 5433
REDIS_PORT ?= 6380

# Switches for the DB-backed integration tests. Deliberately NOT part of
# plain `make test`: once a DSN is set the tests connect for real, and a
# laptop without the compose stack up would go red instead of skipping.
# Each service reaches only its own database, under its own account.
TEST_ENV := \
	IAM_TEST_DSN='postgres://erp_iam:erp_iam_pw@localhost:$(PG_PORT)/erp_iam?sslmode=disable' \
	IAM_MIGRATION_TEST_DSN='postgres://erp_iam:erp_iam_pw@localhost:$(PG_PORT)/erp_iam_migrations?sslmode=disable' \
	MD_TEST_DSN='postgres://erp_masterdata:erp_masterdata_pw@localhost:$(PG_PORT)/erp_masterdata?sslmode=disable' \
	MD_MIGRATION_TEST_DSN='postgres://erp_masterdata:erp_masterdata_pw@localhost:$(PG_PORT)/erp_masterdata_migrations?sslmode=disable' \
	MAIL_TEST_DSN='postgres://erp_mail:erp_mail_pw@localhost:$(PG_PORT)/erp_mail?sslmode=disable' \
	SHIPPING_TEST_DSN='postgres://erp_shipping:erp_shipping_pw@localhost:$(PG_PORT)/erp_shipping?sslmode=disable' \
	SHIPPING_MIGRATION_TEST_DSN='postgres://erp_shipping:erp_shipping_pw@localhost:$(PG_PORT)/erp_shipping_migrations?sslmode=disable' \
	PROCUREMENT_TEST_DSN='postgres://erp_procurement:erp_procurement_pw@localhost:$(PG_PORT)/erp_procurement?sslmode=disable' \
	EXPORT_TEST_DSN='postgres://erp_export:erp_export_pw@localhost:$(PG_PORT)/erp_export?sslmode=disable' \
	APPROVAL_TEST_DSN='postgres://erp_approval:erp_approval_pw@localhost:$(PG_PORT)/erp_approval?sslmode=disable' \
	PRODUCT_TEST_DSN='postgres://erp_product:erp_product_pw@localhost:$(PG_PORT)/erp_product?sslmode=disable' \
	PROCUREMENT_MIGRATION_TEST_DSN='postgres://erp_procurement:erp_procurement_pw@localhost:$(PG_PORT)/erp_procurement_migrations?sslmode=disable' \
	GATEWAY_TEST_REDIS='127.0.0.1:$(REDIS_PORT)'

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

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

.PHONY: proto-check
proto-check: proto ## Regenerate protos and fail if anything drifted
	git diff --exit-code gen/ proto/ \
		|| (echo "gen/ is stale: run 'make proto' and commit" && exit 1)

.PHONY: proto-breaking
proto-breaking: ## Check protos against main for breaking changes
	# origin/main, not main: on a pull_request the runner checks out the merge
	# ref and has no local main branch, so this step failed on every PR it was
	# meant to guard.
	$(BUF) breaking --against '.git#ref=origin/main'

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

.PHONY: topics
topics: ## Create the Kafka topics (producers refuse to auto-create them)
	@for t in erp.approval.task.v1 erp.export.contract.v1 erp.inventory.stock.v1; do \
		echo "==> topic $$t"; \
		docker compose -f deploy/docker-compose.infra.yml exec -T kafka \
			kafka-topics --bootstrap-server localhost:9092 \
			--create --if-not-exists --topic "$$t" --partitions 3 --replication-factor 1; \
	done

.PHONY: migrate
migrate: ## Run goose migrations for every service that has them
	@found=0; \
	for dir in services/*/db/migrations; do \
		[ -d "$$dir" ] || continue; \
		found=1; \
		svc=$$(echo "$$dir" | cut -d/ -f2); \
		echo "==> migrating $$svc"; \
		go run github.com/pressly/goose/v3/cmd/goose@v3.24.0 -dir "$$dir" postgres \
			"postgres://erp_$$svc:erp_$${svc}_pw@localhost:$(PG_PORT)/erp_$$svc?sslmode=disable" up \
			|| { echo "迁移失败：$$svc"; exit 1; }; \
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

.PHONY: test-integration
test-integration: ## All tests including DB-backed ones (needs `make up` + `make migrate` first)
	@env $(TEST_ENV) $(MAKE) test

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

.PHONY: check-iam-seeds
check-iam-seeds: ## Verify role grants follow the role's own tenant
	sh scripts/check-iam-seeds.sh

.PHONY: check-tenant-seeds
check-tenant-seeds: ## Refuse new migrations that seed reference data for tenant 1 only
	sh scripts/check-tenant-seeds.sh

.PHONY: check-migration-safety
check-migration-safety: ## Refuse undeclared destructive migrations (deploy rolls back containers, not schema)
	sh scripts/check-migration-safety.sh

.PHONY: check-mail-sandbox
check-mail-sandbox: ## Verify received mail is only rendered inside the sandbox
	sh scripts/check-mail-sandbox.sh

.PHONY: audit-mail
audit-mail: ## Check stored mail against its invariants (needs a running database)
	sh scripts/audit-mail.sh

# The single definition of what CI checks. The workflow provides the
# environment (Postgres, Redis, created databases, migrations) and then calls
# this; it does not list checks of its own. That is deliberate: when this
# target and the workflow were two separate lists, check-mail-sandbox sat on
# one and not the other, and a guard that had already caught a real incident
# ran nowhere. One list cannot disagree with itself.
#
# Prerequisites run in the order written, cheapest first, so a stale gen/ or
# a missed tenant_id fails in seconds, not after the full test suite.
.PHONY: ci
ci: proto-check check-tenant check-tenant-seeds check-iam-seeds check-migration-safety check-mail-sandbox test-integration lint ## Everything CI runs (needs `make up` + `make migrate` first)
	@echo "ci: all checks passed"

.PHONY: sqlc
sqlc: ## Regenerate sqlc stores for every service that has one
	@for cfg in services/*/db/sqlc.yaml; do \
		[ -f "$$cfg" ] || continue; \
		echo "==> sqlc $$(dirname $$cfg)"; \
		(cd "$$(dirname $$cfg)" && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate) || exit 1; \
	done

.PHONY: services-up
services-up: ## Build and start every app service in its own container
	docker compose -f deploy/docker-compose.services.yml up -d --build

.PHONY: services-down
services-down: ## Stop app service containers
	docker compose -f deploy/docker-compose.services.yml down
