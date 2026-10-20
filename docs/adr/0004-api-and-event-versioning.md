# ADR-0004: API and event versioning

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Clients and consumers need stable contracts.

## Decision
- HTTP APIs under `/v1/...` with OpenAPI per service
- Consistent error envelope (GF-SCS-001)
- Events: `schema_version` integer; additive changes preferred; breaking → version bump
- CI validates OpenAPI and JSON Schema

## Alternatives
- Header-based API versioning only — rejected for clarity
- GraphQL — rejected: not required

## Consequences
+ Explicit compatibility process
- Dual-version support may be needed during migrations

