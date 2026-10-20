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

func invStore(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("INVENTORY_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := postgres.NewPoolWithSearchPath(context.Background(), url, "inventory")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

func TestConfirmVsExpireRace(t *testing.T) {
	st := invStore(t)
	ctx := context.Background()
	p, err := st.UpsertPool(ctx, store.Pool{
		ID: ids.New("pool"), SessionID: "ses", TicketTypeID: ids.New("tt"),
		Name: "race", SellableCapacity: 1, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	res, err := st.Reserve(ctx, store.Reservation{
		ID: ids.New("res"), OperationID: ids.New("op"), PoolID: p.ID,
		Quantity: 1, BuyerID: "b", OrderID: ids.New("ord"),
		ExpiresAt: now.Add(-time.Second), // already past deadline
		CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now.Add(-2 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	var confirmed, expired atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := st.Confirm(ctx, res.ID, time.Now().UTC())
			if err == nil {
				confirmed.Add(1)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := st.Expire(ctx, res.ID, time.Now().UTC())
			if err == nil && r.Status == "EXPIRED" {
				expired.Add(1)
			}
		}()
	}
	wg.Wait()

	final, err := st.GetReservation(ctx, res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != "EXPIRED" && final.Status != "CONFIRMED" {
		t.Fatalf("unexpected status %s", final.Status)
	}
	// Past deadline: Confirm path should expire, not confirm.
	if final.Status == "CONFIRMED" {
		t.Fatal("must not confirm past-deadline reservation")
	}
	if final.Status != "EXPIRED" {
		t.Fatalf("want EXPIRED got %s", final.Status)
	}
	pool, err := st.GetPool(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pool.ActiveReservedQty != 0 || pool.ConfirmedQty != 0 {
		t.Fatalf("pool counters wrong: %+v", pool)
	}
	if pool.ConfirmedQty+pool.ActiveReservedQty > pool.SellableCapacity {
		t.Fatal("invariant broken")
	}
	_ = confirmed
	_ = expired
}

func TestExpireCannotRevive(t *testing.T) {
	st := invStore(t)
	ctx := context.Background()
	p, err := st.UpsertPool(ctx, store.Pool{
		ID: ids.New("pool"), SessionID: "ses", TicketTypeID: ids.New("tt"),
		Name: "revive", SellableCapacity: 5, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	res, err := st.Reserve(ctx, store.Reservation{
		ID: ids.New("res"), OperationID: ids.New("op"), PoolID: p.ID,
		Quantity: 1, BuyerID: "b", OrderID: ids.New("ord"),
		ExpiresAt: now.Add(-time.Minute), CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Expire(ctx, res.ID, time.Now().UTC())
	if err != nil || got.Status != "EXPIRED" {
		t.Fatalf("expire: %v %+v", err, got)
	}
	_, err = st.Confirm(ctx, res.ID, time.Now().UTC())
	if err != store.ErrNotActive && err != store.ErrExpired {
		t.Fatalf("confirm after expire want not-active/expired got %v", err)
	}
	// second expire idempotent
	got2, err := st.Expire(ctx, res.ID, time.Now().UTC())
	if err != nil || got2.Status != "EXPIRED" {
		t.Fatalf("re-expire: %v %+v", err, got2)
	}
}

func TestIdempotentReserveSameOperation(t *testing.T) {
	st := invStore(t)
	ctx := context.Background()
	p, err := st.UpsertPool(ctx, store.Pool{
		ID: ids.New("pool"), SessionID: "ses", TicketTypeID: ids.New("tt"),
		Name: "idem", SellableCapacity: 10, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	op := ids.New("op")
	now := time.Now().UTC()
	base := store.Reservation{
		ID: ids.New("res"), OperationID: op, PoolID: p.ID,
		Quantity: 2, BuyerID: "b", OrderID: "ord1",
		ExpiresAt: now.Add(5 * time.Minute), CreatedAt: now, UpdatedAt: now,
	}
	first, err := st.Reserve(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var ok atomic.Int64
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := base
			r.ID = ids.New("res")
			got, err := st.Reserve(ctx, r)
			if err == nil && got.ID == first.ID {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 15 {
		t.Fatalf("expected all 15 idempotent hits, got %d", ok.Load())
	}
	pool, err := st.GetPool(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pool.ActiveReservedQty != 2 {
		t.Fatalf("reserved qty=%d want 2", pool.ActiveReservedQty)
	}
}

func TestReleaseIdempotent(t *testing.T) {
	st := invStore(t)
	ctx := context.Background()
	p, err := st.UpsertPool(ctx, store.Pool{
		ID: ids.New("pool"), SessionID: "ses", TicketTypeID: ids.New("tt"),
		Name: "rel", SellableCapacity: 3, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	res, err := st.Reserve(ctx, store.Reservation{
		ID: ids.New("res"), OperationID: ids.New("op"), PoolID: p.ID,
		Quantity: 1, BuyerID: "b", OrderID: ids.New("ord"),
		ExpiresAt: now.Add(time.Minute), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Release(ctx, res.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Release(ctx, res.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	pool, err := st.GetPool(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pool.ActiveReservedQty != 0 {
		t.Fatalf("double release corrupted counters: %d", pool.ActiveReservedQty)
	}
}
