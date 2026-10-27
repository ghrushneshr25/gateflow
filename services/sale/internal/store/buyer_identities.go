package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type BuyerIdentity struct {
	ID              string     `json:"id"`
	Provider        string     `json:"provider"`
	ProviderSubject string     `json:"provider_subject"`
	Email           string     `json:"email"`
	EmailVerified   bool       `json:"email_verified"`
	DisplayName     string     `json:"display_name"`
	PictureURL      string     `json:"picture_url,omitempty"`
	BuyerID         string     `json:"buyer_id"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
}

func (s *Store) UpsertBuyerIdentity(ctx context.Context, in BuyerIdentity) (BuyerIdentity, error) {
	now := time.Now().UTC()
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	in.UpdatedAt = now
	in.LastLoginAt = &now
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	var out BuyerIdentity
	err := s.pool.QueryRow(ctx, `
		INSERT INTO buyer_identities (
			id, provider, provider_subject, email, email_verified, display_name, picture_url, buyer_id,
			created_at, updated_at, last_login_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (provider, provider_subject) DO UPDATE SET
			email = EXCLUDED.email,
			email_verified = EXCLUDED.email_verified,
			display_name = EXCLUDED.display_name,
			picture_url = EXCLUDED.picture_url,
			updated_at = EXCLUDED.updated_at,
			last_login_at = EXCLUDED.last_login_at
		RETURNING id, provider, provider_subject, email, email_verified, display_name, picture_url, buyer_id,
			created_at, updated_at, last_login_at`,
		in.ID, in.Provider, in.ProviderSubject, in.Email, in.EmailVerified, in.DisplayName, in.PictureURL, in.BuyerID,
		in.CreatedAt, in.UpdatedAt, in.LastLoginAt,
	).Scan(
		&out.ID, &out.Provider, &out.ProviderSubject, &out.Email, &out.EmailVerified, &out.DisplayName, &out.PictureURL, &out.BuyerID,
		&out.CreatedAt, &out.UpdatedAt, &out.LastLoginAt,
	)
	return out, err
}

func (s *Store) GetBuyerIdentityByProvider(ctx context.Context, provider, subject string) (BuyerIdentity, error) {
	var out BuyerIdentity
	err := s.pool.QueryRow(ctx, `
		SELECT id, provider, provider_subject, email, email_verified, display_name, picture_url, buyer_id,
			created_at, updated_at, last_login_at
		FROM buyer_identities WHERE provider=$1 AND provider_subject=$2`, provider, subject).
		Scan(&out.ID, &out.Provider, &out.ProviderSubject, &out.Email, &out.EmailVerified, &out.DisplayName, &out.PictureURL, &out.BuyerID,
			&out.CreatedAt, &out.UpdatedAt, &out.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, err
}
