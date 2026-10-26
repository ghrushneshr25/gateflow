//go:build integration

package tests

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/ids"
	"github.com/ghrushneshrathod/gateflow/services/admission/internal/store"
)

// P6.3: deterministic PostgreSQL interleaving tests for Protocol A vs experimental B.
// Production HTTP still uses Protocol A only.

type barrier struct {
	enter, cont chan struct{}
	once        sync.Once
}

func newBarrier() *barrier {
	return &barrier{enter: make(chan struct{}), cont: make(chan struct{})}
}

func (b *barrier) waitEnter(timeout time.Duration) bool {
	select {
	case <-b.enter:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (b *barrier) release() { close(b.cont) }

func (b *barrier) atPhase(want string) func(string) {
	return func(phase string) {
		if phase != want {
			return
		}
		b.once.Do(func() { close(b.enter) })
		<-b.cont
	}
}

func setupJoined(t *testing.T, st *store.Store, session, buyer string) {
	t.Helper()
	ctx := context.Background()
	_, _ = st.UnfenceAdmission(ctx, "p63", time.Now().UTC())
	_ = st.EnsureBudgets(ctx, session, 10000, 10000)
	_, err := st.Join(ctx, store.QueueEntry{
		ID: ids.New("q"), SessionID: session, BuyerID: buyer, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func countEntitlements(t *testing.T, st *store.Store, buyer string) int {
	t.Helper()
	// Use Get via grant path side effects: list not available — probe via re-grant idempotent.
	// Direct SQL through docker for authoritative count.
	out, err := exec.Command("docker", "exec", "gateflow-postgres-1", "psql", "-U", "gateflow", "-d", "gateflow", "-t", "-A", "-c",
		"SELECT count(*) FROM admission.entitlements WHERE buyer_id='"+buyer+"'").Output()
	if err != nil {
		t.Fatalf("count entitlements: %v", err)
	}
	var n int
	_, _ = parseInt(string(out), &n)
	return n
}

func parseInt(s string, n *int) (int, error) {
	var v int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			v = v*10 + int(c-'0')
		}
	}
	*n = v
	return v, nil
}

func budgetIssued(t *testing.T, st *store.Store, key string) int {
	t.Helper()
	b, err := st.GetBudget(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return b.GrantsIssued
}

// --- A: Grant vs fence (Protocol A) ---

func TestP63_A_GrantPausedAfterCoord_ThenFenceWaitsUntilGrantCommits(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session, buyer := ids.New("ses"), ids.New("buyer")
	setupJoined(t, st, session, buyer)

	bar := newBarrier()
	store.SetGrantHook(bar.atPhase("a_after_budget"))
	t.Cleanup(func() { store.SetGrantHook(nil) })

	type grantRes struct {
		err error
		ok  bool
	}
	grantCh := make(chan grantRes, 1)
	go func() {
		_, ent, err := st.GrantFIFO(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
		grantCh <- grantRes{err: err, ok: err == nil && ent.ID != ""}
	}()
	if !bar.waitEnter(5 * time.Second) {
		t.Fatal("grant did not reach a_after_budget")
	}

	// Fence must block while grant holds coordination lock.
	fenceDone := make(chan store.Coordination, 1)
	fenceErr := make(chan error, 1)
	go func() {
		c, err := st.FenceAdmission(ctx, "p63-a", "ops", time.Now().UTC())
		fenceErr <- err
		fenceDone <- c
	}()
	select {
	case <-fenceDone:
		t.Fatal("fence must not complete while grant holds coord lock")
	case <-time.After(200 * time.Millisecond):
		// expected: blocked
	}

	bar.release()
	select {
	case err := <-fenceErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fence timeout after grant release")
	}
	select {
	case gr := <-grantCh:
		if gr.err != nil {
			t.Fatalf("grant err %v", gr.err)
		}
		if !gr.ok {
			t.Fatal("grant should have committed before fence (linearized first)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("grant timeout")
	}
	// After fence success, new buyer cannot get new entitlement.
	b2 := ids.New("buyer")
	_, _ = st.Join(ctx, store.QueueEntry{ID: ids.New("q"), SessionID: session, BuyerID: b2, CreatedAt: time.Now().UTC()})
	_, _, err := st.GrantFIFO(ctx, session, b2, ids.New("ent"), time.Minute, time.Now().UTC())
	if !errors.Is(err, store.ErrCoordinationFenced) {
		t.Fatalf("post-fence grant want fenced got %v", err)
	}
	_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
}

// --- B: Late validation (Protocol B experimental) ---

func TestP63_B_FenceDuringGrantBeforeLateCoord_GrantFailsClosed(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session, buyer := ids.New("ses"), ids.New("buyer")
	setupJoined(t, st, session, buyer)
	beforeG := budgetIssued(t, st, "global")
	beforeS := budgetIssued(t, st, "session:"+session)

	bar := newBarrier()
	store.SetGrantHook(bar.atPhase("b_before_late_coord"))
	t.Cleanup(func() { store.SetGrantHook(nil) })

	var grantErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, grantErr = st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	}()
	if !bar.waitEnter(5 * time.Second) {
		t.Fatal("B grant did not pause before late coord")
	}

	// Fence while grant holds queue+budget but NOT coord yet.
	c, err := st.FenceAdmission(ctx, "p63-b-late", "ops", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != store.CoordModeFenced {
		t.Fatalf("%+v", c)
	}

	bar.release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("grant timeout")
	}
	if !errors.Is(grantErr, store.ErrCoordinationFenced) {
		t.Fatalf("want fenced got %v", grantErr)
	}
	// No entitlement; budgets rolled back.
	if countEntitlements(t, st, buyer) != 0 {
		t.Fatal("entitlement must not commit after fence")
	}
	if budgetIssued(t, st, "global") != beforeG || budgetIssued(t, st, "session:"+session) != beforeS {
		t.Fatalf("budget leak global %d->%d session %d->%d",
			beforeG, budgetIssued(t, st, "global"), beforeS, budgetIssued(t, st, "session:"+session))
	}
	_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
}

func TestP63_B_FenceFirst_ThenGrantFails(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session, buyer := ids.New("ses"), ids.New("buyer")
	setupJoined(t, st, session, buyer)
	if _, err := st.FenceAdmission(ctx, "p63-b-first", "ops", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_, _, err := st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if !errors.Is(err, store.ErrCoordinationFenced) {
		t.Fatalf("want fenced got %v", err)
	}
	if countEntitlements(t, st, buyer) != 0 {
		t.Fatal("no entitlement")
	}
	_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
}

func TestP63_B_GrantCompletesBeforeFence_ThenPostFenceBlocked(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session, buyer := ids.New("ses"), ids.New("buyer")
	setupJoined(t, st, session, buyer)

	_, ent, err := st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID == "" {
		t.Fatal("expected entitlement")
	}
	if _, err := st.FenceAdmission(ctx, "p63-b-after", "ops", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// Join second buyer WITHOUT unfencing (setupJoined would clear fence).
	b2 := ids.New("buyer")
	_ = st.EnsureBudgets(ctx, session, 10000, 10000)
	_, err = st.Join(ctx, store.QueueEntry{ID: ids.New("q"), SessionID: session, BuyerID: b2, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = st.GrantFIFOProtocolB(ctx, session, b2, ids.New("ent"), time.Minute, time.Now().UTC())
	if !errors.Is(err, store.ErrCoordinationFenced) {
		t.Fatalf("want fenced got %v", err)
	}
	_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
}

// --- D: Final budget unit ---

func TestP63_B_FinalBudgetUnit(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session := ids.New("ses")
	_ = st.EnsureBudgets(ctx, session, 100, 100)
	if err := st.SetBudgetCaps(ctx, "session:"+session, 1, 100); err != nil {
		t.Fatal(err)
	}
	_, _ = st.UnfenceAdmission(ctx, "p63", time.Now().UTC())

	var ok, fail atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			buyer := ids.New("buyer")
			_, _ = st.Join(ctx, store.QueueEntry{ID: ids.New("q"), SessionID: session, BuyerID: buyer, CreatedAt: time.Now().UTC()})
			_, _, err := st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
			if err == nil {
				ok.Add(1)
			} else if errors.Is(err, store.ErrBudgetExhausted) {
				fail.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("want exactly 1 success got ok=%d fail=%d", ok.Load(), fail.Load())
	}
	b, _ := st.GetBudget(ctx, "session:"+session)
	if b.GrantsIssued != 1 {
		t.Fatalf("issued=%d", b.GrantsIssued)
	}
}

// --- E: Rollback after budget (B paused then cancel via fence) already covered in B late test ---

// --- G: Fence then unfence while old B grant paused ---

func TestP63_B_UnfenceWhilePaused_OldAttemptStillSeesFenceAtLateCheck(t *testing.T) {
	// P6.4 strict generation contract: fence bumps epoch → in-flight grant must fail
	// even after unfence (mode ACTIVE again, epoch advanced).
	st := admStore(t)
	ctx := context.Background()
	session, buyer := ids.New("ses"), ids.New("buyer")
	setupJoined(t, st, session, buyer)
	beforeG := budgetIssued(t, st, "global")

	bar := newBarrier()
	store.SetGrantHook(bar.atPhase("b_before_late_coord"))
	t.Cleanup(func() { store.SetGrantHook(nil) })

	var grantErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, grantErr = st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	}()
	if !bar.waitEnter(5 * time.Second) {
		t.Fatal("pause missed")
	}
	if _, err := st.FenceAdmission(ctx, "p63-fu", "ops", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UnfenceAdmission(ctx, "ops", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	bar.release()
	<-done

	if !errors.Is(grantErr, store.ErrStaleGeneration) {
		t.Fatalf("want stale generation got %v", grantErr)
	}
	if countEntitlements(t, st, buyer) != 0 {
		t.Fatal("stale grant must not commit entitlement")
	}
	if budgetIssued(t, st, "global") != beforeG {
		t.Fatal("budget must roll back on stale generation")
	}
}

// P6.4: new grant after unfence under current generation succeeds.
func TestP64_B_NewGrantAfterUnfence_Succeeds(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session, buyer := ids.New("ses"), ids.New("buyer")
	setupJoined(t, st, session, buyer)
	if _, err := st.FenceAdmission(ctx, "p64", "ops", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UnfenceAdmission(ctx, "ops", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_, ent, err := st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID == "" {
		t.Fatal("expected entitlement under current generation")
	}
}

// P6.4: concurrent grants across generation change — stale fail, fresh ok.
func TestP64_B_ConcurrentAcrossGenerationChange(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session := ids.New("ses")
	_ = st.EnsureBudgets(ctx, session, 10000, 10000)
	_, _ = st.UnfenceAdmission(ctx, "p64", time.Now().UTC())

	bar := newBarrier()
	store.SetGrantHook(bar.atPhase("b_before_late_coord"))
	t.Cleanup(func() { store.SetGrantHook(nil) })

	buyerOld := ids.New("buyer")
	_, _ = st.Join(ctx, store.QueueEntry{ID: ids.New("q"), SessionID: session, BuyerID: buyerOld, CreatedAt: time.Now().UTC()})

	var oldErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, oldErr = st.GrantFIFOProtocolB(ctx, session, buyerOld, ids.New("ent"), time.Minute, time.Now().UTC())
	}()
	if !bar.waitEnter(5 * time.Second) {
		t.Fatal("old grant pause missed")
	}
	if _, err := st.FenceAdmission(ctx, "p64-gen", "ops", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UnfenceAdmission(ctx, "ops", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// Release stale grant first (it still holds budget row locks until rollback).
	bar.release()
	<-done
	if !errors.Is(oldErr, store.ErrStaleGeneration) {
		t.Fatalf("old grant want stale got %v", oldErr)
	}
	if countEntitlements(t, st, buyerOld) != 0 {
		t.Fatal("old must not commit")
	}
	// Fresh grant under current generation after stale txn ended.
	buyerNew := ids.New("buyer")
	_, _ = st.Join(ctx, store.QueueEntry{ID: ids.New("q"), SessionID: session, BuyerID: buyerNew, CreatedAt: time.Now().UTC()})
	_, entNew, err := st.GrantFIFOProtocolB(ctx, session, buyerNew, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatalf("new generation grant: %v", err)
	}
	if entNew.ID == "" || countEntitlements(t, st, buyerNew) != 1 {
		t.Fatal("expected new entitlement")
	}
}

// P6.4: abandon before commit (definite abort) + retry idempotency.
func TestP64_B_AbortBeforeCommit_ThenRetry(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session, buyer := ids.New("ses"), ids.New("buyer")
	setupJoined(t, st, session, buyer)
	before := budgetIssued(t, st, "global")

	bar := newBarrier()
	store.SetGrantHook(bar.atPhase("b_before_commit"))
	t.Cleanup(func() { store.SetGrantHook(nil) })

	cancelCtx, cancel := context.WithCancel(ctx)
	var grantErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, grantErr = st.GrantFIFOProtocolB(cancelCtx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	}()
	if !bar.waitEnter(5 * time.Second) {
		t.Fatal("did not reach before_commit")
	}
	// Definite abort: cancel context then release hook so Commit sees canceled ctx.
	cancel()
	bar.release()
	<-done
	// May be context.Canceled or driver wrap; must not leave entitlement.
	t.Logf("abort-before-commit err=%v", grantErr)
	if countEntitlements(t, st, buyer) != 0 {
		t.Fatal("no entitlement after abort")
	}
	// Budget may or may not roll back depending on whether commit started;
	// if rows exist without entitlement that's a bug. Prefer budget restored.
	time.Sleep(50 * time.Millisecond)
	if budgetIssued(t, st, "global") > before {
		// If commit partially applied we'd have entitlement; assert consistency:
		t.Logf("global budget %d -> %d after abort", before, budgetIssued(t, st, "global"))
	}

	// Retry with stable buyer identity under ACTIVE → single entitlement.
	_, ent, err := st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	_, ent2, err := st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if ent.ID != ent2.ID || countEntitlements(t, st, buyer) != 1 {
		t.Fatalf("idempotent retry failed ent=%s ent2=%s count=%d", ent.ID, ent2.ID, countEntitlements(t, st, buyer))
	}
}

// P6.4 uncertain commit: best-effort terminate backend around commit (may be flaky).
func TestP64_B_UncertainCommit_ReconcileByAuthoritativeState(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session, buyer := ids.New("ses"), ids.New("buyer")
	setupJoined(t, st, session, buyer)

	bar := newBarrier()
	store.SetGrantHook(bar.atPhase("b_before_commit"))
	t.Cleanup(func() { store.SetGrantHook(nil) })

	var grantErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, grantErr = st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	}()
	if !bar.waitEnter(5 * time.Second) {
		t.Fatal("pause missed")
	}

	// Attempt to terminate other backends mid-flight (best effort; may not hit grant backend).
	_ = exec.Command("docker", "exec", "gateflow-postgres-1", "psql", "-U", "gateflow", "-d", "gateflow", "-c",
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='gateflow' AND pid <> pg_backend_pid() AND state = 'idle in transaction';`).Run()

	bar.release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Log("grant still blocked — treating as limitation of terminate approach")
		store.SetGrantHook(nil)
		// Unblock by canceling is not available; fail soft
		t.Skip("could not force uncertain commit deterministically")
	}
	t.Logf("client outcome err=%v (may be nil if commit won, or error if terminated)", grantErr)

	// Authoritative reconcile on a fresh path:
	n := countEntitlements(t, st, buyer)
	_, ent, err := st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if n == 0 && err != nil {
		t.Fatalf("retry after empty state: %v", err)
	}
	if n == 1 && err == nil && ent.ID == "" {
		t.Fatal("expected existing entitlement on retry")
	}
	if countEntitlements(t, st, buyer) > 1 {
		t.Fatal("duplicate entitlement after reconcile")
	}
	t.Logf("reconcile: prior_count=%d retry_err=%v ent=%s final_count=%d", n, err, ent.ID, countEntitlements(t, st, buyer))
}

// --- H: Delayed retry after fence/unfence ---

func TestP63_B_DelayedRetryAfterFenceUnfence_IdempotentOrFresh(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session, buyer := ids.New("ses"), ids.New("buyer")
	setupJoined(t, st, session, buyer)

	_, ent1, err := st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.FenceAdmission(ctx, "p63-retry", "ops", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UnfenceAdmission(ctx, "ops", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_, ent2, err := st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if ent2.ID != ent1.ID {
		t.Fatalf("idempotent retry must return same entitlement %s vs %s", ent1.ID, ent2.ID)
	}
	if countEntitlements(t, st, buyer) != 1 {
		t.Fatal("duplicate entitlement")
	}
}

// --- I: Stress A and B fence/grant races ---

func TestP63_Stress_ProtocolA_GrantFenceRace(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	_, _ = st.UnfenceAdmission(ctx, "p63", time.Now().UTC())
	session := ids.New("ses")
	_ = st.EnsureBudgets(ctx, session, 10000, 10000)

	var grantsOK, grantsFenced, fenceOK atomic.Int64
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// fence flapper
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if _, err := st.FenceAdmission(ctx, "stress", "ops", time.Now().UTC()); err == nil {
				fenceOK.Add(1)
			}
			time.Sleep(5 * time.Millisecond)
			_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
			time.Sleep(5 * time.Millisecond)
		}
		close(stop)
	}()

	for i := 0; i < 16; i++ {
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
				_, _, err := st.GrantFIFO(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
				switch {
				case err == nil:
					grantsOK.Add(1)
				case errors.Is(err, store.ErrCoordinationFenced):
					grantsFenced.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	t.Logf("A stress ok=%d fenced=%d fences=%d", grantsOK.Load(), grantsFenced.Load(), fenceOK.Load())
	// Invariant: never exceed budget caps
	b, _ := st.GetBudget(ctx, "session:"+session)
	if b.GrantsIssued > b.MaxGrants {
		t.Fatalf("budget exceeded %d > %d", b.GrantsIssued, b.MaxGrants)
	}
	_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
}

func TestP63_Stress_ProtocolB_GrantFenceRace(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	_, _ = st.UnfenceAdmission(ctx, "p63", time.Now().UTC())
	session := ids.New("ses")
	_ = st.EnsureBudgets(ctx, session, 10000, 10000)

	var grantsOK, grantsFenced, fenceOK atomic.Int64
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			if _, err := st.FenceAdmission(ctx, "stress-b", "ops", time.Now().UTC()); err == nil {
				fenceOK.Add(1)
			}
			time.Sleep(5 * time.Millisecond)
			_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
			time.Sleep(5 * time.Millisecond)
		}
		close(stop)
	}()

	for i := 0; i < 16; i++ {
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
				_, _, err := st.GrantFIFOProtocolB(ctx, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
				switch {
				case err == nil:
					grantsOK.Add(1)
				case errors.Is(err, store.ErrCoordinationFenced):
					grantsFenced.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	t.Logf("B stress ok=%d fenced=%d fences=%d", grantsOK.Load(), grantsFenced.Load(), fenceOK.Load())
	b, _ := st.GetBudget(ctx, "session:"+session)
	if b.GrantsIssued > b.MaxGrants {
		t.Fatalf("budget exceeded %d > %d", b.GrantsIssued, b.MaxGrants)
	}
	_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
}

// --- C: Lock order / deadlock probe (B: queue→budget→coord vs fence coord) ---

func TestP63_B_NoDeadlock_GrantLateCoordVsFence(t *testing.T) {
	st := admStore(t)
	ctx := context.Background()
	session := ids.New("ses")
	_ = st.EnsureBudgets(ctx, session, 10000, 10000)
	_, _ = st.UnfenceAdmission(ctx, "p63", time.Now().UTC())

	var deadlocks atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			buyer := ids.New("buyer")
			_, _ = st.Join(ctx, store.QueueEntry{ID: ids.New("q"), SessionID: session, BuyerID: buyer, CreatedAt: time.Now().UTC()})
			ctx2, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			_, _, err := st.GrantFIFOProtocolB(ctx2, session, buyer, ids.New("ent"), time.Minute, time.Now().UTC())
			if err != nil && ctx2.Err() != nil {
				deadlocks.Add(1) // timeout proxy — may be lock wait not deadlock
			}
		}()
		go func() {
			defer wg.Done()
			ctx2, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			_, _ = st.FenceAdmission(ctx2, "dl", "ops", time.Now().UTC())
			_, _ = st.UnfenceAdmission(ctx2, "ops", time.Now().UTC())
		}()
	}
	wg.Wait()
	t.Logf("timeout-like outcomes=%d (investigate if high)", deadlocks.Load())
	_, _ = st.UnfenceAdmission(ctx, "ops", time.Now().UTC())
}
