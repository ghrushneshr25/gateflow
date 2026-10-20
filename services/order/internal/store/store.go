package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
	IdempotencyKey       string    `json:"idempotency_key"`
	RequestHash          string    `json:"request_hash"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

const orderSelectCols = `id, buyer_id, session_id, ticket_type_id, pool_id, quantity, status, entitlement_id,
		       checkout_operation_id, inventory_operation_id, payment_operation_id, reservation_id, payment_id,
		       amount_cents, currency, policy_version, max_per_buyer, idempotency_key, request_hash, created_at, updated_at`

func scanOrder(row pgx.Row) (Order, error) {
	var o Order
	err := row.Scan(&o.ID, &o.BuyerID, &o.SessionID, &o.TicketTypeID, &o.PoolID, &o.Quantity, &o.Status, &o.EntitlementID,
		&o.CheckoutOperationID, &o.InventoryOperationID, &o.PaymentOperationID, &o.ReservationID, &o.PaymentID,
		&o.AmountCents, &o.Currency, &o.PolicyVersion, &o.MaxPerBuyer, &o.IdempotencyKey, &o.RequestHash, &o.CreatedAt, &o.UpdatedAt)
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
			policy_version, max_per_buyer, idempotency_key, request_hash, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		o.ID, o.BuyerID, o.SessionID, o.TicketTypeID, o.PoolID, o.Quantity, o.Status, o.EntitlementID,
		o.CheckoutOperationID, o.InventoryOperationID, o.PaymentOperationID, o.AmountCents, o.Currency,
		o.PolicyVersion, o.MaxPerBuyer, o.IdempotencyKey, o.RequestHash, now, now)
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
		UPDATE orders SET status=$2, reservation_id=$3, payment_id=$4, updated_at=$5 WHERE id=$1`,
		o.ID, o.Status, o.ReservationID, o.PaymentID, o.UpdatedAt)
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
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox (event_id, event_type, schema_version, aggregate_id, payload, correlation_id, created_at)
		VALUES ($1,'order.order.confirmed',1,$2,$3,$4,$5)
		ON CONFLICT (event_id) DO NOTHING`, eventID, orderID, raw, corr, now)
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
