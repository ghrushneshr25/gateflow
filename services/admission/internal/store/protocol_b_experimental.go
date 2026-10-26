package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// GrantFIFOProtocolB is an EXPERIMENTAL generation-bound late-validation grant path
// (P6.2 Protocol B + P6.4 generation semantics). NOT wired to HTTP handlers.
// Production continues to use GrantFIFO (Protocol A).
//
// Generation contract (P6.4 proposed default — strict invalidation):
//   - Capture coordination epoch at grant start (authoritative generation).
//   - A successful fence increments epoch; that invalidates in-flight grants
//     that have not yet linearized under the prior generation.
//   - Unfencing does not revive an invalidated generation (epoch stays bumped).
//   - Late validation requires mode=ACTIVE AND epoch == startEpoch.
//
// Lock order: queue_entry → budget global → budget session → coordination (late).
func (s *Store) GrantFIFOProtocolB(ctx context.Context, sessionID, buyerID, entitlementID string, unboundTTL time.Duration, now time.Time) (QueueEntry, Entitlement, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return QueueEntry{}, Entitlement{}, err
	}
	defer tx.Rollback(ctx)

	maybeGrantHook("b_begin")

	// Capture generation early (no lock). Late FOR UPDATE is authoritative.
	var startEpoch int64
	var startMode string
	err = tx.QueryRow(ctx, `
		SELECT epoch, mode FROM coordination_state WHERE id=$1`, CoordID).
		Scan(&startEpoch, &startMode)
	if errors.Is(err, pgx.ErrNoRows) {
		return QueueEntry{}, Entitlement{}, ErrCoordinationUnavailable
	}
	if err != nil {
		return QueueEntry{}, Entitlement{}, err
	}
	if startMode != CoordModeActive && startMode != CoordModeFenced {
		return QueueEntry{}, Entitlement{}, ErrCoordinationUnavailable
	}
	// Fail closed early if already fenced (optimization; late check still authoritative).
	if startMode != CoordModeActive {
		return QueueEntry{}, Entitlement{}, ErrCoordinationFenced
	}
	maybeGrantHook("b_after_epoch_capture")

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
	maybeGrantHook("b_after_queue")

	// Idempotent existing entitlement — allowed while fenced (same product rule as A).
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

	maybeGrantHook("b_before_budget")
	if err := consumeGrantTokens(ctx, tx, sessionID); err != nil {
		return e, Entitlement{}, err
	}
	maybeGrantHook("b_after_budget")

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
	maybeGrantHook("b_before_late_coord")

	// LATE coordination validation — generation-bound linearization vs fence.
	coord, err := lockCoordination(ctx, tx)
	if err != nil {
		return e, Entitlement{}, err
	}
	maybeGrantHook("b_after_late_coord")
	if coord.Mode != CoordModeActive {
		return e, Entitlement{}, ErrCoordinationFenced
	}
	if coord.Epoch != startEpoch {
		// Fence advanced generation; unfence does not restore startEpoch.
		return e, Entitlement{}, ErrStaleGeneration
	}

	maybeGrantHook("b_before_commit")
	if err := tx.Commit(ctx); err != nil {
		return e, ent, err
	}
	maybeGrantHook("b_after_commit")
	e.Status = "GRANTED"
	e.EntitlementID = &ent.ID
	return e, ent, nil
}
