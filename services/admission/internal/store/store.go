package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrBoundOther     = errors.New("bound other")
	ErrExpired         = errors.New("expired")
	ErrInvalidState    = errors.New("invalid state")
	ErrBudgetExhausted = errors.New("budget exhausted")
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

func consumeGrantTokens(ctx context.Context, tx pgx.Tx, sessionID string) error {
	// Lock global then session (consistent order avoids deadlocks).
	for _, key := range []string{"global", sessionScope(sessionID)} {
		var maxG, issued int
		err := tx.QueryRow(ctx, `
			SELECT max_grants, grants_issued FROM budget_scopes WHERE scope_key=$1 FOR UPDATE`, key).
			Scan(&maxG, &issued)
		if errors.Is(err, pgx.ErrNoRows) {
			if key == "global" {
				return ErrBudgetExhausted
			}
			// Auto-create session budget under lock path
			_, err = tx.Exec(ctx, `
				INSERT INTO budget_scopes (scope_key, max_grants, max_active_checkouts)
				VALUES ($1, 10000, 1000)
				ON CONFLICT DO NOTHING`, key)
			if err != nil {
				return err
			}
			err = tx.QueryRow(ctx, `
				SELECT max_grants, grants_issued FROM budget_scopes WHERE scope_key=$1 FOR UPDATE`, key).
				Scan(&maxG, &issued)
		}
		if err != nil {
			return err
		}
		if issued+1 > maxG {
			return ErrBudgetExhausted
		}
		_, err = tx.Exec(ctx, `
			UPDATE budget_scopes SET grants_issued = grants_issued + 1, updated_at=now()
			WHERE scope_key=$1`, key)
		if err != nil {
			return err
		}
	}
	return nil
}

func consumeActiveCheckout(ctx context.Context, tx pgx.Tx, sessionID string) error {
	for _, key := range []string{"global", sessionScope(sessionID)} {
		var maxA, active int
		err := tx.QueryRow(ctx, `
			SELECT max_active_checkouts, active_checkouts FROM budget_scopes WHERE scope_key=$1 FOR UPDATE`, key).
			Scan(&maxA, &active)
		if errors.Is(err, pgx.ErrNoRows) {
			_, err = tx.Exec(ctx, `
				INSERT INTO budget_scopes (scope_key, max_grants, max_active_checkouts)
				VALUES ($1, 10000, 1000) ON CONFLICT DO NOTHING`, key)
			if err != nil {
				return err
			}
			err = tx.QueryRow(ctx, `
				SELECT max_active_checkouts, active_checkouts FROM budget_scopes WHERE scope_key=$1 FOR UPDATE`, key).
				Scan(&maxA, &active)
		}
		if err != nil {
			return err
		}
		if active+1 > maxA {
			return ErrBudgetExhausted
		}
		_, err = tx.Exec(ctx, `
			UPDATE budget_scopes SET active_checkouts = active_checkouts + 1, updated_at=now()
			WHERE scope_key=$1`, key)
		if err != nil {
			return err
		}
	}
	return nil
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
func (s *Store) GrantFIFO(ctx context.Context, sessionID, buyerID, entitlementID string, unboundTTL time.Duration, now time.Time) (QueueEntry, Entitlement, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return QueueEntry{}, Entitlement{}, err
	}
	defer tx.Rollback(ctx)

	var e QueueEntry
	err = tx.QueryRow(ctx, `
		SELECT id, session_id, buyer_id, sequence_no, status, entitlement_id, created_at
		FROM queue_entries WHERE session_id=$1 AND buyer_id=$2 FOR UPDATE`, sessionID, buyerID).
		Scan(&e.ID, &e.SessionID, &e.BuyerID, &e.SequenceNo, &e.Status, &e.EntitlementID, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, Entitlement{}, ErrNotFound
	}
	if err != nil {
		return e, Entitlement{}, err
	}

	if e.EntitlementID != nil && *e.EntitlementID != "" {
		ent, err := s.getEntitlementTx(ctx, tx, *e.EntitlementID)
		if err != nil {
			return e, Entitlement{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return e, ent, err
		}
		return e, ent, nil
	}

	if err := consumeGrantTokens(ctx, tx, sessionID); err != nil {
		return e, Entitlement{}, err
	}

	ent := Entitlement{
		ID: entitlementID, SessionID: sessionID, BuyerID: buyerID, QueueEntryID: e.ID,
		Status: "ISSUED", UnboundExpiresAt: now.Add(unboundTTL), CreatedAt: now, UpdatedAt: now,
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO entitlements (id, session_id, buyer_id, queue_entry_id, status, unbound_expires_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		ent.ID, ent.SessionID, ent.BuyerID, ent.QueueEntryID, ent.Status, ent.UnboundExpiresAt, ent.CreatedAt, ent.UpdatedAt)
	if err != nil {
		return e, ent, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE queue_entries SET status='GRANTED', entitlement_id=$2 WHERE id=$1`, e.ID, ent.ID)
	if err != nil {
		return e, ent, err
	}
	if err := tx.Commit(ctx); err != nil {
		return e, ent, err
	}
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
