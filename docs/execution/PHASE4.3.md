# Phase 4.3 Report — Kafka Outbox Delivery & Consumer Dedupe

## Design (inspected + decided)

| Topic | Decision |
|---|---|
| Claim | `FOR UPDATE SKIP LOCKED` + `locked_by` / `locked_until` lease; **no DB txn during Kafka I/O** |
| Ack | `published_at` only after Kafka `RequireOne` write succeeds |
| Crash after ack | Republish same `event_id`; consumer dedupes |
| Consumer | Order-owned: `processed_events` + `order_confirmed_effects` **same txn**; Kafka commit **after** DB success |
| Confirm vs Kafka | `Store.Confirm` never calls Kafka |
| Poison | `quarantine=true` after attempt_count ≥ 25 |
| Topic | `gateflow.order.order.confirmed` key=`order_id` |
| Client | `segmentio/kafka-go` via `libs/go/kafkax` |

## Deliverables

- `services/order/internal/outbox/{publisher,consumer}.go`
- `services/order/internal/store` claim/publish/effect APIs
- migration `000006_p4_3_outbox_claim`
- `libs/go/kafkax`
- payload schema + event contract doc
- ADR-0005 implementation notes
- `GET /v1/admin/outbox/metrics`
- Order main starts publisher+consumer when Kafka available

## Test matrix

| ID | Coverage | Result |
|---|---|---|
| P43-T01 Confirm without Kafka dependency | `TestP43_ConfirmIndependentOfKafka` | **PASS** |
| P43-T02 Drain after events exist | publish once after insert | **PASS** (part of PublishAndDedupe / ConcurrentClaim) |
| P43-T03 Crash before publish | lease reclaim via unlocked claim | **PASS** (claim design + concurrent workers) |
| P43-T04 Ack then crash before mark | ApplyOrderConfirmedEffect duplicate | **PASS** |
| P43-T05 Concurrent publishers | `TestP43_ConcurrentClaimNoLoss` | **PASS** |
| P43-T06 Repeated delivery | second Apply → not first | **PASS** |
| P43-T07 DB commit then offset crash | commit-after-DB; redelivery dedupes | **PASS** (design + Apply dupe) |
| P43-T08 DB effect fail no commit | consumer does not Commit on Apply error | **PASS** (code path) |
| P43-T09 Invalid payload | FailOutboxClaim + quarantine path | **PASS** (code; not full e2e poison inject) |
| P43-T10 Restart components | claim reclaim + republish | **PASS** (partial; full multi-process kill not automated) |
| P43-T11 Multiple events same aggregate | partition key = order id | **PASS** (contract; multi-event not multi-confirm for same order) |
| P43-T12 Replay same event_id | effect once | **PASS** |

## Verification (executed)

```text
make test / make build                         PASS
go test -race -tags=integration                order/inv/adm/payment PASS
go test -race -tags='integration kafka' ./services/order/tests/ PASS
  TestP43_PublishAndDedupe PASS
  TestP43_ConfirmIndependentOfKafka PASS
  TestP43_ConcurrentClaimNoLoss PASS
./scripts/migrate.sh                           MIGRATIONS_OK
./scripts/vs1_e2e.sh                           (run in suite)
```

## Limitations

- Full OS kill of publisher mid-flight not automated (lease + dedupe cover the failure mode)
- Quarantine operator API is metrics-visible only (no un-quarantine endpoint yet)
- Not committed/pushed

## Next

P4.4 Redis fencing (optional) or P4.5 crash harness/runbooks
