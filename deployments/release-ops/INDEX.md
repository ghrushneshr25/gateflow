# Gateflow release-ops package

**Purpose:** Operator-accessible runbooks and procedures that do **not** depend on the gitignored `docs/` tree.

**Location:** `deployments/release-ops/` (tracked in git)

**Protocol default:** Admission **Protocol A only**. Do not enable Protocol B without a separate CTO decision and new evidence.

## Contents

| Path | Description |
|---|---|
| `runbooks/` | Operational recovery procedures |
| `runbooks/README.md` | Runbook index |
| `runbooks/admission-coordination-fence.md` | Fence / unfence |
| `runbooks/admission-grant-timeout-reconcile.md` | Grant timeout = uncertain |
| `runbooks/outbox-quarantine-replay.md` | Outbox quarantine + replay |
| `runbooks/payment-ambiguity-compensation.md` | Payment ambiguity |
| `runbooks/reconciliation-backlog.md` | Order reconciliation backlog |
| `runbooks/observability-local.md` | Local/obs stack (traces, RED, Loki) |
| `GRANT-LATENCY-SLO.md` | Grant-only vs full-journey latency definitions |
| `STAGING-RUN-PLAN.md` | Shared staging validation plan |
| `ROLLBACK-PLAN.md` | Artifact/config rollback procedure |
| `P7.2-EVIDENCE-CONTRACT.md` | Gate IDs, proof artifacts, pass/fail rules for P7.2 |

## How operators access this package

1. **Preferred:** clone/checkout this repository; open `deployments/release-ops/`.
2. **CI artifact:** package this directory into the release tarball / image sidecar if ops cannot clone.
3. **Do not** assume files under local `docs/` (gitignored) are available to the release team.

## Related local-only evidence

Phase 6/7 engineering reports and bench JSON under `docs/phase-6/` and `docs/phase-7/` may exist on a developer machine only. They are **not** the operator package.
