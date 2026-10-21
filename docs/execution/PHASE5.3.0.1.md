# Phase 5.3.0.1 Report — Admission Self-Grant Safety Gate

## Verdict: **COMPLETE**

## Grant flow (authoritative)

```
POST /v1/sessions/{id}/grants
  ├─ service + admission:grant → buyer_id from JSON body
  └─ buyer JWT → buyer_id := claims.sub (body/header ignored)
       ↓
  Store.GrantFIFO (single path for both)
       ├─ FOR UPDATE coordination (fence linearization)
       ├─ FOR UPDATE queue_entries WHERE session+buyer (must exist → else ErrNotFound)
       ├─ if entitlement_id already set → return existing (no budget consume; OK while FENCED)
       ├─ else require mode=ACTIVE else ErrCoordinationFenced
       ├─ consumeGrantTokens (session+global budgets)
       ├─ INSERT entitlement ISSUED
       └─ UPDATE queue GRANTED
```

**No alternate entitlement write path** for buyers.

## Invariant evidence

| # | Invariant | Enforcement | Test |
|---|---|---|---|
| 1 | Buyer only self | `buyerID = authn.BuyerID(bc)` | `TestGrantBuyerSelfOnlyIgnoresBodyTarget` |
| 2 | Cannot target other buyer | body ignored on buyer path | same |
| 3 | Queue eligibility | GrantFIFO requires queue row | unit 404 + `TestSelfGrantPathRequiresQueueAndIsIdempotent` |
| 4 | Budget | `consumeGrantTokens` in GrantFIFO | `TestGrantBudgetExhausted429` + `TestSelfGrantBudgetExhaustionDurable` |
| 5 | Fence | mode check under coord lock | `TestGrantFencedReturns503` + `TestSelfGrantRespectsFence` |
| 6 | Idempotent / no double budget | existing entitlement short-circuit | durable idempotent test |
| 7 | Service still scoped | `RequireService(admission:grant)` | `TestGrantServiceRequiresScopeAndBody` |
| 8 | Wrong principal / spoof | fail-closed | `TestGrantRejectsMissingTokenAndSpoofHeaders`, `TestGrantOperatorRejected` |
| 9 | Concurrent budget | race integration | budget exhaustion concurrent |
| 10 | No silent create on fail | store errors → HTTP; no commit | fence/not-found tests |

## Changes this gate

- Clarified grant handler comments; always routes to `GrantFIFO`.
- Added `grant_auth_test.go` (HTTP matrix).
- Added `self_grant_test.go` (durable integration).

## Verification (executed)

| Command | Result |
|---|---|
| `go test ./services/admission/internal/httpserver/` | **PASS** |
| `go test -race -tags=integration ./services/admission/tests/` | **PASS** |
| `make test` | **PASS** |
| `make build` | **PASS** |
| `make test-integration-race` + payment | **PASS** |
| `./scripts/vs1_e2e.sh` | **VS1_E2E_PASS** |
| `make test-crash-recovery` | **PASS** A–E |
| buyer-web test/typecheck/build | **PASS** |

## Residual risks

- Grant is per joined buyer (not “next global FIFO head only”); same as pre-existing service GrantFIFO semantics.
- Self-grant remains a product convenience; production may still prefer worker-only issuance via policy/config later.
- Live browser e2e still optional.

## No commit / no push
