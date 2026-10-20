# ADR-0025: Nexus for process-local DI and test doubles

**Status:** Accepted  
**Date:** 2026-10-20

## Context

Services need a consistent way to wire dependencies as lazy singletons and to replace ports with mocks in unit tests without global mutable constructors or concrete coupling in HTTP handlers.

## Decision

Adopt [`github.com/ghrushneshr25/nexus` v1.2.0](https://pkg.go.dev/github.com/ghrushneshr25/nexus) inside each Go service process:

- Interface ports under `internal/ports` (and matching handler interfaces)
- Production constructors registered in `internal/app.Wire` after config/pool exist
- `nexus.Validate()` before serving
- `nexus.MustGet` in `main` for the API server
- Tests: `nexus.Reset()` / `appnexus.ResetForTest()`, then `MustDeclare` mocks

Nexus is a **technical** dependency only. It must not become a shared domain library across services.

## Alternatives

- Manual constructor wiring only — workable but harder to mock consistently  
- Uber FX / Wire — heavier; nexus matches singleton + Reset test model  
- Global package vars — rejected (hard to test, hidden coupling)

## Consequences

+ Lazy singletons, typed contracts, easy mock injection  
+ Structural Validate without constructing heavy deps  
- Process-global registry: tests must Reset; never share registry across services (separate binaries already)  
- Constructors must return interfaces
