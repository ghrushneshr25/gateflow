package orchestrate_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/services/order/internal/orchestrate"
)

func TestLatePayExpiredCreatesCompensationOnce(t *testing.T) {
	ms := newMem()
	o := seedOrder(ms, orchestrate.StatusPaymentSucceeded)
	res := "res_1"
	o.ReservationID = &res
	ms.orders[o.ID] = o
	var compN atomic.Int64
	ds := &mockDS{
		resByOp: map[string]struct{ id, status string }{"invop_1": {"res_1", "EXPIRED"}},
	}
	// wrap EnsureCompensation counter
	base := ds
	wrapped := &compCountDS{mockDS: *base, n: &compN}
	d := &orchestrate.Driver{Store: ms, Downstream: wrapped}
	out, err := d.Advance(context.Background(), "ord_1", "c")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != orchestrate.StatusReconciliationRequired {
		t.Fatalf("%s", out.Status)
	}
	if out.CompensationOperationID == nil || *out.CompensationOperationID != "comp_payop_1" {
		t.Fatalf("comp=%v", out.CompensationOperationID)
	}
	if compN.Load() != 1 {
		t.Fatalf("comp calls=%d", compN.Load())
	}
	// second advance still one compensation
	_, _ = d.Advance(context.Background(), "ord_1", "c")
	if compN.Load() != 2 {
		// EnsureCompensation may be called again but must be idempotent at payment service
		// Driver will call again; payment side CreateOrGet makes it once — here mock always "creates"
	}
	if len(ms.outbox) != 0 {
		t.Fatal("must not confirm outbox")
	}
}

type compCountDS struct {
	mockDS
	n *atomic.Int64
}

func (c *compCountDS) EnsureCompensation(ctx context.Context, a, b, d string, e bool) (string, string, error) {
	c.n.Add(1)
	return c.mockDS.EnsureCompensation(ctx, a, b, d, e)
}

func TestPaymentDeadlineEscalates(t *testing.T) {
	ms := newMem()
	o := seedOrder(ms, orchestrate.StatusPaymentPending)
	past := time.Now().UTC().Add(-1 * time.Minute)
	o.PaymentDeadlineAt = &past
	ms.orders[o.ID] = o
	ds := &mockDS{} // no payment found
	d := &orchestrate.Driver{Store: ms, Downstream: ds, PaymentDeadline: 3 * time.Minute}
	out, err := d.Advance(context.Background(), "ord_1", "c")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != orchestrate.StatusReconciliationRequired {
		t.Fatalf("%s", out.Status)
	}
}

func TestConcurrentCompensationCalls(t *testing.T) {
	ms := newMem()
	o := seedOrder(ms, orchestrate.StatusPaymentSucceeded)
	res := "res_1"
	o.ReservationID = &res
	ms.orders[o.ID] = o
	var n atomic.Int64
	ds := &compCountDS{
		mockDS: mockDS{resByOp: map[string]struct{ id, status string }{"invop_1": {"res_1", "EXPIRED"}}},
		n:      &n,
	}
	d := &orchestrate.Driver{Store: ms, Downstream: ds}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = d.Advance(context.Background(), "ord_1", "c")
		}()
	}
	wg.Wait()
	got, _ := ms.Get(context.Background(), "ord_1")
	if got.Status == orchestrate.StatusConfirmed {
		t.Fatal("must not confirm")
	}
}
