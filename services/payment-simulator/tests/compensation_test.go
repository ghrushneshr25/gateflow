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
	"github.com/ghrushneshrathod/gateflow/services/payment-simulator/internal/store"
)

func payStore(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("PAYMENT_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := postgres.NewPoolWithSearchPath(context.Background(), url, "payment")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

func TestCompensationCreatedOnce(t *testing.T) {
	st := payStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	payOp := ids.New("payop")
	p, err := st.CreateOrGet(ctx, store.Payment{
		ID: ids.New("pay"), OperationID: payOp, OrderID: "ord_1",
		AmountCents: 100, Currency: "USD", Scenario: "success", Status: "SUCCEEDED",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	compOp := "comp_" + payOp
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = st.CreateOrGetCompensation(ctx, store.Compensation{
				ID: ids.New("cmp"), OperationID: compOp, PaymentID: p.ID, OrderID: "ord_1",
				Status: "CREATED", CreatedAt: now, UpdatedAt: now,
			})
		}()
	}
	wg.Wait()
	c, err := st.GetCompensationByOperation(ctx, compOp)
	if err != nil {
		t.Fatal(err)
	}
	if c.OperationID != compOp {
		t.Fatal(c)
	}
}

func TestCompensationFailThenRetry(t *testing.T) {
	st := payStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	payOp := ids.New("payop")
	p, err := st.CreateOrGet(ctx, store.Payment{
		ID: ids.New("pay"), OperationID: payOp, OrderID: "ord_x",
		AmountCents: 50, Currency: "USD", Scenario: "compensation_fail", Status: "SUCCEEDED",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	compOp := "comp_" + payOp
	_, err = st.CreateOrGetCompensation(ctx, store.Compensation{
		ID: ids.New("cmp"), OperationID: compOp, PaymentID: p.ID, OrderID: "ord_x",
		Status: "CREATED", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	c1, err := st.ExecuteCompensationAttempt(ctx, compOp, true, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if c1.Status != "FAILED" || !c1.Escalated {
		t.Fatalf("%+v", c1)
	}
	c2, err := st.ExecuteCompensationAttempt(ctx, compOp, true, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if c2.Status != "SUCCEEDED" {
		t.Fatalf("%+v", c2)
	}
}

func TestDuplicateCallbackIdempotent(t *testing.T) {
	st := payStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	payOp := ids.New("payop")
	_, err := st.CreateOrGet(ctx, store.Payment{
		ID: ids.New("pay"), OperationID: payOp, OrderID: "ord_cb",
		AmountCents: 10, Currency: "USD", Scenario: "unknown", Status: "UNKNOWN",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	p1, err := st.ApplyCallback(ctx, payOp, "SUCCEEDED", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	p2, err := st.ApplyCallback(ctx, payOp, "SUCCEEDED", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if p1.Status != "SUCCEEDED" || p2.Status != "SUCCEEDED" {
		t.Fatal(p1, p2)
	}
	if p2.CallbackCount < 2 {
		t.Fatalf("callbacks=%d", p2.CallbackCount)
	}
}

func TestDelaySuccessResolveDue(t *testing.T) {
	st := payStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	past := now.Add(-time.Second)
	payOp := ids.New("payop")
	p, err := st.CreateOrGet(ctx, store.Payment{
		ID: ids.New("pay"), OperationID: payOp, OrderID: "ord_d",
		AmountCents: 10, Currency: "USD", Scenario: "delay_success", Status: "PENDING",
		ResolveAfter: &past, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	n, err := st.ResolveDue(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("resolved=%d", n)
	}
	got, err := st.GetByOperation(ctx, payOp)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "SUCCEEDED" {
		t.Fatalf("%s id=%s", got.Status, p.ID)
	}
}
