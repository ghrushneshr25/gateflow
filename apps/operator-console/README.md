# Gateflow Operator Console

P5.2.0 recovery spine: reconciliation, outbox replay, admission coordination.

## Dev

```bash
# from repo root — mint operator token (local CLI only)
go run ./tools/devtoken operator \
  ops:sale:write,ops:sale:publish,ops:order:recover,ops:outbox:read,ops:outbox:replay,ops:admission:read,ops:admission:fence,ops:admission:budget

cd apps/operator-console
npm install
npm run dev   # http://127.0.0.1:5174
```

Paste the token into **Operator JWT** (stored in `sessionStorage` for the tab only).

API bases (defaults):

- `VITE_SALE_API_BASE=http://127.0.0.1:8081`
- `VITE_ORDER_API_BASE=http://127.0.0.1:8084`
- `VITE_ADMISSION_API_BASE=http://127.0.0.1:8082`

Pages: Sales (draft → session → ticket type → publish), Budgets (caps), plus P5.2.0 recovery.

Ensure Order and Admission services are running with matching `GATEFLOW_DEV_JWT_SECRET`.

## Scripts

- `npm test` — Vitest
- `npm run typecheck`
- `npm run build`
