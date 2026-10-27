package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type OperatorUser struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"display_name"`
	Scopes       []string  `json:"scopes"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (s *Store) CreateOperatorUser(ctx context.Context, u OperatorUser) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO operator_users (id, username, password_hash, display_name, scopes, active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		u.ID, strings.ToLower(strings.TrimSpace(u.Username)), u.PasswordHash, u.DisplayName, u.Scopes, u.Active, u.CreatedAt, u.UpdatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			return ErrConflict
		}
		return err
	}
	return nil
}

func (s *Store) GetOperatorUserByUsername(ctx context.Context, username string) (OperatorUser, error) {
	var u OperatorUser
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, display_name, scopes, active, created_at, updated_at
		FROM operator_users WHERE lower(username)=lower($1)`, strings.TrimSpace(username)).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Scopes, &u.Active, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (s *Store) ListOperatorUsers(ctx context.Context) ([]OperatorUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, username, display_name, scopes, active, created_at, updated_at
		FROM operator_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OperatorUser
	for rows.Next() {
		var u OperatorUser
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Scopes, &u.Active, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CountOperatorUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*)::int FROM operator_users`).Scan(&n)
	return n, err
}
