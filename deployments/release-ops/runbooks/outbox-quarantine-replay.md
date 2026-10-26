# Runbook: Outbox quarantine and replay

## Symptoms

- `GET /v1/admin/outbox/metrics` shows rising `quarantine_count` or `oldest_age_ms`
- Events stuck unpublished after Kafka/publish errors

## Inspect

```bash
export TOK=$(go run ./tools/devtoken operator ops:outbox:read,ops:outbox:replay ops)

curl -s -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8084/v1/admin/outbox/metrics | jq .

curl -s -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8084/v1/admin/outbox/quarantined | jq .
```

Note `event_id` (stable), `attempt_count`, `replay_count`, `last_error`, `aggregate_id` (order id).

## When to replay

- Root cause of publish failure is fixed (Kafka up, schema OK).
- Event still unpublished / quarantined.
- You accept redelivery of the **same** `event_id` (consumer must dedupe).

## Replay

```bash
curl -s -X POST -H "Authorization: Bearer $TOK" \
  http://127.0.0.1:8084/v1/admin/outbox/events/$EVENT_ID/replay | jq .
```

Expect `result: queued_for_publish`. Payload and `event_id` are preserved.

## Verify

- Event leaves quarantine and eventually has `published_at` (metrics / DB).
- Consumer applies at-most-once effect (`order_confirmed_effects` count ≤ 1 per order).

## Do not

- Edit payload in the browser or invent a new event_id.
- Disable consumer dedupe.
- Replay endlessly without fixing the underlying publish error.
