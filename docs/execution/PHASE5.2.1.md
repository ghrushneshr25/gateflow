# Phase 5.2.1 Report — Sale Administration & Admission Budgets

## Verdict: **COMPLETE**

## Objective

Extend Operator Console with authenticated sale lifecycle configuration and admission budget cap management using existing Sale/Admission APIs.

## Endpoint / scope map

| Method | Path | Scope | UI |
|---|---|---|---|
| GET | `/v1/offerings` | public | Sales list |
| GET | `/v1/offerings/{id}` | public | Detail |
| POST | `/v1/offerings` | `ops:sale:write` | Create draft |
| POST | `/v1/offerings/{id}/sessions` | `ops:sale:write` | Add session |
| GET | `/v1/offerings/{id}/sessions` | public | Detail |
| POST | `/v1/sessions/{id}/ticket-types` | `ops:sale:write` | Add ticket type |
| GET | `/v1/sessions/{id}/ticket-types` | public | Detail |
| POST | `/v1/offerings/{id}/publish` | `ops:sale:publish` (or write) | Publish confirm |
| GET | `/v1/admin/budgets/{scope}` | `ops:admission:budget` \| read \| fence | Load budget |
| PUT | `/v1/admin/budgets/{scope}` | `ops:admission:budget` | Update caps |
| GET | `/v1/admin/coordination` | `ops:admission:read` | Fence banner on budgets |

## Sale workflows

1. List offerings (all statuses).  
2. Create DRAFT offering.  
3. Select → add session (time windows defaulted) + ticket type (capacity/price).  
4. Confirm publish → inventory pools via backend.  
5. Display PUBLISHED vs DRAFT; no invent unsupported transitions.

## Budget behavior & fence

- PUT updates **caps only**; does not reset `grants_issued` / `active_checkouts` (server comment + UI copy).  
- Coordination fence is **independent**: budget UI shows FENCED mode and notes new grants stay blocked; cap updates still allowed.  
- UI never grants seats or bypasses fence.

## Verification

| Command | Result |
|---|---|
| `npm test -- --run` (operator-console) | **PASS** 13/13 |
| `npm run typecheck` | **PASS** |
| `npm run build` | **PASS** |
| Live stack smoke | **NOT RUN** |

## Residual

- No sale “unpublish” API (not invented).  
- Budget scope must exist (404 if session never touched).  
- sessionStorage JWT local-dev only.  
- Uncommitted with P5.1.x–P5.2.0.

## No commit / no push
