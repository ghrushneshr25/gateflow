# ADR-0001: Monorepo with five Go modules and root go.work

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Gateflow needs five independently deployable Go services plus two frontends, shared contracts, and local multi-module development.

## Decision
Use a single Git monorepo with:
- Root `go.work` coordinating service modules
- Each service: own `go.mod` under `services/<name>`
- Module path prefix `github.com/ghrushneshr25/gateflow/...`
- Apps under `apps/`, contracts under `contracts/`, technical libs under `libs/go/`

## Alternatives
- Polyrepo per service — rejected: slower contract iteration for a portfolio/system project
- Single Go module for all services — rejected: weakens independent deploy boundaries
- Nx/Bazel monorepo tooling first — deferred: unnecessary complexity for Phase 1

## Consequences
+ Atomic cross-service PRs and contract changes
+ Independent versioning/deploy still possible via separate modules/images
- Requires discipline to avoid domain coupling via shared libraries

