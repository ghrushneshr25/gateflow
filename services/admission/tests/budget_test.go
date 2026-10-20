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
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/store"
)

func admStore(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("ADMISSION_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := postgres.NewPoolWithSearchPath(context.Background(), url, "admission")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

func TestGrantBudgetExhaustion(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session := ids.New("ses")
	if err := st.SetBudgetCaps(ctx, "session:"+session, 3, 100); err != nil {
		t.Fatal(err)
	}
	// Raise global high so session is the limiter.
	if err := st.SetBudgetCaps(ctx, "global", 100000, 10000); err != nil {
		t.Fatal(err)
	}

	var ok, exhausted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			buyer := ids.New("buyer")
			_, err := st.Join(ctx, store.QueueEntry{
				ID: ids.New("q"), SessionID: session, BuyerID: buyer, CreatedAt: time.Now().UTC(),
			})
			if err != nil {
				t.Errorf("join: %v", err)
				return
			}
			_, _, err = st.GrantFIFO(ctx, session, buyer, ids.New("ent"), 10*time.Minute, time.Now().UTC())
			if err == nil {
				ok.Add(1)
				return
			}
			if err == store.ErrBudgetExhausted {
				exhausted.Add(1)
				return
			}
			t.Errorf("unexpected: %v", err)
		}(i)
	}
	wg.Wait()
	if ok.Load() != 3 {
		t.Fatalf("grants ok=%d want 3 exhausted=%d", ok.Load(), exhausted.Load())
	}
	b, err := st.GetBudget(ctx, "session:"+session)
	if err != nil {
		t.Fatal(err)
	}
	if b.GrantsIssued != 3 {
		t.Fatalf("grants_issued=%d", b.GrantsIssued)
	}
}

func TestActiveCheckoutBudget(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session := ids.New("ses")
	if err := st.SetBudgetCaps(ctx, "session:"+session, 100, 2); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBudgetCaps(ctx, "global", 100000, 10000); err != nil {
		t.Fatal(err)
	}

	makeBound := func(buyer string) (string, error) {
		_, err := st.Join(ctx, store.QueueEntry{
			ID: ids.New("q"), SessionID: session, BuyerID: buyer, CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			return "", err
		}
		_, ent, err := st.GrantFIFO(ctx, session, buyer, ids.New("ent"), 10*time.Minute, time.Now().UTC())
		if err != nil {
			return "", err
		}
		_, err = st.Bind(ctx, ent.ID, buyer, ids.New("chk"), time.Now().UTC())
		return ent.ID, err
	}

	if _, err := makeBound("b1"); err != nil {
		t.Fatal(err)
	}
	if _, err := makeBound("b2"); err != nil {
		t.Fatal(err)
	}
	_, err := makeBound("b3")
	if err != store.ErrBudgetExhausted {
		t.Fatalf("want budget exhausted got %v", err)
	}
}

func TestIdempotentJoinAndGrant(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session := ids.New("ses")
	buyer := ids.New("buyer")
	_ = st.SetBudgetCaps(ctx, "session:"+session, 100, 100)
	_ = st.SetBudgetCaps(ctx, "global", 100000, 10000)

	e1, err := st.Join(ctx, store.QueueEntry{ID: ids.New("q"), SessionID: session, BuyerID: buyer, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	e2, err := st.Join(ctx, store.QueueEntry{ID: ids.New("q"), SessionID: session, BuyerID: buyer, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if e1.ID != e2.ID || e1.SequenceNo != e2.SequenceNo {
		t.Fatalf("join not idempotent: %+v %+v", e1, e2)
	}
	_, ent1, err := st.GrantFIFO(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	_, ent2, err := st.GrantFIFO(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if ent1.ID != ent2.ID {
		t.Fatal("grant not idempotent")
	}
	b, _ := st.GetBudget(ctx, "session:"+session)
	if b.GrantsIssued != 1 {
		t.Fatalf("grant should count once, got %d", b.GrantsIssued)
	}
}

func TestProcessedEventDedupe(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	eventID := ids.New("evt")
	first, err := st.MarkEventProcessed(ctx, eventID)
	if err != nil || !first {
		t.Fatalf("first: %v %v", first, err)
	}
	second, err := st.MarkEventProcessed(ctx, eventID)
	if err != nil || second {
		t.Fatalf("second should be false: %v %v", second, err)
	}
}
