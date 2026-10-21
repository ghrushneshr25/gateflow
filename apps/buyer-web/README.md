# Gateflow Buyer Web (P5.3.0)

Vertical slice: **discover → queue → grant → checkout → order status** using buyer JWT only.

## Local run

```bash
# mint buyer token (CLI only — not an HTTP mint API)
go run ./tools/devtoken buyer buyer:self buyer_demo_1

cd apps/buyer-web
npm install
npm run dev   # :5173
```

Paste the token into **Buyer JWT** (sessionStorage, tab-local).

Requires published offering (operator console Sales) and running Sale/Admission/Order/Inventory/Payment.

## Identity

- Authorization: `Authorization: Bearer <buyer JWT>`
- **Never** `X-Buyer-ID` for auth
- Checkout reuses one `Idempotency-Key` per **buyer + session + ticket** until CONFIRMED
- Sign-out clears tokens and checkout keys (no cross-buyer leakage in the tab)
- Timeouts: keep the same key; use Find prior order / Orders refresh before a new attempt
- CONFIRMED is only shown from server order status

## Scripts

- `npm test` · `npm run typecheck` · `npm run build`
