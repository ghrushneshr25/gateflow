# CHANGE-20261020 — Phase 0 specification + Phase 1 foundation

## Summary

Consolidated Gateflow specifications (PRD, architecture, GF-SCS-001, 24 ADRs) and bootstrapped the monorepo foundation: five Go services, shared technical libs, Compose infra, two React apps, CI, and Makefile.

## Problem and motivation

Empty repository required a coherent baseline before domain implementation. Specs existed only as draft PRD files and were gitignored.

## Requirements addressed

- Master prompt Phase 0 and Phase 1
- AGENTS.md change-record and ADR requirements
- Service ownership and contract documentation

## Previous → new behavior

- **Before:** single init commit, gitignored docs, no code
- **After:** versioned docs, buildable services (health only), runnable frontends scaffolds, local infra compose

## Services / modules / files affected

- `docs/**`, `services/**`, `libs/go/**`, `apps/**`, `contracts/**`, `deployments/**`
- `go.work`, `Makefile`, `docker-compose.yml`, `.github/workflows/ci.yml`, `README.md`, `.gitignore`

## Decision tree

See ADRs 0001–0024 and `docs/execution/PHASE0.md`. Key choices: monorepo + go.work, stdlib HTTP, one Postgres with schemas, Kafka KRaft local, npm frontends.

## API / event / DB / compatibility impact

- Health and `/v1/meta` only — no business API yet
- Event envelope JSON Schema stub added
- Postgres init creates schemas/roles only

## Invariants preserved

N/A for domain state (not implemented yet). Documentation encodes invariants for later enforcement.

## Failure modes

Service process-only readiness; dependency failures not yet reflected in `/readyz`.

## Tests added

- `libs/go/httpx`, `libs/go/authn` unit tests
- Frontend App shell Vitest tests

## Verification

See `docs/execution/PHASE1.md` — all listed commands executed with PASS.

## Security / performance / ops

- Dev JWT helper only; secrets via env example
- No performance claims
- Compose obs stack behind profile `obs`

## Limitations and follow-up

Phase 2 vertical slice VS-1.

## Related ADRs

ADR-0001 through ADR-0024 (Accepted).
