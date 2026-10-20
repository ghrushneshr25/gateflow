CREATE TABLE IF NOT EXISTS coordination_state (
    id            TEXT PRIMARY KEY CHECK (id = 'admission'),
    epoch         BIGINT NOT NULL DEFAULT 1 CHECK (epoch >= 1),
    mode          TEXT NOT NULL CHECK (mode IN ('ACTIVE', 'FENCED')),
    fence_reason  TEXT,
    fenced_at     TIMESTAMPTZ,
    fenced_by     TEXT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO coordination_state (id, epoch, mode, updated_at)
VALUES ('admission', 1, 'ACTIVE', now())
ON CONFLICT (id) DO NOTHING;
