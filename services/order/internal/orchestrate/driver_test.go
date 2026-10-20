package orchestrate_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/services/order/internal/orchestrate"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

type memStore struct {
	mu     sync.Mutex
	orders map[string]store.Order
	outbox map[string]string // event_id -> aggregate
}

func newMem() *memStore {
	return &memStore{orders: map[string]store.Order{}, outbox: map[string]string{}}
}

func (m *memStore) Get(_ context.Context, id string) (store.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orders[id]
	if !ok {
		return store.Order{}, store.ErrNotFound
	}
	return o, nil
}
func (m *memStore) Update(_ context.Context, o store.Order) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orders[o.ID] = o
	return nil
}
func (m *memStore) Confirm(_ context.Context, orderID, eventID, _ string, _ any, now time.Time) (store.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.orders[orderID]
	if !ok {
		return store.Order{}, store.ErrNotFound
	}
	if o.Status == orchestrate.StatusConfirmed {
		return o, nil
	}
	o.Status = orchestrate.StatusConfirmed
	o.UpdatedAt = now
	m.orders[orderID] = o
	m.outbox[eventID] = orderID
	return o, nil
}
func (m *memStore) Fail(_ context.Context, orderID string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.orders[orderID]
	o.Status = orchestrate.StatusFailed
	o.UpdatedAt = now
	m.orders[orderID] = o
	return nil
}

type mockDS struct {
	mu sync.Mutex

	bindErr    error
	consumeErr error
	consumeN   atomic.Int64

	resByOp    map[string]struct{ id, status string }
	reserveN   atomic.Int64
	confirmN   atomic.Int64
	confirmErr error

	payByOp  map[string]struct{ id, status string }
	payCreateN atomic.Int64
	payCreateErr error
	// first CreatePayment returns ambiguous (transport); second path uses Get
	payAmbiguousOnce bool
	payAmbiguousHit  atomic.Bool
}

func (m *mockDS) BindEntitlement(context.Context, string, string, string, string) error {
	return m.bindErr
}
func (m *mockDS) ConsumeEntitlement(context.Context, string, string, string) error {
	m.consumeN.Add(1)
	return m.consumeErr
}
func (m *mockDS) Reserve(_ context.Context, operationID, _, _, _ string, _ int, _ int, _ string) (string, error) {
	m.reserveN.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.resByOp == nil {
		m.resByOp = map[string]struct{ id, status string }{}
	}
	if r, ok := m.resByOp[operationID]; ok {
		return r.id, nil
	}
	id := "res_" + operationID
	m.resByOp[operationID] = struct{ id, status string }{id, "ACTIVE"}
	return id, nil
}
func (m *mockDS) GetReservationByOperation(_ context.Context, operationID, _ string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.resByOp[operationID]
	if !ok {
		return "", "", orchestrate.ErrNotFound
	}
	return r.id, r.status, nil
}
func (m *mockDS) ConfirmReservation(_ context.Context, reservationID, _ string) (string, error) {
	m.confirmN.Add(1)
	if m.confirmErr != nil {
		return "", m.confirmErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, v := range m.resByOp {
		if v.id == reservationID {
			v.status = "CONFIRMED"
			m.resByOp[k] = v
			return "CONFIRMED", nil
		}
	}
	return "CONFIRMED", nil
}
func (m *mockDS) CreatePayment(_ context.Context, operationID, _ string, _ int64, _, _, _ string) (string, string, error) {
	m.payCreateN.Add(1)
	if m.payAmbiguousOnce && !m.payAmbiguousHit.Load() {
		m.payAmbiguousHit.Store(true)
		return "", "", orchestrate.ErrAmbiguous
	}
	if m.payCreateErr != nil {
		return "", "", m.payCreateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.payByOp == nil {
		m.payByOp = map[string]struct{ id, status string }{}
	}
	if p, ok := m.payByOp[operationID]; ok {
		return p.id, p.status, nil
	}
	id := "pay_" + operationID
	m.payByOp[operationID] = struct{ id, status string }{id, "SUCCEEDED"}
	return id, "SUCCEEDED", nil
}
func (m *mockDS) GetPaymentByOperation(_ context.Context, operationID, _ string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.payByOp[operationID]
	if !ok {
		return "", "", orchestrate.ErrNotFound
	}
	return p.id, p.status, nil
}
func (m *mockDS) EnsureCompensation(_ context.Context, paymentOperationID, _, _ string, _ bool) (string, string, error) {
	return "comp_" + paymentOperationID, "SUCCEEDED", nil
}

func seedOrder(m *memStore, status string) store.Order {
	o := store.Order{
		ID: "ord_1", BuyerID: "b1", SessionID: "s1", TicketTypeID: "tt", PoolID: "pool",
		Quantity: 1, Status: status, EntitlementID: "ent",
		CheckoutOperationID: "chk_1", InventoryOperationID: "invop_1", PaymentOperationID: "payop_1",
		AmountCents: 100, Currency: "USD", MaxPerBuyer: 4, IdempotencyKey: "k1", RequestHash: "h",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	m.orders[o.ID] = o
	return o
}

func TestAdvanceHappyPath(t *testing.T) {
	ms := newMem()
	seedOrder(ms, orchestrate.StatusCreated)
	ds := &mockDS{}
	d := &orchestrate.Driver{Store: ms, Downstream: ds, PaymentScenario: "success"}
	out, err := d.Advance(context.Background(), "ord_1", "c")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != orchestrate.StatusConfirmed {
		t.Fatalf("status=%s", out.Status)
	}
	if len(ms.outbox) != 1 {
		t.Fatalf("outbox=%d", len(ms.outbox))
	}
}

func TestPaymentAmbiguousThenResume(t *testing.T) {
	ms := newMem()
	seedOrder(ms, orchestrate.StatusCreated)
	ds := &mockDS{payAmbiguousOnce: true}
	d := &orchestrate.Driver{Store: ms, Downstream: ds}
	out, err := d.Advance(context.Background(), "ord_1", "c")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != orchestrate.StatusPaymentPending {
		t.Fatalf("want PAYMENT_PENDING got %s", out.Status)
	}
	// Simulate payment landed after timeout
	ds.mu.Lock()
	ds.payByOp = map[string]struct{ id, status string }{
		"payop_1": {id: "pay_x", status: "SUCCEEDED"},
	}
	ds.mu.Unlock()
	out, err = d.Advance(context.Background(), "ord_1", "c")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != orchestrate.StatusConfirmed {
		t.Fatalf("got %s", out.Status)
	}
	if ds.payCreateN.Load() != 1 {
		t.Fatalf("payment creates=%d want 1", ds.payCreateN.Load())
	}
}

func TestInventoryConfirmLostOrderWrite(t *testing.T) {
	ms := newMem()
	o := seedOrder(ms, orchestrate.StatusPaymentSucceeded)
	res := "res_1"
	o.ReservationID = &res
	ms.orders[o.ID] = o
	ds := &mockDS{resByOp: map[string]struct{ id, status string }{
		"invop_1": {id: "res_1", status: "CONFIRMED"}, // already confirmed downstream
	}}
	d := &orchestrate.Driver{Store: ms, Downstream: ds}
	out, err := d.Advance(context.Background(), "ord_1", "c")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != orchestrate.StatusConfirmed {
		t.Fatalf("%s", out.Status)
	}
	// second advance idempotent
	out2, _ := d.Advance(context.Background(), "ord_1", "c")
	if out2.Status != orchestrate.StatusConfirmed {
		t.Fatal(out2.Status)
	}
	if len(ms.outbox) != 1 {
		t.Fatalf("duplicate outbox %d", len(ms.outbox))
	}
}

func TestExpiredReservationNotConfirmed(t *testing.T) {
	ms := newMem()
	o := seedOrder(ms, orchestrate.StatusPaymentSucceeded)
	res := "res_1"
	o.ReservationID = &res
	ms.orders[o.ID] = o
	ds := &mockDS{resByOp: map[string]struct{ id, status string }{
		"invop_1": {id: "res_1", status: "EXPIRED"},
	}}
	d := &orchestrate.Driver{Store: ms, Downstream: ds}
	out, err := d.Advance(context.Background(), "ord_1", "c")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != orchestrate.StatusReconciliationRequired {
		t.Fatalf("got %s", out.Status)
	}
	if out.Status == orchestrate.StatusConfirmed {
		t.Fatal("must not confirm")
	}
}

func TestDuplicateConcurrentAdvance(t *testing.T) {
	ms := newMem()
	seedOrder(ms, orchestrate.StatusCreated)
	ds := &mockDS{}
	d := &orchestrate.Driver{Store: ms, Downstream: ds}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = d.Advance(context.Background(), "ord_1", "c")
		}()
	}
	wg.Wait()
	o, _ := ms.Get(context.Background(), "ord_1")
	if o.Status != orchestrate.StatusConfirmed {
		t.Fatalf("%s", o.Status)
	}
	if len(ms.outbox) != 1 {
		t.Fatalf("outbox=%v", ms.outbox)
	}
	if ds.payCreateN.Load() < 1 {
		t.Fatal("expected payment")
	}
}

func TestConsumeDefinitiveFailure(t *testing.T) {
	ms := newMem()
	seedOrder(ms, orchestrate.StatusReservationHeld)
	res := "res_1"
	o := ms.orders["ord_1"]
	o.ReservationID = &res
	ms.orders["ord_1"] = o
	ds := &mockDS{
		resByOp: map[string]struct{ id, status string }{"invop_1": {id: "res_1", status: "ACTIVE"}},
		consumeErr: orchestrate.ErrDefinitive{Code: "ENTITLEMENT_CONSUME", Msg: "nope"},
	}
	d := &orchestrate.Driver{Store: ms, Downstream: ds}
	out, _ := d.Advance(context.Background(), "ord_1", "c")
	if out.Status != orchestrate.StatusReconciliationRequired {
		t.Fatalf("got %s", out.Status)
	}
}

func TestReserveIdempotentAfterAmbiguous(t *testing.T) {
	ms := newMem()
	seedOrder(ms, orchestrate.StatusEntitlementBound)
	ds := &mockDS{}
	// Pre-seed reservation as if first attempt succeeded server-side
	ds.resByOp = map[string]struct{ id, status string }{
		"invop_1": {id: "res_existing", status: "ACTIVE"},
	}
	d := &orchestrate.Driver{Store: ms, Downstream: ds}
	out, err := d.Advance(context.Background(), "ord_1", "c")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != orchestrate.StatusConfirmed {
		t.Fatalf("%s", out.Status)
	}
	if ds.reserveN.Load() != 0 {
		// ensureReservation should find existing via Get first
		t.Fatalf("reserve calls=%d", ds.reserveN.Load())
	}
}

func TestPaymentFailedDefinitive(t *testing.T) {
	ms := newMem()
	seedOrder(ms, orchestrate.StatusReservationHeld)
	res := "res_1"
	o := ms.orders["ord_1"]
	o.ReservationID = &res
	ms.orders["ord_1"] = o
	ds := &mockDS{
		resByOp: map[string]struct{ id, status string }{"invop_1": {"res_1", "ACTIVE"}},
		payCreateErr: nil,
	}
	ds.payByOp = map[string]struct{ id, status string }{}
	// override create to return FAILED
	orig := ds
	_ = orig
	ds2 := &failPayDS{mockDS: *ds}
	d := &orchestrate.Driver{Store: ms, Downstream: ds2}
	out, _ := d.Advance(context.Background(), "ord_1", "c")
	if out.Status != orchestrate.StatusFailed {
		t.Fatalf("%s", out.Status)
	}
}

type failPayDS struct {
	mockDS
}

func (f *failPayDS) CreatePayment(ctx context.Context, operationID, orderID string, amount int64, currency, scenario, corr string) (string, string, error) {
	return "pay_f", "FAILED", nil
}
func (f *failPayDS) GetPaymentByOperation(ctx context.Context, operationID, corr string) (string, string, error) {
	return f.mockDS.GetPaymentByOperation(ctx, operationID, corr)
}
func (f *failPayDS) BindEntitlement(ctx context.Context, a, b, c, d string) error {
	return f.mockDS.BindEntitlement(ctx, a, b, c, d)
}
func (f *failPayDS) ConsumeEntitlement(ctx context.Context, a, b, c string) error {
	return f.mockDS.ConsumeEntitlement(ctx, a, b, c)
}
func (f *failPayDS) Reserve(ctx context.Context, a, b, c, d string, qty, lease int, corr string) (string, error) {
	return f.mockDS.Reserve(ctx, a, b, c, d, qty, lease, corr)
}
func (f *failPayDS) GetReservationByOperation(ctx context.Context, a, b string) (string, string, error) {
	return f.mockDS.GetReservationByOperation(ctx, a, b)
}
func (f *failPayDS) ConfirmReservation(ctx context.Context, a, b string) (string, error) {
	return f.mockDS.ConfirmReservation(ctx, a, b)
}
func (f *failPayDS) EnsureCompensation(ctx context.Context, a, b, c string, d bool) (string, string, error) {
	return f.mockDS.EnsureCompensation(ctx, a, b, c, d)
}

func TestConfirmUsesStableEventID(t *testing.T) {
	ms := newMem()
	o := seedOrder(ms, orchestrate.StatusPaymentSucceeded)
	res := "res_1"
	o.ReservationID = &res
	ms.orders[o.ID] = o
	ds := &mockDS{resByOp: map[string]struct{ id, status string }{"invop_1": {"res_1", "ACTIVE"}}}
	d := &orchestrate.Driver{Store: ms, Downstream: ds}
	_, _ = d.Advance(context.Background(), "ord_1", "c")
	want := "evt_order_confirmed_ord_1"
	if _, ok := ms.outbox[want]; !ok {
		t.Fatalf("outbox keys %v", ms.outbox)
	}
}

func TestErrors(t *testing.T) {
	if !errors.Is(orchestrate.ErrAmbiguous, orchestrate.ErrAmbiguous) {
		t.Fatal()
	}
}
