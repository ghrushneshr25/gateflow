-- Third-party buyer identities (no local passwords). Google first; more providers later.
CREATE TABLE IF NOT EXISTS buyer_identities (
  id                TEXT PRIMARY KEY,
  provider          TEXT NOT NULL,
  provider_subject  TEXT NOT NULL,
  email             TEXT NOT NULL DEFAULT '',
  email_verified    BOOLEAN NOT NULL DEFAULT FALSE,
  display_name      TEXT NOT NULL DEFAULT '',
  picture_url       TEXT NOT NULL DEFAULT '',
  buyer_id          TEXT NOT NULL,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_login_at     TIMESTAMPTZ,
  UNIQUE (provider, provider_subject)
);

CREATE INDEX IF NOT EXISTS buyer_identities_buyer_id_idx ON buyer_identities (buyer_id);
CREATE INDEX IF NOT EXISTS buyer_identities_email_idx ON buyer_identities (lower(email));
