---
description: Implement a Gateflow task with mandatory decision records and verification
---

Implement this Gateflow task:

$ARGUMENTS

Follow the repository's AGENTS.md and
docs/engineering/CHANGE-DECISION-TREE.md.

MANDATORY WORKFLOW:

1. Inspect git status and preserve existing work.
2. Read relevant PRD requirements, service contracts, ADRs, code, and tests.
3. State the requirement, acceptance criteria, affected services, and risks.
4. Decide whether an ADR, API/event contract change, migration, or runbook
   update is required.
5. Compare viable approaches and explain the chosen approach.
6. Implement the smallest coherent change.
7. Add or update tests.
8. Run relevant formatting, lint, type-check, build, and test commands.
9. Inspect the complete diff.
10. Create a Change Decision Record at:
    docs/engineering/change-records/CHANGE-<id>-<slug>.md
11. Update ADRs and contracts when required.
12. Report exact commands and actual outcomes.

The change record must explain:

- What changed and why.
- Previous and new behavior.
- Decision tree and alternatives.
- Services and files affected.
- Correctness invariants.
- Failure and recovery behavior.
- API, event, database, and security implications.
- Tests and actual results.
- Known risks and follow-up work.

Never fabricate verification results.
If a test was not run, report NOT RUN.
If an architectural requirement is ambiguous, resolve it from existing
specification and documented defaults where safe; otherwise stop before
making a consequential irreversible decision.

Do not push to GitHub or discard user changes.
Do not claim completion until the implementation, change record,
and required verification are complete.
