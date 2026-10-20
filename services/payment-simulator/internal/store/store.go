package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")
var ErrIdempotency = errors.New("idempotency conflict")

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type Payment struct {
	ID          string    `json:"id"`
	OperationID string    `json:"operation_id"`
	OrderID     string    `json:"order_id"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
	Scenario    string    `json:"scenario"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s *Store) CreateOrGet(ctx context.Context, p Payment) (Payment, error) {
	var existing Payment
	err := s.pool.QueryRow(ctx, `
		SELECT id, operation_id, order_id, amount_cents, currency, scenario, status, created_at, updated_at
		FROM payment_attempts WHERE operation_id=$1`, p.OperationID).
		Scan(&existing.ID, &existing.OperationID, &existing.OrderID, &existing.AmountCents, &existing.Currency, &existing.Scenario, &existing.Status, &existing.CreatedAt, &existing.UpdatedAt)
	if err == nil {
		if existing.OrderID != p.OrderID || existing.AmountCents != p.AmountCents {
			return existing, ErrIdempotency
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO payment_attempts (id, operation_id, order_id, amount_cents, currency, scenario, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		p.ID, p.OperationID, p.OrderID, p.AmountCents, p.Currency, p.Scenario, p.Status, p.CreatedAt, p.UpdatedAt)
	return p, err
}

func (s *Store) UpdateStatus(ctx context.Context, id, status string, now time.Time) (Payment, error) {
	var p Payment
	err := s.pool.QueryRow(ctx, `
		UPDATE payment_attempts SET status=$2, updated_at=$3 WHERE id=$1
		RETURNING id, operation_id, order_id, amount_cents, currency, scenario, status, created_at, updated_at`,
		id, status, now,
	).Scan(&p.ID, &p.OperationID, &p.OrderID, &p.AmountCents, &p.Currency, &p.Scenario, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) GetByOperation(ctx context.Context, op string) (Payment, error) {
	var p Payment
	err := s.pool.QueryRow(ctx, `
		SELECT id, operation_id, order_id, amount_cents, currency, scenario, status, created_at, updated_at
		FROM payment_attempts WHERE operation_id=$1`, op).
		Scan(&p.ID, &p.OperationID, &p.OrderID, &p.AmountCents, &p.Currency, &p.Scenario, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}
