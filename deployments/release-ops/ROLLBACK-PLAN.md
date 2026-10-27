# Artifact and configuration rollback plan

## 1. Scope

Return Gateflow services to a **previously approved** build and configuration in a non-production environment first.

**Protocol:** Always verify Protocol **A** after rollback. Never roll “forward” into B.

## 2. What “previous artifact” means

| Kind | Identifier |
|---|---|
| Git | Commit SHA (e.g. `85fb1d7`) |
| Container | Image repo + digest |
| Config | Env/config map revision |

Local-only process restart **without** a prior registry image is **config smoke only**, not full artifact rollback proof.

## 3. Procedure (staging)

1. **Decision owner:** release owner (or CTO delegate).  
2. **Retrieve** previous image digest / binary tarball from registry or artifact store.  
3. **Schema check:** confirm DB migrations are backward-compatible with the prior app, or roll forward-only migrations with documented expand/contract.  
4. **Stop** current deployment (rolling).  
5. **Deploy** previous artifact with env: `GATEFLOW_ADMISSION_GRANT_PROTOCOL=A` (or unset).  
6. **Verify** each admission replica `/v1/meta` → `grant_protocol=A`.  
7. **Smoke:** healthz + VS1-equivalent checkout CONFIRMED.  
8. **In-flight:** do not delete committed entitlements/orders; timeout paths use reconcile runbook.  
9. **Record:** time, from-rev, to-rev, who approved, smoke order id, any errors.

## 4. Invariants during rollback

- Committed entitlements and CONFIRMED orders remain durable.  
- Payment success alone never confirms.  
- Unresolved compensation remains visible.  
- Redis is not grant authority.

## 5. Verification status

| Environment | Status |
|---|---|
| Local process restart to default-A (P7.0) | Demonstrated (config smoke) |
| Registry previous-artifact rollback | **NOT VERIFIED** until executed on shared staging |

## 6. Evidence template

```
rollback_at_utc:
from_artifact:
to_artifact:
approver:
admission_meta_protocol:
smoke_order_id:
smoke_status:
notes:
```
