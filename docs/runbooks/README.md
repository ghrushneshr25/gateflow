# Gateflow Runbooks

Operational recovery procedures. Commands must match implemented APIs.

| Runbook | File | Status |
|---|---|---|
| Order reconciliation backlog | [reconciliation-backlog.md](./reconciliation-backlog.md) | **P5.4.0** |
| Outbox quarantine and replay | [outbox-quarantine-replay.md](./outbox-quarantine-replay.md) | **P5.4.0** |
| Admission coordination fence | [admission-coordination-fence.md](./admission-coordination-fence.md) | **P5.4.0** |
| Payment ambiguity / compensation | [payment-ambiguity-compensation.md](./payment-ambiguity-compensation.md) | **P5.4.0** |
| Local stack + observability (traces + RED/alerts) | [observability-local.md](./observability-local.md) | **P5.4.0 / P5.4.1** |
| Inventory expiry worker delay | — | Planned |
| Admission Redis fence | — | Deferred (PG fence is authority) |

**Rule:** Never instruct direct cross-service DB writes. Use service APIs + operator JWT scopes.
