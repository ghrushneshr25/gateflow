# Gateflow

**High-demand event booking platform** — multi-service Go backends plus separate **Buyer** and **Admin** web apps, designed to stay correct under contention and failure.

Portfolio / systems-engineering showcase: controlled admission, scarce inventory, recoverable checkout, operator accounts, Google buyer sign-in, crash tests, load benches, Compose + local Kubernetes.

**Not** a claim of cloud staging, multi-region HA, or production certification.

**Repository:** [github.com/ghrushneshr25/gateflow](https://github.com/ghrushneshr25/gateflow)

---

## Why this project

Ticket drops fail when systems oversell, double-charge, or treat timeouts as known outcomes. Gateflow focuses on:

| Concern | Approach |
|---|---|
| Scarce inventory | PostgreSQL capacity; **confirmed + reserved ≤ sellable** |
| Admission under load | FIFO grants, durable budgets, **coordination fence** |
| Checkout | Orchestrated path; **payment success ≠ order confirm** |
| Ambiguity | Timeout = **uncertain** → reconcile with stable op IDs |
| Delivery | Transactional **outbox** + Kafka, at-least-once + dedupe |
| Failure | Process-kill crash suite (A–E) |
| Performance | Load harness with **classified** checkout outcomes |
| Identity | **Admins:** username/password · **Buyers:** Google (3rd-party only) |

**Default grant protocol: A.** Experimental protocol B stays **off** (lab benches did not beat A).

---

## Architecture

```text
  Buyer Web (:8088)              Admin Console (:8089)
  Google → sale /auth            user/pass → sale /auth
        │                              │
        └──────────┬───────────────────┘
                   ▼
        ┌─────────────────────┐  publish   ┌────────────┐
        │  Sale :8081         │ ─────────► │ Inventory  │
        │  offerings, auth,   │            │  :8083     │
        │  operator users,    │            └────────────┘
        │  buyer_identities   │                   ▲
        └──────────┬──────────┘                   │ hold/confirm
                   │ join / grant                   │
                   ▼                               │
        ┌─────────────────────┐                   │
        │  Admission :8082    │  entitlement ─────┤
        └──────────┬──────────┘                   │
                   ▼                               │
        ┌─────────────────────┐  pay          ┌───┴────────┐
        │  Order :8084        │ ────────────► │ Payment    │
        │  checkout + outbox  │               │ sim :8085  │
        └──────────┬──────────┘               └────────────┘
                   │
                   ▼ Kafka  order.confirmed (deduped effects)

  PostgreSQL = durable authority for booking + admin users + buyer OAuth links
  Kafka      = async notifications (not the source of truth)
  Redis      = optional in Compose; never grant/budget/inventory authority
```

| Component | Role |
|---|---|
| **Sale** | Catalog, publish, **operator users**, **buyer Google auth**, inventory pool create |
| **Admission** | Queue, entitlements, budgets, fence |
| **Inventory** | Pools, reservations, confirm/release |
| **Order** | Checkout, purchase limits, outbox, reconcile |
| **Payment simulator** | Lab payments / compensation |
| **Buyer Web** | Google sign-in, browse, book |
| **Admin Console** | Login, create events, users, fence, recovery |

---

## Identity model

### Admins (local accounts)

| | |
|---|---|
| Storage | `sale.operator_users` (bcrypt password hashes) |
| Bootstrap | If empty DB → **`admin` / `GateflowAdmin1!`** (override with env) |
| Login | `POST /v1/auth/login` `{ "username", "password" }` → operator JWT |
| Create more | `POST /v1/admin/users` (needs `ops:users:write`) or **Admin → Users** |
| UI | http://127.0.0.1:8089 → username/password |

```bash
# optional overrides
GATEFLOW_ADMIN_USERNAME=admin
GATEFLOW_ADMIN_PASSWORD=GateflowAdmin1!
GATEFLOW_DEV_JWT_SECRET=gateflow-dev-secret-change-me
```

### Buyers (third-party only — Google first)

| | |
|---|---|
| Local password | **None** |
| Flow | Google ID token → **sale verifies** → upsert `buyer_identities` → buyer JWT |
| APIs | `GET /v1/auth/buyer/providers` · `POST /v1/auth/buyer/google` `{ "id_token" }` |
| Stable id | `buyer_id = google_<sub>` (stable across logins) |
| Purchases | Durable in `order.orders` keyed by `buyer_id` |

```bash
# required for Google button
GATEFLOW_GOOGLE_CLIENT_ID=xxxx.apps.googleusercontent.com   # sale service
VITE_GOOGLE_CLIENT_ID=xxxx.apps.googleusercontent.com       # buyer-web build (same value)
```

Google Cloud: OAuth **Web** client; authorized JS origins e.g. `http://127.0.0.1:8088`, `http://localhost:5173`.

More OAuth providers can follow the same `buyer_identities` + `/v1/auth/buyer/<provider>` pattern.

---

## End-to-end product path

1. **Admin** signs in → **Create event** (name, capacity, price) → publish  
2. **Buyer** signs in with **Google** → Offerings → queue → grant → checkout **CONFIRMED**  
3. Orders listed for that buyer JWT; inventory never oversells  

---

## Quick start — Compose (fastest laptop demo)

**Needs:** Docker, Go 1.22+ (workspace uses 1.27), curl, python3.

```bash
git clone https://github.com/ghrushneshr25/gateflow.git
cd gateflow
cp .env.example .env
chmod +x scripts/*.sh

./scripts/demo.sh          # postgres/kafka + 5 APIs + VS1 + fence
./scripts/demo-down.sh     # stop host APIs on :8081–:8085
```

### UIs on Compose

```bash
# terminals after demo.sh (APIs up)
npm --prefix apps/operator-console install && npm --prefix apps/operator-console run dev
# Admin → http://127.0.0.1:5174 (or port Vite prints) — sign in admin / GateflowAdmin1!

npm --prefix apps/buyer-web install && npm --prefix apps/buyer-web run dev
# Buyer → set VITE_GOOGLE_CLIENT_ID in apps/buyer-web/.env then restart Vite
```

API bases default to `http://127.0.0.1:8081` (sale), `8082` (admission), `8084` (order).

### What `demo.sh` proves

1. Data plane up  
2. Services healthy; admission **`grant_protocol=A`**  
3. VS1 → order **CONFIRMED** + idempotent replay  
4. Fence → grant **503** → unfence → grant OK  

### Deeper lab

| Command | Purpose |
|---|---|
| `make test` | Unit tests |
| `make test-integration` | DB integration |
| `make test-crash-recovery` | Crash A–E (free :8081–:8085 first) |
| `go build -o bin/bench ./tools/bench && GATEFLOW_DEMO_FLASH=1 ./scripts/demo.sh` | Flash capacity |
| `docker compose --profile obs up -d` | Host Grafana :3000, Prom :9090, Tempo :3200 |

---

## Local Kubernetes (kind + Helm)

Lab only — not cloud staging.

```bash
./scripts/k8s-all-up.sh      # tools → kind → images → app + obs → smoke → port-forwards
# Ctrl-C stops forwards; cluster stays up

./scripts/k8s-ui-up.sh       # browser access without full re-run
./scripts/k8s-all-down.sh    # uninstall releases (PVC kept by default)
```

Step-by-step: [`scripts/K8S-README.md`](scripts/K8S-README.md)

| Script | Role |
|---|---|
| `k8s-tools-setup.sh` | kind + helm → `./.tools` |
| `k8s-cluster-up.sh` | kind cluster `gateflow` |
| `k8s-build-images.sh` | build + `kind load` |
| `k8s-app-up.sh` | Helm app → ns **`gateflow`** |
| `k8s-obs-up.sh` | Helm obs → ns **`gateflow-obs`** |
| `k8s-smoke.sh` | Protocol A, VS1, fence, flash |
| `k8s-ui-up.sh` / `k8s-ui-down.sh` | Port-forwards for browser |

### Browser URLs (after `k8s-ui-up.sh`)

| App | URL |
|---|---|
| **Buyer Web** | http://127.0.0.1:8088 |
| **Admin Console** | http://127.0.0.1:8089 |
| Sale / Admission / Order APIs | :8081 / :8082 / :8084 |
| Grafana (obs ns) | http://127.0.0.1:3000 (`admin`/`admin`) |
| Prometheus | http://127.0.0.1:9090 |

Namespaces stay separate: **`gateflow`** (booking) · **`gateflow-obs`** (metrics/traces/logs).

Helm: `deployments/helm/gateflow`, `deployments/helm/gateflow-obs`.  
Set Google client id for K8s:

```bash
helm upgrade --install gateflow deployments/helm/gateflow -n gateflow \
  -f deployments/helm/gateflow/values-local.yaml \
  --set google.clientId=YOUR_CLIENT_ID.apps.googleusercontent.com \
  --set imageTag=p8-local --set imagePullPolicy=Never
# rebuild buyer-web image with same VITE_GOOGLE_CLIENT_ID
```

---

## Important APIs (auth)

```http
# Admin
POST /v1/auth/login
{ "username": "admin", "password": "GateflowAdmin1!" }

POST /v1/admin/users
Authorization: Bearer <operator-jwt>
{ "username": "alice", "password": "SomeSecure1!", "display_name": "Alice" }

GET /v1/admin/users

# Buyer (Google)
GET  /v1/auth/buyer/providers
POST /v1/auth/buyer/google
{ "id_token": "<Google GIS credential>" }
```

All booking mutations still require the correct **Gateflow JWT** (operator or buyer). No `X-Buyer-ID` spoofing.

---

## Design decisions (interview anchors)

1. **PostgreSQL** owns durable booking truth — Redis is not the grant ledger.  
2. **Payment success never confirms alone** — inventory confirm is on the path.  
3. **Timeout ≠ failure** — reconcile, then decide.  
4. **Outbox + at-least-once Kafka** — stable event IDs + consumer dedupe.  
5. **Fence fail-closed** — no new grants while fenced.  
6. **Protocol A default**; B experimental and off.  
7. **Buyers = OAuth only** (Google first); **admins = local users** with bootstrap + create-user.  
8. **Evidence over slogans** — classified bench outcomes; unclassified blocks fake 5xx SLOs.

---

## Non-negotiable invariants

```text
confirmed_qty + active_reserved_qty  ≤  sellable_capacity
committed + held + requested         ≤  effective purchase limit
```

Expired reservations do not revive; duplicate delivery must not double-apply effects; services do not write each other’s databases.

---

## Repository layout

```text
services/          sale, admission, inventory, order, payment-simulator
libs/go/           authn, httpx, kafkax, metrics, telemetry, …
apps/buyer-web     Buyer UI (Google)
apps/operator-console  Admin UI (login, events, users, fence, recovery)
deployments/docker Compose + Dockerfiles
deployments/helm   gateflow + gateflow-obs charts
deployments/release-ops  runbooks, evidence contract
tools/             bench, devtoken, p72evidence
scripts/           demo, k8s-*, migrate, vs1_e2e
```

---

## Environment reference

| Variable | Used by | Notes |
|---|---|---|
| `GATEFLOW_DEV_JWT_SECRET` | All services | HMAC for Gateflow JWTs |
| `GATEFLOW_GOOGLE_CLIENT_ID` | Sale | Google OAuth **Web** client id |
| `VITE_GOOGLE_CLIENT_ID` | Buyer build | Same as above |
| `GATEFLOW_ADMIN_USERNAME` | Sale bootstrap | Default `admin` |
| `GATEFLOW_ADMIN_PASSWORD` | Sale bootstrap | Default `GateflowAdmin1!` |
| `DATABASE_URL` / `*_DATABASE_URL` | Services | Postgres + search_path |
| `KAFKA_BROKERS` / `KAFKA_ENABLED` | Order | Outbox |
| `INVENTORY_BASE_URL` | Sale | In-cluster or `http://127.0.0.1:8083` |
| `GATEFLOW_ADMISSION_GRANT_PROTOCOL` | Admission | `A` (default) |

See [`.env.example`](.env.example).

---

## Lab evidence (honest scope)

Local / kind measurements only — not production SLOs:

| Signal | Lab result class |
|---|---|
| VS1 checkout | **CONFIRMED** |
| Fence | Fail-closed then recover |
| Flash capacity | confirmed ≤ N (e.g. 5/5) |
| Crash A–E | Pass on isolated local harness |
| kind deploy | App + optional obs namespaces |

---

## License

Proprietary / unpublished unless otherwise stated.
