# Runbook: Order reconciliation backlog

## Symptoms

- Orders stuck in `RECONCILIATION_REQUIRED` or long-lived `PAYMENT_PENDING`
- Operator Console → Reconciliation shows non-empty lists
- Buyers see non-CONFIRMED status after payment

## Inspect

```bash
# Operator token
export TOK=$(go run ./tools/devtoken operator ops:order:recover ops)

curl -s -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8084/v1/admin/reconciliation | jq .
```

Fields: `reconciliation_required[]`, `payment_pending[]` (order objects with `id`, `status`, `payment_operation_id`, …).

Single order:

```bash
curl -s -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8084/v1/orders/$ORDER_ID | jq .status,.payment_operation_id
```

## Safe recovery

```bash
curl -s -X POST -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8084/v1/orders/$ORDER_ID/reconcile | jq .
```

- Response `result` may be `resolved_confirmed`, `still_ambiguous`, etc.
- **Authoritative outcome is `order.status`**, not HTTP 200 alone.
- Reconcile is safe to repeat (idempotent Advance).

## Verify

- Status becomes `CONFIRMED` or remains non-confirmed with durable compensation op if seat expired.
- Never mark success because payment alone succeeded.

## Do not

- Write to another service’s database.
- Invent a new payment_operation_id.
- Treat timeout as definitive failure.

## Escalate when

- Same order stays ambiguous after multiple reconciles and payment lookup is also unknown.
- Inventory and payment disagree after compensation path exhausted.
