package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/telemetry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func captureTraceCarrier(ctx context.Context) string {
	return telemetry.CaptureCarrier(ctx)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var (
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("conflict")
	ErrLimit       = errors.New("purchase limit exceeded")
	ErrIdempotency = errors.New("idempotency conflict")
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type Order struct {
	ID                   string    `json:"id"`
	BuyerID              string    `json:"buyer_id"`
	SessionID            string    `json:"session_id"`
	TicketTypeID         string    `json:"ticket_type_id"`
	PoolID               string    `json:"pool_id"`
	Quantity             int       `json:"quantity"`
	Status               string    `json:"status"`
	EntitlementID        string    `json:"entitlement_id"`
	CheckoutOperationID  string    `json:"checkout_operation_id"`
	InventoryOperationID string    `json:"inventory_operation_id"`
	PaymentOperationID   string    `json:"payment_operation_id"`
	ReservationID        *string   `json:"reservation_id,omitempty"`
	PaymentID            *string   `json:"payment_id,omitempty"`
	AmountCents          int64     `json:"amount_cents"`
	Currency             string    `json:"currency"`
	PolicyVersion        int       `json:"policy_version"`
	MaxPerBuyer          int       `json:"max_per_buyer"`
	IdempotencyKey            string     `json:"idempotency_key"`
	RequestHash               string     `json:"request_hash"`
	PaymentDeadlineAt         *time.Time `json:"payment_deadline_at,omitempty"`
	CompensationOperationID   *string    `json:"compensation_operation_id,omitempty"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

const orderSelectCols = `id, buyer_id, session_id, ticket_type_id, pool_id, quantity, status, entitlement_id,
		       checkout_operation_id, inventory_operation_id, payment_operation_id, reservation_id, payment_id,
		       amount_cents, currency, policy_version, max_per_buyer, idempotency_key, request_hash,
		       payment_deadline_at, compensation_operation_id, created_at, updated_at`

func scanOrder(row pgx.Row) (Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.BuyerID, &o.SessionID, &o.TicketTypeID, &o.PoolID, &o.Quantity, &o.Status, &o.EntitlementID,
		&o.CheckoutOperationID, &o.InventoryOperationID, &o.PaymentOperationID, &o.ReservationID, &o.PaymentID,
		&o.AmountCents, &o.Currency, &o.PolicyVersion, &o.MaxPerBuyer, &o.IdempotencyKey, &o.RequestHash,
		&o.PaymentDeadlineAt, &o.CompensationOperationID, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}

func (s *Store) GetByIdempotency(ctx context.Context, buyerID, key string) (Order, error) {
	o, err := scanOrder(s.pool.QueryRow(ctx, `SELECT `+orderSelectCols+` FROM orders WHERE buyer_id=$1 AND idempotency_key=$2`, buyerID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

func (s *Store) Get(ctx context.Context, id string) (Order, error) {
	o, err := scanOrder(s.pool.QueryRow(ctx, `SELECT `+orderSelectCols+` FROM orders WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

func (s *Store) ListByBuyer(ctx context.Context, buyerID string) ([]Order, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+orderSelectCols+` FROM orders WHERE buyer_id=$1 ORDER BY created_at DESC`, buyerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CreateWithHold creates order + allowance hold atomically if limit allows.
// Serializes on allowance_scopes(buyer_id, session_id). Same idempotency key + hash returns existing.
func (s *Store) CreateWithHold(ctx context.Context, o Order, holdID string, now time.Time) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback(ctx)

	// Ensure scope row exists, then lock it for the duration of the transaction.
	_, err = tx.Exec(ctx, `
		INSERT INTO allowance_scopes (buyer_id, session_id) VALUES ($1,$2)
		ON CONFLICT DO NOTHING`, o.BuyerID, o.SessionID)
	if err != nil {
		return Order{}, err
	}
	_, err = tx.Exec(ctx, `
		SELECT 1 FROM allowance_scopes WHERE buyer_id=$1 AND session_id=$2 FOR UPDATE`, o.BuyerID, o.SessionID)
	if err != nil {
		return Order{}, err
	}

	// Idempotent create under the same lock.
	existing, err := scanOrder(tx.QueryRow(ctx, `
		SELECT `+orderSelectCols+` FROM orders WHERE buyer_id=$1 AND idempotency_key=$2 FOR UPDATE`, o.BuyerID, o.IdempotencyKey))
	if err == nil {
		if existing.RequestHash != "" && o.RequestHash != "" && existing.RequestHash != o.RequestHash {
			return existing, ErrIdempotency
		}
		if existing.SessionID != o.SessionID || existing.TicketTypeID != o.TicketTypeID ||
			existing.PoolID != o.PoolID || existing.Quantity != o.Quantity {
			return existing, ErrIdempotency
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Order{}, err
	}

	var used int
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(quantity),0) FROM allowance_holds
		WHERE buyer_id=$1 AND session_id=$2 AND status IN ('HELD','COMMITTED')`, o.BuyerID, o.SessionID).Scan(&used)
	if err != nil {
		return Order{}, err
	}
	if used+o.Quantity > o.MaxPerBuyer {
		return Order{}, ErrLimit
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO orders (id, buyer_id, session_id, ticket_type_id, pool_id, quantity, status, entitlement_id,
			checkout_operation_id, inventory_operation_id, payment_operation_id, amount_cents, currency,
			policy_version, max_per_buyer, idempotency_key, request_hash, payment_deadline_at, compensation_operation_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		o.ID, o.BuyerID, o.SessionID, o.TicketTypeID, o.PoolID, o.Quantity, o.Status, o.EntitlementID,
		o.CheckoutOperationID, o.InventoryOperationID, o.PaymentOperationID, o.AmountCents, o.Currency,
		o.PolicyVersion, o.MaxPerBuyer, o.IdempotencyKey, o.RequestHash, o.PaymentDeadlineAt, o.CompensationOperationID, now, now)
	if err != nil {
		return Order{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO allowance_holds (id, buyer_id, session_id, order_id, quantity, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,'HELD',$6,$6)`, holdID, o.BuyerID, o.SessionID, o.ID, o.Quantity, now)
	if err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	o.CreatedAt = now
	o.UpdatedAt = now
	return o, nil
}

// HeldAndCommittedQty returns durable allowance usage for a buyer/session.
func (s *Store) HeldAndCommittedQty(ctx context.Context, buyerID, sessionID string) (int, error) {
	var used int
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(quantity),0) FROM allowance_holds
		WHERE buyer_id=$1 AND session_id=$2 AND status IN ('HELD','COMMITTED')`, buyerID, sessionID).Scan(&used)
	return used, err
}

func (s *Store) Update(ctx context.Context, o Order) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE orders SET status=$2, reservation_id=$3, payment_id=$4,
			payment_deadline_at=$5, compensation_operation_id=$6, updated_at=$7 WHERE id=$1`,
		o.ID, o.Status, o.ReservationID, o.PaymentID, o.PaymentDeadlineAt, o.CompensationOperationID, o.UpdatedAt)
	return err
}

func (s *Store) Confirm(ctx context.Context, orderID, eventID, corr string, payload any, now time.Time) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback(ctx)

	o, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderSelectCols+` FROM orders WHERE id=$1 FOR UPDATE`, orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	if o.Status == "CONFIRMED" {
		return o, nil
	}

	_, err = tx.Exec(ctx, `UPDATE orders SET status='CONFIRMED', updated_at=$2 WHERE id=$1`, orderID, now)
	if err != nil {
		return o, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE allowance_holds SET status='COMMITTED', updated_at=$2 WHERE order_id=$1 AND status='HELD'`, orderID, now)
	if err != nil {
		return o, err
	}
	raw, _ := json.Marshal(payload)
	// Capture W3C carrier from checkout span for async outbox publish (P5.4.2).
	// Empty when OTel off or no active span; publisher still works.
	carrier := captureTraceCarrier(ctx)
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox (event_id, event_type, schema_version, aggregate_id, payload, correlation_id, created_at, trace_carrier)
		VALUES ($1,'order.order.confirmed',1,$2,$3,$4,$5,$6)
		ON CONFLICT (event_id) DO NOTHING`, eventID, orderID, raw, corr, now, nullIfEmpty(carrier))
	if err != nil {
		return o, err
	}
	if err := tx.Commit(ctx); err != nil {
		return o, err
	}
	o.Status = "CONFIRMED"
	o.UpdatedAt = now
	return o, nil
}

// MarkEventProcessed is the idempotent consumer gate for outbox/Kafka delivery.
func (s *Store) MarkEventProcessed(ctx context.Context, eventID string) (bool, error) {
	ct, err := s.pool.Exec(ctx, `
		INSERT INTO processed_events (event_id) VALUES ($1)
		ON CONFLICT (event_id) DO NOTHING`, eventID)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

func (s *Store) Fail(ctx context.Context, orderID string, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `UPDATE orders SET status='FAILED', updated_at=$2 WHERE id=$1`, orderID, now)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE allowance_holds SET status='RELEASED', updated_at=$2 WHERE order_id=$1 AND status='HELD'`, orderID, now)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListByStatus returns orders in a given status (bounded).
func (s *Store) ListByStatus(ctx context.Context, status string, limit int) ([]Order, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT `+orderSelectCols+` FROM orders WHERE status=$1 ORDER BY updated_at ASC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// CountOutboxForAggregate returns how many outbox rows exist for an aggregate (order) id.
func (s *Store) CountOutboxForAggregate(ctx context.Context, aggregateID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox WHERE aggregate_id=$1`, aggregateID).Scan(&n)
	return n, err
}

// CountOutboxByEventID returns 1 if event exists.
func (s *Store) CountOutboxByEventID(ctx context.Context, eventID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox WHERE event_id=$1`, eventID).Scan(&n)
	return n, err
}

// OutboxRow is a claimed unpublished event.
type OutboxRow struct {
	ID            int64
	EventID       string
	EventType     string
	SchemaVersion int
	AggregateID   string
	Payload       []byte
	CorrelationID string
	TraceCarrier  string // optional W3C header map JSON from checkout (P5.4.2)
	CreatedAt     time.Time
	AttemptCount  int
}

// ClaimOutbox atomically leases unpublished rows for a worker (SKIP LOCKED).
func (s *Store) ClaimOutbox(ctx context.Context, workerID string, lease time.Duration, limit int) ([]OutboxRow, error) {
	if limit <= 0 {
		limit = 20
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id FROM outbox
		WHERE published_at IS NULL
		  AND quarantine = false
		  AND (locked_until IS NULL OR locked_until < now())
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}

	until := time.Now().UTC().Add(lease)
	var out []OutboxRow
	for _, id := range ids {
		var r OutboxRow
		err := tx.QueryRow(ctx, `
			UPDATE outbox
			SET locked_by=$2, locked_until=$3, attempt_count = attempt_count + 1, last_error = NULL
			WHERE id=$1
			RETURNING id, event_id, event_type, schema_version, aggregate_id, payload, correlation_id,
			          COALESCE(trace_carrier,''), created_at, attempt_count`,
			id, workerID, until,
		).Scan(&r.ID, &r.EventID, &r.EventType, &r.SchemaVersion, &r.AggregateID, &r.Payload, &r.CorrelationID, &r.TraceCarrier, &r.CreatedAt, &r.AttemptCount)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) MarkOutboxPublished(ctx context.Context, id int64, now time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE outbox
		SET published_at=$2, locked_until=NULL, locked_by=NULL, last_error=NULL
		WHERE id=$1 AND published_at IS NULL`, id, now)
	return err
}

func (s *Store) FailOutboxClaim(ctx context.Context, id int64, errMsg string) error {
	// Threshold grows with replay_count so operators can grant additional attempts without wiping history.
	_, err := s.pool.Exec(ctx, `
		UPDATE outbox
		SET locked_until=NULL, locked_by=NULL, last_error=$2,
		    quarantine = CASE
		      WHEN attempt_count >= (25 + COALESCE(replay_count,0) * 10) THEN true
		      ELSE quarantine
		    END
		WHERE id=$1 AND published_at IS NULL`, id, errMsg)
	return err
}

func (s *Store) OutboxBacklog(ctx context.Context) (oldestAge time.Duration, count int, err error) {
	var n int
	var oldest *time.Time
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*), MIN(created_at)
		FROM outbox
		WHERE published_at IS NULL AND quarantine = false`).Scan(&n, &oldest)
	if err != nil {
		return 0, 0, err
	}
	if oldest == nil || n == 0 {
		return 0, 0, nil
	}
	return time.Since(oldest.UTC()), n, nil
}

func (s *Store) CountQuarantined(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox WHERE quarantine = true`).Scan(&n)
	return n, err
}

// ApplyOrderConfirmedEffect records the consumer effect atomically with dedupe.
// Returns first=true if this is the first successful processing of eventID.
func (s *Store) ApplyOrderConfirmedEffect(ctx context.Context, eventID, orderID string) (first bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	ct, err := tx.Exec(ctx, `
		INSERT INTO processed_events (event_id) VALUES ($1)
		ON CONFLICT (event_id) DO NOTHING`, eventID)
	if err != nil {
		return false, err
	}
	if ct.RowsAffected() == 0 {
		return false, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO order_confirmed_effects (event_id, order_id, processed_at)
		VALUES ($1,$2,now())
		ON CONFLICT (event_id) DO NOTHING`, eventID, orderID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) CountConfirmedEffects(ctx context.Context, orderID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM order_confirmed_effects WHERE order_id=$1`, orderID).Scan(&n)
	return n, err
}

func (s *Store) InsertOutboxForTest(ctx context.Context, eventID, eventType, aggregateID, corr string, payload []byte, createdAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO outbox (event_id, event_type, schema_version, aggregate_id, payload, correlation_id, created_at)
		VALUES ($1,$2,1,$3,$4,$5,$6)
		ON CONFLICT (event_id) DO NOTHING`, eventID, eventType, aggregateID, payload, corr, createdAt)
	return err
}

func (s *Store) IsOutboxPublished(ctx context.Context, eventID string) (bool, error) {
	var published *time.Time
	err := s.pool.QueryRow(ctx, `SELECT published_at FROM outbox WHERE event_id=$1`, eventID).Scan(&published)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return published != nil, nil
}

// QuarantinedEvent is a safe operator view of a poisoned outbox row (no full payload dump by default).
type QuarantinedEvent struct {
	ID            int64      `json:"id"`
	EventID       string     `json:"event_id"`
	EventType     string     `json:"event_type"`
	AggregateID   string     `json:"aggregate_id"`
	AttemptCount  int        `json:"attempt_count"`
	ReplayCount   int        `json:"replay_count"`
	LastError     *string    `json:"last_error,omitempty"`
	LastReplayAt  *time.Time `json:"last_replay_at,omitempty"`
	LastReplayBy  *string    `json:"last_replay_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	CorrelationID string     `json:"correlation_id"`
}

func (s *Store) ListQuarantined(ctx context.Context, limit int) ([]QuarantinedEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, event_id, event_type, aggregate_id, attempt_count, replay_count,
		       last_error, last_replay_at, last_replay_by, created_at, correlation_id
		FROM outbox
		WHERE quarantine = true AND published_at IS NULL
		ORDER BY id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QuarantinedEvent
	for rows.Next() {
		var e QuarantinedEvent
		if err := rows.Scan(&e.ID, &e.EventID, &e.EventType, &e.AggregateID, &e.AttemptCount, &e.ReplayCount,
			&e.LastError, &e.LastReplayAt, &e.LastReplayBy, &e.CreatedAt, &e.CorrelationID); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RequestReplay clears quarantine for an unpublished event, preserving event_id and payload.
// Concurrent requests serialize on FOR UPDATE. Idempotent if already un-quarantined and unpublished.
func (s *Store) RequestReplay(ctx context.Context, eventID, actor string, now time.Time) (QuarantinedEvent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return QuarantinedEvent{}, err
	}
	defer tx.Rollback(ctx)

	var e QuarantinedEvent
	var published *time.Time
	var quarantine bool
	err = tx.QueryRow(ctx, `
		SELECT id, event_id, event_type, aggregate_id, attempt_count, replay_count,
		       last_error, last_replay_at, last_replay_by, created_at, correlation_id,
		       published_at, quarantine
		FROM outbox WHERE event_id=$1 FOR UPDATE`, eventID).
		Scan(&e.ID, &e.EventID, &e.EventType, &e.AggregateID, &e.AttemptCount, &e.ReplayCount,
			&e.LastError, &e.LastReplayAt, &e.LastReplayBy, &e.CreatedAt, &e.CorrelationID,
			&published, &quarantine)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	if published != nil {
		return e, fmt.Errorf("%w: already published", ErrConflict)
	}
	// If currently leased, clear lease so publisher can reclaim after replay.
	err = tx.QueryRow(ctx, `
		UPDATE outbox SET
			quarantine = false,
			locked_until = NULL,
			locked_by = NULL,
			replay_count = replay_count + 1,
			last_replay_at = $2,
			last_replay_by = $3,
			last_error = CASE
				WHEN last_error IS NULL OR last_error = '' THEN 'replay requested'
				ELSE left(last_error || '; replay requested', 2000)
			END
		WHERE id=$1
		RETURNING id, event_id, event_type, aggregate_id, attempt_count, replay_count,
		          last_error, last_replay_at, last_replay_by, created_at, correlation_id`,
		e.ID, now, actor,
	).Scan(&e.ID, &e.EventID, &e.EventType, &e.AggregateID, &e.AttemptCount, &e.ReplayCount,
		&e.LastError, &e.LastReplayAt, &e.LastReplayBy, &e.CreatedAt, &e.CorrelationID)
	if err != nil {
		return e, err
	}
	if err := tx.Commit(ctx); err != nil {
		return e, err
	}
	return e, nil
}

// ForceQuarantineForTest marks an event quarantined (tests).
func (s *Store) ForceQuarantineForTest(ctx context.Context, eventID string, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE outbox SET quarantine=true, attempt_count=GREATEST(attempt_count,25), last_error=$2,
			locked_until=NULL, locked_by=NULL
		WHERE event_id=$1 AND published_at IS NULL`, eventID, errMsg)
	return err
}
