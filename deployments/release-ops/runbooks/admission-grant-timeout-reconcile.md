# Runbook: Admission grant timeout and reconciliation

## Contract

**A timeout or lost HTTP response is an uncertain outcome — not proof of success or failure.**

Grant clients (Order S2S, buyer-web, tools) MUST:

1. Use a **stable buyer identity** and session for retries (same JWT `sub` / same `buyer_id` for service grants).
2. **Not** mint a second business effect from a bare timeout.
3. **Reconcile** against authoritative Admission state before assuming failure.
4. Retry only with the same logical operation identity where the API supports it (buyer self-grant is idempotent on existing entitlement for the queue row).

## Authoritative reads

| Check | API |
|---|---|
| Queue + entitlement link | `GET /v1/sessions/{session_id}/queue/me` (buyer JWT) |
| Entitlement by id | `GET /v1/entitlements/{id}` (service/ops scopes as configured) |
| Coordination | `GET /v1/admin/coordination` |

If `queue/me` shows `status=GRANTED` and an `entitlement_id`, the grant **committed** even if the client never saw the POST response.

## Retry

- **Buyer self-grant:** `POST .../grants` again with the same buyer JWT. Protocol A/B both return the **existing** entitlement if already granted (idempotent path).
- **Service grant:** same `buyer_id` + session; same idempotent store behavior.
- Do **not** change buyer identity to “force” a new grant after uncertainty.

## Errors

| Code | Meaning | Action |
|---|---|---|
| `ADM_COORDINATION_FENCED` | Fence active | Wait for unfence / ops |
| `ADM_STALE_GENERATION` | Protocol B: generation invalidated | Start a **new** attempt only after confirming no entitlement; do not assume prior body committed |
| `ADM_BUDGET_EXHAUSTED` | Capacity | Back off; capacity planning |
| 5xx / timeout | Uncertain | Reconcile then retry |

## Protocol flag

`GATEFLOW_ADMISSION_GRANT_PROTOCOL=A|B` (default **A**).  
Timeout/reconcile contract is **identical** for A and B. Rollback to A does not delete entitlements already issued under B.

## Related

- P6.4/P6.5/P6.6 docs under `docs/phase-6/`
- Crash recovery remains order-path focused; this runbook is admission-grant specific
