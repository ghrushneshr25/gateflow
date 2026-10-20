# CHANGE-20261020 — Integrate nexus DI

## Summary

Wired all five Go services with `github.com/ghrushneshr25/nexus` v1.2.0 for interface-first lazy singletons and mockable ports.

## Motivation

User request to use nexus for singleton implementation and easy mocking in tests.

## Previous → new

- Manual `store.New` + `httpserver.New` in main  
- → `app.Wire` registers constructors/values; `nexus.MustGet(APIServerContract)` mounts routes  
- HTTP handlers depend on store/HTTP interfaces, not concrete `*store.Store` only  

## Files

- `services/*/internal/app/wire.go`, `internal/ports/ports.go`
- `services/*/cmd/server/main.go`
- `services/*/internal/httpserver` interface-typed Store
- `services/order/internal/httpserver/checkout_idempotency_test.go` (nexus mock demo)
- `libs/go/appnexus` test Reset helper
- ADR-0025

## Verification

| Command | Result |
|---|---|
| `go build` all five services | PASS |
| `go test ./services/order/internal/httpserver/` | PASS (nexus mock) |
| `go test -tags=integration ./services/inventory/tests/` | PASS |
| VS-1 checkout after nexus wiring | PASS (`CONFIRMED`) |

## Invariants

Unchanged; DI only.

## Follow-up

Expand unit tests with nexus mocks for admission/inventory handlers; optional `MustInitialize` at startup if eager warm preferred.
