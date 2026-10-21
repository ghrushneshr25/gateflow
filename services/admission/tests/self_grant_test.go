//go:build integration

package tests

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/store"
)

// TestSelfGrantPathUsesAuthoritativeGrantFIFO verifies durable invariants for the
// same GrantFIFO path used by buyer self-grant and service grant.
func TestSelfGrantPathRequiresQueueAndIsIdempotent(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session := ids.New("ses")
	buyer := ids.New("buyer")
	_ = st.SetBudgetCaps(ctx, "global", 100000, 10000)
	_ = st.SetBudgetCaps(ctx, "session:"+session, 100, 100)

	// No queue → not found
	_, _, err := st.GrantFIFO(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound got %v", err)
	}

	_, err = st.Join(ctx, store.QueueEntry{
		ID: ids.New("q"), SessionID: session, BuyerID: buyer, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	ent1 := ids.New("ent")
	e1, g1, err := st.GrantFIFO(ctx, session, buyer, ent1, time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if e1.Status != "GRANTED" || g1.ID != ent1 || g1.BuyerID != buyer {
		t.Fatalf("%+v %+v", e1, g1)
	}

	// Duplicate grant returns existing entitlement, no second budget consume
	bBefore, _ := st.GetBudget(ctx, "session:"+session)
	e2, g2, err := st.GrantFIFO(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if g2.ID != ent1 {
		t.Fatalf("want same entitlement %s got %s", ent1, g2.ID)
	}
	if e2.EntitlementID == nil || *e2.EntitlementID != ent1 {
		t.Fatal("queue entitlement drift")
	}
	bAfter, _ := st.GetBudget(ctx, "session:"+session)
	if bAfter.GrantsIssued != bBefore.GrantsIssued {
		t.Fatalf("duplicate grant consumed budget %d -> %d", bBefore.GrantsIssued, bAfter.GrantsIssued)
	}
}

func TestSelfGrantRespectsFence(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session := ids.New("ses")
	buyer := ids.New("buyer")
	_ = st.SetBudgetCaps(ctx, "global", 100000, 10000)
	_ = st.SetBudgetCaps(ctx, "session:"+session, 100, 100)
	_, _ = st.Join(ctx, store.QueueEntry{
		ID: ids.New("q"), SessionID: session, BuyerID: buyer, CreatedAt: time.Now().UTC(),
	})

	if _, err := st.FenceAdmission(ctx, "test-fence", "ops", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
	})

	_, _, err := st.GrantFIFO(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != store.ErrCoordinationFenced {
		t.Fatalf("want fenced got %v", err)
	}
	// No entitlement created
	e, err := st.GetMine(ctx, session, buyer)
	if err != nil {
		t.Fatal(err)
	}
	if e.EntitlementID != nil && *e.EntitlementID != "" {
		t.Fatal("fenced grant must not create entitlement")
	}
}

func TestSelfGrantBudgetExhaustionDurable(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session := ids.New("ses")
	_ = st.SetBudgetCaps(ctx, "global", 100000, 10000)
	_ = st.SetBudgetCaps(ctx, "session:"+session, 2, 100)

	var ok, fail atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			buyer := ids.New("buyer")
			_, _ = st.Join(ctx, store.QueueEntry{
				ID: ids.New("q"), SessionID: session, BuyerID: buyer, CreatedAt: time.Now().UTC(),
			})
			_, _, err := st.GrantFIFO(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
			if err == nil {
				ok.Add(1)
			} else if err == store.ErrBudgetExhausted {
				fail.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if ok.Load() != 2 {
		t.Fatalf("ok=%d want 2 fail=%d", ok.Load(), fail.Load())
	}
	b, err := st.GetBudget(ctx, "session:"+session)
	if err != nil {
		t.Fatal(err)
	}
	if b.GrantsIssued != 2 {
		t.Fatalf("issued=%d", b.GrantsIssued)
	}
}

func TestCrossBuyerGrantDoesNotShareEntitlement(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session := ids.New("ses")
	a, b := ids.New("buyer"), ids.New("buyer")
	_ = st.SetBudgetCaps(ctx, "global", 100000, 10000)
	_ = st.SetBudgetCaps(ctx, "session:"+session, 100, 100)
	_, _ = st.Join(ctx, store.QueueEntry{ID: ids.New("q"), SessionID: session, BuyerID: a, CreatedAt: time.Now().UTC()})
	_, _ = st.Join(ctx, store.QueueEntry{ID: ids.New("q"), SessionID: session, BuyerID: b, CreatedAt: time.Now().UTC()})

	_, ga, err := st.GrantFIFO(ctx, session, a, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	_, gb, err := st.GrantFIFO(ctx, session, b, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if ga.ID == gb.ID || ga.BuyerID == gb.BuyerID {
		t.Fatalf("entitlements must be distinct %+v %+v", ga, gb)
	}
}
