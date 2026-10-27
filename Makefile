.PHONY: help fmt vet lint test test-race build tidy work-sync compose-config compose-up compose-down frontend-install frontend-typecheck frontend-test frontend-build ci demo demo-down migrate vs1-e2e test-integration test-integration-race test-crash-recovery k8s-demo k8s-demo-down k8s-status k8s-logs k8s-obs-up k8s-obs-down k8s-all-up k8s-all-down k8s-tools-setup k8s-cluster-up k8s-build-images k8s-app-up k8s-smoke

GO_SERVICES := sale admission inventory order payment-simulator
GOWORK_PACKAGES := ./libs/go/... $(addprefix ./services/,$(addsuffix /...,$(GO_SERVICES)))

help:
	@echo "Gateflow make targets"
	@echo "  demo demo-down     local Compose lab demo (portfolio)"
	@echo "  k8s-all-up k8s-all-down   FULL kind stack + obs + smoke + PFs"
	@echo "  k8s-demo k8s-demo-down k8s-status k8s-logs"
	@echo "  k8s-obs-up k8s-obs-down   observability ns (gateflow-obs)"
	@echo "  k8s-tools-setup k8s-cluster-up k8s-build-images k8s-app-up k8s-smoke"
	@echo "  fmt lint vet test test-race build tidy"
	@echo "  compose-config compose-up compose-down"
	@echo "  migrate vs1-e2e test-integration test-crash-recovery"
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

demo:
	./scripts/demo.sh

demo-down:
	./scripts/demo-down.sh

k8s-demo:
	./scripts/k8s-demo.sh

k8s-demo-down:
	./scripts/k8s-demo-down.sh

k8s-status:
	@export PATH="$(CURDIR)/.tools:$$PATH"; \
	kubectl --context kind-gateflow -n gateflow get pods,svc,deploy,sts,job -o wide 2>/dev/null || echo "cluster/ns not ready"

k8s-logs:
	@export PATH="$(CURDIR)/.tools:$$PATH"; \
	kubectl --context kind-gateflow -n gateflow logs -l app.kubernetes.io/instance=gateflow --tail=50 2>/dev/null || echo "no logs"

k8s-obs-up:
	./scripts/k8s-obs-up.sh

k8s-obs-down:
	./scripts/k8s-obs-down.sh

k8s-tools-setup:
	./scripts/k8s-tools-setup.sh

k8s-cluster-up:
	./scripts/k8s-cluster-up.sh

k8s-build-images:
	./scripts/k8s-build-images.sh

k8s-app-up:
	./scripts/k8s-app-up.sh

k8s-smoke:
	./scripts/k8s-smoke.sh

k8s-all-up:
	./scripts/k8s-all-up.sh

k8s-all-down:
	./scripts/k8s-all-down.sh

ci: fmt vet test build compose-config frontend-typecheck frontend-test frontend-build
