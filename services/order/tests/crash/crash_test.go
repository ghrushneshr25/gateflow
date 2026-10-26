//go:build integration && crash

package crash

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/libs/go/postgres"
	orderstore "github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

func crashSecret() string {
	return envOr("GATEFLOW_DEV_JWT_SECRET", authn.DefaultDevSecret)
}

func opsSaleToken(t *testing.T) string {
	t.Helper()
	tok, err := authn.MintOperatorJWT(crashSecret(), "crash-ops", []string{authn.ScopeSaleWrite, authn.ScopeSalePublish}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func buyerToken(t *testing.T, buyerID string) string {
	t.Helper()
	tok, err := authn.MintBuyerJWT(crashSecret(), buyerID, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func svcToken(t *testing.T, scopes ...string) string {
	t.Helper()
	if len(scopes) == 0 {
		scopes = authn.OrderServiceScopes
	}
	tok, err := authn.MintServiceJWT(crashSecret(), "crash-svc", scopes, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func dbURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://gateflow:gateflow@localhost:5432/gateflow?sslmode=disable"
}

func baseEnv(hookDir string) []string {
	secret := crashSecret()
	svc, _ := authn.MintServiceJWT(secret, "order-service", authn.OrderServiceScopes, 24*time.Hour)
	saleSvc, _ := authn.MintServiceJWT(secret, "sale-service", authn.SaleServiceScopes, 24*time.Hour)
	return []string{
		"DATABASE_URL=" + dbURL(),
		"ORDER_DATABASE_URL=" + dbURL(),
		"SALE_DATABASE_URL=" + dbURL(),
		"ADMISSION_DATABASE_URL=" + dbURL(),
		"INVENTORY_DATABASE_URL=" + dbURL(),
		"PAYMENT_DATABASE_URL=" + dbURL(),
		"SALE_BASE_URL=http://127.0.0.1:8081",
		"ADMISSION_BASE_URL=http://127.0.0.1:8082",
		"INVENTORY_BASE_URL=http://127.0.0.1:8083",
		"PAYMENT_BASE_URL=http://127.0.0.1:8085",
		"KAFKA_BROKERS=" + envOr("KAFKA_BROKERS", "localhost:9092"),
		"KAFKA_ENABLED=true",
		"GATEFLOW_TEST_HOOKS=1",
		"GATEFLOW_HOOK_DIR=" + hookDir,
		"GATEFLOW_CRASH_DELAY_MS=50",
		"GATEFLOW_DEV_JWT_SECRET=" + secret,
		"ENVIRONMENT=local",
		"GATEFLOW_SERVICE_TOKEN=" + svc,
		// sale process also mints; ensure secret shared
		"GATEFLOW_SALE_SERVICE_TOKEN=" + saleSvc,
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func withHTTPAddr(env []string, addr string) []string {
	return append(env, "HTTP_ADDR="+addr)
}

type stack struct {
	sale, adm, inv, pay, order *Proc
	hookDir                    string
	t                          *testing.T
}

func startStack(t *testing.T, orderExtra []string) *stack {
	t.Helper()
	hookDir := t.TempDir()
	logDir := t.TempDir()
	env := baseEnv(hookDir)
	s := &stack{hookDir: hookDir, t: t}
	var err error
	s.inv, err = startProc("inventory", "inventory", 8083, withHTTPAddr(env, ":8083"), filepath.Join(logDir, "inv.log"))
	if err != nil {
		t.Fatal(err)
	}
	s.sale, err = startProc("sale", "sale", 8081, withHTTPAddr(env, ":8081"), filepath.Join(logDir, "sale.log"))
	if err != nil {
		t.Fatal(err)
	}
	s.adm, err = startProc("admission", "admission", 8082, withHTTPAddr(env, ":8082"), filepath.Join(logDir, "adm.log"))
	if err != nil {
		t.Fatal(err)
	}
	s.pay, err = startProc("payment", "payment-simulator", 8085, withHTTPAddr(env, ":8085"), filepath.Join(logDir, "pay.log"))
	if err != nil {
		t.Fatal(err)
	}
	oenv := append(withHTTPAddr(env, ":8084"), orderExtra...)
	s.order, err = startProc("order", "order", 8084, oenv, filepath.Join(logDir, "ord.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.order.Kill()
		_ = s.pay.Kill()
		_ = s.adm.Kill()
		_ = s.sale.Kill()
		_ = s.inv.Kill()
	})
	return s
}

func postJSON(t *testing.T, url string, headers map[string]string, body any) (int, map[string]any) {
	t.Helper()
	code, out, err := postJSONErr(url, headers, body)
	if err != nil {
		t.Fatal(err)
	}
	return code, out
}

func postJSONErr(url string, headers map[string]string, body any) (int, map[string]any, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, map[string]any{"_err": err.Error()}, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{"_raw": string(raw)}
	}
	return resp.StatusCode, out, nil
}

func getJSON(t *testing.T, url string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func setupBooking(t *testing.T) (buyer, sessionID, ttID, poolID, entID string) {
	t.Helper()
	buyer = ids.New("buyer")
	ops := map[string]string{"Authorization": "Bearer " + opsSaleToken(t)}
	grantAuth := map[string]string{"Authorization": "Bearer " + svcToken(t, authn.ScopeAdmissionGrant)}
	_, off := postJSON(t, "http://127.0.0.1:8081/v1/offerings", ops, map[string]any{
		"name": "crash-" + buyer, "category": "concert",
	})
	offID, _ := off["id"].(string)
	start := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	end := time.Now().UTC().Add(52 * time.Hour).Format(time.RFC3339)
	open := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	closeT := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	_, sess := postJSON(t, "http://127.0.0.1:8081/v1/offerings/"+offID+"/sessions", ops, map[string]any{
		"starts_at": start, "ends_at": end, "sale_opens_at": open, "sale_closes_at": closeT, "max_per_buyer": 4,
	})
	sessionID, _ = sess["session"].(map[string]any)["id"].(string)
	_, tt := postJSON(t, "http://127.0.0.1:8081/v1/sessions/"+sessionID+"/ticket-types", ops, map[string]any{
		"name": "GA", "capacity": 5, "price_cents": 1000,
	})
	ttID, _ = tt["id"].(string)
	_, pub := postJSON(t, "http://127.0.0.1:8081/v1/offerings/"+offID+"/publish", ops, map[string]any{})
	tts, _ := pub["ticket_types"].([]any)
	if len(tts) == 0 {
		t.Fatalf("publish: %#v", pub)
	}
	poolID, _ = tts[0].(map[string]any)["inventory_pool_id"].(string)
	postJSON(t, "http://127.0.0.1:8082/v1/sessions/"+sessionID+"/queue/joins", buyerHdr(t, buyer, ""), map[string]any{})
	_, grant := postJSON(t, "http://127.0.0.1:8082/v1/sessions/"+sessionID+"/grants", grantAuth, map[string]any{"buyer_id": buyer})
	ent, _ := grant["entitlement"].(map[string]any)
	entID, _ = ent["id"].(string)
	if entID == "" || poolID == "" {
		t.Fatalf("setup failed grant=%v pool=%s", grant, poolID)
	}
	return
}

func orderDB(t *testing.T) *orderstore.Store {
	t.Helper()
	pool, err := postgres.NewPoolWithSearchPath(context.Background(), dbURL(), "order")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return orderstore.New(pool)
}

func countCompensations(t *testing.T, paymentOperationID string) (n int, opID, status string) {
	t.Helper()
	opID = "comp_" + paymentOperationID
	pool, err := postgres.NewPoolWithSearchPath(context.Background(), dbURL(), "payment")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var st string
	err = pool.QueryRow(context.Background(),
		`SELECT status FROM compensations WHERE operation_id=$1`, opID).Scan(&st)
	if err != nil {
		return 0, opID, ""
	}
	var cnt int
	_ = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM compensations WHERE operation_id=$1`, opID).Scan(&cnt)
	return cnt, opID, st
}

func waitDead(p *Proc, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) && p.Alive() {
		time.Sleep(50 * time.Millisecond)
	}
	_ = p.Kill()
}

func buyerHdr(t *testing.T, buyer, key string) map[string]string {
	h := map[string]string{"Authorization": "Bearer " + buyerToken(t, buyer)}
	if key != "" {
		h["Idempotency-Key"] = key
	}
	return h
}

func checkoutBody(sessionID, ttID, poolID, entID, key string) map[string]any {
	return map[string]any{
		"session_id": sessionID, "ticket_type_id": ttID, "pool_id": poolID,
		"entitlement_id": entID, "quantity": 1, "amount_cents": 1000, "idempotency_key": key,
	}
}

func TestCrashA_PaymentSuccessLostResponse(t *testing.T) {
	s := startStack(t, []string{"GATEFLOW_CRASH_BOUNDARY=after_payment_success"})
	buyer, sessionID, ttID, poolID, entID := setupBooking(t)
	key := ids.New("key")
	body := checkoutBody(sessionID, ttID, poolID, entID, key)
	hdr := buyerHdr(t, buyer, key)

	go func() { _, _, _ = postJSONErr("http://127.0.0.1:8084/v1/checkouts", hdr, body) }()

	if err := waitFile(filepath.Join(s.hookDir, "reached_after_payment_success"), 25*time.Second); err != nil {
		t.Fatalf("barrier: %v alive=%v log=%s", err, s.order.Alive(), fmtLog(s.order))
	}
	waitDead(s.order, 3*time.Second)

	if err := s.order.Restart(nil); err != nil {
		t.Fatal(err)
	}

	var status, payOp, oid string
	for i := 0; i < 12; i++ {
		_, resp := postJSON(t, "http://127.0.0.1:8084/v1/checkouts", hdr, body)
		status, _ = resp["status"].(string)
		payOp, _ = resp["payment_operation_id"].(string)
		oid, _ = resp["id"].(string)
		if status == "CONFIRMED" {
			break
		}
		if oid != "" {
			_, body2 := postJSON(t, "http://127.0.0.1:8084/v1/orders/"+oid+"/reconcile", buyerHdr(t, buyer, ""), map[string]any{})
			if ord, ok := body2["order"].(map[string]any); ok {
				status, _ = ord["status"].(string)
				if p2, _ := ord["payment_operation_id"].(string); p2 != "" {
					payOp = p2
				}
				if id2, _ := ord["id"].(string); id2 != "" {
					oid = id2
				}
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	if payOp == "" || oid == "" {
		t.Fatalf("missing ids status=%s", status)
	}
	pc, pay := getJSON(t, "http://127.0.0.1:8085/v1/payments/by-operation/"+payOp, map[string]string{"Authorization": "Bearer " + svcToken(t, authn.ScopePaymentCreate)})
	if pc != 200 || pay["status"] != "SUCCEEDED" {
		t.Fatalf("pay %d %#v", pc, pay)
	}
	if status != "CONFIRMED" {
		t.Fatalf("expected CONFIRMED got %s", status)
	}
	st := orderDB(t)
	o, err := st.Get(context.Background(), oid)
	if err != nil {
		t.Fatal(err)
	}
	if o.PaymentOperationID != payOp || o.Status != "CONFIRMED" {
		t.Fatalf("db op=%s status=%s", o.PaymentOperationID, o.Status)
	}
}

// TestCrashB_CompensationInterruptedAfterINVExpired:
// payment durable success observed → kill → release reservation while order is down →
// restart into compensation path → kill after compensation created → restart → never confirm,
// stable payment/compensation operation IDs, single compensation row.
func TestCrashB_CompensationInterruptedAfterINVExpired(t *testing.T) {
	secret := envOr("GATEFLOW_DEV_JWT_SECRET", "gateflow-dev-secret-change-me")
	s := startStack(t, []string{
		"GATEFLOW_CRASH_BOUNDARY=after_payment_success",
		"GATEFLOW_DEV_JWT_SECRET=" + secret,
	})
	buyer, sessionID, ttID, poolID, entID := setupBooking(t)
	key := ids.New("key")
	body := checkoutBody(sessionID, ttID, poolID, entID, key)
	hdr := buyerHdr(t, buyer, key)

	go func() { _, _, _ = postJSONErr("http://127.0.0.1:8084/v1/checkouts", hdr, body) }()
	if err := waitFile(filepath.Join(s.hookDir, "reached_after_payment_success"), 25*time.Second); err != nil {
		t.Fatalf("payment barrier: %v log=%s", err, fmtLog(s.order))
	}
	waitDead(s.order, 3*time.Second)

	// Order process is dead: read durable order + payment, release seat before any resume.
	st := orderDB(t)
	// Idempotency row must exist after first checkout attempt reached payment.
	o, err := st.GetByIdempotency(context.Background(), buyer, key)
	if err != nil {
		t.Fatalf("order by idempotency: %v", err)
	}
	oid := o.ID
	payOp := o.PaymentOperationID
	if payOp == "" {
		t.Fatal("missing payment_operation_id")
	}
	if o.ReservationID == nil || *o.ReservationID == "" {
		t.Fatalf("missing reservation_id status=%s", o.Status)
	}
	if o.Status == "CONFIRMED" {
		t.Fatal("confirmed before release")
	}
	resID := *o.ReservationID
	invOp := o.InventoryOperationID

	pc, pay := getJSON(t, "http://127.0.0.1:8085/v1/payments/by-operation/"+payOp, map[string]string{"Authorization": "Bearer " + svcToken(t, authn.ScopePaymentCreate)})
	if pc != 200 || pay["status"] != "SUCCEEDED" {
		t.Fatalf("durable payment %#v", pay)
	}
	invAuth := map[string]string{"Authorization": "Bearer " + svcToken(t, authn.ScopeInventoryWrite)}
	code, rel := postJSON(t, "http://127.0.0.1:8083/v1/reservations/"+resID+"/release", invAuth, map[string]any{})
	if code >= 500 {
		t.Fatalf("release %d %#v", code, rel)
	}
	_, inv := getJSON(t, "http://127.0.0.1:8083/v1/reservations/by-operation/"+invOp, nil)
	if inv["status"] != "RELEASED" && inv["status"] != "EXPIRED" {
		t.Fatalf("want RELEASED/EXPIRED %#v", inv)
	}
	if n0, _, _ := countCompensations(t, payOp); n0 != 0 {
		t.Fatal("compensation must not exist yet")
	}

	// Resume into INV_EXPIRED + compensation crash window
	_ = os.Remove(filepath.Join(s.hookDir, "reached_after_compensation_created"))
	if err := s.order.Restart([]string{
		"GATEFLOW_TEST_HOOKS=1",
		"GATEFLOW_HOOK_DIR=" + s.hookDir,
		"GATEFLOW_CRASH_BOUNDARY=after_compensation_created",
		"GATEFLOW_CRASH_EVENT_FILTER=" + payOp,
		"GATEFLOW_DEV_JWT_SECRET=" + secret,
	}); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _, _ = postJSONErr("http://127.0.0.1:8084/v1/checkouts", hdr, body)
	}()
	if err := waitFile(filepath.Join(s.hookDir, "reached_after_compensation_created"), 25*time.Second); err != nil {
		// try reconcile as alternate trigger
		go func() {
			_, _, _ = postJSONErr("http://127.0.0.1:8084/v1/orders/"+oid+"/reconcile", buyerHdr(t, buyer, ""), map[string]any{})
		}()
		if err2 := waitFile(filepath.Join(s.hookDir, "reached_after_compensation_created"), 15*time.Second); err2 != nil {
			t.Fatalf("compensation barrier: %v / %v log=%s", err, err2, fmtLog(s.order))
		}
	}
	waitDead(s.order, 3*time.Second)

	n1, compOp, compSt := countCompensations(t, payOp)
	if n1 != 1 || compOp != "comp_"+payOp {
		t.Fatalf("after kill: compensations n=%d op=%s", n1, compOp)
	}
	o2, err := st.Get(context.Background(), oid)
	if err != nil {
		t.Fatal(err)
	}
	if o2.Status == "CONFIRMED" {
		t.Fatal("must never confirm")
	}
	if o2.PaymentOperationID != payOp {
		t.Fatalf("pay op drift")
	}

	if err := s.order.Restart([]string{"GATEFLOW_DEV_JWT_SECRET=" + secret}); err != nil {
		t.Fatal(err)
	}
	var finalStatus, finalComp string
	for i := 0; i < 12; i++ {
		_, body2 := postJSON(t, "http://127.0.0.1:8084/v1/orders/"+oid+"/reconcile", buyerHdr(t, buyer, ""), map[string]any{})
		if ord, ok := body2["order"].(map[string]any); ok {
			finalStatus, _ = ord["status"].(string)
			finalComp, _ = ord["compensation_operation_id"].(string)
			if p2, _ := ord["payment_operation_id"].(string); p2 != "" && p2 != payOp {
				t.Fatalf("pay op changed")
			}
			if finalStatus == "CONFIRMED" {
				t.Fatal("confirmed after recovery")
			}
			if finalStatus == "RECONCILIATION_REQUIRED" && finalComp == compOp {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	o3, err := st.Get(context.Background(), oid)
	if err != nil {
		t.Fatal(err)
	}
	if o3.Status == "CONFIRMED" {
		t.Fatal("db confirmed")
	}
	if o3.Status != "RECONCILIATION_REQUIRED" && o3.Status != "PAYMENT_SUCCEEDED" && o3.Status != "FAILED" {
		// RECONCILIATION_REQUIRED is expected terminal for this path
		t.Logf("status=%s (want RECONCILIATION_REQUIRED)", o3.Status)
	}
	if o3.Status != "RECONCILIATION_REQUIRED" {
		// one more advance
		_, _ = postJSON(t, "http://127.0.0.1:8084/v1/orders/"+oid+"/reconcile", buyerHdr(t, buyer, ""), map[string]any{})
		o3, _ = st.Get(context.Background(), oid)
	}
	if o3.Status != "RECONCILIATION_REQUIRED" {
		t.Fatalf("want RECONCILIATION_REQUIRED got %s", o3.Status)
	}
	if o3.PaymentOperationID != payOp {
		t.Fatal("pay op drift db")
	}
	if o3.CompensationOperationID == nil || *o3.CompensationOperationID != compOp {
		t.Fatalf("want compensation_operation_id=%s got %#v", compOp, o3.CompensationOperationID)
	}
	n2, compOp2, compSt2 := countCompensations(t, payOp)
	if n2 != 1 || compOp2 != compOp {
		t.Fatalf("compensation uniqueness n=%d op=%s", n2, compOp2)
	}
	if compSt == "SUCCEEDED" && compSt2 != "SUCCEEDED" {
		t.Fatalf("successful compensation not recognized %s->%s", compSt, compSt2)
	}
	_, inv2 := getJSON(t, "http://127.0.0.1:8083/v1/reservations/by-operation/"+invOp, nil)
	if inv2["status"] == "ACTIVE" || inv2["status"] == "CONFIRMED" {
		t.Fatalf("reservation revived %#v", inv2)
	}
	// Second recovery is idempotent
	_, body3 := postJSON(t, "http://127.0.0.1:8084/v1/orders/"+oid+"/reconcile", buyerHdr(t, buyer, ""), map[string]any{})
	if ord, ok := body3["order"].(map[string]any); ok {
		if ord["status"] == "CONFIRMED" {
			t.Fatal("second reconcile confirmed")
		}
		if c2, _ := ord["compensation_operation_id"].(string); c2 != "" && c2 != compOp {
			t.Fatal("comp op changed")
		}
	}
	if n3, _, _ := countCompensations(t, payOp); n3 != 1 {
		t.Fatalf("comp count %d", n3)
	}
}

func TestCrashC_PublisherAfterKafkaAck(t *testing.T) {
	if os.Getenv("KAFKA_BROKERS") == "" && os.Getenv("SKIP_KAFKA") == "1" {
		t.Skip("kafka")
	}
	// Publisher only first so consumer lag does not interfere with ack-before-mark window.
	s := startStack(t, []string{
		"GATEFLOW_CRASH_BOUNDARY=after_kafka_ack",
		"GATEFLOW_OUTBOX_CONSUMER=0",
	})
	buyer, sessionID, ttID, poolID, entID := setupBooking(t)
	key := ids.New("key")
	hdr := buyerHdr(t, buyer, key)
	bodyReq := checkoutBody(sessionID, ttID, poolID, entID, key)

	code, body := postJSON(t, "http://127.0.0.1:8084/v1/checkouts", hdr, bodyReq)
	if code >= 500 {
		t.Fatalf("checkout %d %#v log=%s", code, body, fmtLog(s.order))
	}
	oid, _ := body["id"].(string)
	if body["status"] != "CONFIRMED" || oid == "" {
		t.Fatalf("want CONFIRMED %#v", body)
	}
	eventID := "evt_order_confirmed_" + oid

	if err := waitFile(filepath.Join(s.hookDir, "reached_after_kafka_ack"), 30*time.Second); err != nil {
		t.Fatalf("kafka ack barrier: %v alive=%v log=%s", err, s.order.Alive(), fmtLog(s.order))
	}
	waitDead(s.order, 3*time.Second)

	st := orderDB(t)
	o, err := st.Get(context.Background(), oid)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != "CONFIRMED" {
		t.Fatalf("status %s", o.Status)
	}
	if ok, _ := st.IsOutboxPublished(context.Background(), eventID); ok {
		t.Fatal("expected unpublished after ack-before-mark crash")
	}

	// Recover publisher+consumer. Use a fresh consumer group at *latest* so recovery
	// is not blocked draining historical load-test traffic on the shared topic
	// (FirstOffset + polluted topic → timeout before this order's effect appears).
	// Publisher will republish the still-unpublished outbox row; consumer at latest
	// observes that republish (at-least-once; same stable event_id).
	if err := s.order.Restart([]string{
		"GATEFLOW_KAFKA_GROUP=crash-c-" + oid,
		"GATEFLOW_KAFKA_START=latest",
		"GATEFLOW_CRASH_BOUNDARY=", // clear crash hook on recovery
	}); err != nil {
		t.Fatal(err)
	}
	// Brief settle so the consumer group joins at latest before republish races.
	time.Sleep(500 * time.Millisecond)

	pubDeadline := time.Now().Add(20 * time.Second)
	var published bool
	for time.Now().Before(pubDeadline) {
		published, _ = st.IsOutboxPublished(context.Background(), eventID)
		if published {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !published {
		t.Fatalf("not published after recovery log=%s", fmtLog(s.order))
	}
	effDeadline := time.Now().Add(45 * time.Second)
	var n int
	for time.Now().Before(effDeadline) {
		n, _ = st.CountConfirmedEffects(context.Background(), oid)
		if n == 1 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if n != 1 {
		t.Fatalf("effects want 1 got %d order=%s event=%s log=%s", n, oid, eventID, fmtLog(s.order))
	}
}

func TestCrashD_ConsumerAfterDBBeforeOffset(t *testing.T) {
	if os.Getenv("KAFKA_BROKERS") == "" {
		t.Skip("need kafka")
	}
	// Join a fresh group at latest, then publish so only this message is consumed.
	group := "crash-d-" + ids.New("g")
	s := startStack(t, []string{
		"GATEFLOW_OUTBOX_PUBLISHER=0",
		"GATEFLOW_OUTBOX_CONSUMER=0",
	})
	buyer, sessionID, ttID, poolID, entID := setupBooking(t)
	key := ids.New("key")
	hdr := buyerHdr(t, buyer, key)
	bodyReq := checkoutBody(sessionID, ttID, poolID, entID, key)

	// Confirm + outbox row without publishing yet... need publisher for kafka.
	// Step 1: publisher only → confirm + kafka write, no consume.
	if err := s.order.Restart([]string{"GATEFLOW_OUTBOX_CONSUMER=0"}); err != nil {
		t.Fatal(err)
	}
	code, body := postJSON(t, "http://127.0.0.1:8084/v1/checkouts", hdr, bodyReq)
	if code >= 500 {
		t.Fatalf("checkout %d %#v", code, body)
	}
	oid, _ := body["id"].(string)
	if body["status"] != "CONFIRMED" || oid == "" {
		t.Fatalf("want CONFIRMED %#v", body)
	}
	eventID := "evt_order_confirmed_" + oid
	st := orderDB(t)
	pubDeadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(pubDeadline) {
		ok, _ := st.IsOutboxPublished(context.Background(), eventID)
		if ok {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if ok, _ := st.IsOutboxPublished(context.Background(), eventID); !ok {
		t.Fatalf("not published before consumer phase log=%s", fmtLog(s.order))
	}

	// Step 2: fresh group from earliest so this event is visible; crash only on it.
	_ = os.Remove(filepath.Join(s.hookDir, "reached_after_consumer_db"))
	if err := s.order.Restart([]string{
		"GATEFLOW_TEST_HOOKS=1",
		"GATEFLOW_HOOK_DIR=" + s.hookDir,
		"GATEFLOW_CRASH_BOUNDARY=after_consumer_db",
		"GATEFLOW_CRASH_EVENT_FILTER=" + eventID,
		"GATEFLOW_OUTBOX_PUBLISHER=0",
		"GATEFLOW_KAFKA_GROUP=" + group,
	}); err != nil {
		t.Fatal(err)
	}

	if err := waitFile(filepath.Join(s.hookDir, "reached_after_consumer_db"), 60*time.Second); err != nil {
		t.Fatalf("consumer barrier: %v alive=%v log=%s", err, s.order.Alive(), fmtLog(s.order))
	}
	waitDead(s.order, 3*time.Second)

	n, err := st.CountConfirmedEffects(context.Background(), oid)
	if err != nil || n != 1 {
		t.Fatalf("pre-restart effects=%d err=%v", n, err)
	}

	// Step 3: redelivery with same group — must not duplicate effect
	if err := s.order.Restart([]string{
		"GATEFLOW_KAFKA_GROUP=" + group,
		"GATEFLOW_OUTBOX_PUBLISHER=0",
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Second)
	n2, err := st.CountConfirmedEffects(context.Background(), oid)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 1 {
		t.Fatalf("post-restart effects want 1 got %d", n2)
	}
}

func TestCrashE_RepeatedRestartPayment(t *testing.T) {
	s := startStack(t, []string{"GATEFLOW_CRASH_BOUNDARY=after_payment_success"})
	buyer, sessionID, ttID, poolID, entID := setupBooking(t)
	key := ids.New("key")
	bodyReq := checkoutBody(sessionID, ttID, poolID, entID, key)
	hdr := buyerHdr(t, buyer, key)
	go func() { _, _, _ = postJSONErr("http://127.0.0.1:8084/v1/checkouts", hdr, bodyReq) }()
	_ = waitFile(filepath.Join(s.hookDir, "reached_after_payment_success"), 25*time.Second)
	_ = s.order.Kill()
	for i := 0; i < 2; i++ {
		_ = os.Remove(filepath.Join(s.hookDir, "reached_after_payment_success"))
		if err := s.order.Restart([]string{
			"GATEFLOW_TEST_HOOKS=1",
			"GATEFLOW_HOOK_DIR=" + s.hookDir,
			"GATEFLOW_CRASH_BOUNDARY=after_payment_success",
		}); err != nil {
			t.Fatal(err)
		}
		go func() { _, _, _ = postJSONErr("http://127.0.0.1:8084/v1/checkouts", hdr, bodyReq) }()
		_ = waitFile(filepath.Join(s.hookDir, "reached_after_payment_success"), 20*time.Second)
		_ = s.order.Kill()
	}
	if err := s.order.Restart(nil); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	for i := 0; i < 10; i++ {
		_, body = postJSON(t, "http://127.0.0.1:8084/v1/checkouts", hdr, bodyReq)
		if body["status"] == "CONFIRMED" {
			break
		}
		if oid, _ := body["id"].(string); oid != "" {
			_, body2 := postJSON(t, "http://127.0.0.1:8084/v1/orders/"+oid+"/reconcile", buyerHdr(t, buyer, ""), map[string]any{})
			if ord, ok := body2["order"].(map[string]any); ok {
				body = ord
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	if body["status"] != "CONFIRMED" {
		t.Fatalf("%#v", body)
	}
	payOp, _ := body["payment_operation_id"].(string)
	if payOp == "" {
		t.Fatal("missing pay op")
	}
	_, pay := getJSON(t, "http://127.0.0.1:8085/v1/payments/by-operation/"+payOp, map[string]string{"Authorization": "Bearer " + svcToken(t, authn.ScopePaymentCreate)})
	if pay["status"] != "SUCCEEDED" {
		t.Fatalf("%#v", pay)
	}
}

func fmtLog(p *Proc) string {
	if p == nil {
		return ""
	}
	b, _ := os.ReadFile(p.LogPath)
	if len(b) > 2000 {
		return string(b[len(b)-2000:])
	}
	return string(b)
}
