.PHONY: help fmt vet lint test test-race build tidy work-sync compose-config compose-up compose-down frontend-install frontend-typecheck frontend-test frontend-build ci

GO_SERVICES := sale admission inventory order payment-simulator
GOWORK_PACKAGES := ./libs/go/... $(addprefix ./services/,$(addsuffix /...,$(GO_SERVICES)))

help:
	@echo "Gateflow make targets"
	@echo "  fmt lint vet test test-race build tidy"
	@echo "  compose-config compose-up compose-down"
	@echo "  frontend-install frontend-typecheck frontend-test frontend-build"
	@echo "  ci"

work-sync:
	go work sync

tidy: work-sync
	cd libs/go && go mod tidy
	@for s in $(GO_SERVICES); do (cd services/$$s && go mod tidy); done

fmt:
	gofmt -w libs/go services

vet:
	go vet $(GOWORK_PACKAGES)

lint:
	@command -v golangci-lint >/dev/null && golangci-lint run $(GOWORK_PACKAGES) || (echo "golangci-lint not fully configured; running go vet"; go vet $(GOWORK_PACKAGES))

test:
	go test ./libs/go/...
	@for s in $(GO_SERVICES); do (cd services/$$s && go test ./...); done

test-race:
	go test -race ./libs/go/...
	@for s in $(GO_SERVICES); do (cd services/$$s && go test -race ./...); done

build:
	@mkdir -p bin
	@for s in $(GO_SERVICES); do \
		echo "building $$s"; \
		(cd services/$$s && go build -o ../../bin/$$s ./cmd/server); \
	done

compose-config:
	docker compose config >/dev/null

compose-up:
	docker compose up -d postgres redis kafka

compose-down:
	docker compose down

frontend-install:
	npm --prefix apps/buyer-web install
	npm --prefix apps/operator-console install

frontend-typecheck:
	npm --prefix apps/buyer-web run typecheck
	npm --prefix apps/operator-console run typecheck

frontend-test:
	npm --prefix apps/buyer-web test -- --run
	npm --prefix apps/operator-console test -- --run

frontend-build:
	npm --prefix apps/buyer-web run build
	npm --prefix apps/operator-console run build

migrate:
	./scripts/migrate.sh

vs1-e2e:
	./scripts/vs1_e2e.sh

test-integration:
	go test -tags=integration -count=1 ./services/inventory/tests/...
	go test -tags=integration -count=1 ./services/order/tests/...
	go test -tags=integration -count=1 ./services/admission/tests/...

test-integration-race:
	go test -race -tags=integration -count=1 ./services/inventory/tests/...
	go test -race -tags=integration -count=1 ./services/order/tests/...
	go test -race -tags=integration -count=1 ./services/admission/tests/...

# Process-kill recovery suite (slow; requires make build + postgres + optional kafka)
test-crash-recovery:
	go test -tags='integration crash' -count=1 -timeout 15m -v ./services/order/tests/crash/

ci: fmt vet test build compose-config frontend-typecheck frontend-test frontend-build
