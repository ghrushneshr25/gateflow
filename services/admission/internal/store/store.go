package store

import (
	"context"
	"errors"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/metrics"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound             = errors.New("not found")
	ErrBoundOther          = errors.New("bound other")
	ErrExpired              = errors.New("expired")
	ErrInvalidState         = errors.New("invalid state")
	ErrBudgetExhausted      = errors.New("budget exhausted")
	ErrCoordinationFenced      = errors.New("coordination fenced")
	ErrCoordinationUnavailable = errors.New("coordination state unavailable")
	// ErrStaleGeneration: experimental Protocol B — grant started under an older
	// coordination epoch than the authoritative row at late validation (e.g. a
	// fence advanced epoch even if mode is ACTIVE again after unfence).
	ErrStaleGeneration = errors.New("stale coordination generation")
)

const (
	CoordModeActive = "ACTIVE"
	CoordModeFenced = "FENCED"
	CoordID         = "admission"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type QueueEntry struct {
	ID            string    `json:"id"`
	SessionID     string    `json:"session_id"`
	BuyerID       string    `json:"buyer_id"`
	SequenceNo    int64     `json:"sequence_no"`
	Status        string    `json:"status"`
	EntitlementID *string   `json:"entitlement_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type Entitlement struct {
	ID               string    `json:"id"`
	SessionID        string    `json:"session_id"`
	BuyerID          string    `json:"buyer_id"`
	QueueEntryID     string    `json:"queue_entry_id"`
	Status           string    `json:"status"`
	UnboundExpiresAt time.Time `json:"unbound_expires_at"`
	BoundCheckoutOp *string   `json:"bound_checkout_op,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (s *Store) Join(ctx context.Context, entry QueueEntry) (QueueEntry, error) {
	var existing QueueEntry
	// Atomic upsert: concurrent joins for same buyer/session cannot create duplicates.
	err := s.pool.QueryRow(ctx, `
		INSERT INTO queue_entries (id, session_id, buyer_id, status, created_at)
		VALUES ($1,$2,$3,'JOINED',$4)
		ON CONFLICT (session_id, buyer_id) DO UPDATE
			SET session_id = queue_entries.session_id
		RETURNING id, session_id, buyer_id, sequence_no, status, entitlement_id, created_at`,
		entry.ID, entry.SessionID, entry.BuyerID, entry.CreatedAt,
	).Scan(&existing.ID, &existing.SessionID, &existing.BuyerID, &existing.SequenceNo, &existing.Status, &existing.EntitlementID, &existing.CreatedAt)
	return existing, err
}

func (s *Store) GetMine(ctx context.Context, sessionID, buyerID string) (QueueEntry, error) {
	var e QueueEntry
	err := s.pool.QueryRow(ctx, `
		SELECT id, session_id, buyer_id, sequence_no, status, entitlement_id, created_at
		FROM queue_entries WHERE session_id=$1 AND buyer_id=$2`, sessionID, buyerID).
		Scan(&e.ID, &e.SessionID, &e.BuyerID, &e.SequenceNo, &e.Status, &e.EntitlementID, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

type Budget struct {
	ScopeKey           string `json:"scope_key"`
	MaxGrants          int    `json:"max_grants"`
	GrantsIssued       int    `json:"grants_issued"`
	MaxActiveCheckouts int    `json:"max_active_checkouts"`
	ActiveCheckouts    int    `json:"active_checkouts"`
	CoordinationEpoch  int64  `json:"coordination_epoch"`
}

func sessionScope(sessionID string) string { return "session:" + sessionID }

// EnsureBudgets creates session budget scope if missing (defaults for local/tests).
func (s *Store) EnsureBudgets(ctx context.Context, sessionID string, maxGrants, maxActive int) error {
	if maxGrants <= 0 {
		maxGrants = 10000
	}
	if maxActive <= 0 {
		maxActive = 1000
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO budget_scopes (scope_key, max_grants, max_active_checkouts)
		VALUES ($1,$2,$3)
		ON CONFLICT (scope_key) DO NOTHING`, sessionScope(sessionID), maxGrants, maxActive)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO budget_scopes (scope_key, max_grants, max_active_checkouts)
		VALUES ('global', 100000, 10000)
		ON CONFLICT (scope_key) DO NOTHING`)
	return err
}

func (s *Store) GetBudget(ctx context.Context, scopeKey string) (Budget, error) {
	var b Budget
	err := s.pool.QueryRow(ctx, `
		SELECT scope_key, max_grants, grants_issued, max_active_checkouts, active_checkouts, coordination_epoch
		FROM budget_scopes WHERE scope_key=$1`, scopeKey).
		Scan(&b.ScopeKey, &b.MaxGrants, &b.GrantsIssued, &b.MaxActiveCheckouts, &b.ActiveCheckouts, &b.CoordinationEpoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrNotFound
	}
	return b, err
}

// consumeGrantTokens atomically increments global then session grant counters.
// P6.1: per-scope atomic UPDATE. P6.7: one round-trip CTE for both scopes while
// coordination lock is held. Semantics unchanged: fail closed if either bump fails.
func consumeGrantTokens(ctx context.Context, tx pgx.Tx, sessionID string) error {
	sess := sessionScope(sessionID)
	// Ensure session row exists outside the conditional bumps (idempotent).
	if _, err := tx.Exec(ctx, `
		INSERT INTO budget_scopes (scope_key, max_grants, max_active_checkouts)
		VALUES ($1, 10000, 1000)
		ON CONFLICT DO NOTHING`, sess); err != nil {
		return err
	}
	var gOK, sOK int
	err := tx.QueryRow(ctx, `
		WITH g AS (
			UPDATE budget_scopes
			SET grants_issued = grants_issued + 1, updated_at = now()
			WHERE scope_key = 'global' AND grants_issued < max_grants
			RETURNING 1
		),
		s AS (
			UPDATE budget_scopes
			SET grants_issued = grants_issued + 1, updated_at = now()
			WHERE scope_key = $1 AND grants_issued < max_grants
			RETURNING 1
		)
		SELECT (SELECT count(*)::int FROM g), (SELECT count(*)::int FROM s)`, sess).
		Scan(&gOK, &sOK)
	if err != nil {
		return err
	}
	if gOK != 1 || sOK != 1 {
		return ErrBudgetExhausted
	}
	return nil
}

func bumpGrantToken(ctx context.Context, tx pgx.Tx, key string) error {
	// Kept for bind/active paths and tests that may call single-scope bumps.
	var issued int
	err := tx.QueryRow(ctx, `
		UPDATE budget_scopes
		SET grants_issued = grants_issued + 1, updated_at = now()
		WHERE scope_key = $1 AND grants_issued < max_grants
		RETURNING grants_issued`, key).Scan(&issued)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if key == "global" {
		return ErrBudgetExhausted
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO budget_scopes (scope_key, max_grants, max_active_checkouts)
		VALUES ($1, 10000, 1000)
		ON CONFLICT DO NOTHING`, key); err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `
		UPDATE budget_scopes
		SET grants_issued = grants_issued + 1, updated_at = now()
		WHERE scope_key = $1 AND grants_issued < max_grants
		RETURNING grants_issued`, key).Scan(&issued)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBudgetExhausted
	}
	return err
}

func consumeActiveCheckout(ctx context.Context, tx pgx.Tx, sessionID string) error {
	for _, key := range []string{"global", sessionScope(sessionID)} {
		if err := bumpActiveCheckout(ctx, tx, key); err != nil {
			return err
		}
	}
	return nil
}

func bumpActiveCheckout(ctx context.Context, tx pgx.Tx, key string) error {
	var active int
	err := tx.QueryRow(ctx, `
		UPDATE budget_scopes
		SET active_checkouts = active_checkouts + 1, updated_at = now()
		WHERE scope_key = $1 AND active_checkouts < max_active_checkouts
		RETURNING active_checkouts`, key).Scan(&active)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if key == "global" {
		return ErrBudgetExhausted
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO budget_scopes (scope_key, max_grants, max_active_checkouts)
		VALUES ($1, 10000, 1000) ON CONFLICT DO NOTHING`, key); err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `
		UPDATE budget_scopes
		SET active_checkouts = active_checkouts + 1, updated_at = now()
		WHERE scope_key = $1 AND active_checkouts < max_active_checkouts
		RETURNING active_checkouts`, key).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBudgetExhausted
	}
	return err
}

func releaseActiveCheckout(ctx context.Context, tx pgx.Tx, sessionID string) error {
	for _, key := range []string{"global", sessionScope(sessionID)} {
		_, err := tx.Exec(ctx, `
			UPDATE budget_scopes
			SET active_checkouts = GREATEST(active_checkouts - 1, 0), updated_at=now()
			WHERE scope_key=$1`, key)
		if err != nil {
			return err
		}
	}
	return nil
}

// GrantFIFO issues entitlement for JOINED entry under durable grant budgets.
// Coordination row is locked FOR UPDATE for the whole transaction so a concurrent
// fence cannot report success while this grant can still commit under ACTIVE.
//
// Lock order (stable): coordination → queue_entry → budget global → budget session.
// Phase timings are recorded for P6.1 diagnosis (does not change semantics).
func (s *Store) GrantFIFO(ctx context.Context, sessionID, buyerID, entitlementID string, unboundTTL time.Duration, now time.Time) (QueueEntry, Entitlement, error) {
	t0 := time.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return QueueEntry{}, Entitlement{}, err
	}
	defer tx.Rollback(ctx)

	// Linearization lock with operator fence (first durable lock in grant path).
	tCoord := time.Now()
	coord, err := lockCoordination(ctx, tx)
	metrics.AdmissionGrantPhase("coord", time.Since(tCoord).Seconds())
	if err != nil {
		return QueueEntry{}, Entitlement{}, err
	}
	maybeGrantHook("a_after_coord")

	tQueue := time.Now()
	var e QueueEntry
	err = tx.QueryRow(ctx, `
		SELECT id, session_id, buyer_id, sequence_no, status, entitlement_id, created_at
		FROM queue_entries WHERE session_id=$1 AND buyer_id=$2 FOR UPDATE`, sessionID, buyerID).
		Scan(&e.ID, &e.SessionID, &e.BuyerID, &e.SequenceNo, &e.Status, &e.EntitlementID, &e.CreatedAt)
	metrics.AdmissionGrantPhase("queue", time.Since(tQueue).Seconds())
	if errors.Is(err, pgx.ErrNoRows) {
		return e, Entitlement{}, ErrNotFound
	}
	if err != nil {
		return e, Entitlement{}, err
	}
	maybeGrantHook("a_after_queue")

	// Existing entitlement: not a new grant — allowed while FENCED.
	if e.EntitlementID != nil && *e.EntitlementID != "" {
		ent, err := s.getEntitlementTx(ctx, tx, *e.EntitlementID)
		if err != nil {
			return e, Entitlement{}, err
		}
		tCommit := time.Now()
		if err := tx.Commit(ctx); err != nil {
			return e, ent, err
		}
		metrics.AdmissionGrantPhase("commit", time.Since(tCommit).Seconds())
		metrics.AdmissionGrantPhase("total", time.Since(t0).Seconds())
		return e, ent, nil
	}

	// New grant issuance requires ACTIVE under the held coordination lock.
	if coord.Mode != CoordModeActive {
		return e, Entitlement{}, ErrCoordinationFenced
	}

	tBudget := time.Now()
	if err := consumeGrantTokens(ctx, tx, sessionID); err != nil {
		metrics.AdmissionGrantPhase("budget", time.Since(tBudget).Seconds())
		return e, Entitlement{}, err
	}
	budgetDur := time.Since(tBudget)
	maybeGrantHook("a_after_budget")

	tWrite := time.Now()
	ent := Entitlement{
		ID: entitlementID, SessionID: sessionID, BuyerID: buyerID, QueueEntryID: e.ID,
		Status: "ISSUED", UnboundExpiresAt: now.Add(unboundTTL), CreatedAt: now, UpdatedAt: now,
	}
	// P6.7: single round-trip insert entitlement + grant queue row (same txn/locks).
	_, err = tx.Exec(ctx, `
		WITH ins AS (
			INSERT INTO entitlements (id, session_id, buyer_id, queue_entry_id, status, unbound_expires_at, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			RETURNING id
		)
		UPDATE queue_entries q
		SET status = 'GRANTED', entitlement_id = (SELECT id FROM ins)
		WHERE q.id = $4`,
		ent.ID, ent.SessionID, ent.BuyerID, ent.QueueEntryID, ent.Status, ent.UnboundExpiresAt, ent.CreatedAt, ent.UpdatedAt)
	writeDur := time.Since(tWrite)
	if err != nil {
		return e, ent, err
	}
	maybeGrantHook("a_before_commit")

	tCommit := time.Now()
	if err := tx.Commit(ctx); err != nil {
		return e, ent, err
	}
	commitDur := time.Since(tCommit)
	// Record phase metrics after commit so Observe does not extend lock hold
	// (timers still measured under lock). Fence/lock semantics unchanged.
	metrics.AdmissionGrantPhase("budget", budgetDur.Seconds())
	metrics.AdmissionGrantPhase("write", writeDur.Seconds())
	metrics.AdmissionGrantPhase("commit", commitDur.Seconds())
	metrics.AdmissionGrantPhase("total", time.Since(t0).Seconds())
	e.Status = "GRANTED"
	e.EntitlementID = &ent.ID
	return e, ent, nil
}

func (s *Store) GetEntitlement(ctx context.Context, id string) (Entitlement, error) {
	var e Entitlement
	err := s.pool.QueryRow(ctx, `
		SELECT id, session_id, buyer_id, queue_entry_id, status, unbound_expires_at, bound_checkout_op, created_at, updated_at
		FROM entitlements WHERE id=$1`, id).
		Scan(&e.ID, &e.SessionID, &e.BuyerID, &e.QueueEntryID, &e.Status, &e.UnboundExpiresAt, &e.BoundCheckoutOp, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

func (s *Store) getEntitlementTx(ctx context.Context, tx pgx.Tx, id string) (Entitlement, error) {
	var e Entitlement
	err := tx.QueryRow(ctx, `
		SELECT id, session_id, buyer_id, queue_entry_id, status, unbound_expires_at, bound_checkout_op, created_at, updated_at
		FROM entitlements WHERE id=$1`, id).
		Scan(&e.ID, &e.SessionID, &e.BuyerID, &e.QueueEntryID, &e.Status, &e.UnboundExpiresAt, &e.BoundCheckoutOp, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

func (s *Store) Bind(ctx context.Context, id, buyerID, checkoutOp string, now time.Time) (Entitlement, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Entitlement{}, err
	}
	defer tx.Rollback(ctx)

	var e Entitlement
	err = tx.QueryRow(ctx, `
		SELECT id, session_id, buyer_id, queue_entry_id, status, unbound_expires_at, bound_checkout_op, created_at, updated_at
		FROM entitlements WHERE id=$1 FOR UPDATE`, id).
		Scan(&e.ID, &e.SessionID, &e.BuyerID, &e.QueueEntryID, &e.Status, &e.UnboundExpiresAt, &e.BoundCheckoutOp, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	if e.BuyerID != buyerID {
		return e, ErrBoundOther
	}
	if e.Status == "BOUND" || e.Status == "CONSUMED" {
		if e.BoundCheckoutOp != nil && *e.BoundCheckoutOp == checkoutOp {
			if err := tx.Commit(ctx); err != nil {
				return e, err
			}
			return e, nil
		}
		return e, ErrBoundOther
	}
	if e.Status != "ISSUED" {
		return e, ErrInvalidState
	}
	if e.Status == "ISSUED" && !e.UnboundExpiresAt.After(now) {
		_, _ = tx.Exec(ctx, `UPDATE entitlements SET status='EXPIRED', updated_at=$2 WHERE id=$1`, id, now)
		_ = tx.Commit(ctx)
		return e, ErrExpired
	}

	if err := consumeActiveCheckout(ctx, tx, e.SessionID); err != nil {
		return e, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE entitlements SET status='BOUND', bound_checkout_op=$2, updated_at=$3 WHERE id=$1`, id, checkoutOp, now)
	if err != nil {
		return e, err
	}
	if err := tx.Commit(ctx); err != nil {
		return e, err
	}
	op := checkoutOp
	e.Status = "BOUND"
	e.BoundCheckoutOp = &op
	e.UpdatedAt = now
	return e, nil
}

func (s *Store) Consume(ctx context.Context, id, checkoutOp string, now time.Time) (Entitlement, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Entitlement{}, err
	}
	defer tx.Rollback(ctx)
	var e Entitlement
	err = tx.QueryRow(ctx, `
		SELECT id, session_id, buyer_id, queue_entry_id, status, unbound_expires_at, bound_checkout_op, created_at, updated_at
		FROM entitlements WHERE id=$1 FOR UPDATE`, id).
		Scan(&e.ID, &e.SessionID, &e.BuyerID, &e.QueueEntryID, &e.Status, &e.UnboundExpiresAt, &e.BoundCheckoutOp, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	if e.Status == "CONSUMED" {
		if err := tx.Commit(ctx); err != nil {
			return e, err
		}
		return e, nil
	}
	if e.Status != "BOUND" || e.BoundCheckoutOp == nil || *e.BoundCheckoutOp != checkoutOp {
		return e, ErrInvalidState
	}
	_, err = tx.Exec(ctx, `UPDATE entitlements SET status='CONSUMED', updated_at=$2 WHERE id=$1`, id, now)
	if err != nil {
		return e, err
	}
	// Release active-checkout slot once on first consume.
	if err := releaseActiveCheckout(ctx, tx, e.SessionID); err != nil {
		return e, err
	}
	if err := tx.Commit(ctx); err != nil {
		return e, err
	}
	e.Status = "CONSUMED"
	e.UpdatedAt = now
	return e, nil
}

// MarkEventProcessed records consumer dedupe. Returns true if this is the first time.
func (s *Store) MarkEventProcessed(ctx context.Context, eventID string) (bool, error) {
	ct, err := s.pool.Exec(ctx, `
		INSERT INTO processed_events (event_id) VALUES ($1)
		ON CONFLICT (event_id) DO NOTHING`, eventID)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// Coordination is the durable fence/active state for admission grants.
type Coordination struct {
	ID          string     `json:"id"`
	Epoch       int64      `json:"epoch"`
	Mode        string     `json:"mode"`
	FenceReason *string    `json:"fence_reason,omitempty"`
	FencedAt    *time.Time `json:"fenced_at,omitempty"`
	FencedBy    *string    `json:"fenced_by,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func lockCoordination(ctx context.Context, tx pgx.Tx) (Coordination, error) {
	var c Coordination
	err := tx.QueryRow(ctx, `
		SELECT id, epoch, mode, fence_reason, fenced_at, fenced_by, updated_at
		FROM coordination_state WHERE id=$1 FOR UPDATE`, CoordID).
		Scan(&c.ID, &c.Epoch, &c.Mode, &c.FenceReason, &c.FencedAt, &c.FencedBy, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrCoordinationUnavailable
	}
	if err != nil {
		return c, err
	}
	if c.Mode != CoordModeActive && c.Mode != CoordModeFenced {
		return c, ErrCoordinationUnavailable
	}
	return c, nil
}

func (s *Store) GetCoordination(ctx context.Context) (Coordination, error) {
	var c Coordination
	err := s.pool.QueryRow(ctx, `
		SELECT id, epoch, mode, fence_reason, fenced_at, fenced_by, updated_at
		FROM coordination_state WHERE id=$1`, CoordID).
		Scan(&c.ID, &c.Epoch, &c.Mode, &c.FenceReason, &c.FencedAt, &c.FencedBy, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrCoordinationUnavailable
	}
	return c, err
}

// FenceAdmission sets mode=FENCED. Linearization: holds FOR UPDATE until commit.
// After this returns successfully, no GrantFIFO may commit a *new* entitlement under ACTIVE.
func (s *Store) FenceAdmission(ctx context.Context, reason, actor string, now time.Time) (Coordination, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Coordination{}, err
	}
	defer tx.Rollback(ctx)

	c, err := lockCoordination(ctx, tx)
	if err != nil {
		return c, err
	}
	if c.Mode == CoordModeFenced {
		// Idempotent fence: keep state, still "success".
		if err := tx.Commit(ctx); err != nil {
			return c, err
		}
		return c, nil
	}
	if reason == "" {
		reason = "operator_fence"
	}
	err = tx.QueryRow(ctx, `
		UPDATE coordination_state
		SET mode='FENCED',
		    epoch = epoch + 1,
		    fence_reason=$2,
		    fenced_at=$3,
		    fenced_by=$4,
		    updated_at=$3
		WHERE id=$1
		RETURNING id, epoch, mode, fence_reason, fenced_at, fenced_by, updated_at`,
		CoordID, reason, now, actor,
	).Scan(&c.ID, &c.Epoch, &c.Mode, &c.FenceReason, &c.FencedAt, &c.FencedBy, &c.UpdatedAt)
	if err != nil {
		return c, err
	}
	if err := tx.Commit(ctx); err != nil {
		return c, err
	}
	return c, nil
}

// UnfenceAdmission sets mode=ACTIVE. Explicit operator action only.
func (s *Store) UnfenceAdmission(ctx context.Context, actor string, now time.Time) (Coordination, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Coordination{}, err
	}
	defer tx.Rollback(ctx)

	c, err := lockCoordination(ctx, tx)
	if err != nil {
		return c, err
	}
	if c.Mode == CoordModeActive {
		if err := tx.Commit(ctx); err != nil {
			return c, err
		}
		return c, nil
	}
	err = tx.QueryRow(ctx, `
		UPDATE coordination_state
		SET mode='ACTIVE',
		    fence_reason=NULL,
		    fenced_at=NULL,
		    fenced_by=$2,
		    updated_at=$3
		WHERE id=$1
		RETURNING id, epoch, mode, fence_reason, fenced_at, fenced_by, updated_at`,
		CoordID, actor, now,
	).Scan(&c.ID, &c.Epoch, &c.Mode, &c.FenceReason, &c.FencedAt, &c.FencedBy, &c.UpdatedAt)
	if err != nil {
		return c, err
	}
	if err := tx.Commit(ctx); err != nil {
		return c, err
	}
	return c, nil
}

// SetBudgetCaps updates max caps without resetting counters (safe multi-instance config).
func (s *Store) SetBudgetCaps(ctx context.Context, scopeKey string, maxGrants, maxActive int) error {
	if maxGrants < 0 || maxActive < 0 {
		return errors.New("caps must be non-negative")
	}
	ct, err := s.pool.Exec(ctx, `
		INSERT INTO budget_scopes (scope_key, max_grants, max_active_checkouts)
		VALUES ($1,$2,$3)
		ON CONFLICT (scope_key) DO UPDATE SET
			max_grants = EXCLUDED.max_grants,
			max_active_checkouts = EXCLUDED.max_active_checkouts,
			updated_at = now()`, scopeKey, maxGrants, maxActive)
	if err != nil {
		return err
	}
	_ = ct
	return nil
}
