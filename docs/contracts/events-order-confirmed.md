# Event: order.order.confirmed

| Field | Value |
|---|---|
| event_type | `order.order.confirmed` |
| topic | `gateflow.order.order.confirmed` |
| partition key | `aggregate_id` / order id |
| producer | Order service (outbox publisher) |
| schema_version | 1 |
| envelope | `contracts/json-schema/event-envelope.schema.json` |
| payload | `contracts/json-schema/order-order-confirmed.payload.schema.json` |

## Delivery

- At-least-once via transactional outbox + Kafka.
- Stable `event_id` = `evt_order_confirmed_<order_id>`.
- Consumers must dedupe on `event_id`.
- Ordering: per-order partition key; no global order.

## Publisher claim

- `FOR UPDATE SKIP LOCKED` + `locked_until` / `locked_by` lease.
- `published_at` set only after Kafka ack.
- After 25 failed attempts → `quarantine=true` (operator-visible).
