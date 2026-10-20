# ADR-0022: Local orchestration and CI

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Reproducible local and CI verification.

## Decision
- Docker Compose for infra + services
- Makefile targets: fmt, lint, test, build, contract-validate
- GitHub Actions CI: build, unit/integration (services), frontend typecheck/test
- Kubernetes/Helm deferred to Phase 7

## Alternatives
- K8s-first local (kind/minikube) — deferred cost
- No CI until late — rejected

## Consequences
+ Fast local loop
- Compose files must stay maintainable

