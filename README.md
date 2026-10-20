# Gateflow

High-demand booking and admission orchestration platform.

Gateflow demonstrates distributed-systems correctness under contention and failure: controlled admission, scarce-inventory allocation, recoverable checkout, observability, and reproducible performance engineering.

**Repository:** [github.com/ghrushneshr25/gateflow](https://github.com/ghrushneshr25/gateflow)  
**Go module prefix:** `github.com/ghrushneshr25/gateflow`

## Status

| Phase | Focus | Status |
|---|---|---|
| 0 | Specification consolidation | **Complete** |
| 1 | Repository foundation | **Complete** |
| 2 | First vertical slice (FIFO booking) | **Complete (backend E2E)** |
| 3 | Concurrency and invariants | **Complete (verified)** |
| 4 | Failure and recovery | Pending |
| 5 | Product polish and operations | Pending |
| 6 | Performance and resilience | Pending |
| 7 | Deployment maturity (K8s) | Pending |

## Architecture (summary)

Five independently deployable Go services:

| Service | Owns |
|---|---|
| Sale | Offerings, sessions, policies, publication |
| Admission | Queues, entitlements, admission budgets |
| Inventory | Capacity, reservations, confirm/release |
| Order | Checkout orchestration, allowances, reconciliation |
| Payment Simulator | Simulated payments and compensation |

Two React + TypeScript apps: **Buyer Web**, **Operator Console**.

Data plane: PostgreSQL (authoritative), Kafka (events), Redis (admission coordination only).

## Documentation

- [Product requirements](docs/product/PRD.md)
- [Architecture overview](docs/architecture/overview.md)
- [Service contracts (GF-SCS-001)](docs/contracts/GF-SCS-001.md)
- [ADRs](docs/adr/)
- [Phase 0 report](docs/execution/PHASE0.md)
- [Execution plan](docs/execution/PLAN.md)
- [Security](docs/security/overview.md)
- [Testing](docs/testing/strategy.md)
- [Benchmarks methodology](docs/benchmarks/methodology.md)
- [Runbooks](docs/runbooks/)

## Non-negotiable invariants

```
confirmed + active_reserved <= sellable_capacity
committed + held + requested <= effective_purchase_limit
```

Payment success alone does not confirm an order. Timeouts are uncertainty, not failure.

## Local development

```bash
cp .env.example .env
make build test
docker compose up -d postgres redis
./scripts/migrate.sh
# terminals: ./bin/inventory ./bin/sale ./bin/admission ./bin/payment-simulator ./bin/order
./scripts/vs1_e2e.sh   # happy-path booking
npm --prefix apps/buyer-web install && npm --prefix apps/buyer-web run dev
npm --prefix apps/operator-console install && npm --prefix apps/operator-console run dev
```

Observability stack (optional): `docker compose --profile obs up -d`

See `docs/execution/PHASE0.md` and `docs/execution/PHASE1.md` for verification evidence.

## License

Proprietary / unpublished unless otherwise stated.
