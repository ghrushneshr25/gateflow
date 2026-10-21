# Runbook: Payment ambiguity and compensation

## Principles

- **Timeout ≠ failure.**
- **Payment SUCCEEDED ≠ order CONFIRMED.**
- Stable `payment_operation_id` / `comp_<payment_operation_id>`.

## Inspect payment

```bash
# Service token with payment:create or recover
export TOK=$(go run ./tools/devtoken service payment:create,ops:payment:recover svc)

curl -s -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8085/v1/payments/by-operation/$PAYMENT_OP | jq .status,.scenario
```

## Inspect order

```bash
export OPS=$(go run ./tools/devtoken operator ops:order:recover ops)

curl -s -H "Authorization: Bearer $OPS" \
  http://127.0.0.1:8084/v1/orders/$ORDER_ID | jq .status,.payment_operation_id,.compensation_operation_id,.reservation_id
```

## Pending / unknown

1. Do not create a new payment_operation_id.
2. `POST /v1/orders/{id}/reconcile` with operator or owning buyer JWT.
3. Optionally `POST /v1/payments/resolve-due` (ops:payment:recover) for delayed success scenarios.

## Late success + expired seat

- Reconcile → inventory confirm fails → compensation ensured.
- Order stays **not CONFIRMED**; `compensation_operation_id` set when durable.

```bash
curl -s -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8085/v1/compensations/by-operation/comp_$PAYMENT_OP | jq .
```

## Manual compensation create/retry

```bash
curl -s -X POST -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d "{\"payment_operation_id\":\"$PAYMENT_OP\",\"order_id\":\"$ORDER_ID\",\"execute\":true}" \
  http://127.0.0.1:8085/v1/compensations | jq .
```

Retry (if failed/escalated):

```bash
curl -s -X POST -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8085/v1/compensations/by-operation/comp_$PAYMENT_OP/retry | jq .
```

If status `UNKNOWN`, query before force-retry.

## Do not

- Confirm order from payment UI alone.
- Revive expired reservations.
- Double-charge by minting a new payment op for the same checkout.
