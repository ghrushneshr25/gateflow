package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrCapacityExceeded = errors.New("capacity exceeded")
	ErrNotActive        = errors.New("not active")
	ErrExpired          = errors.New("expired")
	ErrIdempotency      = errors.New("idempotency conflict")
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type Pool struct {
	ID                string    `json:"id"`
	SessionID         string    `json:"session_id"`
	TicketTypeID      string    `json:"ticket_type_id"`
	Name              string    `json:"name"`
	SellableCapacity  int       `json:"sellable_capacity"`
	ConfirmedQty      int       `json:"confirmed_qty"`
	ActiveReservedQty int       `json:"active_reserved_qty"`
	CreatedAt         time.Time `json:"created_at"`
}

type Reservation struct {
	ID          string    `json:"id"`
	OperationID string    `json:"operation_id"`
	PoolID      string    `json:"pool_id"`
	Quantity    int       `json:"quantity"`
	BuyerID     string    `json:"buyer_id"`
	OrderID     string    `json:"order_id"`
	Status      string    `json:"status"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s *Store) UpsertPool(ctx context.Context, p Pool) (Pool, error) {
	var out Pool
	err := s.pool.QueryRow(ctx, `
		INSERT INTO pools (id, session_id, ticket_type_id, name, sellable_capacity, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (ticket_type_id) DO UPDATE SET
			name = EXCLUDED.name,
			sellable_capacity = EXCLUDED.sellable_capacity
		RETURNING id, session_id, ticket_type_id, name, sellable_capacity, confirmed_qty, active_reserved_qty, created_at`,
		p.ID, p.SessionID, p.TicketTypeID, p.Name, p.SellableCapacity, p.CreatedAt,
	).Scan(&out.ID, &out.SessionID, &out.TicketTypeID, &out.Name, &out.SellableCapacity, &out.ConfirmedQty, &out.ActiveReservedQty, &out.CreatedAt)
	return out, err
}

func (s *Store) GetPool(ctx context.Context, id string) (Pool, error) {
	var p Pool
	err := s.pool.QueryRow(ctx, `
		SELECT id, session_id, ticket_type_id, name, sellable_capacity, confirmed_qty, active_reserved_qty, created_at
		FROM pools WHERE id=$1`, id).
		Scan(&p.ID, &p.SessionID, &p.TicketTypeID, &p.Name, &p.SellableCapacity, &p.ConfirmedQty, &p.ActiveReservedQty, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) Reserve(ctx context.Context, res Reservation) (Reservation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Reservation{}, err
	}
	defer tx.Rollback(ctx)

	// Idempotent lookup inside the transaction (lost-response safe).
	var existing Reservation
	err = tx.QueryRow(ctx, `
		SELECT id, operation_id, pool_id, quantity, buyer_id, order_id, status, expires_at, created_at, updated_at
		FROM reservations WHERE operation_id=$1 FOR UPDATE`, res.OperationID).
		Scan(&existing.ID, &existing.OperationID, &existing.PoolID, &existing.Quantity, &existing.BuyerID, &existing.OrderID, &existing.Status, &existing.ExpiresAt, &existing.CreatedAt, &existing.UpdatedAt)
	if err == nil {
		if existing.PoolID != res.PoolID || existing.Quantity != res.Quantity || existing.BuyerID != res.BuyerID || existing.OrderID != res.OrderID {
			return existing, ErrIdempotency
		}
		if err := tx.Commit(ctx); err != nil {
			return Reservation{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, err
	}

	var capacity, confirmed, reserved int
	err = tx.QueryRow(ctx, `
		SELECT sellable_capacity, confirmed_qty, active_reserved_qty
		FROM pools WHERE id=$1 FOR UPDATE`, res.PoolID).
		Scan(&capacity, &confirmed, &reserved)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrNotFound
	}
	if err != nil {
		return Reservation{}, err
	}
	if confirmed+reserved+res.Quantity > capacity {
		return Reservation{}, ErrCapacityExceeded
	}

	_, err = tx.Exec(ctx, `UPDATE pools SET active_reserved_qty = active_reserved_qty + $2 WHERE id=$1`, res.PoolID, res.Quantity)
	if err != nil {
		return Reservation{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO reservations (id, operation_id, pool_id, quantity, buyer_id, order_id, status, expires_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		res.ID, res.OperationID, res.PoolID, res.Quantity, res.BuyerID, res.OrderID, "ACTIVE", res.ExpiresAt, res.CreatedAt, res.UpdatedAt)
	if err != nil {
		return Reservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Reservation{}, err
	}
	res.Status = "ACTIVE"
	return res, nil
}

func (s *Store) GetByOperation(ctx context.Context, operationID string) (Reservation, error) {
	var r Reservation
	err := s.pool.QueryRow(ctx, `
		SELECT id, operation_id, pool_id, quantity, buyer_id, order_id, status, expires_at, created_at, updated_at
		FROM reservations WHERE operation_id=$1`, operationID).
		Scan(&r.ID, &r.OperationID, &r.PoolID, &r.Quantity, &r.BuyerID, &r.OrderID, &r.Status, &r.ExpiresAt, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) GetReservation(ctx context.Context, id string) (Reservation, error) {
	var r Reservation
	err := s.pool.QueryRow(ctx, `
		SELECT id, operation_id, pool_id, quantity, buyer_id, order_id, status, expires_at, created_at, updated_at
		FROM reservations WHERE id=$1`, id).
		Scan(&r.ID, &r.OperationID, &r.PoolID, &r.Quantity, &r.BuyerID, &r.OrderID, &r.Status, &r.ExpiresAt, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) Confirm(ctx context.Context, id string, now time.Time) (Reservation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Reservation{}, err
	}
	defer tx.Rollback(ctx)

	var r Reservation
	err = tx.QueryRow(ctx, `
		SELECT id, operation_id, pool_id, quantity, buyer_id, order_id, status, expires_at, created_at, updated_at
		FROM reservations WHERE id=$1 FOR UPDATE`, id).
		Scan(&r.ID, &r.OperationID, &r.PoolID, &r.Quantity, &r.BuyerID, &r.OrderID, &r.Status, &r.ExpiresAt, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if r.Status == "CONFIRMED" {
		return r, nil
	}
	if r.Status != "ACTIVE" {
		return r, ErrNotActive
	}
	if !r.ExpiresAt.After(now) {
		// expire path
		_, err = tx.Exec(ctx, `
			UPDATE reservations SET status='EXPIRED', updated_at=$2 WHERE id=$1`, id, now)
		if err != nil {
			return r, err
		}
		_, err = tx.Exec(ctx, `
			UPDATE pools SET active_reserved_qty = active_reserved_qty - $2 WHERE id=$1`, r.PoolID, r.Quantity)
		if err != nil {
			return r, err
		}
		if err := tx.Commit(ctx); err != nil {
			return r, err
		}
		r.Status = "EXPIRED"
		return r, ErrExpired
	}

	_, err = tx.Exec(ctx, `
		UPDATE reservations SET status='CONFIRMED', updated_at=$2 WHERE id=$1`, id, now)
	if err != nil {
		return r, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE pools SET active_reserved_qty = active_reserved_qty - $2,
		                 confirmed_qty = confirmed_qty + $2
		WHERE id=$1`, r.PoolID, r.Quantity)
	if err != nil {
		return r, err
	}
	if err := tx.Commit(ctx); err != nil {
		return r, err
	}
	r.Status = "CONFIRMED"
	r.UpdatedAt = now
	return r, nil
}

// Expire transitions ACTIVE → EXPIRED when deadline has passed.
// Safe under concurrent Confirm: only one winner via FOR UPDATE + status predicate.
func (s *Store) Expire(ctx context.Context, id string, now time.Time) (Reservation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Reservation{}, err
	}
	defer tx.Rollback(ctx)

	var r Reservation
	err = tx.QueryRow(ctx, `
		SELECT id, operation_id, pool_id, quantity, buyer_id, order_id, status, expires_at, created_at, updated_at
		FROM reservations WHERE id=$1 FOR UPDATE`, id).
		Scan(&r.ID, &r.OperationID, &r.PoolID, &r.Quantity, &r.BuyerID, &r.OrderID, &r.Status, &r.ExpiresAt, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if r.Status == "EXPIRED" || r.Status == "RELEASED" {
		return r, nil
	}
	if r.Status == "CONFIRMED" {
		return r, ErrNotActive
	}
	if r.Status != "ACTIVE" {
		return r, ErrNotActive
	}
	if r.ExpiresAt.After(now) {
		// Not yet due — do not expire early.
		return r, nil
	}
	ct, err := tx.Exec(ctx, `
		UPDATE reservations SET status='EXPIRED', updated_at=$2
		WHERE id=$1 AND status='ACTIVE'`, id, now)
	if err != nil {
		return r, err
	}
	if ct.RowsAffected() == 0 {
		// Lost race to confirm/release.
		err = tx.QueryRow(ctx, `
			SELECT id, operation_id, pool_id, quantity, buyer_id, order_id, status, expires_at, created_at, updated_at
			FROM reservations WHERE id=$1`, id).
			Scan(&r.ID, &r.OperationID, &r.PoolID, &r.Quantity, &r.BuyerID, &r.OrderID, &r.Status, &r.ExpiresAt, &r.CreatedAt, &r.UpdatedAt)
		return r, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE pools SET active_reserved_qty = active_reserved_qty - $2 WHERE id=$1`, r.PoolID, r.Quantity)
	if err != nil {
		return r, err
	}
	if err := tx.Commit(ctx); err != nil {
		return r, err
	}
	r.Status = "EXPIRED"
	r.UpdatedAt = now
	return r, nil
}

// ExpireDue expires all ACTIVE reservations with expires_at <= now. Returns count expired.
func (s *Store) ExpireDue(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM reservations
		WHERE status='ACTIVE' AND expires_at <= $1
		ORDER BY expires_at
		LIMIT $2`, now, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		r, err := s.Expire(ctx, id, now)
		if err != nil {
			continue
		}
		if r.Status == "EXPIRED" {
			n++
		}
	}
	return n, nil
}

func (s *Store) Release(ctx context.Context, id string, now time.Time) (Reservation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Reservation{}, err
	}
	defer tx.Rollback(ctx)

	var r Reservation
	err = tx.QueryRow(ctx, `
		SELECT id, operation_id, pool_id, quantity, buyer_id, order_id, status, expires_at, created_at, updated_at
		FROM reservations WHERE id=$1 FOR UPDATE`, id).
		Scan(&r.ID, &r.OperationID, &r.PoolID, &r.Quantity, &r.BuyerID, &r.OrderID, &r.Status, &r.ExpiresAt, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if r.Status == "RELEASED" || r.Status == "EXPIRED" {
		return r, nil
	}
	if r.Status != "ACTIVE" {
		return r, ErrNotActive
	}
	_, err = tx.Exec(ctx, `UPDATE reservations SET status='RELEASED', updated_at=$2 WHERE id=$1`, id, now)
	if err != nil {
		return r, err
	}
	_, err = tx.Exec(ctx, `UPDATE pools SET active_reserved_qty = active_reserved_qty - $2 WHERE id=$1`, r.PoolID, r.Quantity)
	if err != nil {
		return r, err
	}
	if err := tx.Commit(ctx); err != nil {
		return r, err
	}
	r.Status = "RELEASED"
	r.UpdatedAt = now
	return r, nil
}
