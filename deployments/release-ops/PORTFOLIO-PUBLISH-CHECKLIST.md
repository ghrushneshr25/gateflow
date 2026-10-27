# Portfolio publish checklist (Gateflow)

**Purpose:** Ship a reviewable GitHub repo for interviews — not a production release.  
**Rule:** No commit/push until the owner explicitly authorizes.

---

## 1. Current Git posture (inspect before acting)

| Item | Typical state |
|---|---|
| Branch | `main` (may be ahead of `origin`) |
| Secrets | `.env` gitignored; only `.env.example` tracked |
| Binaries / demo logs | `bin/`, `.demo-logs/` gitignored |
| Local phase reports | `docs/` gitignored (engineering evidence stays local) |
| Operator docs | `deployments/release-ops/` **tracked** (preferred for public ops story) |

Protect dirty work: no `git reset --hard`, no force-clean of uncommitted Phase 6/7 code.

---

## 2. Suggested commit slices (selective)

### Commit A — Portfolio surface (safe, high visibility)

- `README.md`
- `scripts/demo.sh`, `scripts/demo-down.sh`
- `Makefile` (demo targets)
- `.gitignore` (`.demo-logs/`)

**Message idea:** `docs: portfolio README and one-command local demo`

### Commit B — Lab hardening that demo/tests rely on

- `services/order/tests/crash/*` (isolation)
- `services/order/internal/outbox/runtime.go` (`GATEFLOW_KAFKA_TOPIC`)
- `services/order/cmd/server/main.go` (http start logging)
- `tools/bench/*` (classification wiring)
- `tools/p72evidence/*` + `go.work` entry
- `deployments/docker/tempo/tempo.yaml` (optional; small)

**Message idea:** `test: crash isolation, bench outcomes, evidence validator`

### Commit C — Operator package (resume “ops thinking”)

- `deployments/release-ops/*` (INDEX, SLO, staging plan, rollback, evidence contract, this checklist)

**Message idea:** `docs(ops): release-ops runbooks and evidence contract`

Do **not** force-add gitignored `docs/phase-*` unless you deliberately want huge local reports public.

---

## 3. Pre-commit checks

```bash
# no secrets
test ! -f .env
git status   # confirm .env not staged

# demo still works
./scripts/demo.sh
./scripts/demo-down.sh

# unit smoke
make test
go test ./tools/bench/ ./tools/p72evidence/
```

`demo-down` must only stop Gateflow `bin/*` PIDs / demo pidfiles — not arbitrary :808x apps.

---

## 4. Clone-to-demo prerequisites (document in README — already)

| Need | Notes |
|---|---|
| Docker | postgres, redis, kafka |
| Go | workspace Go 1.27.x locally |
| curl, python3 | VS1 + fence drill |
| Ports 8081–8085 | free for services |

`.env.example` is enough for local lab defaults.

---

## 5. Resume claim → evidence map

| Claim | Point to |
|---|---|
| Multi-service booking | `services/*`, README architecture |
| No oversell | bench scenario D / flash; inventory invariants |
| Fence fail-closed | `./scripts/demo.sh` step 7 / fence APIs |
| Crash recovery | `make test-crash-recovery` |
| Idempotent checkout | `scripts/vs1_e2e.sh` replay |
| Measured grants | `tools/bench` + classified outcomes |
| Ops discipline | `deployments/release-ops/` |
| Protocol A choice | README decisions; B off by default |

Avoid: “production ready”, “exactly-once”, “cloud staging passed”, absolute p95 SLOs without lab disclaimer.

---

## 6. After push (owner)

- Tag optional `lab-demo` or release note pointing at README demo  
- Pin GitHub description: “Multi-service Go booking system — admission, inventory, crash recovery”  
- Keep Protocol B disabled in all public defaults  
