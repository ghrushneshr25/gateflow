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
	ID            string     `json:"id"`
	OperationID   string     `json:"operation_id"`
	OrderID       string     `json:"order_id"`
	AmountCents   int64      `json:"amount_cents"`
	Currency      string     `json:"currency"`
	Scenario      string     `json:"scenario"`
	Status        string     `json:"status"`
	ResolveAfter  *time.Time `json:"resolve_after,omitempty"`
	CallbackCount int        `json:"callback_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type Compensation struct {
	ID            string     `json:"id"`
	OperationID   string     `json:"operation_id"`
	PaymentID     string     `json:"payment_id"`
	OrderID       string     `json:"order_id"`
	Status        string     `json:"status"`
	AttemptCount  int        `json:"attempt_count"`
	LastError     *string    `json:"last_error,omitempty"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	Escalated     bool       `json:"escalated"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

const payCols = `id, operation_id, order_id, amount_cents, currency, scenario, status, resolve_after, callback_count, created_at, updated_at`

func scanPay(row pgx.Row) (Payment, error) {
	var p Payment
	err := row.Scan(&p.ID, &p.OperationID, &p.OrderID, &p.AmountCents, &p.Currency, &p.Scenario, &p.Status, &p.ResolveAfter, &p.CallbackCount, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

func (s *Store) CreateOrGet(ctx context.Context, p Payment) (Payment, error) {
	existing, err := scanPay(s.pool.QueryRow(ctx, `SELECT `+payCols+` FROM payment_attempts WHERE operation_id=$1`, p.OperationID))
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
		INSERT INTO payment_attempts (id, operation_id, order_id, amount_cents, currency, scenario, status, resolve_after, callback_count, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		p.ID, p.OperationID, p.OrderID, p.AmountCents, p.Currency, p.Scenario, p.Status, p.ResolveAfter, p.CallbackCount, p.CreatedAt, p.UpdatedAt)
	return p, err
}

func (s *Store) UpdateStatus(ctx context.Context, id, status string, now time.Time) (Payment, error) {
	p, err := scanPay(s.pool.QueryRow(ctx, `
		UPDATE payment_attempts SET status=$2, updated_at=$3 WHERE id=$1
		RETURNING `+payCols, id, status, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) GetByOperation(ctx context.Context, op string) (Payment, error) {
	p, err := scanPay(s.pool.QueryRow(ctx, `SELECT `+payCols+` FROM payment_attempts WHERE operation_id=$1`, op))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) GetByID(ctx context.Context, id string) (Payment, error) {
	p, err := scanPay(s.pool.QueryRow(ctx, `SELECT `+payCols+` FROM payment_attempts WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// ApplyCallback sets terminal status idempotently and increments callback_count.
func (s *Store) ApplyCallback(ctx context.Context, operationID, status string, now time.Time) (Payment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Payment{}, err
	}
	defer tx.Rollback(ctx)
	p, err := scanPay(tx.QueryRow(ctx, `SELECT `+payCols+` FROM payment_attempts WHERE operation_id=$1 FOR UPDATE`, operationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.CallbackCount++
	if p.Status == "SUCCEEDED" || p.Status == "FAILED" {
		// already terminal — count callback, keep status
		_, err = tx.Exec(ctx, `UPDATE payment_attempts SET callback_count=$2, updated_at=$3 WHERE id=$1`, p.ID, p.CallbackCount, now)
		if err != nil {
			return p, err
		}
		if err := tx.Commit(ctx); err != nil {
			return p, err
		}
		p.UpdatedAt = now
		return p, nil
	}
	_, err = tx.Exec(ctx, `
		UPDATE payment_attempts SET status=$2, callback_count=$3, updated_at=$4 WHERE id=$1`,
		p.ID, status, p.CallbackCount, now)
	if err != nil {
		return p, err
	}
	if err := tx.Commit(ctx); err != nil {
		return p, err
	}
	p.Status = status
	p.UpdatedAt = now
	return p, nil
}

// ResolveDue moves delay_success payments past resolve_after to SUCCEEDED.
func (s *Store) ResolveDue(ctx context.Context, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id FROM payment_attempts
		WHERE status='PENDING' AND scenario='delay_success' AND resolve_after IS NOT NULL AND resolve_after <= $1
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
	n := 0
	for _, id := range ids {
		if _, err := s.UpdateStatus(ctx, id, "SUCCEEDED", now); err == nil {
			n++
		}
	}
	return n, nil
}

const compCols = `id, operation_id, payment_id, order_id, status, attempt_count, last_error, last_attempt_at, escalated, created_at, updated_at`

func scanComp(row pgx.Row) (Compensation, error) {
	var c Compensation
	err := row.Scan(&c.ID, &c.OperationID, &c.PaymentID, &c.OrderID, &c.Status, &c.AttemptCount, &c.LastError, &c.LastAttemptAt, &c.Escalated, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// CreateOrGetCompensation is idempotent on operation_id.
func (s *Store) CreateOrGetCompensation(ctx context.Context, c Compensation) (Compensation, error) {
	existing, err := scanComp(s.pool.QueryRow(ctx, `SELECT `+compCols+` FROM compensations WHERE operation_id=$1`, c.OperationID))
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Compensation{}, err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO compensations (id, operation_id, payment_id, order_id, status, attempt_count, last_error, last_attempt_at, escalated, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		c.ID, c.OperationID, c.PaymentID, c.OrderID, c.Status, c.AttemptCount, c.LastError, c.LastAttemptAt, c.Escalated, c.CreatedAt, c.UpdatedAt)
	return c, err
}

func (s *Store) GetCompensationByOperation(ctx context.Context, op string) (Compensation, error) {
	c, err := scanComp(s.pool.QueryRow(ctx, `SELECT `+compCols+` FROM compensations WHERE operation_id=$1`, op))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) GetCompensation(ctx context.Context, id string) (Compensation, error) {
	c, err := scanComp(s.pool.QueryRow(ctx, `SELECT `+compCols+` FROM compensations WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// ExecuteCompensationAttempt runs one attempt. forceFail for compensation_fail scenario first attempt.
func (s *Store) ExecuteCompensationAttempt(ctx context.Context, operationID string, forceFail bool, now time.Time) (Compensation, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Compensation{}, err
	}
	defer tx.Rollback(ctx)
	c, err := scanComp(tx.QueryRow(ctx, `SELECT `+compCols+` FROM compensations WHERE operation_id=$1 FOR UPDATE`, operationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if c.Status == "SUCCEEDED" {
		return c, nil
	}
	// Ambiguous: do not start second attempt without query — caller should Get first.
	if c.Status == "UNKNOWN" || c.Status == "PENDING" {
		// Allow retry of PENDING/UNKNOWN only by explicit Execute after Get
	}
	c.AttemptCount++
	c.LastAttemptAt = &now
	var status string
	var lastErr *string
	escalated := c.Escalated
	if forceFail && c.AttemptCount == 1 {
		status = "FAILED"
		msg := "simulated compensation failure"
		lastErr = &msg
		escalated = true
	} else if forceFail && c.AttemptCount > 1 {
		// subsequent manual retries succeed unless still forced — success for recovery demo
		status = "SUCCEEDED"
		lastErr = nil
	} else {
		status = "SUCCEEDED"
	}
	_, err = tx.Exec(ctx, `
		UPDATE compensations SET status=$2, attempt_count=$3, last_error=$4, last_attempt_at=$5, escalated=$6, updated_at=$5
		WHERE id=$1`, c.ID, status, c.AttemptCount, lastErr, now, escalated)
	if err != nil {
		return c, err
	}
	if err := tx.Commit(ctx); err != nil {
		return c, err
	}
	c.Status = status
	c.LastError = lastErr
	c.Escalated = escalated
	c.UpdatedAt = now
	return c, nil
}

// MarkCompensationUnknown for ambiguous outcome simulation.
func (s *Store) MarkCompensationUnknown(ctx context.Context, operationID string, now time.Time) (Compensation, error) {
	c, err := scanComp(s.pool.QueryRow(ctx, `
		UPDATE compensations SET status='UNKNOWN', updated_at=$2, last_attempt_at=$2,
			attempt_count = attempt_count + 1,
			last_error='outcome unknown'
		WHERE operation_id=$1
		RETURNING `+compCols, operationID, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}
