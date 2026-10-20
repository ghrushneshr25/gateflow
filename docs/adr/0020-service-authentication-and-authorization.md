# ADR-0020: Service authentication and authorization

**Status:** Accepted  
**Date:** 2026-10-20

## Context
Buyers, operators, and services need distinct trust boundaries.

## Decision
- Dev HMAC JWT adapter locally; OIDC path later
- principal_type + scopes; each service enforces authz
- S2S credentials audience/scope restricted
- Verified payment callbacks
- Audited sensitive operator actions

## Alternatives
- Shared long-lived static tokens without scopes — rejected
- Frontend-only auth — rejected
- Full custom IdP — non-goal

## Consequences
+ Clear security model for local and future prod
- Dev adapter must never ship as sole prod auth

