# Gateflow — Product Requirements Document (PRD)

**Document ID:** GF-PRD-001  
**Product:** Gateflow  
**Version:** 1.1 — Phase 0 consolidated baseline  
**Status:** Accepted for implementation (see docs/execution/PHASE0.md)  
**Document owner:** Product & Engineering  
**Architecture reference:** GF-SCS-001 — Service Contract Specification  
**Target platform:** Web  
**Primary implementation language:** Go  
**Initial deployment:** Docker Compose  
**Intended audience:** Product, engineering, QA, platform, security, and technical reviewers

---

# 1. Executive Summary

Gateflow is a high-demand booking and admission orchestration platform designed to manage the lifecycle of scarce inventory under unpredictable traffic.

The platform supports booking scenarios such as concert tickets, sports events, movie screenings, conferences, theatre performances, exhibitions, and other scheduled experiences. These categories are configuration data, not separate product implementations.

Gateflow addresses a recurring problem in booking systems: a large number of users may attempt to obtain a limited number of reservations within a short time window. The platform must preserve fairness, prevent overselling, enforce purchase limits, coordinate admission, process payments, and recover correctly when services fail or responses are lost.

The product is designed around a central principle:

**High throughput is valuable only when the system preserves business correctness under contention and failure.**

Gateflow will demonstrate this principle through independently deployable services, durable state transitions, explicit admission budgets, idempotent operations, asynchronous event processing, reconciliation workflows, observable failure modes, and reproducible performance benchmarks.

The initial product will provide two web applications:

1. **Buyer Web:** Browse offerings, join queues, obtain admission, reserve inventory, complete simulated payments, and track orders.
2. **Operator Console:** Configure offerings and sales, manage admission strategies, inspect operational state, investigate failures, and execute authorized recovery actions.

The initial backend consists of five independently deployable Go services:

- Sale Service
- Admission Service
- Inventory Service
- Order Service
- Payment Simulator

The architecture begins with one PostgreSQL instance divided into service-owned schemas and roles, Kafka for asynchronous integration, and Redis for admission coordination and hot-path projections. PostgreSQL remains authoritative for durable business state.

Gateflow is initially a simulation and engineering platform, not a production payment processor or a commercially certified ticketing system.

# 2. Product Vision

## 2.1 Vision statement

Build a credible, extensible booking platform that demonstrates how a distributed system can safely coordinate scarce inventory, admission fairness, checkout, payment outcomes, and recovery during traffic spikes.

## 2.2 Product mission

Provide a configurable booking and admission workflow that:

- Allows operators to create and publish scheduled offerings.
- Allows buyers to participate in fair, controlled admission.
- Prevents inventory overselling and purchase-limit violations.
- Coordinates checkout across independently deployable services.
- Handles duplicate requests, delayed responses, service failures, and ambiguous payment outcomes.
- Gives operators visibility into unresolved transactions and controlled recovery tools.
- Provides measurable evidence of correctness, latency, throughput, and resilience.

## 2.3 Product principles

### Correctness before throughput

Inventory and purchase-limit invariants must hold under concurrency. A performance benchmark that violates either invariant is a failure.

### Durable truth

Every business-critical state transition must be recoverable from durable records. Redis, Kafka, in-memory caches, and UI state must not independently determine whether an order is confirmed or inventory is sold.

### Explicit uncertainty

A timeout does not prove that an operation failed. The platform must represent unknown outcomes and reconcile them using stable operation identities.

### Fairness with controlled admission

Queue ordering and admission policies must be explicit, reproducible where applicable, and protected by global and per-session budgets.

### Independent service ownership

Each service owns its business state, persistence schema, API contracts, and lifecycle transitions.

### Observable operations

Critical failures must be visible through logs, metrics, traces, durable workflow records, and operator-facing investigation tools.

### Measurable claims

Performance and resilience claims must be supported by reproducible tests, documented environments, and published results.

# 3. Problem Statement

High-demand booking systems face a combination of business and technical challenges.

## 3.1 Traffic concentration

A popular sale can receive a large burst of requests within seconds. Traffic may be highly skewed toward one offering, session, or ticket type.

Uncontrolled traffic can overload databases, exhaust connection pools, increase tail latency, and cause cascading failures.

## 3.2 Scarce inventory

The number of available tickets or reservations is finite. Concurrent requests must not allocate more inventory than is sellable.

A read-then-write implementation is unsafe when multiple requests observe the same remaining capacity.

## 3.3 Fairness and admission

A service that admits every request immediately may overload downstream services and create an unfair experience.

Queue position must survive reconnects and service restarts. Admission must be controlled by explicit budgets rather than client-side timing or in-memory ordering.

## 3.4 Distributed checkout

Checkout spans multiple independently owned services. There is no single database transaction covering admission, order creation, inventory reservation, and payment.

Partial success is therefore possible. A payment may succeed while an inventory reservation expires, or a service may commit a change while its response is lost.

## 3.5 Ambiguous failures

Network timeouts, process crashes, duplicate events, delayed callbacks, and retry storms can make it difficult to know whether an operation succeeded.

The platform must avoid duplicate charges, duplicate reservations, invalid order confirmation, and unsafe retries.

## 3.6 Operational recovery

A failed payment reversal or reconciliation task must not disappear into logs. Operators need a durable record of the issue, enough context to investigate it, and safe controls to recover it.

## 3.7 Engineering evidence

A booking demo that works only on the happy path does not demonstrate robust distributed-systems engineering.

Gateflow must produce evidence that its correctness guarantees survive contention, restart, duplicate delivery, dependency failure, and ambiguous outcomes.

# 4. Product Goals and Non-Goals

## 4.1 Product goals

| ID | Goal | Expected outcome |
|---|---|---|
| PG-01 | Configurable offerings | Operators can create different kinds of bookable experiences without category-specific code paths. |
| PG-02 | Controlled admission | Queueing and admission strategies can manage concentrated traffic. |
| PG-03 | Inventory correctness | Concurrent reservations cannot exceed sellable capacity. |
| PG-04 | Purchase-limit correctness | Concurrent checkouts cannot bypass buyer-level purchase limits. |
| PG-05 | Reliable checkout | Order, reservation, and payment outcomes are coordinated through explicit workflows. |
| PG-06 | Recoverability | Ambiguous and failed operations remain visible and recoverable. |
| PG-07 | Independent deployment | Each backend service can be built and deployed independently. |
| PG-08 | Operational visibility | Operators and engineers can inspect system behavior and investigate failures. |
| PG-09 | Performance evidence | Benchmarks report repeatable latency, throughput, resource use, and correctness results. |
| PG-10 | Extensibility | Admission strategies and sale policies can evolve without weakening shared invariants. |
| PG-11 | Security boundaries | Buyers, operators, and services have distinct identities and permissions. |
| PG-12 | Portfolio quality | The repository demonstrates documented architecture, meaningful tests, operational tooling, and engineering trade-offs. |

## 4.2 Non-goals for the initial release

The initial release will not attempt to provide:

- Real payment processing or storage of actual payment-card details.
- A commercial identity-management platform.
- A complete anti-bot, fraud-scoring, or identity-verification product.
- Dynamic pricing or an automated revenue-management engine.
- A full refund-policy engine supporting every commercial rule.
- Native iOS or Android applications.
- A multi-region active-active deployment.
- A multi-cloud deployment platform.
- A dedicated analytics warehouse.
- A general-purpose workflow engine.
- A standalone notification microservice.
- A dedicated API gateway without a demonstrated need.
- A schema-registry deployment from the beginning.
- A separate Identity Service solely for token validation.
- Kubernetes as a prerequisite for the initial correctness milestone.
- A claim of unlimited scalability or a predetermined production traffic capacity.

These capabilities may be considered later if they serve a demonstrated product need.

# 5. Target Users and Personas

## 5.1 Buyer

**Objective:** Obtain a reservation for a limited-capacity experience and understand the current booking status.

Needs:

- Discover published offerings.
- View session availability and purchase policies.
- Join a queue when admission is controlled.
- Retain queue position after reconnecting.
- Receive a durable admission entitlement.
- Complete checkout without accidentally creating duplicate orders.
- See reservation, payment, and order status.
- Understand when a checkout is pending or requires reconciliation.

Success means the buyer receives an accurate, understandable result even when the system is under load or a dependency is delayed.

## 5.2 Sale operator

**Objective:** Configure and publish an offering with correct inventory, pricing, timing, and purchase policies.

Needs:

- Create and edit offerings and sessions.
- Define ticket types, sections, capacities, and prices.
- Configure sale windows and publication state.
- Configure purchase limits and admission strategy.
- Inspect configuration versions and changes.
- Avoid accidental changes that silently alter existing checkouts.

## 5.3 Operations operator

**Objective:** Understand and recover failures without compromising business correctness.

Needs:

- Inspect queues, active grants, reservations, orders, and payments.
- Find transactions with unknown or inconsistent outcomes.
- View correlation and operation identifiers.
- Inspect reconciliation and compensation failures.
- Execute authorized retries of recoverable operations.
- Record an audit trail for every sensitive action.
- Distinguish an operation that failed from one whose outcome is unknown.

## 5.4 Engineering operator

**Objective:** Determine how the system behaves under load and failure.

Needs:

- View latency distributions and throughput.
- Inspect database contention, Redis behavior, Kafka lag, and service health.
- Trace requests across service boundaries.
- Run deterministic benchmarks.
- Trigger controlled failure scenarios in a non-production environment.
- Compare performance between revisions.

## 5.5 Security administrator

The initial product may combine this role with another operator role, but the permission boundary must remain explicit.

Needs:

- Manage or configure development identities.
- Control operator access.
- Review security-sensitive audit records.
- Investigate unauthorized access attempts.
- Ensure service credentials and secrets are not exposed.

# 6. Product Scope

## 6.1 Initial release scope

The first functional release must support the following end-to-end scenario:

1. An operator creates and publishes a scheduled offering.
2. A buyer discovers the offering and its sessions.
3. The buyer joins a queue.
4. Admission grants an entitlement according to configured policy and available budget.
5. The buyer begins checkout using an idempotency key.
6. Order verifies and binds the entitlement to the checkout.
7. Inventory creates a reservation if capacity and policies permit.
8. Order establishes entitlement consumption and payment processing.
9. The payment simulator records a successful or failed outcome.
10. Order confirms inventory and finalizes the order when the required conditions hold.
11. Durable domain events are published.
12. Failures and ambiguous outcomes remain recoverable.
13. Operators can inspect and recover supported exceptional cases.
14. Engineers can execute benchmark and failure-injection scenarios.

## 6.2 Initial admission strategies

The product must support a common admission interface with these strategy types:

- **FIFO:** Durable queue sequence determines order.
- **Lottery:** Eligible buyers are selected using a reproducible and auditable draw.
- **Scheduled slots:** Admission is granted within configured time windows and budgets.
- **Direct admission:** Eligible buyers proceed without a waiting queue, subject to rate and concurrency limits.

FIFO is the default strategy.

The initial release need not provide sophisticated lottery algorithms or a rich scheduling UI. It must, however, define the strategy interface and enforce shared admission budgets and entitlement invariants for every strategy.

## 6.3 Delivery phases

| Phase | Focus | Exit condition |
|---|---|---|
| P0 | Specification and architecture | Contracts, invariants, state machines, and acceptance tests are approved. |
| P1 | Foundation | Services, modules, migrations, local infrastructure, and CI are reliable. |
| P2 | Sale and inventory | Operators can configure offerings; inventory invariants hold under contention. |
| P3 | Admission | Queue, FIFO, budgets, entitlements, and coordination recovery are tested. |
| P4 | Checkout and payment | End-to-end booking and idempotent payment simulation work. |
| P5 | Recovery and operations | Ambiguous outcomes, compensation, audit, and operator tools work. |
| P6 | Performance and resilience | Reproducible benchmarks and fault-injection tests pass. |
| P7 | Deployment maturity | Kubernetes/Helm milestone, independent scaling, and deployment documentation are completed. |

The phases are a delivery plan, not a mandate to create separate release branches or to implement every infrastructure component before the first end-to-end workflow exists.

# 7. Product Domain Model

## 7.1 Offering

An offering represents a bookable product or experience.

Examples include a concert, sporting event, movie screening, or conference.

Core attributes:

- Offering ID.
- Name and description.
- Category metadata.
- Publication status.
- Sale configuration.
- Applicable purchase policies.
- Configuration version.
- Creation and modification timestamps.

Categories must be extensible metadata. The booking workflow must not contain hardcoded branches for particular event categories.

## 7.2 Session

A session represents a scheduled occurrence of an offering.

Core attributes:

- Session ID.
- Offering ID.
- Start and end time.
- Time zone or unambiguous time representation.
- Sale window.
- Session status.
- Applicable inventory pools.
- Applicable admission configuration.

A multi-session offering may have separate inventory and purchase-policy scopes.

## 7.3 Ticket type or inventory pool

A ticket type defines a sellable class of inventory.

Examples include general admission, premium seating, or a particular section.

Core attributes:

- Inventory pool ID.
- Session ID.
- Ticket type or section.
- Sellable capacity.
- Price configuration reference.
- Purchase-policy scope.
- Current operational status.

The inventory service owns capacity and reservation state. Sale owns the configured commercial description and policy.

## 7.4 Queue entry

A durable record representing a buyer's participation in a queue.

Core attributes:

- Queue entry ID.
- Buyer ID.
- Session or admission scope.
- Durable sequence.
- Queue status.
- Entry timestamp.
- Admission strategy and relevant version.
- Grant reference, when applicable.

Repeated join requests for the same buyer and applicable queue scope must not create uncontrolled duplicate entries.

## 7.5 Entitlement

A durable authorization to proceed through the controlled admission path.

Core attributes:

- Entitlement ID.
- Buyer ID.
- Admission scope.
- Issue time and unbound expiry.
- State.
- Bound checkout/order reference, when applicable.
- Strategy and policy versions.
- Audit metadata.

An entitlement is not a ticket and does not guarantee inventory availability.

## 7.6 Reservation

A temporary allocation of inventory.

Core attributes:

- Reservation ID.
- Inventory pool.
- Quantity.
- Buyer/order/operation reference.
- State.
- Creation time.
- Expiry deadline.
- Confirmation or release timestamps.
- Idempotency and correlation references.

## 7.7 Order

A durable record of a buyer's purchase workflow.

Core attributes:

- Order ID.
- Buyer ID.
- Checkout operation ID.
- Line items and quantities.
- Price and policy snapshots.
- Order state.
- Reservation reference.
- Payment-attempt references.
- Idempotency metadata.
- Creation and modification timestamps.

## 7.8 Payment attempt

A simulated attempt to obtain a payment outcome.

Core attributes:

- Payment attempt ID.
- Order ID.
- Stable payment operation ID.
- Amount and currency.
- Attempt state.
- Simulator outcome.
- Callback or result metadata.
- Creation and resolution timestamps.

The payment attempt is distinct from the order's overall state.

## 7.9 Compensation operation

A durable record representing a simulated reversal or refund action.

Core attributes:

- Compensation operation ID.
- Related payment attempt.
- Related order.
- Operation type.
- State.
- Attempt count.
- Last error.
- Audit information.
- Reconciliation status.

Compensation must not be inferred from an order status change alone.

# 8. Functional Requirements

The identifiers below are stable requirement references. Detailed API schemas, error codes, event payloads, and transition rules belong in `GF-SCS-001`.

## 8.1 Offering and sale management

### FR-SAL-001 — Create offering

Operators must be able to create an offering with a name, category, description, and initial configuration.

**Acceptance criteria:**

- The service validates required fields.
- A unique offering ID is generated.
- Invalid configurations are rejected with stable error codes.
- Creation is auditable.
- No public sale is available before publication and sale-window rules permit it.

### FR-SAL-002 — Configure sessions

Operators must be able to create scheduled sessions for an offering.

**Acceptance criteria:**

- Session times are unambiguous.
- Invalid or contradictory time windows are rejected.
- Each session has an explicit lifecycle state.
- Session-specific inventory and policy references are maintained.

### FR-SAL-003 — Configure inventory pools

Operators must be able to configure ticket types, sections, capacities, and commercial metadata.

**Acceptance criteria:**

- Capacity changes follow an explicit policy.
- A capacity reduction cannot silently invalidate existing confirmed or active reserved quantities.
- The inventory service validates sellable-capacity changes against its authoritative state.
- Changes are versioned and auditable.

### FR-SAL-004 — Configure sale windows

The platform must support sale start and end times and explicit publication state.

**Acceptance criteria:**

- Sales cannot begin before their configured start time.
- Sales stop accepting new checkout operations after their configured cutoff.
- The service defines what happens to already-created reservations at sale close.
- Clock handling and boundary conditions are tested.

### FR-SAL-005 — Configure purchase policies

Operators must configure purchase limits by session, offering, ticket type, or combinations of scopes.

The default is four tickets per buyer per session unless the offering configuration specifies otherwise.

**Acceptance criteria:**

- The effective scopes and limits are explicit.
- Conflicting policy definitions have deterministic precedence.
- Policy changes create a new policy version.
- Existing checkout holds retain their policy snapshot unless explicitly migrated.

### FR-SAL-006 — Publish and unpublish

Operators must be able to change publication state using validated transitions.

**Acceptance criteria:**

- Invalid transitions are rejected.
- Publication changes are audited.
- Unpublishing prevents new eligible sale activity but does not silently delete existing orders or reservations.

## 8.2 Buyer discovery and queue participation

### FR-ADM-001 — Browse offerings

Buyers can browse published offerings and eligible sessions.

**Acceptance criteria:**

- Unpublished or ineligible offerings are not presented as available for purchase.
- Availability is not represented as guaranteed inventory.
- Sale windows and relevant purchase policies are visible.

### FR-ADM-002 — Join queue

Buyers can join the applicable admission queue.

**Acceptance criteria:**

- Queue membership is durable.
- Repeated requests are idempotent within the defined scope.
- Queue sequence is assigned by the server, not trusted from the client.
- A reconnect does not create a new position unless the product's explicit policy requires a new entry.

### FR-ADM-003 — Preserve FIFO order

For FIFO queues, the durable queue-entry sequence determines ordering.

**Acceptance criteria:**

- Concurrent joins receive a deterministic durable order.
- Process restart does not reset queue order.
- Reconnect does not silently change position.
- A buyer cannot improve queue position by repeatedly submitting join requests.
- The implementation documents how ordering behaves across multiple Admission instances.

### FR-ADM-004 — Apply admission budgets

Admission must enforce global and per-session rate limits and active-checkout limits.

**Acceptance criteria:**

- Exhausted budgets stop new grants.
- Existing queue membership is preserved subject to configured storage limits.
- Rate budgets, active-checkout budgets, and inventory capacity remain distinct.
- A session-level burst cannot exceed its allowed budget merely because traffic is spread across multiple service instances.
- Budget exhaustion is observable.

### FR-ADM-005 — Issue entitlement

Eligible buyers receive a durable admission entitlement.

**Acceptance criteria:**

- The entitlement has a unique ID and durable state.
- The default unbound TTL is ten minutes.
- A buyer cannot use the entitlement to bypass the admission policy.
- Entitlement issue and budget accounting are traceable.
- Entitlement issuance is safe under concurrent requests and retries.

### FR-ADM-006 — Bind entitlement

An entitlement may be bound atomically to one buyer and one checkout/order operation.

**Acceptance criteria:**

- A bound entitlement cannot be used by a different order.
- Retries for the same checkout operation are permitted.
- The original unbound TTL does not invalidate a valid bound checkout by itself.
- Binding survives service restart.
- A definitive rejection may release binding only when policy permits it and the operation state is known.
- Ambiguous outcomes do not permit unsafe entitlement reuse.

### FR-ADM-007 — Support alternative strategies

The platform must provide a common interface for FIFO, lottery, scheduled slots, and direct admission.

**Acceptance criteria:**

- Each strategy returns a common grant result.
- Shared budget and entitlement checks cannot be bypassed.
- Lottery results are reproducible or auditable according to the selected algorithm.
- Scheduled-slot and direct-admission strategies remain subject to configured rate and concurrency limits.

## 8.3 Inventory management

### FR-INV-001 — Reserve inventory atomically

Inventory must atomically validate capacity and create a reservation.

**Acceptance criteria:**

- Concurrent requests cannot oversell a pool.
- Reservation creation uses a stable operation identity.
- Repeating the same reservation operation returns the established result.
- A timeout does not cause the caller to create a new logical reservation.
- Capacity accounting includes confirmed quantities and active reservations.

### FR-INV-002 — Enforce the capacity invariant

For each sellable pool:

`confirmed quantity + active reserved quantity <= sellable capacity`

**Acceptance criteria:**

- The invariant holds during normal operation.
- The invariant holds under concurrent reservation attempts.
- Expiry, confirmation, release, and capacity updates preserve the invariant.
- Integration tests verify database-level atomicity and relevant constraints.

### FR-INV-003 — Support reservation lookup

Inventory must expose lookup by stable operation ID.

**Acceptance criteria:**

- The caller can determine whether an operation created a reservation.
- A missing response can be reconciled without generating a second reservation.
- Lookup results distinguish absence, known state, and any unresolved consistency condition defined by the contract.

### FR-INV-004 — Enforce reservation leases

The default reservation lease is five minutes, with a proposed configurable range of one to 15 minutes.

**Acceptance criteria:**

- Expiry begins at durable reservation creation.
- Invalid lease configuration is rejected.
- The expiry deadline is stored durably in UTC.
- Confirmation and expiry compete through conditional state transitions.
- Expired reservations cannot be revived.
- Cleanup-worker delay does not extend reservation eligibility.

### FR-INV-005 — Confirm reservation

Inventory may confirm a reservation only when the reservation is eligible and its state permits confirmation.

**Acceptance criteria:**

- Confirmation and expiry cannot both win.
- Confirmation is idempotent.
- A successful payment does not independently confirm inventory.
- A late confirmation request against an expired reservation is rejected.

### FR-INV-006 — Release reservation

Inventory must support safe release of eligible reservations.

**Acceptance criteria:**

- Release is idempotent.
- Confirmed inventory is not released through an ordinary active-reservation release path.
- Cancellation of confirmed inventory uses the explicit cancellation workflow.
- Release does not double-decrement reserved quantity.

## 8.4 Order and checkout orchestration

### FR-ORD-001 — Initiate checkout

The buyer submits a checkout command with an idempotency key.

**Acceptance criteria:**

- The authenticated buyer identity is derived from validated claims.
- A stable order/checkout operation identity is established.
- The effective purchase policy is snapshotted.
- The applicable entitlement is validated and bound.
- Repeated requests with the same key and same payload do not create duplicate orders.
- Reuse with a conflicting payload produces a stable conflict response.

### FR-ORD-002 — Enforce purchase limits

Order must reserve buyer allowance atomically.

**Acceptance criteria:**

- Applicable session, offering, ticket-type, and combined limits are enforced.
- Concurrent checkout attempts cannot exceed the applicable limit.
- Active allowance holds count against the limit.
- Successful orders convert held allowance to committed allowance.
- Definitive failure releases the applicable hold.
- Ambiguous outcomes retain the hold until safely resolved.

### FR-ORD-003 — Orchestrate reservation

Order requests inventory reservation using a stable operation ID.

**Acceptance criteria:**

- Retries use the same operation ID.
- Lost responses trigger lookup/reconciliation.
- A second logical reservation is not created to resolve an ambiguous first request.
- The order remains in an appropriate non-confirmed state until the required inventory state is established.

### FR-ORD-004 — Orchestrate payment

Order initiates the payment simulator only after the required checkout and reservation conditions hold.

**Acceptance criteria:**

- The payment operation is stable and idempotent.
- A payment result is associated with the correct order and attempt.
- Unknown outcomes enter reconciliation.
- The three-minute outcome deadline triggers escalation/reconciliation rather than automatic assumption of failure.
- Payment success alone does not confirm the order.

### FR-ORD-005 — Finalize order

Order can finalize a successful purchase only when the required payment and inventory conditions are established.

**Acceptance criteria:**

- The inventory reservation is confirmed before order confirmation.
- Finalization is idempotent.
- Duplicate callbacks or events do not produce duplicate business effects.
- Confirmed order state is durable.
- An order cannot transition from a terminal failure state to success without an explicitly defined recovery transition.

### FR-ORD-006 — Reconcile checkout

Order must identify and resolve incomplete or ambiguous workflows.

**Acceptance criteria:**

- Reconciliation can find orders with unknown payment or inventory outcomes.
- Reconciliation reuses original operation identities.
- Reconciliation does not silently release active holds while an outcome is ambiguous.
- Every unresolved workflow has a durable state and a visible last-known error or status.
- Reconciliation attempts are observable.

### FR-ORD-007 — Handle late payment success

If payment succeeds after reservation expiry:

- The reservation is not revived.
- The payment state remains successful.
- The order enters `RECONCILIATION_REQUIRED` or the equivalent defined state.
- An idempotent compensation operation is created.
- The order is not confirmed without valid inventory confirmation.
- Operators can inspect the situation and recover it safely.

## 8.5 Payment simulation and compensation

### FR-PAY-001 — Simulate payment outcomes

The payment simulator must support deterministic success, definitive failure, and ambiguous/delayed outcome scenarios.

**Acceptance criteria:**

- Test scenarios can be selected reproducibly.
- Every attempt has a durable operation ID and state.
- Duplicate requests do not create duplicate logical payment operations.
- Simulated outcomes are inspectable and traceable to the associated order.

### FR-PAY-002 — Simulate callbacks and delays

The simulator must support delayed outcomes and callback duplication for testing.

**Acceptance criteria:**

- Duplicate callbacks are handled idempotently.
- Delayed success is possible after reservation expiry.
- Callback processing validates the payment operation and associated order.
- The test harness can reproduce callback ordering and timing scenarios.

### FR-PAY-003 — Create compensation operation

Compensation must be a distinct durable operation.

**Acceptance criteria:**

- The operation has a stable ID.
- Duplicate requests resolve to the same logical operation.
- The payment state is not rewritten to imply that compensation occurred.
- Compensation status is independently queryable.

### FR-PAY-004 — Handle compensation failure

After the first definitive compensation failure, the platform escalates the operation for investigation.

**Acceptance criteria:**

- The failure is durable and visible.
- The operator can see the last error and attempt history.
- An authorized operator can request an idempotent manual retry.
- Ambiguous outcomes are queried before retry.
- The platform never marks compensation successful without a verified outcome.

### FR-PAY-005 — Support cancellation

Cancellation eligibility is configurable according to the applicable order and payment state.

**Acceptance criteria:**

- Cancellation is a durable workflow.
- Eligibility is validated against authoritative state.
- Inventory release, allowance release, and compensation are separate explicit transitions.
- Cancellation retries are idempotent.
- Every sensitive operator action is audited.
- The system does not report completed cancellation while required compensation remains unresolved.

## 8.6 Operator console

### FR-OPS-001 — Manage offerings

Operators can create, configure, publish, and unpublish offerings and sessions according to their permissions.

### FR-OPS-002 — Inspect admission

Operators can view relevant queue state, strategy configuration, admission budgets, active grants, and coordination health.

### FR-OPS-003 — Inspect inventory

Operators can inspect sellable capacity, confirmed quantity, active reservations, and reservation expiry information.

### FR-OPS-004 — Inspect orders and payments

Operators can inspect an order's state, payment attempts, reservation references, and reconciliation status.

### FR-OPS-005 — Inspect compensation

Operators can inspect compensation status, operation IDs, attempts, last errors, and available recovery actions.

### FR-OPS-006 — Execute authorized recovery

The console supports only explicitly permitted actions.

**Acceptance criteria:**

- Permissions are enforced by the backend.
- Actions require confirmation where appropriate.
- Every sensitive action records the operator, action, target, timestamp, reason where required, and outcome.
- Retrying a workflow reuses its operation identity.
- The console cannot directly mutate service-owned database state.

### FR-OPS-007 — Surface operational health

The console or associated operational dashboards must expose the health of critical workflows and dependencies.

The initial implementation may use Grafana for infrastructure dashboards and the Operator Console for business-level investigations.

## 8.7 Observability and benchmark tooling

### FR-OBS-001 — Correlate requests

Critical workflows must carry correlation and causation identifiers across service calls and events.

### FR-OBS-002 — Record structured logs

Logs must support investigation without exposing secrets or unnecessary personal data.

### FR-OBS-003 — Publish metrics

The platform must expose latency, throughput, error rate, resource utilization, queue growth, admission-budget utilization, reservation outcomes, reconciliation backlog, and compensation failures.

### FR-OBS-004 — Trace distributed workflows

OpenTelemetry traces should link relevant operations across services and Kafka processing where supported.

### FR-OBS-005 — Avoid high-cardinality metrics

Buyer IDs, order IDs, entitlement IDs, and similar unique identifiers must not be metric labels.

### FR-OBS-006 — Run reproducible benchmarks

The benchmark harness must record environment, configuration, workload, duration, concurrency, result distributions, errors, and correctness outcomes.

### FR-OBS-007 — Inject controlled failures

The test environment must support reproducible service restarts, dependency failures, delayed responses, duplicate events, and ambiguous payment outcomes.

# 9. Business Rules and Invariants

The following invariants apply across the product.

## 9.1 Inventory capacity

For every sellable pool:

`confirmed quantity + active reserved quantity <= sellable capacity`

## 9.2 Purchase allowance

For every buyer and applicable policy scope:

`committed quantity + held quantity + requested quantity <= effective purchase limit`

The check and hold operation must be atomic.

## 9.3 Entitlement ownership

An entitlement is bound to one buyer and one checkout/order operation. It cannot be reused for another operation.

## 9.4 Idempotent operations

A repeated command with the same idempotency identity and equivalent request must not produce duplicate business effects.

A repeated key with a conflicting payload must be rejected.

## 9.5 Payment/order distinction

Payment success is not equivalent to order confirmation. The order can be confirmed only when all required business conditions, including inventory confirmation, are established.

## 9.6 Reservation expiry

Expiry is determined by durable reservation state and deadline. A cleanup worker's execution time does not determine whether a reservation remains eligible.

## 9.7 Ambiguous outcomes

A timeout is uncertainty, not proof of failure. The system must query or reconcile the original operation before initiating a replacement operation.

## 9.8 Durable recovery

Unresolved workflows must remain discoverable after service restart. No critical workflow may depend solely on in-memory state.

## 9.9 Service ownership

Only a service may mutate its owned business state through its own persistence boundary. Other services interact through documented APIs or events.

## 9.10 Auditable operations

Sensitive configuration changes, cancellation actions, and manual compensation attempts must be attributable to an authenticated operator.

# 10. High-Level Checkout Workflow

The following is the intended happy path. Each step must also define failure, timeout, retry, and recovery behavior in the service-contract specification.

1. Buyer discovers an eligible offering and session.
2. Buyer joins the admission queue.
3. Admission assigns or retrieves a durable queue entry.
4. Admission grants an entitlement within configured budgets.
5. Buyer initiates checkout with an idempotency key.
6. Order creates or retrieves the checkout operation.
7. Order snapshots the effective purchase policy and reserves buyer allowance.
8. Admission binds the entitlement to the checkout/order operation.
9. Order requests inventory reservation using a stable operation ID.
10. Inventory atomically validates capacity and records the reservation and expiry deadline.
11. Order consumes the entitlement after reservation success is established.
12. Order initiates a payment attempt using a stable payment operation ID.
13. Payment simulator records a definitive or ambiguous outcome.
14. For successful payment, Order requests inventory confirmation.
15. Inventory confirms only if the reservation remains eligible.
16. Order finalizes the order and commits the allowance.
17. Outbox-backed events are published.
18. Buyer receives the established order status.

If any response is lost, the caller retries or reconciles using the same operation identity. The workflow must never infer failure from a timeout alone.

# 11. State and Lifecycle Requirements

The final specification must define complete state machines for at least:

- Offering and session lifecycle.
- Queue entry lifecycle.
- Entitlement lifecycle.
- Admission budget reservation and release.
- Inventory reservation lifecycle.
- Checkout and order lifecycle.
- Payment attempt lifecycle.
- Compensation lifecycle.
- Cancellation lifecycle.
- Reconciliation lifecycle.
- Outbox publication and consumer deduplication lifecycle.

Each state machine must define:

- Valid states.
- Allowed transitions.
- Transition authority.
- Preconditions.
- Durable side effects.
- Idempotency behavior.
- Timeout behavior.
- Retry behavior.
- Terminal-state semantics.
- Recovery behavior.
- Required audit and telemetry.

The state names in the detailed specification must be consistent across API contracts, database models, events, operator UI, and tests.

# 12. Non-Functional Requirements

## 12.1 Performance

Initial latency targets are provisional local targets:

| Operation | p95 target | p99 target |
|---|---:|---:|
| Admission grant | ≤ 100 ms | ≤ 250 ms |
| Inventory reservation API | ≤ 100 ms | ≤ 250 ms |
| Order API synchronous response | ≤ 250 ms | ≤ 500 ms |

These targets must be measured under a declared workload. Order API response latency does not represent end-to-end checkout completion.

The initial system must establish a reproducible baseline before declaring a guaranteed requests-per-second capacity.

## 12.2 Correctness

Correctness is a hard acceptance gate.

Required outcomes:

- Zero inventory oversells.
- Zero purchase-limit violations.
- Zero duplicate business effects from retries.
- No confirmed order without confirmed inventory.
- No unsafe budget reset after Redis recovery.
- No silent loss of ambiguous payment or compensation state.

## 12.3 Reliability and recovery

The system must survive the tested failure modes without violating business invariants.

Required scenarios include:

- Service process restart.
- Database transaction rollback.
- Lost response after committed operation.
- Duplicate Kafka delivery.
- Consumer restart and replay.
- Redis outage and recovery.
- Kafka outage and recovery.
- Payment timeout and delayed success.
- Reservation expiry racing with confirmation.
- Compensation failure.
- Concurrent checkouts for the same buyer.
- Admission-budget exhaustion.
- Stale Admission worker after coordination recovery.

## 12.4 Scalability

The architecture must allow services to scale independently.

Initial evidence should include:

- Multiple Admission instances under a hot-session workload.
- Multiple Order and Inventory request workers.
- Measurement of database connection-pool saturation and lock contention.
- Kafka consumer scaling and lag behavior.
- Evidence that global and per-session admission budgets remain enforced across instances.

Independent deployment does not itself prove safe independent scaling. The relevant contention and coordination tests must pass.

## 12.5 Security

The system must enforce:

- Validated buyer identity.
- Separate operator authorization.
- Distinct service identities.
- Audience- and scope-restricted service credentials.
- Least-privilege database roles.
- Secret-safe logs.
- Authenticated and verified payment callbacks.
- Auditable operator recovery actions.
- Server-side authorization for all sensitive operations.

## 12.6 Maintainability

The repository must provide:

- Service ownership documentation.
- OpenAPI and event-schema contracts.
- ADRs for consequential architectural decisions.
- Explicit migrations.
- Unit, integration, contract, and concurrency tests.
- Deterministic test fixtures.
- Local setup and recovery instructions.
- Documented benchmark methodology.
- Clear separation of technical shared libraries and domain ownership.

## 12.7 Observability

The platform will use OpenTelemetry, Prometheus, Grafana, Loki, and Tempo.

The specification must define the initial metrics, logs, traces, dashboards, and alerts, including cardinality controls and operational response expectations.

# 13. Architecture and Technology Constraints

## 13.1 Backend

- Go services.
- Five independently deployable modules.
- Root `go.work`.
- Service-owned business state and persistence boundaries.
- REST/JSON with OpenAPI.
- Kafka for asynchronous integration.
- Transactional outbox for state changes requiring durable event publication.
- Idempotent consumers.

## 13.2 Persistence and coordination

- PostgreSQL is the durable business-state authority.
- One initial PostgreSQL instance with separate schemas and restricted service roles.
- Redis supports admission rate/concurrency coordination and hot queue projections.
- Redis must not independently establish inventory or order truth.
- Redis recovery requires fencing and reconciliation before new grants resume.

## 13.3 Frontend

- Buyer Web: React + TypeScript.
- Operator Console: React + TypeScript.
- Backend services enforce authorization and business invariants.
- Frontend state must not be treated as authoritative for inventory, queue position, payment, or order status.

## 13.4 Local and later deployment

- Docker Compose for initial local development and integration tests.
- Kubernetes/Helm after core correctness and integration tests.
- Independent service images and configuration.
- Secrets excluded from source control.
- Migrations and health checks integrated into the engineering workflow.

# 14. Event and Integration Requirements

The final event catalogue must identify each event's producer, consumers, topic, schema version, partition key, and delivery requirements.

Expected event families include:

- Offering and sale configuration changes.
- Queue and admission lifecycle changes.
- Entitlement issue, binding, and consumption.
- Reservation creation, confirmation, expiry, and release.
- Order lifecycle changes.
- Payment attempt and outcome changes.
- Cancellation and compensation changes.
- Reconciliation and operational escalation events.

These are event families, not a finalized event-name or topic list.

The final specification must also define:

- Event envelope and stable event IDs.
- Schema compatibility rules.
- Topic naming conventions.
- Partitioning and ordering expectations.
- Outbox publication behavior.
- Consumer deduplication.
- Retry and backoff behavior.
- Dead-letter and poison-event handling.
- Replay and retention rules.
- Correlation and causation identifiers.

No consumer may assume exactly-once business effects solely because Kafka is used.

# 15. Data Ownership

| Service | Authoritative state |
|---|---|
| Sale | Offerings, sessions, sale windows, ticket-type configuration, commercial policies and versions |
| Admission | Queue membership, durable queue sequence, entitlement lifecycle, admission budgets and grants |
| Inventory | Sellable capacity, reservation state, reservation expiry, confirmation and release |
| Order | Checkout orchestration, orders, allowance holds and commits, reconciliation state |
| Payment Simulator | Payment attempts, simulated outcomes, callbacks and compensation operations |

This table establishes ownership boundaries. The detailed specification must distinguish which fields are owned by a service and which are only referenced by IDs or snapshots.

Cross-service database writes are prohibited. Services communicate through APIs and events, with no distributed database transaction.

# 16. Operator Experience and Recovery

The Operator Console is a business-level recovery tool, not a database administration interface.

Every recoverable issue should expose, as applicable:

- Order, reservation, payment, entitlement, and compensation identifiers.
- Current state of each related entity.
- Correlation and causation identifiers.
- Creation and last-update timestamps.
- Attempt count and last known error.
- Reconciliation status.
- Available authorized actions.
- Audit history.

Manual recovery must be explicit, idempotent, authorized, and auditable.

The UI must distinguish:

- Definitive failure.
- Pending operation.
- Unknown or ambiguous outcome.
- Reconciliation required.
- Compensation failed.
- Successfully recovered.

The platform must never display a success state merely because a request was accepted or a retry was queued.

# 17. Testing and Acceptance Strategy

## 17.1 Unit tests

Cover business rules, policy precedence, state transitions, idempotency decisions, error classification, and admission strategy behavior.

## 17.2 Database integration tests

Cover atomic reservations, allowance holds, conditional transitions, outbox transactions, migrations, and transaction rollback.

## 17.3 Contract tests

Validate OpenAPI definitions, JSON Schemas, compatibility rules, stable error envelopes, and producer/consumer expectations.

## 17.4 Concurrency tests

Test:

- Competing reservations for the last units of inventory.
- Concurrent checkouts by one buyer.
- Multiple Admission workers issuing grants.
- Simultaneous reservation expiry and confirmation.
- Duplicate operations and conflicting idempotency-key reuse.

## 17.5 Failure-injection tests

Test lost responses, process crashes, duplicate events, Redis and Kafka outages, delayed callbacks, stale workers, and compensation failures.

## 17.6 End-to-end tests

Validate the complete buyer journey, including both success and recoverable failure paths.

## 17.7 Benchmark tests

Run reproducible tests for:

- Steady-state load.
- Hot-session contention.
- Multiple sessions with uneven traffic.
- Global and per-session budget exhaustion.
- Slow downstream dependencies.
- Redis disruption and recovery.
- Kafka lag and consumer restart.
- Long-running soak behavior.

## 17.8 Initial benchmark acceptance

The initial proposed soak duration is 30 minutes. Acceptance requires no correctness violation, unbounded queue growth, or sustained unexplained resource growth.

Performance reports must include environment details, workload parameters, latency distributions, throughput, error rates, resource utilization, and correctness outcomes.

A benchmark that violates an invariant fails even if latency targets are met.

# 18. Product Success Metrics

## 18.1 Functional success

- A buyer can complete a valid booking from discovery through confirmed order.
- A buyer can recover from a lost response without creating duplicate business effects.
- Operators can configure a sale and inspect its operational state.
- Supported exceptional workflows remain visible and recoverable.

## 18.2 Correctness success

- Zero inventory oversells in defined concurrency and failure tests.
- Zero purchase-limit violations in defined concurrency tests.
- No confirmed orders without established inventory confirmation.
- No duplicate reservations or payment operations from idempotent retries.
- No unsafe admission-budget resumption after tested Redis failures.

## 18.3 Performance success

- Provisional local latency targets are met under the documented workload.
- Sustainable throughput is measured and published with reproducible methodology.
- Resource utilization and tail latency remain explainable under contention.
- The system completes the declared soak workload without correctness violations or unbounded growth.

## 18.4 Operational success

- Critical failures are observable.
- Unresolved operations can be identified by stable identifiers.
- Compensation failures remain visible until resolved.
- Authorized manual recovery is auditable.
- Recovery tests demonstrate safe resumption after dependency failures.

## 18.5 Engineering success

The project must have:

- A consistent architecture and service ownership model.
- Versioned API and event contracts.
- ADRs with rationale and consequences.
- Automated correctness and integration tests.
- Reproducible benchmarks.
- Failure-injection evidence.
- Local deployment instructions.
- Operational and recovery documentation.

# 19. Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Redis/PostgreSQL coordination races | Admission budgets may be exceeded | Durable grant identities, fencing, reconciliation, and failure-injection tests |
| Inventory race conditions | Overselling | Atomic conditional transitions and concurrency tests |
| Ambiguous payment outcome | Duplicate payment or incorrect order state | Stable operation IDs, reconciliation, explicit payment states |
| Entitlement reuse | Admission bypass | Atomic binding to buyer and checkout |
| Policy changes during checkout | Purchase-limit inconsistency | Versioned policy snapshots and explicit migration |
| Duplicate Kafka events | Duplicate business effects | Event IDs and idempotent consumers |
| Excessive microservice complexity | Slow delivery and difficult debugging | Five services only, one initial database instance, no unnecessary gateway or workflow engine |
| Poor benchmark reproducibility | Unsupported performance claims | Seeded workloads, documented environments, repeated runs |
| High metric cardinality | Monitoring overload | Bounded labels and identifiers in logs/traces instead |
| Unsafe operator recovery | Incorrect inventory/payment state | Role-based authorization, state validation, idempotency, audit history |
| Scope expansion | Delayed correctness milestones | Explicit non-goals and phased delivery |
| Overstated scalability claims | Reduced technical credibility | Publish measured results with environment and limitations |

# 20. Open Specification Dependencies

The following are implementation-critical details that must be resolved in `GF-SCS-001`. They are not permission to begin coding.

1. Exact API requests, responses, error codes, and idempotency semantics.
2. Admission budget accounting and grant lifecycle.
3. Redis fencing, reconciliation, and safe-resumption protocol.
4. Reservation, payment, cancellation, and compensation race semantics.
5. Benchmark workload, environment, and pass/fail calculations.
6. Event topics, partition keys, schema compatibility, retries, and replay.
7. Identity claims, operator permissions, and privacy controls.
8. Policy migration when new limits conflict with existing holds.
9. Reservation lease validation and expiry edge cases.
10. Observability signals, alert thresholds, and recovery visibility.
11. Service startup, migrations, CI gates, and local orchestration.
12. Admission strategy interface and shared invariants.

The final specification must either resolve these details or assign a documented default before implementation begins.

# 21. Delivery and Release Gates

## Gate 0 — Specification approval

Required:

- Product scope and non-goals are stable.
- Service boundaries and ownership are explicit.
- State machines and invariants are defined.
- API and event contracts are specified.
- Redis recovery and critical race behavior are documented.
- Acceptance criteria map requirements to tests.
- Unresolved material decisions are identified.

## Gate 1 — Foundation

Required:

- Repository and module structure.
- Local infrastructure.
- Service-owned schemas and roles.
- CI, migrations, health checks, and contract validation.
- Security and observability foundations.

## Gate 2 — Core booking correctness

Required:

- Sale configuration.
- Admission and entitlement lifecycle.
- Inventory reservation and expiry.
- Order and payment workflow.
- Hard correctness tests passing.

## Gate 3 — Failure recovery

Required:

- Idempotent retry behavior.
- Reconciliation.
- Late payment handling.
- Compensation and cancellation.
- Redis recovery and Kafka replay tests.
- Operator recovery workflow.

## Gate 4 — Performance evidence

Required:

- Reproducible workload.
- Baseline latency and throughput.
- Contention and soak results.
- Failure-injection results.
- Documented resource constraints and bottlenecks.

## Gate 5 — Deployment maturity

Required:

- Independently deployable services.
- Deployment and rollback procedures.
- Kubernetes/Helm milestone completed when justified.
- Independent scaling evidence.
- Operational documentation.

# 22. Documentation Deliverables

The project should maintain these documents:

- `docs/product/PRD.md` — product requirements and scope.
- `docs/architecture/overview.md` — architecture and service boundaries.
- `docs/contracts/GF-SCS-001.md` — service-contract specification.
- `docs/adr/` — architecture decision records.
- `docs/runbooks/` — operational recovery procedures.
- `docs/testing/` — test strategy and failure-injection methodology.
- `docs/benchmarks/` — workload definitions, environment, and measured results.
- `docs/security/` — identity, authorization, secret handling, and audit policy.

The exact filenames may be adjusted to match the repository convention, but each document's responsibility must remain clear.

# 23. Final Product Acceptance Criteria

Gateflow's initial release is accepted only when all of the following are true:

1. Operators can configure and publish a bookable offering.
2. Buyers can join the configured admission path and receive valid entitlements.
3. Admission budgets are enforced across multiple service instances.
4. Inventory and purchase-limit invariants survive concurrent requests.
5. Checkout is idempotent and recoverable after lost responses.
6. Payment and inventory outcomes are modeled independently.
7. Late payment success does not revive expired inventory.
8. Cancellation and compensation are durable, explicit, and auditable.
9. Redis recovery cannot resume admission unsafely.
10. Kafka duplicates and replay do not create duplicate business effects.
11. Critical failures remain visible and recoverable through operational tooling.
12. Security boundaries are enforced by backend services.
13. The declared benchmark suite is reproducible and passes its correctness gates.
14. Performance results are documented without unsupported capacity claims.
15. The architecture, API/event contracts, ADRs, tests, and operational documentation are consistent.

# 24. Product Positioning

Gateflow should be presented as a **high-demand booking and admission orchestration platform built to demonstrate distributed-systems correctness, controlled traffic admission, recoverable workflows, and measured performance**.

Its differentiators are not the number of microservices or the use of Kafka and Redis in isolation. They are the explicit invariants, failure semantics, recovery mechanisms, and empirical evidence that the system preserves correct behavior under stress.

The strongest technical demonstration will show that Gateflow remains correct when responses are lost, dependencies fail, events are duplicated, reservations expire, and payment outcomes arrive late.

**Final status:** Consolidated with `GF-SCS-001` and ADRs 0001–0024. Phase 0 complete; Phase 1 foundation authorized by the master implementation prompt.

---

# Appendix A — Frontend Technology and Architecture

**Requirement ID:** FR-UI-001  
**Status:** Accepted

### 1. Technology stack

Both Gateflow web applications must use:

- **React** for component-based UI development.
- **TypeScript** for static typing.
- **Vite** for development and production builds.
- **React Router** for client-side routing.
- **TanStack Query** for server-state fetching, caching, invalidation, and retry policies.
- **React Hook Form + Zod** for form handling and client-side schema validation.
- **Vitest + React Testing Library** for component and interaction tests.
- **Playwright** for browser-based end-to-end testing.

The exact dependency versions will be pinned when implementation begins.

### 2. Application boundaries

**Buyer Web**

- Browse offerings and sessions.
- View sale windows, ticket types, and purchase policies.
- Join a queue and track queue status.
- View admission entitlement and expiry information.
- Start checkout and observe reservation/payment progress.
- View confirmed orders and recoverable pending states.

**Operator Console**

- Create, configure, publish, and unpublish offerings.
- Configure admission strategy and budgets.
- Inspect inventory, reservations, orders, payments, and compensation.
- Investigate reconciliation failures.
- Execute authorized recovery actions.
- View operational status and audit history.

### 3. Frontend engineering requirements

- Use reusable components and feature-oriented module organization.
- Separate API clients, domain-facing types, route components, and UI components.
- Generate or validate API types from the backend OpenAPI contracts where practical.
- Keep authentication and authorization enforcement in the backend; frontend route guards are supplementary.
- Treat queue position, inventory, payment, and order state as server-authoritative.
- Use polling or another explicitly designed update mechanism for queue and checkout status initially; do not introduce WebSockets without a demonstrated requirement.
- Handle loading, empty, error, pending, and reconciliation-required states explicitly.
- Never report a booking as confirmed based solely on a successful frontend request or payment response.

### 4. Frontend acceptance criteria

- Both applications build and type-check in CI.
- Critical user journeys have component and end-to-end tests.
- Duplicate submissions do not bypass backend idempotency.
- UI state remains correct after refresh, reconnect, and delayed API responses.
- Pending and ambiguous outcomes are presented accurately.
- Operator recovery controls respect backend authorization and provide clear action results.
- API contract changes trigger appropriate frontend validation or build failures.

### 5. Scope control

The initial implementation will use two React applications. A shared UI component package may be introduced only for genuinely reusable presentation components; business-domain logic and service ownership must not be moved into a shared frontend package.
