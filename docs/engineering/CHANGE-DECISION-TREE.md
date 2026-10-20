# Gateflow Change Decision Tree

Every implementation task must follow this process.

## Step 1 — Understand the request

Is the requirement clear and consistent with the PRD and service contracts?

- YES -> Continue.
- NO -> Inspect existing documentation and code.
    - If a safe default exists, document the assumption and proceed.
    - If the ambiguity materially changes scope, correctness, security, or
      user-visible behavior, stop and explain the decision needed.

## Step 2 — Classify the change

Does it change an architectural decision, service boundary, API/event
contract, persistence model, security boundary, or business invariant?

- YES -> Create/update an ADR and relevant contract documentation.
- NO -> Record the rationale in the Change Decision Record.

Does it change only documentation?

- YES -> Explain the motivation and update the relevant document.
- NO -> Continue to implementation planning.

## Step 3 — Analyze impact

Identify:

- Requirements and acceptance criteria.
- Owning service and affected modules.
- API/event/database changes.
- Concurrency and consistency implications.
- Failure modes and retry behavior.
- Security and authorization implications.
- Observability requirements.
- Tests required.

If a change crosses service boundaries, explicitly describe the interaction.

## Step 4 — Compare approaches

Document at least:

- Preferred approach.
- One reasonable alternative, when one exists.
- Why the preferred approach is better.
- Costs and trade-offs.
- Conditions under which the decision should be revisited.

Do not invent alternatives merely to fill a template.

## Step 5 — Implement

- Make the smallest coherent change.
- Add or update tests.
- Update contracts and documentation.
- Preserve existing invariants.
- Do not expand scope without justification.

## Step 6 — Verify

Did the relevant checks pass?

- YES -> Continue.
- NO -> Diagnose and fix.
- Cannot run -> Record NOT RUN, explain why, and identify residual risk.

For correctness-sensitive changes, verify persisted state and relevant
concurrency/failure behavior, not just response codes.

## Step 7 — Review the diff

Check:

- Unrelated changes.
- Missing tests.
- Contract drift.
- Security issues.
- Missing migration or rollback considerations.
- Unbounded retries or resource use.
- Missing observability.
- Inaccurate documentation.

## Step 8 — Record and report

Create the Change Decision Record.

Update ADRs, OpenAPI, event schemas, migrations, and runbooks as applicable.

Report what changed, why, alternatives, verification evidence, risks,
and the next recommended step.

A task is complete only when required documentation and verification
are complete or their limitations are explicitly recorded.
