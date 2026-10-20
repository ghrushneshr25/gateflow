# ADR-0002: PostgreSQL topology and service-owned schemas

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Services need durable authoritative state without distributed transactions.

## Decision
One PostgreSQL instance initially with:
- Separate schemas: `sale`, `admission`, `inventory`, `order`, `payment`
- Least-privilege roles per service (DML only on own schema)
- Migrations owned per service under `services/<name>/migrations`
- No cross-schema writes from application code

## Alternatives
- Database per service from day one — deferred: heavier local ops; same logical ownership achievable with schemas
- Shared tables — rejected: destroys ownership

## Consequences
+ Clear ownership and simpler local Compose
- Schema isolation is convention+roles; must enforce in code review and grants
- Physical split remains a later extraction path (ADR-0024)

