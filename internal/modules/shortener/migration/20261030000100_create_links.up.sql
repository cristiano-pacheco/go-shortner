CREATE TABLE links (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(32) NOT NULL UNIQUE,
    destination TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX links_created_at_id_idx ON links (created_at DESC, id DESC) WHERE deleted_at IS NULL;
