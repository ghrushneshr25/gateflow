package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type Offering struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Session struct {
	ID                 string    `json:"id"`
	OfferingID         string    `json:"offering_id"`
	StartsAt           time.Time `json:"starts_at"`
	EndsAt             time.Time `json:"ends_at"`
	SaleOpensAt        time.Time `json:"sale_opens_at"`
	SaleClosesAt       time.Time `json:"sale_closes_at"`
	Status             string    `json:"status"`
	AdmissionStrategy  string    `json:"admission_strategy"`
	CreatedAt          time.Time `json:"created_at"`
}

type TicketType struct {
	ID              string    `json:"id"`
	SessionID       string    `json:"session_id"`
	Name            string    `json:"name"`
	Section         string    `json:"section"`
	Capacity        int       `json:"capacity"`
	PriceCents      int64     `json:"price_cents"`
	Currency        string    `json:"currency"`
	InventoryPoolID *string   `json:"inventory_pool_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type PurchasePolicy struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"session_id"`
	Version     int       `json:"version"`
	MaxPerBuyer int       `json:"max_per_buyer"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) CreateOffering(ctx context.Context, o Offering) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO offerings (id, name, description, category, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		o.ID, o.Name, o.Description, o.Category, o.Status, o.CreatedAt, o.UpdatedAt)
	return err
}

func (s *Store) GetOffering(ctx context.Context, id string) (Offering, error) {
	var o Offering
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, description, category, status, created_at, updated_at
		FROM offerings WHERE id=$1`, id).
		Scan(&o.ID, &o.Name, &o.Description, &o.Category, &o.Status, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	return o, err
}

func (s *Store) ListOfferings(ctx context.Context, publishedOnly bool) ([]Offering, error) {
	q := `SELECT id, name, description, category, status, created_at, updated_at FROM offerings`
	if publishedOnly {
		q += ` WHERE status='PUBLISHED'`
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Offering
	for rows.Next() {
		var o Offering
		if err := rows.Scan(&o.ID, &o.Name, &o.Description, &o.Category, &o.Status, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) CreateSession(ctx context.Context, sess Session, policy PurchasePolicy) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
		INSERT INTO sessions (id, offering_id, starts_at, ends_at, sale_opens_at, sale_closes_at, status, admission_strategy, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		sess.ID, sess.OfferingID, sess.StartsAt, sess.EndsAt, sess.SaleOpensAt, sess.SaleClosesAt, sess.Status, sess.AdmissionStrategy, sess.CreatedAt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO purchase_policies (id, session_id, version, max_per_buyer, created_at)
		VALUES ($1,$2,$3,$4,$5)`,
		policy.ID, policy.SessionID, policy.Version, policy.MaxPerBuyer, policy.CreatedAt)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListSessions(ctx context.Context, offeringID string) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, offering_id, starts_at, ends_at, sale_opens_at, sale_closes_at, status, admission_strategy, created_at
		FROM sessions WHERE offering_id=$1 ORDER BY starts_at`, offeringID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.OfferingID, &sess.StartsAt, &sess.EndsAt, &sess.SaleOpensAt, &sess.SaleClosesAt, &sess.Status, &sess.AdmissionStrategy, &sess.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	var sess Session
	err := s.pool.QueryRow(ctx, `
		SELECT id, offering_id, starts_at, ends_at, sale_opens_at, sale_closes_at, status, admission_strategy, created_at
		FROM sessions WHERE id=$1`, id).
		Scan(&sess.ID, &sess.OfferingID, &sess.StartsAt, &sess.EndsAt, &sess.SaleOpensAt, &sess.SaleClosesAt, &sess.Status, &sess.AdmissionStrategy, &sess.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return sess, ErrNotFound
	}
	return sess, err
}

func (s *Store) CreateTicketType(ctx context.Context, tt TicketType) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ticket_types (id, session_id, name, section, capacity, price_cents, currency, inventory_pool_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		tt.ID, tt.SessionID, tt.Name, tt.Section, tt.Capacity, tt.PriceCents, tt.Currency, tt.InventoryPoolID, tt.CreatedAt)
	return err
}

func (s *Store) ListTicketTypes(ctx context.Context, sessionID string) ([]TicketType, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, session_id, name, section, capacity, price_cents, currency, inventory_pool_id, created_at
		FROM ticket_types WHERE session_id=$1 ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TicketType
	for rows.Next() {
		var tt TicketType
		if err := rows.Scan(&tt.ID, &tt.SessionID, &tt.Name, &tt.Section, &tt.Capacity, &tt.PriceCents, &tt.Currency, &tt.InventoryPoolID, &tt.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, tt)
	}
	return out, rows.Err()
}

func (s *Store) SetTicketPoolID(ctx context.Context, ticketTypeID, poolID string) error {
	ct, err := s.pool.Exec(ctx, `UPDATE ticket_types SET inventory_pool_id=$2 WHERE id=$1`, ticketTypeID, poolID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) LatestPolicy(ctx context.Context, sessionID string) (PurchasePolicy, error) {
	var p PurchasePolicy
	err := s.pool.QueryRow(ctx, `
		SELECT id, session_id, version, max_per_buyer, created_at
		FROM purchase_policies WHERE session_id=$1 ORDER BY version DESC LIMIT 1`, sessionID).
		Scan(&p.ID, &p.SessionID, &p.Version, &p.MaxPerBuyer, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) PublishOffering(ctx context.Context, offeringID string, now time.Time) (Offering, []Session, []TicketType, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Offering{}, nil, nil, err
	}
	defer tx.Rollback(ctx)

	var o Offering
	err = tx.QueryRow(ctx, `
		SELECT id, name, description, category, status, created_at, updated_at
		FROM offerings WHERE id=$1 FOR UPDATE`, offeringID).
		Scan(&o.ID, &o.Name, &o.Description, &o.Category, &o.Status, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, nil, nil, ErrNotFound
	}
	if err != nil {
		return o, nil, nil, err
	}
	if o.Status == "PUBLISHED" {
		sessions, err := s.listSessionsTx(ctx, tx, offeringID)
		if err != nil {
			return o, nil, nil, err
		}
		var tts []TicketType
		for _, sess := range sessions {
			items, err := s.listTicketTypesTx(ctx, tx, sess.ID)
			if err != nil {
				return o, nil, nil, err
			}
			tts = append(tts, items...)
		}
		return o, sessions, tts, nil
	}
	if o.Status != "DRAFT" && o.Status != "UNPUBLISHED" {
		return o, nil, nil, fmt.Errorf("%w: cannot publish from %s", ErrConflict, o.Status)
	}

	sessions, err := s.listSessionsTx(ctx, tx, offeringID)
	if err != nil {
		return o, nil, nil, err
	}
	if len(sessions) == 0 {
		return o, nil, nil, fmt.Errorf("%w: at least one session required", ErrConflict)
	}
	var allTT []TicketType
	for _, sess := range sessions {
		tts, err := s.listTicketTypesTx(ctx, tx, sess.ID)
		if err != nil {
			return o, nil, nil, err
		}
		if len(tts) == 0 {
			return o, nil, nil, fmt.Errorf("%w: session %s needs ticket types", ErrConflict, sess.ID)
		}
		allTT = append(allTT, tts...)
	}

	_, err = tx.Exec(ctx, `UPDATE offerings SET status='PUBLISHED', updated_at=$2 WHERE id=$1`, offeringID, now)
	if err != nil {
		return o, nil, nil, err
	}
	o.Status = "PUBLISHED"
	o.UpdatedAt = now
	if err := tx.Commit(ctx); err != nil {
		return o, nil, nil, err
	}
	return o, sessions, allTT, nil
}

func (s *Store) listSessionsTx(ctx context.Context, tx pgx.Tx, offeringID string) ([]Session, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, offering_id, starts_at, ends_at, sale_opens_at, sale_closes_at, status, admission_strategy, created_at
		FROM sessions WHERE offering_id=$1`, offeringID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.OfferingID, &sess.StartsAt, &sess.EndsAt, &sess.SaleOpensAt, &sess.SaleClosesAt, &sess.Status, &sess.AdmissionStrategy, &sess.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) listTicketTypesTx(ctx context.Context, tx pgx.Tx, sessionID string) ([]TicketType, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, session_id, name, section, capacity, price_cents, currency, inventory_pool_id, created_at
		FROM ticket_types WHERE session_id=$1`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TicketType
	for rows.Next() {
		var tt TicketType
		if err := rows.Scan(&tt.ID, &tt.SessionID, &tt.Name, &tt.Section, &tt.Capacity, &tt.PriceCents, &tt.Currency, &tt.InventoryPoolID, &tt.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, tt)
	}
	return out, rows.Err()
}
