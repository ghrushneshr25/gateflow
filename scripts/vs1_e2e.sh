#!/usr/bin/env bash
set -euo pipefail
# End-to-end VS-1 happy path against local services.

SALE=${SALE_BASE:-http://127.0.0.1:8081}
ADM=${ADMISSION_BASE:-http://127.0.0.1:8082}
ORD=${ORDER_BASE:-http://127.0.0.1:8084}
# Unique buyer/key per run avoids collisions with prior local DB state.
BUYER=${BUYER_ID:-buyer_demo_$(date +%s)_$$}
IDEM_KEY=${IDEM_KEY:-vs1-key-${BUYER}}
CURL=(curl -sf --max-time 20)

echo "1) create offering"
OFF=$("${CURL[@]}" -X POST "$SALE/v1/offerings" -H 'Content-Type: application/json' \
  -d '{"name":"VS1 Concert","description":"Phase2 slice","category":"concert"}')
OFF_ID=$(echo "$OFF" | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')
echo "   offering=$OFF_ID"

START=$(python3 - <<'PY'
from datetime import datetime, timedelta, timezone
print((datetime.now(timezone.utc)+timedelta(days=7)).strftime("%Y-%m-%dT%H:%M:%SZ"))
PY
)
END=$(python3 - <<'PY'
from datetime import datetime, timedelta, timezone
print((datetime.now(timezone.utc)+timedelta(days=7, hours=3)).strftime("%Y-%m-%dT%H:%M:%SZ"))
PY
)
OPEN=$(python3 - <<'PY'
from datetime import datetime, timedelta, timezone
print((datetime.now(timezone.utc)-timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%SZ"))
PY
)
CLOSE=$(python3 - <<'PY'
from datetime import datetime, timedelta, timezone
print((datetime.now(timezone.utc)+timedelta(days=6)).strftime("%Y-%m-%dT%H:%M:%SZ"))
PY
)

echo "2) create session"
SESS=$("${CURL[@]}" -X POST "$SALE/v1/offerings/$OFF_ID/sessions" -H 'Content-Type: application/json' \
  -d "{\"starts_at\":\"$START\",\"ends_at\":\"$END\",\"sale_opens_at\":\"$OPEN\",\"sale_closes_at\":\"$CLOSE\",\"max_per_buyer\":4}")
SES_ID=$(echo "$SESS" | python3 -c 'import sys,json; print(json.load(sys.stdin)["session"]["id"])')
echo "   session=$SES_ID"

echo "3) ticket type capacity=2"
TT=$("${CURL[@]}" -X POST "$SALE/v1/sessions/$SES_ID/ticket-types" -H 'Content-Type: application/json' \
  -d '{"name":"GA","capacity":2,"price_cents":5000}')
TT_ID=$(echo "$TT" | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')
echo "   ticket_type=$TT_ID"

echo "4) publish (creates inventory pool)"
PUB=$("${CURL[@]}" -X POST "$SALE/v1/offerings/$OFF_ID/publish")
POOL_ID=$(echo "$PUB" | python3 -c 'import sys,json; print(json.load(sys.stdin)["ticket_types"][0]["inventory_pool_id"])')
echo "   pool=$POOL_ID"

echo "5) join queue + grant (buyer=$BUYER)"
"${CURL[@]}" -X POST "$ADM/v1/sessions/$SES_ID/queue/joins" -H "X-Buyer-ID: $BUYER" -H 'Content-Type: application/json' -d "{}" >/dev/null
# idempotent rejoin
"${CURL[@]}" -X POST "$ADM/v1/sessions/$SES_ID/queue/joins" -H "X-Buyer-ID: $BUYER" -H 'Content-Type: application/json' -d "{}" >/dev/null
GRANT=$("${CURL[@]}" -X POST "$ADM/v1/sessions/$SES_ID/grants" -H 'Content-Type: application/json' -d "{\"buyer_id\":\"$BUYER\"}")
ENT=$(echo "$GRANT" | python3 -c 'import sys,json; print(json.load(sys.stdin)["entitlement"]["id"])')
echo "   entitlement=$ENT"

echo "6) checkout"
ORDER=$("${CURL[@]}" -X POST "$ORD/v1/checkouts" \
  -H "Content-Type: application/json" \
  -H "X-Buyer-ID: $BUYER" \
  -H "Idempotency-Key: $IDEM_KEY" \
  -d "{\"session_id\":\"$SES_ID\",\"ticket_type_id\":\"$TT_ID\",\"pool_id\":\"$POOL_ID\",\"entitlement_id\":\"$ENT\",\"quantity\":1,\"amount_cents\":5000,\"idempotency_key\":\"$IDEM_KEY\"}")
STATUS=$(echo "$ORDER" | python3 -c 'import sys,json; print(json.load(sys.stdin)["status"])')
OID=$(echo "$ORDER" | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')
echo "   order=$OID status=$STATUS"
test "$STATUS" = "CONFIRMED"

echo "7) idempotent checkout replay"
ORDER2=$("${CURL[@]}" -X POST "$ORD/v1/checkouts" \
  -H "Content-Type: application/json" \
  -H "X-Buyer-ID: $BUYER" \
  -H "Idempotency-Key: $IDEM_KEY" \
  -d "{\"session_id\":\"$SES_ID\",\"ticket_type_id\":\"$TT_ID\",\"pool_id\":\"$POOL_ID\",\"entitlement_id\":\"$ENT\",\"quantity\":1,\"amount_cents\":5000,\"idempotency_key\":\"$IDEM_KEY\"}")
OID2=$(echo "$ORDER2" | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')
test "$OID" = "$OID2"

echo "8) published offerings visible"
"${CURL[@]}" "$SALE/v1/offerings?buyer=1" | python3 -c 'import sys,json; d=json.load(sys.stdin); assert any(i["id"]=="'"$OFF_ID"'" for i in d["items"])'

echo "VS1_E2E_PASS order=$OID"
