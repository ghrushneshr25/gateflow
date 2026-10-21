# Runbook: Admission coordination fencing

## Symptoms

- Grant API returns `503 ADM_COORDINATION_FENCED`
- Operator Console → Coordination shows `mode: FENCED`
- Metrics: `fenced_grant_total` rising

## Inspect

```bash
export TOK=$(go run ./tools/devtoken operator ops:admission:read,ops:admission:fence ops)

curl -s -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8082/v1/admin/coordination | jq .

curl -s -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8082/v1/admin/coordination/metrics | jq .
```

Fields: `mode` (`ACTIVE`|`FENCED`), `epoch`, `fence_reason`, `fenced_at`, `fenced_by`.

## Fence (stop new grants)

```bash
curl -s -X POST -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{"reason":"incident-xyz"}' \
  http://127.0.0.1:8082/v1/admin/coordination/fence | jq .
```

- New `GrantFIFO` issuance fails closed while FENCED.
- Existing entitlement retries (already issued) may still succeed.

## Before unfence

1. Confirm incident resolved (capacity, Redis deferred N/A — PG fence is authority).
2. Confirm epoch/reason recorded.
3. Budget caps sensible (`/v1/admin/budgets/...`).

## Unfence

```bash
curl -s -X POST -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8082/v1/admin/coordination/unfence | jq .
```

**Never automatic on process restart.**

## Verify

- `mode` returns `ACTIVE`.
- New grants succeed for joined buyers.
- Epoch incremented on fence transitions.
