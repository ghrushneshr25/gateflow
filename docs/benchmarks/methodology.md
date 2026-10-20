# Gateflow Benchmark Methodology

**Status:** Methodology only — **no performance results claimed yet**  
**Related:** ADR-0023

## Provisional local latency targets (not capacity claims)

| Operation | p95 | p99 |
|---|---:|---:|
| Admission grant | 100 ms | 250 ms |
| Inventory reservation API | 100 ms | 250 ms |
| Order API synchronous response | 250 ms | 500 ms |

Order API target ≠ end-to-end checkout completion.

## Every run must declare

- Host and container resource limits  
- Runtime and dependency versions  
- Dataset size  
- Request mix  
- Concurrency  
- Workload duration  
- Warm-up interval  
- Measurement interval  
- Repetitions  
- Error-rate calculation  
- Latency calculation (e.g., HDR histogram)  
- Resource utilization  
- **Correctness assertions** (zero oversell, zero limit violation)

## Required scenarios (Phase 6)

1. Steady-state load  
2. Hot-session contention  
3. Uneven traffic across sessions  
4. Global budget exhaustion  
5. Per-session budget exhaustion  
6. Slow downstream dependency  
7. Concurrent scarce inventory  
8. Concurrent purchase-limit enforcement  
9. Service restart  
10. Redis outage and recovery  
11. Kafka outage, lag, replay  
12. Lost response after commit  
13. Delayed payment success  
14. Reservation expiry vs confirmation  
15. Compensation failure and manual recovery  

Initial soak: **30 minutes**.  
Any correctness violation = failed run regardless of latency.
