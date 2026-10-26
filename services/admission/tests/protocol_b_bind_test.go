//go:build integration

package tests

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/store"
)

// P6.6: Protocol B grant concurrent with bind/release on shared budgets.

func TestP66_B_GrantVsBindRelease_NoBudgetOvershoot(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	_, _ = st.UnfenceAdmission(ctx, "p66", time.Now().UTC())
	session := ids.New("ses")
	_ = st.EnsureBudgets(ctx, session, 500, 50) // tight active checkout cap

	var grantOK, bindOK, bindFail, dead atomic.Int64
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// binders: grant under A or B then bind/release
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				buyer := ids.New("buyer")
				_, _ = st.Join(ctx, store.QueueEntry{ID: ids.New("q"), SessionID: session, BuyerID: buyer, CreatedAt: time.Now().UTC()})
				ctx2, cancel := context.WithTimeout(ctx, 2*time.Second)
				_, ent, err := st.GrantFIFOProtocolB(ctx2, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
				cancel()
				if err != nil {
					if ctx2.Err() != nil {
						dead.Add(1)
					}
					continue
				}
				grantOK.Add(1)
				ctx3, cancel3 := context.WithTimeout(ctx, 2*time.Second)
				_, err = st.Bind(ctx3, ent.ID, buyer, ids.New("chk"), time.Now().UTC())
				cancel3()
				if err == nil {
					bindOK.Add(1)
					_, _ = st.Consume(ctx, ent.ID, ids.New("chk"), time.Now().UTC())
				} else if errors.Is(err, store.ErrBudgetExhausted) {
					bindFail.Add(1)
				}
			}
		}()
	}

	// fence flapper
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 15; i++ {
			_, _ = st.FenceAdmission(ctx, "p66", "ops", time.Now().UTC())
			time.Sleep(3 * time.Millisecond)
			_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
			time.Sleep(3 * time.Millisecond)
		}
		close(stop)
	}()
	wg.Wait()

	b, err := st.GetBudget(ctx, "session:"+session)
	if err != nil {
		t.Fatal(err)
	}
	if b.GrantsIssued > b.MaxGrants || b.ActiveCheckouts > b.MaxActiveCheckouts {
		t.Fatalf("budget overshoot grants=%d/%d active=%d/%d", b.GrantsIssued, b.MaxGrants, b.ActiveCheckouts, b.MaxActiveCheckouts)
	}
	t.Logf("grantOK=%d bindOK=%d bindFail=%d timeoutish=%d issued=%d active=%d",
		grantOK.Load(), bindOK.Load(), bindFail.Load(), dead.Load(), b.GrantsIssued, b.ActiveCheckouts)
	_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
}
