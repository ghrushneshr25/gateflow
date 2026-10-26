# Phase 5.4.2 Report — Deeper Tracing and Log Correlation

## Verdict: **COMPLETE**

## HEAD / branch

- Baseline HEAD at start: `2a88a01` (Phase 5 commit)
- Branch: `main` ahead of `origin/main` by 13
- Working tree: **dirty with P5.4.2 only** (uncommitted)
- **No commit / no push**

## Design

### Kafka W3C propagation

| Boundary | Behavior |
|---|---|
| `Store.Confirm` | Captures W3C carrier JSON into nullable `outbox.trace_carrier` |
| `Publisher.publishRow` | `PublishWithTrace`: restore carrier → producer span → inject `traceparent`/`tracestate` + `correlation_id` headers |
| `Consumer.processMessage` | Extract headers → **new root** + **span link** → apply effect → commit |
| Missing/malformed headers | Processing continues |

### Async parent/link policy

- **Publish** (background): child of checkout when carrier present (same trace).
- **Consume**: `WithNewRoot` + `Link` to remote context (async side effect).
- **Retry/replay**: stable event_id/payload; carrier preserved on row; each attempt new spans.

### Log correlation

- `httpx.Logger` → `correlation_id`, `trace_id`, `span_id`
- Outbox publish/consume wired
- `logging.New` → `environment`; optional `GATEFLOW_LOG_DIR/<service>.log`
- Correlation ULID ≠ trace_id; never used as Prometheus labels

### Loki shipping (verified)

```
GATEFLOW_LOG_DIR/*.log (JSON)
  → Promtail (labels: job, environment, service, level)
  → Loki :3100
  → Grafana Loki DS
```

OTLP logs→Loki on collector is secondary path.

## Live evidence (this milestone)

| Check | Result |
|---|---|
| VS1 + OTEL + LOG_DIR | **VS1_E2E_PASS** `ord_01M5FW9PEV5CK3H6QSS77Q6R7W` |
| `outbox.trace_carrier` | `{"traceparent":"00-32dcfcc1f7a2abfcecdb1b0d21c80601-…"}` |
| Tempo checkout trace `32dcfcc1…` | spans: `POST /v1/checkouts`, **`messaging.publish`** |
| Tempo consumer `0fc745c1…` | span: **`messaging.process`**, **links=1** |
| Loki query (reproducible) | see below |

### Loki query (reproducible)

```bash
CORR=01M5FW9PEKD3EFD7MDR0N8EY9D
curl -sG 'http://localhost:3100/loki/api/v1/query_range' \
  --data-urlencode "query={job=\"gateflow\"} |= \"$CORR\"" \
  --data-urlencode 'limit=20' \
  --data-urlencode "start=$(python3 -c 'import time; print(int((time.time()-600)*1e9))')" \
  --data-urlencode "end=$(python3 -c 'import time; print(int(time.time()*1e9))')"
```

**Observed:** `status=success`, `streams=1`, labels include `job=gateflow`, `service=order`, `environment=local`, `level=INFO`. Log lines included:

- `outbox published` with `correlation_id=01M5FW9PEKD3EFD7MDR0N8EY9D`, `event_id=evt_order_confirmed_ord_01M5FW9PEV5CK3H6QSS77Q6R7W`
- `processed order confirmed event` with same `correlation_id`, plus `trace_id=0fc745c14a6de66e859456608e663480`

| Loki result summary | streams=1; published + processed lines for correlation ULID above |
| Collector stopped + VS1 | booking still **PASS** (isolation) |

## Verification commands

| Command | Result |
|---|---|
| `make test` | **PASS** |
| `make build` | **PASS** |
| `make compose-config` | **PASS** |
| telemetry/kafkax/httpx unit | **PASS** |
| `./scripts/vs1_e2e.sh` | **PASS** |
| `make test-crash-recovery` | **PASS** A–E |
| `make test-integration-race` | **PASS** |
| promtool (unchanged rules) | prior P5.4.1 still valid |

## Failure isolation

- Collector down: VS1 still confirms orders
- Malformed trace headers: unit-tested; consumer continues
- Loki/Promtail down: apps keep stdout/file logs

## Residual

- P5.4.1 shared metric registry zeros
- Optional deeper orchestrate internal spans
- External alert notifiers

## No commit / no push
