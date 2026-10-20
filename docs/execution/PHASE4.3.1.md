# Phase 4.3.1 Report — Kafka Recovery & Poison Replay

## A. Startup / reconnect

`outbox.Runtime`:
- Retries `EnsureTopic` with exponential backoff + jitter until ctx cancel
- Starts **one** publisher + **one** consumer when Kafka becomes ready (`started` atomic)
- Health probe every 5s; on failure shuts workers and reconnects (no duplicate loops)
- Graceful shutdown via parent context cancel closes producer/reader

## B. Poison replay

| Endpoint | Auth |
|---|---|
| `GET /v1/admin/outbox/quarantined` | JWT `ops:outbox:read` or `ops:outbox:replay` |
| `POST /v1/admin/outbox/events/{event_id}/replay` | JWT `ops:outbox:replay` |
| `GET /v1/admin/outbox/metrics` | same read scopes |

Replay: `FOR UPDATE`, clear quarantine, keep `event_id`/payload, `replay_count++`, audit in `last_error` / `last_replay_*`.  
Quarantine threshold: `attempt_count >= 25 + replay_count*10`.

## Verification (executed)

| Test | Result |
|---|---|
| `TestP431_ReplayQuarantinedPreservesEventID` | PASS |
| `TestP431_RuntimeRecoversWhenKafkaAppears` | PASS |
| `TestRuntimeShutdownCancels` | PASS |
| Prior P43/P41 integration | PASS |
| make test / build | PASS |
| vs1_e2e | PASS |
| Live spoof quarantine list | 401 |
| Live JWT list | 200 |

## Limitations

- Full “start app with Kafka down then start Kafka” OS-level test approximated by Runtime ready loop (not docker stop/start automation)
- Not committed/pushed

## Next

P4.5 process-kill harness
