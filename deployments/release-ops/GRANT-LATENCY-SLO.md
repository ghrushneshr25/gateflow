# Grant latency SLO definitions (Protocol A)

Two measurements are **not interchangeable**.

## 1. Grant-only latency

| Field | Definition |
|---|---|
| **Harness** | `tools/bench` scenario **G** |
| **Path** | `POST .../queue/joins` then `POST .../grants` only (no checkout) |
| **Boundary** | Client timer around the **grant** HTTP call |
| **Success filter** | HTTP 2xx grant; errors counted separately |
| **Provisional p95 target** | **&lt; 50 ms** at concurrency **1, 5, 10, 20** (local/staging starting point) |
| **P7.0 local result** | c10 **18.5 ms**, c20 **32.3 ms** (PASS vs 50 ms) |

## 2. Full-journey grant latency

| Field | Definition |
|---|---|
| **Harness** | `tools/bench` scenario **C** (or any full journey) |
| **Path** | Browse → join → grant → checkout under end-to-end load |
| **Boundary** | Client timer around the **grant** HTTP call **while** other journey steps and checkouts contend for PG/CPU |
| **Provisional p95 target** | **Not the same as grant-only 50 ms** unless explicitly approved for a named concurrency |
| **P7.0 local result** | c10 **20.9 ms**, c20 **100.6 ms** |

## 3. Checkout latency (journey success path)

| Field | Definition |
|---|---|
| **Harness** | Scenario C/D checkout op |
| **Boundary** | Full `POST /v1/checkouts` |
| **Provisional p95** | **&lt; 200 ms** at moderate concurrency (c10 default “moderate”) |
| **P7.0** | c10 **97.3 ms**, c20 **124.8 ms** |

## 4. Decisions locked for P7.1

1. **Moderate concurrency** for provisional gates means **c=10** unless a staging plan says otherwise.
2. The **50 ms grant p95** target applies to **grant-only (scenario G)** by default.
3. Full-journey grant p95 at **c20 ≈ 100.6 ms** is **recorded capacity behavior**, not an automatic waiver and not an automatic P0. Staging must re-measure and either:
   - keep c20 full-journey grant under an **explicitly approved** higher target, or
   - treat it as a capacity limit (reduce offered load / scale PG), **without** a coord-lock redesign in P7.x.
4. Protocol **B** remains disabled; no B latency comparison in staging gates.

## 5. Reporting requirements

Every bench JSON must label:

- `scenario` (G vs C)
- concurrency step
- which op name is grant (`cN_grant` vs `grant`)
- `checkout_outcomes` classification when checkout is in scope
