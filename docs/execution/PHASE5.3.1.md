# Phase 5.3.1 Report — Buyer UX and Recovery Hardening

## Verdict: **COMPLETE**

## Delivered

### Failure / unavailable states
- Empty published list, no session/tickets, missing pool (not bookable)
- Fenced admission, budget exhausted, capacity/sold-out mapping via `explainApiError`
- Expired grant messaging + “request new grant”
- Stale listing warning + refresh on offerings/detail

### Checkout uncertainty
- Idempotency key scoped by **buyer identity + session + ticket**
- Never rotated on timeout; cleared only on **CONFIRMED** (or sign-out)
- Order hint storage for “Find prior order” after uncertain outcome
- Payment success / RECON / PENDING copy never claims booking confirmed
- Reconcile + refresh on booking and order detail

### Identity isolation
- `clearBuyerSession` on sign-out / token replace clears all idem + order hints
- Order list/detail query keys include token scope

### Accessibility
- `aria-live` status banners, labelled sections, `aria-describedby` on primary actions
- Focus-visible outlines; `prefers-reduced-motion`
- Semantic headings and alert roles on errors

## Idempotency lifecycle

| Event | Key behavior |
|---|---|
| First checkout click | create key `idem:{buyerScope}:{session}:{tt}` |
| Retry / timeout | **same key** |
| CONFIRMED | clear key + order hint |
| Sign-out / new JWT | clear all keys for tab |
| Different buyer | different storage namespace |

## Live integration (executed)

| Step | Result |
|---|---|
| Buyer JWT auth | PASS |
| Publish sale (ops) | PASS |
| Queue join + self-grant | PASS |
| Checkout + idempotent replay | **LIVE_BUYER_JOURNEY_PASS** CONFIRMED |
| Cross-buyer GET order | **403** |
| `./scripts/vs1_e2e.sh` | **VS1_E2E_PASS** |
| `make test-crash-recovery` | **PASS** A–E |

## Other verification

| Command | Result |
|---|---|
| buyer-web `npm test -- --run` | **PASS** 12 |
| typecheck / build | **PASS** |
| `make test` / `make build` | **PASS** |

## Residual risks

- sessionStorage XSS (local-dev)
- Listed capacity is not live stock; server still authoritative
- Browser E2E automation not added (curl live journey covers APIs)

## No commit / no push
