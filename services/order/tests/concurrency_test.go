//go:build integration

package tests

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/libs/go/postgres"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

func testStore(t *testing.T) *store.Store {
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

func TestConcurrentPurchaseLimit(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	buyer := ids.New("buyer")
	session := ids.New("ses")
	const limit = 4
	const workers = 20

	var ok atomic.Int64
	var limited atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			now := time.Now().UTC()
			o := store.Order{
				ID: ids.New("ord"), BuyerID: buyer, SessionID: session,
				TicketTypeID: "tt", PoolID: "pool", Quantity: 1, Status: "CREATED",
				EntitlementID: ids.New("ent"), CheckoutOperationID: ids.New("chk"),
				InventoryOperationID: ids.New("invop"), PaymentOperationID: ids.New("payop"),
				MaxPerBuyer: limit, IdempotencyKey: ids.New("key"), RequestHash: ids.New("hash"),
			}
			_, err := st.CreateWithHold(ctx, o, ids.New("hold"), now)
			if err == nil {
				ok.Add(1)
				return
			}
			if err == store.ErrLimit {
				limited.Add(1)
				return
			}
			t.Errorf("unexpected err: %v", err)
		}(i)
	}
	wg.Wait()
	if ok.Load() != int64(limit) {
		t.Fatalf("expected %d successes, got %d (limited=%d)", limit, ok.Load(), limited.Load())
	}
	used, err := st.HeldAndCommittedQty(ctx, buyer, session)
	if err != nil {
		t.Fatal(err)
	}
	if used != limit {
		t.Fatalf("held+committed=%d want %d", used, limit)
	}
}

func TestIdempotentCreateSameKey(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	buyer := ids.New("buyer")
	session := ids.New("ses")
	key := ids.New("key")
	hash := "abc123"
	base := store.Order{
		ID: ids.New("ord"), BuyerID: buyer, SessionID: session,
		TicketTypeID: "tt", PoolID: "pool", Quantity: 2, Status: "CREATED",
		EntitlementID: "ent", CheckoutOperationID: ids.New("chk"),
		InventoryOperationID: ids.New("invop"), PaymentOperationID: ids.New("payop"),
		MaxPerBuyer: 4, IdempotencyKey: key, RequestHash: hash,
	}
	first, err := st.CreateWithHold(ctx, base, ids.New("hold"), now)
	if err != nil {
		t.Fatal(err)
	}
	second := base
	second.ID = ids.New("ord")
	second.CheckoutOperationID = ids.New("chk")
	got, err := st.CreateWithHold(ctx, second, ids.New("hold"), now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != first.ID {
		t.Fatalf("expected same order id %s got %s", first.ID, got.ID)
	}
	used, err := st.HeldAndCommittedQty(ctx, buyer, session)
	if err != nil {
		t.Fatal(err)
	}
	if used != 2 {
		t.Fatalf("allowance should count once, got %d", used)
	}
}

func TestIdempotencyConflictDifferentHash(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	key := ids.New("key")
	base := store.Order{
		ID: ids.New("ord"), BuyerID: ids.New("buyer"), SessionID: ids.New("ses"),
		TicketTypeID: "tt", PoolID: "pool", Quantity: 1, Status: "CREATED",
		EntitlementID: "ent", CheckoutOperationID: ids.New("chk"),
		InventoryOperationID: ids.New("invop"), PaymentOperationID: ids.New("payop"),
		MaxPerBuyer: 4, IdempotencyKey: key, RequestHash: "hash-a",
	}
	if _, err := st.CreateWithHold(ctx, base, ids.New("hold"), now); err != nil {
		t.Fatal(err)
	}
	conflict := base
	conflict.ID = ids.New("ord")
	conflict.RequestHash = "hash-b"
	conflict.Quantity = 2
	_, err := st.CreateWithHold(ctx, conflict, ids.New("hold"), now)
	if err != store.ErrIdempotency {
		t.Fatalf("want ErrIdempotency got %v", err)
	}
}
