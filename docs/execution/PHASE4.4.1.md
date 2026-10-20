# Phase 4.4.1 Report — PostgreSQL Admission Coordination Fence

## CTO decision implemented

- PostgreSQL remains budget/entitlement authority.
- **Redis not on grant path.**
- Durable `coordination_state` with `ACTIVE` | `FENCED`.
- Fence linearization via shared `FOR UPDATE` on coordination row with `GrantFIFO`.

## Guarantee

Once `POST /v1/admin/coordination/fence` returns successfully:

1. Fence txn held coordination `FOR UPDATE`, set `FENCED`, committed.
2. Any grant that had the lock first finished or rolled back before fence committed.
3. Any subsequent grant observes `FENCED` and returns `ADM_COORDINATION_FENCED` without budget/entitlement side effects.
4. Unfence is explicit JWT-authorized only (never automatic on startup).

## APIs (JWT required)

| Method | Path | Scope |
|---|---|---|
| GET | `/v1/admin/coordination` | `ops:admission:read` or fence |
| POST | `/v1/admin/coordination/fence` | `ops:admission:fence` |
| POST | `/v1/admin/coordination/unfence` | `ops:admission:fence` |
| GET | `/v1/admin/coordination/metrics` | read |

Spoofed `X-Ops-Role` → 401.

## Verification (executed)

| Command | Result |
|---|---|
| admission httpserver unit (auth) | PASS |
| `go test -race -tags=integration ./services/admission/tests/` | PASS (fence, epoch, idempotent, linearization, existing tests) |
| make test / make build | PASS |
| migrate | MIGRATIONS_OK |
| vs1_e2e | VS1_E2E_PASS |
| live spoof header | 401 |
| live JWT get/fence/unfence | 200 |

## Files

- `services/admission/migrations/000004_p4_4_1_coordination.*`
- `services/admission/internal/store/store.go` (lock + fence/unfence + GrantFIFO)
- `services/admission/internal/httpserver/*`
- `services/admission/cmd/server/main.go` (authn middleware)
- `services/admission/tests/coordination_test.go`
- change record + this report

## Deferred

- Redis (P4.4.2+)
- Auto-unfence (never)
- P4.3 poison unquarantine, Kafka startup reconnect
- P4.5 process-kill harness

## Not committed / not pushed
