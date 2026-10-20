//go:build integration

package tests

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/libs/go/postgres"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/orchestrate"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

func orderStore(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("ORDER_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := postgres.NewPoolWithSearchPath(context.Background(), url, "order")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

type recDS struct {
	mu       sync.Mutex
	res      map[string]struct{ id, st string }
	pay      map[string]struct{ id, st string }
	payCalls int
}

func (d *recDS) BindEntitlement(context.Context, string, string, string, string) error {
	return nil
}
func (d *recDS) ConsumeEntitlement(context.Context, string, string, string) error { return nil }
func (d *recDS) Reserve(_ context.Context, op, _, _, _ string, _, _ int, _ string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.res == nil {
		d.res = map[string]struct{ id, st string }{}
	}
	if r, ok := d.res[op]; ok {
		return r.id, nil
	}
	id := "res_" + op
	d.res[op] = struct{ id, st string }{id, "ACTIVE"}
	return id, nil
}
func (d *recDS) GetReservationByOperation(_ context.Context, op, _ string) (string, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.res[op]
	if !ok {
		return "", "", orchestrate.ErrNotFound
	}
	return r.id, r.st, nil
}
func (d *recDS) ConfirmReservation(_ context.Context, resID, _ string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, v := range d.res {
		if v.id == resID {
			v.st = "CONFIRMED"
			d.res[k] = v
			return "CONFIRMED", nil
		}
	}
	return "CONFIRMED", nil
}
func (d *recDS) CreatePayment(_ context.Context, op, _ string, _ int64, _, _, _ string) (string, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.payCalls++
	if d.pay == nil {
		d.pay = map[string]struct{ id, st string }{}
	}
	if p, ok := d.pay[op]; ok {
		return p.id, p.st, nil
	}
	id := "pay_" + op
	d.pay[op] = struct{ id, st string }{id, "SUCCEEDED"}
	return id, "SUCCEEDED", nil
}
func (d *recDS) GetPaymentByOperation(_ context.Context, op, _ string) (string, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.pay[op]
	if !ok {
		return "", "", orchestrate.ErrNotFound
	}
	return p.id, p.st, nil
}
func (d *recDS) EnsureCompensation(_ context.Context, payOp, _, _ string, _ bool) (string, string, error) {
	return "comp_" + payOp, "SUCCEEDED", nil
}

func createOrder(t *testing.T, st *store.Store, status string) store.Order {
	t.Helper()
	now := time.Now().UTC()
	o := store.Order{
		ID: ids.New("ord"), BuyerID: ids.New("buyer"), SessionID: ids.New("ses"),
		TicketTypeID: "tt", PoolID: "pool", Quantity: 1, Status: status,
		EntitlementID: ids.New("ent"), CheckoutOperationID: ids.New("chk"),
		InventoryOperationID: ids.New("invop"), PaymentOperationID: ids.New("payop"),
		AmountCents: 100, Currency: "USD", MaxPerBuyer: 4,
		IdempotencyKey: ids.New("key"), RequestHash: ids.New("hash"),
	}
	got, err := st.CreateWithHold(context.Background(), o, ids.New("hold"), now)
	if err != nil {
		t.Fatal(err)
	}
	if status != orchestrate.StatusCreated {
		got.Status = status
		got.UpdatedAt = now
		if err := st.Update(context.Background(), got); err != nil {
			t.Fatal(err)
		}
	}
	return got
}

func TestP41_ProcessRestartResume(t *testing.T) {
	st := orderStore(t)
	o := createOrder(t, st, orchestrate.StatusCreated)
	ds := &recDS{}
	d := &orchestrate.Driver{Store: st, Downstream: ds}

	// Partial: bind+reserve only by advancing once... full advance confirms.
	// Simulate restart mid-flight: persist ENTITLEMENT_BOUND then advance.
	o.Status = orchestrate.StatusEntitlementBound
	o.UpdatedAt = time.Now().UTC()
	_ = st.Update(context.Background(), o)

	out, err := d.Advance(context.Background(), o.ID, "corr")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != orchestrate.StatusConfirmed {
		t.Fatalf("%s", out.Status)
	}
	n, err := st.CountOutboxByEventID(context.Background(), "evt_order_confirmed_"+o.ID)
	if err != nil || n != 1 {
		t.Fatalf("outbox n=%d err=%v", n, err)
	}
	// "restart" again
	out2, _ := d.Advance(context.Background(), o.ID, "corr")
	if out2.Status != orchestrate.StatusConfirmed {
		t.Fatal(out2.Status)
	}
	n2, _ := st.CountOutboxForAggregate(context.Background(), o.ID)
	if n2 != 1 {
		t.Fatalf("duplicate outbox %d", n2)
	}
}

func TestP41_PaymentSucceededLookupNoSecondCreate(t *testing.T) {
	st := orderStore(t)
	o := createOrder(t, st, orchestrate.StatusReservationHeld)
	ds := &recDS{
		res: map[string]struct{ id, st string }{o.InventoryOperationID: {"res_x", "ACTIVE"}},
		pay: map[string]struct{ id, st string }{o.PaymentOperationID: {"pay_pre", "SUCCEEDED"}},
	}
	res := "res_x"
	o.ReservationID = &res
	o.UpdatedAt = time.Now().UTC()
	_ = st.Update(context.Background(), o)

	d := &orchestrate.Driver{Store: st, Downstream: ds}
	out, err := d.Advance(context.Background(), o.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != orchestrate.StatusConfirmed {
		t.Fatalf("%s", out.Status)
	}
	if ds.payCalls != 0 {
		// CreatePayment should not be called when Get finds existing
		// ensurePayment tries Get first — payCalls only on Create
		t.Fatalf("pay create calls=%d", ds.payCalls)
	}
}

func TestP41_DuplicateReconcile(t *testing.T) {
	st := orderStore(t)
	o := createOrder(t, st, orchestrate.StatusCreated)
	ds := &recDS{}
	d := &orchestrate.Driver{Store: st, Downstream: ds}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = d.Advance(context.Background(), o.ID, "c")
		}()
	}
	wg.Wait()
	got, err := st.Get(context.Background(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != orchestrate.StatusConfirmed {
		t.Fatalf("%s", got.Status)
	}
	n, _ := st.CountOutboxForAggregate(context.Background(), o.ID)
	if n != 1 {
		t.Fatalf("outbox=%d", n)
	}
}

func TestP41_ExpiredLeavesReconciliation(t *testing.T) {
	st := orderStore(t)
	o := createOrder(t, st, orchestrate.StatusPaymentSucceeded)
	ds := &recDS{
		res: map[string]struct{ id, st string }{o.InventoryOperationID: {"res_e", "EXPIRED"}},
	}
	res := "res_e"
	o.ReservationID = &res
	_ = st.Update(context.Background(), o)
	d := &orchestrate.Driver{Store: st, Downstream: ds}
	out, err := d.Advance(context.Background(), o.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != orchestrate.StatusReconciliationRequired {
		t.Fatalf("%s", out.Status)
	}
	n, _ := st.CountOutboxForAggregate(context.Background(), o.ID)
	if n != 0 {
		t.Fatal("must not outbox confirm")
	}
}
