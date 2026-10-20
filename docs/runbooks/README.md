# Gateflow Runbooks

Operational recovery procedures. Expand per incident class as features land.

| Runbook | Status | Phase |
|---|---|---|
| Local stack bring-up | Planned | P1 |
| Admission Redis fence and resume | Planned | P4 |
| Order reconciliation backlog | Planned | P4 |
| Late payment success / compensation | Planned | P4 |
| Kafka consumer lag and replay | Planned | P4 |
| Inventory expiry worker delay | Planned | P3 |
| Manual compensation retry | Planned | P4–P5 |

**Rule:** Runbooks must match actual implemented recovery APIs and never instruct direct cross-service DB writes.
