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
	"github.com/ghrushneshrathod/gateflow/services/inventory/internal/store"
)

func TestConcurrentReserveLastUnit(t *testing.T) {
	url := os.Getenv("INVENTORY_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := postgres.NewPoolWithSearchPath(ctx, url, "inventory")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	st := store.New(pool)

	p, err := st.UpsertPool(ctx, store.Pool{
		ID: ids.New("pool"), SessionID: "ses_test", TicketTypeID: ids.New("tt"),
		Name: "race", SellableCapacity: 1, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	var okCount atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			now := time.Now().UTC()
			_, err := st.Reserve(ctx, store.Reservation{
				ID: ids.New("res"), OperationID: ids.New("op"), PoolID: p.ID,
				Quantity: 1, BuyerID: "b", OrderID: ids.New("ord"),
				ExpiresAt: now.Add(5 * time.Minute), CreatedAt: now, UpdatedAt: now,
			})
			if err == nil {
				okCount.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if okCount.Load() != 1 {
		t.Fatalf("expected exactly 1 successful reserve, got %d", okCount.Load())
	}
	got, err := st.GetPool(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveReservedQty != 1 || got.ConfirmedQty != 0 {
		t.Fatalf("pool state %+v", got)
	}
	if got.ConfirmedQty+got.ActiveReservedQty > got.SellableCapacity {
		t.Fatal("invariant violated")
	}
}
